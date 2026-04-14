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

// Package ovh provides a client wrapper around the OVH Cloud API.
package ovh

// Instance represents an OVH Public Cloud compute instance.
type Instance struct {
	ID              string           `json:"id"`
	Name            string           `json:"name"`
	Status          string           `json:"status"` // ACTIVE, BUILD, REBOOT, ERROR, DELETED, etc.
	FlavorID        string           `json:"flavorId"`
	ImageID         string           `json:"imageId"`
	Region          string           `json:"region"`
	Created         string           `json:"created"`
	IPAddresses     []IPAddress      `json:"ipAddresses"`
	AttachedVolumes []AttachedVolume `json:"attachedVolumes,omitempty"`
	SSHKeyID        string           `json:"sshKeyId,omitempty"`
	MonthlyBilling  bool             `json:"monthlyBilling,omitempty"`
}

// Instance status constants.
const (
	InstanceStatusActive  = "ACTIVE"
	InstanceStatusBuild   = "BUILD"
	InstanceStatusReboot  = "REBOOT"
	InstanceStatusError   = "ERROR"
	InstanceStatusDeleted = "DELETED"
	InstanceStatusStopped = "STOPPED"
)

// IPAddress represents an IP address assigned to an instance.
type IPAddress struct {
	IP      string `json:"ip"`
	Type    string `json:"type"`    // "public" or "private"
	Version int    `json:"version"` // 4 or 6
}

// AttachedVolume represents a volume attached to an instance.
type AttachedVolume struct {
	ID string `json:"id"`
}

// CreateInstanceOpts are the parameters for creating a new instance.
type CreateInstanceOpts struct {
	Name           string            `json:"name"`
	FlavorID       string            `json:"flavorId"`
	ImageID        string            `json:"imageId"`
	Region         string            `json:"region"`
	SSHKeyID       string            `json:"sshKeyId,omitempty"`
	UserData       string            `json:"userData,omitempty"`
	Networks       []InstanceNetwork `json:"networks,omitempty"`
	MonthlyBilling bool              `json:"monthlyBilling,omitempty"`
}

// InstanceNetwork describes a network to attach during instance creation.
type InstanceNetwork struct {
	NetworkID string `json:"networkId"`
}

// Flavor represents an OVH instance flavor (size).
type Flavor struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	VCPUs     int    `json:"vcpus"`
	RAM       int    `json:"ram"`  // in MB
	Disk      int    `json:"disk"` // in GB
	Type      string `json:"type"`
	Region    string `json:"region"`
	Bandwidth int    `json:"bandwidth,omitempty"`
	PlanCode  string `json:"planCode,omitempty"`
}

// Image represents an OS image available for instances.
type Image struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Status       string  `json:"status"`
	Region       string  `json:"region"`
	CreationDate string  `json:"creationDate"`
	MinRAM       int     `json:"minRam"`
	MinDisk      int     `json:"minDisk"`
	Size         float64 `json:"size"`
	Type         string  `json:"type"`
	User         string  `json:"user"`
	Visibility   string  `json:"visibility"`
	PlanCode     string  `json:"planCode,omitempty"`
}

// SSHKey represents an SSH key registered in OVH.
type SSHKey struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	PublicKey   string   `json:"publicKey"`
	Fingerprint string   `json:"fingerprint,omitempty"`
	Regions     []string `json:"regions,omitempty"`
}

// PrivateNetwork represents a vRack private network.
type PrivateNetwork struct {
	ID      string          `json:"id"`
	Name    string          `json:"name"`
	VlanID  int             `json:"vlanId"`
	Regions []NetworkRegion `json:"regions,omitempty"`
	Status  string          `json:"status"`
	Type    string          `json:"type"`
}

// NetworkRegion describes the status of a network in a specific region,
// including the OpenStack-internal UUID used by region-scoped APIs (LB, etc.).
type NetworkRegion struct {
	Region      string `json:"region"`
	Status      string `json:"status"`
	OpenStackID string `json:"openstackId,omitempty"`
}

// OpenStackIDForRegion returns the OpenStack network UUID for the given region,
// or empty if not found. Required for region-scoped APIs (Octavia LB, etc.)
// which use OpenStack UUIDs rather than OVH "pn-NNNNNN_N" IDs.
func (n *PrivateNetwork) OpenStackIDForRegion(region string) string {
	for _, r := range n.Regions {
		if r.Region == region {
			return r.OpenStackID
		}
	}

	return ""
}

// CreateNetworkOpts are the parameters for creating a private network.
type CreateNetworkOpts struct {
	Name    string   `json:"name"`
	VlanID  int      `json:"vlanId,omitempty"`
	Regions []string `json:"regions,omitempty"`
}

