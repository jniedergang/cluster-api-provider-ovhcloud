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
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/annotations"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/cluster-api/util/kubeconfig"
	"sigs.k8s.io/cluster-api/util/patch"
	"sigs.k8s.io/cluster-api/util/predicates"

	infrav1 "github.com/rancher-sandbox/cluster-api-provider-ovhcloud/api/v1alpha2"
	capiovhmetrics "github.com/rancher-sandbox/cluster-api-provider-ovhcloud/internal/metrics"
	ovhclient "github.com/rancher-sandbox/cluster-api-provider-ovhcloud/pkg/ovh"
	locutil "github.com/rancher-sandbox/cluster-api-provider-ovhcloud/util"
)

const (
	requeueDelay     = 10 * time.Second
	requeueDelayLong = 30 * time.Second
	apiServerPort    = 6443
)

var (
	machineInitializationProvisioned = infrav1.Initialization{
		Provisioned: true,
	}

	machineInitializationNotProvisioned = infrav1.Initialization{
		Provisioned: false,
	}
)

// MachineScope stores context data for the OVHMachine reconciler.
type MachineScope struct {
	Ctx        context.Context
	Cluster    *clusterv1.Cluster
	Machine    *clusterv1.Machine
	OVHCluster *infrav1.OVHCluster
	OVHMachine *infrav1.OVHMachine
	OVHClient  *ovhclient.Client
	Logger     *logr.Logger
}

// OVHMachineReconciler reconciles an OVHMachine object.
type OVHMachineReconciler struct {
	client.Client

	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=ovhmachines,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=ovhmachines/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=ovhmachines/finalizers,verbs=update
//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=ovhclusters,verbs=get;list
//+kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters;machines,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile handles OVHMachine reconciliation.
func (r *OVHMachineReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, rerr error) {
	logger := log.FromContext(ctx)
	ctx = ctrl.LoggerInto(ctx, logger)

	logger.Info("Reconciling OVHMachine ...")

	ovhMachine := &infrav1.OVHMachine{}

	err := r.Get(ctx, req.NamespacedName, ovhMachine)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("OVHMachine not found")

			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, err
	}

	// Initialize the patch helper
	patchHelper, err := patch.NewHelper(ovhMachine, r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Always attempt to Patch the OVHMachine object and status after each reconciliation.
	defer func() {
		patchErr := patchHelper.Patch(ctx, ovhMachine)
		if patchErr != nil {
			logger.Error(patchErr, "failed to patch OVHMachine")

			if rerr == nil {
				rerr = patchErr
			}
		}
	}()

	// Get owner Machine
	ownerMachine, err := util.GetOwnerMachine(ctx, r.Client, ovhMachine.ObjectMeta)
	if err != nil {
		return ctrl.Result{}, err
	}

	if ownerMachine == nil {
		logger.Info("Waiting for Machine Controller to set OwnerRef on OVHMachine")

		return ctrl.Result{RequeueAfter: requeueDelay}, nil
	}

	// Get owner Cluster
	ownerCluster, err := util.GetClusterFromMetadata(ctx, r.Client, ownerMachine.ObjectMeta)
	if err != nil {
		return ctrl.Result{}, err
	}

	if ownerCluster == nil {
		logger.Info("Please associate this machine with a cluster")

		return ctrl.Result{}, nil
	}

	logger = logger.WithValues(
		"machine", ownerMachine.Namespace+"/"+ownerMachine.Name,
		"cluster", ownerCluster.Namespace+"/"+ownerCluster.Name,
	)
	ctx = ctrl.LoggerInto(ctx, logger)

	// Get OVHCluster
	ovhCluster := &infrav1.OVHCluster{}
	ovhClusterKey := types.NamespacedName{
		Namespace: ownerCluster.Spec.InfrastructureRef.Namespace,
		Name:      ownerCluster.Spec.InfrastructureRef.Name,
	}

	if err := r.Get(ctx, ovhClusterKey, ovhCluster); err != nil {
		logger.Error(err, "unable to find corresponding OVHCluster")

		return ctrl.Result{}, err
	}

	// Create OVH API client
	ovhClient, err := locutil.GetOVHClientFromCluster(ctx, r.Client, ovhCluster, logger)
	if err != nil {
		logger.Error(err, "unable to create OVH client")

		return ctrl.Result{RequeueAfter: requeueDelay}, nil
	}

	scope := &MachineScope{
		Ctx:        ctx,
		Cluster:    ownerCluster,
		Machine:    ownerMachine,
		OVHCluster: ovhCluster,
		OVHMachine: ovhMachine,
		OVHClient:  ovhClient,
		Logger:     &logger,
	}

	if !ovhMachine.DeletionTimestamp.IsZero() {
		return r.ReconcileDelete(scope)
	}

	return r.ReconcileNormal(scope)
}

// SetupWithManager sets up the controller with the Manager.
func (r *OVHMachineReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	clusterToOVHMachine, err := util.ClusterToTypedObjectsMapper(
		mgr.GetClient(), &infrav1.OVHMachineList{}, mgr.GetScheme(),
	)
	if err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&infrav1.OVHMachine{}).
		Watches(
			&clusterv1.Machine{},
			handler.EnqueueRequestsFromMapFunc(
				util.MachineToInfrastructureMapFunc(infrav1.GroupVersion.WithKind("OVHMachine")),
			),
			builder.WithPredicates(predicates.ResourceNotPaused(mgr.GetScheme(), ctrl.LoggerFrom(ctx))),
		).
		Watches(
			&clusterv1.Cluster{},
			handler.EnqueueRequestsFromMapFunc(clusterToOVHMachine),
			builder.WithPredicates(predicates.ClusterUnpaused(mgr.GetScheme(), ctrl.LoggerFrom(ctx))),
		).
		Complete(r)
}

