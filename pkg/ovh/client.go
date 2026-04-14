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
func (c *Client) projectPath(format string, args ...any) string {
	prefix := "/cloud/project/" + c.serviceName
	if format != "" {
		suffix := fmt.Sprintf(format, args...)

		return prefix + suffix
	}

	return prefix
}

// regionPath returns the API path prefix for region-scoped resources (LB, floating IP).
func (c *Client) regionPath(format string, args ...any) string {
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

// ValidateCredentials tests the OVH API connection and credentials scope by
// calling a project-scoped read-only endpoint (region listing). This works
// even with narrowly scoped Consumer Keys that do not have access to /me
// (the typical CAPIOVH deployment uses CKs scoped to /cloud/project/{sn}/*).
func (c *Client) ValidateCredentials() error {
	if c.serviceName == "" {
		return errors.New("serviceName is empty")
	}

	var regions []string

	err := c.retryWithBackoff("ValidateCredentials", func() error {
		return c.api.Get(c.projectPath("/region"), &regions)
	})
	if err != nil {
		return fmt.Errorf("validating OVH credentials: %w", err)
	}

	if len(regions) == 0 {
		return errors.New("validating OVH credentials: no regions returned by project")
	}

	return nil
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

// ListImages lists all public images available in the project, optionally filtered by region.
// Public images are OVH-managed (Ubuntu, Debian, etc.).
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

// ListSnapshots lists all private images (BYOI / snapshots) available in the project.
// Custom images uploaded via Glance appear here, not in /image.
func (c *Client) ListSnapshots() ([]Image, error) {
	var snapshots []Image

	path := c.projectPath("/snapshot")
	if c.region != "" {
		path += "?region=" + c.region
	}

	err := c.retryWithBackoff("ListSnapshots", func() error {
		return c.api.Get(path, &snapshots)
	})
	if err != nil {
		return nil, fmt.Errorf("listing snapshots: %w", err)
	}

	return snapshots, nil
}

// isUUID reports whether s looks like a UUID (8-4-4-4-12 hex digits with dashes).
func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}

	for i, c := range s {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			isHexDigit := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
			if !isHexDigit {
				return false
			}
		}
	}

	return true
}

// GetImageByName resolves an image identifier to an Image. The identifier may be:
//   - a UUID (returned directly without lookup)
//   - an image name matching a public OVH image (searched in /image)
//   - a name matching a private/BYOI image (searched in /snapshot as fallback)
//
// This unified lookup lets users specify any image transparently in OVHMachine.spec.imageName.
func (c *Client) GetImageByName(name string) (*Image, error) {
	if name == "" {
		return nil, errors.New("image name is empty")
	}

	// Shortcut: if the name is a UUID, trust it as-is (no lookup needed)
	if isUUID(name) {
		return &Image{ID: name, Name: name, Region: c.region}, nil
	}

	// 1. Search public images (most common case)
	images, err := c.ListImages()
	if err != nil {
		return nil, err
	}

	if img := findImageByName(images, name); img != nil {
		return img, nil
	}

	// 2. Fallback: search private/BYOI images (snapshots)
	snapshots, err := c.ListSnapshots()
	if err != nil {
		// If snapshots lookup also fails, report the original "not found" with public images
		return nil, fmt.Errorf("image %q not found in public images and snapshot lookup failed: %w", name, err)
	}

	if img := findImageByName(snapshots, name); img != nil {
		return img, nil
	}

	return nil, fmt.Errorf("image %q not found in region %s (searched public images and BYOI snapshots)", name, c.region)
}

