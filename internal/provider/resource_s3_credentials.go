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
	_ resource.Resource                = (*s3CredentialsResource)(nil)
	_ resource.ResourceWithConfigure   = (*s3CredentialsResource)(nil)
	_ resource.ResourceWithImportState = (*s3CredentialsResource)(nil)
)

// NewS3CredentialsResource returns the cloudaxion_s3_credentials resource.
func NewS3CredentialsResource() resource.Resource {
	return &s3CredentialsResource{}
}

type s3CredentialsResource struct{ meta *Meta }

type s3CredentialsModel struct {
	ID        types.String `tfsdk:"id"`
	AccessKey types.String `tfsdk:"access_key"`
	SecretKey types.String `tfsdk:"secret_key"`
	CreatedAt types.String `tfsdk:"created_at"`
}

func (r *s3CredentialsResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_s3_credentials"
}

func (r *s3CredentialsResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "An access key pair for the S3-compatible object-storage endpoint.\n\n" +
			"~> **`secret_key` is returned only once, at creation.** The API never discloses it " +
			"again, so it lives in Terraform state and nowhere else. That has two consequences: " +
			"treat state as a secret, and understand that a lost state means a lost key — the " +
			"recovery is to create a new pair and revoke the old one.\n\n" +
			"~> **Unverified against a live account.** Object storage was not provisioned on the " +
			"account this provider was developed against, so this resource is written from the " +
			"documented contract. Treat the first real apply as a test.\n\n" +
			"The pair has no configurable input: it is requested, not described.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The access key, which is also the API identifier.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"access_key": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Access key identifier. Use as `AWS_ACCESS_KEY_ID`.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"secret_key": schema.StringAttribute{
				Computed:  true,
				Sensitive: true,
				MarkdownDescription: "Secret access key. Use as `AWS_SECRET_ACCESS_KEY`. " +
					"Never re-readable from the API — held in state only.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"created_at": computedString("Creation timestamp reported by the API."),
		},
	}
}

func (r *s3CredentialsResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	meta, errDetail := metaFromProviderData(req.ProviderData)
	if errDetail != "" {
		resp.Diagnostics.AddError("Unexpected provider data", errDetail)
		return
	}
	r.meta = meta
}

func (r *s3CredentialsResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan s3CredentialsModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	key, err := r.meta.Client.CreateS3Key(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Unable to create the S3 credentials", client.DescribeError(err))
		return
	}

	plan.ID = types.StringValue(key.AccessKey)
	plan.AccessKey = types.StringValue(key.AccessKey)
	plan.SecretKey = types.StringValue(key.SecretKey)
	plan.CreatedAt = nullableString(key.CreatedAt)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *s3CredentialsResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state s3CredentialsModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Existence is checked by listing: there is no single-key GET, and the list
	// never includes secrets. The stored secret is therefore left untouched —
	// overwriting it with an empty value would destroy the only copy.
	keys, err := r.meta.Client.ListS3Keys(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read the S3 credentials", client.DescribeError(err))
		return
	}

	accessKey := state.ID.ValueString()
	for _, key := range keys {
		if key.AccessKey == accessKey {
			if key.CreatedAt != "" {
				state.CreatedAt = types.StringValue(key.CreatedAt)
			}
			resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
			return
		}
	}

	// Revoked elsewhere: drop it so the next plan issues a new pair.
	resp.State.RemoveResource(ctx)
}

func (r *s3CredentialsResource) Update(context.Context, resource.UpdateRequest, *resource.UpdateResponse) {
	// Nothing is configurable, so there is nothing to update.
}

func (r *s3CredentialsResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state s3CredentialsModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.meta.Client.DeleteS3Key(ctx, state.ID.ValueString()); err != nil {
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Unable to revoke the S3 credentials", client.DescribeError(err))
	}
}

func (r *s3CredentialsResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("access_key"), req.ID)...)

	// Importing recovers the identifier but not the secret — the API will not
	// disclose it. Say so plainly rather than leaving a half-usable resource.
	resp.Diagnostics.AddWarning(
		"The secret key cannot be imported",
		"CloudAxion returns a secret key only when the pair is created, so an imported "+
			"cloudaxion_s3_credentials has an empty secret_key and anything consuming it will "+
			"break. If you need a usable pair under Terraform management, create a new one and "+
			"revoke this one instead of importing it.",
	)
}