// ReconcileNormal handles create/update of OVH instances.
func (r *OVHMachineReconciler) ReconcileNormal(scope *MachineScope) (reconcile.Result, error) {
	reconcileStart := time.Now()

	defer func() {
		capiovhmetrics.MachineReconcileDuration.WithLabelValues("normal").Observe(
			time.Since(reconcileStart).Seconds(),
		)

		clusterName := scope.Cluster.Namespace + "/" + scope.Cluster.Name
		machineName := scope.OVHMachine.Namespace + "/" + scope.OVHMachine.Name

		if scope.OVHMachine.Status.Ready {
			capiovhmetrics.MachineStatus.WithLabelValues(clusterName, machineName).Set(1)
		} else {
			capiovhmetrics.MachineStatus.WithLabelValues(clusterName, machineName).Set(0)
		}
	}()

	logger := log.FromContext(scope.Ctx)

	// Return early if paused
	if annotations.IsPaused(scope.Cluster, scope.OVHMachine) {
		logger.Info("Reconciliation is paused for this object")

		scope.OVHMachine.Status.Ready = false
		scope.OVHMachine.Status.Initialization = machineInitializationNotProvisioned

		return ctrl.Result{}, nil
	}

	// Add finalizer if missing
	if !controllerutil.ContainsFinalizer(scope.OVHMachine, infrav1.MachineFinalizer) &&
		scope.OVHMachine.DeletionTimestamp.IsZero() {
		controllerutil.AddFinalizer(scope.OVHMachine, infrav1.MachineFinalizer)
		scope.OVHMachine.Status.Ready = false
		scope.OVHMachine.Status.Initialization = machineInitializationNotProvisioned

		return ctrl.Result{}, nil
	}

	// Wait for cluster infrastructure to be ready
	if !scope.Cluster.Status.InfrastructureReady {
		logger.Info("Waiting for cluster infrastructure to be ready ...")
		conditions.MarkFalse(scope.OVHMachine, infrav1.InstanceProvisioningReadyCondition,
			infrav1.InfrastructureProvisioningInProgressReason, clusterv1.ConditionSeverityInfo,
			"Waiting for cluster infrastructure")

		return ctrl.Result{RequeueAfter: requeueDelay}, nil
	}

	// Wait for bootstrap data
	if scope.Machine.Spec.Bootstrap.DataSecretName == nil {
		logger.Info("Waiting for bootstrap data to be available ...")

		return ctrl.Result{RequeueAfter: requeueDelay}, nil
	}

	// Check if instance already exists (by stored ID or by name)
	var instance *ovhclient.Instance

	if scope.OVHMachine.Status.InstanceID != "" {
		// We already know the instance ID
		inst, err := scope.OVHClient.GetInstance(scope.OVHMachine.Status.InstanceID)
		if err != nil {
			if ovhclient.IsNotFound(err) {
				logger.Info("Previously known instance not found, will create new one",
					"instanceID", scope.OVHMachine.Status.InstanceID)
				scope.OVHMachine.Status.InstanceID = ""
			} else {
				return ctrl.Result{}, fmt.Errorf("getting instance %s: %w", scope.OVHMachine.Status.InstanceID, err)
			}
		} else {
			instance = inst
		}
	}

	if instance == nil {
		// Try to find by name
		instanceName := locutil.GenerateRFC1035Name(scope.Cluster.Name, scope.OVHMachine.Name)

		inst, err := scope.OVHClient.FindInstanceByName(instanceName)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("finding instance by name %s: %w", instanceName, err)
		}

		instance = inst
	}

	// Instance exists — handle its current state
	if instance != nil {
		return r.handleExistingInstance(scope, instance)
	}

	// Instance does not exist — create it
	return r.createInstance(scope)
}