// Subnet represents a subnet within a private network.
type Subnet struct {
	ID        string   `json:"id"`
	CIDR      string   `json:"cidr"`
	GatewayIP string   `json:"gatewayIp"`
	IPPools   []IPPool `json:"ipPools,omitempty"`
	Region    string   `json:"region,omitempty"`
	DHCP      bool     `json:"dhcp,omitempty"`
	NoGateway bool     `json:"noGateway,omitempty"`
}

// IPPool represents an IP allocation pool within a subnet.
type IPPool struct {
	Start   string `json:"start"`
	End     string `json:"end"`
	Network string `json:"network"`
	Region  string `json:"region"`
	DHCP    bool   `json:"dhcp"`
}

// CreateSubnetOpts are the parameters for creating a subnet.
type CreateSubnetOpts struct {
	Network   string `json:"network"` // CIDR
	Start     string `json:"start,omitempty"`
	End       string `json:"end,omitempty"`
	Region    string `json:"region"`
	DHCP      bool   `json:"dhcp"`
	NoGateway bool   `json:"noGateway,omitempty"`
}

// LBFlavor represents an Octavia load balancer flavor (size).
type LBFlavor struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Region string `json:"region,omitempty"`
}

// LoadBalancer represents an OVH managed load balancer (Octavia).
type LoadBalancer struct {
	ID                 string         `json:"id"`
	Name               string         `json:"name"`
	CreatedAt          string         `json:"createdAt,omitempty"`
	UpdatedAt          string         `json:"updatedAt,omitempty"`
	Region             string         `json:"region,omitempty"`
	FlavorID           string         `json:"flavorId,omitempty"`
	OperatingStatus    string         `json:"operatingStatus"`
	ProvisioningStatus string         `json:"provisioningStatus"`
	VIPAddress         string         `json:"vipAddress"`
	VIPNetworkID       string         `json:"vipNetworkId,omitempty"`
	VIPSubnetID        string         `json:"vipSubnetId,omitempty"`
	FloatingIP         *FloatingIPRef `json:"floatingIp,omitempty"`
	Listeners          []ListenerRef  `json:"listeners,omitempty"`
}

// LoadBalancer status constants. OVH returns these in lowercase.
const (
	LBProvisioningStatusActive   = "active"
	LBProvisioningStatusCreating = "creating"
	LBProvisioningStatusError    = "error"
	LBOperatingStatusOnline      = "online"
	LBOperatingStatusOffline     = "offline"
	LBOperatingStatusError       = "error"
)

// FloatingIPRef is a reference to a floating IP on a load balancer.
type FloatingIPRef struct {
	ID string `json:"id"`
	IP string `json:"ip"`
}

// ListenerRef is a reference to a listener on a load balancer.
type ListenerRef struct {
	ID string `json:"id"`
}

// CreateLoadBalancerOpts are the parameters for creating a load balancer.
// Note: the OVH API rejects extra fields like "description" — only documented
// fields below are accepted.
type CreateLoadBalancerOpts struct {
	Name     string          `json:"name"`
	FlavorID string          `json:"flavorId"`
	Network  LBNetworkConfig `json:"network"`
}

// LBNetworkConfig describes the network configuration of a load balancer.
// The "private" sub-field references the vRack network and subnet for the VIP.
type LBNetworkConfig struct {
	Private LBPrivateNetwork `json:"private"`
}

// LBPrivateNetwork references a private vRack network for the LB VIP.
// The OVH API nests the subnetId inside the network reference.
type LBPrivateNetwork struct {
	Network LBNetworkRef `json:"network"`
}

// LBNetworkRef references a network by its OpenStack UUID with a subnet hint.
type LBNetworkRef struct {
	ID       string `json:"id"`
	SubnetID string `json:"subnetId,omitempty"`
}

// Listener represents a load balancer listener.
type Listener struct {
	ID                 string   `json:"id"`
	Name               string   `json:"name"`
	Protocol           string   `json:"protocol"` // TCP, HTTP, etc.
	ProtocolPort       int32    `json:"protocolPort,omitempty"`
	Port               int32    `json:"port,omitempty"`
	DefaultPoolID      string   `json:"defaultPoolId,omitempty"`
	LoadBalancerID     string   `json:"loadbalancerId,omitempty"`
	LoadBalancerIDs    []string `json:"loadBalancerIds,omitempty"`
	ProvisioningStatus string   `json:"provisioningStatus,omitempty"`
	OperatingStatus    string   `json:"operatingStatus,omitempty"`
}

// CreateListenerOpts are the parameters for creating a listener.
// OVH-specific: uses "port" (not "protocolPort") and "loadbalancerId" lowercase b.
type CreateListenerOpts struct {
	Name           string `json:"name"`
	Protocol       string `json:"protocol"`
	Port           int32  `json:"port"`
	LoadBalancerID string `json:"loadbalancerId"`
	DefaultPoolID  string `json:"defaultPoolId,omitempty"`
}

