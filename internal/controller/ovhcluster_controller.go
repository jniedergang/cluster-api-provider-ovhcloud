/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/cluster-api/util/patch"

	infrav1 "github.com/rancher-sandbox/cluster-api-provider-ovhcloud/api/v1alpha1"
	capiovhmetrics "github.com/rancher-sandbox/cluster-api-provider-ovhcloud/internal/metrics"
	ovhclient "github.com/rancher-sandbox/cluster-api-provider-ovhcloud/pkg/ovh"
	locutil "github.com/rancher-sandbox/cluster-api-provider-ovhcloud/util"
)

const (
	requeueTimeShort  = 30 * time.Second
	requeueTimeMedium = 1 * time.Minute
	requeueTimeLong   = 3 * time.Minute

	apiServerListenerName = "api-server"
	apiServerLBPort       = 6443
	apiServerBackendPort  = 6443
	apiServerProtocol     = "tcp"
	lbAlgorithm           = "roundRobin"

	// RKE2 supervisor / registration port. Worker agents connect here to
	// register with the cluster and fetch CA certificates. Without a listener
	// on this port, nodes can't join a multi-node cluster behind an LB.
	rke2RegisterListenerName = "rke2-register"
	rke2RegisterLBPort       = 9345
	rke2RegisterBackendPort  = 9345

	// Health-monitor tuning. Without an HM the LB pool stays "noMonitor"
	// and routes round-robin to ALL members, including dead/booting CPs
	// during a rolling update or failover. With these defaults a backend
	// is marked unhealthy after ~10 s (2 retries × 5 s delay) of failed
	// TCP probes.
	hmType       = "tcp"
	hmDelay      = 5
	hmTimeout    = 3
	hmMaxRetries = 2
)

// ClusterScope stores context data for the OVHCluster reconciler.
type ClusterScope struct {
	Cluster    *clusterv1.Cluster
	OVHCluster *infrav1.OVHCluster
	OVHClient  *ovhclient.Client
	Logger     logr.Logger
	Ctx        context.Context
}

// OVHClusterReconciler reconciles an OVHCluster object.
type OVHClusterReconciler struct {
	client.Client

	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=ovhclusters,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=ovhclusters/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=ovhclusters/finalizers,verbs=update
//+kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters;clusters/status,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

// Reconcile handles OVHCluster reconciliation.
func (r *OVHClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, rerr error) {
	logger := log.FromContext(ctx)
	ctx = ctrl.LoggerInto(ctx, logger)

	logger.Info("Reconciling OVHCluster ...")

	ovhCluster := &infrav1.OVHCluster{}

	if err := r.Get(ctx, req.NamespacedName, ovhCluster); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("OVHCluster not found")

			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, err
	}

	// Initialize the patch helper
	patchHelper, err := patch.NewHelper(ovhCluster, r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}

	defer func() {
		patchErr := patchHelper.Patch(ctx, ovhCluster)
		if patchErr != nil {
			logger.Error(patchErr, "failed to patch OVHCluster")

			if rerr == nil {
				rerr = patchErr
			}
		}
	}()

	// Get owner Cluster
	ownerCluster, err := util.GetOwnerCluster(ctx, r.Client, ovhCluster.ObjectMeta)
	if err != nil {
		return ctrl.Result{}, err
	}

	if ownerCluster == nil {
		logger.Info("Waiting for Cluster Controller to set OwnerRef on OVHCluster")

		return ctrl.Result{RequeueAfter: requeueTimeShort}, nil
	}

	logger = logger.WithValues("cluster", ownerCluster.Namespace+"/"+ownerCluster.Name)
	ctx = ctrl.LoggerInto(ctx, logger)

	// Create OVH API client
	ovhClient, err := locutil.GetOVHClientFromCluster(ctx, r.Client, ovhCluster, logger)
	if err != nil {
		logger.Error(err, "unable to create OVH client")
		conditions.MarkFalse(ovhCluster, infrav1.OVHConnectionReadyCondition,
			infrav1.OVHConnectionFailedReason, clusterv1.ConditionSeverityError,
			"Failed to create OVH client: %s", err.Error())

		return ctrl.Result{RequeueAfter: requeueTimeShort}, nil
	}

	scope := &ClusterScope{
		Cluster:    ownerCluster,
		OVHCluster: ovhCluster,
		OVHClient:  ovhClient,
		Logger:     logger,
		Ctx:        ctx,
	}

	if !ovhCluster.DeletionTimestamp.IsZero() {
		return r.ReconcileDelete(scope)
	}

	return r.ReconcileNormal(scope)
}

// SetupWithManager sets up the controller with the Manager.
func (r *OVHClusterReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	// Index OVHCluster by identity secret name for watch
	err := mgr.GetFieldIndexer().IndexField(ctx, &infrav1.OVHCluster{},
		".spec.identitySecret.name",
		func(obj client.Object) []string {
			cluster := obj.(*infrav1.OVHCluster)
			if cluster.Spec.IdentitySecret.Name == "" {
				return nil
			}

			return []string{cluster.Spec.IdentitySecret.Name}
		},
	)
	if err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&infrav1.OVHCluster{}).
		Watches(
			&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(r.secretToOVHCluster),
		).
		WatchesRawSource(source.Kind(
			mgr.GetCache(),
			&clusterv1.Cluster{},
			handler.TypedEnqueueRequestsFromMapFunc(r.clusterToOVHCluster),
		)).
		Complete(r)
}

