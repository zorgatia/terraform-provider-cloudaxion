package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
)

// Firewall rule directions and endpoint specification types.
const (
	DirectionInbound  = "inbound"
	DirectionOutbound = "outbound"

	EndpointSpecAny        = "any"
	EndpointSpecIPPrefixes = "ip_prefixes"
)

// FirewallRule filters traffic for the VMs a firewall is attached to.
//
// PortStart may be nil, meaning every port. A nil PortEnd implies it equals
// PortStart. EndpointSpec holds IP addresses or CIDR blocks and is only
// meaningful when EndpointSpecType is "ip_prefixes".
type FirewallRule struct {
	UUID             string   `json:"uuid,omitempty"`
	Protocol         string   `json:"protocol"`
	Direction        string   `json:"direction"`
	PortStart        *int     `json:"port_start"`
	PortEnd          *int     `json:"port_end"`
	EndpointSpecType string   `json:"endpoint_spec_type"`
	EndpointSpec     []string `json:"endpoint_spec,omitempty"`
}

// Firewall is a named set of rules that can be attached to VMs.
type Firewall struct {
	UUID              string         `json:"uuid"`
	DisplayName       string         `json:"display_name"`
	Description       string         `json:"description"`
	BillingAccountID  int            `json:"billing_account_id"`
	Rules             []FirewallRule `json:"rules"`
	ResourcesAssigned []string       `json:"-"`
	UserID            int            `json:"user_id"`
	CreatedAt         string         `json:"created_at"`
	DeletedAt         string         `json:"deleted_at"`
}

// CreateFirewall creates a firewall with its initial rule set.
func (c *Client) CreateFirewall(ctx context.Context, location, displayName string, billingAccountID int, rules []FirewallRule) (*Firewall, error) {
	body := map[string]any{
		"display_name":       displayName,
		"billing_account_id": billingAccountID,
		"rules":              normaliseRules(rules),
	}

	var out Firewall
	err := c.Do(ctx, Request{
		Method:   http.MethodPost,
		Path:     "network/firewalls",
		Scoped:   true,
		Location: location,
		JSON:     body,
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// GetFirewall reads one firewall.
//
// The API documents no single-firewall GET, so this filters the list. Without
// pagination that is one request either way.
func (c *Client) GetFirewall(ctx context.Context, location, uuid string) (*Firewall, error) {
	firewalls, err := c.ListFirewalls(ctx, location)
	if err != nil {
		return nil, err
	}
	for i := range firewalls {
		if firewalls[i].UUID == uuid {
			return &firewalls[i], nil
		}
	}
	return nil, &APIError{
		StatusCode: http.StatusNotFound,
		Message:    "firewall " + uuid + " not found",
	}
}

// ListFirewalls returns every firewall in a location.
func (c *Client) ListFirewalls(ctx context.Context, location string) ([]Firewall, error) {
	var out []Firewall
	err := c.Do(ctx, Request{
		Method:   http.MethodGet,
		Path:     "network/firewalls",
		Scoped:   true,
		Location: location,
	}, &out)
	return out, err
}

// UpdateFirewall replaces a firewall's name, description and rules.
//
// The rule list is a full replacement, not a merge: rules absent from the
// request are removed. Note the field name changes between create and update —
// create takes display_name, update takes name.
func (c *Client) UpdateFirewall(ctx context.Context, location, uuid, name, description string, rules []FirewallRule) (*Firewall, error) {
	body := map[string]any{
		"name":  name,
		"rules": normaliseRules(rules),
	}
	if description != "" {
		body["description"] = description
	}

	var out Firewall
	err := c.Do(ctx, Request{
		Method:   http.MethodPut,
		Path:     "network/firewalls/" + url.PathEscape(uuid),
		Scoped:   true,
		Location: location,
		JSON:     body,
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteFirewall removes a firewall.
func (c *Client) DeleteFirewall(ctx context.Context, location, uuid string) error {
	return c.Do(ctx, Request{
		Method:   http.MethodDelete,
		Path:     "network/firewalls/" + url.PathEscape(uuid),
		Scoped:   true,
		Location: location,
	}, nil)
}

// AttachFirewallToVM applies a firewall to a virtual machine.
func (c *Client) AttachFirewallToVM(ctx context.Context, location, firewallUUID, vmUUID string) error {
	return c.firewallVMLink(ctx, http.MethodPost, location, firewallUUID, vmUUID)
}

// DetachFirewallFromVM removes a firewall from a virtual machine.
func (c *Client) DetachFirewallFromVM(ctx context.Context, location, firewallUUID, vmUUID string) error {
	return c.firewallVMLink(ctx, http.MethodDelete, location, firewallUUID, vmUUID)
}

func (c *Client) firewallVMLink(ctx context.Context, method, location, firewallUUID, vmUUID string) error {
	q := url.Values{}
	q.Set("vm_uuid", vmUUID)

	return c.Do(ctx, Request{
		Method:   method,
		Path:     "network/firewalls/" + url.PathEscape(firewallUUID) + "/vms",
		Scoped:   true,
		Location: location,
		Query:    q,
	}, nil)
}

// normaliseRules strips server-assigned rule identifiers before sending, and
// applies the documented default that an omitted endpoint specification means
// "any". Sending a stale uuid back on update is not part of the contract.
func normaliseRules(rules []FirewallRule) []FirewallRule {
	out := make([]FirewallRule, 0, len(rules))
	for _, rule := range rules {
		rule.UUID = ""
		if rule.EndpointSpecType == "" {
			rule.EndpointSpecType = EndpointSpecAny
		}
		if rule.EndpointSpecType == EndpointSpecAny {
			rule.EndpointSpec = nil
		}
		out = append(out, rule)
	}
	return out
}

// UnmarshalJSON handles resources_assigned, whose element type the API
// documentation leaves unspecified.
func (f *Firewall) UnmarshalJSON(data []byte) error {
	type alias Firewall
	aux := struct {
		ResourcesAssigned json.RawMessage `json:"resources_assigned"`
		*alias
	}{alias: (*alias)(f)}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	f.ResourcesAssigned = decodeUUIDList(aux.ResourcesAssigned)
	return nil
}
