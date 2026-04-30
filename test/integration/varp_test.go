//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestEAPI_VarpLifecycle drives the global VARP MAC binding through the
// eAPI session primitives the l2.Varp resource uses.
func TestEAPI_VarpLifecycle(t *testing.T) {
	cli := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	t.Cleanup(func() {
		ctxC, cancelC := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancelC()
		sess, err := cli.OpenSession(ctxC, "pulumi-it-varp-cleanup")
		if err != nil {
			return
		}
		// Sweep both possible canonical forms; ignore failures from the
		// non-existent one.
		_ = sess.Stage(ctxC, []string{"no ip virtual-router mac-address 00:1c:73:00:00:01"})
		_ = sess.Stage(ctxC, []string{"no ip virtual-router mac-address 00:aa:00:bb:00:cc"})
		_ = sess.Commit(ctxC)
	})

	// 1. Create. Submit Cisco-style; cEOS normalises to colon-separated
	// lowercase under `show running-config`.
	mustApply(ctx, t, cli, "pulumi-it-varp-create", []string{
		"ip virtual-router mac-address 001c.7300.0001",
	})

	cfg := mustShowVarp(ctx, t, cli)
	if !strings.Contains(cfg, "ip virtual-router mac-address 00:1c:73:00:00:01") {
		t.Fatalf("expected VARP MAC 00:1c:73:00:00:01, got:\n%s", cfg)
	}

	// 2. Update — replace MAC.
	mustApply(ctx, t, cli, "pulumi-it-varp-update", []string{
		"ip virtual-router mac-address 00:aa:00:bb:00:cc",
	})

	cfg = mustShowVarp(ctx, t, cli)
	if !strings.Contains(cfg, "ip virtual-router mac-address 00:aa:00:bb:00:cc") {
		t.Fatalf("expected updated VARP MAC, got:\n%s", cfg)
	}
	if strings.Contains(cfg, "00:1c:73:00:00:01") {
		t.Fatalf("expected old MAC gone, got:\n%s", cfg)
	}

	// 3. Delete.
	mustApply(ctx, t, cli, "pulumi-it-varp-delete", []string{"no ip virtual-router mac-address 00:aa:00:bb:00:cc"})

	cfg = mustShowVarp(ctx, t, cli)
	if strings.Contains(cfg, "ip virtual-router mac-address") {
		t.Fatalf("expected VARP gone, got:\n%s", cfg)
	}
}

func mustShowVarp(ctx context.Context, t *testing.T, cli runCmds) string {
	t.Helper()
	resp, err := cli.RunCmds(ctx, []string{"show running-config | include ^ip virtual-router mac-address"}, "text")
	if err != nil {
		t.Fatalf("show running-config | include ip virtual-router mac-address: %v", err)
	}
	if len(resp) == 0 {
		return ""
	}
	if v, ok := resp[0]["output"].(string); ok {
		return v
	}
	return ""
}
