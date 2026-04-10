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
	"math"
	"strings"
	"time"

	"github.com/go-logr/logr"
	goovh "github.com/ovh/go-ovh/ovh"
	corev1 "k8s.io/api/core/v1"
)

const (
	maxRetries     = 5
	initialBackoff = 2 * time.Second
	maxBackoff     = 60 * time.Second
)

// Client wraps the OVH API client with project-scoped operations.
type Client struct {
	api         *goovh.Client
	serviceName string
	region      string
	logger      logr.Logger
}

// NewClient creates a new OVH API client from explicit credentials.
func NewClient(endpoint, appKey, appSecret, consumerKey, serviceName, region string, logger logr.Logger) (*Client, error) {
	api, err := goovh.NewClient(endpoint, appKey, appSecret, consumerKey)
	if err != nil {
		return nil, fmt.Errorf("creating OVH client: %w", err)
	}

	return &Client{
		api:         api,
		serviceName: serviceName,
		region:      region,
		logger:      logger.WithName("ovh-client"),
	}, nil
}

// NewClientFromSecret creates a new OVH API client from a Kubernetes Secret.
// The Secret must contain keys: endpoint, applicationKey, applicationSecret, consumerKey.
func NewClientFromSecret(secret *corev1.Secret, serviceName, region string, logger logr.Logger) (*Client, error) {
	endpoint := string(secret.Data["endpoint"])
	appKey := string(secret.Data["applicationKey"])
	appSecret := string(secret.Data["applicationSecret"])
	consumerKey := string(secret.Data["consumerKey"])

	if endpoint == "" || appKey == "" || appSecret == "" || consumerKey == "" {
		return nil, fmt.Errorf("secret %s/%s missing required keys (endpoint, applicationKey, applicationSecret, consumerKey)",
			secret.Namespace, secret.Name)
	}

	return NewClient(endpoint, appKey, appSecret, consumerKey, serviceName, region, logger)
}

// projectPath returns the API path prefix for the current project.
func (c *Client) projectPath(format string, args ...interface{}) string {
	prefix := fmt.Sprintf("/cloud/project/%s", c.serviceName)
	if format != "" {
		suffix := fmt.Sprintf(format, args...)
		return prefix + suffix
	}

	return prefix
}

// regionPath returns the API path prefix for region-scoped resources (LB, floating IP).
func (c *Client) regionPath(format string, args ...interface{}) string {
	prefix := fmt.Sprintf("/cloud/project/%s/region/%s", c.serviceName, c.region)
	if format != "" {
		suffix := fmt.Sprintf(format, args...)
		return prefix + suffix
	}

	return prefix
}

// retryWithBackoff retries the given function with exponential backoff on transient errors.
func (c *Client) retryWithBackoff(operation string, fn func() error) error {
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		lastErr = fn()
		if lastErr == nil {
			return nil
		}

		if !IsRetryable(lastErr) {
			return lastErr
		}

		if attempt < maxRetries {
			backoff := time.Duration(math.Min(
				float64(initialBackoff)*math.Pow(2, float64(attempt)),
				float64(maxBackoff),
			))
			c.logger.V(1).Info("retrying after transient error",
				"operation", operation,
				"attempt", attempt+1,
				"backoff", backoff,
				"error", lastErr,
			)
			time.Sleep(backoff)
		}
	}

	return fmt.Errorf("%s failed after %d retries: %w", operation, maxRetries, lastErr)
}

// --- Credential Validation ---

// ValidateCredentials tests the OVH API connection by calling GET /me.
func (c *Client) ValidateCredentials() (*Me, error) {
	var me Me

	err := c.retryWithBackoff("ValidateCredentials", func() error {
		return c.api.Get("/me", &me)
	})
	if err != nil {
		return nil, fmt.Errorf("validating OVH credentials: %w", err)
	}

	return &me, nil
}

// --- Instance Operations ---