// secretToOVHCluster maps a Secret event to the OVHCluster(s) that reference it.
func (r *OVHClusterReconciler) secretToOVHCluster(ctx context.Context, obj client.Object) []reconcile.Request {
	secret, ok := obj.(*corev1.Secret)
	if !ok {
		return nil
	}

	clusterList := &infrav1.OVHClusterList{}
	err := r.List(ctx, clusterList, client.MatchingFields{
		".spec.identitySecret.name": secret.Name,
	})
	if err != nil {
		return nil
	}

	requests := make([]reconcile.Request, 0, len(clusterList.Items))
	for _, cluster := range clusterList.Items {
		if cluster.Spec.IdentitySecret.Namespace == secret.Namespace {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: cluster.Namespace,
					Name:      cluster.Name,
				},
			})
		}
	}

	return requests
}

// clusterToOVHCluster maps a Cluster event to the OVHCluster it references.
func (r *OVHClusterReconciler) clusterToOVHCluster(ctx context.Context, obj *clusterv1.Cluster) []reconcile.Request {
	if obj.Spec.InfrastructureRef == nil {
		return nil
	}

	if obj.Spec.InfrastructureRef.GroupVersionKind().Kind != "OVHCluster" {
		return nil
	}

	return []reconcile.Request{
		{
			NamespacedName: types.NamespacedName{
				Namespace: obj.Spec.InfrastructureRef.Namespace,
				Name:      obj.Spec.InfrastructureRef.Name,
			},
		},
	}
}

// ReconcileNormal handles create/update of OVH cluster infrastructure.
func (r *OVHClusterReconciler) ReconcileNormal(scope *ClusterScope) (reconcile.Result, error) {
	reconcileStart := time.Now()

	defer func() {
		capiovhmetrics.ClusterReconcileDuration.WithLabelValues("normal").Observe(
			time.Since(reconcileStart).Seconds(),
		)

		clusterName := scope.OVHCluster.Namespace + "/" + scope.OVHCluster.Name
		if scope.OVHCluster.Status.Ready {
			capiovhmetrics.ClusterReady.WithLabelValues(clusterName).Set(1)
		} else {
			capiovhmetrics.ClusterReady.WithLabelValues(clusterName).Set(0)
		}
	}()

	logger := scope.Logger

	// Add finalizer
	if !controllerutil.ContainsFinalizer(scope.OVHCluster, infrav1.ClusterFinalizer) &&
		scope.OVHCluster.DeletionTimestamp.IsZero() {
		controllerutil.AddFinalizer(scope.OVHCluster, infrav1.ClusterFinalizer)

		return ctrl.Result{}, nil
	}

	// Step 1: Validate OVH credentials
	if err := r.reconcileCredentials(scope); err != nil {
		return ctrl.Result{RequeueAfter: requeueTimeShort}, err
	}

	// Step 2: Reconcile network (private network + subnet)
	if err := r.reconcileNetwork(scope); err != nil {
		// Network not yet ACTIVE in region: requeue without surfacing as error
		// (the LB step would fail anyway without subnet).
		if errors.Is(err, errNetworkNotReady) {
			return ctrl.Result{RequeueAfter: requeueTimeShort}, nil
		}

		return ctrl.Result{RequeueAfter: requeueTimeShort}, err
	}

	// Step 3: Reconcile load balancer
	result, err := r.reconcileLoadBalancer(scope)
	if err != nil || result.RequeueAfter > 0 {
		return result, err
	}

	// All infrastructure ready
	scope.OVHCluster.Status.Ready = true
	conditions.MarkTrue(scope.OVHCluster, infrav1.InfrastructureReadyCondition)
	logger.Info("OVH cluster infrastructure is ready",
		"controlPlaneEndpoint", scope.OVHCluster.Spec.ControlPlaneEndpoint)

	// Requeue periodically to reconcile LB members
	return ctrl.Result{RequeueAfter: requeueTimeLong}, nil
}

// reconcileCredentials validates the OVH API connection.
func (r *OVHClusterReconciler) reconcileCredentials(scope *ClusterScope) error {
	err := scope.OVHClient.ValidateCredentials()
	if err != nil {
		conditions.MarkFalse(scope.OVHCluster, infrav1.OVHConnectionReadyCondition,
			infrav1.OVHAuthenticationFailedReason, clusterv1.ConditionSeverityError,
			"Failed to validate OVH credentials: %s", err.Error())

		return fmt.Errorf("validating OVH credentials: %w", err)
	}

	scope.Logger.V(1).Info("OVH credentials validated", "serviceName", scope.OVHCluster.Spec.ServiceName)
	conditions.MarkTrue(scope.OVHCluster, infrav1.OVHConnectionReadyCondition)

	return nil
}

