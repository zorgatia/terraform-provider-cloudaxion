package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/zorgatia/terraform-provider-cloudaxion/internal/client"
)

var (
	_ datasource.DataSource              = (*locationsDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*locationsDataSource)(nil)
)

// NewLocationsDataSource returns the cloudaxion_locations data source.
func NewLocationsDataSource() datasource.DataSource {
	return &locationsDataSource{}
}

type locationsDataSource struct {
	meta *Meta
}

type locationModel struct {
	Slug        types.String `tfsdk:"slug"`
	DisplayName types.String `tfsdk:"display_name"`
	Description types.String `tfsdk:"description"`
	CountryCode types.String `tfsdk:"country_code"`
	IsDefault   types.Bool   `tfsdk:"is_default"`
	IsPreferred types.Bool   `tfsdk:"is_preferred"`
}

type locationsModel struct {
	Locations   []locationModel `tfsdk:"locations"`
	DefaultSlug types.String    `tfsdk:"default_slug"`
}

func (d *locationsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_locations"
}

func (d *locationsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Every CloudAxion location available to the account.\n\n" +
			"Location slugs are how resources are placed. There is no endpoint that lists " +
			"resources across locations, so anything needing a global view must iterate over these.",
		Attributes: map[string]schema.Attribute{
			"default_slug": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "Slug of the location marked as default. " +
					"Requests that specify no location act on it.",
			},
			"locations": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "All available locations, in the order the API returns them.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"slug": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Identifier used in location-scoped API paths and in the provider's `location`.",
						},
						"display_name": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Human-readable name.",
						},
						"description": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Description supplied by CloudAxion.",
						},
						"country_code": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Country the location sits in.",
						},
						"is_default": schema.BoolAttribute{
							Computed:            true,
							MarkdownDescription: "Whether this is the account's default location.",
						},
						"is_preferred": schema.BoolAttribute{
							Computed:            true,
							MarkdownDescription: "Whether CloudAxion marks this location as preferred.",
						},
					},
				},
			},
		},
	}
}

func (d *locationsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	meta, errDetail := metaFromProviderData(req.ProviderData)
	if errDetail != "" {
		resp.Diagnostics.AddError("Unexpected provider data", errDetail)
		return
	}
	d.meta = meta
}

func (d *locationsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	locations, err := d.meta.Client.ListLocations(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Unable to list CloudAxion locations", client.DescribeError(err))
		return
	}

	state := locationsModel{
		Locations:   make([]locationModel, 0, len(locations)),
		DefaultSlug: types.StringNull(),
	}
	for _, location := range locations {
		state.Locations = append(state.Locations, locationModel{
			Slug:        types.StringValue(location.Slug),
			DisplayName: types.StringValue(location.DisplayName),
			Description: types.StringValue(location.Description),
			CountryCode: types.StringValue(location.CountryCode),
			IsDefault:   types.BoolValue(location.IsDefault),
			IsPreferred: types.BoolValue(location.IsPreferred),
		})
		if location.IsDefault {
			state.DefaultSlug = types.StringValue(location.Slug)
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
