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
	"errors"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/conversion"

	corev1 "k8s.io/api/core/v1"

	infrav1alpha2 "github.com/rancher-sandbox/cluster-api-provider-ovhcloud/api/v1alpha2"
)

// ConvertTo converts this OVHCluster (v1alpha1) to the Hub version (v1alpha2).
func (src *OVHCluster) ConvertTo(dstRaw conversion.Hub) error {
	dst, ok := dstRaw.(*infrav1alpha2.OVHCluster)
	if !ok {
		return fmt.Errorf("expected *v1alpha2.OVHCluster, got %T", dstRaw)
	}

	dst.ObjectMeta = src.ObjectMeta

	// Spec — copy all fields, converting Protocol on listeners.
	dst.Spec.ServiceName = src.Spec.ServiceName
	dst.Spec.Region = src.Spec.Region
	dst.Spec.IdentitySecret = infrav1alpha2.SecretKey(src.Spec.IdentitySecret)
	dst.Spec.ControlPlaneEndpoint = src.Spec.ControlPlaneEndpoint
	dst.Spec.SSHKeyName = src.Spec.SSHKeyName

	// LoadBalancerConfig
	dst.Spec.LoadBalancerConfig.SubnetID = src.Spec.LoadBalancerConfig.SubnetID
	dst.Spec.LoadBalancerConfig.FlavorName = src.Spec.LoadBalancerConfig.FlavorName
	dst.Spec.LoadBalancerConfig.FloatingNetworkID = src.Spec.LoadBalancerConfig.FloatingNetworkID

	if len(src.Spec.LoadBalancerConfig.Listeners) > 0 {
		dst.Spec.LoadBalancerConfig.Listeners = make([]infrav1alpha2.OVHListener, len(src.Spec.LoadBalancerConfig.Listeners))

		for i, l := range src.Spec.LoadBalancerConfig.Listeners {
			proto, err := convertProtocolToV1Alpha2(l.Protocol)
			if err != nil {
				return fmt.Errorf("listener %q: %w", l.Name, err)
			}

			dst.Spec.LoadBalancerConfig.Listeners[i] = infrav1alpha2.OVHListener{
				Name:        l.Name,
				Port:        l.Port,
				Protocol:    proto,
				BackendPort: l.BackendPort,
			}
		}
	}

	// NetworkConfig
	if src.Spec.NetworkConfig != nil {
		nc := &infrav1alpha2.OVHNetworkConfig{
			PrivateNetworkID: src.Spec.NetworkConfig.PrivateNetworkID,
			SubnetCIDR:       src.Spec.NetworkConfig.SubnetCIDR,
			VlanID:           src.Spec.NetworkConfig.VlanID,
			Gateway:          src.Spec.NetworkConfig.Gateway,
		}
		nc.DNSServers = append(nc.DNSServers, src.Spec.NetworkConfig.DNSServers...)
		dst.Spec.NetworkConfig = nc
	}

	// Status — identical fields, direct copy.
	dst.Status = infrav1alpha2.OVHClusterStatus(src.Status)

	return nil
}

// ConvertFrom converts from the Hub version (v1alpha2) to this OVHCluster (v1alpha1).
func (dst *OVHCluster) ConvertFrom(srcRaw conversion.Hub) error {
	src, ok := srcRaw.(*infrav1alpha2.OVHCluster)
	if !ok {
		return fmt.Errorf("expected *v1alpha2.OVHCluster, got %T", srcRaw)
	}

	dst.ObjectMeta = src.ObjectMeta

	// Spec
	dst.Spec.ServiceName = src.Spec.ServiceName
	dst.Spec.Region = src.Spec.Region
	dst.Spec.IdentitySecret = SecretKey(src.Spec.IdentitySecret)
	dst.Spec.ControlPlaneEndpoint = src.Spec.ControlPlaneEndpoint
	dst.Spec.SSHKeyName = src.Spec.SSHKeyName

	// LoadBalancerConfig
	dst.Spec.LoadBalancerConfig.SubnetID = src.Spec.LoadBalancerConfig.SubnetID
	dst.Spec.LoadBalancerConfig.FlavorName = src.Spec.LoadBalancerConfig.FlavorName
	dst.Spec.LoadBalancerConfig.FloatingNetworkID = src.Spec.LoadBalancerConfig.FloatingNetworkID

	if len(src.Spec.LoadBalancerConfig.Listeners) > 0 {
		dst.Spec.LoadBalancerConfig.Listeners = make([]OVHListener, len(src.Spec.LoadBalancerConfig.Listeners))

		for i, l := range src.Spec.LoadBalancerConfig.Listeners {
			dst.Spec.LoadBalancerConfig.Listeners[i] = OVHListener{
				Name:        l.Name,
				Port:        l.Port,
				Protocol:    string(l.Protocol),
				BackendPort: l.BackendPort,
			}
		}
	}

	// NetworkConfig
	if src.Spec.NetworkConfig != nil {
		nc := &OVHNetworkConfig{
			PrivateNetworkID: src.Spec.NetworkConfig.PrivateNetworkID,
			SubnetCIDR:       src.Spec.NetworkConfig.SubnetCIDR,
			VlanID:           src.Spec.NetworkConfig.VlanID,
			Gateway:          src.Spec.NetworkConfig.Gateway,
		}
		nc.DNSServers = append(nc.DNSServers, src.Spec.NetworkConfig.DNSServers...)
		dst.Spec.NetworkConfig = nc
	}

	// Status
	dst.Status = OVHClusterStatus(src.Status)

	return nil
}

