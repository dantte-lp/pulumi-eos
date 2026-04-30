//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dantte-lp/pulumi-eos/internal/client/eapi"
)

// TestEAPI_InterfaceLifecycle drives a physical interface through the same
// eAPI session primitives the l2.Interface resource uses.
//
// cEOS exposes Ethernet1, Ethernet2, ... by default. We mutate Ethernet48
// (high-numbered to avoid colliding with what other tests touch) and reset
// it back at the end via `default interface`.
//
// Steps:
//
//  1. Set description, mtu, switchport mode trunk, allowed vlans 100-200.
//  2. Read — assert running-config has the new lines.
//  3. Update — switch to access mode + access vlan 800.
//  4. Read — assert.
//  5. Reset via `default interface Ethernet48`.
//  6. Read — defaults restored (no description).
func TestEAPI_InterfaceLifecycle(t *testing.T) {
	cli := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	const ifname = "Ethernet48"

	t.Cleanup(func() {
		ctxC, cancelC := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancelC()
		sess, err := cli.OpenSession(ctxC, "pulumi-it-iface-cleanup")
		if err != nil {
			return
		}
		_ = sess.Stage(ctxC, []string{"default interface " + ifname})
		_ = sess.Commit(ctxC)
	})

	// 1. Configure as trunk.
	mustApply(ctx, t, cli, "pulumi-it-iface-trunk", []string{
		"interface " + ifname,
		"description pulumi-it",
		"mtu 9214",
		"switchport",
		"switchport mode trunk",
		"switchport trunk allowed vlan 100-200",
	})

	// 2. Read.
	cfg := mustShowRunningInterface(ctx, t, cli, ifname)
	if !strings.Contains(cfg, "description pulumi-it") {
		t.Fatalf("expected description pulumi-it, got:\n%s", cfg)
	}
	if !strings.Contains(cfg, "switchport mode trunk") {
		t.Fatalf("expected mode trunk, got:\n%s", cfg)
	}
	if !strings.Contains(cfg, "switchport trunk allowed vlan 100-200") {
		t.Fatalf("expected allowed-vlan 100-200, got:\n%s", cfg)
	}

	// 3. Update — re-purpose as access port.
	mustApply(ctx, t, cli, "pulumi-it-iface-access", []string{
		"interface " + ifname,
		"switchport mode access",
		"switchport access vlan 800",
	})

	cfg = mustShowRunningInterface(ctx, t, cli, ifname)
	// cEOS only echoes `switchport mode <X>` when X is non-default; access
	// is the default, so we assert the absence of `mode trunk` instead.
	if strings.Contains(cfg, "switchport mode trunk") {
		t.Fatalf("expected mode trunk gone after access switch, got:\n%s", cfg)
	}
	if !strings.Contains(cfg, "switchport access vlan 800") {
		t.Fatalf("expected access-vlan 800, got:\n%s", cfg)
	}

	// 4. Reset to defaults.
	mustApply(ctx, t, cli, "pulumi-it-iface-reset", []string{
		"default interface " + ifname,
	})

	cfg = mustShowRunningInterface(ctx, t, cli, ifname)
	if strings.Contains(cfg, "description pulumi-it") {
		t.Fatalf("expected description gone after default, got:\n%s", cfg)
	}
}

func mustShowRunningInterface(ctx context.Context, t *testing.T, cli *eapi.Client, name string) string {
	t.Helper()
	resp, err := cli.RunCmds(ctx, []string{"show running-config interfaces " + name}, "text")
	if err != nil {
		t.Fatalf("show running-config interfaces %s: %v", name, err)
	}
	if len(resp) == 0 {
		return ""
	}
	if v, ok := resp[0]["output"].(string); ok {
		return v
	}
	return ""
}
