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

	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
)

// OVHMachineTemplateSpec defines the desired state of OVHMachineTemplate.
type OVHMachineTemplateSpec struct {
	// Template is the OVHMachineTemplate template.
	Template OVHMachineTemplateResource `json:"template,omitempty"`
}

// OVHMachineTemplateResource describes the data needed to create an OVHMachine from a template.
type OVHMachineTemplateResource struct {
	// Standard object's metadata.
	// +optional
	ObjectMeta clusterv1.ObjectMeta `json:"metadata,omitempty"`
	// Spec is the specification of the desired behavior of the machine.
	Spec OVHMachineSpec `json:"spec"`
}

//+kubebuilder:object:root=true
//+kubebuilder:storageversion

// OVHMachineTemplate is the Schema for the ovhmachinetemplates API.
type OVHMachineTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec OVHMachineTemplateSpec `json:"spec,omitempty"`
}

//+kubebuilder:object:root=true

// OVHMachineTemplateList contains a list of OVHMachineTemplate.
type OVHMachineTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []OVHMachineTemplate `json:"items"`
}

func init() {
	SchemeBuilder.Register(&OVHMachineTemplate{}, &OVHMachineTemplateList{})
}