// Pool represents a load balancer backend pool.
type Pool struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	Protocol           string `json:"protocol"`
	LBAlgorithm        string `json:"lbAlgorithm"`
	ListenerID         string `json:"listenerId,omitempty"`
	LoadBalancerID     string `json:"loadbalancerId,omitempty"`
	ProvisioningStatus string `json:"provisioningStatus,omitempty"`
	OperatingStatus    string `json:"operatingStatus,omitempty"`
}

// CreatePoolOpts are the parameters for creating a backend pool.
// OVH-specific: uses "algorithm" (not "lbAlgorithm") and "loadbalancerId" lowercase b.
type CreatePoolOpts struct {
	Name           string `json:"name"`
	Protocol       string `json:"protocol"`
	Algorithm      string `json:"algorithm"` // roundRobin, leastConnections, sourceIp
	ListenerID     string `json:"listenerId,omitempty"`
	LoadBalancerID string `json:"loadbalancerId,omitempty"`
}

// Member represents a backend pool member (instance).
type Member struct {
	ID                 string `json:"id"`
	Name               string `json:"name,omitempty"`
	Address            string `json:"address"`
	ProtocolPort       int32  `json:"protocolPort"`
	SubnetID           string `json:"subnetId,omitempty"`
	OperatingStatus    string `json:"operatingStatus,omitempty"`
	ProvisioningStatus string `json:"provisioningStatus,omitempty"`
	Weight             int    `json:"weight,omitempty"`
}

// CreateMemberOpts are the parameters for adding a member to a pool.
type CreateMemberOpts struct {
	Name         string `json:"name,omitempty"`
	Address      string `json:"address"`
	ProtocolPort int32  `json:"protocolPort"`
	SubnetID     string `json:"subnetId,omitempty"`
	Weight       int    `json:"weight,omitempty"`
}

// addPoolMembersRequest wraps a list of members for the OVH batch-add endpoint.
type addPoolMembersRequest struct {
	Members []CreateMemberOpts `json:"members"`
}

// FloatingIP represents a floating IP resource.
type FloatingIP struct {
	ID               string                 `json:"id"`
	IP               string                 `json:"ip"`
	Status           string                 `json:"status"`
	AssociatedEntity *FloatingIPAssociation `json:"associatedEntity,omitempty"`
	Region           string                 `json:"region,omitempty"`
	NetworkID        string                 `json:"networkId,omitempty"`
}

// FloatingIPAssociation describes what a floating IP is currently attached to
// (instance / loadbalancer) and the underlying gateway.
type FloatingIPAssociation struct {
	ID        string `json:"id"`
	Type      string `json:"type"` // "loadbalancer" or "instance"
	IP        string `json:"ip,omitempty"`
	GatewayID string `json:"gatewayId,omitempty"`
}

// Gateway represents an OVH internet gateway attached to a private subnet.
type Gateway struct {
	ID         string             `json:"id"`
	Name       string             `json:"name"`
	Status     string             `json:"status,omitempty"`
	Model      string             `json:"model,omitempty"`
	Region     string             `json:"region,omitempty"`
	Interfaces []GatewayInterface `json:"interfaces,omitempty"`
}

// GatewayInterface is one of the network interfaces attached to a gateway
// (usually one private + one public after `expose`).
type GatewayInterface struct {
	ID        string `json:"id"`
	IP        string `json:"ip,omitempty"`
	NetworkID string `json:"networkId,omitempty"`
	SubnetID  string `json:"subnetId,omitempty"`
}

// CreateFloatingIPOpts are the parameters for creating a floating IP.
type CreateFloatingIPOpts struct {
	IP          string `json:"ip,omitempty"`
	Description string `json:"description,omitempty"`
}

// Volume represents an OVH block storage volume.
type Volume struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Size        int      `json:"size"` // in GB
	Type        string   `json:"type"` // classic, high-speed, high-speed-gen2
	Region      string   `json:"region"`
	Status      string   `json:"status"`
	AttachedTo  []string `json:"attachedTo,omitempty"`
}

// CreateVolumeOpts are the parameters for creating a block storage volume.
type CreateVolumeOpts struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Size        int    `json:"size"`
	Type        string `json:"type,omitempty"`
	Region      string `json:"region"`
}

// AttachVolumeOpts are the parameters for attaching a volume to an instance.
type AttachVolumeOpts struct {
	InstanceID string `json:"instanceId"`
}

// Me represents the OVH account info (used for credential validation).
type Me struct {
	Nichandle string `json:"nichandle"`
	FirstName string `json:"firstname"`
	Name      string `json:"name"`
	Email     string `json:"email"`
}
