// Package main is the entry point for the pulumi-eos resource provider plugin.
//
// The binary is named pulumi-resource-eos per Pulumi's plugin discovery contract.
// It is invoked by the Pulumi engine over gRPC and serves resource CRUD requests
// for Arista EOS devices and CloudVision (CVP / CVaaS) fleets.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/dantte-lp/pulumi-eos/internal/version"
)

const providerName = "eos"

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "pulumi-resource-%s: %v\n", providerName, err)
		os.Exit(1)
	}
}

// run is the testable entry point.
//
// In Sprint 4 (S4) it will be replaced by:
//
//	provider, err := internalprovider.New(version.Full())
//	if err != nil { return err }
//	return provider.Run(ctx, providerName, version.Version)
//
// For now it only handles -version / -schema introspection so the binary is
// usable in CI smoke tests.
func run(_ context.Context, args []string) error {
	if len(args) == 1 {
		switch args[0] {
		case "-version", "--version":
			fmt.Println(version.Full())
			return nil
		case "-schema", "--schema":
			fmt.Println(`{"name":"eos","version":"` + version.Version + `","resources":{}}`)
			return nil
		}
	}
	return errors.New("provider runtime not yet wired (Sprint S4)")
}
