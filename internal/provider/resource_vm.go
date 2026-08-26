package provider

import (
	"context"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/zorgatia/terraform-provider-cloudaxion/internal/client"
)

var (
	_ resource.Resource                = (*vmResource)(nil)
	_ resource.ResourceWithConfigure   = (*vmResource)(nil)
	_ resource.ResourceWithImportState = (*vmResource)(nil)
)

// NewVMResource returns the cloudaxion_vm resource.
func NewVMResource() resource.Resource {
	return &vmResource{}
}

type vmResource struct {
	meta *Meta
}

type vmModel struct {
	ID       types.String `tfsdk:"id"`
	Name     types.String `tfsdk:"name"`
	Location types.String `tfsdk:"location"`

	OSName    types.String `tfsdk:"os_name"`
	OSVersion types.String `tfsdk:"os_version"`

	VCPU   types.Int64 `tfsdk:"vcpu"`
	RAM    types.Int64 `tfsdk:"ram"`
	DiskGB types.Int64 `tfsdk:"disk_gb"`

	Username   types.String `tfsdk:"username"`
	Password   types.String `tfsdk:"password"`
	PublicKeys types.List   `tfsdk:"public_keys"`
	CloudInit  types.String `tfsdk:"cloud_init"`

	NetworkUUID        types.String `tfsdk:"network_uuid"`
	DesignatedPoolUUID types.String `tfsdk:"designated_pool_uuid"`
	BillingAccountID   types.Int64  `tfsdk:"billing_account_id"`
	Description        types.String `tfsdk:"description"`

	ReservePublicIP types.Bool   `tfsdk:"reserve_public_ip"`
	Backup          types.Bool   `tfsdk:"backup"`
	State           types.String `tfsdk:"state"`

	Status             types.String `tfsdk:"status"`
	Hostname           types.String `tfsdk:"hostname"`
	MAC                types.String `tfsdk:"mac"`
	PrivateIPv4        types.String `tfsdk:"private_ipv4"`
	PublicIPv4         types.String `tfsdk:"public_ipv4"`
	PublicIPv6         types.String `tfsdk:"public_ipv6"`
	BootDiskID         types.String `tfsdk:"boot_disk_id"`
	DesignatedPoolName types.String `tfsdk:"designated_pool_name"`
	CreatedAt          types.String `tfsdk:"created_at"`

	Timeouts timeouts.Value `tfsdk:"timeouts"`
}

func (r *vmResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vm"
}

