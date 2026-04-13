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
	"encoding/base64"
	"errors"
)

// PrepareUserData reads the bootstrap data secret value and prepares it
// for use as OVH instance userData. OVH expects the userData to be a
// base64-encoded cloud-init script passed in the instance creation request.
//
// The bootstrap data from CAPI is already the cloud-init content (not base64).
// OVH API may accept it as-is or base64-encoded depending on the endpoint version.
func PrepareUserData(bootstrapData []byte) (string, error) {
	if len(bootstrapData) == 0 {
		return "", errors.New("bootstrap data is empty")
	}

	// OVH instance creation API accepts base64-encoded userData
	return base64.StdEncoding.EncodeToString(bootstrapData), nil
}
