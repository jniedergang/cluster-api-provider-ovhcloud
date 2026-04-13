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
	expectedPath := fmt.Sprintf("/cloud/project/%s/region", testServiceName)

	_, client := newTestServer(t, map[string]http.HandlerFunc{
		"GET " + expectedPath: func(w http.ResponseWriter, r *http.Request) {
			jsonResponse(w, http.StatusOK, []string{"EU-WEST-PAR", "GRA9"})
		},
	})

	if err := client.ValidateCredentials(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateCredentials_NoRegions(t *testing.T) {
	expectedPath := fmt.Sprintf("/cloud/project/%s/region", testServiceName)

	_, client := newTestServer(t, map[string]http.HandlerFunc{
		"GET " + expectedPath: func(w http.ResponseWriter, r *http.Request) {
			jsonResponse(w, http.StatusOK, []string{})
		},
	})

	err := client.ValidateCredentials()
	if err == nil {
		t.Fatal("expected error when no regions returned")
	}

	if !strings.Contains(err.Error(), "no regions") {
		t.Errorf("expected 'no regions' error, got: %v", err)
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

func TestGetImageByName_UUIDShortcut(t *testing.T) {
	// When given a UUID, the client must NOT make any API call —
	// it returns the UUID as-is so the caller can use it directly.
	_, client := newTestServer(t, map[string]http.HandlerFunc{
		// No handlers — any HTTP call would 404 and fail the test
	})

	uuid := "865193d1-cd97-445c-ade9-ac9981fd1cbe"

	image, err := client.GetImageByName(uuid)
	if err != nil {
		t.Fatalf("unexpected error for UUID input: %v", err)
	}

	if image.ID != uuid {
		t.Errorf("expected ID=%s, got %s", uuid, image.ID)
	}
}

func TestGetImageByName_EmptyName(t *testing.T) {
	_, client := newTestServer(t, map[string]http.HandlerFunc{})

	_, err := client.GetImageByName("")
	if err == nil {
		t.Fatal("expected error for empty image name")
	}
}

func TestGetImageByName_BYOIFallback(t *testing.T) {
	// Image not in /image (public), but exists in /snapshot (BYOI).
	// The client must fall back to /snapshot transparently.
	imagePath := fmt.Sprintf("/cloud/project/%s/image", testServiceName)
	snapshotPath := fmt.Sprintf("/cloud/project/%s/snapshot", testServiceName)

	publicCalled := false
	snapshotCalled := false

	_, client := newTestServer(t, map[string]http.HandlerFunc{
		"GET " + imagePath: func(w http.ResponseWriter, r *http.Request) {
			publicCalled = true
			jsonResponse(w, http.StatusOK, []Image{
				{ID: "pub-1", Name: "Ubuntu 22.04", Region: testRegion},
			})
		},
		"GET " + snapshotPath: func(w http.ResponseWriter, r *http.Request) {
			snapshotCalled = true
			jsonResponse(w, http.StatusOK, []Image{
				{ID: "byoi-1", Name: "openSUSE-Leap-15.6", Region: testRegion, Visibility: "private"},
				{ID: "byoi-2", Name: "MyCompany-RHEL-9", Region: testRegion, Visibility: "private"},
			})
		},
	})

	image, err := client.GetImageByName("openSUSE-Leap-15.6")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if image.ID != "byoi-1" {
		t.Errorf("expected BYOI image ID byoi-1, got %s", image.ID)
	}

	if !publicCalled {
		t.Error("expected public images endpoint to be called first")
	}

	if !snapshotCalled {
		t.Error("expected snapshot endpoint to be called as fallback")
	}
}

func TestGetImageByName_PublicPreferred(t *testing.T) {
	// When image exists in public, snapshot endpoint is NOT called (perf).
	imagePath := fmt.Sprintf("/cloud/project/%s/image", testServiceName)
	snapshotPath := fmt.Sprintf("/cloud/project/%s/snapshot", testServiceName)

	snapshotCalled := false

	_, client := newTestServer(t, map[string]http.HandlerFunc{
		"GET " + imagePath: func(w http.ResponseWriter, r *http.Request) {
			jsonResponse(w, http.StatusOK, []Image{
				{ID: "pub-1", Name: "Ubuntu 22.04", Region: testRegion},
			})
		},
		"GET " + snapshotPath: func(w http.ResponseWriter, r *http.Request) {
			snapshotCalled = true
			jsonResponse(w, http.StatusOK, []Image{})
		},
	})

	image, err := client.GetImageByName("Ubuntu 22.04")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if image.ID != "pub-1" {
		t.Errorf("expected pub-1, got %s", image.ID)
	}

	if snapshotCalled {
		t.Error("snapshot endpoint should NOT be called when public match found (perf regression)")
	}
}

func TestGetImageByName_NotFoundAnywhere(t *testing.T) {
	imagePath := fmt.Sprintf("/cloud/project/%s/image", testServiceName)
	snapshotPath := fmt.Sprintf("/cloud/project/%s/snapshot", testServiceName)

	_, client := newTestServer(t, map[string]http.HandlerFunc{
		"GET " + imagePath: func(w http.ResponseWriter, r *http.Request) {
			jsonResponse(w, http.StatusOK, []Image{})
		},
		"GET " + snapshotPath: func(w http.ResponseWriter, r *http.Request) {
			jsonResponse(w, http.StatusOK, []Image{})
		},
	})

	_, err := client.GetImageByName("nonexistent")
	if err == nil {
		t.Fatal("expected error when image is in neither endpoint")
	}

	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

func TestIsUUID(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"865193d1-cd97-445c-ade9-ac9981fd1cbe", true},
		{"00000000-0000-0000-0000-000000000000", true},
		{"865193D1-CD97-445C-ADE9-AC9981FD1CBE", true}, // uppercase
		{"Ubuntu 22.04", false},
		{"openSUSE-Leap-15.6", false},
		{"865193d1-cd97-445c-ade9-ac9981fd1cb", false},   // too short
		{"865193d1-cd97-445c-ade9-ac9981fd1cbex", false}, // too long
		{"865193d1cd97445cade9ac9981fd1cbe", false},      // no dashes
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := isUUID(tt.in); got != tt.want {
				t.Errorf("isUUID(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestGetLBFlavorByName(t *testing.T) {
	expectedPath := fmt.Sprintf("/cloud/project/%s/region/%s/loadbalancing/flavor",
		testServiceName, testRegion)

	_, client := newTestServer(t, map[string]http.HandlerFunc{
		"GET " + expectedPath: func(w http.ResponseWriter, r *http.Request) {
			jsonResponse(w, http.StatusOK, []LBFlavor{
				{ID: "fl-1", Name: "small", Region: testRegion},
				{ID: "fl-2", Name: "medium", Region: testRegion},
				{ID: "fl-3", Name: "large", Region: testRegion},
				{ID: "fl-4", Name: "xl", Region: testRegion},
			})
		},
	})

	flavor, err := client.GetLBFlavorByName("medium")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if flavor.ID != "fl-2" {
		t.Errorf("expected fl-2, got %s", flavor.ID)
	}
}

func TestGetLBFlavorByName_NotFound(t *testing.T) {
	expectedPath := fmt.Sprintf("/cloud/project/%s/region/%s/loadbalancing/flavor",
		testServiceName, testRegion)

	_, client := newTestServer(t, map[string]http.HandlerFunc{
		"GET " + expectedPath: func(w http.ResponseWriter, r *http.Request) {
			jsonResponse(w, http.StatusOK, []LBFlavor{
				{ID: "fl-1", Name: "small"},
			})
		},
	})

	_, err := client.GetLBFlavorByName("nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown flavor")
	}
}

func TestCreateLoadBalancer(t *testing.T) {
	// Flow: GET (idempotency check, no LB found) -> POST -> GET (poll until LB found).
	expectedPath := fmt.Sprintf("/cloud/project/%s/region/%s/loadbalancing/loadbalancer",
		testServiceName, testRegion)

	postCalled := false
	getCallCount := 0

	_, client := newTestServer(t, map[string]http.HandlerFunc{
		"POST " + expectedPath: func(w http.ResponseWriter, r *http.Request) {
			postCalled = true
			jsonResponse(w, http.StatusOK, map[string]interface{}{
				"id":     "task-xyz-123",
				"status": "in-progress",
			})
		},
		"GET " + expectedPath: func(w http.ResponseWriter, r *http.Request) {
			getCallCount++

			// First call (idempotency check): empty list, force POST
			if getCallCount == 1 {
				jsonResponse(w, http.StatusOK, []LoadBalancer{})
				return
			}

			// Subsequent calls (post-POST poll): return the LB
			jsonResponse(w, http.StatusOK, []LoadBalancer{
				{
					ID:                 "lb-real-456",
					Name:               "capi-lb",
					ProvisioningStatus: LBProvisioningStatusActive,
					OperatingStatus:    LBOperatingStatusOnline,
					VIPAddress:         "10.0.0.100",
				},
			})
		},
	})

	lb, err := client.CreateLoadBalancer(CreateLoadBalancerOpts{
		Name:     "capi-lb",
		FlavorID: "fl-small",
		Network: LBNetworkConfig{
			Private: LBPrivateNetwork{
				Network: LBNetworkRef{ID: "openstack-uuid", SubnetID: "subnet-1"},
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !postCalled {
		t.Error("expected POST to be called when no LB exists")
	}

	if getCallCount < 2 {
		t.Errorf("expected at least 2 GETs (idempotency + poll), got %d", getCallCount)
	}

	if lb.ID != "lb-real-456" {
		t.Errorf("expected LB id from list, got %s", lb.ID)
	}
}

func TestCreateLoadBalancer_Idempotent(t *testing.T) {
	// If an LB with the same name already exists, CreateLoadBalancer must
	// return it without POSTing (idempotent).
	expectedPath := fmt.Sprintf("/cloud/project/%s/region/%s/loadbalancing/loadbalancer",
		testServiceName, testRegion)

	postCalled := false

	_, client := newTestServer(t, map[string]http.HandlerFunc{
		"POST " + expectedPath: func(w http.ResponseWriter, r *http.Request) {
			postCalled = true
			t.Error("POST must not be called when LB already exists")
		},
		"GET " + expectedPath: func(w http.ResponseWriter, r *http.Request) {
			jsonResponse(w, http.StatusOK, []LoadBalancer{
				{
					ID:                 "lb-existing",
					Name:               "capi-lb",
					ProvisioningStatus: LBProvisioningStatusActive,
					VIPAddress:         "10.0.0.100",
				},
			})
		},
	})

	lb, err := client.CreateLoadBalancer(CreateLoadBalancerOpts{
		Name:     "capi-lb",
		FlavorID: "fl-small",
		Network: LBNetworkConfig{
			Private: LBPrivateNetwork{Network: LBNetworkRef{ID: "openstack-uuid", SubnetID: "subnet-1"}},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if postCalled {
		t.Error("idempotency violated: POST was called even though LB exists")
	}

	if lb.ID != "lb-existing" {
		t.Errorf("expected existing LB, got %s", lb.ID)
	}
}

func TestAddPoolMembers_Batch(t *testing.T) {
	poolID := "pool-1"
	expectedPath := fmt.Sprintf("/cloud/project/%s/region/%s/loadbalancing/pool/%s/member",
		testServiceName, testRegion, poolID)

	_, client := newTestServer(t, map[string]http.HandlerFunc{
		"POST " + expectedPath: func(w http.ResponseWriter, r *http.Request) {
			var req addPoolMembersRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("decode error: %v", err)
			}

			if len(req.Members) != 2 {
				t.Errorf("expected 2 members in batch, got %d", len(req.Members))
			}

			jsonResponse(w, http.StatusOK, []Member{
				{ID: "m-1", Address: req.Members[0].Address, ProtocolPort: req.Members[0].ProtocolPort},
				{ID: "m-2", Address: req.Members[1].Address, ProtocolPort: req.Members[1].ProtocolPort},
			})
		},
	})

	members, err := client.AddPoolMembers(poolID, []CreateMemberOpts{
		{Address: "10.0.0.10", ProtocolPort: 6443},
		{Address: "10.0.0.11", ProtocolPort: 6443},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(members) != 2 {
		t.Fatalf("expected 2 members, got %d", len(members))
	}

	if members[0].ID != "m-1" || members[1].ID != "m-2" {
		t.Errorf("unexpected member IDs: %v", members)
	}
}

func TestAddPoolMember_Single(t *testing.T) {
	poolID := "pool-1"
	expectedPath := fmt.Sprintf("/cloud/project/%s/region/%s/loadbalancing/pool/%s/member",
		testServiceName, testRegion, poolID)

	_, client := newTestServer(t, map[string]http.HandlerFunc{
		"POST " + expectedPath: func(w http.ResponseWriter, r *http.Request) {
			jsonResponse(w, http.StatusOK, []Member{
				{ID: "m-1", Address: "10.0.0.10", ProtocolPort: 6443},
			})
		},
	})

	m, err := client.AddPoolMember(poolID, CreateMemberOpts{
		Address:      "10.0.0.10",
		ProtocolPort: 6443,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if m.ID != "m-1" {
		t.Errorf("expected m-1, got %s", m.ID)
	}
}

func TestCreateSSHKey(t *testing.T) {
	expectedPath := fmt.Sprintf("/cloud/project/%s/sshkey", testServiceName)

	_, client := newTestServer(t, map[string]http.HandlerFunc{
		"POST " + expectedPath: func(w http.ResponseWriter, r *http.Request) {
			var opts CreateSSHKeyOpts
			json.NewDecoder(r.Body).Decode(&opts) //nolint:errcheck

			if opts.Name != "test-key" {
				t.Errorf("expected name test-key, got %s", opts.Name)
			}

			jsonResponse(w, http.StatusOK, SSHKey{
				ID:        "key-abc-123",
				Name:      opts.Name,
				PublicKey: opts.PublicKey,
			})
		},
	})

	key, err := client.CreateSSHKey(CreateSSHKeyOpts{
		Name:      "test-key",
		PublicKey: "ssh-ed25519 AAAA...",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if key.ID != "key-abc-123" {
		t.Errorf("expected ID key-abc-123, got %s", key.ID)
	}
}

func TestDeleteSSHKey(t *testing.T) {
	keyID := "key-abc-123"
	expectedPath := fmt.Sprintf("/cloud/project/%s/sshkey/%s", testServiceName, keyID)

	_, client := newTestServer(t, map[string]http.HandlerFunc{
		"DELETE " + expectedPath: func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		},
	})

	err := client.DeleteSSHKey(keyID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeleteSSHKey_NotFound(t *testing.T) {
	keyID := "key-gone"
	expectedPath := fmt.Sprintf("/cloud/project/%s/sshkey/%s", testServiceName, keyID)

	_, client := newTestServer(t, map[string]http.HandlerFunc{
		"DELETE " + expectedPath: func(w http.ResponseWriter, r *http.Request) {
			jsonResponse(w, http.StatusNotFound, map[string]string{"message": "not found"})
		},
	})

	err := client.DeleteSSHKey(keyID)
	if err != nil {
		t.Fatalf("expected nil error for 404, got: %v", err)
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
