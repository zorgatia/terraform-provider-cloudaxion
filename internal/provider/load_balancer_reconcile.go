package provider

import (
	"context"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/diag"

	"github.com/zorgatia/terraform-provider-cloudaxion/internal/client"
)

// The load balancer has no bulk-replace endpoint for its rules or targets: each
// is added and removed one at a time. Updates therefore diff the API's current
// contents against the plan and issue only the differences, which keeps
// unchanged rules from being torn down and recreated — a rebuild would drop
// live connections.

// lbRulesToAPI converts configured rule blocks into API rules.
func lbRulesToAPI(rules []lbRuleModel) []client.ForwardingRule {
	out := make([]client.ForwardingRule, 0, len(rules))
	for _, rule := range rules {
		out = append(out, client.ForwardingRule{
			SourcePort: int(rule.SourcePort.ValueInt64()),
			TargetPort: int(rule.TargetPort.ValueInt64()),
		})
	}
	return out
}

// lbTargetsToAPI converts configured target blocks into API targets.
func lbTargetsToAPI(targets []lbTargetModel) []client.LoadBalancerTarget {
	out := make([]client.LoadBalancerTarget, 0, len(targets))
	for _, target := range targets {
		targetType := target.Type.ValueString()
		if targetType == "" {
			targetType = client.LoadBalancerTargetVM
		}
		out = append(out, client.LoadBalancerTarget{
			TargetUUID: target.ID.ValueString(),
			TargetType: targetType,
		})
	}
	return out
}

// rulePortKey identifies a rule by its port pair, which is the only stable
// handle available: rule UUIDs are assigned by the API and absent from the
// create response.
func rulePortKey(sourcePort, targetPort int) string {
	return strconv.Itoa(sourcePort) + "->" + strconv.Itoa(targetPort)
}

// reconcileRules adds rules the plan introduces and removes those it drops,
// leaving untouched rules alone.
func (r *loadBalancerResource) reconcileRules(
	ctx context.Context, location, uuid string,
	current *client.LoadBalancer, planned []lbRuleModel,
) diag.Diagnostics {
	var diags diag.Diagnostics

	existing := make(map[string]client.ForwardingRule, len(current.ForwardingRules))
	for _, rule := range current.ForwardingRules {
		existing[rulePortKey(rule.SourcePort, rule.TargetPort)] = rule
	}

	wanted := make(map[string]lbRuleModel, len(planned))
	for _, rule := range planned {
		wanted[rulePortKey(int(rule.SourcePort.ValueInt64()), int(rule.TargetPort.ValueInt64()))] = rule
	}

	for key, rule := range wanted {
		if _, present := existing[key]; present {
			continue
		}
		_, err := r.meta.Client.AddForwardingRule(ctx, location, uuid,
			int(rule.SourcePort.ValueInt64()), int(rule.TargetPort.ValueInt64()))
		if err != nil {
			diags.AddError("Unable to add a forwarding rule", client.DescribeError(err))
			return diags
		}
	}

	for key, rule := range existing {
		if _, present := wanted[key]; present {
			continue
		}
		if rule.UUID == "" {
			// Deletion is by UUID, and the API did not report one. Say so rather
			// than silently leaving a rule the configuration no longer wants.
			diags.AddWarning(
				"A forwarding rule could not be removed",
				"The rule "+key+" is no longer in the configuration, but the API did not "+
					"report a UUID for it, and rules can only be deleted by UUID. "+
					"Remove it in the CloudAxion console.",
			)
			continue
		}
		if err := r.meta.Client.RemoveForwardingRule(ctx, location, uuid, rule.UUID); err != nil {
			if client.IsNotFound(err) {
				continue
			}
			diags.AddError("Unable to remove a forwarding rule", client.DescribeError(err))
			return diags
		}
	}

	return diags
}

// reconcileTargets adds and removes backends to match the plan.
func (r *loadBalancerResource) reconcileTargets(
	ctx context.Context, location, uuid string,
	current *client.LoadBalancer, planned []lbTargetModel,
) diag.Diagnostics {
	var diags diag.Diagnostics

	existing := make(map[string]struct{}, len(current.Targets))
	for _, target := range current.Targets {
		existing[target.TargetUUID] = struct{}{}
	}

	wanted := make(map[string]lbTargetModel, len(planned))
	for _, target := range planned {
		wanted[target.ID.ValueString()] = target
	}

	for targetUUID, target := range wanted {
		if _, present := existing[targetUUID]; present {
			continue
		}
		if _, err := r.meta.Client.AddLoadBalancerTarget(
			ctx, location, uuid, targetUUID, target.Type.ValueString(),
		); err != nil {
			diags.AddError("Unable to add a load balancer target", client.DescribeError(err))
			return diags
		}
	}

	for targetUUID := range existing {
		if _, present := wanted[targetUUID]; present {
			continue
		}
		if err := r.meta.Client.RemoveLoadBalancerTarget(ctx, location, uuid, targetUUID); err != nil {
			if client.IsNotFound(err) {
				continue
			}
			diags.AddError("Unable to remove a load balancer target", client.DescribeError(err))
			return diags
		}
	}

	return diags
}
