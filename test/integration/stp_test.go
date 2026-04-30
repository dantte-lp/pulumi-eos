//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestEAPI_StpLifecycle drives the global spanning-tree configuration
// through the eAPI session primitives the l2.Stp resource uses.
func TestEAPI_StpLifecycle(t *testing.T) {
	cli := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	t.Cleanup(func() {
		ctxC, cancelC := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancelC()
		sess, err := cli.OpenSession(ctxC, "pulumi-it-stp-cleanup")
		if err != nil {
			return
		}
		_ = sess.Stage(ctxC, []string{
			"default spanning-tree mode",
			"no spanning-tree edge-port bpduguard default",
			"no spanning-tree mst configuration",
		})
		_ = sess.Commit(ctxC)
	})

	// 1. Create — mode mstp, edge-port BPDU Guard default, MST region.
	mustApply(ctx, t, cli, "pulumi-it-stp-create", []string{
		"spanning-tree mode mstp",
		"spanning-tree edge-port bpduguard default",
		"no spanning-tree mst configuration",
		"spanning-tree mst configuration",
		"name REGION-A",
		"revision 7",
		"instance 1 vlan 100-199",
		"instance 2 vlan 200-299",
		"exit",
	})

	cfg := mustShowStp(ctx, t, cli)
	if !strings.Contains(cfg, "spanning-tree mode mstp") {
		t.Fatalf("expected mode mstp, got:\n%s", cfg)
	}
	if !strings.Contains(cfg, "spanning-tree edge-port bpduguard default") {
		t.Fatalf("expected edge-port bpduguard default, got:\n%s", cfg)
	}
	// cEOS canonicalises `instance N vlan <range>` to two spaces between
	// `vlan` and the range. Assert on the normalised form (single-space)
	// after collapsing runs of whitespace.
	canonical := strings.Join(strings.Fields(cfg), " ")
	if !strings.Contains(canonical, "name REGION-A") || !strings.Contains(canonical, "instance 1 vlan 100-199") {
		t.Fatalf("expected MST config, got:\n%s", cfg)
	}

	// 2. Update — rstp, drop MST.
	mustApply(ctx, t, cli, "pulumi-it-stp-update", []string{
		"spanning-tree mode rstp",
		"no spanning-tree mst configuration",
	})

	cfg = mustShowStp(ctx, t, cli)
	if !strings.Contains(cfg, "spanning-tree mode rstp") {
		t.Fatalf("expected mode rstp, got:\n%s", cfg)
	}
	if strings.Contains(cfg, "name REGION-A") {
		t.Fatalf("expected MST gone, got:\n%s", cfg)
	}

	// 3. Reset to defaults.
	mustApply(ctx, t, cli, "pulumi-it-stp-reset", []string{
		"default spanning-tree mode",
		"no spanning-tree edge-port bpduguard default",
		"no spanning-tree mst configuration",
	})

	cfg = mustShowStp(ctx, t, cli)
	if strings.Contains(cfg, "spanning-tree edge-port bpduguard default") {
		t.Fatalf("expected bpduguard default gone, got:\n%s", cfg)
	}
}

func mustShowStp(ctx context.Context, t *testing.T, cli runCmds) string {
	t.Helper()
	resp, err := cli.RunCmds(ctx, []string{"show running-config | section spanning-tree"}, "text")
	if err != nil {
		t.Fatalf("show running-config | section spanning-tree: %v", err)
	}
	if len(resp) == 0 {
		return ""
	}
	if v, ok := resp[0]["output"].(string); ok {
		return v
	}
	return ""
}
