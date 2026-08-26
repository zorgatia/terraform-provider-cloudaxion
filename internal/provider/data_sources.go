package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/zorgatia/terraform-provider-cloudaxion/internal/client"
)

// dsBase carries the provider meta for the simple listing data sources below.
type dsBase struct{ meta *Meta }

func (d *dsBase) configure(req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	meta, errDetail := metaFromProviderData(req.ProviderData)
	if errDetail != "" {
		resp.Diagnostics.AddError("Unexpected provider data", errDetail)
		return
	}
	d.meta = meta
}

// ------------------------------------------------------------- vm_images

var (
	_ datasource.DataSource              = (*vmImagesDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*vmImagesDataSource)(nil)
)

// NewVMImagesDataSource returns the cloudaxion_vm_images data source.
func NewVMImagesDataSource() datasource.DataSource { return &vmImagesDataSource{} }

type vmImagesDataSource struct{ dsBase }

type imageVersionModel struct {
	OSVersion   types.String `tfsdk:"os_version"`
	DisplayName types.String `tfsdk:"display_name"`
	Published   types.Bool   `tfsdk:"published"`
}

type imageModel struct {
	OSName       types.String        `tfsdk:"os_name"`
	DisplayName  types.String        `tfsdk:"display_name"`
	IsDefault    types.Bool          `tfsdk:"is_default"`
	IsAppCatalog types.Bool          `tfsdk:"is_app_catalog"`
	Versions     []imageVersionModel `tfsdk:"versions"`
}

type vmImagesModel struct {
	Images []imageModel `tfsdk:"images"`
}

func (d *vmImagesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vm_images"
}

func (d *vmImagesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The VM image catalogue.\n\n" +
			"CloudAxion has no image identifier: a `cloudaxion_vm` selects its image by the " +
			"`os_name`/`os_version` pair, and this data source is how to discover valid pairs. " +
			"The catalogue includes both plain operating systems and preinstalled applications.",
		Attributes: map[string]schema.Attribute{
			"images": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "Available image families.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"os_name":        dsComputedString("Value to pass as `os_name`."),
						"display_name":   dsComputedString("Human-readable name."),
						"is_default":     dsComputedBool("Whether this is the platform default image."),
						"is_app_catalog": dsComputedBool("Whether this is a preinstalled application rather than a plain OS."),
						"versions": schema.ListNestedAttribute{
							Computed:            true,
							MarkdownDescription: "Versions of this image.",
							NestedObject: schema.NestedAttributeObject{
								Attributes: map[string]schema.Attribute{
									"os_version":   dsComputedString("Value to pass as `os_version`."),
									"display_name": dsComputedString("Human-readable version name."),
									"published":    dsComputedBool("Whether the version is currently selectable."),
								},
							},
						},
					},
				},
			},
		},
	}
}

func (d *vmImagesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.configure(req, resp)
}