// reconcileNetwork ensures the private network and subnet exist.
func (r *OVHClusterReconciler) reconcileNetwork(scope *ClusterScope) error {
	logger := scope.Logger

	// If no network config, skip network reconciliation
	if scope.OVHCluster.Spec.NetworkConfig == nil {
		logger.V(1).Info("No network config specified, skipping network reconciliation")
		conditions.MarkTrue(scope.OVHCluster, infrav1.NetworkReadyCondition)

		return nil
	}

	netConfig := scope.OVHCluster.Spec.NetworkConfig
	region := scope.OVHCluster.Spec.Region

	// 1. Ensure the network exists (use existing or create new).
	if scope.OVHCluster.Status.NetworkID == "" {
		if netConfig.PrivateNetworkID != "" {
			// Use pre-existing network (user-provided)
			scope.OVHCluster.Status.NetworkID = netConfig.PrivateNetworkID
		} else {
			// Create a new network
			networkName := locutil.GenerateRFC1035Name("capi", scope.Cluster.Name, "net")
			logger.Info("Creating private network", "name", networkName)

			network, err := scope.OVHClient.CreatePrivateNetwork(ovhclient.CreateNetworkOpts{
				Name:    networkName,
				VlanID:  netConfig.VlanID,
				Regions: []string{region},
			})
			if err != nil {
				conditions.MarkFalse(scope.OVHCluster, infrav1.NetworkReadyCondition,
					infrav1.NetworkCreationFailedReason, clusterv1.ConditionSeverityError,
					"Failed to create network: %s", err.Error())

				return fmt.Errorf("creating private network: %w", err)
			}

			scope.OVHCluster.Status.NetworkID = network.ID
			conditions.MarkTrue(scope.OVHCluster, infrav1.NetworkCreatedByControllerCondition)
			logger.Info("Private network created", "networkID", network.ID)
		}
	}

	// 2. Verify network is ACTIVE in the target region (subnet creation otherwise fails).
	net, err := scope.OVHClient.GetPrivateNetwork(scope.OVHCluster.Status.NetworkID)
	if err != nil {
		if ovhclient.IsNotFound(err) {
			logger.Info("Previously known network not found, clearing state")

			scope.OVHCluster.Status.NetworkID = ""
			scope.OVHCluster.Status.SubnetID = ""

			return fmt.Errorf("network %s vanished, will recreate", scope.OVHCluster.Status.NetworkID)
		}

		return fmt.Errorf("getting network %s: %w", scope.OVHCluster.Status.NetworkID, err)
	}

	regionStatus := ""

	for _, r := range net.Regions {
		if r.Region == region {
			regionStatus = r.Status

			break
		}
	}

	if regionStatus != "ACTIVE" {
		conditions.MarkFalse(scope.OVHCluster, infrav1.NetworkReadyCondition,
			infrav1.NetworkCreationFailedReason, clusterv1.ConditionSeverityInfo,
			"Network %s status in %s is %q, waiting for ACTIVE", net.ID, region, regionStatus)
		logger.Info("Network not yet ACTIVE in region, requeueing", "region", region, "status", regionStatus)

		return errNetworkNotReady
	}

	// 3. Ensure a subnet exists. Re-list each iteration so we recover from
	// a previous subnet-creation failure (e.g. region activation race).
	if scope.OVHCluster.Status.SubnetID == "" {
		subnets, err := scope.OVHClient.ListSubnets(scope.OVHCluster.Status.NetworkID)
		if err != nil {
			return fmt.Errorf("listing subnets: %w", err)
		}

		if len(subnets) > 0 {
			scope.OVHCluster.Status.SubnetID = subnets[0].ID
			logger.Info("Found existing subnet", "subnetID", subnets[0].ID, "cidr", subnets[0].CIDR)
		} else if netConfig.SubnetCIDR != "" {
			logger.Info("Creating subnet", "cidr", netConfig.SubnetCIDR)

			start, end := subnetRange(netConfig.SubnetCIDR)

			subnet, err := scope.OVHClient.CreateSubnet(scope.OVHCluster.Status.NetworkID, ovhclient.CreateSubnetOpts{
				Network: netConfig.SubnetCIDR,
				Start:   start,
				End:     end,
				Region:  region,
				DHCP:    true,
			})
			if err != nil {
				return fmt.Errorf("creating subnet: %w", err)
			}

			scope.OVHCluster.Status.SubnetID = subnet.ID
			logger.Info("Subnet created", "subnetID", subnet.ID, "cidr", subnet.CIDR)
		}
	}

	conditions.MarkTrue(scope.OVHCluster, infrav1.NetworkReadyCondition)

	return nil
}

// errNetworkNotReady is a sentinel error indicating the network is not yet
// ACTIVE in the target region. Callers should requeue rather than fail.
var errNetworkNotReady = errors.New("network not yet ACTIVE in region")

