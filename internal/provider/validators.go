package provider

import (
	"context"
	"unicode"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// vmPasswordValidator enforces the rule the live API applies to VM passwords:
// at least eight characters, containing a lowercase letter, an uppercase letter
// and a digit.
//
// The API expresses this as a lookahead regex. Go's regexp package is RE2 and
// has no lookaheads, so the check is written out rather than translated —
// attempting the regex would panic at provider startup.
//
// Validating here turns a rejected apply into a failed plan, which is both
// faster and safer: VM creation is synchronous and slow, so a late rejection
// costs a minute of waiting.
type vmPasswordValidator struct{}

func (v vmPasswordValidator) Description(_ context.Context) string {
	return "at least 8 characters, with a lowercase letter, an uppercase letter and a digit"
}

func (v vmPasswordValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v vmPasswordValidator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}

	password := req.ConfigValue.ValueString()

	var hasLower, hasUpper, hasDigit bool
	for _, r := range password {
		switch {
		case unicode.IsLower(r):
			hasLower = true
		case unicode.IsUpper(r):
			hasUpper = true
		case unicode.IsDigit(r):
			hasDigit = true
		}
	}

	if len(password) >= 8 && hasLower && hasUpper && hasDigit {
		return
	}

	// The password itself is never echoed back — it is a secret, and a
	// diagnostic is not a safe place for one.
	resp.Diagnostics.AddAttributeError(
		req.Path,
		"Invalid password",
		"CloudAxion requires at least 8 characters including a lowercase letter, "+
			"an uppercase letter and a digit. Consider using public_keys instead: "+
			"the API never returns the password, so Terraform cannot detect drift on it.",
	)
}

// VMPassword returns the VM password validator.
func VMPassword() validator.String { return vmPasswordValidator{} }
