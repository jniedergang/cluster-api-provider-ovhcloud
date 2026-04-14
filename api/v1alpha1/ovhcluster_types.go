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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
)

const (
	// ClusterFinalizer allows ReconcileOVHCluster to clean up OVH resources before
	// removing the OVHCluster from the apiserver.
	ClusterFinalizer = "ovhcluster.infrastructure.cluster.x-k8s.io/finalizer"
)

const (
	// OVHConnectionReadyCondition documents the status of the OVH API connection.
	OVHConnectionReadyCondition clusterv1.ConditionType = "OVHConnectionReady"
	// OVHConnectionFailedReason documents that connection to OVH API failed.
	OVHConnectionFailedReason = "OVHConnectionFailed"
	// OVHAuthenticationFailedReason documents that authentication to OVH API failed.
	OVHAuthenticationFailedReason = "OVHAuthenticationFailed"
	// OVHConnectionReadyReason documents that OVH API connection is successful.
	OVHConnectionReadyReason = "OVHConnectionReady"

	// NetworkReadyCondition documents the status of the private network.
	NetworkReadyCondition clusterv1.ConditionType = "NetworkReady"
	// NetworkCreationFailedReason documents that the private network creation failed.
	NetworkCreationFailedReason = "NetworkCreationFailed"
	// NetworkReadyReason documents that the private network is ready.
	NetworkReadyReason = "NetworkReady"
	// NetworkCreatedByControllerCondition documents that the network was created by the controller (not pre-existing).
	NetworkCreatedByControllerCondition clusterv1.ConditionType = "NetworkCreatedByController"
	// NetworkCreatedByControllerReason documents the network was created by the controller.
	NetworkCreatedByControllerReason = "NetworkCreatedByController"

	// LoadBalancerReadyCondition documents the status of the OVH load balancer.
	LoadBalancerReadyCondition clusterv1.ConditionType = "LoadBalancerReady"
	// LoadBalancerNotReadyReason documents that the load balancer is not ready.
	LoadBalancerNotReadyReason = "LoadBalancerNotReady"
	// LoadBalancerCreationFailedReason documents that load balancer creation failed.
	LoadBalancerCreationFailedReason = "LoadBalancerCreationFailed"
	// LoadBalancerReadyReason documents that the load balancer is ready.
	LoadBalancerReadyReason = "LoadBalancerReady"

	// InfrastructureReadyCondition documents that all OVH infrastructure is provisioned.
	InfrastructureReadyCondition clusterv1.ConditionType = "InfrastructureReady"
	// InfrastructureProvisioningInProgressReason documents that infrastructure provisioning is in progress.
	InfrastructureProvisioningInProgressReason = "InfrastructureProvisioningInProgress"
	// InfrastructureProvisioningFailedReason documents that infrastructure provisioning has failed.
	InfrastructureProvisioningFailedReason = "InfrastructureProvisioningFailed"
	// InfrastructureReadyReason documents that all infrastructure is ready.
	InfrastructureReadyReason = "InfrastructureReady"

	// InitMachineCreatedCondition documents the status of the first control plane machine.
	InitMachineCreatedCondition clusterv1.ConditionType = "InitMachineCreated"
	// InitMachineNotYetCreatedReason documents that the first control plane machine is not yet created.
	InitMachineNotYetCreatedReason = "InitMachineNotYetCreated"
)

// SecretKey is a reference to a Secret containing OVH API credentials.
type SecretKey struct {
	// Namespace is the namespace of the Secret.
	Namespace string `json:"namespace"`

	// Name is the name of the Secret.
	Name string `json:"name"`
}

// OVHLoadBalancerConfig describes the OVH managed load balancer configuration.
type OVHLoadBalancerConfig struct {
	// SubnetID is the private subnet to attach the load balancer to.
	// If empty, the controller uses the subnet it created via NetworkConfig.
	// +optional
	SubnetID string `json:"subnetID,omitempty"`

	// FlavorName is the OVH Octavia LB flavor name (small, medium, large, xl).
	// Defaults to "small" if not specified.
	// +optional
	// +kubebuilder:default:=small
	FlavorName string `json:"flavorName,omitempty"`

	// FloatingNetworkID is the external network for the floating IP.
	// If set, a floating IP is created for external access to the control plane.
	// +optional
	FloatingNetworkID string `json:"floatingNetworkID,omitempty"`

	// Listeners is a list of additional listeners beyond the default api-server listener (6443).
	// +optional
	Listeners []OVHListener `json:"listeners,omitempty"`
}

// OVHListener describes an additional listener on the load balancer.
type OVHListener struct {
	// Name is the name of the listener.
	Name string `json:"name"`

	// Port is the port that the listener should listen on.
	Port int32 `json:"port"`

	// Protocol is the protocol for the listener.
	// +kubebuilder:validation:Enum:=TCP;HTTP
	Protocol string `json:"protocol"`

	// BackendPort is the port on the backend instances.
	BackendPort int32 `json:"backendPort"`
}

