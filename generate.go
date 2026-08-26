//go:build generate

package main

// Regenerate docs/ from the schemas and the examples/ tree.
//go:generate go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs generate --provider-name cloudaxion
