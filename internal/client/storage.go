package client

import (
	"context"
	"net/http"
	"net/url"
)

// Bucket is an object-storage bucket.
//
// Only the bucket's lifecycle lives in the CloudAxion API. Everything inside it
// — objects, ACLs, versioning, encryption, lifecycle rules, policies — is
// reached through the S3-compatible endpoint from GetS3Info, using an ordinary
// S3 client.
type Bucket struct {
	Name             string `json:"name"`
	BillingAccountID int    `json:"billing_account_id"`
	Size             int64  `json:"size"`
	ObjectCount      int64  `json:"object_count"`
	UserID           int    `json:"user_id"`
	CreatedAt        string `json:"created_at"`
	UpdatedAt        string `json:"updated_at"`
}

// CreateBucket creates an object-storage bucket. Note the verb: this endpoint
// is a PUT, not a POST.
func (c *Client) CreateBucket(ctx context.Context, name string, billingAccountID int) (*Bucket, error) {
	body := map[string]any{"name": name}
	if billingAccountID != 0 {
		body["billing_account_id"] = billingAccountID
	}

	var out Bucket
	err := c.Do(ctx, Request{
		Method: http.MethodPut,
		Path:   "storage/bucket",
		JSON:   body,
	}, &out)
	if err != nil {
		return nil, err
	}
	if out.Name == "" {
		out.Name = name
	}
	return &out, nil
}

// GetBucket reads a bucket by name.
func (c *Client) GetBucket(ctx context.Context, name string) (*Bucket, error) {
	q := url.Values{}
	q.Set("name", name)

	var out Bucket
	err := c.Do(ctx, Request{
		Method: http.MethodGet,
		Path:   "storage/bucket",
		Query:  q,
	}, &out)
	if err != nil {
		return nil, err
	}
	if out.Name == "" {
		out.Name = name
	}
	return &out, nil
}

// ListBuckets returns every bucket on the account.
func (c *Client) ListBuckets(ctx context.Context) ([]Bucket, error) {
	var out []Bucket
	err := c.Do(ctx, Request{Method: http.MethodGet, Path: "storage/bucket/list"}, &out)
	return out, err
}

// UpdateBucketBillingAccount moves a bucket to another billing account, which
// is the only mutable bucket attribute.
func (c *Client) UpdateBucketBillingAccount(ctx context.Context, name string, billingAccountID int) (*Bucket, error) {
	var out Bucket
	err := c.Do(ctx, Request{
		Method: http.MethodPatch,
		Path:   "storage/bucket",
		JSON: map[string]any{
			"name":               name,
			"billing_account_id": billingAccountID,
		},
	}, &out)
	if err != nil {
		return nil, err
	}
	if out.Name == "" {
		out.Name = name
	}
	return &out, nil
}

// DeleteBucket removes a bucket. The bucket must be empty; emptying it is an S3
// operation, not a CloudAxion one.
func (c *Client) DeleteBucket(ctx context.Context, name string) error {
	q := url.Values{}
	q.Set("name", name)

	return c.Do(ctx, Request{
		Method: http.MethodDelete,
		Path:   "storage/bucket",
		Query:  q,
	}, nil)
}

// S3Key is an access key pair for the S3-compatible endpoint.
//
// SecretKey is only returned when the pair is created; it is never readable
// again, so callers must persist it at that moment.
type S3Key struct {
	AccessKey string `json:"access_key"`
	SecretKey string `json:"secret_key"`
	UserID    int    `json:"user_id"`
	CreatedAt string `json:"created_at"`
}

// CreateS3Key generates an S3 access key pair.
func (c *Client) CreateS3Key(ctx context.Context) (*S3Key, error) {
	var out S3Key
	if err := c.Do(ctx, Request{Method: http.MethodPost, Path: "storage/user/keys"}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListS3Keys returns the account's S3 access keys, without their secrets.
func (c *Client) ListS3Keys(ctx context.Context) ([]S3Key, error) {
	var out []S3Key
	err := c.Do(ctx, Request{Method: http.MethodGet, Path: "storage/user/keys"}, &out)
	return out, err
}

// DeleteS3Key revokes an S3 access key pair.
func (c *Client) DeleteS3Key(ctx context.Context, accessKey string) error {
	q := url.Values{}
	q.Set("access_key", accessKey)

	return c.Do(ctx, Request{
		Method: http.MethodDelete,
		Path:   "storage/user/keys",
		Query:  q,
	}, nil)
}

// SSHKey is a stored public key that can be injected into new VMs.
type SSHKey struct {
	UUID      string `json:"uuid"`
	Name      string `json:"name"`
	PublicKey string `json:"public_key"`

	Fingerprint string `json:"fingerprint"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// CreateSSHKey stores a public key on the account.
func (c *Client) CreateSSHKey(ctx context.Context, name, publicKey string) (*SSHKey, error) {
	var out SSHKey
	err := c.Do(ctx, Request{
		Method: http.MethodPost,
		Path:   "user-resource/ssh_keys",
		JSON: map[string]string{
			"name":       name,
			"public_key": publicKey,
		},
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// GetSSHKey reads one stored key.
//
// The API documents no single-key GET, so this filters the list.
func (c *Client) GetSSHKey(ctx context.Context, uuid string) (*SSHKey, error) {
	keys, err := c.ListSSHKeys(ctx)
	if err != nil {
		return nil, err
	}
	for i := range keys {
		if keys[i].UUID == uuid {
			return &keys[i], nil
		}
	}
	return nil, &APIError{
		StatusCode: http.StatusNotFound,
		Message:    "ssh key " + uuid + " not found",
	}
}

// ListSSHKeys returns every stored public key.
func (c *Client) ListSSHKeys(ctx context.Context) ([]SSHKey, error) {
	var out []SSHKey
	err := c.Do(ctx, Request{Method: http.MethodGet, Path: "user-resource/ssh_keys"}, &out)
	return out, err
}

// RenameSSHKey changes a stored key's name. The key material itself is immutable.
func (c *Client) RenameSSHKey(ctx context.Context, uuid, name string) (*SSHKey, error) {
	var out SSHKey
	err := c.Do(ctx, Request{
		Method: http.MethodPatch,
		Path:   "user-resource/ssh_keys/" + url.PathEscape(uuid),
		JSON:   map[string]string{"name": name},
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteSSHKey removes a stored public key.
func (c *Client) DeleteSSHKey(ctx context.Context, uuid string) error {
	return c.Do(ctx, Request{
		Method: http.MethodDelete,
		Path:   "user-resource/ssh_keys/" + url.PathEscape(uuid),
	}, nil)
}
