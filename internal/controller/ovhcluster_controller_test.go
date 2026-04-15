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
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	infrav1 "github.com/rancher-sandbox/cluster-api-provider-ovhcloud/api/v1alpha2"
)

func TestOVHClusterCRD_EnvTest(t *testing.T) {
	setupTestEnv(t)

	cluster := &infrav1.OVHCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "envtest-cluster",
			Namespace: "default",
		},
		Spec: infrav1.OVHClusterSpec{
			ServiceName: "test-project-id",
			Region:      "GRA7",
			IdentitySecret: infrav1.SecretKey{
				Namespace: "default",
				Name:      "ovh-creds",
			},
			LoadBalancerConfig: infrav1.OVHLoadBalancerConfig{
				SubnetID: "subnet-123",
			},
		},
	}

	// Create
	if err := k8sClient.Create(ctx, cluster); err != nil {
		t.Fatalf("failed to create OVHCluster: %v", err)
	}

	// Read back
	got := &infrav1.OVHCluster{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), got); err != nil {
		t.Fatalf("failed to get OVHCluster: %v", err)
	}

	if got.Spec.ServiceName != "test-project-id" {
		t.Errorf("expected serviceName test-project-id, got %s", got.Spec.ServiceName)
	}

	if got.Spec.Region != "GRA7" {
		t.Errorf("expected region GRA7, got %s", got.Spec.Region)
	}

	if got.Spec.IdentitySecret.Name != "ovh-creds" {
		t.Errorf("expected identitySecret.name ovh-creds, got %s", got.Spec.IdentitySecret.Name)
	}

	// Update status
	got.Status.Ready = true
	got.Status.NetworkID = "net-abc"
	got.Status.SubnetID = "sub-def"
	got.Status.LoadBalancerID = "lb-ghi"

	if err := k8sClient.Status().Update(ctx, got); err != nil {
		t.Fatalf("failed to update OVHCluster status: %v", err)
	}

	// Verify status
	updated := &infrav1.OVHCluster{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), updated); err != nil {
		t.Fatalf("failed to get updated OVHCluster: %v", err)
	}

	if !updated.Status.Ready {
		t.Error("expected status.ready to be true")
	}

	if updated.Status.NetworkID != "net-abc" {
		t.Errorf("expected networkID net-abc, got %s", updated.Status.NetworkID)
	}

	if updated.Status.LoadBalancerID != "lb-ghi" {
		t.Errorf("expected loadBalancerID lb-ghi, got %s", updated.Status.LoadBalancerID)
	}

	// Cleanup
	if err := k8sClient.Delete(ctx, cluster); err != nil {
		t.Fatalf("failed to delete OVHCluster: %v", err)
	}
}

func TestOVHClusterTemplate_EnvTest(t *testing.T) {
	setupTestEnv(t)

	template := &infrav1.OVHClusterTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "envtest-cluster-template",
			Namespace: "default",
		},
		Spec: infrav1.OVHClusterTemplateSpec{
			Template: infrav1.OVHClusterTemplateResource{
				Spec: infrav1.OVHClusterSpec{
					ServiceName: "template-project",
					Region:      "SBG5",
					IdentitySecret: infrav1.SecretKey{
						Namespace: "default",
						Name:      "ovh-creds-template",
					},
					LoadBalancerConfig: infrav1.OVHLoadBalancerConfig{
						SubnetID: "subnet-tpl",
					},
				},
			},
		},
	}

	if err := k8sClient.Create(ctx, template); err != nil {
		t.Fatalf("failed to create OVHClusterTemplate: %v", err)
	}

	got := &infrav1.OVHClusterTemplate{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(template), got); err != nil {
		t.Fatalf("failed to get OVHClusterTemplate: %v", err)
	}

	if got.Spec.Template.Spec.Region != "SBG5" {
		t.Errorf("expected template region SBG5, got %s", got.Spec.Template.Spec.Region)
	}

	if err := k8sClient.Delete(ctx, template); err != nil {
		t.Fatalf("failed to delete OVHClusterTemplate: %v", err)
	}
}

func TestOVHMachineTemplate_EnvTest(t *testing.T) {
	setupTestEnv(t)

	template := &infrav1.OVHMachineTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "envtest-machine-template",
			Namespace: "default",
		},
		Spec: infrav1.OVHMachineTemplateSpec{
			Template: infrav1.OVHMachineTemplateResource{
				Spec: infrav1.OVHMachineSpec{
					FlavorName:   "c2-15",
					ImageName:    "Debian 12",
					RootDiskSize: 100,
					AdditionalVolumes: []infrav1.OVHVolume{
						{Name: "data", SizeGB: 50, Type: "high-speed"},
					},
				},
			},
		},
	}

	if err := k8sClient.Create(ctx, template); err != nil {
		t.Fatalf("failed to create OVHMachineTemplate: %v", err)
	}

	got := &infrav1.OVHMachineTemplate{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(template), got); err != nil {
		t.Fatalf("failed to get OVHMachineTemplate: %v", err)
	}

	if got.Spec.Template.Spec.FlavorName != "c2-15" {
		t.Errorf("expected flavor c2-15, got %s", got.Spec.Template.Spec.FlavorName)
	}

	if len(got.Spec.Template.Spec.AdditionalVolumes) != 1 {
		t.Fatalf("expected 1 volume, got %d", len(got.Spec.Template.Spec.AdditionalVolumes))
	}

	if got.Spec.Template.Spec.AdditionalVolumes[0].SizeGB != 50 {
		t.Errorf("expected volume size 50, got %d", got.Spec.Template.Spec.AdditionalVolumes[0].SizeGB)
	}

	if err := k8sClient.Delete(ctx, template); err != nil {
		t.Fatalf("failed to delete OVHMachineTemplate: %v", err)
	}
}