// convertProtocolToV1Alpha2 converts v1alpha1 Protocol string to corev1.Protocol.
// TCP maps directly; HTTP is deprecated and rejected.
func convertProtocolToV1Alpha2(p string) (corev1.Protocol, error) {
	switch p {
	case "TCP":
		return corev1.ProtocolTCP, nil
	case "UDP":
		return corev1.ProtocolUDP, nil
	case "SCTP":
		return corev1.ProtocolSCTP, nil
	case "HTTP":
		return "", errors.New("protocol HTTP is deprecated in v1alpha2; use TCP with an HTTP health check instead")
	default:
		return "", fmt.Errorf("unknown protocol %q", p)
	}
}

// ConvertTo converts this OVHClusterList (v1alpha1) to the Hub version (v1alpha2).
func (src *OVHClusterList) ConvertTo(dstRaw conversion.Hub) error {
	dst, ok := dstRaw.(*infrav1alpha2.OVHClusterList)
	if !ok {
		return fmt.Errorf("expected *v1alpha2.OVHClusterList, got %T", dstRaw)
	}

	dst.ListMeta = src.ListMeta
	dst.Items = make([]infrav1alpha2.OVHCluster, len(src.Items))

	for i := range src.Items {
		if err := src.Items[i].ConvertTo(&dst.Items[i]); err != nil {
			return err
		}
	}

	return nil
}

// ConvertFrom converts from the Hub version (v1alpha2) to this OVHClusterList (v1alpha1).
func (dst *OVHClusterList) ConvertFrom(srcRaw conversion.Hub) error {
	src, ok := srcRaw.(*infrav1alpha2.OVHClusterList)
	if !ok {
		return fmt.Errorf("expected *v1alpha2.OVHClusterList, got %T", srcRaw)
	}

	dst.ListMeta = src.ListMeta
	dst.Items = make([]OVHCluster, len(src.Items))

	for i := range src.Items {
		if err := dst.Items[i].ConvertFrom(&src.Items[i]); err != nil {
			return err
		}
	}

	return nil
}

// ConvertTo converts this OVHMachine (v1alpha1) to the Hub version (v1alpha2).
func (src *OVHMachine) ConvertTo(dstRaw conversion.Hub) error {
	dst, ok := dstRaw.(*infrav1alpha2.OVHMachine)
	if !ok {
		return fmt.Errorf("expected *v1alpha2.OVHMachine, got %T", dstRaw)
	}

	dst.ObjectMeta = src.ObjectMeta
	convertMachineSpecTo(&src.Spec, &dst.Spec)
	convertMachineStatusTo(&src.Status, &dst.Status)

	return nil
}

// ConvertFrom converts from the Hub version (v1alpha2) to this OVHMachine (v1alpha1).
func (dst *OVHMachine) ConvertFrom(srcRaw conversion.Hub) error {
	src, ok := srcRaw.(*infrav1alpha2.OVHMachine)
	if !ok {
		return fmt.Errorf("expected *v1alpha2.OVHMachine, got %T", srcRaw)
	}

	dst.ObjectMeta = src.ObjectMeta
	convertMachineSpecFrom(&src.Spec, &dst.Spec)
	convertMachineStatusFrom(&src.Status, &dst.Status)

	return nil
}

func convertMachineSpecTo(src *OVHMachineSpec, dst *infrav1alpha2.OVHMachineSpec) {
	dst.ProviderID = src.ProviderID
	dst.FlavorName = src.FlavorName
	dst.ImageName = src.ImageName
	dst.SSHKeyName = src.SSHKeyName
	dst.RootDiskSize = src.RootDiskSize
	dst.FailureDomain = src.FailureDomain

	if len(src.AdditionalVolumes) > 0 {
		dst.AdditionalVolumes = make([]infrav1alpha2.OVHVolume, len(src.AdditionalVolumes))
		for i, v := range src.AdditionalVolumes {
			dst.AdditionalVolumes[i] = infrav1alpha2.OVHVolume{Name: v.Name, SizeGB: v.SizeGB, Type: v.Type}
		}
	}
}

