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
	// MachineFinalizer allows ReconcileOVHMachine to clean up OVH resources before
	// removing the OVHMachine from the apiserver.
	MachineFinalizer = "ovhmachine.infrastructure.cluster.x-k8s.io/finalizer"
)

const (
	// InstanceCreatedCondition documents that the OVH instance has been created.
	InstanceCreatedCondition clusterv1.ConditionType = "InstanceCreated"
	// InstanceNotFoundReason documents that the instance was not found.
	InstanceNotFoundReason = "InstanceNotFound"

	// InstanceProvisioningReadyCondition documents instance creation and provisioning status.
	InstanceProvisioningReadyCondition clusterv1.ConditionType = "InstanceProvisioningReady"
	// InstanceProvisioningInProgressReason documents that instance provisioning is in progress (BUILD state).
	InstanceProvisioningInProgressReason = "InstanceProvisioningInProgress"
	// InstanceProvisioningFailedReason documents that instance provisioning has failed (ERROR state).
	InstanceProvisioningFailedReason = "InstanceProvisioningFailed"
	// InstanceProvisioningReadyReason documents that instance provisioning is complete (ACTIVE state).
	InstanceProvisioningReadyReason = "InstanceProvisioningReady"

	// InstanceRunningCondition documents whether the instance is running.
	InstanceRunningCondition clusterv1.ConditionType = "InstanceRunning"
	// InstanceRunningReason documents that the instance is running (ACTIVE).
	InstanceRunningReason = "InstanceRunning"
	// InstanceNotRunningReason documents that the instance is not yet running.
	InstanceNotRunningReason = "InstanceNotRunning"
)

// OVHVolume defines an additional block storage volume to attach to the instance.
type OVHVolume struct {
	// Name is a human-readable name for the volume.
	Name string `json:"name"`

	// SizeGB is the volume size in gigabytes.
	// +kubebuilder:validation:Minimum=1
	SizeGB int `json:"sizeGB"`

	// Type is the volume type (e.g. "classic", "high-speed", "high-speed-gen2").
	// +optional
	Type string `json:"type,omitempty"`
}

// Initialization tracks internal instance provisioning state.
type Initialization struct {
	// Provisioned shows if the instance has been provisioned.
	Provisioned bool `json:"provisioned,omitempty"`
}

// OVHMachineSpec defines the desired state of OVHMachine.
type OVHMachineSpec struct {
	// ProviderID is set by the controller after instance creation.
	// Format: "ovhcloud://<region>/<instanceID>"
	// +optional
	ProviderID string `json:"providerID,omitempty"`

	// FlavorName is the OVH instance flavor (e.g. "b2-7", "b2-15", "c2-15").
	FlavorName string `json:"flavorName"`

	// ImageName is the OS image name (e.g. "Ubuntu 22.04", "Debian 12").
	ImageName string `json:"imageName"`

	// SSHKeyName overrides the cluster-level SSH key for this machine.
	// +optional
	SSHKeyName string `json:"sshKeyName,omitempty"`

	// RootDiskSize is the root disk size in GB. 0 uses the flavor default.
	// +optional
	RootDiskSize int `json:"rootDiskSize,omitempty"`

	// AdditionalVolumes is a list of additional block storage volumes to attach.
	// +optional
	AdditionalVolumes []OVHVolume `json:"additionalVolumes,omitempty"`

	// FailureDomain is the availability zone.
	// +optional
	FailureDomain string `json:"failureDomain,omitempty"`
}

// OVHMachineStatus defines the observed state of OVHMachine.
type OVHMachineStatus struct {
	// Ready is true when the provider resource is ready.
	Ready bool `json:"ready,omitempty"`

	// Conditions defines current service state of the OVH machine.
	// +optional
	Conditions clusterv1.Conditions `json:"conditions,omitempty"`

	// FailureReason is the short name for the reason why a failure might be happening.
	// +optional
	FailureReason string `json:"failureReason,omitempty"`

	// FailureMessage is a full error message dump of the above failureReason.
	// +optional
	FailureMessage string `json:"failureMessage,omitempty"`

	// Addresses contains the OVH instance associated addresses.
	Addresses []clusterv1.MachineAddress `json:"addresses,omitempty"`

	// Initialization tracks internal provisioning state.
	Initialization Initialization `json:"initialization,omitempty"`

	// InstanceID is the OVH instance UUID.
	// +optional
	InstanceID string `json:"instanceID,omitempty"`

	// VolumeIDs tracks attached additional volume IDs.
	// +optional
	VolumeIDs []string `json:"volumeIDs,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.ready",description="Machine is ready"
// +kubebuilder:printcolumn:name="InstanceID",type="string",JSONPath=".status.instanceID",description="OVH instance ID",priority=1

// OVHMachine is the Schema for the ovhmachines API.
type OVHMachine struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   OVHMachineSpec   `json:"spec,omitempty"`
	Status OVHMachineStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// OVHMachineList contains a list of OVHMachine.
type OVHMachineList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []OVHMachine `json:"items"`
}

func init() {
	SchemeBuilder.Register(&OVHMachine{}, &OVHMachineList{})
}

// GetConditions returns the set of conditions for this object.
func (m *OVHMachine) GetConditions() clusterv1.Conditions {
	return m.Status.Conditions
}

// SetConditions sets the conditions on this object.
func (m *OVHMachine) SetConditions(conditions clusterv1.Conditions) {
	m.Status.Conditions = conditions
}