// subnetRange computes a default DHCP start/end IP range for a /24 CIDR.
// Convention: reserve .1 for gateway, allocate .2-.254 for DHCP. For non-/24
// CIDRs returns empty strings (caller may then omit start/end).
func subnetRange(cidr string) (start, end string) {
	// Very simple: works for /24 cidrs of form X.Y.Z.0/24
	parts := []byte(cidr)
	idx := -1

	for i, b := range parts {
		if b == '/' {
			idx = i

			break
		}
	}

	if idx == -1 || string(parts[idx:]) != "/24" {
		return "", ""
	}

	prefix := string(parts[:idx])
	// strip last octet
	last := -1

	for i := len(prefix) - 1; i >= 0; i-- {
		if prefix[i] == '.' {
			last = i

			break
		}
	}

	if last == -1 {
		return "", ""
	}

	base := prefix[:last+1]

	return base + "2", base + "254"
}

// reconcileLoadBalancer ensures the Octavia load balancer exists and has a VIP.
func (r *OVHClusterReconciler) reconcileLoadBalancer(scope *ClusterScope) (reconcile.Result, error) {
	logger := scope.Logger

	// If LB already exists, check its status
	if scope.OVHCluster.Status.LoadBalancerID != "" {
		lb, err := scope.OVHClient.GetLoadBalancer(scope.OVHCluster.Status.LoadBalancerID)
		if err != nil {
			if ovhclient.IsNotFound(err) {
				logger.Info("Previously known LB not found, will create new one")

				scope.OVHCluster.Status.LoadBalancerID = ""
				scope.OVHCluster.Status.ListenerID = ""
				scope.OVHCluster.Status.PoolID = ""
			} else {
				return ctrl.Result{}, fmt.Errorf("getting LB %s: %w", scope.OVHCluster.Status.LoadBalancerID, err)
			}
		} else {
			return r.handleExistingLB(scope, lb)
		}
	}

	// Create new LB. Resolve LB flavor (defaults to "small").
	lbFlavorName := scope.OVHCluster.Spec.LoadBalancerConfig.FlavorName
	if lbFlavorName == "" {
		lbFlavorName = "small"
	}

	lbFlavor, err := scope.OVHClient.GetLBFlavorByName(lbFlavorName)
	if err != nil {
		conditions.MarkFalse(scope.OVHCluster, infrav1.LoadBalancerReadyCondition,
			infrav1.LoadBalancerCreationFailedReason, clusterv1.ConditionSeverityError,
			"LB flavor %q not found: %s", lbFlavorName, err.Error())

		return ctrl.Result{}, fmt.Errorf("resolving LB flavor %q: %w", lbFlavorName, err)
	}

	lbName := locutil.GenerateRFC1035Name("capi", scope.Cluster.Name, "lb")
	logger.Info("Creating load balancer", "name", lbName, "flavor", lbFlavorName, "flavorID", lbFlavor.ID)

	subnetID := scope.OVHCluster.Spec.LoadBalancerConfig.SubnetID
	if subnetID == "" {
		subnetID = scope.OVHCluster.Status.SubnetID
	}

	if subnetID == "" || scope.OVHCluster.Status.NetworkID == "" {
		return ctrl.Result{RequeueAfter: requeueTimeShort},
			fmt.Errorf("LB requires both networkID and subnetID, got network=%q subnet=%q",
				scope.OVHCluster.Status.NetworkID, subnetID)
	}

	// The Octavia LB API requires the OpenStack network UUID, not the OVH ID.
	net, err := scope.OVHClient.GetPrivateNetwork(scope.OVHCluster.Status.NetworkID)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("resolving network for LB: %w", err)
	}

	osNetID := net.OpenStackIDForRegion(scope.OVHCluster.Spec.Region)
	if osNetID == "" {
		return ctrl.Result{}, fmt.Errorf("no OpenStack ID for network %s in region %s",
			scope.OVHCluster.Status.NetworkID, scope.OVHCluster.Spec.Region)
	}

	opts := ovhclient.CreateLoadBalancerOpts{
		Name:     lbName,
		FlavorID: lbFlavor.ID,
		Network: ovhclient.LBNetworkConfig{
			Private: ovhclient.LBPrivateNetwork{
				Network: ovhclient.LBNetworkRef{
					ID:       osNetID,
					SubnetID: subnetID,
				},
			},
		},
	}

	lbStart := time.Now()
	lb, err := scope.OVHClient.CreateLoadBalancer(opts)
	capiovhmetrics.LBPollDuration.Observe(time.Since(lbStart).Seconds())
	if err != nil {
		conditions.MarkFalse(scope.OVHCluster, infrav1.LoadBalancerReadyCondition,
			infrav1.LoadBalancerCreationFailedReason, clusterv1.ConditionSeverityError,
			"Failed to create load balancer: %s", err.Error())

		return ctrl.Result{}, fmt.Errorf("creating load balancer: %w", err)
	}

	scope.OVHCluster.Status.LoadBalancerID = lb.ID
	logger.Info("Load balancer created, waiting for ACTIVE", "lbID", lb.ID, "status", lb.ProvisioningStatus)

	return ctrl.Result{RequeueAfter: requeueTimeShort}, nil
}

