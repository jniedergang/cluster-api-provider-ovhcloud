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

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"

	infrav1 "github.com/rancher-sandbox/cluster-api-provider-ovhcloud/api/v1alpha2"
	ovhclient "github.com/rancher-sandbox/cluster-api-provider-ovhcloud/pkg/ovh"
)

func TestHandleExistingInstance_Active(t *testing.T) {
	r := &OVHMachineReconciler{}

	ovhMachine := &infrav1.OVHMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-machine",
			Namespace: "default",
		},
		Spec: infrav1.OVHMachineSpec{
			FlavorName: "b2-7",
			ImageName:  "Ubuntu 22.04",
		},
	}

	ovhCluster := &infrav1.OVHCluster{
		Spec: infrav1.OVHClusterSpec{
			Region: "GRA7",
		},
	}

	cluster := &clusterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
	}

	logger := logr.Discard()

	scope := &MachineScope{
		Ctx:        ctx,
		Cluster:    cluster,
		OVHCluster: ovhCluster,
		OVHMachine: ovhMachine,
		Logger:     &logger,
	}

	instance := &ovhclient.Instance{
		ID:     "inst-abc-123",
		Name:   "test-machine",
		Status: ovhclient.InstanceStatusActive,
		Region: "GRA7",
		IPAddresses: []ovhclient.IPAddress{
			{IP: "10.0.0.5", Type: "private", Version: 4},
			{IP: "51.83.42.10", Type: "public", Version: 4},
			{IP: "2001:db8::1", Type: "public", Version: 6}, // Should be skipped
		},
	}

	result, err := r.handleExistingInstance(scope, instance)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.RequeueAfter != 0 {
		t.Errorf("expected no requeue, got %v", result.RequeueAfter)
	}

	// Verify status
	if !ovhMachine.Status.Ready {
		t.Error("expected machine to be ready")
	}

	if ovhMachine.Status.InstanceID != "inst-abc-123" {
		t.Errorf("expected instanceID inst-abc-123, got %s", ovhMachine.Status.InstanceID)
	}

	if ovhMachine.Spec.ProviderID != "ovhcloud://GRA7/inst-abc-123" {
		t.Errorf("expected providerID ovhcloud://GRA7/inst-abc-123, got %s", ovhMachine.Spec.ProviderID)
	}

	// Should have 2 IPv4 addresses (IPv6 skipped)
	if len(ovhMachine.Status.Addresses) != 2 {
		t.Fatalf("expected 2 addresses, got %d", len(ovhMachine.Status.Addresses))
	}

	if ovhMachine.Status.Addresses[0].Type != clusterv1.MachineInternalIP {
		t.Errorf("expected first address to be InternalIP, got %s", ovhMachine.Status.Addresses[0].Type)
	}

	if ovhMachine.Status.Addresses[1].Type != clusterv1.MachineExternalIP {
		t.Errorf("expected second address to be ExternalIP, got %s", ovhMachine.Status.Addresses[1].Type)
	}

	if !ovhMachine.Status.Initialization.Provisioned {
		t.Error("expected initialization.provisioned to be true")
	}
}

func TestHandleExistingInstance_Build(t *testing.T) {
	r := &OVHMachineReconciler{}

	ovhMachine := &infrav1.OVHMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-machine",
			Namespace: "default",
		},
	}

	logger := logr.Discard()

	scope := &MachineScope{
		Ctx:        ctx,
		Cluster:    &clusterv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "default"}},
		OVHCluster: &infrav1.OVHCluster{Spec: infrav1.OVHClusterSpec{Region: "GRA7"}},
		OVHMachine: ovhMachine,
		Logger:     &logger,
	}

	instance := &ovhclient.Instance{
		ID:     "inst-build",
		Status: ovhclient.InstanceStatusBuild,
	}

	result, err := r.handleExistingInstance(scope, instance)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.RequeueAfter != requeueDelayLong {
		t.Errorf("expected requeue after %v, got %v", requeueDelayLong, result.RequeueAfter)
	}

	if ovhMachine.Status.Ready {
		t.Error("expected machine to NOT be ready during BUILD")
	}
}

func TestHandleExistingInstance_Error(t *testing.T) {
	r := &OVHMachineReconciler{}

	ovhMachine := &infrav1.OVHMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-machine",
			Namespace: "default",
		},
	}

	logger := logr.Discard()

	scope := &MachineScope{
		Ctx:        ctx,
		Cluster:    &clusterv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "default"}},
		OVHCluster: &infrav1.OVHCluster{Spec: infrav1.OVHClusterSpec{Region: "GRA7"}},
		OVHMachine: ovhMachine,
		Logger:     &logger,
	}

	instance := &ovhclient.Instance{
		ID:     "inst-error",
		Status: ovhclient.InstanceStatusError,
	}

	result, err := r.handleExistingInstance(scope, instance)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.RequeueAfter != 0 {
		t.Errorf("expected no requeue for ERROR state, got %v", result.RequeueAfter)
	}

	if ovhMachine.Status.Ready {
		t.Error("expected machine to NOT be ready on ERROR")
	}

	if ovhMachine.Status.FailureReason != "InstanceError" {
		t.Errorf("expected FailureReason InstanceError, got %s", ovhMachine.Status.FailureReason)
	}
}

func TestOVHMachineCRD_EnvTest(t *testing.T) {
	setupTestEnv(t)

	// Create an OVHMachine and verify it persists
	machine := &infrav1.OVHMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "envtest-machine",
			Namespace: "default",
		},
		Spec: infrav1.OVHMachineSpec{
			FlavorName: "b2-7",
			ImageName:  "Ubuntu 22.04",
		},
	}

	// Create
	if err := k8sClient.Create(ctx, machine); err != nil {
		t.Fatalf("failed to create OVHMachine: %v", err)
	}

	// Read back
	got := &infrav1.OVHMachine{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(machine), got); err != nil {
		t.Fatalf("failed to get OVHMachine: %v", err)
	}

	if got.Spec.FlavorName != "b2-7" {
		t.Errorf("expected flavorName b2-7, got %s", got.Spec.FlavorName)
	}

	if got.Spec.ImageName != "Ubuntu 22.04" {
		t.Errorf("expected imageName Ubuntu 22.04, got %s", got.Spec.ImageName)
	}

	// Update status
	got.Status.Ready = true
	got.Status.InstanceID = "inst-test-123"

	if err := k8sClient.Status().Update(ctx, got); err != nil {
		t.Fatalf("failed to update OVHMachine status: %v", err)
	}

	// Verify status
	updated := &infrav1.OVHMachine{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(machine), updated); err != nil {
		t.Fatalf("failed to get updated OVHMachine: %v", err)
	}

	if !updated.Status.Ready {
		t.Error("expected status.ready to be true after update")
	}

	if updated.Status.InstanceID != "inst-test-123" {
		t.Errorf("expected instanceID inst-test-123, got %s", updated.Status.InstanceID)
	}

	// Cleanup
	if err := k8sClient.Delete(ctx, machine); err != nil {
		t.Fatalf("failed to delete OVHMachine: %v", err)
	}
}
