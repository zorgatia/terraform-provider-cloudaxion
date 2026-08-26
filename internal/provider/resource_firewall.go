package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/zorgatia/terraform-provider-cloudaxion/internal/client"
)

var (
	_ resource.Resource                = (*firewallResource)(nil)
	_ resource.ResourceWithConfigure   = (*firewallResource)(nil)
	_ resource.ResourceWithImportState = (*firewallResource)(nil)
)

// NewFirewallResource returns the cloudaxion_firewall resource.
func NewFirewallResource() resource.Resource {
	return &firewallResource{}
}

type firewallResource struct {
	meta *Meta
}

type firewallRuleModel struct {
	Protocol         types.String `tfsdk:"protocol"`
	Direction        types.String `tfsdk:"direction"`
	PortStart        types.Int64  `tfsdk:"port_start"`
	PortEnd          types.Int64  `tfsdk:"port_end"`
	EndpointSpecType types.String `tfsdk:"endpoint_spec_type"`
	EndpointSpec     types.List   `tfsdk:"endpoint_spec"`
	UUID             types.String `tfsdk:"uuid"`
}

type firewallModel struct {
	ID               types.String `tfsdk:"id"`
	Name             types.String `tfsdk:"name"`
	Description      types.String `tfsdk:"description"`
	Location         types.String `tfsdk:"location"`
	BillingAccountID types.Int64  `tfsdk:"billing_account_id"`

	Rules []firewallRuleModel `tfsdk:"rule"`

	CreatedAt types.String `tfsdk:"created_at"`
}

func (r *firewallResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_firewall"
}

func (r *firewallResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A stateful L4 firewall, attachable to virtual machines with " +
			"`cloudaxion_firewall_attachment`.\n\n" +
			"Rules are declared inline. The API replaces the whole rule set on update, so the " +
			"`rule` blocks are the complete definition — a rule removed from configuration is " +
			"removed from the firewall.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Firewall UUID.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Name of the firewall.",
			},
			"description": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Free-text description.",
			},
			"location": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Location slug. Defaults to the provider's `location`.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"billing_account_id": schema.Int64Attribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Billing account. Defaults to the provider's `billing_account_id`.",
			},
			"created_at": computedString("Creation timestamp reported by the API."),
		},
		Blocks: map[string]schema.Block{
			"rule": schema.ListNestedBlock{
				MarkdownDescription: "A traffic rule. Order is not significant to the API.",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"protocol": schema.StringAttribute{
							Required:            true,
							MarkdownDescription: "Protocol: `tcp`, `udp` or `icmp`.",
							Validators:          []validator.String{stringvalidator.OneOf("tcp", "udp", "icmp")},
						},
						"direction": schema.StringAttribute{
							Required:            true,
							MarkdownDescription: "Traffic direction: `inbound` or `outbound`.",
							Validators: []validator.String{
								stringvalidator.OneOf(client.DirectionInbound, client.DirectionOutbound),
							},
						},
						"port_start": schema.Int64Attribute{
							Optional: true,
							MarkdownDescription: "First port in the range, 1–65535. " +
								"**Omit to match every port** — that is what the API means by a null value.",
							Validators: []validator.Int64{int64validator.Between(1, 65535)},
						},
						"port_end": schema.Int64Attribute{
							Optional: true,
							Computed: true,
							MarkdownDescription: "Last port in the range. Omit for a single port: " +
								"the API then **normalises it to `port_start`** and returns that, " +
								"which is why this attribute is computed as well as optional.",
							Validators: []validator.Int64{int64validator.Between(1, 65535)},
						},
						"endpoint_spec_type": schema.StringAttribute{
							Optional: true,
							Computed: true,
							Default:  stringdefault.StaticString(client.EndpointSpecAny),
							MarkdownDescription: "`any` to match every address, or `ip_prefixes` to " +
								"restrict to the addresses in `endpoint_spec`.",
							Validators: []validator.String{
								stringvalidator.OneOf(client.EndpointSpecAny, client.EndpointSpecIPPrefixes),
							},
						},
						"endpoint_spec": schema.ListAttribute{
							Optional:    true,
							ElementType: types.StringType,
							MarkdownDescription: "IP addresses or CIDR blocks the rule applies to. " +
								"Only meaningful when `endpoint_spec_type` is `ip_prefixes`; " +
								"ignored otherwise.",
						},
						"uuid": computedString("Rule UUID assigned by the API."),
					},
				},
			},
		},
	}
}

func (r *firewallResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	meta, errDetail := metaFromProviderData(req.ProviderData)
	if errDetail != "" {
		resp.Diagnostics.AddError("Unexpected provider data", errDetail)
		return
	}
	r.meta = meta
}

func (r *firewallResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan firewallModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	location := resolveLocation(plan.Location, r.meta)

	billingAccountID, diags := resolveBillingAccount(plan.BillingAccountID, r.meta, "billing_account_id")
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	rules, diags := firewallRulesToAPI(ctx, plan.Rules)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	firewall, err := r.meta.Client.CreateFirewall(ctx, location, plan.Name.ValueString(), billingAccountID, rules)
	if err != nil {
		resp.Diagnostics.AddError("Unable to create the firewall", client.DescribeError(err))
		return
	}

	r.apply(ctx, &plan, firewall, location, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *firewallResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state firewallModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	location := resolveLocation(state.Location, r.meta)

	firewall, err := r.meta.Client.GetFirewall(ctx, location, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Unable to read the firewall", client.DescribeError(err))
		return
	}

	r.apply(ctx, &state, firewall, location, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *firewallResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state firewallModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	location := resolveLocation(state.Location, r.meta)

	rules, diags := firewallRulesToAPI(ctx, plan.Rules)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The API replaces the entire rule set, so the plan's rules are sent whole.
	firewall, err := r.meta.Client.UpdateFirewall(
		ctx, location, state.ID.ValueString(),
		plan.Name.ValueString(), plan.Description.ValueString(), rules,
	)
	if err != nil {
		resp.Diagnostics.AddError("Unable to update the firewall", client.DescribeError(err))
		return
	}

	plan.ID = state.ID
	r.apply(ctx, &plan, firewall, location, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *firewallResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state firewallModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	location := resolveLocation(state.Location, r.meta)

	if err := r.meta.Client.DeleteFirewall(ctx, location, state.ID.ValueString()); err != nil {
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Unable to delete the firewall", client.DescribeError(err))
	}
}

func (r *firewallResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(applyImportID(ctx, req.ID, "id", &resp.State)...)
}

func (r *firewallResource) apply(ctx context.Context, model *firewallModel, firewall *client.Firewall, location string, diags *diag.Diagnostics) {
	model.ID = types.StringValue(firewall.UUID)
	model.Name = types.StringValue(firewall.DisplayName)
	model.Location = types.StringValue(location)
	model.BillingAccountID = types.Int64Value(int64(firewall.BillingAccountID))
	model.CreatedAt = nullableString(firewall.CreatedAt)
	if firewall.Description != "" {
		model.Description = types.StringValue(firewall.Description)
	}

	// The API returns rules in its own order, so line them up with the order the
	// configuration asked for before writing state.
	ordered := reorderFirewallRules(model.Rules, firewall.Rules)

	rules, d := firewallRulesFromAPI(ctx, ordered)
	diags.Append(d...)
	model.Rules = rules
}
