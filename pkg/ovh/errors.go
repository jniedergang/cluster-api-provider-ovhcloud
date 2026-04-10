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
	"errors"
	"net/http"
	"strings"

	goovh "github.com/ovh/go-ovh/ovh"
)

// IsNotFound returns true if the error indicates a 404 Not Found response.
func IsNotFound(err error) bool {
	if err == nil {
		return false
	}

	var apiErr *goovh.APIError
	if errors.As(err, &apiErr) {
		return apiErr.Code == http.StatusNotFound
	}

	return strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "not found")
}

// IsConflict returns true if the error indicates a 409 Conflict response.
func IsConflict(err error) bool {
	if err == nil {
		return false
	}

	var apiErr *goovh.APIError
	if errors.As(err, &apiErr) {
		return apiErr.Code == http.StatusConflict
	}

	return false
}

// IsRetryable returns true if the error is transient and the operation should be retried.
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}

	var apiErr *goovh.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.Code {
		case http.StatusTooManyRequests, // 429
			http.StatusInternalServerError,     // 500
			http.StatusBadGateway,              // 502
			http.StatusServiceUnavailable,      // 503
			http.StatusGatewayTimeout:          // 504
			return true
		}

		return false
	}

	// Network errors are retryable
	msg := err.Error()

	return strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "temporary failure")
}

// IsForbidden returns true if the error indicates a 403 Forbidden response.
func IsForbidden(err error) bool {
	if err == nil {
		return false
	}

	var apiErr *goovh.APIError
	if errors.As(err, &apiErr) {
		return apiErr.Code == http.StatusForbidden
	}

	return false
}

// IsUnauthorized returns true if the error indicates a 401 Unauthorized response.
func IsUnauthorized(err error) bool {
	if err == nil {
		return false
	}

	var apiErr *goovh.APIError
	if errors.As(err, &apiErr) {
		return apiErr.Code == http.StatusUnauthorized
	}

	return false
}
