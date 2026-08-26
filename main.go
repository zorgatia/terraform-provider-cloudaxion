// terraform-provider-cloudaxion manages CloudAxion infrastructure from
// OpenTofu or Terraform.
//
// See docs/api-notes.md for the recorded CloudAxion API contract: the vendor
// publishes no OpenAPI specification, so that file is the reference.
package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/zorgatia/terraform-provider-cloudaxion/internal/provider"
)

// version is overwritten at release time by GoReleaser ldflags.
var version = "dev"

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "run with support for debuggers such as delve")
	flag.Parse()

	opts := providerserver.ServeOpts{
		// The registry address, which is also what users put in
		// required_providers. It must match the published namespace.
		Address: "registry.opentofu.org/zorgatia/cloudaxion",
		Debug:   debug,
	}

	if err := providerserver.Serve(context.Background(), provider.New(version), opts); err != nil {
		log.Fatal(err.Error())
	}
}
