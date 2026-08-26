package client

import (
	"errors"
	"net/http"
	"regexp"
	"strings"
)

// CloudAxion does not use 404 consistently for "this resource does not exist".
// Verified against the live API on 2026-08-26:
//
//	GET /vm?uuid=<unknown>        400  {"errors": {"Error": "No such virtual machine exists: …"}}
//	GET /network/network/<unknown> 400  {"message": "Network UUID is invalid."}
//	GET /storage/disks/<unknown>   404  {"message": "Disk not found"}
//	GET /network/ip_addresses/<x>  404  {"message": "Ip address … was not found."}
//
// This matters more than it looks. Terraform's Read must distinguish "gone" from
// "broken": on gone it drops the resource from state so the next plan proposes
// recreating it, and on broken it must fail loudly. Treating the 400 cases as
// hard errors would make `tofu plan` blow up whenever anything was deleted
// outside Terraform — the single most common drift there is.
//
// Matching on message text is unpleasant but unavoidable: the status code alone
// does not carry the distinction, and a blanket "400 means gone" rule would
// silently swallow genuine validation failures.
var notFoundMessages = regexp.MustCompile(
	`(?i)(no such .* exists|not found|was not found|does not exist|is invalid|no longer exists)`,
)

// routingMessages are the API's answer to an unknown path — which includes an
// unknown location slug. A mistyped `location` must surface as an error, not be
// mistaken for a deleted resource and silently dropped from state.
var routingMessages = regexp.MustCompile(
	`(?i)(no route and no api found|no route found)`,
)

// IsNotFound reports whether err means the resource is absent.
//
// It accepts a 404, and a 400 whose message reads as absence. Routing failures
// are explicitly excluded: an unknown location slug also answers 404, and must
// not be mistaken for a missing resource.
func IsNotFound(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}

	if routingMessages.MatchString(apiErr.Message) {
		return false
	}

	switch apiErr.StatusCode {
	case http.StatusNotFound:
		return true
	case http.StatusBadRequest:
		return notFoundMessages.MatchString(apiErr.Message)
	default:
		return false
	}
}

// IsRoutingError reports whether err came from an unknown path. In practice
// that almost always means an invalid location slug or an endpoint this
// provider has wrong, and it deserves a clearer message than "not found".
func IsRoutingError(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return routingMessages.MatchString(apiErr.Message)
}

// IsUnauthorized reports whether err is an authentication or authorisation
// failure.
//
// A missing apikey header answers 401 while an invalid token answers 403, so
// both count.
func IsUnauthorized(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.StatusCode == http.StatusUnauthorized || apiErr.StatusCode == http.StatusForbidden
}

// IsConflict reports whether err is a 409, which the API returns for duplicate
// names among other things.
func IsConflict(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusConflict
}

// DescribeError renders err for a Terraform diagnostic, adding a hint when the
// cause is one the message alone does not explain.
func DescribeError(err error) string {
	if err == nil {
		return ""
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return err.Error()
	}

	var b strings.Builder
	b.WriteString(apiErr.Error())

	switch {
	case IsRoutingError(err):
		b.WriteString("\n\nThe API reported no route for this request. " +
			"The most common cause is an unknown location slug — check the provider's " +
			"`location` against the cloudaxion_locations data source.")
	case IsUnauthorized(err):
		b.WriteString("\n\nCheck that CLOUDAXION_API_KEY holds a valid, unexpired token " +
			"with access to this resource.")
	}

	return b.String()
}
