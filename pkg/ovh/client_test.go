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

package ovh

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	goovh "github.com/ovh/go-ovh/ovh"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	testServiceName = "test-project-123"
	testRegion      = "GRA7"
)

// newTestServer creates a mock HTTP server that handles OVH API requests.
// The handler map keys are "METHOD /path" strings.
func newTestServer(t *testing.T, handlers map[string]http.HandlerFunc) (*httptest.Server, *Client) {
	t.Helper()

	mux := http.NewServeMux()

	// OVH time endpoint (required by go-ovh for request signing)
	mux.HandleFunc("GET /auth/time", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, "%d", 1700000000)
	})

	// Register handlers
	for pattern, handler := range handlers {
		mux.HandleFunc(pattern, handler)
	}

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	// Create OVH client pointing to our test server
	api, err := goovh.NewClient(server.URL, "test-ak", "test-as", "test-ck")
	if err != nil {
		t.Fatalf("failed to create test OVH client: %v", err)
	}

	client := &Client{
		api:         api,
		serviceName: testServiceName,
		region:      testRegion,
		logger:      logr.Discard(),
	}

	return server, client
}

func jsonResponse(w http.ResponseWriter, statusCode int, body interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	if body != nil {
		json.NewEncoder(w).Encode(body) //nolint:errcheck
	}
}

func TestNewClientFromSecret(t *testing.T) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ovh-creds",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"endpoint":          []byte("ovh-eu"),
			"applicationKey":    []byte("test-ak"),
			"applicationSecret": []byte("test-as"),
			"consumerKey":       []byte("test-ck"),
		},
	}

	client, err := NewClientFromSecret(secret, "svc-123", "GRA7", logr.Discard())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.serviceName != "svc-123" {
		t.Errorf("expected serviceName svc-123, got %s", client.serviceName)
	}

	if client.region != "GRA7" {
		t.Errorf("expected region GRA7, got %s", client.region)
	}
}

func TestNewClientFromSecret_MissingKeys(t *testing.T) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ovh-creds",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"endpoint":       []byte("ovh-eu"),
			"applicationKey": []byte("test-ak"),
			// Missing applicationSecret and consumerKey
		},
	}

	_, err := NewClientFromSecret(secret, "svc-123", "GRA7", logr.Discard())
	if err == nil {
		t.Fatal("expected error for missing keys, got nil")
	}

	if !strings.Contains(err.Error(), "missing required keys") {
		t.Errorf("expected 'missing required keys' error, got: %v", err)
	}
}

func TestValidateCredentials(t *testing.T) {
	_, client := newTestServer(t, map[string]http.HandlerFunc{
		"GET /me": func(w http.ResponseWriter, r *http.Request) {
			jsonResponse(w, http.StatusOK, Me{
				Nichandle: "ab12345-ovh",
				FirstName: "Test",
				Name:      "User",
				Email:     "test@example.com",
			})
		},
	})

	me, err := client.ValidateCredentials()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if me.Nichandle != "ab12345-ovh" {
		t.Errorf("expected nichandle ab12345-ovh, got %s", me.Nichandle)
	}
}

