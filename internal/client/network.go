package client

import (
	"context"
	"net/http"
	"net/url"
)

// Resource types a floating IP can be attached to.
const (
	FloatingIPTargetVM           = "virtual_machine"
	FloatingIPTargetService      = "service"
	FloatingIPTargetLoadBalancer = "load_balancer"
)

// PrivateNetwork is a private network.
//
// Subnet is allocated by CloudAxion and cannot be requested: there is no IPAM or
// CIDR control in the API. Consumers needing the address range must read it back
// after creation.
//
// VLANID is a pointer because the live API does not return vlan_id at all —
// verified 2026-08-26 against nine networks, none of which carried the field,
// although the published documentation shows it. A plain int would silently
// report 0, which reads as a real VLAN. Nil means "not reported".
type PrivateNetwork struct {
	UUID           string   `json:"uuid"`
	Name           string   `json:"name"`
	Type           string   `json:"type"`
	VLANID         *int     `json:"vlan_id"`
	Subnet         string   `json:"subnet"`
	SubnetIPv6     string   `json:"subnet_ipv6"`
	IsDefault      bool     `json:"is_default"`
	VMUUIDs        []string `json:"vm_uuids"`
	ResourcesCount int      `json:"resources_count"`
	CreatedAt      string   `json:"created_at"`
	UpdatedAt      string   `json:"updated_at"`
}

// CreatePrivateNetwork creates a private network.
//
// The name travels as a query parameter; this endpoint takes no request body.
// The first network an account creates becomes its default network.
func (c *Client) CreatePrivateNetwork(ctx context.Context, location, name string) (*PrivateNetwork, error) {
	q := url.Values{}
	if name != "" {
		q.Set("name", name)
	}

	var out PrivateNetwork
	err := c.Do(ctx, Request{
		Method:   http.MethodPost,
		Path:     "network/network",
		Scoped:   true,
		Location: location,
		Query:    q,
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// GetPrivateNetwork reads a private network by uuid.
func (c *Client) GetPrivateNetwork(ctx context.Context, location, uuid string) (*PrivateNetwork, error) {
	var out PrivateNetwork
	err := c.Do(ctx, Request{
		Method:   http.MethodGet,
		Path:     "network/network/" + url.PathEscape(uuid),
		Scoped:   true,
		Location: location,
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// ListPrivateNetworks returns every private network in a location.
func (c *Client) ListPrivateNetworks(ctx context.Context, location string) ([]PrivateNetwork, error) {
	var out []PrivateNetwork
	err := c.Do(ctx, Request{
		Method:   http.MethodGet,
		Path:     "network/networks",
		Scoped:   true,
		Location: location,
	}, &out)
	return out, err
}

// RenamePrivateNetwork changes a network's descriptive name.
func (c *Client) RenamePrivateNetwork(ctx context.Context, location, uuid, name string) (*PrivateNetwork, error) {
	var out PrivateNetwork
	err := c.Do(ctx, Request{
		Method:   http.MethodPatch,
		Path:     "network/network/" + url.PathEscape(uuid),
		Scoped:   true,
		Location: location,
		JSON:     map[string]string{"name": name},
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// SetDefaultPrivateNetwork marks a network as the account default for its location.
func (c *Client) SetDefaultPrivateNetwork(ctx context.Context, location, uuid string) error {
	return c.Do(ctx, Request{
		Method:   http.MethodPut,
		Path:     "network/network/" + url.PathEscape(uuid) + "/default",
		Scoped:   true,
		Location: location,
	}, nil)
}

// DeletePrivateNetwork removes a private network.
func (c *Client) DeletePrivateNetwork(ctx context.Context, location, uuid string) error {
	return c.Do(ctx, Request{
		Method:   http.MethodDelete,
		Path:     "network/network/" + url.PathEscape(uuid),
		Scoped:   true,
		Location: location,
	}, nil)
}

// FloatingIP is a reservable public address.
//
// Floating IPs are addressed by their IPv4 address rather than by a uuid, so
// Address is the identifier for every subsequent call.
type FloatingIP struct {
	ID               int    `json:"id"`
	Address          string `json:"address"`
	Name             string `json:"name"`
	Type             string `json:"type"`
	BillingAccountID int    `json:"billing_account_id"`
	Enabled          bool   `json:"enabled"`
	IsDeleted        bool   `json:"is_deleted"`
	IsVirtual        bool   `json:"is_virtual"`

	AssignedTo             string `json:"assigned_to"`
	AssignedToResourceType string `json:"assigned_to_resource_type"`
	AssignedToPrivateIP    string `json:"assigned_to_private_ip"`

	UserID    int    `json:"user_id"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// CreateFloatingIP reserves a public address.
func (c *Client) CreateFloatingIP(ctx context.Context, location, name string, billingAccountID int) (*FloatingIP, error) {
	body := map[string]any{"billing_account_id": billingAccountID}
	if name != "" {
		body["name"] = name
	}

	var out FloatingIP
	err := c.Do(ctx, Request{
		Method:   http.MethodPost,
		Path:     "network/ip_addresses",
		Scoped:   true,
		Location: location,
		JSON:     body,
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// GetFloatingIP reads a floating IP by address.
func (c *Client) GetFloatingIP(ctx context.Context, location, address string) (*FloatingIP, error) {
	var out FloatingIP
	err := c.Do(ctx, Request{
		Method:   http.MethodGet,
		Path:     "network/ip_addresses/" + url.PathEscape(address),
		Scoped:   true,
		Location: location,
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// ListFloatingIPs returns every floating IP in a location.
func (c *Client) ListFloatingIPs(ctx context.Context, location string) ([]FloatingIP, error) {
	var out []FloatingIP
	err := c.Do(ctx, Request{
		Method:   http.MethodGet,
		Path:     "network/ip_addresses",
		Scoped:   true,
		Location: location,
	}, &out)
	return out, err
}

// RenameFloatingIP changes a floating IP's descriptive name.
func (c *Client) RenameFloatingIP(ctx context.Context, location, address, name string) (*FloatingIP, error) {
	var out FloatingIP
	err := c.Do(ctx, Request{
		Method:   http.MethodPatch,
		Path:     "network/ip_addresses/" + url.PathEscape(address),
		Scoped:   true,
		Location: location,
		JSON:     map[string]string{"name": name},
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteFloatingIP releases a reserved address.
func (c *Client) DeleteFloatingIP(ctx context.Context, location, address string) error {
	return c.Do(ctx, Request{
		Method:   http.MethodDelete,
		Path:     "network/ip_addresses/" + url.PathEscape(address),
		Scoped:   true,
		Location: location,
	}, nil)
}

// AssignFloatingIP attaches an address to a resource. resourceType is one of
// the FloatingIPTarget constants.
func (c *Client) AssignFloatingIP(ctx context.Context, location, address, resourceUUID, resourceType string) (*FloatingIP, error) {
	var out FloatingIP
	err := c.Do(ctx, Request{
		Method:   http.MethodPost,
		Path:     "network/ip_addresses/" + url.PathEscape(address) + "/assign",
		Scoped:   true,
		Location: location,
		JSON: map[string]string{
			"assigned_to":               resourceUUID,
			"assigned_to_resource_type": resourceType,
		},
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// UnassignFloatingIP detaches an address from whatever holds it.
func (c *Client) UnassignFloatingIP(ctx context.Context, location, address string) error {
	return c.Do(ctx, Request{
		Method:   http.MethodPost,
		Path:     "network/ip_addresses/" + url.PathEscape(address) + "/unassign",
		Scoped:   true,
		Location: location,
	}, nil)
}
