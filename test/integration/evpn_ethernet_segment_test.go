//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestEAPI_EvpnEthernetSegmentLifecycle drives the ESI block lifecycle
// against cEOS via the same eAPI session primitives the
// l2.EvpnEthernetSegment resource uses.
//
// Steps:
//
//  1. Create Port-Channel99 (parent interface — switchport trunk).
//  2. Apply ESI block: identifier + redundancy single-active + RT.
//  3. Read — assert running-config contains the ESI lines.
//  4. Update — switch redundancy to all-active + add DF preference algo.
//  5. Read — assert the new lines and absence of single-active.
//  6. Delete — `no evpn ethernet-segment`. Read — block gone, parent kept.
func TestEAPI_EvpnEthernetSegmentLifecycle(t *testing.T) {
	cli := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	const parent = "Port-Channel99"

	t.Cleanup(func() {
		ctxC, cancelC := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancelC()
		sess, err := cli.OpenSession(ctxC, "pulumi-it-es-cleanup")
		if err != nil {
			return
		}
		_ = sess.Stage(ctxC, []string{"no interface " + parent})
		_ = sess.Commit(ctxC)
	})

	// 1. Create the parent Port-Channel.
	mustApply(ctx, t, cli, "pulumi-it-es-parent", []string{
		"interface " + parent,
		"switchport",
		"switchport mode trunk",
		"switchport trunk allowed vlan 100-200",
	})

	// 2. Create the ESI block.
	mustApply(ctx, t, cli, "pulumi-it-es-create", []string{
		"interface " + parent,
		"no evpn ethernet-segment",
		"evpn ethernet-segment",
		"identifier 0011:1111:1111:1111:1111",
		"redundancy single-active",
		"route-target import 12:34:12:34:12:34",
	})

	cfg := mustShowRunningInterface(ctx, t, cli, parent)
	if !strings.Contains(cfg, "evpn ethernet-segment") {
		t.Fatalf("expected evpn ethernet-segment block, got:\n%s", cfg)
	}
	if !strings.Contains(cfg, "identifier 0011:1111:1111:1111:1111") {
		t.Fatalf("expected identifier, got:\n%s", cfg)
	}
	if !strings.Contains(cfg, "redundancy single-active") {
		t.Fatalf("expected redundancy single-active, got:\n%s", cfg)
	}

	// 3. Update — all-active + preference DF.
	mustApply(ctx, t, cli, "pulumi-it-es-update", []string{
		"interface " + parent,
		"no evpn ethernet-segment",
		"evpn ethernet-segment",
		"identifier 0011:1111:1111:1111:1111",
		"redundancy all-active",
		"route-target import 12:34:12:34:12:34",
		"designated-forwarder election algorithm preference 10000",
	})

	cfg = mustShowRunningInterface(ctx, t, cli, parent)
	if strings.Contains(cfg, "redundancy single-active") {
		t.Fatalf("expected stale single-active gone, got:\n%s", cfg)
	}
	if !strings.Contains(cfg, "designated-forwarder election algorithm preference 10000") {
		t.Fatalf("expected DF preference 10000, got:\n%s", cfg)
	}

	// 4. Delete the ESI block.
	mustApply(ctx, t, cli, "pulumi-it-es-delete", []string{
		"interface " + parent,
		"no evpn ethernet-segment",
	})

	cfg = mustShowRunningInterface(ctx, t, cli, parent)
	if strings.Contains(cfg, "evpn ethernet-segment") {
		t.Fatalf("expected ESI block gone, got:\n%s", cfg)
	}
	if !strings.Contains(cfg, "interface "+parent) {
		t.Fatalf("expected parent %s to remain, got:\n%s", parent, cfg)
	}
}