func (r *vmResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A CloudAxion virtual machine.\n\n" +
			"Images are selected by the `os_name`/`os_version` pair — CloudAxion has no image " +
			"identifier. Use the `cloudaxion_vm_images` data source to see what is published.\n\n" +
			"~> **Creation is synchronous and slow.** The API blocks until the guest is running; " +
			"the smallest possible VM measured 33 seconds, and large Windows guests take " +
			"considerably longer. Raise the `timeouts` block rather than assuming a hang.\n\n" +
			"~> **`network_uuid` is effectively required on a populated account.** Omitting it " +
			"places the guest in the account's *default* network, which is rarely what you want.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Virtual machine UUID.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Name of the virtual machine. Also used to derive the hostname.",
			},
			"location": schema.StringAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "Location slug. Defaults to the provider's `location`. " +
					"A VM cannot move between locations, so changing this replaces it.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},

			"os_name": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Base image name, for example `ubuntu` or `debian`. " +
					"Ignored when `boot_disk_uuid` or a clone source is given.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"os_version": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Base image version, for example `24.04`. Valid values depend " +
					"on `os_name` — see the `cloudaxion_vm_images` data source.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},

			"vcpu": schema.Int64Attribute{
				Required: true,
				MarkdownDescription: "Number of virtual CPUs. The permitted range comes from the " +
					"host pool's `guest_limits`, not from a fixed platform maximum — see the " +
					"`cloudaxion_host_pools` data source.",
				Validators: []validator.Int64{int64validator.AtLeast(1)},
			},
			"ram": schema.Int64Attribute{
				Required:            true,
				MarkdownDescription: "Memory in **megabytes**. The host pool's `guest_limits` set the range.",
				Validators:          []validator.Int64{int64validator.AtLeast(512)},
			},
			"disk_gb": schema.Int64Attribute{
				Required: true,
				MarkdownDescription: "Boot disk size in gigabytes. Sent to the API as `disks`. " +
					"Changing it replaces the VM — the API offers no in-place boot-disk resize here.",
				Validators:    []validator.Int64{int64validator.AtLeast(20)},
				PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace()},
			},

			"username": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Initial login user. Overridden if `cloud_init` sets `users`.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"password": schema.StringAttribute{
				Optional:  true,
				Sensitive: true,
				MarkdownDescription: "Initial password. Must contain a lowercase letter, an uppercase " +
					"letter and a digit, and be at least 8 characters.\n\n" +
					"~> The API **never returns this value**, so Terraform cannot detect drift on it, " +
					"and an *imported* VM will show a replacement because state has no password to " +
					"compare against. Prefer `public_keys`; if a password is required on an imported " +
					"machine, add `lifecycle { ignore_changes = [password] }`.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:    []validator.String{VMPassword()},
			},
			"public_keys": schema.ListAttribute{
				Optional:    true,
				ElementType: types.StringType,
				MarkdownDescription: "OpenSSH public key lines to install for the initial user. " +
					"Sent as a repeated parameter, so several keys are supported.",
				PlanModifiers: []planmodifier.List{listplanmodifier.RequiresReplace()},
			},
			"cloud_init": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "cloud-init user-data, as YAML or JSON. Keys given here override " +
					"platform defaults, and setting `users` overrides `username` and `password`.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},

			"network_uuid": schema.StringAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "Private network to attach the VM to. **Omitting this places " +
					"the guest in the account's default network**, which on a populated account " +
					"usually means production.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"designated_pool_uuid": schema.StringAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "Host pool (server class) to place the VM on. " +
					"See the `cloudaxion_host_pools` data source.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"billing_account_id": schema.Int64Attribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "Billing account charged for this VM. " +
					"Defaults to the provider's `billing_account_id`.",
				PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"description": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Free-text description.",
			},

			"reserve_public_ip": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(true),
				MarkdownDescription: "Whether to reserve a public IPv4 address at creation. " +
					"Defaults to `true`, matching the API.\n\n" +
					"~> Two things to know. First, CloudAxion has **no NAT gateway**: a VM " +
					"without a public address has no outbound route at all, not even DNS, so it " +
					"cannot install packages or pull images. Second, the address is created as a " +
					"**floating IP that is not released when the VM is destroyed** — it survives " +
					"and keeps billing at the higher unassigned rate. For anything long-lived, " +
					"set this to `false` and manage the address explicitly with " +
					"`cloudaxion_floating_ip` and `cloudaxion_floating_ip_assignment`, so " +
					"Terraform owns its lifecycle.",
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.RequiresReplace()},
			},
			"backup": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
				MarkdownDescription: "Whether automatic backups are enabled.",
			},
			"state": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString(client.VMStatusRunning),
				MarkdownDescription: "Desired power state, `running` or `stopped`. Changing it " +
					"starts or stops the VM in place.",
				Validators: []validator.String{
					stringvalidator.OneOf(client.VMStatusRunning, client.VMStatusStopped),
				},
			},

			"status":       computedString("Current power state reported by the API."),
			"hostname":     computedString("Hostname assigned to the guest."),
			"mac":          computedString("MAC address of the primary interface."),
			"private_ipv4": computedString("Address on the attached private network."),
			"public_ipv4": computedString(
				"Reserved public IPv4 address, or null when none is reserved. " +
					"Resolved from the floating IP bound to this VM, since the VM payload " +
					"itself does not report it."),
			"public_ipv6":          computedString("Public IPv6 address, or null when none is assigned."),
			"boot_disk_id":         computedString("UUID of the primary disk, usable with `cloudaxion_block_volume` data lookups."),
			"designated_pool_name": computedString("Human-readable name of the host pool the VM runs on."),
			"created_at":           computedString("Creation timestamp reported by the API."),
		},
		Blocks: map[string]schema.Block{
			"timeouts": timeouts.Block(ctx, timeouts.Opts{Create: true, Update: true, Delete: true}),
		},
	}
}

func (r *vmResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	meta, errDetail := metaFromProviderData(req.ProviderData)
	if errDetail != "" {
		resp.Diagnostics.AddError("Unexpected provider data", errDetail)
		return
	}
	r.meta = meta
}