func convertMachineSpecFrom(src *infrav1alpha2.OVHMachineSpec, dst *OVHMachineSpec) {
	dst.ProviderID = src.ProviderID
	dst.FlavorName = src.FlavorName
	dst.ImageName = src.ImageName
	dst.SSHKeyName = src.SSHKeyName
	dst.RootDiskSize = src.RootDiskSize
	dst.FailureDomain = src.FailureDomain

	if len(src.AdditionalVolumes) > 0 {
		dst.AdditionalVolumes = make([]OVHVolume, len(src.AdditionalVolumes))
		for i, v := range src.AdditionalVolumes {
			dst.AdditionalVolumes[i] = OVHVolume{Name: v.Name, SizeGB: v.SizeGB, Type: v.Type}
		}
	}
}

func convertMachineStatusTo(src *OVHMachineStatus, dst *infrav1alpha2.OVHMachineStatus) {
	dst.Ready = src.Ready
	dst.Conditions = src.Conditions
	dst.FailureReason = src.FailureReason
	dst.FailureMessage = src.FailureMessage
	dst.Addresses = src.Addresses
	dst.Initialization = infrav1alpha2.Initialization{Provisioned: src.Initialization.Provisioned}
	dst.InstanceID = src.InstanceID
	dst.VolumeIDs = src.VolumeIDs
	dst.LBPoolMemberID = src.LBPoolMemberID
	dst.RegisterPoolMemberID = src.RegisterPoolMemberID
}

func convertMachineStatusFrom(src *infrav1alpha2.OVHMachineStatus, dst *OVHMachineStatus) {
	dst.Ready = src.Ready
	dst.Conditions = src.Conditions
	dst.FailureReason = src.FailureReason
	dst.FailureMessage = src.FailureMessage
	dst.Addresses = src.Addresses
	dst.Initialization = Initialization{Provisioned: src.Initialization.Provisioned}
	dst.InstanceID = src.InstanceID
	dst.VolumeIDs = src.VolumeIDs
	dst.LBPoolMemberID = src.LBPoolMemberID
	dst.RegisterPoolMemberID = src.RegisterPoolMemberID
}

// ConvertTo converts this OVHMachineList (v1alpha1) to the Hub version (v1alpha2).
func (src *OVHMachineList) ConvertTo(dstRaw conversion.Hub) error {
	dst, ok := dstRaw.(*infrav1alpha2.OVHMachineList)
	if !ok {
		return fmt.Errorf("expected *v1alpha2.OVHMachineList, got %T", dstRaw)
	}

	dst.ListMeta = src.ListMeta
	dst.Items = make([]infrav1alpha2.OVHMachine, len(src.Items))

	for i := range src.Items {
		if err := src.Items[i].ConvertTo(&dst.Items[i]); err != nil {
			return err
		}
	}

	return nil
}

// ConvertFrom converts from the Hub version (v1alpha2) to this OVHMachineList (v1alpha1).
func (dst *OVHMachineList) ConvertFrom(srcRaw conversion.Hub) error {
	src, ok := srcRaw.(*infrav1alpha2.OVHMachineList)
	if !ok {
		return fmt.Errorf("expected *v1alpha2.OVHMachineList, got %T", srcRaw)
	}

	dst.ListMeta = src.ListMeta
	dst.Items = make([]OVHMachine, len(src.Items))

	for i := range src.Items {
		if err := dst.Items[i].ConvertFrom(&src.Items[i]); err != nil {
			return err
		}
	}

	return nil
}

// ConvertTo converts this OVHClusterTemplate (v1alpha1) to the Hub version (v1alpha2).
func (src *OVHClusterTemplate) ConvertTo(dstRaw conversion.Hub) error {
	dst, ok := dstRaw.(*infrav1alpha2.OVHClusterTemplate)
	if !ok {
		return fmt.Errorf("expected *v1alpha2.OVHClusterTemplate, got %T", dstRaw)
	}

	dst.ObjectMeta = src.ObjectMeta

	// Convert the inner OVHClusterSpec via a temporary OVHCluster.
	tmpSrc := &OVHCluster{Spec: src.Spec.Template.Spec}
	tmpDst := &infrav1alpha2.OVHCluster{}

	if err := tmpSrc.ConvertTo(tmpDst); err != nil {
		return err
	}

	dst.Spec.Template.ObjectMeta = src.Spec.Template.ObjectMeta
	dst.Spec.Template.Spec = tmpDst.Spec

	return nil
}

