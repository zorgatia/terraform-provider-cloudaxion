// Package client is a hand-written Go SDK for the CloudAxion REST API.
//
// CloudAxion publishes no OpenAPI specification; see docs/api-notes.md for the
// recorded contract this package is written against. Two properties of that API
// shape everything here:
//
//   - Request bodies are form-encoded on some endpoints and JSON on others, so a
//     Request carries either Form or JSON, never both.
//   - Many endpoints are location-scoped, meaning the location slug is injected
//     into the path immediately after the API version.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultEndpoint is the public CloudAxion API root, version included.
const DefaultEndpoint = "https://api.cloudaxion.net/v1"

const (
	// CloudAxion has no task or job endpoint because its write operations are
	// synchronous: the call blocks until the work is done and returns the final
	// state. Measured against the live API on 2026-08-26, creating the smallest
	// possible VM (1 vCPU, 512 MB, 20 GB) blocked for 33 seconds, and stopping it
	// for 22. A larger Windows VM will take considerably longer.
	//
	// The client timeout is therefore a backstop against a hung connection, not a
	// per-operation budget. Real deadlines come from the context Terraform passes
	// in, which carries the resource's own timeouts block.
	defaultTimeout    = 30 * time.Minute
	defaultMaxRetries = 3
	defaultUserAgent  = "terraform-provider-cloudaxion"
)

// Client talks to the CloudAxion API. It is safe for concurrent use.
type Client struct {
	endpoint   string
	apiKey     string
	location   string
	userAgent  string
	maxRetries int
	httpClient *http.Client
}

// Option customises a Client.
type Option func(*Client)

// WithEndpoint overrides the API root. The trailing slash is optional.
func WithEndpoint(endpoint string) Option {
	return func(c *Client) {
		if endpoint != "" {
			c.endpoint = strings.TrimRight(endpoint, "/")
		}
	}
}

// WithLocation sets the default location slug used by location-scoped requests.
// An empty slug means "the account's default location", which is what the API
// does when no slug appears in the path.
func WithLocation(slug string) Option {
	return func(c *Client) { c.location = strings.Trim(slug, "/") }
}

// WithHTTPClient supplies the underlying HTTP client, mainly for tests.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) {
		if h != nil {
			c.httpClient = h
		}
	}
}

// WithUserAgent appends provider and version information to the User-Agent.
func WithUserAgent(ua string) Option {
	return func(c *Client) {
		if ua != "" {
			c.userAgent = ua
		}
	}
}

// WithTimeout overrides the HTTP client timeout. It guards against a hung
// connection; per-operation deadlines belong on the context.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) {
		if d > 0 {
			c.httpClient.Timeout = d
		}
	}
}

// WithMaxRetries caps how many times a retryable failure is re-attempted.
// Zero disables retrying.
func WithMaxRetries(n int) Option {
	return func(c *Client) {
		if n >= 0 {
			c.maxRetries = n
		}
	}
}

