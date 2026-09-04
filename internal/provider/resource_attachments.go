package provider

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/zorgatia/terraform-provider-cloudaxion/internal/client"
)

// The API models attachment as an operation rather than as an object: a
// firewall is applied to a VM, a disk is plugged into one, an address is bound
// to a resource. None of them has an identifier of its own.
//
// Each is therefore its own Terraform resource with a synthetic composite id,
// which keeps the underlying objects independently manageable — a disk can
// outlive the VM it was attached to — and lets Terraform order create and
// destroy correctly through ordinary dependencies.

// ---------------------------------------------------------------- firewall

var (
	_ resource.Resource                = (*firewallAttachmentResource)(nil)
	_ resource.ResourceWithConfigure   = (*firewallAttachmentResource)(nil)
	_ resource.ResourceWithImportState = (*firewallAttachmentResource)(nil)
)

// NewFirewallAttachmentResource returns the cloudaxion_firewall_attachment resource.
func NewFirewallAttachmentResource() resource.Resource {
	return &firewallAttachmentResource{}
}

type firewallAttachmentResource struct{ meta *Meta }

type firewallAttachmentModel struct {
	ID         types.String `tfsdk:"id"`
	FirewallID types.String `tfsdk:"firewall_id"`
	VMID       types.String `tfsdk:"vm_id"`
	Location   types.String `tfsdk:"location"`
}

func (r *firewallAttachmentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_firewall_attachment"
}

func (r *firewallAttachmentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Applies a `cloudaxion_firewall` to a `cloudaxion_vm`.\n\n" +
			"Attachment is an operation in the API, not an object, so this resource carries a " +
			"synthetic `id` of `firewall_id:vm_id`. Both sides stay independently managed.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Composite identifier, `firewall_id:vm_id`.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"firewall_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "UUID of the firewall to apply.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"vm_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "UUID of the virtual machine to apply it to.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
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
		},
	}
}

func (r *firewallAttachmentResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	meta, errDetail := metaFromProviderData(req.ProviderData)
	if errDetail != "" {
		resp.Diagnostics.AddError("Unexpected provider data", errDetail)
		return
	}
	r.meta = meta
}

func (r *firewallAttachmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan firewallAttachmentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	location := resolveLocation(plan.Location, r.meta)
	firewallID, vmID := plan.FirewallID.ValueString(), plan.VMID.ValueString()

	if err := r.meta.Client.AttachFirewallToVM(ctx, location, firewallID, vmID); err != nil {
		resp.Diagnostics.AddError("Unable to attach the firewall", client.DescribeError(err))
		return
	}

	plan.ID = types.StringValue(firewallID + ":" + vmID)
	plan.Location = types.StringValue(location)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *firewallAttachmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state firewallAttachmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	location := resolveLocation(state.Location, r.meta)

	firewall, err := r.meta.Client.GetFirewall(ctx, location, state.FirewallID.ValueString())
	if err != nil {
		// The firewall itself is gone, so the attachment cannot exist either.
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Unable to read the firewall attachment", client.DescribeError(err))
		return
	}

	// The attachment exists only as membership in resources_assigned, so absence
	// there is what drift looks like.
	vmID := state.VMID.ValueString()
	for _, assigned := range firewall.ResourcesAssigned {
		if assigned == vmID {
			resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
			return
		}
	}
	resp.State.RemoveResource(ctx)
}

func (r *firewallAttachmentResource) Update(context.Context, resource.UpdateRequest, *resource.UpdateResponse) {
	// Every attribute forces replacement, so Update is never called.
}

func (r *firewallAttachmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state firewallAttachmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	location := resolveLocation(state.Location, r.meta)
	err := r.meta.Client.DetachFirewallFromVM(ctx, location,
		state.FirewallID.ValueString(), state.VMID.ValueString())
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Unable to detach the firewall", client.DescribeError(err))
	}
}

func (r *firewallAttachmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	location, firewallID, vmID, diags := splitPairImportID(req.ID, "firewall_id:vm_id")
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), firewallID+":"+vmID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("firewall_id"), firewallID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("vm_id"), vmID)...)
	if location != "" {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("location"), location)...)
	}
}

// ------------------------------------------------------------------ volume

