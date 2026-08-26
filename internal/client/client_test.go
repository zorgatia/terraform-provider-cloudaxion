package client

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// newTestClient wires a Client to a test server, disabling retries so a single
// request produces a single call unless a test asks otherwise.
func newTestClient(t *testing.T, handler http.Handler, opts ...Option) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	base := []Option{
		WithEndpoint(srv.URL + "/v1"),
		WithHTTPClient(srv.Client()),
		WithMaxRetries(0),
	}
	c, err := New("test-token", append(base, opts...)...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestNewRequiresAPIKey(t *testing.T) {
	if _, err := New("   "); err == nil {
		t.Fatal("expected an error for a blank API key")
	}
}

func TestDoSendsAPIKeyHeader(t *testing.T) {
	var got string
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("apikey")
		w.Write([]byte(`{}`))
	}))

	if err := c.Do(context.Background(), Request{Method: http.MethodGet, Path: "config/locations"}, nil); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if got != "test-token" {
		t.Errorf("apikey header = %q, want %q", got, "test-token")
	}
}

func TestLocationScoping(t *testing.T) {
	tests := []struct {
		name     string
		clientAt string
		req      Request
		wantPath string
	}{
		{
			name:     "unscoped request ignores the client location",
			clientAt: "tun1",
			req:      Request{Method: http.MethodGet, Path: "config/locations"},
			wantPath: "/v1/config/locations",
		},
		{
			name:     "scoped request inserts the slug after the version",
			clientAt: "tun1",
			req:      Request{Method: http.MethodGet, Path: "user-resource/vm", Scoped: true},
			wantPath: "/v1/tun1/user-resource/vm",
		},
		{
			name:     "per-request location wins over the client default",
			clientAt: "tun1",
			req:      Request{Method: http.MethodGet, Path: "user-resource/vm", Scoped: true, Location: "tun2"},
			wantPath: "/v1/tun2/user-resource/vm",
		},
		{
			name:     "empty location falls through to the account default",
			clientAt: "",
			req:      Request{Method: http.MethodGet, Path: "user-resource/vm", Scoped: true},
			wantPath: "/v1/user-resource/vm",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var gotPath string
			c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				w.Write([]byte(`{}`))
			}), WithLocation(tc.clientAt))

			if err := c.Do(context.Background(), tc.req, nil); err != nil {
				t.Fatalf("Do: %v", err)
			}
			if gotPath != tc.wantPath {
				t.Errorf("path = %q, want %q", gotPath, tc.wantPath)
			}
		})
	}
}

func TestFormAndJSONBodies(t *testing.T) {
	t.Run("form encodes as urlencoded", func(t *testing.T) {
		var contentType, body string
		c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			contentType = r.Header.Get("Content-Type")
			raw := make([]byte, r.ContentLength)
			r.Body.Read(raw)
			body = string(raw)
			w.Write([]byte(`{}`))
		}))

		form := url.Values{}
		form.Set("name", "node-1")
		form.Set("vcpu", "4")
		req := Request{Method: http.MethodPost, Path: "user-resource/vm", Scoped: true, Form: form}
		if err := c.Do(context.Background(), req, nil); err != nil {
			t.Fatalf("Do: %v", err)
		}

		if contentType != "application/x-www-form-urlencoded" {
			t.Errorf("Content-Type = %q", contentType)
		}
		if !strings.Contains(body, "name=node-1") || !strings.Contains(body, "vcpu=4") {
			t.Errorf("body = %q, want the form fields", body)
		}
	})

	t.Run("json encodes as application/json", func(t *testing.T) {
		var contentType, body string
		c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			contentType = r.Header.Get("Content-Type")
			raw := make([]byte, r.ContentLength)
			r.Body.Read(raw)
			body = string(raw)
			w.Write([]byte(`{}`))
		}))

		req := Request{
			Method: http.MethodPost,
			Path:   "network/firewalls",
			Scoped: true,
			JSON:   map[string]any{"display_name": "fw"},
		}
		if err := c.Do(context.Background(), req, nil); err != nil {
			t.Fatalf("Do: %v", err)
		}

		if contentType != "application/json" {
			t.Errorf("Content-Type = %q", contentType)
		}
		if !strings.Contains(body, `"display_name":"fw"`) {
			t.Errorf("body = %q", body)
		}
	})

	t.Run("setting both is rejected before any call is made", func(t *testing.T) {
		called := false
		c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
		}))

		req := Request{Method: http.MethodPost, Path: "x", Form: url.Values{}, JSON: map[string]any{}}
		if err := c.Do(context.Background(), req, nil); err == nil {
			t.Fatal("expected an error when both Form and JSON are set")
		}
		if called {
			t.Error("the request should not have been sent")
		}
	})
}

func TestQueryParameters(t *testing.T) {
	var got string
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query().Get("uuid")
		w.Write([]byte(`{}`))
	}))

	q := url.Values{}
	q.Set("uuid", "f80b1d62-ffe4-43ef-9210-60f05445456a")
	req := Request{Method: http.MethodGet, Path: "user-resource/vm", Scoped: true, Query: q}
	if err := c.Do(context.Background(), req, nil); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if got != "f80b1d62-ffe4-43ef-9210-60f05445456a" {
		t.Errorf("uuid query = %q", got)
	}
}

