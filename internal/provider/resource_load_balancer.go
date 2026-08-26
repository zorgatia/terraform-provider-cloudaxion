package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/zorgatia/terraform-provider-cloudaxion/internal/client"
)

var (
	_ resource.Resource                = (*loadBalancerResource)(nil)
	_ resource.ResourceWithConfigure   = (*loadBalancerResource)(nil)
	_ resource.ResourceWithImportState = (*loadBalancerResource)(nil)
)

// NewLoadBalancerResource returns the cloudaxion_load_balancer resource.
func NewLoadBalancerResource() resource.Resource {
	return &loadBalancerResource{}
}

type loadBalancerResource struct{ meta *Meta }

type lbRuleModel struct {
	SourcePort types.Int64  `tfsdk:"source_port"`
	TargetPort types.Int64  `tfsdk:"target_port"`
	UUID       types.String `tfsdk:"uuid"`
}

type lbTargetModel struct {
	ID        types.String `tfsdk:"id"`
	Type      types.String `tfsdk:"type"`
	IPAddress types.String `tfsdk:"ip_address"`
}

type loadBalancerModel struct {
	ID               types.String `tfsdk:"id"`
	Name             types.String `tfsdk:"name"`
	NetworkID        types.String `tfsdk:"network_id"`
	Location         types.String `tfsdk:"location"`
	BillingAccountID types.Int64  `tfsdk:"billing_account_id"`
	ReservePublicIP  types.Bool   `tfsdk:"reserve_public_ip"`

	Rules   []lbRuleModel   `tfsdk:"rule"`
	Targets []lbTargetModel `tfsdk:"target"`

	PrivateAddress types.String `tfsdk:"private_address"`
	PublicAddress  types.String `tfsdk:"public_address"`
	CreatedAt      types.String `tfsdk:"created_at"`
}

func (r *loadBalancerResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_load_balancer"
}

func (r *loadBalancerResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A layer 4 network load balancer.\n\n" +
			"~> **TCP only.** The API offers no HTTP mode, no TLS termination and no health " +
			"checks. In a Kubernetes cell this is the plain front door: terminate TLS in-cluster " +
			"(Envoy Gateway or similar) and point the load balancer at the ingress node ports.\n\n" +
			"Rules and targets are declared inline and reconciled against the API on update.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Load balancer UUID.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Display name.",
			},
			"network_id": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Private network the load balancer sits in.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
					stringplanmodifier.UseStateForUnknown(),
				},
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
			"reserve_public_ip": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(true),
				MarkdownDescription: "Whether to reserve a public address for the load balancer. " +
					"On import this is inferred from whether `public_address` is set, since the " +
					"API does not report the original request.",
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.RequiresReplace()},
			},
			"private_address": computedString("Address of the load balancer on its private network."),
			"public_address":  computedString("Public address, when one is reserved."),
			"created_at":      computedString("Creation timestamp reported by the API."),
		},
		Blocks: map[string]schema.Block{
			"rule": schema.ListNestedBlock{
				MarkdownDescription: "A TCP port mapping from the load balancer to its targets.",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"source_port": schema.Int64Attribute{
							Required:            true,
							MarkdownDescription: "Port the load balancer listens on.",
							Validators:          []validator.Int64{int64validator.Between(1, 65535)},
						},
						"target_port": schema.Int64Attribute{
							Required:            true,
							MarkdownDescription: "Port on the targets to forward to.",
							Validators:          []validator.Int64{int64validator.Between(1, 65535)},
						},
						"uuid": computedString(
							"Rule UUID. Assigned by the API and only visible after a read — " +
								"creating a rule does not return it."),
					},
				},
			},
			"target": schema.ListNestedBlock{
				MarkdownDescription: "A backend behind the load balancer.",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							Required:            true,
							MarkdownDescription: "UUID of the backend, normally a virtual machine.",
						},
						"type": schema.StringAttribute{
							Optional:            true,
							Computed:            true,
							MarkdownDescription: "Backend kind. `vm` is the only documented value.",
						},
						"ip_address": computedString("Private address of the backend."),
					},
				},
			},
		},
	}
}

func (r *loadBalancerResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	meta, errDetail := metaFromProviderData(req.ProviderData)
	if errDetail != "" {
		resp.Diagnostics.AddError("Unexpected provider data", errDetail)
		return
	}
	r.meta = meta
}