var (
	_ resource.Resource                = (*volumeAttachmentResource)(nil)
	_ resource.ResourceWithConfigure   = (*volumeAttachmentResource)(nil)
	_ resource.ResourceWithImportState = (*volumeAttachmentResource)(nil)
)

// NewVolumeAttachmentResource returns the cloudaxion_volume_attachment resource.
func NewVolumeAttachmentResource() resource.Resource {
	return &volumeAttachmentResource{}
}

type volumeAttachmentResource struct{ meta *Meta }

type volumeAttachmentModel struct {
	ID       types.String `tfsdk:"id"`
	VolumeID types.String `tfsdk:"volume_id"`
	VMID     types.String `tfsdk:"vm_id"`
	Location types.String `tfsdk:"location"`
	Device   types.String `tfsdk:"device"`
}

func (r *volumeAttachmentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_volume_attachment"
}

func (r *volumeAttachmentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Attaches a `cloudaxion_block_volume` to a `cloudaxion_vm`.\n\n" +
			"~> The guest device name in `device` is **not stable across reboots**. Mount by " +
			"`/dev/disk/by-id/virtio-<first 20 characters of volume_id>` instead. udev builds " +
			"that link from the virtio-blk serial field, which is capped at 20 bytes, so the " +
			"36-character UUID is truncated — see the example below.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Composite identifier, `volume_id:vm_id`.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"volume_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "UUID of the block volume.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"vm_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "UUID of the virtual machine.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
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
			"device": computedString(
				"Guest device name reported at attach time, for example `vdb`. Not stable " +
					"across reboots — prefer `/dev/disk/by-id/virtio-<volume_id truncated to 20 " +
					"characters>`."),
		},
	}
}

func (r *volumeAttachmentResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	meta, errDetail := metaFromProviderData(req.ProviderData)
	if errDetail != "" {
		resp.Diagnostics.AddError("Unexpected provider data", errDetail)
		return
	}
	r.meta = meta
}

func (r *volumeAttachmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan volumeAttachmentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	location := resolveLocation(plan.Location, r.meta)
	volumeID, vmID := plan.VolumeID.ValueString(), plan.VMID.ValueString()

	attachment, err := r.meta.Client.AttachDisk(ctx, location, vmID, volumeID)
	if err != nil {
		resp.Diagnostics.AddError("Unable to attach the volume", client.DescribeError(err))
		return
	}

	plan.ID = types.StringValue(volumeID + ":" + vmID)
	plan.Location = types.StringValue(location)
	plan.Device = nullableString(attachment.Name)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *volumeAttachmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state volumeAttachmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	location := resolveLocation(state.Location, r.meta)

	// The VM's storage list is the authoritative record of what is attached.
	vm, err := r.meta.Client.GetVM(ctx, location, state.VMID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Unable to read the volume attachment", client.DescribeError(err))
		return
	}

	volumeID := state.VolumeID.ValueString()
	for _, disk := range vm.Storage {
		if disk.UUID == volumeID {
			state.Device = nullableString(disk.Name)
			resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
			return
		}
	}
	resp.State.RemoveResource(ctx)
}

func (r *volumeAttachmentResource) Update(context.Context, resource.UpdateRequest, *resource.UpdateResponse) {
	// Every configurable attribute forces replacement.
}

func (r *volumeAttachmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state volumeAttachmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	location := resolveLocation(state.Location, r.meta)
	err := r.meta.Client.DetachDisk(ctx, location,
		state.VMID.ValueString(), state.VolumeID.ValueString())
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Unable to detach the volume", client.DescribeError(err))
	}
}

func (r *volumeAttachmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	location, volumeID, vmID, diags := splitPairImportID(req.ID, "volume_id:vm_id")
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), volumeID+":"+vmID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("volume_id"), volumeID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("vm_id"), vmID)...)
	if location != "" {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("location"), location)...)
	}
}

// splitPairImportID parses "a:b" or "location/a:b".
func splitPairImportID(id, shape string) (location, first, second string, diags diag.Diagnostics) {
	rest := id
	if before, after, qualified := strings.Cut(id, "/"); qualified {
		location, rest = before, after
	}

	first, second, ok := strings.Cut(rest, ":")
	if !ok || first == "" || second == "" {
		diags.AddError(
			"Invalid import identifier",
			"Expected \""+shape+"\" or \"location/"+shape+"\", got "+id+".",
		)
	}
	return location, first, second, diags
}