// handleExistingLB handles a load balancer that already exists.
func (r *OVHClusterReconciler) handleExistingLB(scope *ClusterScope, lb *ovhclient.LoadBalancer) (reconcile.Result, error) {
	logger := scope.Logger

	if lb.ProvisioningStatus != ovhclient.LBProvisioningStatusActive {
		logger.Info("Load balancer not yet ACTIVE", "status", lb.ProvisioningStatus)
		conditions.MarkFalse(scope.OVHCluster, infrav1.LoadBalancerReadyCondition,
			infrav1.LoadBalancerNotReadyReason, clusterv1.ConditionSeverityInfo,
			"Load balancer provisioning: %s", lb.ProvisioningStatus)

		return ctrl.Result{RequeueAfter: requeueTimeShort}, nil
	}

	// LB is ACTIVE — ensure listener and pool exist for the API server
	// (port 6443) and for RKE2 supervisor (port 9345, used for worker node
	// registration).
	if err := r.reconcileLBPort(scope,
		apiServerListenerName, apiServerLBPort,
		&scope.OVHCluster.Status.ListenerID, &scope.OVHCluster.Status.PoolID,
	); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.reconcileLBPort(scope,
		rke2RegisterListenerName, rke2RegisterLBPort,
		&scope.OVHCluster.Status.RegisterListenerID, &scope.OVHCluster.Status.RegisterPoolID,
	); err != nil {
		return ctrl.Result{}, err
	}

	// If a floating IP network is configured, allocate and attach a floating IP
	// to the LB; the public IP becomes the control plane endpoint. Without a
	// floating IP configured, we fall back to the private LB VIP (only usable
	// from inside the OVH vRack).
	endpointHost := lb.VIPAddress

	if scope.OVHCluster.Spec.LoadBalancerConfig.FloatingNetworkID != "" {
		fipIP, err := r.reconcileFloatingIP(scope, lb)
		if err != nil {
			return ctrl.Result{RequeueAfter: requeueTimeShort}, err
		}

		if fipIP == "" {
			// Floating IP not yet ready — don't set the endpoint to the private
			// VIP, we want the public IP once available. Retry soon.
			logger.Info("Waiting for floating IP to be allocated", "lbID", lb.ID)

			return ctrl.Result{RequeueAfter: requeueTimeShort}, nil
		}

		endpointHost = fipIP
	}

	// Set control plane endpoint
	if endpointHost != "" && scope.OVHCluster.Spec.ControlPlaneEndpoint.Host == "" {
		scope.OVHCluster.Spec.ControlPlaneEndpoint = clusterv1.APIEndpoint{
			Host: endpointHost,
			Port: apiServerLBPort,
		}
		logger.Info("Control plane endpoint set", "host", endpointHost, "port", apiServerLBPort)
	}

	conditions.MarkTrue(scope.OVHCluster, infrav1.LoadBalancerReadyCondition)

	return ctrl.Result{}, nil
}