// New builds a Client. The API key is sent as the "apikey" header on every request.
func New(apiKey string, opts ...Option) (*Client, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("cloudaxion: API key is required")
	}
	c := &Client{
		endpoint:   DefaultEndpoint,
		apiKey:     apiKey,
		userAgent:  defaultUserAgent,
		maxRetries: defaultMaxRetries,
		httpClient: &http.Client{Timeout: defaultTimeout},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// Location reports the client's default location slug, which may be empty.
func (c *Client) Location() string { return c.location }

// Request describes a single API call.
//
// Exactly one of Form and JSON may be set. Scoped requests get the location slug
// inserted after the API version; Location overrides the client default for one call.
type Request struct {
	Method   string
	Path     string // relative to the endpoint, without a leading slash
	Scoped   bool   // insert the location slug after the version
	Location string // per-request location override
	Query    url.Values
	Form     url.Values
	JSON     any
}

// Do performs req and decodes a successful JSON response into out, which may be
// nil when the response body is not needed (for example a 204 on delete).
func (c *Client) Do(ctx context.Context, req Request, out any) error {
	body, contentType, err := encodeBody(req)
	if err != nil {
		return err
	}

	endpoint, err := c.buildURL(req)
	if err != nil {
		return err
	}

	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			if err := sleepBackoff(ctx, attempt); err != nil {
				return err
			}
		}

		httpReq, err := http.NewRequestWithContext(ctx, req.Method, endpoint, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("cloudaxion: building request: %w", err)
		}
		httpReq.Header.Set("apikey", c.apiKey)
		httpReq.Header.Set("Accept", "application/json")
		httpReq.Header.Set("User-Agent", c.userAgent)
		if contentType != "" {
			httpReq.Header.Set("Content-Type", contentType)
		}

		resp, err := c.httpClient.Do(httpReq)
		if err != nil {
			// Transport failures are always worth retrying; the request either
			// never arrived or its response was lost.
			lastErr = fmt.Errorf("cloudaxion: %s %s: %w", req.Method, endpoint, err)
			continue
		}

		respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		resp.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("cloudaxion: reading response: %w", readErr)
			continue
		}

		if resp.StatusCode >= 400 {
			apiErr := newAPIError(resp.StatusCode, respBody)
			if apiErr.Retryable() {
				lastErr = apiErr
				continue
			}
			return apiErr
		}

		if out == nil || len(bytes.TrimSpace(respBody)) == 0 {
			return nil
		}
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("cloudaxion: decoding %s %s response: %w", req.Method, endpoint, err)
		}
		return nil
	}

	return lastErr
}

// buildURL assembles the full request URL, inserting the location slug for
// scoped requests and appending any query parameters.
func (c *Client) buildURL(req Request) (string, error) {
	slug := c.location
	if req.Location != "" {
		slug = req.Location
	}

	path := strings.TrimLeft(req.Path, "/")
	if req.Scoped && slug != "" {
		path = strings.Trim(slug, "/") + "/" + path
	}

	u, err := url.Parse(c.endpoint + "/" + path)
	if err != nil {
		return "", fmt.Errorf("cloudaxion: building URL for %q: %w", req.Path, err)
	}
	if len(req.Query) > 0 {
		u.RawQuery = req.Query.Encode()
	}
	return u.String(), nil
}

// encodeBody renders the request body and its content type. Form and JSON are
// mutually exclusive: the API uses one or the other per endpoint, never both.
func encodeBody(req Request) ([]byte, string, error) {
	switch {
	case req.Form != nil && req.JSON != nil:
		return nil, "", fmt.Errorf("cloudaxion: request to %q sets both Form and JSON", req.Path)
	case req.Form != nil:
		return []byte(encodeForm(req.Form)), "application/x-www-form-urlencoded", nil
	case req.JSON != nil:
		body, err := json.Marshal(req.JSON)
		if err != nil {
			return nil, "", fmt.Errorf("cloudaxion: encoding JSON body for %q: %w", req.Path, err)
		}
		return body, "application/json", nil
	default:
		return nil, "", nil
	}
}

// encodeForm renders form values with spaces percent-encoded as %20 rather than
// as "+".
//
// Both are legal in application/x-www-form-urlencoded, and Go's
// url.Values.Encode emits "+". CloudAxion decodes with a strict percent-decoder
// that does not treat "+" as a space, so a "+" arrives as a literal plus.
//
// This is not cosmetic. Verified 2026-08-26: a cloud-init document sent with
// "+" separators reached the guest with every space replaced by a plus, which
// made the YAML unparseable. cloud-init then silently skipped it — including
// the users block that installs the SSH keys — leaving a VM nobody could log
// into. The same document sent with %20 worked.
//
// url.Values.Encode already percent-encodes a literal plus as %2B, so replacing
// the remaining separators is unambiguous.
func encodeForm(values url.Values) string {
	return strings.ReplaceAll(values.Encode(), "+", "%20")
}

// sleepBackoff waits before retry attempt n (1-based), or returns early if the
// context is cancelled.
func sleepBackoff(ctx context.Context, attempt int) error {
	delay := time.Duration(1<<uint(attempt-1)) * time.Second
	if delay > 8*time.Second {
		delay = 8 * time.Second
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
