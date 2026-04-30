//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestEAPI_PortChannelLifecycle drives a Port-Channel lifecycle end-to-end
// through the same eAPI session primitives the l2.PortChannel resource uses.
//
// Steps:
//
//  1. Create Port-Channel 99 with description, mtu, trunk + allowed-vlan,
//     LACP static fallback, timeout 100 s.
//  2. Read — assert running-config has the new lines.
//  3. Update — switch fallback to individual, change timeout.
//  4. Delete via `no interface Port-Channel99`; verify absence.
func TestEAPI_PortChannelLifecycle(t *testing.T) {
	cli := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	const ifname = "Port-Channel99"

	t.Cleanup(func() {
		ctxC, cancelC := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancelC()
		sess, err := cli.OpenSession(ctxC, "pulumi-it-po-cleanup")
		if err != nil {
			return
		}
		_ = sess.Stage(ctxC, []string{"no interface " + ifname})
		_ = sess.Commit(ctxC)
	})

	mustApply(ctx, t, cli, "pulumi-it-po-create", []string{
		"interface " + ifname,
		"description pulumi-it",
		"mtu 9214",
		"switchport",
		"switchport mode trunk",
		"switchport trunk allowed vlan 100-200",
		"port-channel lacp fallback static",
		"port-channel lacp fallback timeout 100",
	})

	cfg := mustShowRunningInterface(ctx, t, cli, ifname)
	for _, want := range []string{
		"description pulumi-it",
		"switchport mode trunk",
		"port-channel lacp fallback static",
		"port-channel lacp fallback timeout 100",
	} {
		if !strings.Contains(cfg, want) {
			t.Fatalf("expected %q present, got:\n%s", want, cfg)
		}
	}

	mustApply(ctx, t, cli, "pulumi-it-po-update", []string{
		"interface " + ifname,
		"port-channel lacp fallback individual",
		"port-channel lacp fallback timeout 50",
	})
	cfg = mustShowRunningInterface(ctx, t, cli, ifname)
	if !strings.Contains(cfg, "port-channel lacp fallback individual") {
		t.Fatalf("expected fallback individual, got:\n%s", cfg)
	}
	if !strings.Contains(cfg, "port-channel lacp fallback timeout 50") {
		t.Fatalf("expected timeout 50, got:\n%s", cfg)
	}

	mustApply(ctx, t, cli, "pulumi-it-po-delete", []string{"no interface " + ifname})
	cfg = mustShowRunningInterface(ctx, t, cli, ifname)
	if strings.Contains(cfg, "interface "+ifname) {
		t.Fatalf("expected port-channel gone, got:\n%s", cfg)
	}
}
