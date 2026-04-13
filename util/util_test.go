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

package util

import (
	"strings"
	"testing"
)

func TestGenerateRFC1035Name(t *testing.T) {
	tests := []struct {
		name     string
		parts    []string
		expected string
	}{
		{
			name:     "simple join",
			parts:    []string{"capi", "test", "vm"},
			expected: "capi-test-vm",
		},
		{
			name:     "uppercase converted",
			parts:    []string{"CAPI", "MyCluster"},
			expected: "capi-mycluster",
		},
		{
			name:     "special chars replaced",
			parts:    []string{"my_cluster", "node.1"},
			expected: "my-cluster-node-1",
		},
		{
			name:     "leading/trailing hyphens trimmed",
			parts:    []string{"-test-", "name-"},
			expected: "test--name",
		},
		{
			name:  "truncated at 63 chars",
			parts: []string{"this-is-a-very-long-cluster-name-that-exceeds-sixty-three-characters-total"},
			expected: func() string {
				s := "this-is-a-very-long-cluster-name-that-exceeds-sixty-three-chara"
				if len(s) > 63 {
					s = s[:63]
				}

				return strings.TrimRight(s, "-")
			}(),
		},
		{
			name:     "empty parts",
			parts:    []string{"", "test", ""},
			expected: "test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GenerateRFC1035Name(tt.parts...)
			if got != tt.expected {
				t.Errorf("GenerateRFC1035Name(%v) = %q, want %q", tt.parts, got, tt.expected)
			}

			if len(got) > 63 {
				t.Errorf("GenerateRFC1035Name(%v) = %q (len %d), exceeds 63 chars", tt.parts, got, len(got))
			}
		})
	}
}

func TestProviderIDFromInstance(t *testing.T) {
	tests := []struct {
		region     string
		instanceID string
		expected   string
	}{
		{"GRA7", "inst-abc-123", "ovhcloud://GRA7/inst-abc-123"},
		{"SBG5", "uuid-456", "ovhcloud://SBG5/uuid-456"},
	}

	for _, tt := range tests {
		t.Run(tt.region+"/"+tt.instanceID, func(t *testing.T) {
			got := ProviderIDFromInstance(tt.region, tt.instanceID)
			if got != tt.expected {
				t.Errorf("ProviderIDFromInstance(%q, %q) = %q, want %q", tt.region, tt.instanceID, got, tt.expected)
			}
		})
	}
}

func TestParseProviderID(t *testing.T) {
	tests := []struct {
		name       string
		providerID string
		region     string
		instanceID string
		wantErr    bool
	}{
		{
			name:       "valid",
			providerID: "ovhcloud://GRA7/inst-abc-123",
			region:     "GRA7",
			instanceID: "inst-abc-123",
		},
		{
			name:       "valid with UUID",
			providerID: "ovhcloud://SBG5/550e8400-e29b-41d4-a716-446655440000",
			region:     "SBG5",
			instanceID: "550e8400-e29b-41d4-a716-446655440000",
		},
		{
			name:       "wrong prefix",
			providerID: "aws://us-east-1/i-123",
			wantErr:    true,
		},
		{
			name:       "missing instance ID",
			providerID: "ovhcloud://GRA7/",
			wantErr:    true,
		},
		{
			name:       "missing region",
			providerID: "ovhcloud:///inst-123",
			wantErr:    true,
		},
		{
			name:       "empty string",
			providerID: "",
			wantErr:    true,
		},
		{
			name:       "no slashes after prefix",
			providerID: "ovhcloud://onlyone",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			region, instanceID, err := ParseProviderID(tt.providerID)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseProviderID(%q) expected error, got nil", tt.providerID)
				}

				return
			}

			if err != nil {
				t.Fatalf("ParseProviderID(%q) unexpected error: %v", tt.providerID, err)
			}

			if region != tt.region {
				t.Errorf("region = %q, want %q", region, tt.region)
			}

			if instanceID != tt.instanceID {
				t.Errorf("instanceID = %q, want %q", instanceID, tt.instanceID)
			}
		})
	}
}

func TestParseProviderID_Roundtrip(t *testing.T) {
	// Verify that ProviderIDFromInstance and ParseProviderID are inverses
	region := "GRA7"
	instanceID := "inst-abc-123"

	providerID := ProviderIDFromInstance(region, instanceID)

	gotRegion, gotInstanceID, err := ParseProviderID(providerID)
	if err != nil {
		t.Fatalf("roundtrip failed: %v", err)
	}

	if gotRegion != region {
		t.Errorf("roundtrip region = %q, want %q", gotRegion, region)
	}

	if gotInstanceID != instanceID {
		t.Errorf("roundtrip instanceID = %q, want %q", gotInstanceID, instanceID)
	}
}

func TestGetSSHKeyName(t *testing.T) {
	tests := []struct {
		name       string
		machineKey string
		clusterKey string
		expected   string
	}{
		{"machine key takes precedence", "machine-key", "cluster-key", "machine-key"},
		{"fallback to cluster key", "", "cluster-key", "cluster-key"},
		{"both empty", "", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetSSHKeyName(tt.machineKey, tt.clusterKey)
			if got != tt.expected {
				t.Errorf("GetSSHKeyName(%q, %q) = %q, want %q", tt.machineKey, tt.clusterKey, got, tt.expected)
			}
		})
	}
}