func (r *vmResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan vmModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Creation blocks until the guest is running, so the deadline has to cover
	// the whole provisioning run rather than a single quick call.
	timeout, diags := plan.Timeouts.Create(ctx, 30*time.Minute)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	location := resolveLocation(plan.Location, r.meta)

	billingAccountID, diags := resolveBillingAccount(plan.BillingAccountID, r.meta, "billing_account_id")
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	publicKeys, diags := stringSlice(ctx, plan.PublicKeys)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	reservePublicIP := plan.ReservePublicIP.ValueBool()
	backup := plan.Backup.ValueBool()

	vm, err := r.meta.Client.CreateVM(ctx, location, client.CreateVMRequest{
		Name:               plan.Name.ValueString(),
		OSName:             plan.OSName.ValueString(),
		OSVersion:          plan.OSVersion.ValueString(),
		DiskGB:             int(plan.DiskGB.ValueInt64()),
		VCPU:               int(plan.VCPU.ValueInt64()),
		RAM:                int(plan.RAM.ValueInt64()),
		Username:           plan.Username.ValueString(),
		Password:           plan.Password.ValueString(),
		PublicKeys:         publicKeys,
		CloudInit:          plan.CloudInit.ValueString(),
		NetworkUUID:        plan.NetworkUUID.ValueString(),
		DesignatedPoolUUID: plan.DesignatedPoolUUID.ValueString(),
		BillingAccountID:   &billingAccountID,
		Description:        plan.Description.ValueString(),
		ReservePublicIP:    &reservePublicIP,
		Backup:             &backup,
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to create the virtual machine", client.DescribeError(err))
		return
	}

	// The API answers with the settled VM, but honour a requested "stopped"
	// state: creation always leaves the guest running.
	if plan.State.ValueString() == client.VMStatusStopped {
		if err := r.meta.Client.StopVM(ctx, location, vm.UUID); err != nil {
			resp.Diagnostics.AddError("VM created, but stopping it failed", client.DescribeError(err))
			// Fall through and save state: the VM exists and must not be orphaned.
		} else if refreshed, err := r.meta.Client.GetVM(ctx, location, vm.UUID); err == nil {
			vm = refreshed
		}
	}

	r.apply(ctx, &plan, vm, location, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *vmResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state vmModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	location := resolveLocation(state.Location, r.meta)

	vm, err := r.meta.Client.GetVM(ctx, location, state.ID.ValueString())
	if err != nil {
		// A VM deleted outside Terraform reports absence as HTTP 400, not 404;
		// client.IsNotFound accounts for that. Dropping it here lets the next
		// plan propose a rebuild instead of failing.
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Unable to read the virtual machine", client.DescribeError(err))
		return
	}

	r.apply(ctx, &state, vm, location, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *vmResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state vmModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	timeout, diags := plan.Timeouts.Update(ctx, 30*time.Minute)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	location := resolveLocation(state.Location, r.meta)
	uuid := state.ID.ValueString()

	resizing := !plan.VCPU.Equal(state.VCPU) || !plan.RAM.Equal(state.RAM)
	wasRunning := state.Status.ValueString() == client.VMStatusRunning

	// A running guest cannot be resized, so stop it, resize, and restore the
	// power state the plan asks for.
	if resizing && wasRunning {
		if err := r.meta.Client.StopVM(ctx, location, uuid); err != nil {
			resp.Diagnostics.AddError(
				"Unable to stop the virtual machine before resizing",
				client.DescribeError(err),
			)
			return
		}
	}

	if resizing || !plan.Name.Equal(state.Name) || !plan.Description.Equal(state.Description) {
		if _, err := r.meta.Client.UpdateVM(ctx, location, uuid, client.UpdateVMRequest{
			Name:        plan.Name.ValueString(),
			VCPU:        int(plan.VCPU.ValueInt64()),
			RAM:         int(plan.RAM.ValueInt64()),
			Description: plan.Description.ValueString(),
		}); err != nil {
			resp.Diagnostics.AddError("Unable to update the virtual machine", client.DescribeError(err))
			return
		}
	}

	// Reconcile power state last, so a resize that required a stop still ends up
	// where the configuration asks.
	if diags := r.reconcilePower(ctx, location, uuid, plan.State.ValueString()); diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}

	vm, err := r.meta.Client.GetVM(ctx, location, uuid)
	if err != nil {
		resp.Diagnostics.AddError("Unable to re-read the virtual machine after update", client.DescribeError(err))
		return
	}

	plan.ID = state.ID
	r.apply(ctx, &plan, vm, location, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *vmResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state vmModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	timeout, diags := state.Timeouts.Delete(ctx, 30*time.Minute)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	location := resolveLocation(state.Location, r.meta)

	if err := r.meta.Client.DeleteVM(ctx, location, state.ID.ValueString()); err != nil {
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Unable to delete the virtual machine", client.DescribeError(err))
	}
}

func (r *vmResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(applyImportID(ctx, req.ID, "id", &resp.State)...)
}

// reconcilePower drives the VM to the requested power state, doing nothing when
// it is already there.
func (r *vmResource) reconcilePower(ctx context.Context, location, uuid, desired string) (diags diag.Diagnostics) {
	vm, err := r.meta.Client.GetVM(ctx, location, uuid)
	if err != nil {
		diags.AddError("Unable to read the virtual machine power state", client.DescribeError(err))
		return diags
	}
	if vm.Status == desired {
		return diags
	}

	switch desired {
	case client.VMStatusRunning:
		err = r.meta.Client.StartVM(ctx, location, uuid)
	case client.VMStatusStopped:
		err = r.meta.Client.StopVM(ctx, location, uuid)
	default:
		return diags
	}
	if err != nil {
		diags.AddError("Unable to change the virtual machine power state", client.DescribeError(err))
	}
	return diags
}

// apply copies an API response into the Terraform model.
//
// Attributes the API omits are written as null rather than as zero values: with
// reserve_public_ip disabled there is no public address key at all, and an empty
// string would read as a real, blank address.
// resolvePublicIPv4 finds the public address bound to a VM.
//
// Setting reserve_public_ip creates a floating IP and assigns it, but the VM
// payload never reports the address — verified 2026-08-26, a VM created with
// reserve_public_ip=true came back with no public address field at all. The
// binding is only visible from the floating IP side, via assigned_to.
func (r *vmResource) resolvePublicIPv4(ctx context.Context, location, vmUUID string) string {
	addresses, err := r.meta.Client.ListFloatingIPs(ctx, location)
	if err != nil {
		// Best effort: this is an extra lookup, not the read itself.
		return ""
	}
	for _, address := range addresses {
		if address.AssignedTo == vmUUID {
			return address.Address
		}
	}
	return ""
}

// resolveNetworkUUID finds which private network holds a VM.
//
// The VM payload does not echo network_uuid, so after an import there is nothing
// to populate it from and the next plan would propose replacing a perfectly good
// machine. The networks list does carry vm_uuids, so the membership is
// recoverable — at the cost of one extra call, made only when the value is
// actually missing.
func (r *vmResource) resolveNetworkUUID(ctx context.Context, location, vmUUID string) string {
	networks, err := r.meta.Client.ListPrivateNetworks(ctx, location)
	if err != nil {
		// Best effort: a failure here is not worth failing the read over.
		return ""
	}
	for _, network := range networks {
		for _, member := range network.VMUUIDs {
			if member == vmUUID {
				return network.UUID
			}
		}
	}
	return ""
}

func (r *vmResource) apply(ctx context.Context, model *vmModel, vm *client.VM, location string, _ *diag.Diagnostics) {
	model.ID = types.StringValue(vm.UUID)
	model.Name = types.StringValue(vm.Name)
	model.Location = types.StringValue(location)

	model.VCPU = types.Int64Value(int64(vm.VCPU))
	model.RAM = types.Int64Value(int64(vm.Memory))

	model.OSName = nullableString(vm.OSName)
	model.OSVersion = nullableString(vm.OSVersion)
	model.Description = nullableString(vm.Description)
	// The API only echoes the username when it created the account itself. With a
	// cloud-init document that declares its own users, no username is sent and
	// none comes back, so the configured value is kept rather than nulled.
	if vm.Username != "" {
		model.Username = types.StringValue(vm.Username)
	}

	model.DesignatedPoolUUID = nullableString(vm.DesignatedPoolUUID)
	model.DesignatedPoolName = nullableString(vm.DesignatedPoolName)
	model.BillingAccountID = types.Int64Value(int64(vm.BillingAccount))
	model.Backup = types.BoolValue(vm.Backup)

	model.Status = types.StringValue(vm.Status)
	model.State = types.StringValue(vm.Status)
	model.Hostname = nullableString(vm.Hostname)
	model.MAC = nullableString(vm.MAC)
	model.PrivateIPv4 = nullableString(vm.PrivateIPv4)
	model.PublicIPv4 = nullableString(vm.PublicIPv4)
	model.PublicIPv6 = nullableString(vm.PublicIPv6)
	model.CreatedAt = nullableString(vm.CreatedAt)

	if boot := vm.BootDisk(); boot != nil {
		model.BootDiskID = types.StringValue(boot.UUID)
		if model.DiskGB.IsNull() || model.DiskGB.IsUnknown() {
			model.DiskGB = types.Int64Value(int64(boot.Size))
		}
	} else {
		model.BootDiskID = types.StringNull()
	}

	// Recover the network only when it is unknown, which in practice means an
	// import: on a normal read the configured value is already correct and an
	// extra API call would be waste.
	if model.NetworkUUID.IsNull() || model.NetworkUUID.IsUnknown() {
		if networkUUID := r.resolveNetworkUUID(ctx, location, vm.UUID); networkUUID != "" {
			model.NetworkUUID = types.StringValue(networkUUID)
		}
	}

	// The public address lives on the floating IP, not on the VM, so it takes a
	// second lookup. Only worth making when one was actually requested.
	if model.PublicIPv4.IsNull() && model.ReservePublicIP.ValueBool() {
		if address := r.resolvePublicIPv4(ctx, location, vm.UUID); address != "" {
			model.PublicIPv4 = types.StringValue(address)
		}
	}
}
