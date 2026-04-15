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
	"context"
	"path/filepath"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"

	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"

	infrav1 "github.com/rancher-sandbox/cluster-api-provider-ovhcloud/api/v1alpha2"
)

var (
	testEnv   *envtest.Environment
	k8sClient client.Client
	scheme    = runtime.NewScheme()
	ctx       = context.Background()
)

func setupTestEnv(t *testing.T) {
	t.Helper()

	// Register schemes
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add client-go scheme: %v", err)
	}

	if err := infrav1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add infrav1 scheme: %v", err)
	}

	if err := clusterv1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add clusterv1 scheme: %v", err)
	}

	// Setup envtest
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "config", "crd", "bases"),
		},
		ErrorIfCRDPathMissing: true,
	}

	cfg, err := testEnv.Start()
	if err != nil {
		t.Fatalf("failed to start envtest: %v", err)
	}

	t.Cleanup(func() {
		err := testEnv.Stop()
		if err != nil {
			t.Errorf("failed to stop envtest: %v", err)
		}
	})

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatalf("failed to create k8s client: %v", err)
	}
}