// handleExistingInstance handles an instance that already exists in OVH.
func (r *OVHMachineReconciler) handleExistingInstance(scope *MachineScope, instance *ovhclient.Instance) (reconcile.Result, error) {
	logger := log.FromContext(scope.Ctx)

	scope.OVHMachine.Status.InstanceID = instance.ID
	conditions.MarkTrue(scope.OVHMachine, infrav1.InstanceCreatedCondition)

	switch instance.Status {
	case ovhclient.InstanceStatusActive:
		logger.Info("Instance is ACTIVE", "instanceID", instance.ID)

		// Observe BUILD->ACTIVE duration once per machine, on the transition to
		// Ready. `!Status.Ready` ensures we only observe on the first ACTIVE
		// reconcile, not every subsequent refresh.
		if !scope.OVHMachine.Status.Ready && instance.Created != "" {
			if createdAt, parseErr := time.Parse(time.RFC3339, instance.Created); parseErr == nil {
				capiovhmetrics.BootstrapWaitDuration.Observe(time.Since(createdAt).Seconds())
			}
		}

		// Set addresses from instance IPs
		addresses := make([]clusterv1.MachineAddress, 0, len(instance.IPAddresses))

		for _, ip := range instance.IPAddresses {
			if ip.Version != 4 {
				continue // Skip IPv6 for now
			}

			addrType := clusterv1.MachineInternalIP
			if ip.Type == "public" {
				addrType = clusterv1.MachineExternalIP
			}

			addresses = append(addresses, clusterv1.MachineAddress{
				Type:    addrType,
				Address: ip.IP,
			})
		}

		scope.OVHMachine.Status.Addresses = addresses

		// Set ProviderID
		scope.OVHMachine.Spec.ProviderID = locutil.ProviderIDFromInstance(
			scope.OVHCluster.Spec.Region, instance.ID,
		)

		// Mark as ready
		scope.OVHMachine.Status.Ready = true
		scope.OVHMachine.Status.Initialization = machineInitializationProvisioned

		conditions.MarkTrue(scope.OVHMachine, infrav1.InstanceProvisioningReadyCondition)
		conditions.MarkTrue(scope.OVHMachine, infrav1.InstanceRunningCondition)

		// Register CP machines as backend members of the API server LB pool.
		// Worker nodes are not added to the api-server pool. Best-effort: if
		// the LB or pool isn't ready yet, we'll retry on the next reconcile.
		if err := r.ensureLBPoolMember(scope, instance); err != nil {
			logger.Info("Warning: failed to register CP node in LB pool, will retry",
				"error", err)
		}

		// Patch the providerID on the workload cluster node so CAPI can link
		// Machine -> Node. RKE2 registers nodes with "rke2://<hostname>" by
		// default; without this patch CAPI MachineDeployments stay stuck in
		// ScalingUp/Unavailable even though nodes are Ready.
		// Best-effort: no-op if the workload kubeconfig isn't available yet
		// or if the node hasn't joined — will retry on next reconcile.
		if cfg, cfgErr := r.getWorkloadRESTConfig(scope); cfgErr == nil && cfg != nil {
			locutil.InitializeWorkloadNode(scope.Ctx, logger, cfg, instance.Name,
				scope.OVHMachine.Spec.ProviderID)
		} else if cfgErr != nil {
			logger.V(1).Info("Workload kubeconfig not yet available for node init", "error", cfgErr)
		}

		return ctrl.Result{}, nil

	case ovhclient.InstanceStatusBuild:
		logger.Info("Instance is still building ...", "instanceID", instance.ID)

		conditions.MarkFalse(scope.OVHMachine, infrav1.InstanceProvisioningReadyCondition,
			infrav1.InstanceProvisioningInProgressReason, clusterv1.ConditionSeverityInfo,
			"Instance is being provisioned (BUILD state)")
		conditions.MarkFalse(scope.OVHMachine, infrav1.InstanceRunningCondition,
			infrav1.InstanceNotRunningReason, clusterv1.ConditionSeverityInfo,
			"Instance is not yet running")

		scope.OVHMachine.Status.Ready = false

		return ctrl.Result{RequeueAfter: requeueDelayLong}, nil

	case ovhclient.InstanceStatusError:
		logger.Error(nil, "Instance is in ERROR state", "instanceID", instance.ID)

		conditions.MarkFalse(scope.OVHMachine, infrav1.InstanceProvisioningReadyCondition,
			infrav1.InstanceProvisioningFailedReason, clusterv1.ConditionSeverityError,
			"Instance provisioning failed (ERROR state)")

		scope.OVHMachine.Status.Ready = false
		scope.OVHMachine.Status.FailureReason = "InstanceError"
		scope.OVHMachine.Status.FailureMessage = fmt.Sprintf("OVH instance %s is in ERROR state", instance.ID)

		capiovhmetrics.MachineCreateErrorsTotal.Inc()

		return ctrl.Result{}, nil

	default:
		// REBOOT, STOPPED, etc.
		logger.Info("Instance in transitional state", "instanceID", instance.ID, "status", instance.Status)

		scope.OVHMachine.Status.Ready = false

		return ctrl.Result{RequeueAfter: requeueDelayLong}, nil
	}
}

