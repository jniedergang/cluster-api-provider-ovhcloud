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

package v1alpha1

import (
	"context"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestValidateOVHMachine_Valid(t *testing.T) {
	m := &OVHMachine{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: OVHMachineSpec{
			FlavorName: "b2-7",
			ImageName:  "Ubuntu 22.04",
		},
	}

	v := &OVHMachineValidator{}

	_, err := v.ValidateCreate(context.Background(), m)
	if err != nil {
		t.Errorf("expected valid machine, got error: %v", err)
	}
}

func TestValidateOVHMachine_MissingFlavor(t *testing.T) {
	m := &OVHMachine{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: OVHMachineSpec{
			ImageName: "Ubuntu 22.04",
		},
	}

	v := &OVHMachineValidator{}

	_, err := v.ValidateCreate(context.Background(), m)
	if err == nil {
		t.Fatal("expected error for missing flavorName")
	}

	if !strings.Contains(err.Error(), "spec.flavorName is required") {
		t.Errorf("expected flavorName error, got: %v", err)
	}
}

func TestValidateOVHMachine_MissingImage(t *testing.T) {
	m := &OVHMachine{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: OVHMachineSpec{
			FlavorName: "b2-7",
		},
	}

	v := &OVHMachineValidator{}

	_, err := v.ValidateCreate(context.Background(), m)
	if err == nil {
		t.Fatal("expected error for missing imageName")
	}

	if !strings.Contains(err.Error(), "spec.imageName is required") {
		t.Errorf("expected imageName error, got: %v", err)
	}
}

func TestValidateOVHMachine_InvalidVolume(t *testing.T) {
	m := &OVHMachine{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: OVHMachineSpec{
			FlavorName: "b2-7",
			ImageName:  "Ubuntu 22.04",
			AdditionalVolumes: []OVHVolume{
				{Name: "", SizeGB: 0},
			},
		},
	}

	v := &OVHMachineValidator{}

	_, err := v.ValidateCreate(context.Background(), m)
	if err == nil {
		t.Fatal("expected error for invalid volume")
	}

	if !strings.Contains(err.Error(), "name is required") {
		t.Errorf("expected volume name error, got: %v", err)
	}

	if !strings.Contains(err.Error(), "sizeGB must be >= 1") {
		t.Errorf("expected volume size error, got: %v", err)
	}
}

func TestValidateOVHMachine_ValidWithVolumes(t *testing.T) {
	m := &OVHMachine{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: OVHMachineSpec{
			FlavorName:   "c2-15",
			ImageName:    "Debian 12",
			RootDiskSize: 100,
			AdditionalVolumes: []OVHVolume{
				{Name: "data", SizeGB: 50, Type: "high-speed"},
				{Name: "logs", SizeGB: 20},
			},
		},
	}

	v := &OVHMachineValidator{}

	_, err := v.ValidateCreate(context.Background(), m)
	if err != nil {
		t.Errorf("expected valid machine with volumes, got error: %v", err)
	}
}

func TestValidateOVHMachine_Update(t *testing.T) {
	old := &OVHMachine{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec:       OVHMachineSpec{FlavorName: "b2-7", ImageName: "Ubuntu 22.04"},
	}

	new := &OVHMachine{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec:       OVHMachineSpec{FlavorName: "b2-15", ImageName: "Ubuntu 22.04"},
	}

	v := &OVHMachineValidator{}

	_, err := v.ValidateUpdate(context.Background(), old, new)
	if err != nil {
		t.Errorf("expected valid update, got error: %v", err)
	}
}

func TestValidateOVHMachine_Delete(t *testing.T) {
	v := &OVHMachineValidator{}

	_, err := v.ValidateDelete(context.Background(), &OVHMachine{})
	if err != nil {
		t.Errorf("delete should always succeed, got error: %v", err)
	}
}
