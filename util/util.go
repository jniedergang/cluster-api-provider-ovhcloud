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

// Package util provides utility functions for CAPIOVH controllers.
package util

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	infrav1 "github.com/rancher-sandbox/cluster-api-provider-ovhcloud/api/v1alpha1"
	ovhclient "github.com/rancher-sandbox/cluster-api-provider-ovhcloud/pkg/ovh"
)

// rfc1035Regex matches characters not allowed in RFC 1035 DNS labels.
var rfc1035Regex = regexp.MustCompile(`[^a-z0-9-]`)

// GetOVHClientFromCluster creates an OVH API client using the identity secret referenced by the OVHCluster.
func GetOVHClientFromCluster(
	ctx context.Context,
	c client.Client,
	ovhCluster *infrav1.OVHCluster,
	logger logr.Logger,
) (*ovhclient.Client, error) {
	secretRef := ovhCluster.Spec.IdentitySecret

	secret := &corev1.Secret{}

	err := c.Get(ctx, types.NamespacedName{
		Namespace: secretRef.Namespace,
		Name:      secretRef.Name,
	}, secret)
	if err != nil {
		return nil, fmt.Errorf("getting OVH identity secret %s/%s: %w", secretRef.Namespace, secretRef.Name, err)
	}

	return ovhclient.NewClientFromSecret(
		secret,
		ovhCluster.Spec.ServiceName,
		ovhCluster.Spec.Region,
		logger,
	)
}

// GenerateRFC1035Name generates an RFC 1035-compliant name from the given parts.
// The result is lowercase, alphanumeric + hyphens only, max 63 chars.
func GenerateRFC1035Name(parts ...string) string {
	name := strings.ToLower(strings.Join(parts, "-"))
	name = rfc1035Regex.ReplaceAllString(name, "-")
	name = strings.Trim(name, "-")

	if len(name) > 63 {
		name = name[:63]
		name = strings.TrimRight(name, "-")
	}

	return name
}

// ProviderIDFromInstance returns the provider ID for an OVH instance.
// Format: "ovhcloud://<region>/<instanceID>"
func ProviderIDFromInstance(region, instanceID string) string {
	return fmt.Sprintf("ovhcloud://%s/%s", region, instanceID)
}

// ParseProviderID extracts region and instance ID from a provider ID string.
func ParseProviderID(providerID string) (region, instanceID string, err error) {
	if !strings.HasPrefix(providerID, "ovhcloud://") {
		return "", "", fmt.Errorf("invalid providerID format: %q (expected ovhcloud://<region>/<instanceID>)", providerID)
	}

	parts := strings.SplitN(strings.TrimPrefix(providerID, "ovhcloud://"), "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid providerID format: %q", providerID)
	}

	return parts[0], parts[1], nil
}

// GetSSHKeyName returns the effective SSH key name for a machine,
// falling back to the cluster-level key if not set on the machine.
func GetSSHKeyName(machineSSHKey, clusterSSHKey string) string {
	if machineSSHKey != "" {
		return machineSSHKey
	}

	return clusterSSHKey
}
