package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
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
	_ resource.Resource                = (*floatingIPResource)(nil)
	_ resource.ResourceWithConfigure   = (*floatingIPResource)(nil)
	_ resource.ResourceWithImportState = (*floatingIPResource)(nil)
)

// NewFloatingIPResource returns the cloudaxion_floating_ip resource.
func NewFloatingIPResource() resource.Resource {
	return &floatingIPResource{}
}

type floatingIPResource struct{ meta *Meta }

type floatingIPModel struct {
	ID               types.String `tfsdk:"id"`
	Address          types.String `tfsdk:"address"`
	Name             types.String `tfsdk:"name"`
	Location         types.String `tfsdk:"location"`
	BillingAccountID types.Int64  `tfsdk:"billing_account_id"`

	AssignedTo             types.String `tfsdk:"assigned_to"`
	AssignedToResourceType types.String `tfsdk:"assigned_to_resource_type"`
	AssignedToPrivateIP    types.String `tfsdk:"assigned_to_private_ip"`
	Enabled                types.Bool   `tfsdk:"enabled"`
	CreatedAt              types.String `tfsdk:"created_at"`
}

func (r *floatingIPResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_floating_ip"
}

func (r *floatingIPResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A reservable public IPv4 address.\n\n" +
			"Floating IPs are how a cluster gets a **stable egress address** for customer " +
			"allow-lists. Bind one with `cloudaxion_floating_ip_assignment`.\n\n" +
			"~> An **unassigned** floating IP is billed, at a higher rate than an assigned one. " +
			"Reserve them only when something will use them.\n\n" +
			"The API addresses these by their IPv4 address rather than a UUID, so `id` is the " +
			"address itself.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "The IPv4 address, which is also the API identifier — " +
					"floating IPs have no UUID.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"address": computedString("The reserved IPv4 address."),
			"name": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Descriptive name.",
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
			"assigned_to":               computedString("UUID of the resource currently holding the address, or null."),
			"assigned_to_resource_type": computedString("Kind of resource holding the address."),
			"assigned_to_private_ip":    computedString("Private address the floating IP maps to."),
			"enabled":                   computedBool("Whether the address is enabled."),
			"created_at":                computedString("Creation timestamp reported by the API."),
		},
	}
}

func (r *floatingIPResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	meta, errDetail := metaFromProviderData(req.ProviderData)
	if errDetail != "" {
		resp.Diagnostics.AddError("Unexpected provider data", errDetail)
		return
	}
	r.meta = meta
}

func (r *floatingIPResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan floatingIPModel
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

	ip, err := r.meta.Client.CreateFloatingIP(ctx, location, plan.Name.ValueString(), billingAccountID)
	if err != nil {
		resp.Diagnostics.AddError("Unable to reserve the floating IP", client.DescribeError(err))
		return
	}

	r.apply(&plan, ip, location)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *floatingIPResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state floatingIPModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	location := resolveLocation(state.Location, r.meta)

	ip, err := r.meta.Client.GetFloatingIP(ctx, location, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Unable to read the floating IP", client.DescribeError(err))
		return
	}

	r.apply(&state, ip, location)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *floatingIPResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state floatingIPModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	location := resolveLocation(state.Location, r.meta)

	ip, err := r.meta.Client.RenameFloatingIP(ctx, location, state.ID.ValueString(), plan.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to rename the floating IP", client.DescribeError(err))
		return
	}

	r.apply(&plan, ip, location)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *floatingIPResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state floatingIPModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	location := resolveLocation(state.Location, r.meta)

	if err := r.meta.Client.DeleteFloatingIP(ctx, location, state.ID.ValueString()); err != nil {
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Unable to release the floating IP", client.DescribeError(err))
	}
}

func (r *floatingIPResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// The identifier is the address, so "location/1.2.3.4" splits on the first slash only.
	resp.Diagnostics.Append(applyImportID(ctx, req.ID, "id", &resp.State)...)
}

func (r *floatingIPResource) apply(model *floatingIPModel, ip *client.FloatingIP, location string) {
	model.ID = types.StringValue(ip.Address)
	model.Address = types.StringValue(ip.Address)
	model.Name = nullableString(ip.Name)
	model.Location = types.StringValue(location)
	model.BillingAccountID = types.Int64Value(int64(ip.BillingAccountID))
	model.AssignedTo = nullableString(ip.AssignedTo)
	model.AssignedToResourceType = nullableString(ip.AssignedToResourceType)
	model.AssignedToPrivateIP = nullableString(ip.AssignedToPrivateIP)
	model.Enabled = types.BoolValue(ip.Enabled)
	model.CreatedAt = nullableString(ip.CreatedAt)
}

