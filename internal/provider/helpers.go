package provider

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	rschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// computedString builds a read-only string attribute, which resources have many of.
func computedString(description string) rschema.StringAttribute {
	return rschema.StringAttribute{
		Computed:            true,
		MarkdownDescription: description,
	}
}

// computedBool builds a read-only boolean attribute.
func computedBool(description string) rschema.BoolAttribute {
	return rschema.BoolAttribute{
		Computed:            true,
		MarkdownDescription: description,
	}
}

// resolveLocation picks a resource's own location slug, falling back to the
// provider default. An empty result is valid and means "the account's default
// location", which is what the API does when no slug appears in the path.
func resolveLocation(value types.String, meta *Meta) string {
	if !value.IsNull() && !value.IsUnknown() && value.ValueString() != "" {
		return value.ValueString()
	}
	if meta == nil {
		return ""
	}
	return meta.Location
}

// resolveBillingAccount picks a resource's own billing account, falling back to
// the provider default.
//
// The API requires this on nearly every create call, and rejects the request
// without it, so a missing value is reported as a configuration error rather
// than being sent as zero.
func resolveBillingAccount(value types.Int64, meta *Meta, attribute string) (int, diag.Diagnostics) {
	var diags diag.Diagnostics

	if !value.IsNull() && !value.IsUnknown() {
		return int(value.ValueInt64()), diags
	}
	if meta != nil && meta.BillingAccountID != nil {
		return int(*meta.BillingAccountID), diags
	}

	diags.AddAttributeError(
		path.Root(attribute),
		"Missing billing account",
		"CloudAxion requires a billing account when creating this resource. Set "+attribute+
			" on the resource, billing_account_id on the provider, or the "+
			EnvBillingAccountID+" environment variable. The cloudaxion_billing_accounts "+
			"data source lists the available accounts.",
	)
	return 0, diags
}

// splitImportID parses an import identifier of the form "uuid" or
// "location/uuid", returning the location (possibly empty) and the identifier.
//
// The qualified form is needed whenever the resource is not in the provider's
// default location, since the API has no way to find a resource without knowing
// which location holds it.
func splitImportID(id string) (location, resourceID string, diags diag.Diagnostics) {
	before, after, qualified := strings.Cut(id, "/")
	if qualified {
		location, resourceID = before, after
	} else {
		resourceID = before
	}

	if resourceID == "" {
		diags.AddError(
			"Invalid import identifier",
			"Expected \"id\" or \"location/id\", got "+id+".",
		)
	}
	return location, resourceID, diags
}

// applyImportID writes the parsed import identifier into state, setting the
// location only when the caller qualified it so the provider default still applies.
func applyImportID(ctx context.Context, id, idAttribute string, setter attributeSetter) diag.Diagnostics {
	location, resourceID, diags := splitImportID(id)
	if diags.HasError() {
		return diags
	}

	diags.Append(setter.SetAttribute(ctx, path.Root(idAttribute), resourceID)...)
	if location != "" {
		diags.Append(setter.SetAttribute(ctx, path.Root("location"), location)...)
	}
	return diags
}

// attributeSetter is the part of the framework's state object these helpers
// need, kept narrow so it can be satisfied by both resource and import state.
type attributeSetter interface {
	SetAttribute(ctx context.Context, p path.Path, val any) diag.Diagnostics
}

// nullableString returns a null value for the empty string, so an attribute the
// API simply does not populate reads as null rather than as "".
func nullableString(value string) types.String {
	if value == "" {
		return types.StringNull()
	}
	return types.StringValue(value)
}

// stringSlice converts a Terraform list of strings into a Go slice, skipping
// null and unknown elements.
func stringSlice(ctx context.Context, list types.List) ([]string, diag.Diagnostics) {
	var diags diag.Diagnostics
	if list.IsNull() || list.IsUnknown() {
		return nil, diags
	}

	var values []types.String
	diags.Append(list.ElementsAs(ctx, &values, false)...)
	if diags.HasError() {
		return nil, diags
	}

	out := make([]string, 0, len(values))
	for _, v := range values {
		if !v.IsNull() && !v.IsUnknown() {
			out = append(out, v.ValueString())
		}
	}
	return out, diags
}
