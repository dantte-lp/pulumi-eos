// Package main is the entry point for pulumi-eos-gen, a code-generation helper.
//
// Responsibilities:
//   - Emit the Pulumi JSON schema from the in-memory resource registry.
//   - Generate per-resource documentation pages from schema metadata.
//   - Optionally regenerate Go bindings from upstream protobuf sources.
//
// Wiring is added in Sprint S3 (schema + Args/State freeze) and Sprint S4
// (provider runtime).
package main

import (
	"fmt"
	"os"

	"github.com/dantte-lp/pulumi-eos/internal/version"
)

func main() {
	fmt.Printf("pulumi-eos-gen %s\n", version.Full())
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: pulumi-eos-gen <schema|docs|protos>")
		os.Exit(2)
	}
	fmt.Fprintln(os.Stderr, "subcommand wiring is scheduled for Sprint S3.")
	os.Exit(0)
}
