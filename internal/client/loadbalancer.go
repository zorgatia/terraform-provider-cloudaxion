package client

import (
	"context"
	"net/http"
	"net/url"
)

// LoadBalancerTargetVM is the only documented target type.
const LoadBalancerTargetVM = "vm"

// ForwardingRule maps a port on the load balancer to a port on its targets.
//
// The load balancer is layer 4 and TCP only. Protocol and Settings are reported
// by the API but are not settable on create — there is no HTTP mode, no TLS
// termination and no health checking. TLS terminates behind it, in-cluster.
type ForwardingRule struct {
	UUID       string         `json:"uuid,omitempty"`
	SourcePort int            `json:"source_port"`
	TargetPort int            `json:"target_port"`
	Protocol   string         `json:"protocol,omitempty"`
	Settings   map[string]any `json:"settings,omitempty"`
	CreatedAt  string         `json:"created_at,omitempty"`
}

// LoadBalancerTarget is a backend behind the load balancer.
type LoadBalancerTarget struct {
	TargetUUID      string `json:"target_uuid"`
	TargetType      string `json:"target_type"`
	TargetIPAddress string `json:"target_ip_address,omitempty"`
	CreatedAt       string `json:"created_at,omitempty"`
}

// LoadBalancer distributes TCP connections across targets.
type LoadBalancer struct {
	UUID             string `json:"uuid"`
	DisplayName      string `json:"display_name"`
	NetworkUUID      string `json:"network_uuid"`
	BillingAccountID int    `json:"billing_account_id"`
	PrivateAddress   string `json:"private_address"`
	PublicAddress    string `json:"public_address"`
	IsDeleted        bool   `json:"is_deleted"`

	ForwardingRules []ForwardingRule     `json:"forwarding_rules"`
	Targets         []LoadBalancerTarget `json:"targets"`

	UserID    int    `json:"user_id"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// FindRule returns the forwarding rule matching a source/target port pair.
//
// Rule creation does not return the new rule's uuid, but deletion needs it, so
// callers re-read the load balancer and look the rule up by its ports.
func (lb *LoadBalancer) FindRule(sourcePort, targetPort int) *ForwardingRule {
	for i := range lb.ForwardingRules {
		rule := &lb.ForwardingRules[i]
		if rule.SourcePort == sourcePort && rule.TargetPort == targetPort {
			return rule
		}
	}
	return nil
}

// CreateLoadBalancerRequest describes a new load balancer and its initial
// rules and targets.
type CreateLoadBalancerRequest struct {
	DisplayName      string
	NetworkUUID      string
	BillingAccountID int
	ReservePublicIP  *bool
	Rules            []ForwardingRule
	Targets          []LoadBalancerTarget
}

// CreateLoadBalancer provisions a load balancer.
func (c *Client) CreateLoadBalancer(ctx context.Context, location string, req CreateLoadBalancerRequest) (*LoadBalancer, error) {
	body := map[string]any{}
	if req.DisplayName != "" {
		body["display_name"] = req.DisplayName
	}
	if req.NetworkUUID != "" {
		body["network_uuid"] = req.NetworkUUID
	}
	if req.BillingAccountID != 0 {
		body["billing_account_id"] = req.BillingAccountID
	}
	if req.ReservePublicIP != nil {
		body["reserve_public_ip"] = *req.ReservePublicIP
	}
	if len(req.Rules) > 0 {
		body["rules"] = portPairs(req.Rules)
	}
	if len(req.Targets) > 0 {
		body["targets"] = req.Targets
	}

	var out LoadBalancer
	err := c.Do(ctx, Request{
		Method:   http.MethodPost,
		Path:     "network/load_balancers",
		Scoped:   true,
		Location: location,
		JSON:     body,
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// GetLoadBalancer reads a load balancer with its rules and targets.
func (c *Client) GetLoadBalancer(ctx context.Context, location, uuid string) (*LoadBalancer, error) {
	var out LoadBalancer
	err := c.Do(ctx, Request{
		Method:   http.MethodGet,
		Path:     "network/load_balancers/" + url.PathEscape(uuid),
		Scoped:   true,
		Location: location,
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// ListLoadBalancers returns every load balancer in a location.
func (c *Client) ListLoadBalancers(ctx context.Context, location string) ([]LoadBalancer, error) {
	var out []LoadBalancer
	err := c.Do(ctx, Request{
		Method:   http.MethodGet,
		Path:     "network/load_balancers",
		Scoped:   true,
		Location: location,
	}, &out)
	return out, err
}

// RenameLoadBalancer changes the display name.
func (c *Client) RenameLoadBalancer(ctx context.Context, location, uuid, displayName string) (*LoadBalancer, error) {
	var out LoadBalancer
	err := c.Do(ctx, Request{
		Method:   http.MethodPatch,
		Path:     "network/load_balancers/" + url.PathEscape(uuid),
		Scoped:   true,
		Location: location,
		JSON:     map[string]string{"display_name": displayName},
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteLoadBalancer removes a load balancer.
func (c *Client) DeleteLoadBalancer(ctx context.Context, location, uuid string) error {
	return c.Do(ctx, Request{
		Method:   http.MethodDelete,
		Path:     "network/load_balancers/" + url.PathEscape(uuid),
		Scoped:   true,
		Location: location,
	}, nil)
}

// AddLoadBalancerTarget puts a backend behind a load balancer.
func (c *Client) AddLoadBalancerTarget(ctx context.Context, location, lbUUID, targetUUID, targetType string) (*LoadBalancerTarget, error) {
	if targetType == "" {
		targetType = LoadBalancerTargetVM
	}

	var out LoadBalancerTarget
	err := c.Do(ctx, Request{
		Method:   http.MethodPost,
		Path:     "network/load_balancers/" + url.PathEscape(lbUUID) + "/targets",
		Scoped:   true,
		Location: location,
		JSON: map[string]string{
			"target_uuid": targetUUID,
			"target_type": targetType,
		},
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// RemoveLoadBalancerTarget takes a backend out of a load balancer.
func (c *Client) RemoveLoadBalancerTarget(ctx context.Context, location, lbUUID, targetUUID string) error {
	return c.Do(ctx, Request{
		Method:   http.MethodDelete,
		Path:     "network/load_balancers/" + url.PathEscape(lbUUID) + "/targets/" + url.PathEscape(targetUUID),
		Scoped:   true,
		Location: location,
	}, nil)
}

// AddForwardingRule adds a port mapping.
//
// The response carries only the ports, not the new rule's uuid, so this returns
// the rule read back from the load balancer to give callers a usable identifier.
func (c *Client) AddForwardingRule(ctx context.Context, location, lbUUID string, sourcePort, targetPort int) (*ForwardingRule, error) {
	err := c.Do(ctx, Request{
		Method:   http.MethodPost,
		Path:     "network/load_balancers/" + url.PathEscape(lbUUID) + "/forwarding_rules",
		Scoped:   true,
		Location: location,
		JSON: map[string]int{
			"source_port": sourcePort,
			"target_port": targetPort,
		},
	}, nil)
	if err != nil {
		return nil, err
	}

	lb, err := c.GetLoadBalancer(ctx, location, lbUUID)
	if err != nil {
		return nil, err
	}
	if rule := lb.FindRule(sourcePort, targetPort); rule != nil {
		return rule, nil
	}
	// The rule was accepted but is not visible yet; report the ports so the
	// caller still has something to store.
	return &ForwardingRule{SourcePort: sourcePort, TargetPort: targetPort}, nil
}

// RemoveForwardingRule deletes a port mapping by its uuid.
func (c *Client) RemoveForwardingRule(ctx context.Context, location, lbUUID, ruleUUID string) error {
	return c.Do(ctx, Request{
		Method:   http.MethodDelete,
		Path:     "network/load_balancers/" + url.PathEscape(lbUUID) + "/forwarding_rules/" + url.PathEscape(ruleUUID),
		Scoped:   true,
		Location: location,
	}, nil)
}

// portPairs reduces rules to the two fields the create endpoint accepts, so
// server-assigned metadata is never echoed back.
func portPairs(rules []ForwardingRule) []map[string]int {
	out := make([]map[string]int, 0, len(rules))
	for _, rule := range rules {
		out = append(out, map[string]int{
			"source_port": rule.SourcePort,
			"target_port": rule.TargetPort,
		})
	}
	return out
}
