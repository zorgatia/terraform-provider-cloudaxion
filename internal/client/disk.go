package client

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Sources a new disk can be created from.
const (
	DiskSourceEmpty    = "EMPTY"
	DiskSourceOSBase   = "OS_BASE"
	DiskSourceDisk     = "DISK"
	DiskSourceSnapshot = "SNAPSHOT"

	// DiskStatusActive is the ready state. The API capitalises this one, unlike
	// VM statuses; comparisons go through the case-insensitive poll matcher.
	DiskStatusActive = "Active"
)

// Disk is a block storage volume that exists independently of any VM.
type Disk struct {
	UUID             string `json:"uuid"`
	ID               int    `json:"id"`
	DisplayName      string `json:"display_name"`
	Name             string `json:"name"`
	SizeGB           int    `json:"size_gb"`
	Size             int    `json:"size"`
	Status           string `json:"status"`
	Pool             string `json:"pool"`
	Type             string `json:"type"`
	Primary          bool   `json:"primary"`
	Shared           bool   `json:"shared"`
	ReadOnlyBootable bool   `json:"read_only_bootable"`
	BillingAccountID int    `json:"billing_account_id"`

	// AttachedTo is the VM holding this disk, when the API reports one.
	AttachedTo string `json:"attached_to"`

	UserID    int    `json:"user_id"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// Capacity returns the disk size in GB, tolerating the two field names the API
// uses for it across endpoints.
func (d *Disk) Capacity() int {
	if d.SizeGB > 0 {
		return d.SizeGB
	}
	return d.Size
}

// CreateDiskRequest describes a new block volume.
type CreateDiskRequest struct {
	DisplayName      string
	SizeGB           int
	BillingAccountID int

	// SourceImageType is one of the DiskSource constants; SourceImage names the
	// image, disk or snapshot to clone when the type is not EMPTY.
	SourceImageType string
	SourceImage     string
}

// CreateDisk provisions a block volume.
//
// This endpoint is form-encoded despite the documentation showing a JSON body.
// Verified 2026-08-26: a JSON request is rejected with "'billing_account_id' is
// required if using a global API token" even when the field is present, because
// the body is never parsed. The same is true of the other disk endpoints.
func (c *Client) CreateDisk(ctx context.Context, location string, req CreateDiskRequest) (*Disk, error) {
	f := url.Values{}
	f.Set("size_gb", strconv.Itoa(req.SizeGB))
	f.Set("billing_account_id", strconv.Itoa(req.BillingAccountID))
	setStr(f, "display_name", req.DisplayName)
	setStr(f, "source_image_type", req.SourceImageType)
	setStr(f, "source_image", req.SourceImage)

	var out Disk
	err := c.Do(ctx, Request{
		Method:   http.MethodPost,
		Path:     "storage/disks",
		Scoped:   true,
		Location: location,
		Form:     f,
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// GetDisk reads a block volume.
func (c *Client) GetDisk(ctx context.Context, location, uuid string) (*Disk, error) {
	var out Disk
	err := c.Do(ctx, Request{
		Method:   http.MethodGet,
		Path:     "storage/disks/" + url.PathEscape(uuid),
		Scoped:   true,
		Location: location,
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// ListDisks returns every block volume in a location.
func (c *Client) ListDisks(ctx context.Context, location string) ([]Disk, error) {
	var out []Disk
	err := c.Do(ctx, Request{
		Method:   http.MethodGet,
		Path:     "storage/disks",
		Scoped:   true,
		Location: location,
	}, &out)
	return out, err
}

// UpdateDiskRequest carries the mutable disk metadata. Nil fields are left
// untouched.
type UpdateDiskRequest struct {
	DisplayName      *string
	BillingAccountID *int
	ReadOnlyBootable *bool
}

// UpdateDisk changes disk metadata. It does not resize the volume — growing a
// disk goes through the VM storage endpoint.
func (c *Client) UpdateDisk(ctx context.Context, location, uuid string, req UpdateDiskRequest) (*Disk, error) {
	f := url.Values{}
	if req.DisplayName != nil {
		f.Set("display_name", *req.DisplayName)
	}
	if req.BillingAccountID != nil {
		f.Set("billing_account_id", strconv.Itoa(*req.BillingAccountID))
	}
	if req.ReadOnlyBootable != nil {
		f.Set("read_only_bootable", strconv.FormatBool(*req.ReadOnlyBootable))
	}

	var out Disk
	err := c.Do(ctx, Request{
		Method:   http.MethodPatch,
		Path:     "storage/disks/" + url.PathEscape(uuid),
		Scoped:   true,
		Location: location,
		Form:     f,
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteDisk removes a block volume. The disk must not be attached.
func (c *Client) DeleteDisk(ctx context.Context, location, uuid string) error {
	return c.Do(ctx, Request{
		Method:   http.MethodDelete,
		Path:     "storage/disks/" + url.PathEscape(uuid),
		Scoped:   true,
		Location: location,
	}, nil)
}

// DiskAttachment is the result of attaching a disk, carrying the guest device
// name the volume appears as.
type DiskAttachment struct {
	// Name is the guest device, for example "vdb". The API also exposes it under
	// /dev/disk/by-id/virtio-<uuid>, which is the stable path to use in
	// cloud-init and fstab.
	Name string `json:"name"`
	UUID string `json:"uuid"`
}

// AttachDisk connects a block volume to a VM.
//
// This is a VM endpoint rather than a storage one. Like the disk endpoints, it
// is form-encoded.
func (c *Client) AttachDisk(ctx context.Context, location, vmUUID, diskUUID string) (*DiskAttachment, error) {
	f := url.Values{}
	f.Set("uuid", vmUUID)
	f.Set("storage_uuid", diskUUID)

	var out DiskAttachment
	err := c.Do(ctx, Request{
		Method:   http.MethodPost,
		Path:     "user-resource/vm/storage/attach",
		Scoped:   true,
		Location: location,
		Form:     f,
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// DetachDisk disconnects a block volume from a VM.
func (c *Client) DetachDisk(ctx context.Context, location, vmUUID, diskUUID string) error {
	f := url.Values{}
	f.Set("uuid", vmUUID)
	f.Set("storage_uuid", diskUUID)

	return c.Do(ctx, Request{
		Method:   http.MethodPost,
		Path:     "user-resource/vm/storage/detach",
		Scoped:   true,
		Location: location,
		Form:     f,
	}, nil)
}

// WaitForDisk blocks until a disk reaches its ready state.
func (c *Client) WaitForDisk(ctx context.Context, location, uuid string, timeout time.Duration) (*Disk, error) {
	var last *Disk

	poll := Poll{
		Fetch: func(ctx context.Context) (string, error) {
			disk, err := c.GetDisk(ctx, location, uuid)
			if err != nil {
				return "", err
			}
			last = disk
			return disk.Status, nil
		},
		Target:  []string{DiskStatusActive},
		Pending: []string{"creating", "pending", "provisioning", "attaching", "detaching"},
		Timeout: timeout,
	}
	if err := poll.Wait(ctx); err != nil {
		return nil, fmt.Errorf("waiting for disk %s: %w", uuid, err)
	}
	return last, nil
}

// WaitForDiskDeleted blocks until a disk is gone.
func (c *Client) WaitForDiskDeleted(ctx context.Context, location, uuid string, timeout time.Duration) error {
	poll := Poll{
		Fetch: func(ctx context.Context) (string, error) {
			disk, err := c.GetDisk(ctx, location, uuid)
			if err != nil {
				return "", err
			}
			return disk.Status, nil
		},
		Target:     []string{"deleted"},
		Pending:    []string{DiskStatusActive, "deleting"},
		GoneIsDone: true,
		Timeout:    timeout,
	}
	if err := poll.Wait(ctx); err != nil {
		return fmt.Errorf("waiting for disk %s to be deleted: %w", uuid, err)
	}
	return nil
}
