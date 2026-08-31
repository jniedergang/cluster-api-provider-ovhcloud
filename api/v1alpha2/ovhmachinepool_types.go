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

package v1alpha2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// OVHMachinePoolSpec defines the desired state of OVHMachinePool.
type OVHMachinePoolSpec struct {
	// FlavorName is the OVH instance flavor (e.g. "b2-7", "b2-15").
	FlavorName string `json:"flavorName"`

	// ImageName is the OS image name.
	ImageName string `json:"imageName"`

	// SSHKeyName is the SSH key to inject.
	// +optional
	SSHKeyName string `json:"sshKeyName,omitempty"`

	// RootDiskSize is the root disk size in GB. 0 uses the flavor default.
	// +optional
	RootDiskSize int `json:"rootDiskSize,omitempty"`

	// ProviderIDList is set by the controller with the provider IDs of all
	// instances in the pool.
	// +optional
	ProviderIDList []string `json:"providerIDList,omitempty"`
}

// OVHMachinePoolStatus defines the observed state of OVHMachinePool.
type OVHMachinePoolStatus struct {
	// Ready is true when all pool instances are provisioned.
	// +optional
	Ready bool `json:"ready,omitempty"`

	// Replicas is the current number of instances in the pool.
	// +optional
	Replicas int32 `json:"replicas,omitempty"`

	// ReadyReplicas is the number of ready instances.
	// +optional
	ReadyReplicas int32 `json:"readyReplicas,omitempty"`

	// Conditions defines current service state of the pool.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// FailureReason is the short name for a failure.
	// +optional
	FailureReason string `json:"failureReason,omitempty"`

	// FailureMessage is a full error message.
	// +optional
	FailureMessage string `json:"failureMessage,omitempty"`

	// InfrastructureReady is true when the pool infrastructure is ready.
	// +optional
	InfrastructureReady bool `json:"infrastructureReady,omitempty"`

	// InstanceIDs tracks the OVH instance UUIDs in the pool.
	// +optional
	InstanceIDs []string `json:"instanceIDs,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:storageversion
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.ready",description="Pool is ready"
// +kubebuilder:printcolumn:name="Replicas",type="integer",JSONPath=".status.replicas",description="Current replicas"

// OVHMachinePool is the Schema for the ovhmachinepools API.
type OVHMachinePool struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   OVHMachinePoolSpec   `json:"spec,omitempty"`
	Status OVHMachinePoolStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// OVHMachinePoolList contains a list of OVHMachinePool.
type OVHMachinePoolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []OVHMachinePool `json:"items"`
}

func init() {
	SchemeBuilder.Register(&OVHMachinePool{}, &OVHMachinePoolList{})
}

// GetConditions returns the set of conditions for this object.
func (m *OVHMachinePool) GetConditions() []metav1.Condition {
	return m.Status.Conditions
}

// SetConditions sets the conditions on this object.
func (m *OVHMachinePool) SetConditions(conditions []metav1.Condition) {
	m.Status.Conditions = conditions
}