// ConvertFrom converts from the Hub version (v1alpha2) to this OVHClusterTemplate (v1alpha1).
func (dst *OVHClusterTemplate) ConvertFrom(srcRaw conversion.Hub) error {
	src, ok := srcRaw.(*infrav1alpha2.OVHClusterTemplate)
	if !ok {
		return fmt.Errorf("expected *v1alpha2.OVHClusterTemplate, got %T", srcRaw)
	}

	dst.ObjectMeta = src.ObjectMeta

	tmpSrc := &infrav1alpha2.OVHCluster{Spec: src.Spec.Template.Spec}
	tmpDst := &OVHCluster{}

	if err := tmpDst.ConvertFrom(tmpSrc); err != nil {
		return err
	}

	dst.Spec.Template.ObjectMeta = src.Spec.Template.ObjectMeta
	dst.Spec.Template.Spec = tmpDst.Spec

	return nil
}

// ConvertTo converts this OVHClusterTemplateList (v1alpha1) to the Hub version (v1alpha2).
func (src *OVHClusterTemplateList) ConvertTo(dstRaw conversion.Hub) error {
	dst, ok := dstRaw.(*infrav1alpha2.OVHClusterTemplateList)
	if !ok {
		return fmt.Errorf("expected *v1alpha2.OVHClusterTemplateList, got %T", dstRaw)
	}

	dst.ListMeta = src.ListMeta
	dst.Items = make([]infrav1alpha2.OVHClusterTemplate, len(src.Items))

	for i := range src.Items {
		if err := src.Items[i].ConvertTo(&dst.Items[i]); err != nil {
			return err
		}
	}

	return nil
}

// ConvertFrom converts from the Hub version (v1alpha2) to this OVHClusterTemplateList (v1alpha1).
func (dst *OVHClusterTemplateList) ConvertFrom(srcRaw conversion.Hub) error {
	src, ok := srcRaw.(*infrav1alpha2.OVHClusterTemplateList)
	if !ok {
		return fmt.Errorf("expected *v1alpha2.OVHClusterTemplateList, got %T", srcRaw)
	}

	dst.ListMeta = src.ListMeta
	dst.Items = make([]OVHClusterTemplate, len(src.Items))

	for i := range src.Items {
		if err := dst.Items[i].ConvertFrom(&src.Items[i]); err != nil {
			return err
		}
	}

	return nil
}

// ConvertTo converts this OVHMachineTemplate (v1alpha1) to the Hub version (v1alpha2).
func (src *OVHMachineTemplate) ConvertTo(dstRaw conversion.Hub) error {
	dst, ok := dstRaw.(*infrav1alpha2.OVHMachineTemplate)
	if !ok {
		return fmt.Errorf("expected *v1alpha2.OVHMachineTemplate, got %T", dstRaw)
	}

	dst.ObjectMeta = src.ObjectMeta
	dst.Spec.Template.ObjectMeta = src.Spec.Template.ObjectMeta
	convertMachineSpecTo(&src.Spec.Template.Spec, &dst.Spec.Template.Spec)

	return nil
}

// ConvertFrom converts from the Hub version (v1alpha2) to this OVHMachineTemplate (v1alpha1).
func (dst *OVHMachineTemplate) ConvertFrom(srcRaw conversion.Hub) error {
	src, ok := srcRaw.(*infrav1alpha2.OVHMachineTemplate)
	if !ok {
		return fmt.Errorf("expected *v1alpha2.OVHMachineTemplate, got %T", srcRaw)
	}

	dst.ObjectMeta = src.ObjectMeta
	dst.Spec.Template.ObjectMeta = src.Spec.Template.ObjectMeta
	convertMachineSpecFrom(&src.Spec.Template.Spec, &dst.Spec.Template.Spec)

	return nil
}

// ConvertTo converts this OVHMachineTemplateList (v1alpha1) to the Hub version (v1alpha2).
func (src *OVHMachineTemplateList) ConvertTo(dstRaw conversion.Hub) error {
	dst, ok := dstRaw.(*infrav1alpha2.OVHMachineTemplateList)
	if !ok {
		return fmt.Errorf("expected *v1alpha2.OVHMachineTemplateList, got %T", dstRaw)
	}

	dst.ListMeta = src.ListMeta
	dst.Items = make([]infrav1alpha2.OVHMachineTemplate, len(src.Items))

	for i := range src.Items {
		if err := src.Items[i].ConvertTo(&dst.Items[i]); err != nil {
			return err
		}
	}

	return nil
}

// ConvertFrom converts from the Hub version (v1alpha2) to this OVHMachineTemplateList (v1alpha1).
func (dst *OVHMachineTemplateList) ConvertFrom(srcRaw conversion.Hub) error {
	src, ok := srcRaw.(*infrav1alpha2.OVHMachineTemplateList)
	if !ok {
		return fmt.Errorf("expected *v1alpha2.OVHMachineTemplateList, got %T", srcRaw)
	}

	dst.ListMeta = src.ListMeta
	dst.Items = make([]OVHMachineTemplate, len(src.Items))

	for i := range src.Items {
		if err := dst.Items[i].ConvertFrom(&src.Items[i]); err != nil {
			return err
		}
	}

	return nil
}
