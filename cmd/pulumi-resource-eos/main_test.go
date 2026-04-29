package main

import (
	"context"
	"strings"
	"testing"
)

func TestRun_Version(t *testing.T) {
	t.Parallel()
	if err := run(context.Background(), []string{"-version"}); err != nil {
		t.Fatalf("run(-version): %v", err)
	}
}

func TestRun_Schema(t *testing.T) {
	t.Parallel()
	if err := run(context.Background(), []string{"-schema"}); err != nil {
		t.Fatalf("run(-schema): %v", err)
	}
}

func TestRun_Empty(t *testing.T) {
	t.Parallel()
	err := run(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for empty args")
	}
	if !strings.Contains(err.Error(), "Sprint S4") {
		t.Fatalf("unexpected error: %v", err)
	}
}