// ------------------------------------------------------------- assignment

var (
	_ resource.Resource                = (*floatingIPAssignmentResource)(nil)
	_ resource.ResourceWithConfigure   = (*floatingIPAssignmentResource)(nil)
	_ resource.ResourceWithImportState = (*floatingIPAssignmentResource)(nil)
)

// NewFloatingIPAssignmentResource returns the cloudaxion_floating_ip_assignment resource.
func NewFloatingIPAssignmentResource() resource.Resource {
	return &floatingIPAssignmentResource{}
}

type floatingIPAssignmentResource struct{ meta *Meta }

type floatingIPAssignmentModel struct {
	ID           types.String `tfsdk:"id"`
	Address      types.String `tfsdk:"address"`
	ResourceID   types.String `tfsdk:"resource_id"`
	ResourceType types.String `tfsdk:"resource_type"`
	Location     types.String `tfsdk:"location"`
	PrivateIP    types.String `tfsdk:"private_ip"`
}

func (r *floatingIPAssignmentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_floating_ip_assignment"
}

func (r *floatingIPAssignmentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Binds a `cloudaxion_floating_ip` to a virtual machine, " +
			"managed service or load balancer.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Composite identifier, `address:resource_id`.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"address": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The floating IP address to bind.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"resource_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "UUID of the resource to bind the address to.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"resource_type": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString(client.FloatingIPTargetVM),
				MarkdownDescription: "Kind of resource: `virtual_machine` (the default), " +
					"`service` or `load_balancer`.",
				Validators: []validator.String{
					stringvalidator.OneOf(
						client.FloatingIPTargetVM,
						client.FloatingIPTargetService,
						client.FloatingIPTargetLoadBalancer,
					),
				},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
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
			"private_ip": computedString("Private address the floating IP maps to."),
		},
	}
}

func (r *floatingIPAssignmentResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	meta, errDetail := metaFromProviderData(req.ProviderData)
	if errDetail != "" {
		resp.Diagnostics.AddError("Unexpected provider data", errDetail)
		return
	}
	r.meta = meta
}

func (r *floatingIPAssignmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan floatingIPAssignmentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	location := resolveLocation(plan.Location, r.meta)
	address, resourceID := plan.Address.ValueString(), plan.ResourceID.ValueString()

	ip, err := r.meta.Client.AssignFloatingIP(ctx, location, address, resourceID, plan.ResourceType.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to assign the floating IP", client.DescribeError(err))
		return
	}

	plan.ID = types.StringValue(address + ":" + resourceID)
	plan.Location = types.StringValue(location)
	plan.PrivateIP = nullableString(ip.AssignedToPrivateIP)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *floatingIPAssignmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state floatingIPAssignmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	location := resolveLocation(state.Location, r.meta)

	ip, err := r.meta.Client.GetFloatingIP(ctx, location, state.Address.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Unable to read the floating IP assignment", client.DescribeError(err))
		return
	}

	// Bound to something else, or to nothing: either way this assignment is gone.
	if ip.AssignedTo != state.ResourceID.ValueString() {
		resp.State.RemoveResource(ctx)
		return
	}

	state.PrivateIP = nullableString(ip.AssignedToPrivateIP)
	if ip.AssignedToResourceType != "" {
		state.ResourceType = types.StringValue(ip.AssignedToResourceType)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *floatingIPAssignmentResource) Update(context.Context, resource.UpdateRequest, *resource.UpdateResponse) {
	// Every configurable attribute forces replacement.
}

func (r *floatingIPAssignmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state floatingIPAssignmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	location := resolveLocation(state.Location, r.meta)

	if err := r.meta.Client.UnassignFloatingIP(ctx, location, state.Address.ValueString()); err != nil {
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Unable to unassign the floating IP", client.DescribeError(err))
	}
}

func (r *floatingIPAssignmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	location, address, resourceID, diags := splitPairImportID(req.ID, "address:resource_id")
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), address+":"+resourceID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("address"), address)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("resource_id"), resourceID)...)
	if location != "" {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("location"), location)...)
	}
}
