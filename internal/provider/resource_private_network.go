package provider

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/zorgatia/terraform-provider-cloudaxion/internal/client"
)

var (
	_ resource.Resource                = (*privateNetworkResource)(nil)
	_ resource.ResourceWithConfigure   = (*privateNetworkResource)(nil)
	_ resource.ResourceWithImportState = (*privateNetworkResource)(nil)
)

// NewPrivateNetworkResource returns the cloudaxion_private_network resource.
func NewPrivateNetworkResource() resource.Resource {
	return &privateNetworkResource{}
}

type privateNetworkResource struct {
	meta *Meta
}

type privateNetworkModel struct {
	ID       types.String `tfsdk:"id"`
	Name     types.String `tfsdk:"name"`
	Location types.String `tfsdk:"location"`

	VLANID     types.Int64  `tfsdk:"vlan_id"`
	Subnet     types.String `tfsdk:"subnet"`
	SubnetIPv6 types.String `tfsdk:"subnet_ipv6"`
	Type       types.String `tfsdk:"type"`
	IsDefault  types.Bool   `tfsdk:"is_default"`
}

func (r *privateNetworkResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_private_network"
}

func (r *privateNetworkResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A VLAN-backed private network.\n\n" +
			"CloudAxion allocates the VLAN and the address range: there is no way to request a " +
			"particular subnet, so `subnet` and `vlan_id` are read-only outputs. Plan cluster " +
			"addressing around the range the API hands back rather than choosing one up front.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Network UUID.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Descriptive name.",
			},
			"location": schema.StringAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "Location slug. Defaults to the provider's `location`. " +
					"Changing it replaces the network, which cannot move between locations.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"vlan_id": schema.Int64Attribute{
				Computed: true,
				MarkdownDescription: "VLAN identifier, when CloudAxion reports one. " +
					"**Null in practice** — the live API does not return `vlan_id`, " +
					"even though the published documentation shows it.",
			},
			"subnet": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "IPv4 range allocated by CloudAxion, in CIDR notation.",
			},
			"subnet_ipv6": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "IPv6 range allocated by CloudAxion, " +
					"or null when the location does not assign one.",
			},
			"type": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Network type reported by the API.",
			},
			"is_default": schema.BoolAttribute{
				Computed: true,
				MarkdownDescription: "Whether this is the account's default network for the location. " +
					"The first network created in an account becomes the default.",
			},
		},
	}
}

func (r *privateNetworkResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	meta, errDetail := metaFromProviderData(req.ProviderData)
	if errDetail != "" {
		resp.Diagnostics.AddError("Unexpected provider data", errDetail)
		return
	}
	r.meta = meta
}

func (r *privateNetworkResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan privateNetworkModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	location := r.resolveLocation(plan.Location)

	network, err := r.meta.Client.CreatePrivateNetwork(ctx, location, plan.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to create the private network", client.DescribeError(err))
		return
	}

	r.apply(&plan, network, location)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *privateNetworkResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state privateNetworkModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	location := r.resolveLocation(state.Location)

	network, err := r.meta.Client.GetPrivateNetwork(ctx, location, state.ID.ValueString())
	if err != nil {
		// A network removed outside Terraform leaves state quietly, so the next
		// plan proposes recreating it instead of failing.
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Unable to read the private network", client.DescribeError(err))
		return
	}

	r.apply(&state, network, location)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *privateNetworkResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state privateNetworkModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	location := r.resolveLocation(state.Location)

	// The name is the only mutable attribute; everything else is either
	// allocated by CloudAxion or forces replacement.
	network, err := r.meta.Client.RenamePrivateNetwork(ctx, location, state.ID.ValueString(), plan.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to rename the private network", client.DescribeError(err))
		return
	}

	plan.ID = state.ID
	r.apply(&plan, network, location)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *privateNetworkResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state privateNetworkModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	location := r.resolveLocation(state.Location)

	if err := r.meta.Client.DeletePrivateNetwork(ctx, location, state.ID.ValueString()); err != nil {
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Unable to delete the private network", client.DescribeError(err))
	}
}

// ImportState accepts "uuid" or "location/uuid". The qualified form is needed
// whenever the network is not in the provider's default location.
func (r *privateNetworkResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	location, uuid, qualified := strings.Cut(req.ID, "/")
	if !qualified {
		uuid = location
		location = ""
	}

	if uuid == "" {
		resp.Diagnostics.AddError(
			"Invalid import identifier",
			"Expected \"uuid\" or \"location/uuid\", got "+req.ID+".",
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), uuid)...)
	if location != "" {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("location"), location)...)
	}
}

// resolveLocation picks the resource's own location, falling back to the
// provider default.
func (r *privateNetworkResource) resolveLocation(value types.String) string {
	if !value.IsNull() && !value.IsUnknown() && value.ValueString() != "" {
		return value.ValueString()
	}
	return r.meta.Location
}

// apply copies an API response into the Terraform model.
func (r *privateNetworkResource) apply(model *privateNetworkModel, network *client.PrivateNetwork, location string) {
	model.ID = types.StringValue(network.UUID)
	model.Name = types.StringValue(network.Name)
	model.Location = types.StringValue(location)
	// The live API omits vlan_id entirely and returns an empty subnet_ipv6, so
	// both are reported as null rather than as 0 and "" — a zero VLAN would read
	// as a real one.
	model.VLANID = types.Int64Null()
	if network.VLANID != nil {
		model.VLANID = types.Int64Value(int64(*network.VLANID))
	}
	model.Subnet = types.StringValue(network.Subnet)
	model.SubnetIPv6 = types.StringNull()
	if network.SubnetIPv6 != "" {
		model.SubnetIPv6 = types.StringValue(network.SubnetIPv6)
	}
	model.Type = types.StringValue(network.Type)
	model.IsDefault = types.BoolValue(network.IsDefault)
}
