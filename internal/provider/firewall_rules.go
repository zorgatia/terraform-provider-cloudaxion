package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/zorgatia/terraform-provider-cloudaxion/internal/client"
)

// firewallRulesToAPI converts configured rule blocks into API rules.
//
// Ports are pointers on the wire because null is meaningful: a null port_start
// means every port, and a null port_end means the range is a single port. Zero
// would be a valid-looking but wrong port number, so the distinction is kept.
func firewallRulesToAPI(ctx context.Context, rules []firewallRuleModel) ([]client.FirewallRule, diag.Diagnostics) {
	var diags diag.Diagnostics

	out := make([]client.FirewallRule, 0, len(rules))
	for _, rule := range rules {
		spec, d := stringSlice(ctx, rule.EndpointSpec)
		diags.Append(d...)
		if diags.HasError() {
			return nil, diags
		}

		specType := rule.EndpointSpecType.ValueString()
		if specType == "" {
			specType = client.EndpointSpecAny
		}

		out = append(out, client.FirewallRule{
			Protocol:         rule.Protocol.ValueString(),
			Direction:        rule.Direction.ValueString(),
			PortStart:        int64PtrOrNil(rule.PortStart),
			PortEnd:          int64PtrOrNil(rule.PortEnd),
			EndpointSpecType: specType,
			EndpointSpec:     spec,
		})
	}
	return out, diags
}

// firewallRulesFromAPI converts API rules back into configuration blocks.
func firewallRulesFromAPI(ctx context.Context, rules []client.FirewallRule) ([]firewallRuleModel, diag.Diagnostics) {
	var diags diag.Diagnostics

	out := make([]firewallRuleModel, 0, len(rules))
	for _, rule := range rules {
		spec := types.ListNull(types.StringType)
		if len(rule.EndpointSpec) > 0 {
			list, d := types.ListValueFrom(ctx, types.StringType, rule.EndpointSpec)
			diags.Append(d...)
			if diags.HasError() {
				return nil, diags
			}
			spec = list
		}

		out = append(out, firewallRuleModel{
			Protocol:         types.StringValue(rule.Protocol),
			Direction:        types.StringValue(rule.Direction),
			PortStart:        int64OrNull(rule.PortStart),
			PortEnd:          int64OrNull(rule.PortEnd),
			EndpointSpecType: types.StringValue(rule.EndpointSpecType),
			EndpointSpec:     spec,
			UUID:             nullableString(rule.UUID),
		})
	}
	return out, diags
}

// int64PtrOrNil converts a Terraform int to a pointer, preserving null.
func int64PtrOrNil(value types.Int64) *int {
	if value.IsNull() || value.IsUnknown() {
		return nil
	}
	v := int(value.ValueInt64())
	return &v
}

// int64OrNull converts an API pointer back to a Terraform int, preserving null.
func int64OrNull(value *int) types.Int64 {
	if value == nil {
		return types.Int64Null()
	}
	return types.Int64Value(int64(*value))
}
