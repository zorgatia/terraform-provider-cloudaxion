package provider

import (
	"sort"
	"strconv"
	"strings"

	"github.com/zorgatia/terraform-provider-cloudaxion/internal/client"
)

// The firewall API does not preserve rule order: a create that sends SSH first
// and the outbound catch-all last can read back with the catch-all at index 0.
// Terraform compares list elements positionally, so writing the API's order into
// state fails the apply with "provider produced inconsistent result".
//
// Rules are genuinely unordered here — the API has no precedence concept, every
// rule is evaluated. Modelling them as a set would say that honestly, but a set
// whose elements carry a computed uuid makes the whole element unknown at plan
// time, which is worse to use. So the list stays, and the API's answer is
// re-sorted to match the order the configuration asked for.
//
// reorderFirewallRules returns the API's rules arranged to match `configured`,
// matching on content rather than on the server-assigned uuid, which the
// configuration never has. Rules the configuration does not mention keep their
// relative order and go at the end, so a rule added outside Terraform still
// shows up as drift rather than vanishing.
func reorderFirewallRules(configured []firewallRuleModel, fromAPI []client.FirewallRule) []client.FirewallRule {
	if len(configured) == 0 || len(fromAPI) == 0 {
		return fromAPI
	}

	// Bucket by content key: several rules can be identical in content, so each
	// key holds a queue rather than a single rule.
	buckets := make(map[string][]client.FirewallRule, len(fromAPI))
	order := make([]string, 0, len(fromAPI))
	for _, rule := range fromAPI {
		key := apiRuleKey(rule)
		if _, seen := buckets[key]; !seen {
			order = append(order, key)
		}
		buckets[key] = append(buckets[key], rule)
	}

	out := make([]client.FirewallRule, 0, len(fromAPI))
	for _, want := range configured {
		key := configuredRuleKey(want)
		queue := buckets[key]
		if len(queue) == 0 {
			// The configuration asked for a rule the API did not return. Leave a
			// gap rather than inventing one: the apply will surface it.
			continue
		}
		out = append(out, queue[0])
		buckets[key] = queue[1:]
	}

	// Anything left over is a rule the configuration did not describe.
	for _, key := range order {
		out = append(out, buckets[key]...)
	}

	return out
}

// apiRuleKey builds the content key for a rule as the API reports it.
func apiRuleKey(rule client.FirewallRule) string {
	return ruleKey(
		rule.Protocol, rule.Direction, rule.EndpointSpecType,
		rule.PortStart, rule.PortEnd, rule.EndpointSpec,
	)
}

// configuredRuleKey builds the same key from a configured rule, applying the
// normalisations the API performs so both sides compare equal:
//
//   - an omitted endpoint type means "any"
//   - an endpoint list is meaningless unless the type is ip_prefixes
//   - an omitted port_end is stored as port_start
func configuredRuleKey(rule firewallRuleModel) string {
	specType := rule.EndpointSpecType.ValueString()
	if specType == "" {
		specType = client.EndpointSpecAny
	}

	var spec []string
	if specType == client.EndpointSpecIPPrefixes && !rule.EndpointSpec.IsNull() && !rule.EndpointSpec.IsUnknown() {
		for _, element := range rule.EndpointSpec.Elements() {
			spec = append(spec, strings.Trim(element.String(), `"`))
		}
	}

	portStart := int64PtrOrNil(rule.PortStart)
	portEnd := int64PtrOrNil(rule.PortEnd)
	if portEnd == nil {
		portEnd = portStart
	}

	return ruleKey(
		rule.Protocol.ValueString(), rule.Direction.ValueString(), specType,
		portStart, portEnd, spec,
	)
}

func ruleKey(protocol, direction, specType string, portStart, portEnd *int, spec []string) string {
	if specType != client.EndpointSpecIPPrefixes {
		spec = nil
	}

	// The endpoint list is a set on the wire, so sort a copy before hashing it
	// into the key. Sorting in place would reorder the caller's slice.
	sorted := append([]string(nil), spec...)
	sort.Strings(sorted)

	var b strings.Builder
	b.WriteString(strings.ToLower(protocol))
	b.WriteByte('|')
	b.WriteString(strings.ToLower(direction))
	b.WriteByte('|')
	b.WriteString(strings.ToLower(specType))
	b.WriteByte('|')
	b.WriteString(portString(portStart))
	b.WriteByte('|')
	b.WriteString(portString(portEnd))
	b.WriteByte('|')
	b.WriteString(strings.Join(sorted, ","))
	return b.String()
}

func portString(port *int) string {
	if port == nil {
		return "*"
	}
	return strconv.Itoa(*port)
}
