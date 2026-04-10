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
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func validOVHCluster() *OVHCluster {
	return &OVHCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: OVHClusterSpec{
			ServiceName: "project-123",
			Region:      "GRA7",
			IdentitySecret: SecretKey{
				Namespace: "default",
				Name:      "ovh-creds",
			},
			LoadBalancerConfig: OVHLoadBalancerConfig{
				SubnetID: "subnet-abc",
			},
		},
	}
}

func TestValidateOVHCluster_Valid(t *testing.T) {
	v := &OVHClusterValidator{}

	_, err := v.ValidateCreate(context.Background(), validOVHCluster())
	if err != nil {
		t.Errorf("expected valid cluster, got error: %v", err)
	}
}

func TestValidateOVHCluster_MissingServiceName(t *testing.T) {
	c := validOVHCluster()
	c.Spec.ServiceName = ""

	v := &OVHClusterValidator{}

	_, err := v.ValidateCreate(context.Background(), c)
	if err == nil {
		t.Fatal("expected error for missing serviceName")
	}

	if !strings.Contains(err.Error(), "spec.serviceName is required") {
		t.Errorf("expected serviceName error, got: %v", err)
	}
}

func TestValidateOVHCluster_MissingRegion(t *testing.T) {
	c := validOVHCluster()
	c.Spec.Region = ""

	v := &OVHClusterValidator{}

	_, err := v.ValidateCreate(context.Background(), c)
	if err == nil {
		t.Fatal("expected error for missing region")
	}

	if !strings.Contains(err.Error(), "spec.region is required") {
		t.Errorf("expected region error, got: %v", err)
	}
}

func TestValidateOVHCluster_MissingIdentitySecret(t *testing.T) {
	c := validOVHCluster()
	c.Spec.IdentitySecret.Name = ""
	c.Spec.IdentitySecret.Namespace = ""

	v := &OVHClusterValidator{}

	_, err := v.ValidateCreate(context.Background(), c)
	if err == nil {
		t.Fatal("expected error for missing identitySecret")
	}

	if !strings.Contains(err.Error(), "spec.identitySecret.name is required") {
		t.Errorf("expected name error, got: %v", err)
	}

	if !strings.Contains(err.Error(), "spec.identitySecret.namespace is required") {
		t.Errorf("expected namespace error, got: %v", err)
	}
}

func TestValidateOVHCluster_MissingSubnetID(t *testing.T) {
	c := validOVHCluster()
	c.Spec.LoadBalancerConfig.SubnetID = ""

	v := &OVHClusterValidator{}

	_, err := v.ValidateCreate(context.Background(), c)
	if err == nil {
		t.Fatal("expected error for missing subnetID")
	}

	if !strings.Contains(err.Error(), "spec.loadBalancerConfig.subnetID is required") {
		t.Errorf("expected subnetID error, got: %v", err)
	}
}

func TestValidateOVHCluster_InvalidGateway(t *testing.T) {
	c := validOVHCluster()
	c.Spec.NetworkConfig = &OVHNetworkConfig{
		SubnetCIDR: "10.0.0.0/24",
		Gateway:    "not-an-ip",
	}

	v := &OVHClusterValidator{}

	_, err := v.ValidateCreate(context.Background(), c)
	if err == nil {
		t.Fatal("expected error for invalid gateway")
	}

	if !strings.Contains(err.Error(), "not a valid IP address") {
		t.Errorf("expected IP validation error, got: %v", err)
	}
}

func TestValidateOVHCluster_NetworkConfigMissing(t *testing.T) {
	c := validOVHCluster()
	c.Spec.NetworkConfig = &OVHNetworkConfig{
		// Neither privateNetworkID nor subnetCIDR
	}

	v := &OVHClusterValidator{}

	_, err := v.ValidateCreate(context.Background(), c)
	if err == nil {
		t.Fatal("expected error for empty networkConfig")
	}

	if !strings.Contains(err.Error(), "requires either privateNetworkID or subnetCIDR") {
		t.Errorf("expected networkConfig error, got: %v", err)
	}
}

func TestValidateOVHCluster_ValidWithNetwork(t *testing.T) {
	c := validOVHCluster()
	c.Spec.NetworkConfig = &OVHNetworkConfig{
		SubnetCIDR: "10.0.0.0/24",
		Gateway:    "10.0.0.1",
		DNSServers: []string{"213.186.33.99"},
	}

	v := &OVHClusterValidator{}

	_, err := v.ValidateCreate(context.Background(), c)
	if err != nil {
		t.Errorf("expected valid cluster with network, got error: %v", err)
	}
}

func TestValidateOVHCluster_MultipleErrors(t *testing.T) {
	c := &OVHCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "bad", Namespace: "default"},
		Spec:       OVHClusterSpec{},
	}

	v := &OVHClusterValidator{}

	_, err := v.ValidateCreate(context.Background(), c)
	if err == nil {
		t.Fatal("expected errors for empty spec")
	}

	// Should report all errors at once
	errMsg := err.Error()
	expectedErrors := []string{"serviceName", "region", "identitySecret.name", "identitySecret.namespace", "subnetID"}

	for _, expected := range expectedErrors {
		if !strings.Contains(errMsg, expected) {
			t.Errorf("expected error message to contain %q, got: %s", expected, errMsg)
		}
	}
}

func TestValidateOVHCluster_Update(t *testing.T) {
	v := &OVHClusterValidator{}

	_, err := v.ValidateUpdate(context.Background(), validOVHCluster(), validOVHCluster())
	if err != nil {
		t.Errorf("expected valid update, got error: %v", err)
	}
}

func TestValidateOVHCluster_Delete(t *testing.T) {
	v := &OVHClusterValidator{}

	_, err := v.ValidateDelete(context.Background(), &OVHCluster{})
	if err != nil {
		t.Errorf("delete should always succeed, got error: %v", err)
	}
}