// createInstance creates a new OVH instance.
func (r *OVHMachineReconciler) createInstance(scope *MachineScope) (reconcile.Result, error) {
	logger := log.FromContext(scope.Ctx)

	// Resolve flavor
	flavor, err := scope.OVHClient.GetFlavorByName(scope.OVHMachine.Spec.FlavorName)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("resolving flavor %q: %w", scope.OVHMachine.Spec.FlavorName, err)
	}

	logger.Info("Resolved flavor", "flavor", flavor.Name, "flavorID", flavor.ID,
		"vcpus", flavor.VCPUs, "ram", flavor.RAM, "disk", flavor.Disk)

	// Resolve image
	image, err := scope.OVHClient.GetImageByName(scope.OVHMachine.Spec.ImageName)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("resolving image %q: %w", scope.OVHMachine.Spec.ImageName, err)
	}

	logger.Info("Resolved image", "image", image.Name, "imageID", image.ID)

	// Get bootstrap data
	bootstrapData, err := r.getBootstrapData(scope)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("getting bootstrap data: %w", err)
	}

	userData, err := locutil.PrepareUserData(bootstrapData)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("preparing user data: %w", err)
	}

	// Resolve SSH key (optional)
	var sshKeyID string

	sshKeyName := locutil.GetSSHKeyName(scope.OVHMachine.Spec.SSHKeyName, scope.OVHCluster.Spec.SSHKeyName)
	if sshKeyName != "" {
		sshKey, err := scope.OVHClient.GetSSHKeyByName(sshKeyName)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("resolving SSH key %q: %w", sshKeyName, err)
		}

		sshKeyID = sshKey.ID
		logger.Info("Resolved SSH key", "sshKey", sshKeyName, "sshKeyID", sshKeyID)
	}

	// Build network config
	var networks []ovhclient.InstanceNetwork
	if scope.OVHCluster.Status.NetworkID != "" {
		networks = append(networks, ovhclient.InstanceNetwork{
			NetworkID: scope.OVHCluster.Status.NetworkID,
		})
	}

	// Create instance
	instanceName := locutil.GenerateRFC1035Name(scope.Cluster.Name, scope.OVHMachine.Name)

	capiovhmetrics.MachineCreateTotal.Inc()

	logger.Info("Creating OVH instance",
		"name", instanceName,
		"flavor", flavor.Name,
		"image", image.Name,
		"region", scope.OVHCluster.Spec.Region,
	)

	instance, err := scope.OVHClient.CreateInstance(ovhclient.CreateInstanceOpts{
		Name:     instanceName,
		FlavorID: flavor.ID,
		ImageID:  image.ID,
		Region:   scope.OVHCluster.Spec.Region,
		SSHKeyID: sshKeyID,
		UserData: userData,
		Networks: networks,
	})
	if err != nil {
		capiovhmetrics.MachineCreateErrorsTotal.Inc()

		conditions.MarkFalse(scope.OVHMachine, infrav1.InstanceCreatedCondition,
			infrav1.InstanceProvisioningFailedReason, clusterv1.ConditionSeverityError,
			"Failed to create instance: %s", err.Error())

		return ctrl.Result{}, fmt.Errorf("creating OVH instance: %w", err)
	}

	logger.Info("Instance created successfully", "instanceID", instance.ID, "status", instance.Status)

	// Store instance ID in status
	scope.OVHMachine.Status.InstanceID = instance.ID
	conditions.MarkTrue(scope.OVHMachine, infrav1.InstanceCreatedCondition)
	conditions.MarkFalse(scope.OVHMachine, infrav1.InstanceProvisioningReadyCondition,
		infrav1.InstanceProvisioningInProgressReason, clusterv1.ConditionSeverityInfo,
		"Instance created, waiting for ACTIVE state")

	// Requeue to check for ACTIVE state
	return ctrl.Result{RequeueAfter: requeueDelayLong}, nil
}

