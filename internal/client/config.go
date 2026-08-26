package client

import (
	"context"
	"net/http"
)

// Location is a CloudAxion data centre. Requests that omit a slug act on the
// location whose IsDefault is true.
type Location struct {
	Slug        string `json:"slug"`
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
	CountryCode string `json:"country_code"`
	IsDefault   bool   `json:"is_default"`
	IsPreferred bool   `json:"is_preferred"`
	OrderNr     int    `json:"order_nr"`
}

// ListLocations returns every location available to the account.
//
// There is no endpoint that lists resources across locations, so callers that
// need a global view must iterate over this list.
func (c *Client) ListLocations(ctx context.Context) ([]Location, error) {
	var out []Location
	err := c.Do(ctx, Request{Method: http.MethodGet, Path: "config/locations"}, &out)
	return out, err
}

// ImageVersion is one selectable version of a VM image.
type ImageVersion struct {
	OSVersion   string `json:"os_version"`
	DisplayName string `json:"display_name"`
	Published   bool   `json:"published"`
}

// Image is a VM base image family. Images are selected on VM creation by the
// (OSName, OSVersion) pair; there is no image identifier.
type Image struct {
	OSName       string         `json:"os_name"`
	DisplayName  string         `json:"display_name"`
	UIPosition   int            `json:"ui_position"`
	IsDefault    bool           `json:"is_default"`
	IsAppCatalog bool           `json:"is_app_catalog"`
	Versions     []ImageVersion `json:"versions"`
}

// ListImages returns the VM image catalogue.
func (c *Client) ListImages(ctx context.Context) ([]Image, error) {
	var out []Image
	err := c.Do(ctx, Request{Method: http.MethodGet, Path: "config/vm_images"}, &out)
	return out, err
}

// ListPlainOSImages returns only plain operating-system images, excluding the
// application catalogue.
func (c *Client) ListPlainOSImages(ctx context.Context) ([]Image, error) {
	var out []Image
	err := c.Do(ctx, Request{Method: http.MethodGet, Path: "config/vm_images/plain_os"}, &out)
	return out, err
}

// ListAppCatalogImages returns only application-catalogue images.
func (c *Client) ListAppCatalogImages(ctx context.Context) ([]Image, error) {
	var out []Image
	err := c.Do(ctx, Request{Method: http.MethodGet, Path: "config/vm_images/app_catalog"}, &out)
	return out, err
}

// VMParameterLimit narrows a parameter's constraint for a particular value of
// another parameter, for example a higher RAM minimum when os_name is windows.
type VMParameterLimit struct {
	OSName    string   `json:"os_name"`
	Min       *int     `json:"min"`
	Max       *int     `json:"max"`
	Mandatory *bool    `json:"mandatory"`
	Values    []string `json:"values"`
}

// VMParameter is one machine-readable constraint on VM creation.
//
// Constraint is "range", "regexp" or "enum"; the fields that matter depend on
// which. LimitedBy names a parameter that narrows this one via Limits.
type VMParameter struct {
	Parameter   string             `json:"parameter"`
	Type        string             `json:"type"`
	Constraint  string             `json:"constraint"`
	Description string             `json:"description"`
	Mandatory   bool               `json:"mandatory"`
	Min         *int               `json:"min"`
	Max         *int               `json:"max"`
	Expression  string             `json:"expression"`
	Values      []string           `json:"values"`
	LimitedBy   string             `json:"limited_by"`
	Limits      []VMParameterLimit `json:"limits"`
}

// ListVMParameters returns the constraints the API applies to VM creation.
//
// Note the unusual path: this endpoint lives under /api/parameters/vm, not
// under /config like the rest of the catalogue.
func (c *Client) ListVMParameters(ctx context.Context) ([]VMParameter, error) {
	var out []VMParameter
	err := c.Do(ctx, Request{Method: http.MethodGet, Path: "api/parameters/vm"}, &out)
	return out, err
}

// HostPool is a server class a VM can be placed on.
type HostPool struct {
	UUID               string `json:"uuid"`
	Name               string `json:"name"`
	Description        string `json:"description"`
	IsDefaultDesignate bool   `json:"is_default_designated"`
	CreatedAt          string `json:"created_at"`
	UpdatedAt          string `json:"updated_at"`
}

// ListHostPools returns the server classes available in a location.
func (c *Client) ListHostPools(ctx context.Context, location string) ([]HostPool, error) {
	var out []HostPool
	err := c.Do(ctx, Request{
		Method:   http.MethodGet,
		Path:     "user-resource/host_pool/list",
		Scoped:   true,
		Location: location,
	}, &out)
	return out, err
}

// BillingAccount identifies who pays for a resource. Its ID is a required field
// on almost every create call, which is why this read-only lookup exists even
// though billing itself is out of scope for the provider.
type BillingAccount struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	IsDefault bool   `json:"is_default"`
	Currency  string `json:"currency"`
	Type      string `json:"type"`
}

// ListBillingAccounts returns the account's billing accounts.
func (c *Client) ListBillingAccounts(ctx context.Context) ([]BillingAccount, error) {
	var out []BillingAccount
	err := c.Do(ctx, Request{Method: http.MethodGet, Path: "payment/billing_account/list"}, &out)
	return out, err
}

// S3Info describes the S3-compatible endpoint fronting object storage.
//
// Only bucket lifecycle is managed through the CloudAxion API; object-level
// concerns (ACLs, versioning, encryption, policies) go through this endpoint
// with an ordinary S3 client.
type S3Info struct {
	Endpoint string `json:"endpoint"`
	Region   string `json:"region"`
	URL      string `json:"url"`
}

// GetS3Info returns the object-storage S3 endpoint.
func (c *Client) GetS3Info(ctx context.Context) (*S3Info, error) {
	var out S3Info
	if err := c.Do(ctx, Request{Method: http.MethodGet, Path: "storage/api/s3"}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