// CreateInstance creates a new compute instance.
func (c *Client) CreateInstance(opts CreateInstanceOpts) (*Instance, error) {
	var instance Instance

	err := c.retryWithBackoff("CreateInstance", func() error {
		return c.api.Post(c.projectPath("/instance"), opts, &instance)
	})
	if err != nil {
		return nil, fmt.Errorf("creating instance %q: %w", opts.Name, err)
	}

	return &instance, nil
}

// GetInstance retrieves an instance by ID.
func (c *Client) GetInstance(instanceID string) (*Instance, error) {
	var instance Instance

	err := c.retryWithBackoff("GetInstance", func() error {
		return c.api.Get(c.projectPath("/instance/%s", instanceID), &instance)
	})
	if err != nil {
		return nil, fmt.Errorf("getting instance %s: %w", instanceID, err)
	}

	return &instance, nil
}

// DeleteInstance deletes an instance by ID.
func (c *Client) DeleteInstance(instanceID string) error {
	err := c.retryWithBackoff("DeleteInstance", func() error {
		return c.api.Delete(c.projectPath("/instance/%s", instanceID), nil)
	})
	if err != nil {
		if IsNotFound(err) {
			return nil // Already deleted
		}

		return fmt.Errorf("deleting instance %s: %w", instanceID, err)
	}

	return nil
}

// ListInstances lists all instances in the project, optionally filtered by region.
func (c *Client) ListInstances() ([]Instance, error) {
	var instances []Instance

	path := c.projectPath("/instance")
	if c.region != "" {
		path += "?region=" + c.region
	}

	err := c.retryWithBackoff("ListInstances", func() error {
		return c.api.Get(path, &instances)
	})
	if err != nil {
		return nil, fmt.Errorf("listing instances: %w", err)
	}

	return instances, nil
}

// FindInstanceByName searches for an instance by name in the current region.
func (c *Client) FindInstanceByName(name string) (*Instance, error) {
	instances, err := c.ListInstances()
	if err != nil {
		return nil, err
	}

	for i := range instances {
		if instances[i].Name == name {
			return &instances[i], nil
		}
	}

	return nil, nil // Not found, not an error
}

// --- Flavor Operations ---

// ListFlavors lists all available flavors, optionally filtered by region.
func (c *Client) ListFlavors() ([]Flavor, error) {
	var flavors []Flavor

	path := c.projectPath("/flavor")
	if c.region != "" {
		path += "?region=" + c.region
	}

	err := c.retryWithBackoff("ListFlavors", func() error {
		return c.api.Get(path, &flavors)
	})
	if err != nil {
		return nil, fmt.Errorf("listing flavors: %w", err)
	}

	return flavors, nil
}

// GetFlavorByName finds a flavor by name (case-insensitive match).
func (c *Client) GetFlavorByName(name string) (*Flavor, error) {
	flavors, err := c.ListFlavors()
	if err != nil {
		return nil, err
	}

	for i := range flavors {
		if strings.EqualFold(flavors[i].Name, name) {
			return &flavors[i], nil
		}
	}

	return nil, fmt.Errorf("flavor %q not found in region %s", name, c.region)
}

// --- Image Operations ---

// ListImages lists all available images, optionally filtered by region.
func (c *Client) ListImages() ([]Image, error) {
	var images []Image

	path := c.projectPath("/image")
	if c.region != "" {
		path += "?region=" + c.region
	}

	err := c.retryWithBackoff("ListImages", func() error {
		return c.api.Get(path, &images)
	})
	if err != nil {
		return nil, fmt.Errorf("listing images: %w", err)
	}

	return images, nil
}

// GetImageByName finds an image by name (partial, case-insensitive match).
func (c *Client) GetImageByName(name string) (*Image, error) {
	images, err := c.ListImages()
	if err != nil {
		return nil, err
	}

	nameLower := strings.ToLower(name)

	// Exact match first
	for i := range images {
		if strings.EqualFold(images[i].Name, name) {
			return &images[i], nil
		}
	}

	// Partial match fallback
	for i := range images {
		if strings.Contains(strings.ToLower(images[i].Name), nameLower) {
			return &images[i], nil
		}
	}

	return nil, fmt.Errorf("image %q not found in region %s", name, c.region)
}

