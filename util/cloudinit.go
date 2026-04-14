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
	"errors"
)

// PrepareUserData validates the bootstrap data from CAPI and returns it as a
// string suitable for use as OVH instance userData in the instance creation
// request.
//
// OVH's metadata service delivers userData verbatim to the instance, so we
// must send the raw cloud-init YAML (the bootstrap data from CAPI is already
// in that format). Base64-encoding here would double-wrap the content and
// cloud-init would fail with "Unhandled non-multipart userdata".
func PrepareUserData(bootstrapData []byte) (string, error) {
	if len(bootstrapData) == 0 {
		return "", errors.New("bootstrap data is empty")
	}

	return string(bootstrapData), nil
}