// reconcileFloatingIP allocates a floating IP on the external network and
// attaches it to the LB. The IP is recorded in status.FloatingIPID so it can
// be cleaned up on cluster deletion. Returns the public IP address (or "" if
// not yet ready).
//
// OVH Public Cloud has no standalone floating IP allocation endpoint: the
// only way to get a public IP on an Octavia LB is to POST to
// /loadbalancer/{id}/floatingIp, which allocates and attaches in one call,
// optionally creating an internet gateway on the private network if needed.
func (r *OVHClusterReconciler) reconcileFloatingIP(scope *ClusterScope, lb *ovhclient.LoadBalancer) (string, error) {
	logger := scope.Logger

	// LB already has a floating IP (populated by previous reconcile).
	if lb.FloatingIP != nil && lb.FloatingIP.IP != "" {
		scope.OVHCluster.Status.FloatingIPID = lb.FloatingIP.ID

		if err := r.ensureGatewayExposed(scope); err != nil {
			logger.Info("Warning: failed to expose gateway, will retry", "error", err)
		}

		return lb.FloatingIP.IP, nil
	}

	// Floating IP was allocated in a previous reconcile but the LB response
	// hasn't caught up; fetch it directly.
	if scope.OVHCluster.Status.FloatingIPID != "" {
		if err := r.ensureGatewayExposed(scope); err != nil {
			logger.Info("Warning: failed to expose gateway, will retry", "error", err)
		}

		fip, gerr := scope.OVHClient.GetFloatingIP(scope.OVHCluster.Status.FloatingIPID)
		if gerr == nil && fip != nil && fip.IP != "" {
			return fip.IP, nil
		}

		// FIP ID stale (404) — OVH replaced it during async provisioning.
		// Clear the status and rediscover via the LB / FIP listing.
		if ovhclient.IsNotFound(gerr) {
			logger.Info("Stale FIP ID in status, rediscovering", "staleID", scope.OVHCluster.Status.FloatingIPID)
			scope.OVHCluster.Status.FloatingIPID = ""
		} else {
			return "", nil // keep retrying next reconcile
		}
	}

	// No (or stale) FloatingIPID — try to discover the FIP via LB association.
	if fips, lerr := scope.OVHClient.ListFloatingIPs(); lerr == nil {
		for i := range fips {
			if fips[i].AssociatedEntity != nil && fips[i].AssociatedEntity.ID == lb.ID {
				scope.OVHCluster.Status.FloatingIPID = fips[i].ID

				if fips[i].IP != "" {
					if err := r.ensureGatewayExposed(scope); err != nil {
						logger.Info("Warning: failed to expose gateway, will retry", "error", err)
					}

					return fips[i].IP, nil
				}

				return "", nil
			}
		}
	}

	if lb.VIPAddress == "" {
		return "", nil // LB not yet fully provisioned with a VIP; retry later
	}

	logger.Info("Allocating and attaching floating IP to LB",
		"lbID", lb.ID, "privateIP", lb.VIPAddress)

	gwName := "capi-" + scope.Cluster.Name + "-gw"

	fip, err := scope.OVHClient.CreateLoadBalancerFloatingIP(lb.ID, lb.VIPAddress, gwName)
	if err != nil {
		return "", fmt.Errorf("allocating floating IP for LB: %w", err)
	}

	logger.Info("Floating IP allocation requested", "fipID", fip.ID, "ip", fip.IP, "lbID", lb.ID)

	// The allocate+attach call created an internet gateway on the private
	// subnet. For instances on that subnet to get SNAT outbound internet
	// (needed to download the RKE2 install script, container images, etc.),
	// the gateway must be explicitly exposed.
	if err := r.ensureGatewayExposed(scope); err != nil {
		logger.Info("Warning: failed to expose gateway for outbound internet, will retry",
			"error", err)
	}

	// OVH quirk: the ID returned from CreateLoadBalancerFloatingIP is a
	// transient placeholder — the actually-allocated FIP gets a different
	// ID and is recorded on the LB once provisioning completes. Look up
	// the LB to find the real FIP, falling back to listing FIPs by LB
	// association if the LB hasn't caught up yet.
	if refreshedLB, lerr := scope.OVHClient.GetLoadBalancer(lb.ID); lerr == nil &&
		refreshedLB != nil && refreshedLB.FloatingIP != nil &&
		refreshedLB.FloatingIP.ID != "" {
		scope.OVHCluster.Status.FloatingIPID = refreshedLB.FloatingIP.ID

		if refreshedLB.FloatingIP.IP != "" {
			return refreshedLB.FloatingIP.IP, nil
		}
	} else if fips, lerr := scope.OVHClient.ListFloatingIPs(); lerr == nil {
		for i := range fips {
			if fips[i].AssociatedEntity != nil && fips[i].AssociatedEntity.ID == lb.ID {
				scope.OVHCluster.Status.FloatingIPID = fips[i].ID

				if fips[i].IP != "" {
					return fips[i].IP, nil
				}

				break
			}
		}
	}

	return "", nil // retry next reconcile
}

// ensureGatewayExposed finds the internet gateway on the cluster's private
// subnet and calls POST /gateway/{id}/expose so that instances on the subnet
// get SNAT outbound connectivity. Idempotent: skips if already exposed.
func (r *OVHClusterReconciler) ensureGatewayExposed(scope *ClusterScope) error {
	logger := scope.Logger

	if scope.OVHCluster.Status.GatewayExposed {
		return nil
	}

	// Discover the gateway if we haven't recorded it yet.
	if scope.OVHCluster.Status.GatewayID == "" {
		gws, err := scope.OVHClient.ListGateways()
		if err != nil {
			return fmt.Errorf("listing gateways: %w", err)
		}

		gwName := "capi-" + scope.Cluster.Name + "-gw"

		for i := range gws {
			if gws[i].Name == gwName {
				scope.OVHCluster.Status.GatewayID = gws[i].ID
				logger.Info("Discovered internet gateway", "gatewayID", gws[i].ID, "name", gwName)

				break
			}
		}

		if scope.OVHCluster.Status.GatewayID == "" {
			return fmt.Errorf("no gateway named %s found in region", gwName)
		}
	}

	logger.Info("Exposing gateway to public network for SNAT outbound",
		"gatewayID", scope.OVHCluster.Status.GatewayID)

	if err := scope.OVHClient.ExposeGateway(scope.OVHCluster.Status.GatewayID); err != nil {
		return fmt.Errorf("exposing gateway: %w", err)
	}

	scope.OVHCluster.Status.GatewayExposed = true

	return nil
}