// getBootstrapData reads the bootstrap data secret referenced by the Machine.
func (r *OVHMachineReconciler) getBootstrapData(scope *MachineScope) ([]byte, error) {
	if scope.Machine.Spec.Bootstrap.DataSecretName == nil {
		return nil, errors.New("bootstrap data secret name is nil")
	}

	secret := &corev1.Secret{}

	key := types.NamespacedName{
		Namespace: scope.Machine.Namespace,
		Name:      *scope.Machine.Spec.Bootstrap.DataSecretName,
	}

	err := r.Get(scope.Ctx, key, secret)
	if err != nil {
		return nil, fmt.Errorf("getting bootstrap secret %s: %w", key, err)
	}

	data, ok := secret.Data["value"]
	if !ok {
		return nil, fmt.Errorf("bootstrap secret %s missing 'value' key", key)
	}

	return data, nil
}

// getWorkloadRESTConfig returns a rest.Config for the workload cluster
// associated with the given MachineScope, by reading the CAPI-generated
// kubeconfig Secret from the management cluster. Returns (nil, nil) if the
// secret does not yet exist.
func (r *OVHMachineReconciler) getWorkloadRESTConfig(scope *MachineScope) (*rest.Config, error) {
	if r.Client == nil || scope.Cluster == nil {
		return nil, nil
	}

	data, err := kubeconfig.FromSecret(scope.Ctx, r.Client, client.ObjectKey{
		Namespace: scope.Cluster.Namespace,
		Name:      scope.Cluster.Name,
	})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}

		return nil, fmt.Errorf("reading kubeconfig secret: %w", err)
	}

	cfg, err := clientcmd.RESTConfigFromKubeConfig(data)
	if err != nil {
		return nil, fmt.Errorf("parsing kubeconfig: %w", err)
	}

	return cfg, nil
}