// OVHNetworkConfig describes the private network configuration for the cluster.
type OVHNetworkConfig struct {
	// PrivateNetworkID is the ID of an existing vRack private network.
	// If empty, a new private network is created automatically.
	// +optional
	PrivateNetworkID string `json:"privateNetworkID,omitempty"`

	// SubnetCIDR is the CIDR for the subnet (e.g. "10.0.0.0/24").
	// Required when creating a new network.
	SubnetCIDR string `json:"subnetCIDR"`

	// Gateway is the gateway IP address for the subnet.
	// +optional
	Gateway string `json:"gateway,omitempty"`

	// DNSServers is a list of DNS server IP addresses.
	// +optional
	DNSServers []string `json:"dnsServers,omitempty"`
}

// OVHClusterSpec defines the desired state of OVHCluster.
type OVHClusterSpec struct {
	// ServiceName is the OVH Public Cloud project ID.
	ServiceName string `json:"serviceName"`

	// Region is the OVH Cloud region (e.g. "GRA7", "SBG5", "BHS5").
	Region string `json:"region"`

	// IdentitySecret references the Secret containing OVH API credentials.
	// The Secret must contain keys: endpoint, applicationKey, applicationSecret, consumerKey.
	IdentitySecret SecretKey `json:"identitySecret"`

	// ControlPlaneEndpoint represents the endpoint used to communicate with the control plane.
	// +optional
	ControlPlaneEndpoint clusterv1.APIEndpoint `json:"controlPlaneEndpoint,omitempty"`

	// LoadBalancerConfig describes how the OVH managed load balancer should be configured.
	LoadBalancerConfig OVHLoadBalancerConfig `json:"loadBalancerConfig"`

	// NetworkConfig describes the private network configuration for the cluster.
	// +optional
	NetworkConfig *OVHNetworkConfig `json:"networkConfig,omitempty"`

	// SSHKeyName is the name of the SSH key registered in OVH to inject into instances.
	// +optional
	SSHKeyName string `json:"sshKeyName,omitempty"`
}

// OVHClusterStatus defines the observed state of OVHCluster.
type OVHClusterStatus struct {
	// Ready describes if the OVH cluster infrastructure is ready for machine creation.
	// +optional
	Ready bool `json:"ready,omitempty"`

	// FailureReason is the short name for the reason why a failure might be happening.
	// +optional
	FailureReason string `json:"failureReason,omitempty"`

	// FailureMessage is a full error message dump of the above failureReason.
	// +optional
	FailureMessage string `json:"failureMessage,omitempty"`

	// Conditions defines current service state of the OVH cluster.
	// +optional
	Conditions clusterv1.Conditions `json:"conditions,omitempty"`

	// NetworkID is the ID of the private network being used.
	// +optional
	NetworkID string `json:"networkID,omitempty"`

	// SubnetID is the ID of the subnet being used.
	// +optional
	SubnetID string `json:"subnetID,omitempty"`

	// LoadBalancerID is the OVH load balancer identifier.
	// +optional
	LoadBalancerID string `json:"loadBalancerID,omitempty"`

	// ListenerID is the ID of the API server listener on the load balancer.
	// +optional
	ListenerID string `json:"listenerID,omitempty"`

	// PoolID is the ID of the backend pool on the load balancer.
	// +optional
	PoolID string `json:"poolID,omitempty"`

	// FloatingIPID is the floating IP assigned to the load balancer.
	// +optional
	FloatingIPID string `json:"floatingIPID,omitempty"`

	// GatewayID is the internet gateway created on the private network when a
	// floating IP is allocated. The gateway has an interface on the subnet and,
	// once "exposed", provides SNAT outbound internet access for all instances
	// on the subnet.
	// +optional
	GatewayID string `json:"gatewayID,omitempty"`

	// GatewayExposed is set to true once the gateway has been attached to a
	// public port (via POST /gateway/{id}/expose) enabling SNAT outbound
	// connectivity for instances on the subnet.
	// +optional
	GatewayExposed bool `json:"gatewayExposed,omitempty"`

	// RegisterListenerID is the ID of the RKE2 supervisor (port 9345)
	// listener on the load balancer. Only populated for RKE2-based clusters.
	// +optional
	RegisterListenerID string `json:"registerListenerID,omitempty"`

	// RegisterPoolID is the ID of the backend pool for the RKE2 supervisor
	// port on the load balancer.
	// +optional
	RegisterPoolID string `json:"registerPoolID,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.ready",description="Cluster infrastructure is ready"
// +kubebuilder:printcolumn:name="Region",type="string",JSONPath=".spec.region",description="OVH region"
// +kubebuilder:printcolumn:name="Endpoint",type="string",JSONPath=".spec.controlPlaneEndpoint.host",description="API endpoint",priority=1

// OVHCluster is the Schema for the ovhclusters API.
type OVHCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   OVHClusterSpec   `json:"spec"`
	Status OVHClusterStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// OVHClusterList contains a list of OVHCluster.
type OVHClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []OVHCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&OVHCluster{}, &OVHClusterList{})
}

// GetConditions returns the set of conditions for this object.
func (c *OVHCluster) GetConditions() clusterv1.Conditions {
	return c.Status.Conditions
}

// SetConditions sets the conditions on this object.
func (c *OVHCluster) SetConditions(conditions clusterv1.Conditions) {
	c.Status.Conditions = conditions
}
