package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/zorgatia/terraform-provider-cloudaxion/internal/client"
)

var (
	_ resource.Resource                = (*blockVolumeResource)(nil)
	_ resource.ResourceWithConfigure   = (*blockVolumeResource)(nil)
	_ resource.ResourceWithImportState = (*blockVolumeResource)(nil)
)

// NewBlockVolumeResource returns the cloudaxion_block_volume resource.
func NewBlockVolumeResource() resource.Resource {
	return &blockVolumeResource{}
}

type blockVolumeResource struct{ meta *Meta }

type blockVolumeModel struct {
	ID               types.String `tfsdk:"id"`
	Name             types.String `tfsdk:"name"`
	SizeGB           types.Int64  `tfsdk:"size_gb"`
	Location         types.String `tfsdk:"location"`
	BillingAccountID types.Int64  `tfsdk:"billing_account_id"`

	SourceType  types.String `tfsdk:"source_type"`
	SourceImage types.String `tfsdk:"source_image"`

	ReadOnlyBootable types.Bool   `tfsdk:"read_only_bootable"`
	Status           types.String `tfsdk:"status"`
	CreatedAt        types.String `tfsdk:"created_at"`
}

func (r *blockVolumeResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_block_volume"
}

func (r *blockVolumeResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A block storage volume, independent of any virtual machine.\n\n" +
			"Attach it with `cloudaxion_volume_attachment`. Keeping the volume and the attachment " +
			"separate means data can outlive the VM that used it.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Volume UUID.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Display name of the volume.",
			},
			"size_gb": schema.Int64Attribute{
				Required: true,
				MarkdownDescription: "Size in gigabytes. **Changing this replaces the volume and " +
					"destroys its data** — this endpoint has no in-place resize.",
				Validators:    []validator.Int64{int64validator.AtLeast(1)},
				PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace()},
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
			"source_type": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "What to seed the volume from: `EMPTY` (the default), " +
					"`OS_BASE`, `DISK` or `SNAPSHOT`.",
				Validators: []validator.String{
					stringvalidator.OneOf(
						client.DiskSourceEmpty, client.DiskSourceOSBase,
						client.DiskSourceDisk, client.DiskSourceSnapshot,
					),
				},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"source_image": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Image, disk or snapshot to clone, when `source_type` " +
					"is not `EMPTY`.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"read_only_bootable": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Whether the volume is exposed as read-only bootable media.",
			},
			"status":     computedString("Volume status reported by the API."),
			"created_at": computedString("Creation timestamp reported by the API."),
		},
	}
}

func (r *blockVolumeResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	meta, errDetail := metaFromProviderData(req.ProviderData)
	if errDetail != "" {
		resp.Diagnostics.AddError("Unexpected provider data", errDetail)
		return
	}
	r.meta = meta
}

func (r *blockVolumeResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan blockVolumeModel
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

	volume, err := r.meta.Client.CreateDisk(ctx, location, client.CreateDiskRequest{
		DisplayName:      plan.Name.ValueString(),
		SizeGB:           int(plan.SizeGB.ValueInt64()),
		BillingAccountID: billingAccountID,
		SourceImageType:  plan.SourceType.ValueString(),
		SourceImage:      plan.SourceImage.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to create the block volume", client.DescribeError(err))
		return
	}

	r.apply(&plan, volume, location)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *blockVolumeResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state blockVolumeModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	location := resolveLocation(state.Location, r.meta)

	volume, err := r.meta.Client.GetDisk(ctx, location, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Unable to read the block volume", client.DescribeError(err))
		return
	}

	r.apply(&state, volume, location)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *blockVolumeResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state blockVolumeModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	location := resolveLocation(state.Location, r.meta)

	name := plan.Name.ValueString()
	update := client.UpdateDiskRequest{DisplayName: &name}

	if !plan.BillingAccountID.Equal(state.BillingAccountID) && !plan.BillingAccountID.IsUnknown() {
		id := int(plan.BillingAccountID.ValueInt64())
		update.BillingAccountID = &id
	}
	if !plan.ReadOnlyBootable.IsNull() && !plan.ReadOnlyBootable.IsUnknown() {
		readOnly := plan.ReadOnlyBootable.ValueBool()
		update.ReadOnlyBootable = &readOnly
	}

	volume, err := r.meta.Client.UpdateDisk(ctx, location, state.ID.ValueString(), update)
	if err != nil {
		resp.Diagnostics.AddError("Unable to update the block volume", client.DescribeError(err))
		return
	}

	plan.ID = state.ID
	r.apply(&plan, volume, location)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *blockVolumeResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state blockVolumeModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	location := resolveLocation(state.Location, r.meta)

	if err := r.meta.Client.DeleteDisk(ctx, location, state.ID.ValueString()); err != nil {
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Unable to delete the block volume", client.DescribeError(err))
	}
}

func (r *blockVolumeResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(applyImportID(ctx, req.ID, "id", &resp.State)...)
}

func (r *blockVolumeResource) apply(model *blockVolumeModel, volume *client.Disk, location string) {
	model.ID = types.StringValue(volume.UUID)
	model.Location = types.StringValue(location)
	model.Status = nullableString(volume.Status)
	model.CreatedAt = nullableString(volume.CreatedAt)
	model.ReadOnlyBootable = types.BoolValue(volume.ReadOnlyBootable)

	// The API reports the display name under either key depending on endpoint,
	// and size under either size_gb or size.
	if volume.DisplayName != "" {
		model.Name = types.StringValue(volume.DisplayName)
	} else if volume.Name != "" {
		model.Name = types.StringValue(volume.Name)
	}
	if capacity := volume.Capacity(); capacity > 0 {
		model.SizeGB = types.Int64Value(int64(capacity))
	}
	if volume.BillingAccountID > 0 {
		model.BillingAccountID = types.Int64Value(int64(volume.BillingAccountID))
	}
}