// --- SSH Key Operations ---

// ListSSHKeys lists all SSH keys in the project.
func (c *Client) ListSSHKeys() ([]SSHKey, error) {
	var keys []SSHKey

	path := c.projectPath("/sshkey")
	if c.region != "" {
		path += "?region=" + c.region
	}

	err := c.retryWithBackoff("ListSSHKeys", func() error {
		return c.api.Get(path, &keys)
	})
	if err != nil {
		return nil, fmt.Errorf("listing SSH keys: %w", err)
	}

	return keys, nil
}

// GetSSHKeyByName finds an SSH key by name.
func (c *Client) GetSSHKeyByName(name string) (*SSHKey, error) {
	keys, err := c.ListSSHKeys()
	if err != nil {
		return nil, err
	}

	for i := range keys {
		if keys[i].Name == name {
			return &keys[i], nil
		}
	}

	return nil, fmt.Errorf("SSH key %q not found", name)
}

// --- Network Operations ---

// ListPrivateNetworks lists all private networks in the project.
func (c *Client) ListPrivateNetworks() ([]PrivateNetwork, error) {
	var networks []PrivateNetwork

	err := c.retryWithBackoff("ListPrivateNetworks", func() error {
		return c.api.Get(c.projectPath("/network/private"), &networks)
	})
	if err != nil {
		return nil, fmt.Errorf("listing private networks: %w", err)
	}

	return networks, nil
}

// GetPrivateNetwork retrieves a private network by ID.
func (c *Client) GetPrivateNetwork(networkID string) (*PrivateNetwork, error) {
	var network PrivateNetwork

	err := c.retryWithBackoff("GetPrivateNetwork", func() error {
		return c.api.Get(c.projectPath("/network/private/%s", networkID), &network)
	})
	if err != nil {
		return nil, fmt.Errorf("getting private network %s: %w", networkID, err)
	}

	return &network, nil
}

// CreatePrivateNetwork creates a new private network.
func (c *Client) CreatePrivateNetwork(opts CreateNetworkOpts) (*PrivateNetwork, error) {
	var network PrivateNetwork

	err := c.retryWithBackoff("CreatePrivateNetwork", func() error {
		return c.api.Post(c.projectPath("/network/private"), opts, &network)
	})
	if err != nil {
		return nil, fmt.Errorf("creating private network %q: %w", opts.Name, err)
	}

	return &network, nil
}

// DeletePrivateNetwork deletes a private network by ID.
func (c *Client) DeletePrivateNetwork(networkID string) error {
	err := c.retryWithBackoff("DeletePrivateNetwork", func() error {
		return c.api.Delete(c.projectPath("/network/private/%s", networkID), nil)
	})
	if err != nil {
		if IsNotFound(err) {
			return nil
		}

		return fmt.Errorf("deleting private network %s: %w", networkID, err)
	}

	return nil
}

// ListSubnets lists all subnets in a private network.
func (c *Client) ListSubnets(networkID string) ([]Subnet, error) {
	var subnets []Subnet

	err := c.retryWithBackoff("ListSubnets", func() error {
		return c.api.Get(c.projectPath("/network/private/%s/subnet", networkID), &subnets)
	})
	if err != nil {
		return nil, fmt.Errorf("listing subnets for network %s: %w", networkID, err)
	}

	return subnets, nil
}

// CreateSubnet creates a subnet in a private network.
func (c *Client) CreateSubnet(networkID string, opts CreateSubnetOpts) (*Subnet, error) {
	var subnet Subnet

	err := c.retryWithBackoff("CreateSubnet", func() error {
		return c.api.Post(c.projectPath("/network/private/%s/subnet", networkID), opts, &subnet)
	})
	if err != nil {
		return nil, fmt.Errorf("creating subnet in network %s: %w", networkID, err)
	}

	return &subnet, nil
}

// --- Load Balancer Operations ---

