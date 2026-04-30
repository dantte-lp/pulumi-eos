// Package main is the entry point for the pulumi-eos resource provider plugin.
//
// The binary is named pulumi-resource-eos per Pulumi's plugin discovery
// contract. It is invoked by the Pulumi engine over gRPC and serves resource
// CRUD requests for Arista EOS devices and CloudVision (CVP / CVaaS) fleets.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/dantte-lp/pulumi-eos/internal/provider"
	"github.com/dantte-lp/pulumi-eos/internal/version"
)

const providerName = "eos"

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "pulumi-resource-%s: %v\n", providerName, err)
		os.Exit(1)
	}
}

// run is the testable entry point. It supports a small set of pre-engine
// inspection flags before handing the binary off to the Pulumi engine over
// the provider gRPC contract.
func run(ctx context.Context, args []string) error {
	if len(args) == 1 {
		switch args[0] {
		case "-version", "--version":
			fmt.Println(version.Full())
			return nil
		}
	}

	p, err := provider.New()
	if err != nil {
		return fmt.Errorf("build provider: %w", err)
	}
	return p.Run(ctx, providerName, version.Version)
}
