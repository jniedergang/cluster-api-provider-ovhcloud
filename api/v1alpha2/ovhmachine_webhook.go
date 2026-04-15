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
	"context"
	"fmt"
	"strings"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"k8s.io/apimachinery/pkg/runtime"
)

// OVHMachineValidator implements admission.CustomValidator for OVHMachine.
type OVHMachineValidator struct{}

// SetupOVHMachineWebhookWithManager sets up the validating webhook for OVHMachine.
func SetupOVHMachineWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&OVHMachine{}).
		WithValidator(&OVHMachineValidator{}).
		Complete()
}

//nolint:lll
// +kubebuilder:webhook:path=/validate-infrastructure-cluster-x-k8s-io-v1alpha2-ovhmachine,mutating=false,failurePolicy=fail,sideEffects=None,groups=infrastructure.cluster.x-k8s.io,resources=ovhmachines,verbs=create;update,versions=v1alpha2,name=v2ovhmachine.kb.io,admissionReviewVersions=v1

var _ admission.CustomValidator = &OVHMachineValidator{}

// ValidateCreate implements admission.CustomValidator.
func (v *OVHMachineValidator) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	m, ok := obj.(*OVHMachine)
	if !ok {
		return nil, fmt.Errorf("expected OVHMachine, got %T", obj)
	}

	return validateOVHMachine(m)
}

// ValidateUpdate implements admission.CustomValidator.
func (v *OVHMachineValidator) ValidateUpdate(_ context.Context, _, newObj runtime.Object) (admission.Warnings, error) {
	m, ok := newObj.(*OVHMachine)
	if !ok {
		return nil, fmt.Errorf("expected OVHMachine, got %T", newObj)
	}

	return validateOVHMachine(m)
}

// ValidateDelete implements admission.CustomValidator.
func (v *OVHMachineValidator) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

func validateOVHMachine(m *OVHMachine) (admission.Warnings, error) {
	var errs []string

	if m.Spec.FlavorName == "" {
		errs = append(errs, "spec.flavorName is required")
	}

	if m.Spec.ImageName == "" {
		errs = append(errs, "spec.imageName is required")
	}

	if m.Spec.RootDiskSize < 0 {
		errs = append(errs, "spec.rootDiskSize must be >= 0")
	}

	for i, vol := range m.Spec.AdditionalVolumes {
		if vol.Name == "" {
			errs = append(errs, fmt.Sprintf("spec.additionalVolumes[%d].name is required", i))
		}

		if vol.SizeGB < 1 {
			errs = append(errs, fmt.Sprintf("spec.additionalVolumes[%d].sizeGB must be >= 1", i))
		}
	}

	if len(errs) > 0 {
		return nil, fmt.Errorf("validation failed for OVHMachine %s/%s: %s",
			m.Namespace, m.Name, strings.Join(errs, "; "))
	}

	return nil, nil
}
