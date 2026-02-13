package main

import (
	"context"
	"log"

	// Packages
	providerserver "github.com/hashicorp/terraform-plugin-framework/providerserver"
)

///////////////////////////////////////////////////////////////////////////////
// GLOBALS

const (
	providerAddress = "registry.terraform.io/mutablelogic/kaiak"
)

// version is set at build time via ldflags.
var version string

///////////////////////////////////////////////////////////////////////////////
// MAIN

func main() {
	if err := providerserver.Serve(context.Background(), New(version), providerserver.ServeOpts{
		Address: providerAddress,
	}); err != nil {
		log.Fatal(err)
	}
}
