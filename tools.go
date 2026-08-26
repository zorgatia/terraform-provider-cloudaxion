//go:build tools

// Package main pins the documentation generator so `go generate` uses a known
// version rather than whatever happens to be installed.
package main

import (
	_ "github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs"
)
