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
	if err := r.reconcileLBListener(scope); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.reconcileLBPool(scope); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.reconcileRKE2RegisterListener(scope); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.reconcileRKE2RegisterPool(scope); err != nil {
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

		return "", nil // keep retrying next reconcile
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

	scope.OVHCluster.Status.FloatingIPID = fip.ID
	logger.Info("Floating IP attached", "fipID", fip.ID, "ip", fip.IP, "lbID", lb.ID)

	// The allocate+attach call created an internet gateway on the private
	// subnet. For instances on that subnet to get SNAT outbound internet
	// (needed to download the RKE2 install script, container images, etc.),
	// the gateway must be explicitly exposed.
	if err := r.ensureGatewayExposed(scope); err != nil {
		logger.Info("Warning: failed to expose gateway for outbound internet, will retry",
			"error", err)
	}

	// The allocation response may not yet include the public IP address.
	// Fetch the floating IP directly to get the actual IP, retrying on next
	// reconcile if still empty.
	if fip.IP == "" {
		refreshed, gerr := scope.OVHClient.GetFloatingIP(fip.ID)
		if gerr == nil && refreshed != nil && refreshed.IP != "" {
			return refreshed.IP, nil
		}

		return "", nil // retry next reconcile
	}

	return fip.IP, nil
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

// reconcileLBListener ensures the API server listener exists on the LB.
// Idempotent: if a listener with the expected name already exists (e.g. from
// a previous reconcile where the status patch didn't persist), adopt it.
func (r *OVHClusterReconciler) reconcileLBListener(scope *ClusterScope) error {
	if scope.OVHCluster.Status.ListenerID != "" {
		return nil
	}

	logger := scope.Logger
	lbID := scope.OVHCluster.Status.LoadBalancerID

	existing, err := scope.OVHClient.FindListenerByName(lbID, apiServerListenerName)
	if err != nil {
		return fmt.Errorf("looking up api-server listener: %w", err)
	}

	if existing != nil {
		logger.Info("Adopting existing API server listener", "listenerID", existing.ID)
		scope.OVHCluster.Status.ListenerID = existing.ID

		return nil
	}

	logger.Info("Creating API server listener on LB")

	listener, err := scope.OVHClient.CreateListener(ovhclient.CreateListenerOpts{
		Name:           apiServerListenerName,
		Protocol:       apiServerProtocol,
		Port:           apiServerLBPort,
		LoadBalancerID: lbID,
	})
	if err != nil {
		return fmt.Errorf("creating listener: %w", err)
	}

	scope.OVHCluster.Status.ListenerID = listener.ID
	logger.Info("Listener created", "listenerID", listener.ID)

	return nil
}

// reconcileLBPool ensures the backend pool exists on the LB. Idempotent.
func (r *OVHClusterReconciler) reconcileLBPool(scope *ClusterScope) error {
	if scope.OVHCluster.Status.PoolID != "" {
		return nil
	}

	logger := scope.Logger
	lbID := scope.OVHCluster.Status.LoadBalancerID
	poolName := apiServerListenerName + "-pool"

	existing, err := scope.OVHClient.FindPoolByName(lbID, poolName)
	if err != nil {
		return fmt.Errorf("looking up api-server pool: %w", err)
	}

	if existing != nil {
		logger.Info("Adopting existing API server pool", "poolID", existing.ID)
		scope.OVHCluster.Status.PoolID = existing.ID

		return nil
	}

	logger.Info("Creating backend pool on LB")

	pool, err := scope.OVHClient.CreatePool(ovhclient.CreatePoolOpts{
		Name:           poolName,
		Protocol:       apiServerProtocol,
		Algorithm:      lbAlgorithm,
		ListenerID:     scope.OVHCluster.Status.ListenerID,
		LoadBalancerID: lbID,
	})
	if err != nil {
		return fmt.Errorf("creating pool: %w", err)
	}

	scope.OVHCluster.Status.PoolID = pool.ID
	logger.Info("Pool created", "poolID", pool.ID)

	return nil
}

// reconcileRKE2RegisterListener ensures the RKE2 supervisor (port 9345)
// listener exists on the LB so worker nodes can register with the cluster.
// Idempotent via FindListenerByName.
func (r *OVHClusterReconciler) reconcileRKE2RegisterListener(scope *ClusterScope) error {
	if scope.OVHCluster.Status.RegisterListenerID != "" {
		return nil
	}

	logger := scope.Logger
	lbID := scope.OVHCluster.Status.LoadBalancerID

	existing, err := scope.OVHClient.FindListenerByName(lbID, rke2RegisterListenerName)
	if err != nil {
		return fmt.Errorf("looking up rke2-register listener: %w", err)
	}

	if existing != nil {
		logger.Info("Adopting existing RKE2 supervisor listener", "listenerID", existing.ID)
		scope.OVHCluster.Status.RegisterListenerID = existing.ID

		return nil
	}

	logger.Info("Creating RKE2 supervisor (9345) listener on LB")

	listener, err := scope.OVHClient.CreateListener(ovhclient.CreateListenerOpts{
		Name:           rke2RegisterListenerName,
		Protocol:       apiServerProtocol,
		Port:           rke2RegisterLBPort,
		LoadBalancerID: lbID,
	})
	if err != nil {
		return fmt.Errorf("creating RKE2 supervisor listener: %w", err)
	}

	scope.OVHCluster.Status.RegisterListenerID = listener.ID
	logger.Info("RKE2 supervisor listener created", "listenerID", listener.ID)

	return nil
}

// reconcileRKE2RegisterPool ensures the backend pool for the RKE2 supervisor
// port exists on the LB. Idempotent.
func (r *OVHClusterReconciler) reconcileRKE2RegisterPool(scope *ClusterScope) error {
	if scope.OVHCluster.Status.RegisterPoolID != "" {
		return nil
	}

	logger := scope.Logger
	lbID := scope.OVHCluster.Status.LoadBalancerID
	poolName := rke2RegisterListenerName + "-pool"

	existing, err := scope.OVHClient.FindPoolByName(lbID, poolName)
	if err != nil {
		return fmt.Errorf("looking up rke2-register pool: %w", err)
	}

	if existing != nil {
		logger.Info("Adopting existing RKE2 supervisor pool", "poolID", existing.ID)
		scope.OVHCluster.Status.RegisterPoolID = existing.ID

		return nil
	}

	logger.Info("Creating RKE2 supervisor backend pool on LB")

	pool, err := scope.OVHClient.CreatePool(ovhclient.CreatePoolOpts{
		Name:           poolName,
		Protocol:       apiServerProtocol,
		Algorithm:      lbAlgorithm,
		ListenerID:     scope.OVHCluster.Status.RegisterListenerID,
		LoadBalancerID: lbID,
	})
	if err != nil {
		return fmt.Errorf("creating RKE2 supervisor pool: %w", err)
	}

	scope.OVHCluster.Status.RegisterPoolID = pool.ID
	logger.Info("RKE2 supervisor pool created", "poolID", pool.ID)

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

	// Delete floating IP if allocated
	if scope.OVHCluster.Status.FloatingIPID != "" {
		logger.Info("Deleting floating IP", "fipID", scope.OVHCluster.Status.FloatingIPID)

		err := scope.OVHClient.DeleteFloatingIP(scope.OVHCluster.Status.FloatingIPID)
		if err != nil {
			logger.Error(err, "failed to delete floating IP")
		}
	}

	// Delete network only if created by the controller
	if conditions.IsTrue(scope.OVHCluster, infrav1.NetworkCreatedByControllerCondition) {
		if scope.OVHCluster.Status.NetworkID != "" {
			logger.Info("Deleting private network (created by controller)", "networkID", scope.OVHCluster.Status.NetworkID)

			err := scope.OVHClient.DeletePrivateNetwork(scope.OVHCluster.Status.NetworkID)
			if err != nil {
				logger.Error(err, "failed to delete private network")
			}
		}
	}

	logger.Info("OVH cluster infrastructure deleted successfully")

	// Remove finalizer
	controllerutil.RemoveFinalizer(scope.OVHCluster, infrav1.ClusterFinalizer)

	return ctrl.Result{}, nil
}