// ensureLBPoolMember registers a CP machine's primary private IP as a member
// of the parent OVHCluster's API server LB pool. Idempotent: skips if a
// member ID is already recorded and still present in OVH.
//
// Workers and machines without a ready cluster LB pool are no-ops.
func (r *OVHMachineReconciler) ensureLBPoolMember(scope *MachineScope, instance *ovhclient.Instance) error {
	if scope.Machine == nil || !util.IsControlPlaneMachine(scope.Machine) {
		return nil
	}

	poolID := scope.OVHCluster.Status.PoolID
	if poolID == "" {
		return errors.New("OVHCluster.Status.PoolID not set yet")
	}

	// Find the primary IPv4 private address.
	var privateIP string

	for _, ip := range instance.IPAddresses {
		if ip.Version == 4 && ip.Type == "private" {
			privateIP = ip.IP

			break
		}
	}

	if privateIP == "" {
		return fmt.Errorf("no IPv4 private address on instance %s", instance.ID)
	}

	// Idempotent check: skip add if a member at this address:6443 already
	// exists in the pool (covers status patches dropped by apiserver, pod
	// restarts mid-reconcile, etc.).
	alreadyRegistered := false

	if members, err := scope.OVHClient.ListPoolMembers(poolID); err == nil {
		for i := range members {
			if members[i].Address == privateIP && members[i].ProtocolPort == 6443 {
				scope.OVHMachine.Status.LBPoolMemberID = members[i].ID
				alreadyRegistered = true

				break
			}
		}
	}

	if !alreadyRegistered {
		member, err := scope.OVHClient.AddPoolMember(poolID, ovhclient.CreateMemberOpts{
			Name:         scope.Machine.Name,
			Address:      privateIP,
			ProtocolPort: 6443,
			Weight:       1,
		})
		if err != nil {
			return fmt.Errorf("adding pool member: %w", err)
		}

		scope.OVHMachine.Status.LBPoolMemberID = member.ID
		(*scope.Logger).Info("Registered CP node as LB pool member",
			"poolID", poolID, "memberID", member.ID, "address", privateIP)
	}

	// Also register in the RKE2 supervisor (9345) pool so worker agents can
	// reach the CP for CA fetch + registration. Discover the pool dynamically
	// via FindPoolByName rather than relying on Status.RegisterPoolID, which
	// may be empty if a previous status patch was dropped by the apiserver
	// (e.g. openAPI schema cache lag after a CRD update).
	r.ensureRKE2RegisterPoolMember(scope, privateIP)

	return nil
}

// ensureRKE2RegisterPoolMember looks up the RKE2 supervisor pool by name on
// the OVHCluster's LB and registers this CP machine as a member on port 9345.
// Idempotent: skips if the pool already contains a member at the same address.
// Best-effort: failures are logged, not returned.
func (r *OVHMachineReconciler) ensureRKE2RegisterPoolMember(scope *MachineScope, privateIP string) {
	lbID := scope.OVHCluster.Status.LoadBalancerID
	if lbID == "" {
		return
	}

	pool, err := scope.OVHClient.FindPoolByName(lbID, "rke2-register-pool")
	if err != nil {
		(*scope.Logger).Info("Warning: failed to look up RKE2 supervisor pool",
			"error", err)

		return
	}

	if pool == nil {
		return // pool not yet created; next reconcile will retry
	}

	// Skip if already registered by address.
	members, err := scope.OVHClient.ListPoolMembers(pool.ID)
	if err == nil {
		for i := range members {
			if members[i].Address == privateIP && members[i].ProtocolPort == 9345 {
				scope.OVHMachine.Status.RegisterPoolMemberID = members[i].ID

				return
			}
		}
	}

	regMember, err := scope.OVHClient.AddPoolMember(pool.ID, ovhclient.CreateMemberOpts{
		Name:         scope.Machine.Name,
		Address:      privateIP,
		ProtocolPort: 9345,
		Weight:       1,
	})
	if err != nil {
		(*scope.Logger).Info("Warning: failed to register CP in RKE2 supervisor pool",
			"error", err)

		return
	}

	scope.OVHMachine.Status.RegisterPoolMemberID = regMember.ID
	(*scope.Logger).Info("Registered CP node as RKE2 supervisor pool member",
		"poolID", pool.ID, "memberID", regMember.ID, "address", privateIP)
}