func TestDecodesIntoOut(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"uuid":"abc","name":"node-1","status":"running"}`))
	}))

	var out struct {
		UUID   string `json:"uuid"`
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	if err := c.Do(context.Background(), Request{Method: http.MethodGet, Path: "x"}, &out); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if out.UUID != "abc" || out.Name != "node-1" || out.Status != "running" {
		t.Errorf("decoded %+v", out)
	}
}

func TestEmptyBodyWithOutIsNotAnError(t *testing.T) {
	// Deletes answer 204 with no body; decoding must not be attempted.
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	var out map[string]any
	if err := c.Do(context.Background(), Request{Method: http.MethodDelete, Path: "x"}, &out); err != nil {
		t.Fatalf("Do: %v", err)
	}
}

func TestAPIErrorMapping(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		wantMsg    string
		wantIsFunc func(error) bool
	}{
		{
			name:       "documented error shape",
			status:     http.StatusBadRequest,
			body:       `{"errors": {"Error": "name is invalid"}}`,
			wantMsg:    "name is invalid",
			wantIsFunc: nil,
		},
		{
			name:       "field validation map",
			status:     http.StatusBadRequest,
			body:       `{"errors": {"name": "too short"}}`,
			wantMsg:    "name: too short",
			wantIsFunc: nil,
		},
		{
			name:       "not found",
			status:     http.StatusNotFound,
			body:       `{"errors": {"Error": "no such vm"}}`,
			wantMsg:    "no such vm",
			wantIsFunc: IsNotFound,
		},
		{
			name:       "unauthorized",
			status:     http.StatusUnauthorized,
			body:       `{"errors": {"Error": "bad token"}}`,
			wantMsg:    "bad token",
			wantIsFunc: IsUnauthorized,
		},
		{
			name:       "conflict",
			status:     http.StatusConflict,
			body:       `{"errors": {"Error": "duplicate name"}}`,
			wantMsg:    "duplicate name",
			wantIsFunc: IsConflict,
		},
		{
			name:       "non-JSON body falls back to raw text",
			status:     http.StatusBadGateway,
			body:       "upstream unavailable",
			wantMsg:    "upstream unavailable",
			wantIsFunc: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				w.Write([]byte(tc.body))
			}))

			err := c.Do(context.Background(), Request{Method: http.MethodGet, Path: "x"}, nil)
			if err == nil {
				t.Fatal("expected an error")
			}

			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("error is %T, want *APIError", err)
			}
			if apiErr.StatusCode != tc.status {
				t.Errorf("status = %d, want %d", apiErr.StatusCode, tc.status)
			}
			if apiErr.Message != tc.wantMsg {
				t.Errorf("message = %q, want %q", apiErr.Message, tc.wantMsg)
			}
			if tc.wantIsFunc != nil && !tc.wantIsFunc(err) {
				t.Error("classification helper did not match the error")
			}
		})
	}
}

func TestRetryOnServerErrorThenSucceed(t *testing.T) {
	calls := 0
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte(`{"ok":true}`))
	}), WithMaxRetries(3))

	if err := c.Do(context.Background(), Request{Method: http.MethodGet, Path: "x"}, nil); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3", calls)
	}
}

func TestClientErrorsAreNotRetried(t *testing.T) {
	calls := 0
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"errors":{"Error":"nope"}}`))
	}), WithMaxRetries(3))

	if err := c.Do(context.Background(), Request{Method: http.MethodGet, Path: "x"}, nil); err == nil {
		t.Fatal("expected an error")
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1 — 400 must not be retried", calls)
	}
}

func TestContextCancellationStopsRetries(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}), WithMaxRetries(5))

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	if err := c.Do(ctx, Request{Method: http.MethodGet, Path: "x"}, nil); err == nil {
		t.Fatal("expected an error")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("took %s — cancellation should have cut the backoff short", elapsed)
	}
}

func TestFormEncodesSpacesAsPercent20(t *testing.T) {
	// CloudAxion decodes form bodies with a strict percent-decoder: a "+" is a
	// literal plus, not a space. Go's default encoding would corrupt every
	// multi-word value — cloud-init documents and SSH keys above all.
	var body string
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body = string(raw)
		w.Write([]byte(`{}`))
	}))

	form := url.Values{}
	form.Set("cloud_init", "#cloud-config\nusers:\n  - name: ops\n")
	form.Set("public_keys", "ssh-ed25519 AAAAC3Nza key comment")
	form.Set("literal_plus", "a+b")

	req := Request{Method: http.MethodPost, Path: "user-resource/vm", Scoped: true, Form: form}
	if err := c.Do(context.Background(), req, nil); err != nil {
		t.Fatalf("Do: %v", err)
	}

	if strings.Contains(body, "+") {
		t.Errorf("body still contains a raw '+' separator:\n%s", body)
	}
	if !strings.Contains(body, "%20") {
		t.Errorf("spaces were not percent-encoded:\n%s", body)
	}

	// A literal plus in a value must survive as %2B, not be mangled.
	if !strings.Contains(body, "a%2Bb") {
		t.Errorf("a literal plus was not preserved as %%2B:\n%s", body)
	}

	// And the round trip has to give back exactly what went in.
	decoded, err := url.ParseQuery(body)
	if err != nil {
		t.Fatalf("re-parsing the body: %v", err)
	}
	for key, want := range map[string]string{
		"cloud_init":   "#cloud-config\nusers:\n  - name: ops\n",
		"public_keys":  "ssh-ed25519 AAAAC3Nza key comment",
		"literal_plus": "a+b",
	} {
		if got := decoded.Get(key); got != want {
			t.Errorf("%s round-tripped as %q, want %q", key, got, want)
		}
	}
}
