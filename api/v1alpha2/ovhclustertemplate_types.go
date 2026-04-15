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

// OVHClusterTemplateSpec defines the desired state of OVHClusterTemplate.
type OVHClusterTemplateSpec struct {
	Template OVHClusterTemplateResource `json:"template"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=ovhclustertemplates,scope=Namespaced,categories=cluster-api,shortName=ovhct
// +kubebuilder:storageversion

// OVHClusterTemplate is the Schema for the ovhclustertemplates API.
type OVHClusterTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec OVHClusterTemplateSpec `json:"spec,omitempty"`
}

// OVHClusterTemplateResource defines the template resource for OVHCluster.
type OVHClusterTemplateResource struct {
	// Standard object's metadata.
	// +optional
	ObjectMeta clusterv1.ObjectMeta `json:"metadata,omitempty"`
	Spec       OVHClusterSpec       `json:"spec"`
}

//+kubebuilder:object:root=true

// OVHClusterTemplateList contains a list of OVHClusterTemplate.
type OVHClusterTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []OVHClusterTemplate `json:"items"`
}

func init() {
	SchemeBuilder.Register(&OVHClusterTemplate{}, &OVHClusterTemplateList{})
}
