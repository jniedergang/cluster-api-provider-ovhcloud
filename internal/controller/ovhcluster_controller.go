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

	infrav1 "gitea.home.zypp.fr/jniedergang/cluster-api-provider-ovhcloud/api/v1alpha1"
	capiovhmetrics "gitea.home.zypp.fr/jniedergang/cluster-api-provider-ovhcloud/internal/metrics"
	ovhclient "gitea.home.zypp.fr/jniedergang/cluster-api-provider-ovhcloud/pkg/ovh"
	locutil "gitea.home.zypp.fr/jniedergang/cluster-api-provider-ovhcloud/util"
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
		if patchErr := patchHelper.Patch(ctx, ovhCluster); patchErr != nil {
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
	if err := mgr.GetFieldIndexer().IndexField(ctx, &infrav1.OVHCluster{},
		".spec.identitySecret.name",
		func(obj client.Object) []string {
			cluster := obj.(*infrav1.OVHCluster)
			if cluster.Spec.IdentitySecret.Name == "" {
				return nil
			}

			return []string{cluster.Spec.IdentitySecret.Name}
		},
	); err != nil {
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
	if err := r.List(ctx, clusterList, client.MatchingFields{
		".spec.identitySecret.name": secret.Name,
	}); err != nil {
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
		if err == errNetworkNotReady {
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
	if err := scope.OVHClient.ValidateCredentials(); err != nil {
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
var errNetworkNotReady = fmt.Errorf("network not yet ACTIVE in region")

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

	lb, err := scope.OVHClient.CreateLoadBalancer(opts)
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

	// LB is ACTIVE — ensure listener and pool exist
	if err := r.reconcileLBListener(scope); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.reconcileLBPool(scope); err != nil {
		return ctrl.Result{}, err
	}

	// Set control plane endpoint from VIP
	if lb.VIPAddress != "" && scope.OVHCluster.Spec.ControlPlaneEndpoint.Host == "" {
		scope.OVHCluster.Spec.ControlPlaneEndpoint = clusterv1.APIEndpoint{
			Host: lb.VIPAddress,
			Port: apiServerLBPort,
		}
		logger.Info("Control plane endpoint set", "host", lb.VIPAddress, "port", apiServerLBPort)
	}

	conditions.MarkTrue(scope.OVHCluster, infrav1.LoadBalancerReadyCondition)

	return ctrl.Result{}, nil
}

// reconcileLBListener ensures the API server listener exists on the LB.
func (r *OVHClusterReconciler) reconcileLBListener(scope *ClusterScope) error {
	if scope.OVHCluster.Status.ListenerID != "" {
		return nil // Already created
	}

	logger := scope.Logger
	logger.Info("Creating API server listener on LB")

	listener, err := scope.OVHClient.CreateListener(ovhclient.CreateListenerOpts{
		Name:           apiServerListenerName,
		Protocol:       apiServerProtocol,
		Port:           apiServerLBPort,
		LoadBalancerID: scope.OVHCluster.Status.LoadBalancerID,
	})
	if err != nil {
		return fmt.Errorf("creating listener: %w", err)
	}

	scope.OVHCluster.Status.ListenerID = listener.ID
	logger.Info("Listener created", "listenerID", listener.ID)

	return nil
}

// reconcileLBPool ensures the backend pool exists on the LB.
func (r *OVHClusterReconciler) reconcileLBPool(scope *ClusterScope) error {
	if scope.OVHCluster.Status.PoolID != "" {
		return nil // Already created
	}

	logger := scope.Logger
	logger.Info("Creating backend pool on LB")

	pool, err := scope.OVHClient.CreatePool(ovhclient.CreatePoolOpts{
		Name:           apiServerListenerName + "-pool",
		Protocol:       apiServerProtocol,
		Algorithm:      lbAlgorithm,
		ListenerID:     scope.OVHCluster.Status.ListenerID,
		LoadBalancerID: scope.OVHCluster.Status.LoadBalancerID,
	})
	if err != nil {
		return fmt.Errorf("creating pool: %w", err)
	}

	scope.OVHCluster.Status.PoolID = pool.ID
	logger.Info("Pool created", "poolID", pool.ID)

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
			if err := scope.OVHClient.DeletePool(scope.OVHCluster.Status.PoolID); err != nil {
				logger.Error(err, "failed to delete pool", "poolID", scope.OVHCluster.Status.PoolID)
			}
		}

		if scope.OVHCluster.Status.ListenerID != "" {
			if err := scope.OVHClient.DeleteListener(scope.OVHCluster.Status.ListenerID); err != nil {
				logger.Error(err, "failed to delete listener", "listenerID", scope.OVHCluster.Status.ListenerID)
			}
		}

		logger.Info("Deleting load balancer", "lbID", scope.OVHCluster.Status.LoadBalancerID)

		if err := scope.OVHClient.DeleteLoadBalancer(scope.OVHCluster.Status.LoadBalancerID); err != nil {
			return ctrl.Result{}, fmt.Errorf("deleting LB: %w", err)
		}
	}

	// Delete floating IP if allocated
	if scope.OVHCluster.Status.FloatingIPID != "" {
		logger.Info("Deleting floating IP", "fipID", scope.OVHCluster.Status.FloatingIPID)

		if err := scope.OVHClient.DeleteFloatingIP(scope.OVHCluster.Status.FloatingIPID); err != nil {
			logger.Error(err, "failed to delete floating IP")
		}
	}

	// Delete network only if created by the controller
	if conditions.IsTrue(scope.OVHCluster, infrav1.NetworkCreatedByControllerCondition) {
		if scope.OVHCluster.Status.NetworkID != "" {
			logger.Info("Deleting private network (created by controller)", "networkID", scope.OVHCluster.Status.NetworkID)

			if err := scope.OVHClient.DeletePrivateNetwork(scope.OVHCluster.Status.NetworkID); err != nil {
				logger.Error(err, "failed to delete private network")
			}
		}
	}

	logger.Info("OVH cluster infrastructure deleted successfully")

	// Remove finalizer
	controllerutil.RemoveFinalizer(scope.OVHCluster, infrav1.ClusterFinalizer)

	return ctrl.Result{}, nil
}