func (d *vmImagesDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	images, err := d.meta.Client.ListImages(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Unable to list CloudAxion VM images", client.DescribeError(err))
		return
	}

	state := vmImagesModel{Images: make([]imageModel, 0, len(images))}
	for _, image := range images {
		versions := make([]imageVersionModel, 0, len(image.Versions))
		for _, version := range image.Versions {
			versions = append(versions, imageVersionModel{
				OSVersion:   types.StringValue(version.OSVersion),
				DisplayName: types.StringValue(version.DisplayName),
				Published:   types.BoolValue(version.Published),
			})
		}
		state.Images = append(state.Images, imageModel{
			OSName:       types.StringValue(image.OSName),
			DisplayName:  types.StringValue(image.DisplayName),
			IsDefault:    types.BoolValue(image.IsDefault),
			IsAppCatalog: types.BoolValue(image.IsAppCatalog),
			Versions:     versions,
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// ------------------------------------------------------------ host_pools

var (
	_ datasource.DataSource              = (*hostPoolsDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*hostPoolsDataSource)(nil)
)

// NewHostPoolsDataSource returns the cloudaxion_host_pools data source.
func NewHostPoolsDataSource() datasource.DataSource { return &hostPoolsDataSource{} }

type hostPoolsDataSource struct{ dsBase }

type hostPoolModel struct {
	UUID        types.String `tfsdk:"uuid"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	IsDefault   types.Bool   `tfsdk:"is_default"`
}

type hostPoolsModel struct {
	Location types.String    `tfsdk:"location"`
	Pools    []hostPoolModel `tfsdk:"pools"`
}

func (d *hostPoolsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_host_pools"
}

func (d *hostPoolsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Host pools (server classes) available in a location.\n\n" +
			"A pool also carries the real sizing limits for VMs placed on it. Those limits are " +
			"**not** in the VM parameters endpoint and differ between locations, so a `vcpu` or " +
			"`ram` value valid in one location may be rejected in another.",
		Attributes: map[string]schema.Attribute{
			"location": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Location slug. Defaults to the provider's `location`.",
			},
			"pools": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "Available host pools.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"uuid":        dsComputedString("Pool UUID, for a VM's `designated_pool_uuid`."),
						"name":        dsComputedString("Pool name."),
						"description": dsComputedString("Description supplied by CloudAxion."),
						"is_default":  dsComputedBool("Whether VMs land here when no pool is given."),
					},
				},
			},
		},
	}
}

func (d *hostPoolsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.configure(req, resp)
}

func (d *hostPoolsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config hostPoolsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	location := resolveLocation(config.Location, d.meta)

	pools, err := d.meta.Client.ListHostPools(ctx, location)
	if err != nil {
		resp.Diagnostics.AddError("Unable to list CloudAxion host pools", client.DescribeError(err))
		return
	}

	state := hostPoolsModel{
		Location: types.StringValue(location),
		Pools:    make([]hostPoolModel, 0, len(pools)),
	}
	for _, pool := range pools {
		state.Pools = append(state.Pools, hostPoolModel{
			UUID:        types.StringValue(pool.UUID),
			Name:        types.StringValue(pool.Name),
			Description: types.StringValue(pool.Description),
			IsDefault:   types.BoolValue(pool.IsDefaultDesignate),
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// ------------------------------------------------------- billing_accounts

var (
	_ datasource.DataSource              = (*billingAccountsDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*billingAccountsDataSource)(nil)
)

// NewBillingAccountsDataSource returns the cloudaxion_billing_accounts data source.
func NewBillingAccountsDataSource() datasource.DataSource { return &billingAccountsDataSource{} }

type billingAccountsDataSource struct{ dsBase }

type billingAccountModel struct {
	ID        types.Int64  `tfsdk:"id"`
	Name      types.String `tfsdk:"name"`
	IsDefault types.Bool   `tfsdk:"is_default"`
}

type billingAccountsModel struct {
	Accounts  []billingAccountModel `tfsdk:"accounts"`
	DefaultID types.Int64           `tfsdk:"default_id"`
}

func (d *billingAccountsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_billing_accounts"
}

func (d *billingAccountsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Billing accounts on the CloudAxion account.\n\n" +
			"Billing is otherwise out of scope for this provider, but nearly every create call " +
			"requires a `billing_account_id`, so this read-only lookup exists to find it.",
		Attributes: map[string]schema.Attribute{
			"default_id": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "Identifier of the account marked as default.",
			},
			"accounts": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "Available billing accounts.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":         schema.Int64Attribute{Computed: true, MarkdownDescription: "Billing account identifier."},
						"name":       dsComputedString("Account name, when the API reports one."),
						"is_default": dsComputedBool("Whether this is the default account."),
					},
				},
			},
		},
	}
}

func (d *billingAccountsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.configure(req, resp)
}

func (d *billingAccountsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	accounts, err := d.meta.Client.ListBillingAccounts(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Unable to list CloudAxion billing accounts", client.DescribeError(err))
		return
	}

	state := billingAccountsModel{
		Accounts:  make([]billingAccountModel, 0, len(accounts)),
		DefaultID: types.Int64Null(),
	}
	for _, account := range accounts {
		state.Accounts = append(state.Accounts, billingAccountModel{
			ID:        types.Int64Value(int64(account.ID)),
			Name:      nullableString(account.Name),
			IsDefault: types.BoolValue(account.IsDefault),
		})
		if account.IsDefault {
			state.DefaultID = types.Int64Value(int64(account.ID))
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// dsComputedString and dsComputedBool mirror the resource helpers for the
// data source schema package, which is a distinct type tree.
func dsComputedString(description string) schema.StringAttribute {
	return schema.StringAttribute{Computed: true, MarkdownDescription: description}
}

func dsComputedBool(description string) schema.BoolAttribute {
	return schema.BoolAttribute{Computed: true, MarkdownDescription: description}
}
