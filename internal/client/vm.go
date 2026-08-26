package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// VM power states reported by the API.
const (
	VMStatusRunning = "running"
	VMStatusStopped = "stopped"
)

// VMStorage is a disk as reported inside a VM payload.
type VMStorage struct {
	UUID      string   `json:"uuid"`
	ID        int      `json:"id"`
	Name      string   `json:"name"` // guest device name, e.g. "sda"
	Size      int      `json:"size"` // GB
	Type      string   `json:"type"`
	Pool      string   `json:"pool"`
	Primary   bool     `json:"primary"`
	Shared    bool     `json:"shared"`
	Replica   []string `json:"-"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at"`
}

// VM is a virtual machine.
type VM struct {
	UUID     string `json:"uuid"`
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Status   string `json:"status"`
	Hostname string `json:"hostname"`

	VCPU   int `json:"vcpu"`
	Memory int `json:"memory"` // MB

	OSName    string `json:"os_name"`
	OSVersion string `json:"os_version"`

	PrivateIPv4 string `json:"private_ipv4"`
	PublicIPv4  string `json:"public_ipv4"`
	PublicIPv6  string `json:"public_ipv6"`
	MAC         string `json:"mac"`

	Username    string `json:"username"`
	Description string `json:"description"`
	Backup      bool   `json:"backup"`
	LicenseType string `json:"license_type"`

	BillingAccount int `json:"billing_account"`

	DesignatedPoolUUID string `json:"designated_pool_uuid"`
	DesignatedPoolName string `json:"designated_pool_name"`

	Storage []VMStorage `json:"storage"`

	UserID    int    `json:"user_id"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// BootDisk returns the primary disk, or nil when the VM reports none.
func (v *VM) BootDisk() *VMStorage {
	for i := range v.Storage {
		if v.Storage[i].Primary {
			return &v.Storage[i]
		}
	}
	return nil
}

// CreateVMRequest holds the arguments to VM creation.
//
// Images are chosen by the OSName/OSVersion pair rather than by an identifier.
// Pointer fields are optional: nil omits the parameter and lets the API apply
// its own default, which matters for ReservePublicIP because its default is true.
type CreateVMRequest struct {
	Name      string
	OSName    string
	OSVersion string

	DiskGB int
	VCPU   int
	RAM    int // MB

	Username string
	Password string

	PublicKeys []string

	NetworkUUID        string
	DesignatedPoolUUID string
	BillingAccountID   *int
	Description        string

	// CloudInit is user-data as JSON or YAML. Setting "users" here overrides
	// Username and Password.
	CloudInit string

	ReservePublicIP *bool
	Backup          *bool

	// Clone and restore sources. SourceUUID with SourceReplica creates from a
	// snapshot or backup; DiskUUID boots an existing unattached disk, in which
	// case DiskGB has no effect.
	SourceUUID    string
	SourceReplica string
	DiskUUID      string
}

func (r CreateVMRequest) form() url.Values {
	f := url.Values{}
	setStr(f, "name", r.Name)
	setStr(f, "os_name", r.OSName)
	setStr(f, "os_version", r.OSVersion)
	setStr(f, "network_uuid", r.NetworkUUID)
	setStr(f, "designated_pool_uuid", r.DesignatedPoolUUID)
	setStr(f, "description", r.Description)
	setStr(f, "cloud_init", r.CloudInit)
	setStr(f, "source_uuid", r.SourceUUID)
	setStr(f, "source_replica", r.SourceReplica)
	setStr(f, "disk_uuid", r.DiskUUID)

	if r.DiskGB > 0 {
		f.Set("disks", strconv.Itoa(r.DiskGB))
	}
	if r.VCPU > 0 {
		f.Set("vcpu", strconv.Itoa(r.VCPU))
	}
	if r.RAM > 0 {
		f.Set("ram", strconv.Itoa(r.RAM))
	}
	if r.BillingAccountID != nil {
		f.Set("billing_account_id", strconv.Itoa(*r.BillingAccountID))
	}
	if r.ReservePublicIP != nil {
		f.Set("reserve_public_ip", strconv.FormatBool(*r.ReservePublicIP))
	}
	if r.Backup != nil {
		f.Set("backup", strconv.FormatBool(*r.Backup))
	}

	// Sending username or password alongside a cloud-init document that declares
	// its own users breaks the guest outright: the API injects a conflicting user
	// configuration and the result is a machine with no working login at all.
	//
	// Verified 2026-08-26 with three otherwise identical VMs — cloud-init alone
	// booted with a working SSH login, and adding username to the very same
	// request produced a machine that rejected every key. The documentation only
	// says cloud-init "overrides" these, which undersells it.
	if !cloudInitDefinesUsers(r.CloudInit) {
		setStr(f, "username", r.Username)
		setStr(f, "password", r.Password)
	}

	// Multiple SSH keys are passed by repeating the parameter rather than by
	// joining them into one value.
	for _, key := range r.PublicKeys {
		if key != "" {
			f.Add("public_keys", key)
		}
	}

	return f
}

// CreateVM provisions a virtual machine. The returned VM reflects the immediate
// API response; callers that need it to be usable should follow with WaitForVM.
func (c *Client) CreateVM(ctx context.Context, location string, req CreateVMRequest) (*VM, error) {
	var out VM
	err := c.Do(ctx, Request{
		Method:   http.MethodPost,
		Path:     "user-resource/vm",
		Scoped:   true,
		Location: location,
		Form:     req.form(),
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// GetVM reads a virtual machine. The uuid travels as a query parameter, not as
// a path segment.
func (c *Client) GetVM(ctx context.Context, location, uuid string) (*VM, error) {
	q := url.Values{}
	q.Set("uuid", uuid)

	var out VM
	err := c.Do(ctx, Request{
		Method:   http.MethodGet,
		Path:     "user-resource/vm",
		Scoped:   true,
		Location: location,
		Query:    q,
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// ListVMs returns every VM in a location.
func (c *Client) ListVMs(ctx context.Context, location string) ([]VM, error) {
	var out []VM
	err := c.Do(ctx, Request{
		Method:   http.MethodGet,
		Path:     "user-resource/vm/list",
		Scoped:   true,
		Location: location,
	}, &out)
	return out, err
}

// UpdateVMRequest carries the mutable VM attributes. Zero-valued fields are left
// untouched.
type UpdateVMRequest struct {
	Name        string
	VCPU        int
	RAM         int
	Description string
}

// UpdateVM changes a VM's name, size or description.
//
// Resizing generally requires the VM to be stopped; the caller is responsible
// for the power cycle.
func (c *Client) UpdateVM(ctx context.Context, location, uuid string, req UpdateVMRequest) (*VM, error) {
	f := url.Values{}
	f.Set("uuid", uuid)
	setStr(f, "name", req.Name)
	setStr(f, "description", req.Description)
	if req.VCPU > 0 {
		f.Set("vcpu", strconv.Itoa(req.VCPU))
	}
	if req.RAM > 0 {
		f.Set("ram", strconv.Itoa(req.RAM))
	}

	var out VM
	err := c.Do(ctx, Request{
		Method:   http.MethodPatch,
		Path:     "user-resource/vm",
		Scoped:   true,
		Location: location,
		Form:     f,
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteVM destroys a virtual machine.
func (c *Client) DeleteVM(ctx context.Context, location, uuid string) error {
	f := url.Values{}
	f.Set("uuid", uuid)

	return c.Do(ctx, Request{
		Method:   http.MethodDelete,
		Path:     "user-resource/vm",
		Scoped:   true,
		Location: location,
		Form:     f,
	}, nil)
}

// StartVM powers a VM on.
func (c *Client) StartVM(ctx context.Context, location, uuid string) error {
	return c.vmAction(ctx, location, uuid, "start")
}

// StopVM powers a VM off.
func (c *Client) StopVM(ctx context.Context, location, uuid string) error {
	return c.vmAction(ctx, location, uuid, "stop")
}

func (c *Client) vmAction(ctx context.Context, location, uuid, action string) error {
	f := url.Values{}
	f.Set("uuid", uuid)

	return c.Do(ctx, Request{
		Method:   http.MethodPost,
		Path:     "user-resource/vm/" + action,
		Scoped:   true,
		Location: location,
		Form:     f,
	}, nil)
}

// WaitForVM blocks until the VM reports one of the target statuses.
//
// The API exposes no task endpoint, so this polls GetVM. Any status outside
// target and pending ends the wait immediately rather than running out the clock.
func (c *Client) WaitForVM(ctx context.Context, location, uuid string, target []string, timeout time.Duration) (*VM, error) {
	var last *VM

	poll := Poll{
		Fetch: func(ctx context.Context) (string, error) {
			vm, err := c.GetVM(ctx, location, uuid)
			if err != nil {
				return "", err
			}
			last = vm
			return vm.Status, nil
		},
		Target:  target,
		Pending: []string{"provisioning", "starting", "stopping", "pending", "creating", "building"},
		Timeout: timeout,
	}
	if err := poll.Wait(ctx); err != nil {
		return nil, fmt.Errorf("waiting for VM %s: %w", uuid, err)
	}
	return last, nil
}

// WaitForVMDeleted blocks until the VM is gone.
func (c *Client) WaitForVMDeleted(ctx context.Context, location, uuid string, timeout time.Duration) error {
	poll := Poll{
		Fetch: func(ctx context.Context) (string, error) {
			vm, err := c.GetVM(ctx, location, uuid)
			if err != nil {
				return "", err
			}
			return vm.Status, nil
		},
		Target:     []string{"deleted"},
		Pending:    []string{VMStatusRunning, VMStatusStopped, "deleting", "stopping"},
		GoneIsDone: true,
		Timeout:    timeout,
	}
	if err := poll.Wait(ctx); err != nil {
		return fmt.Errorf("waiting for VM %s to be deleted: %w", uuid, err)
	}
	return nil
}

// UnmarshalJSON handles the replica field, whose element type the API
// documentation leaves unspecified.
func (s *VMStorage) UnmarshalJSON(data []byte) error {
	type alias VMStorage
	aux := struct {
		Replica json.RawMessage `json:"replica"`
		*alias
	}{alias: (*alias)(s)}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	s.Replica = decodeUUIDList(aux.Replica)
	return nil
}

// cloudInitDefinesUsers reports whether a cloud-init document declares its own
// users, in which case the API's username and password parameters must not be
// sent alongside it.
//
// This is a structural check rather than a YAML parse: cloud-init accepts both
// YAML and JSON, the documents are user-authored and may not parse at all, and
// a failure to parse must not stop the request. Only a top-level key counts —
// an indented "users:" belongs to something else.
func cloudInitDefinesUsers(cloudInit string) bool {
	if cloudInit == "" {
		return false
	}

	// A compact JSON document has no line structure to walk.
	if strings.Contains(cloudInit, "\"users\"") {
		return true
	}

	for _, line := range strings.Split(cloudInit, "\n") {
		if line == "" {
			continue
		}
		// Only a top-level key matters; anything indented belongs to another block.
		switch line[0] {
		case ' ', '\t', '-', '#':
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(line), "users:") {
			return true
		}
	}

	return false
}

// setStr adds a form value only when it is non-empty, so optional parameters
// stay absent rather than being sent as empty strings.
func setStr(f url.Values, key, value string) {
	if value != "" {
		f.Set(key, value)
	}
}