// CreateLoadBalancer creates a new managed load balancer (Octavia).
func (c *Client) CreateLoadBalancer(opts CreateLoadBalancerOpts) (*LoadBalancer, error) {
	var lb LoadBalancer

	err := c.retryWithBackoff("CreateLoadBalancer", func() error {
		return c.api.Post(c.regionPath("/loadbalancing/loadbalancer"), opts, &lb)
	})
	if err != nil {
		return nil, fmt.Errorf("creating load balancer %q: %w", opts.Name, err)
	}

	return &lb, nil
}

// GetLoadBalancer retrieves a load balancer by ID.
func (c *Client) GetLoadBalancer(lbID string) (*LoadBalancer, error) {
	var lb LoadBalancer

	err := c.retryWithBackoff("GetLoadBalancer", func() error {
		return c.api.Get(c.regionPath("/loadbalancing/loadbalancer/%s", lbID), &lb)
	})
	if err != nil {
		return nil, fmt.Errorf("getting load balancer %s: %w", lbID, err)
	}

	return &lb, nil
}

// DeleteLoadBalancer deletes a load balancer by ID.
func (c *Client) DeleteLoadBalancer(lbID string) error {
	err := c.retryWithBackoff("DeleteLoadBalancer", func() error {
		return c.api.Delete(c.regionPath("/loadbalancing/loadbalancer/%s", lbID), nil)
	})
	if err != nil {
		if IsNotFound(err) {
			return nil
		}

		return fmt.Errorf("deleting load balancer %s: %w", lbID, err)
	}

	return nil
}

// --- Listener Operations ---

// CreateListener creates a listener on a load balancer.
func (c *Client) CreateListener(opts CreateListenerOpts) (*Listener, error) {
	var listener Listener

	err := c.retryWithBackoff("CreateListener", func() error {
		return c.api.Post(c.regionPath("/loadbalancing/listener"), opts, &listener)
	})
	if err != nil {
		return nil, fmt.Errorf("creating listener %q: %w", opts.Name, err)
	}

	return &listener, nil
}

// DeleteListener deletes a listener by ID.
func (c *Client) DeleteListener(listenerID string) error {
	err := c.retryWithBackoff("DeleteListener", func() error {
		return c.api.Delete(c.regionPath("/loadbalancing/listener/%s", listenerID), nil)
	})
	if err != nil {
		if IsNotFound(err) {
			return nil
		}

		return fmt.Errorf("deleting listener %s: %w", listenerID, err)
	}

	return nil
}

// --- Pool Operations ---

// CreatePool creates a backend pool.
func (c *Client) CreatePool(opts CreatePoolOpts) (*Pool, error) {
	var pool Pool

	err := c.retryWithBackoff("CreatePool", func() error {
		return c.api.Post(c.regionPath("/loadbalancing/pool"), opts, &pool)
	})
	if err != nil {
		return nil, fmt.Errorf("creating pool %q: %w", opts.Name, err)
	}

	return &pool, nil
}

// DeletePool deletes a backend pool by ID.
func (c *Client) DeletePool(poolID string) error {
	err := c.retryWithBackoff("DeletePool", func() error {
		return c.api.Delete(c.regionPath("/loadbalancing/pool/%s", poolID), nil)
	})
	if err != nil {
		if IsNotFound(err) {
			return nil
		}

		return fmt.Errorf("deleting pool %s: %w", poolID, err)
	}

	return nil
}

// --- Member Operations ---

// AddPoolMember adds a member (instance) to a backend pool.
func (c *Client) AddPoolMember(poolID string, opts CreateMemberOpts) (*Member, error) {
	var member Member

	err := c.retryWithBackoff("AddPoolMember", func() error {
		return c.api.Post(c.regionPath("/loadbalancing/pool/%s/member", poolID), opts, &member)
	})
	if err != nil {
		return nil, fmt.Errorf("adding member to pool %s: %w", poolID, err)
	}

	return &member, nil
}

// RemovePoolMember removes a member from a backend pool.
func (c *Client) RemovePoolMember(poolID, memberID string) error {
	err := c.retryWithBackoff("RemovePoolMember", func() error {
		return c.api.Delete(c.regionPath("/loadbalancing/pool/%s/member/%s", poolID, memberID), nil)
	})
	if err != nil {
		if IsNotFound(err) {
			return nil
		}

		return fmt.Errorf("removing member %s from pool %s: %w", memberID, poolID, err)
	}

	return nil
}