// reconcileLBPort ensures the listener + backend pool + health monitor for
// a single TCP port (e.g. 6443 for kube-apiserver, 9345 for RKE2 supervisor)
// exist on the LB. All operations are idempotent: missing-status fields are
// recovered via Find*ByName, and the health monitor is reattached on every
// reconcile to absorb the OVH "pool immutable" race that locks the pool ~1 s
// after creation.
func (r *OVHClusterReconciler) reconcileLBPort(
	scope *ClusterScope,
	listenerName string,
	port int32,
	listenerIDPtr, poolIDPtr *string,
) error {
	logger := scope.Logger
	lbID := scope.OVHCluster.Status.LoadBalancerID

	if *listenerIDPtr == "" {
		existing, err := scope.OVHClient.FindListenerByName(lbID, listenerName)
		if err != nil {
			return fmt.Errorf("looking up %s listener: %w", listenerName, err)
		}
		if existing != nil {
			logger.Info("Adopting existing listener", "name", listenerName, "listenerID", existing.ID)
			*listenerIDPtr = existing.ID
		} else {
			logger.Info("Creating listener on LB", "name", listenerName, "port", port)
			listener, err := scope.OVHClient.CreateListener(ovhclient.CreateListenerOpts{
				Name:           listenerName,
				Protocol:       apiServerProtocol,
				Port:           port,
				LoadBalancerID: lbID,
			})
			if err != nil {
				return fmt.Errorf("creating %s listener: %w", listenerName, err)
			}
			*listenerIDPtr = listener.ID
			logger.Info("Listener created", "name", listenerName, "listenerID", listener.ID)
		}
	}

	poolName := listenerName + "-pool"
	if *poolIDPtr == "" {
		existing, err := scope.OVHClient.FindPoolByName(lbID, poolName)
		if err != nil {
			return fmt.Errorf("looking up %s pool: %w", poolName, err)
		}
		if existing != nil {
			logger.Info("Adopting existing pool", "name", poolName, "poolID", existing.ID)
			*poolIDPtr = existing.ID
		} else {
			logger.Info("Creating backend pool on LB", "name", poolName)
			pool, err := scope.OVHClient.CreatePool(ovhclient.CreatePoolOpts{
				Name:           poolName,
				Protocol:       apiServerProtocol,
				Algorithm:      lbAlgorithm,
				ListenerID:     *listenerIDPtr,
				LoadBalancerID: lbID,
			})
			if err != nil {
				return fmt.Errorf("creating %s pool: %w", poolName, err)
			}
			*poolIDPtr = pool.ID
			logger.Info("Pool created", "name", poolName, "poolID", pool.ID)
		}
	}

	if err := r.ensurePoolHealthMonitor(scope, *poolIDPtr, poolName); err != nil {
		logger.Info("Warning: failed to attach health monitor, will retry", "pool", poolName, "error", err)
	}

	return nil
}

// ensurePoolHealthMonitor attaches a TCP health monitor to a pool so the
// LB only routes to healthy backends. Idempotent: looks up by name first,
// no-op if already exists. Without this, the pool stays "noMonitor" and
// requests are round-robined to dead/booting CPs during failover.
func (r *OVHClusterReconciler) ensurePoolHealthMonitor(scope *ClusterScope, poolID, poolName string) error {
	hmName := poolName + "-hm"

	existing, err := scope.OVHClient.FindHealthMonitorByName(hmName)
	if err != nil {
		return fmt.Errorf("looking up health monitor %q: %w", hmName, err)
	}

	if existing != nil {
		return nil
	}

	hm, err := scope.OVHClient.CreateHealthMonitor(ovhclient.CreateHealthMonitorOpts{
		Name:        hmName,
		MonitorType: hmType,
		Delay:       hmDelay,
		Timeout:     hmTimeout,
		MaxRetries:  hmMaxRetries,
		PoolID:      poolID,
	})
	if err != nil {
		return fmt.Errorf("creating health monitor %q: %w", hmName, err)
	}

	scope.Logger.Info("Attached TCP health monitor to pool",
		"hmID", hm.ID, "poolID", poolID, "delay", hmDelay, "timeout", hmTimeout, "maxRetries", hmMaxRetries)

	return nil
}

