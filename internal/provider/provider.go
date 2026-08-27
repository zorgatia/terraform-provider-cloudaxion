// Package provider wires the CloudAxion API client into the Terraform plugin
// framework.
package provider

import (
	"context"
	"os"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/function"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/zorgatia/terraform-provider-cloudaxion/internal/client"
)

// Environment variables consulted when the matching argument is absent from the
// configuration. Credentials belong in the environment, not in .tf files.
const (
	EnvAPIKey           = "CLOUDAXION_API_KEY"
	EnvEndpoint         = "CLOUDAXION_ENDPOINT"
	EnvLocation         = "CLOUDAXION_LOCATION"
	EnvBillingAccountID = "CLOUDAXION_BILLING_ACCOUNT_ID"
)

// Ensure the provider satisfies the framework interfaces at compile time.
var _ provider.Provider = (*cloudaxionProvider)(nil)

// Meta is handed to every resource and data source through Configure.
//
// BillingAccountID is carried here because the API requires it on almost every
// create call; resources may override it individually.
type Meta struct {
	Client           *client.Client
	Location         string
	BillingAccountID *int64
}

type cloudaxionProvider struct {
	version string
}

// New returns a provider factory for the given build version.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &cloudaxionProvider{version: version}
	}
}

func (p *cloudaxionProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "cloudaxion"
	resp.Version = p.version
}

type providerModel struct {
	APIKey           types.String `tfsdk:"api_key"`
	Endpoint         types.String `tfsdk:"endpoint"`
	Location         types.String `tfsdk:"location"`
	BillingAccountID types.Int64  `tfsdk:"billing_account_id"`
}

func (p *cloudaxionProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manage CloudAxion infrastructure — the Tunisian cloud operated by DataXion.",
		Attributes: map[string]schema.Attribute{
			"api_key": schema.StringAttribute{
				Optional:  true,
				Sensitive: true,
				MarkdownDescription: "CloudAxion API token, sent as the `apikey` header. " +
					"Prefer the `" + EnvAPIKey + "` environment variable over writing it into configuration.",
			},
			"endpoint": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "API root. Defaults to `" + client.DefaultEndpoint + "`, " +
					"or the `" + EnvEndpoint + "` environment variable.",
			},
			"location": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Default location slug (see the `cloudaxion_locations` data source). " +
					"Location-scoped resources use it unless they set their own. " +
					"When empty, the API acts on the account's default location. " +
					"May also be set with `" + EnvLocation + "`.",
			},
			"billing_account_id": schema.Int64Attribute{
				Optional: true,
				MarkdownDescription: "Default billing account charged for created resources. " +
					"The API requires this on most create calls. " +
					"May also be set with `" + EnvBillingAccountID + "`.",
			},
		},
	}
}

func (p *cloudaxionProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Unknown values appear when an argument depends on something not yet
	// applied. Nothing can be validated in that pass, so report it plainly
	// rather than failing later inside the client.
	if config.APIKey.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("api_key"),
			"API key is not known at configuration time",
			"The provider cannot be configured from a value produced by another resource. "+
				"Set it statically, or supply it through the "+EnvAPIKey+" environment variable.",
		)
		return
	}

	apiKey := stringOrEnv(config.APIKey, EnvAPIKey)
	if apiKey == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("api_key"),
			"Missing CloudAxion API key",
			"Set the api_key argument, or export "+EnvAPIKey+". "+
				"API tokens are created in the CloudAxion web interface.",
		)
		return
	}

	location := stringOrEnv(config.Location, EnvLocation)

	billingAccountID, diagErr := resolveBillingAccountID(config.BillingAccountID)
	if diagErr != "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("billing_account_id"),
			"Invalid "+EnvBillingAccountID,
			diagErr,
		)
		return
	}

	apiClient, err := client.New(apiKey,
		client.WithEndpoint(stringOrEnv(config.Endpoint, EnvEndpoint)),
		client.WithLocation(location),
		client.WithUserAgent("terraform-provider-cloudaxion/"+p.version),
	)
	if err != nil {
		resp.Diagnostics.AddError("Unable to create the CloudAxion API client", err.Error())
		return
	}

	meta := &Meta{
		Client:           apiClient,
		Location:         location,
		BillingAccountID: billingAccountID,
	}
	resp.DataSourceData = meta
	resp.ResourceData = meta
}

func (p *cloudaxionProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewSSHKeyResource,
		NewPrivateNetworkResource,
		NewVMResource,
		NewBlockVolumeResource,
		NewVolumeAttachmentResource,
		NewFirewallResource,
		NewFirewallAttachmentResource,
		NewFloatingIPResource,
		NewFloatingIPAssignmentResource,
		NewLoadBalancerResource,
		NewBucketResource,
		NewS3CredentialsResource,
	}
}

func (p *cloudaxionProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewLocationsDataSource,
		NewVMImagesDataSource,
		NewHostPoolsDataSource,
		NewBillingAccountsDataSource,
		NewS3EndpointDataSource,
	}
}

func (p *cloudaxionProvider) Functions(_ context.Context) []func() function.Function {
	return nil
}

// stringOrEnv prefers an explicit configuration value and falls back to the
// environment.
func stringOrEnv(value types.String, envVar string) string {
	if !value.IsNull() && !value.IsUnknown() && value.ValueString() != "" {
		return value.ValueString()
	}
	return os.Getenv(envVar)
}

// resolveBillingAccountID reads the billing account from configuration or the
// environment. The second return value is a diagnostic detail, empty on success.
func resolveBillingAccountID(value types.Int64) (*int64, string) {
	if !value.IsNull() && !value.IsUnknown() {
		id := value.ValueInt64()
		return &id, ""
	}

	raw := os.Getenv(EnvBillingAccountID)
	if raw == "" {
		return nil, ""
	}

	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return nil, EnvBillingAccountID + " must be a whole number, got " + strconv.Quote(raw) + "."
	}
	return &id, ""
}

// metaFromProviderData extracts the shared Meta, reporting the framework's
// standard error when the type is not what Configure supplied.
//
// providerData is nil during early framework passes, which is not an error:
// callers return without configuring themselves.
func metaFromProviderData(providerData any) (*Meta, string) {
	if providerData == nil {
		return nil, ""
	}
	meta, ok := providerData.(*Meta)
	if !ok {
		return nil, "Expected *provider.Meta from the provider. This is a bug in the provider."
	}
	return meta, ""
}