func (r *loadBalancerResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan loadBalancerModel
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

	reservePublicIP := plan.ReservePublicIP.ValueBool()

	lb, err := r.meta.Client.CreateLoadBalancer(ctx, location, client.CreateLoadBalancerRequest{
		DisplayName:      plan.Name.ValueString(),
		NetworkUUID:      plan.NetworkID.ValueString(),
		BillingAccountID: billingAccountID,
		ReservePublicIP:  &reservePublicIP,
		Rules:            lbRulesToAPI(plan.Rules),
		Targets:          lbTargetsToAPI(plan.Targets),
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to create the load balancer", client.DescribeError(err))
		return
	}

	// Re-read so rule UUIDs are populated: the create response does not carry them.
	if refreshed, err := r.meta.Client.GetLoadBalancer(ctx, location, lb.UUID); err == nil {
		lb = refreshed
	}

	r.apply(&plan, lb, location)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *loadBalancerResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state loadBalancerModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	location := resolveLocation(state.Location, r.meta)

	lb, err := r.meta.Client.GetLoadBalancer(ctx, location, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Unable to read the load balancer", client.DescribeError(err))
		return
	}

	r.apply(&state, lb, location)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *loadBalancerResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state loadBalancerModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	location := resolveLocation(state.Location, r.meta)
	uuid := state.ID.ValueString()

	if !plan.Name.Equal(state.Name) {
		if _, err := r.meta.Client.RenameLoadBalancer(ctx, location, uuid, plan.Name.ValueString()); err != nil {
			resp.Diagnostics.AddError("Unable to rename the load balancer", client.DescribeError(err))
			return
		}
	}

	// Rules and targets have no bulk-replace endpoint, so reconcile by diffing
	// what the API currently holds against what the plan asks for.
	current, err := r.meta.Client.GetLoadBalancer(ctx, location, uuid)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read the load balancer before updating", client.DescribeError(err))
		return
	}

	if diags := r.reconcileRules(ctx, location, uuid, current, plan.Rules); diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}
	if diags := r.reconcileTargets(ctx, location, uuid, current, plan.Targets); diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}

	lb, err := r.meta.Client.GetLoadBalancer(ctx, location, uuid)
	if err != nil {
		resp.Diagnostics.AddError("Unable to re-read the load balancer after update", client.DescribeError(err))
		return
	}

	plan.ID = state.ID
	r.apply(&plan, lb, location)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *loadBalancerResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state loadBalancerModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	location := resolveLocation(state.Location, r.meta)

	if err := r.meta.Client.DeleteLoadBalancer(ctx, location, state.ID.ValueString()); err != nil {
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Unable to delete the load balancer", client.DescribeError(err))
	}
}

func (r *loadBalancerResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(applyImportID(ctx, req.ID, "id", &resp.State)...)
}

func (r *loadBalancerResource) apply(model *loadBalancerModel, lb *client.LoadBalancer, location string) {
	model.ID = types.StringValue(lb.UUID)
	model.Name = types.StringValue(lb.DisplayName)
	model.Location = types.StringValue(location)
	model.NetworkID = nullableString(lb.NetworkUUID)
	model.BillingAccountID = types.Int64Value(int64(lb.BillingAccountID))
	model.PrivateAddress = nullableString(lb.PrivateAddress)
	model.PublicAddress = nullableString(lb.PublicAddress)
	model.CreatedAt = nullableString(lb.CreatedAt)

	// The payload does not report whether a public address was requested, only
	// whether one exists. After an import there is nothing to populate the flag
	// from, and the next plan would propose replacing a working load balancer,
	// so infer it from the address itself.
	if model.ReservePublicIP.IsNull() || model.ReservePublicIP.IsUnknown() {
		model.ReservePublicIP = types.BoolValue(lb.PublicAddress != "")
	}

	rules := make([]lbRuleModel, 0, len(lb.ForwardingRules))
	for _, rule := range lb.ForwardingRules {
		rules = append(rules, lbRuleModel{
			SourcePort: types.Int64Value(int64(rule.SourcePort)),
			TargetPort: types.Int64Value(int64(rule.TargetPort)),
			UUID:       nullableString(rule.UUID),
		})
	}
	model.Rules = rules

	targets := make([]lbTargetModel, 0, len(lb.Targets))
	for _, target := range lb.Targets {
		targets = append(targets, lbTargetModel{
			ID:        types.StringValue(target.TargetUUID),
			Type:      types.StringValue(target.TargetType),
			IPAddress: nullableString(target.TargetIPAddress),
		})
	}
	model.Targets = targets
}