func TestCreateInstance(t *testing.T) {
	expectedPath := fmt.Sprintf("/cloud/project/%s/instance", testServiceName)

	_, client := newTestServer(t, map[string]http.HandlerFunc{
		"POST " + expectedPath: func(w http.ResponseWriter, r *http.Request) {
			var opts CreateInstanceOpts
			json.NewDecoder(r.Body).Decode(&opts) //nolint:errcheck

			if opts.Name != "test-vm" {
				t.Errorf("expected name test-vm, got %s", opts.Name)
			}

			jsonResponse(w, http.StatusOK, Instance{
				ID:     "inst-abc-123",
				Name:   opts.Name,
				Status: InstanceStatusBuild,
				Region: testRegion,
			})
		},
	})

	instance, err := client.CreateInstance(CreateInstanceOpts{
		Name:     "test-vm",
		FlavorID: "flavor-1",
		ImageID:  "image-1",
		Region:   testRegion,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if instance.ID != "inst-abc-123" {
		t.Errorf("expected ID inst-abc-123, got %s", instance.ID)
	}

	if instance.Status != InstanceStatusBuild {
		t.Errorf("expected status BUILD, got %s", instance.Status)
	}
}

func TestGetInstance(t *testing.T) {
	instanceID := "inst-abc-123"
	expectedPath := fmt.Sprintf("/cloud/project/%s/instance/%s", testServiceName, instanceID)

	_, client := newTestServer(t, map[string]http.HandlerFunc{
		"GET " + expectedPath: func(w http.ResponseWriter, r *http.Request) {
			jsonResponse(w, http.StatusOK, Instance{
				ID:     instanceID,
				Name:   "test-vm",
				Status: InstanceStatusActive,
				Region: testRegion,
				IPAddresses: []IPAddress{
					{IP: "10.0.0.5", Type: "private", Version: 4},
					{IP: "51.83.42.10", Type: "public", Version: 4},
				},
			})
		},
	})

	instance, err := client.GetInstance(instanceID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if instance.Status != InstanceStatusActive {
		t.Errorf("expected status ACTIVE, got %s", instance.Status)
	}

	if len(instance.IPAddresses) != 2 {
		t.Fatalf("expected 2 IP addresses, got %d", len(instance.IPAddresses))
	}

	if instance.IPAddresses[0].IP != "10.0.0.5" {
		t.Errorf("expected first IP 10.0.0.5, got %s", instance.IPAddresses[0].IP)
	}
}

func TestDeleteInstance(t *testing.T) {
	instanceID := "inst-abc-123"
	expectedPath := fmt.Sprintf("/cloud/project/%s/instance/%s", testServiceName, instanceID)

	_, client := newTestServer(t, map[string]http.HandlerFunc{
		"DELETE " + expectedPath: func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		},
	})

	err := client.DeleteInstance(instanceID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeleteInstance_NotFound(t *testing.T) {
	instanceID := "inst-gone"
	expectedPath := fmt.Sprintf("/cloud/project/%s/instance/%s", testServiceName, instanceID)

	_, client := newTestServer(t, map[string]http.HandlerFunc{
		"DELETE " + expectedPath: func(w http.ResponseWriter, r *http.Request) {
			jsonResponse(w, http.StatusNotFound, map[string]string{"message": "not found"})
		},
	})

	// Should not return error for already-deleted instance
	err := client.DeleteInstance(instanceID)
	if err != nil {
		t.Fatalf("expected nil error for 404, got: %v", err)
	}
}

func TestGetFlavorByName(t *testing.T) {
	expectedPath := fmt.Sprintf("/cloud/project/%s/flavor", testServiceName)

	_, client := newTestServer(t, map[string]http.HandlerFunc{
		"GET " + expectedPath: func(w http.ResponseWriter, r *http.Request) {
			jsonResponse(w, http.StatusOK, []Flavor{
				{ID: "f-1", Name: "b2-7", VCPUs: 2, RAM: 7168, Disk: 50, Region: testRegion},
				{ID: "f-2", Name: "b2-15", VCPUs: 4, RAM: 15360, Disk: 100, Region: testRegion},
				{ID: "f-3", Name: "c2-15", VCPUs: 4, RAM: 15360, Disk: 100, Region: testRegion},
			})
		},
	})

	flavor, err := client.GetFlavorByName("b2-15")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if flavor.ID != "f-2" {
		t.Errorf("expected flavor ID f-2, got %s", flavor.ID)
	}

	if flavor.VCPUs != 4 {
		t.Errorf("expected 4 vCPUs, got %d", flavor.VCPUs)
	}
}

func TestGetFlavorByName_NotFound(t *testing.T) {
	expectedPath := fmt.Sprintf("/cloud/project/%s/flavor", testServiceName)

	_, client := newTestServer(t, map[string]http.HandlerFunc{
		"GET " + expectedPath: func(w http.ResponseWriter, r *http.Request) {
			jsonResponse(w, http.StatusOK, []Flavor{
				{ID: "f-1", Name: "b2-7", Region: testRegion},
			})
		},
	})

	_, err := client.GetFlavorByName("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent flavor")
	}

	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

func TestGetImageByName(t *testing.T) {
	expectedPath := fmt.Sprintf("/cloud/project/%s/image", testServiceName)

	_, client := newTestServer(t, map[string]http.HandlerFunc{
		"GET " + expectedPath: func(w http.ResponseWriter, r *http.Request) {
			jsonResponse(w, http.StatusOK, []Image{
				{ID: "img-1", Name: "Ubuntu 22.04", Region: testRegion, Status: "active"},
				{ID: "img-2", Name: "Debian 12", Region: testRegion, Status: "active"},
				{ID: "img-3", Name: "SLES 15 SP6", Region: testRegion, Status: "active"},
			})
		},
	})

	// Exact match
	image, err := client.GetImageByName("Ubuntu 22.04")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if image.ID != "img-1" {
		t.Errorf("expected image ID img-1, got %s", image.ID)
	}

	// Partial match
	image, err = client.GetImageByName("debian")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if image.ID != "img-2" {
		t.Errorf("expected image ID img-2 for partial match, got %s", image.ID)
	}
}

func TestCreateLoadBalancer(t *testing.T) {
	expectedPath := fmt.Sprintf("/cloud/project/%s/region/%s/loadbalancing/loadbalancer",
		testServiceName, testRegion)

	_, client := newTestServer(t, map[string]http.HandlerFunc{
		"POST " + expectedPath: func(w http.ResponseWriter, r *http.Request) {
			jsonResponse(w, http.StatusOK, LoadBalancer{
				ID:                 "lb-123",
				Name:               "capi-lb",
				ProvisioningStatus: "ACTIVE",
				OperatingStatus:    "ONLINE",
				VIPAddress:         "10.0.0.100",
			})
		},
	})

	lb, err := client.CreateLoadBalancer(CreateLoadBalancerOpts{
		Name:        "capi-lb",
		VIPSubnetID: "subnet-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if lb.VIPAddress != "10.0.0.100" {
		t.Errorf("expected VIP 10.0.0.100, got %s", lb.VIPAddress)
	}
}

func TestProjectPath(t *testing.T) {
	client := &Client{
		serviceName: "proj-abc",
		region:      "GRA7",
		logger:      logr.Discard(),
	}

	path := client.projectPath("/instance/%s", "inst-123")
	expected := "/cloud/project/proj-abc/instance/inst-123"

	if path != expected {
		t.Errorf("expected %s, got %s", expected, path)
	}
}

func TestRegionPath(t *testing.T) {
	client := &Client{
		serviceName: "proj-abc",
		region:      "GRA7",
		logger:      logr.Discard(),
	}

	path := client.regionPath("/loadbalancing/loadbalancer/%s", "lb-123")
	expected := "/cloud/project/proj-abc/region/GRA7/loadbalancing/loadbalancer/lb-123"

	if path != expected {
		t.Errorf("expected %s, got %s", expected, path)
	}
}
