package client

import (
	"strings"
	"testing"
)

// The bodies and status codes below were captured from the live CloudAxion API
// on 2026-08-26. They are verbatim, because the whole point of these tests is
// that the real API does not behave the way its documentation describes.
func TestClassifyRealAPIResponses(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		gone    bool
		routing bool
		authErr bool
	}{
		{
			// The important one. A VM that does not exist answers 400, not 404.
			// Read must still drop it from state, or every plan after an
			// out-of-band delete fails instead of proposing a rebuild.
			name:   "unknown VM answers 400",
			status: 400,
			body:   `{"errors": {"Error": "No such virtual machine exists: 00000000-0000-0000-0000-000000000000"}}`,
			gone:   true,
		},
		{
			name:   "unknown network answers 400 with the undocumented message shape",
			status: 400,
			body:   `{"message":"Network UUID is invalid."}`,
			gone:   true,
		},
		{
			name:   "unknown disk answers 404",
			status: 404,
			body:   `{"message":"Disk not found"}`,
			gone:   true,
		},
		{
			name:   "unreserved floating IP answers 404",
			status: 404,
			body:   `{"message":"Ip address 203.0.113.99 was not found."}`,
			gone:   true,
		},
		{
			// An unknown location slug answers 404 too. Treating this as "gone"
			// would silently erase real resources from state over a typo.
			name:    "unknown location slug is a routing error, not a missing resource",
			status:  404,
			body:    `{"message":"no route and no API found with those values"}`,
			gone:    false,
			routing: true,
		},
		{
			name:    "invalid token answers 403",
			status:  403,
			body:    `{"message":"Invalid authentication credentials"}`,
			authErr: true,
		},
		{
			name:    "missing apikey header answers 401",
			status:  401,
			body:    `{"message":"missing apikey header"}`,
			authErr: true,
		},
		{
			// A genuine validation failure must stay an error. If this were
			// classified as "gone", a malformed configuration would look like
			// successful drift detection.
			name:   "missing required parameter stays an error",
			status: 400,
			body:   `{"errors": {"uuid": "Required parameter 'uuid' not supplied"}}`,
			gone:   false,
		},
		{
			name:   "empty 400 body stays an error",
			status: 400,
			body:   ``,
			gone:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := newAPIError(tc.status, []byte(tc.body))

			if got := IsNotFound(err); got != tc.gone {
				t.Errorf("IsNotFound = %v, want %v (message %q)", got, tc.gone, err.Message)
			}
			if got := IsRoutingError(err); got != tc.routing {
				t.Errorf("IsRoutingError = %v, want %v", got, tc.routing)
			}
			if got := IsUnauthorized(err); got != tc.authErr {
				t.Errorf("IsUnauthorized = %v, want %v", got, tc.authErr)
			}
		})
	}
}

func TestExtractsBothErrorShapes(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "documented shape",
			body: `{"errors": {"Error": "No such virtual machine exists: abc"}}`,
			want: "No such virtual machine exists: abc",
		},
		{
			name: "undocumented message shape used by network, disk and auth endpoints",
			body: `{"message":"Network UUID is invalid."}`,
			want: "Network UUID is invalid.",
		},
		{
			name: "field-keyed validation errors",
			body: `{"errors": {"uuid": "Required parameter 'uuid' not supplied"}}`,
			want: "uuid: Required parameter 'uuid' not supplied",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := extractMessage([]byte(tc.body)); got != tc.want {
				t.Errorf("extractMessage = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDescribeErrorAddsHints(t *testing.T) {
	routing := newAPIError(404, []byte(`{"message":"no route and no API found with those values"}`))
	if !strings.Contains(DescribeError(routing), "location") {
		t.Error("a routing error should point at the location slug — it is the usual cause")
	}

	auth := newAPIError(403, []byte(`{"message":"Invalid authentication credentials"}`))
	if !strings.Contains(DescribeError(auth), "CLOUDAXION_API_KEY") {
		t.Error("an auth error should name the environment variable to check")
	}

	plain := newAPIError(400, []byte(`{"errors":{"Error":"name too short"}}`))
	if got := DescribeError(plain); got != plain.Error() {
		t.Errorf("an ordinary error should not gain a hint, got %q", got)
	}
}