// ListPoolMembers lists all members in a backend pool.
func (c *Client) ListPoolMembers(poolID string) ([]Member, error) {
	var members []Member

	err := c.retryWithBackoff("ListPoolMembers", func() error {
		return c.api.Get(c.regionPath("/loadbalancing/pool/%s/member", poolID), &members)
	})
	if err != nil {
		return nil, fmt.Errorf("listing members for pool %s: %w", poolID, err)
	}

	return members, nil
}

// --- Volume Operations ---

// CreateVolume creates a new block storage volume.
func (c *Client) CreateVolume(opts CreateVolumeOpts) (*Volume, error) {
	var volume Volume

	err := c.retryWithBackoff("CreateVolume", func() error {
		return c.api.Post(c.projectPath("/volume"), opts, &volume)
	})
	if err != nil {
		return nil, fmt.Errorf("creating volume %q: %w", opts.Name, err)
	}

	return &volume, nil
}

// GetVolume retrieves a volume by ID.
func (c *Client) GetVolume(volumeID string) (*Volume, error) {
	var volume Volume

	err := c.retryWithBackoff("GetVolume", func() error {
		return c.api.Get(c.projectPath("/volume/%s", volumeID), &volume)
	})
	if err != nil {
		return nil, fmt.Errorf("getting volume %s: %w", volumeID, err)
	}

	return &volume, nil
}

// AttachVolume attaches a volume to an instance.
func (c *Client) AttachVolume(volumeID, instanceID string) error {
	opts := AttachVolumeOpts{InstanceID: instanceID}

	err := c.retryWithBackoff("AttachVolume", func() error {
		return c.api.Post(c.projectPath("/volume/%s/attach", volumeID), opts, nil)
	})
	if err != nil {
		return fmt.Errorf("attaching volume %s to instance %s: %w", volumeID, instanceID, err)
	}

	return nil
}

// DetachVolume detaches a volume from an instance.
func (c *Client) DetachVolume(volumeID, instanceID string) error {
	opts := AttachVolumeOpts{InstanceID: instanceID}

	err := c.retryWithBackoff("DetachVolume", func() error {
		return c.api.Post(c.projectPath("/volume/%s/detach", volumeID), opts, nil)
	})
	if err != nil {
		if IsNotFound(err) {
			return nil
		}

		return fmt.Errorf("detaching volume %s from instance %s: %w", volumeID, instanceID, err)
	}

	return nil
}

// DeleteVolume deletes a block storage volume.
func (c *Client) DeleteVolume(volumeID string) error {
	err := c.retryWithBackoff("DeleteVolume", func() error {
		return c.api.Delete(c.projectPath("/volume/%s", volumeID), nil)
	})
	if err != nil {
		if IsNotFound(err) {
			return nil
		}

		return fmt.Errorf("deleting volume %s: %w", volumeID, err)
	}

	return nil
}

// --- Floating IP Operations ---

// CreateFloatingIP creates a floating IP in the current region.
func (c *Client) CreateFloatingIP(opts CreateFloatingIPOpts) (*FloatingIP, error) {
	var fip FloatingIP

	err := c.retryWithBackoff("CreateFloatingIP", func() error {
		return c.api.Post(c.regionPath("/floatingip"), opts, &fip)
	})
	if err != nil {
		return nil, fmt.Errorf("creating floating IP: %w", err)
	}

	return &fip, nil
}

// DeleteFloatingIP deletes a floating IP.
func (c *Client) DeleteFloatingIP(fipID string) error {
	err := c.retryWithBackoff("DeleteFloatingIP", func() error {
		return c.api.Delete(c.regionPath("/floatingip/%s", fipID), nil)
	})
	if err != nil {
		if IsNotFound(err) {
			return nil
		}

		return fmt.Errorf("deleting floating IP %s: %w", fipID, err)
	}

	return nil
}