// ReconcileDelete handles deletion of OVH cluster infrastructure.
func (r *OVHClusterReconciler) ReconcileDelete(scope *ClusterScope) (reconcile.Result, error) {
	reconcileStart := time.Now()

	defer func() {
		capiovhmetrics.ClusterReconcileDuration.WithLabelValues("delete").Observe(
			time.Since(reconcileStart).Seconds(),
		)
	}()

	logger := scope.Logger
	logger.Info("Reconciling OVHCluster deletion ...")

	// Capture all FIPs associated with our LB BEFORE we delete the LB:
	// once the LB is gone, the FIPs are detached and we lose the reverse link.
	var fipsToDelete []string
	if scope.OVHCluster.Status.FloatingIPID != "" {
		fipsToDelete = append(fipsToDelete, scope.OVHCluster.Status.FloatingIPID)
	}

	if scope.OVHCluster.Status.LoadBalancerID != "" {
		if fips, err := scope.OVHClient.ListFloatingIPs(); err == nil {
			for i := range fips {
				if fips[i].AssociatedEntity != nil &&
					fips[i].AssociatedEntity.ID == scope.OVHCluster.Status.LoadBalancerID {
					if !slices.Contains(fipsToDelete, fips[i].ID) {
						fipsToDelete = append(fipsToDelete, fips[i].ID)
					}
				}
			}
		}
	}

	// Delete LB
	if scope.OVHCluster.Status.LoadBalancerID != "" {
		// Delete pool members, pool, listener, then LB
		if scope.OVHCluster.Status.PoolID != "" {
			err := scope.OVHClient.DeletePool(scope.OVHCluster.Status.PoolID)
			if err != nil {
				logger.Error(err, "failed to delete pool", "poolID", scope.OVHCluster.Status.PoolID)
			}
		}

		if scope.OVHCluster.Status.ListenerID != "" {
			err := scope.OVHClient.DeleteListener(scope.OVHCluster.Status.ListenerID)
			if err != nil {
				logger.Error(err, "failed to delete listener", "listenerID", scope.OVHCluster.Status.ListenerID)
			}
		}

		logger.Info("Deleting load balancer", "lbID", scope.OVHCluster.Status.LoadBalancerID)

		err := scope.OVHClient.DeleteLoadBalancer(scope.OVHCluster.Status.LoadBalancerID)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("deleting LB: %w", err)
		}
	}

	// Cleanup orphan LBs that share our cluster's name prefix. Defends against
	// duplicate LBs created by previous reconciles (e.g. before idempotency or
	// if the controller was killed between POST and status persist).
	lbPrefix := locutil.GenerateRFC1035Name("capi", scope.Cluster.Name, "lb")

	orphans, err := scope.OVHClient.ListLoadBalancersByPrefix(lbPrefix)
	if err != nil {
		logger.Error(err, "failed to list orphan LBs (continuing cleanup)")
	} else {
		for _, orphan := range orphans {
			logger.Info("Deleting orphan LB", "lbID", orphan.ID, "name", orphan.Name)

			if err := scope.OVHClient.DeleteLoadBalancer(orphan.ID); err != nil {
				logger.Error(err, "failed to delete orphan LB", "lbID", orphan.ID)
			}
		}
	}

	if r.cleanupFloatingIPs(scope, fipsToDelete) {
		return ctrl.Result{RequeueAfter: requeueTimeShort}, nil
	}

	// Delete the internet gateway we created (named capi-<cluster>-gw).
	// Discover via ListGateways since Status.GatewayID may not have persisted.
	gwName := "capi-" + scope.Cluster.Name + "-gw"

	if gws, err := scope.OVHClient.ListGateways(); err == nil {
		for i := range gws {
			if gws[i].Name == gwName {
				logger.Info("Deleting gateway", "gatewayID", gws[i].ID, "name", gwName)

				if err := scope.OVHClient.DeleteGateway(gws[i].ID); err != nil {
					logger.Error(err, "failed to delete gateway", "gatewayID", gws[i].ID)
				}

				break
			}
		}
	}

	// Delete network only if created by the controller
	if conditions.IsTrue(scope.OVHCluster, infrav1.NetworkCreatedByControllerCondition) {
		if scope.OVHCluster.Status.NetworkID != "" {
			logger.Info("Deleting private network (created by controller)", "networkID", scope.OVHCluster.Status.NetworkID)

			if err := scope.OVHClient.DeletePrivateNetwork(scope.OVHCluster.Status.NetworkID); err != nil {
				logger.Error(err, "failed to delete private network")

				// Network may still have a gateway attached; requeue for next try.
				return ctrl.Result{RequeueAfter: requeueTimeShort}, nil
			}
		}
	}

	logger.Info("OVH cluster infrastructure deleted successfully")

	// Remove finalizer
	controllerutil.RemoveFinalizer(scope.OVHCluster, infrav1.ClusterFinalizer)

	return ctrl.Result{}, nil
}

// cleanupFloatingIPs deletes the captured FIPs and returns true if at least
// one is still genuinely present (caller should requeue).
//
// Two OVH quirks handled here:
//  1. If the LB is in PENDING_DELETE, the first DeleteFloatingIP "succeeds"
//     (200 OK) but only DETACHES the FIP. A subsequent call deletes it once
//     the LB is fully gone.
//  2. After full detach (status: down, associatedEntity: nil),
//     DeleteFloatingIP returns 200 and OVH schedules an async removal.
//     Subsequent GET keeps returning the "down" resource for several
//     minutes. Treat that state as deleted from CAPI's POV — OVH reaps it
//     eventually and it no longer blocks other cleanup steps.
func (r *OVHClusterReconciler) cleanupFloatingIPs(scope *ClusterScope, fipsToDelete []string) bool {
	logger := scope.Logger
	requeue := false

	for _, fipID := range fipsToDelete {
		logger.Info("Deleting floating IP", "fipID", fipID)

		if err := scope.OVHClient.DeleteFloatingIP(fipID); err != nil {
			logger.Error(err, "failed to delete floating IP", "fipID", fipID)
		}

		fip, gerr := scope.OVHClient.GetFloatingIP(fipID)
		if gerr != nil || fip == nil {
			continue // gone for real
		}

		if fip.AssociatedEntity == nil && fip.Status == "down" {
			logger.Info("Floating IP detached and down; OVH will reap async, treating as deleted",
				"fipID", fipID)
			continue
		}

		logger.Info("Floating IP still present after delete; will retry",
			"fipID", fipID, "status", fip.Status)
		requeue = true
	}

	return requeue
}
