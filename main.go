package main

import (
	"context"
	"flag"
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
	var debug bool
	flag.BoolVar(&debug, "debug", false, "Start provider in debug mode (set TF_REATTACH_PROVIDERS to connect)")
	flag.Parse()

	if err := providerserver.Serve(context.Background(), New(version), providerserver.ServeOpts{
		Address: providerAddress,
		Debug:   debug,
	}); err != nil {
		log.Fatal(err)
	}
}
