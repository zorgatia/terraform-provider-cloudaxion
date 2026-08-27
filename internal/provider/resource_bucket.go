package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/zorgatia/terraform-provider-cloudaxion/internal/client"
)

var (
	_ resource.Resource                = (*bucketResource)(nil)
	_ resource.ResourceWithConfigure   = (*bucketResource)(nil)
	_ resource.ResourceWithImportState = (*bucketResource)(nil)
)

// NewBucketResource returns the cloudaxion_bucket resource.
func NewBucketResource() resource.Resource {
	return &bucketResource{}
}

type bucketResource struct{ meta *Meta }

type bucketModel struct {
	ID               types.String `tfsdk:"id"`
	Name             types.String `tfsdk:"name"`
	BillingAccountID types.Int64  `tfsdk:"billing_account_id"`
	CreatedAt        types.String `tfsdk:"created_at"`
}

func (r *bucketResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_bucket"
}

func (r *bucketResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "An object-storage bucket.\n\n" +
			"Only the bucket's own lifecycle lives in the CloudAxion API. Everything *inside* it — " +
			"objects, ACLs, versioning, encryption, lifecycle rules, policies — is reached through " +
			"the S3-compatible endpoint from the `cloudaxion_s3_endpoint` data source, with the " +
			"`aws` provider. This resource deliberately does not reimplement S3.\n\n" +
			"~> **Unverified against a live account.** Object storage was not provisioned on the " +
			"account this provider was developed against (`/v1/storage/user/keys` answers " +
			"`404 Storage account not found`), so this resource is written from the documented " +
			"contract and exercised only against a fake. Treat the first real apply as a test.\n\n" +
			"Buckets are **account-wide**, not per location.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "The bucket name, which is also the API identifier — " +
					"buckets have no UUID.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Bucket name. Must be globally unique within the platform, " +
					"and cannot be changed — renaming replaces the bucket, which loses its contents.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"billing_account_id": schema.Int64Attribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Billing account. Defaults to the provider's `billing_account_id`.",
			},
			"created_at": computedString("Creation timestamp reported by the API."),
		},
	}
}

func (r *bucketResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	meta, errDetail := metaFromProviderData(req.ProviderData)
	if errDetail != "" {
		resp.Diagnostics.AddError("Unexpected provider data", errDetail)
		return
	}
	r.meta = meta
}

func (r *bucketResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan bucketModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	billingAccountID, diags := resolveBillingAccount(plan.BillingAccountID, r.meta, "billing_account_id")
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	bucket, err := r.meta.Client.CreateBucket(ctx, plan.Name.ValueString(), billingAccountID)
	if err != nil {
		resp.Diagnostics.AddError("Unable to create the bucket", client.DescribeError(err))
		return
	}

	r.apply(&plan, bucket, billingAccountID)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *bucketResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state bucketModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	bucket, err := r.meta.Client.GetBucket(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Unable to read the bucket", client.DescribeError(err))
		return
	}

	r.apply(&state, bucket, int(state.BillingAccountID.ValueInt64()))
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *bucketResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state bucketModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	billingAccountID, diags := resolveBillingAccount(plan.BillingAccountID, r.meta, "billing_account_id")
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The billing account is the only mutable attribute; the name forces replacement.
	bucket, err := r.meta.Client.UpdateBucketBillingAccount(ctx, state.ID.ValueString(), billingAccountID)
	if err != nil {
		resp.Diagnostics.AddError("Unable to update the bucket", client.DescribeError(err))
		return
	}

	r.apply(&plan, bucket, billingAccountID)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *bucketResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state bucketModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.meta.Client.DeleteBucket(ctx, state.ID.ValueString()); err != nil {
		if client.IsNotFound(err) {
			return
		}
		// The API refuses to delete a bucket that still holds objects, and
		// emptying one is an S3 operation this provider does not perform.
		resp.Diagnostics.AddError(
			"Unable to delete the bucket",
			client.DescribeError(err)+
				"\n\nA bucket must be empty before it can be deleted. Emptying it is an S3 "+
				"operation — use the aws provider or an S3 client against the endpoint from "+
				"the cloudaxion_s3_endpoint data source.",
		)
	}
}

func (r *bucketResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Account-wide, and identified by name rather than by uuid.
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), req.ID)...)
}

func (r *bucketResource) apply(model *bucketModel, bucket *client.Bucket, billingAccountID int) {
	model.ID = types.StringValue(bucket.Name)
	model.Name = types.StringValue(bucket.Name)
	model.CreatedAt = nullableString(bucket.CreatedAt)

	// The API does not consistently echo the billing account, so the resolved
	// value is kept rather than nulling a perfectly good one.
	if bucket.BillingAccountID > 0 {
		model.BillingAccountID = types.Int64Value(int64(bucket.BillingAccountID))
	} else if billingAccountID > 0 {
		model.BillingAccountID = types.Int64Value(int64(billingAccountID))
	}
}
