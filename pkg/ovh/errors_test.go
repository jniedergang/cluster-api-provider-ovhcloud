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
	"fmt"
	"testing"

	goovh "github.com/ovh/go-ovh/ovh"
)

func TestIsNotFound(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil error", nil, false},
		{"OVH 404", &goovh.APIError{Code: 404, Message: "not found"}, true},
		{"OVH 200", &goovh.APIError{Code: 200, Message: "ok"}, false},
		{"generic not found", fmt.Errorf("resource not found"), true},
		{"generic error", fmt.Errorf("something went wrong"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsNotFound(tt.err); got != tt.expected {
				t.Errorf("IsNotFound() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestIsRetryable(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil error", nil, false},
		{"OVH 429", &goovh.APIError{Code: 429, Message: "rate limited"}, true},
		{"OVH 500", &goovh.APIError{Code: 500, Message: "internal"}, true},
		{"OVH 502", &goovh.APIError{Code: 502, Message: "bad gateway"}, true},
		{"OVH 503", &goovh.APIError{Code: 503, Message: "unavailable"}, true},
		{"OVH 504", &goovh.APIError{Code: 504, Message: "timeout"}, true},
		{"OVH 400", &goovh.APIError{Code: 400, Message: "bad request"}, false},
		{"OVH 404", &goovh.APIError{Code: 404, Message: "not found"}, false},
		{"connection refused", fmt.Errorf("connection refused"), true},
		{"timeout", fmt.Errorf("context timeout exceeded"), true},
		{"generic error", fmt.Errorf("invalid input"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsRetryable(tt.err); got != tt.expected {
				t.Errorf("IsRetryable() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestIsForbidden(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil", nil, false},
		{"OVH 403", &goovh.APIError{Code: 403, Message: "forbidden"}, true},
		{"OVH 401", &goovh.APIError{Code: 401, Message: "unauthorized"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsForbidden(tt.err); got != tt.expected {
				t.Errorf("IsForbidden() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestIsUnauthorized(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil", nil, false},
		{"OVH 401", &goovh.APIError{Code: 401, Message: "unauthorized"}, true},
		{"OVH 403", &goovh.APIError{Code: 403, Message: "forbidden"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsUnauthorized(tt.err); got != tt.expected {
				t.Errorf("IsUnauthorized() = %v, want %v", got, tt.expected)
			}
		})
	}
}
