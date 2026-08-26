package client

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// APIError is a non-2xx response from the CloudAxion API.
//
// The documented error shape is {"errors": {"Error": "description"}}, but not
// every endpoint is guaranteed to use it, so the raw body is kept for diagnostics.
type APIError struct {
	StatusCode int
	Message    string
	Body       string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("cloudaxion: HTTP %d: %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("cloudaxion: HTTP %d", e.StatusCode)
}

// Retryable reports whether re-sending the request could plausibly succeed.
// Server faults, gateway errors and rate limiting are retried; client errors are not.
func (e *APIError) Retryable() bool {
	switch e.StatusCode {
	case http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

// newAPIError builds an APIError, extracting the human-readable message from
// the documented body shape when it is present.
func newAPIError(status int, body []byte) *APIError {
	raw := strings.TrimSpace(string(body))
	return &APIError{
		StatusCode: status,
		Message:    extractMessage(body),
		Body:       raw,
	}
}

// extractMessage pulls a message out of the response body, tolerating the
// several shapes the API is known or likely to use.
func extractMessage(body []byte) string {
	if len(body) == 0 {
		return ""
	}

	// The documented shape: {"errors": {"Error": "..."}}. The map may also hold
	// per-field validation messages, so every value is collected.
	var wrapped struct {
		Errors  json.RawMessage `json:"errors"`
		Error   string          `json:"error"`
		Message string          `json:"message"`
		Detail  string          `json:"detail"`
	}
	if err := json.Unmarshal(body, &wrapped); err == nil {
		if msg := flattenErrors(wrapped.Errors); msg != "" {
			return msg
		}
		for _, candidate := range []string{wrapped.Error, wrapped.Message, wrapped.Detail} {
			if candidate != "" {
				return candidate
			}
		}
	}

	// Not JSON, or an unrecognised shape: fall back to the raw body, truncated
	// so a stray HTML error page cannot flood a diagnostic.
	raw := strings.TrimSpace(string(body))
	const maxLen = 512
	if len(raw) > maxLen {
		return raw[:maxLen] + "…"
	}
	return raw
}

// flattenErrors renders the "errors" member, which may be an object of
// field/message pairs, an array of strings, or a bare string.
func flattenErrors(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	var asMap map[string]any
	if err := json.Unmarshal(raw, &asMap); err == nil {
		parts := make([]string, 0, len(asMap))
		// "Error" is the documented generic key; render it without a prefix so
		// the common case reads naturally.
		if generic, ok := asMap["Error"]; ok {
			if s := stringify(generic); s != "" {
				parts = append(parts, s)
			}
		}
		for key, value := range asMap {
			if key == "Error" {
				continue
			}
			if s := stringify(value); s != "" {
				parts = append(parts, key+": "+s)
			}
		}
		return strings.Join(parts, "; ")
	}

	var asList []string
	if err := json.Unmarshal(raw, &asList); err == nil {
		return strings.Join(asList, "; ")
	}

	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return asString
	}

	return strings.TrimSpace(string(raw))
}

func stringify(v any) string {
	switch typed := v.(type) {
	case string:
		return typed
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			if s := stringify(item); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, ", ")
	case nil:
		return ""
	default:
		return fmt.Sprint(typed)
	}
}