// removeLBPoolMember removes the CP machine from the LB pool, best-effort.
func (r *OVHMachineReconciler) removeLBPoolMember(scope *MachineScope) {
	if scope.OVHMachine.Status.LBPoolMemberID == "" {
		return
	}

	poolID := scope.OVHCluster.Status.PoolID
	if poolID == "" {
		return // pool already deleted by OVHCluster ReconcileDelete
	}

	if err := scope.OVHClient.RemovePoolMember(poolID, scope.OVHMachine.Status.LBPoolMemberID); err != nil {
		(*scope.Logger).Info("Warning: failed to remove LB pool member",
			"poolID", poolID,
			"memberID", scope.OVHMachine.Status.LBPoolMemberID,
			"error", err)

		return
	}

	scope.OVHMachine.Status.LBPoolMemberID = ""
}

// ReconcileDelete handles deletion of OVH instances.
func (r *OVHMachineReconciler) ReconcileDelete(scope *MachineScope) (reconcile.Result, error) {
	reconcileStart := time.Now()

	defer func() {
		capiovhmetrics.MachineReconcileDuration.WithLabelValues("delete").Observe(
			time.Since(reconcileStart).Seconds(),
		)
	}()

	logger := log.FromContext(scope.Ctx)
	logger.Info("Reconciling OVHMachine deletion ...")

	capiovhmetrics.MachineDeleteTotal.Inc()

	// Delete additional volumes first
	for _, volID := range scope.OVHMachine.Status.VolumeIDs {
		logger.Info("Detaching and deleting volume", "volumeID", volID)

		if scope.OVHMachine.Status.InstanceID != "" {
			err := scope.OVHClient.DetachVolume(volID, scope.OVHMachine.Status.InstanceID)
			if err != nil {
				logger.Error(err, "failed to detach volume", "volumeID", volID)
			}
		}

		err := scope.OVHClient.DeleteVolume(volID)
		if err != nil {
			logger.Error(err, "failed to delete volume", "volumeID", volID)
		}
	}

	// Remove from LB pool first (best-effort, no-op for workers).
	r.removeLBPoolMember(scope)

	// Delete instance
	if scope.OVHMachine.Status.InstanceID != "" {
		logger.Info("Deleting OVH instance", "instanceID", scope.OVHMachine.Status.InstanceID)

		if err := scope.OVHClient.DeleteInstance(scope.OVHMachine.Status.InstanceID); err != nil {
			capiovhmetrics.MachineDeleteErrorsTotal.Inc()

			return ctrl.Result{}, fmt.Errorf("deleting instance %s: %w", scope.OVHMachine.Status.InstanceID, err)
		}

		// Verify instance is gone
		inst, err := scope.OVHClient.GetInstance(scope.OVHMachine.Status.InstanceID)
		if err == nil && inst != nil && inst.Status != ovhclient.InstanceStatusDeleted {
			logger.Info("Instance still terminating, requeueing ...", "status", inst.Status)

			return ctrl.Result{RequeueAfter: requeueDelay}, nil
		}
	}

	logger.Info("OVH instance deleted successfully")

	// Remove finalizer
	controllerutil.RemoveFinalizer(scope.OVHMachine, infrav1.MachineFinalizer)

	return ctrl.Result{}, nil
}
