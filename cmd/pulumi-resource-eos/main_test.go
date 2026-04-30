package main

import (
	"context"
	"testing"

	"github.com/dantte-lp/pulumi-eos/internal/provider"
)

func TestRun_Version(t *testing.T) {
	t.Parallel()
	if err := run(context.Background(), []string{"-version"}); err != nil {
		t.Fatalf("run(-version): %v", err)
	}
	if err := run(context.Background(), []string{"--version"}); err != nil {
		t.Fatalf("run(--version): %v", err)
	}
}

// TestProvider_Build ensures the inferred provider builds without error.
// This catches Annotate-pointer mistakes, schema-generation panics, and
// missing resource registrations early.
//
// The full gRPC entry point (provider.Provider.Run) is exercised in S10
// acceptance tests; calling it from a unit test would register process-wide
// command-line flags ("tracing", "logflow", …) and break test re-runs.
func TestProvider_Build(t *testing.T) {
	t.Parallel()
	p, err := provider.New()
	if err != nil {
		t.Fatalf("provider.New(): %v", err)
	}
	_ = p
}
