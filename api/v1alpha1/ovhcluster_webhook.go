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
	"context"
	"fmt"
	"net"
	"strings"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"k8s.io/apimachinery/pkg/runtime"
)

// OVHClusterValidator implements admission.CustomValidator for OVHCluster.
type OVHClusterValidator struct{}

// SetupOVHClusterWebhookWithManager sets up the validating webhook for OVHCluster.
func SetupOVHClusterWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&OVHCluster{}).
		WithValidator(&OVHClusterValidator{}).
		Complete()
}

//nolint:lll
// +kubebuilder:webhook:path=/validate-infrastructure-cluster-x-k8s-io-v1alpha1-ovhcluster,mutating=false,failurePolicy=fail,sideEffects=None,groups=infrastructure.cluster.x-k8s.io,resources=ovhclusters,verbs=create;update,versions=v1alpha1,name=vovhcluster.kb.io,admissionReviewVersions=v1

var _ admission.CustomValidator = &OVHClusterValidator{}

// ValidateCreate implements admission.CustomValidator.
func (v *OVHClusterValidator) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	c, ok := obj.(*OVHCluster)
	if !ok {
		return nil, fmt.Errorf("expected OVHCluster, got %T", obj)
	}

	return validateOVHCluster(c)
}

// ValidateUpdate implements admission.CustomValidator.
func (v *OVHClusterValidator) ValidateUpdate(_ context.Context, _, newObj runtime.Object) (admission.Warnings, error) {
	c, ok := newObj.(*OVHCluster)
	if !ok {
		return nil, fmt.Errorf("expected OVHCluster, got %T", newObj)
	}

	return validateOVHCluster(c)
}

// ValidateDelete implements admission.CustomValidator.
func (v *OVHClusterValidator) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

func validateOVHCluster(c *OVHCluster) (admission.Warnings, error) {
	var errs []string

	if c.Spec.ServiceName == "" {
		errs = append(errs, "spec.serviceName is required")
	}

	if c.Spec.Region == "" {
		errs = append(errs, "spec.region is required")
	}

	if c.Spec.IdentitySecret.Name == "" {
		errs = append(errs, "spec.identitySecret.name is required")
	}

	if c.Spec.IdentitySecret.Namespace == "" {
		errs = append(errs, "spec.identitySecret.namespace is required")
	}

	// SubnetID is optional: the controller creates a subnet via NetworkConfig if unset.
	// Validate that at least one of subnetID or networkConfig is provided.
	if c.Spec.LoadBalancerConfig.SubnetID == "" && c.Spec.NetworkConfig == nil {
		errs = append(errs, "either spec.loadBalancerConfig.subnetID or spec.networkConfig must be provided")
	}

	if c.Spec.NetworkConfig != nil {
		nc := c.Spec.NetworkConfig
		if nc.PrivateNetworkID == "" && nc.SubnetCIDR == "" {
			errs = append(errs, "spec.networkConfig requires either privateNetworkID or subnetCIDR")
		}

		if nc.Gateway != "" {
			if net.ParseIP(nc.Gateway) == nil {
				errs = append(errs, fmt.Sprintf("spec.networkConfig.gateway %q is not a valid IP address", nc.Gateway))
			}
		}
	}

	if len(errs) > 0 {
		return nil, fmt.Errorf("validation failed for OVHCluster %s/%s: %s",
			c.Namespace, c.Name, strings.Join(errs, "; "))
	}

	return nil, nil
}