// findImageByName returns a pointer to the matching image (exact match preferred,
// case-insensitive partial match as fallback), or nil if no match.
func findImageByName(images []Image, name string) *Image {
	// Exact match first
	for i := range images {
		if strings.EqualFold(images[i].Name, name) {
			return &images[i]
		}
	}

	// Partial match fallback
	nameLower := strings.ToLower(name)
	for i := range images {
		if strings.Contains(strings.ToLower(images[i].Name), nameLower) {
			return &images[i]
		}
	}

	return nil
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

// CreateSSHKeyOpts are the parameters for registering a new SSH public key.
type CreateSSHKeyOpts struct {
	Name      string `json:"name"`
	PublicKey string `json:"publicKey"`
	Region    string `json:"region,omitempty"`
}

// CreateSSHKey registers a new SSH public key in the project.
func (c *Client) CreateSSHKey(opts CreateSSHKeyOpts) (*SSHKey, error) {
	var key SSHKey

	err := c.retryWithBackoff("CreateSSHKey", func() error {
		return c.api.Post(c.projectPath("/sshkey"), opts, &key)
	})
	if err != nil {
		return nil, fmt.Errorf("creating SSH key %q: %w", opts.Name, err)
	}

	return &key, nil
}

// DeleteSSHKey removes an SSH key by ID.
func (c *Client) DeleteSSHKey(keyID string) error {
	err := c.retryWithBackoff("DeleteSSHKey", func() error {
		return c.api.Delete(c.projectPath("/sshkey/%s", keyID), nil)
	})
	if err != nil {
		if IsNotFound(err) {
			return nil
		}

		return fmt.Errorf("deleting SSH key %s: %w", keyID, err)
	}

	return nil
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

// ListLBFlavors lists available Octavia load balancer flavors in the region.
// Typical names: "small", "medium", "large", "xl".
func (c *Client) ListLBFlavors() ([]LBFlavor, error) {
	var flavors []LBFlavor

	err := c.retryWithBackoff("ListLBFlavors", func() error {
		return c.api.Get(c.regionPath("/loadbalancing/flavor"), &flavors)
	})
	if err != nil {
		return nil, fmt.Errorf("listing LB flavors: %w", err)
	}

	return flavors, nil
}

// GetLBFlavorByName resolves an LB flavor by name (small, medium, large, xl).
func (c *Client) GetLBFlavorByName(name string) (*LBFlavor, error) {
	flavors, err := c.ListLBFlavors()
	if err != nil {
		return nil, err
	}

	for i := range flavors {
		if strings.EqualFold(flavors[i].Name, name) {
			return &flavors[i], nil
		}
	}

	return nil, fmt.Errorf("LB flavor %q not found in region %s", name, c.region)
}

// CreateLoadBalancer creates a managed load balancer (Octavia), or returns
// the existing one if one with the same name already exists in the region.
//
// Idempotency: a POST may succeed but the controller restart (or a slow API
// response) may leave us without the ID. To avoid creating duplicate LBs we
// first list LBs by name; only if not found do we POST.
//
// OVH async behavior: the POST returns a task descriptor, not the LB. After
// POST we poll list-by-name with backoff until the LB appears (typically 2-15
// seconds).
func (c *Client) CreateLoadBalancer(opts CreateLoadBalancerOpts) (*LoadBalancer, error) {
	// Idempotency: skip POST if an LB with this name already exists
	if existing, err := c.findLoadBalancerByName(opts.Name); err == nil && existing != nil {
		return existing, nil
	}

	var taskResp map[string]any

	err := c.retryWithBackoff("CreateLoadBalancer", func() error {
		return c.api.Post(c.regionPath("/loadbalancing/loadbalancer"), opts, &taskResp)
	})
	if err != nil {
		return nil, fmt.Errorf("creating load balancer %q: %w", opts.Name, err)
	}

	// Poll list-by-name with backoff: the LB may take several seconds to
	// appear after POST returns the task descriptor.
	const maxLookupAttempts = 10

	for attempt := range maxLookupAttempts {
		lb, err := c.findLoadBalancerByName(opts.Name)
		if err != nil {
			return nil, fmt.Errorf("LB %q created but lookup failed: %w", opts.Name, err)
		}

		if lb != nil {
			return lb, nil
		}

		time.Sleep(time.Duration(2*(attempt+1)) * time.Second)
	}

	return nil, fmt.Errorf("LB %q was created but did not appear in list within %d attempts", opts.Name, maxLookupAttempts)
}

// findLoadBalancerByName lists LBs in the region and returns the first matching name.
func (c *Client) findLoadBalancerByName(name string) (*LoadBalancer, error) {
	var lbs []LoadBalancer

	err := c.retryWithBackoff("ListLoadBalancers", func() error {
		return c.api.Get(c.regionPath("/loadbalancing/loadbalancer"), &lbs)
	})
	if err != nil {
		return nil, err
	}

	for i := range lbs {
		if lbs[i].Name == name {
			return &lbs[i], nil
		}
	}

	return nil, nil
}

// ListLoadBalancersByPrefix lists LBs whose name starts with the given prefix.
// Used by the cluster controller's ReconcileDelete to clean up orphan LBs that
// may have been created during a previous reconcile (e.g. before the
// idempotency fix landed, or because the controller restarted between POST and
// status persist).
func (c *Client) ListLoadBalancersByPrefix(prefix string) ([]LoadBalancer, error) {
	var lbs []LoadBalancer

	err := c.retryWithBackoff("ListLoadBalancers", func() error {
		return c.api.Get(c.regionPath("/loadbalancing/loadbalancer"), &lbs)
	})
	if err != nil {
		return nil, err
	}

	matched := make([]LoadBalancer, 0, len(lbs))

	for i := range lbs {
		if strings.HasPrefix(lbs[i].Name, prefix) {
			matched = append(matched, lbs[i])
		}
	}

	return matched, nil
}

// AssociateFloatingIPToLB attaches an existing floating IP to a load balancer.
// OVH endpoint: POST /cloud/project/{sn}/region/{r}/loadbalancing/loadbalancer/{lbId}/associateFloatingIp
// with body { "floatingIpId": "..." }.
func (c *Client) AssociateFloatingIPToLB(lbID, floatingIPID string) error {
	body := map[string]string{"floatingIpId": floatingIPID}

	err := c.retryWithBackoff("AssociateFloatingIPToLB", func() error {
		return c.api.Post(c.regionPath("/loadbalancing/loadbalancer/%s/associateFloatingIp", lbID), body, nil)
	})
	if err != nil {
		return fmt.Errorf("associating floating IP %s to LB %s: %w", floatingIPID, lbID, err)
	}

	return nil
}

// ListGateways lists all gateways in the current region.
func (c *Client) ListGateways() ([]Gateway, error) {
	var gws []Gateway

	err := c.retryWithBackoff("ListGateways", func() error {
		return c.api.Get(c.regionPath("/gateway"), &gws)
	})
	if err != nil {
		return nil, fmt.Errorf("listing gateways: %w", err)
	}

	return gws, nil
}

// DeleteGateway removes the gateway. Idempotent: NotFound is treated as success.
func (c *Client) DeleteGateway(gatewayID string) error {
	err := c.retryWithBackoff("DeleteGateway", func() error {
		return c.api.Delete(c.regionPath("/gateway/%s", gatewayID), nil)
	})
	if err != nil {
		if IsNotFound(err) {
			return nil
		}

		return fmt.Errorf("deleting gateway %s: %w", gatewayID, err)
	}

	return nil
}

// ExposeGateway attaches a public port to the gateway, enabling SNAT outbound
// internet access for instances on the subnet the gateway is attached to.
//
// OVH endpoint: POST /gateway/{id}/expose
// A gateway created implicitly by the floating-IP flow is set up for inbound
// DNAT only. Calling expose adds a public interface so traffic from the
// private subnet can egress through the gateway's new public IP.
func (c *Client) ExposeGateway(gatewayID string) error {
	err := c.retryWithBackoff("ExposeGateway", func() error {
		return c.api.Post(c.regionPath("/gateway/%s/expose", gatewayID), nil, nil)
	})
	if err != nil {
		return fmt.Errorf("exposing gateway %s: %w", gatewayID, err)
	}

	return nil
}

// CreateLoadBalancerFloatingIP allocates a new floating IP on the external
// network and attaches it to a load balancer in a single call. This is the
// only reliable way to get a public IP on an Octavia LB in OVH Public Cloud
// — there is no standalone `POST /floatingip` endpoint to allocate one first.
//
// OVH endpoint: POST /loadbalancing/loadbalancer/{lbId}/floatingIp
// body: { "ip": "<private LB VIP>", "gateway": { "model": "s", "name": "..." } }
//
// If no internet gateway exists on the LB's private network, one is created
// with the provided model/name. If one already exists, the gateway field is
// ignored.
func (c *Client) CreateLoadBalancerFloatingIP(lbID, privateIP, gatewayName string) (*FloatingIP, error) {
	body := map[string]any{
		"ip": privateIP,
		"gateway": map[string]string{
			"model": "s",
			"name":  gatewayName,
		},
	}

	var fip FloatingIP

	err := c.retryWithBackoff("CreateLoadBalancerFloatingIP", func() error {
		return c.api.Post(c.regionPath("/loadbalancing/loadbalancer/%s/floatingIp", lbID), body, &fip)
	})
	if err != nil {
		return nil, fmt.Errorf("creating floating IP for LB %s: %w", lbID, err)
	}

	return &fip, nil
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

// ListListeners lists all listeners in the current region.
func (c *Client) ListListeners() ([]Listener, error) {
	var listeners []Listener

	err := c.retryWithBackoff("ListListeners", func() error {
		return c.api.Get(c.regionPath("/loadbalancing/listener"), &listeners)
	})
	if err != nil {
		return nil, fmt.Errorf("listing listeners: %w", err)
	}

	return listeners, nil
}

// FindListenerByName returns the first listener matching the given name on the
// specified load balancer, or nil if none exists. Useful for idempotent
// listener creation: check before POST to avoid 409 conflicts if status didn't
// persist after a successful prior create.
func (c *Client) FindListenerByName(lbID, name string) (*Listener, error) {
	listeners, err := c.ListListeners()
	if err != nil {
		return nil, err
	}

	for i := range listeners {
		if listeners[i].Name != name {
			continue
		}

		for _, id := range listeners[i].LoadBalancerIDs {
			if id == lbID {
				return &listeners[i], nil
			}
		}
	}

	return nil, nil
}

// FindPoolByName returns the first pool matching the given name on the
// specified load balancer, or nil if none exists.
func (c *Client) FindPoolByName(lbID, name string) (*Pool, error) {
	var pools []Pool

	err := c.retryWithBackoff("ListPools", func() error {
		return c.api.Get(c.regionPath("/loadbalancing/pool"), &pools)
	})
	if err != nil {
		return nil, fmt.Errorf("listing pools: %w", err)
	}

	for i := range pools {
		if pools[i].Name == name && pools[i].LoadBalancerID == lbID {
			return &pools[i], nil
		}
	}

	return nil, nil
}

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

// AddPoolMember adds a single member (instance) to a backend pool.
//
// Note: the OVH API accepts batch creation under {"members":[...]} and returns
// an array. We wrap a single member here for the common case; use
// AddPoolMembers for batch creation.
func (c *Client) AddPoolMember(poolID string, opts CreateMemberOpts) (*Member, error) {
	members, err := c.AddPoolMembers(poolID, []CreateMemberOpts{opts})
	if err != nil {
		return nil, err
	}

	if len(members) == 0 {
		return nil, fmt.Errorf("OVH returned no members after add to pool %s", poolID)
	}

	return &members[0], nil
}

// AddPoolMembers adds multiple members in a single API call.
func (c *Client) AddPoolMembers(poolID string, opts []CreateMemberOpts) ([]Member, error) {
	var members []Member

	body := addPoolMembersRequest{Members: opts}

	err := c.retryWithBackoff("AddPoolMembers", func() error {
		return c.api.Post(c.regionPath("/loadbalancing/pool/%s/member", poolID), body, &members)
	})
	if err != nil {
		return nil, fmt.Errorf("adding members to pool %s: %w", poolID, err)
	}

	return members, nil
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

// ListFloatingIPs returns all floating IPs in the current region.
func (c *Client) ListFloatingIPs() ([]FloatingIP, error) {
	var fips []FloatingIP

	err := c.retryWithBackoff("ListFloatingIPs", func() error {
		return c.api.Get(c.regionPath("/floatingip"), &fips)
	})
	if err != nil {
		return nil, fmt.Errorf("listing floating IPs: %w", err)
	}

	return fips, nil
}

// GetFloatingIP retrieves a floating IP by ID.
func (c *Client) GetFloatingIP(fipID string) (*FloatingIP, error) {
	var fip FloatingIP

	err := c.retryWithBackoff("GetFloatingIP", func() error {
		return c.api.Get(c.regionPath("/floatingip/%s", fipID), &fip)
	})
	if err != nil {
		return nil, fmt.Errorf("getting floating IP %s: %w", fipID, err)
	}

	return &fip, nil
}

// CreateFloatingIP creates a floating IP in the current region.
func (c *Client) CreateFloatingIP(opts CreateFloatingIPOpts) (*FloatingIP, error) {
	var fip FloatingIP

	err := c.retryWithBackoff("CreateFloatingIP", func() error {
		return c.api.Post(c.regionPath("/floatingIp"), opts, &fip)
	})
	if err != nil {
		return nil, fmt.Errorf("creating floating IP: %w", err)
	}

	return &fip, nil
}

// DeleteFloatingIP deletes a floating IP.
func (c *Client) DeleteFloatingIP(fipID string) error {
	err := c.retryWithBackoff("DeleteFloatingIP", func() error {
		return c.api.Delete(c.regionPath("/floatingIp/%s", fipID), nil)
	})
	if err != nil {
		if IsNotFound(err) {
			return nil
		}

		return fmt.Errorf("deleting floating IP %s: %w", fipID, err)
	}

	return nil
}
