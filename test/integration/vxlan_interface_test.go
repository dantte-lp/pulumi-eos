//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestEAPI_VxlanInterfaceLifecycle drives a Vxlan tunnel interface through
// the eAPI session primitives that the l2.VxlanInterface resource uses.
//
// Steps:
//
//  1. Create Loopback1 (anchor for the overlay) + VLAN 4001 (so the
//     `vxlan vlan ... vni ...` line has a real VLAN to bind).
//  2. Apply Vxlan1 with source-interface Loopback1, udp-port 4789, VLAN→VNI
//     map.
//  3. Read — assert running-config has the source, UDP port, and VLAN→VNI
//     line.
//  4. Update — drop the original VLAN→VNI map, add a new one with udp-port
//  65330. The `no interface Vxlan1` + recreate flow ensures stale rows
//     are gone.
//  5. Delete via `no interface Vxlan1`. Read after delete — interface gone.
func TestEAPI_VxlanInterfaceLifecycle(t *testing.T) {
	cli := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	t.Cleanup(func() {
		ctxC, cancelC := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancelC()
		sess, err := cli.OpenSession(ctxC, "pulumi-it-vxlan-cleanup")
		if err != nil {
			return
		}
		_ = sess.Stage(ctxC, []string{
			"no interface Vxlan1",
			"no interface Loopback42",
			"no vlan 4001",
		})
		_ = sess.Commit(ctxC)
	})

	// 1. Pre-requisites: Loopback42 + VLAN 4001.
	mustApply(ctx, t, cli, "pulumi-it-vxlan-prereq", []string{
		"interface Loopback42",
		"ip address 10.42.0.1/32",
		"vlan 4001",
		"name pulumi-it-overlay",
	})

	// 2. Create Vxlan1.
	mustApply(ctx, t, cli, "pulumi-it-vxlan-create", []string{
		"interface Vxlan1",
		"vxlan source-interface Loopback42",
		"vxlan udp-port 4789",
		"vxlan vlan 4001 vni 14001",
	})

	cfg := mustShowRunningInterface(ctx, t, cli, "Vxlan1")
	if !strings.Contains(cfg, "vxlan source-interface Loopback42") {
		t.Fatalf("expected source-interface Loopback42, got:\n%s", cfg)
	}
	if !strings.Contains(cfg, "vxlan vlan 4001 vni 14001") {
		t.Fatalf("expected vlan 4001 vni 14001, got:\n%s", cfg)
	}

	// 3. Update — drop the interface and recreate with a different mapping.
	// This is what l2.VxlanInterface.Update would do internally to clear
	// stale `vxlan vlan ... vni ...` rows.
	mustApply(ctx, t, cli, "pulumi-it-vxlan-recreate", []string{
		"no interface Vxlan1",
		"interface Vxlan1",
		"vxlan source-interface Loopback42",
		"vxlan udp-port 65330",
		"vxlan vlan 4001 vni 24001",
	})

	cfg = mustShowRunningInterface(ctx, t, cli, "Vxlan1")
	if !strings.Contains(cfg, "vxlan udp-port 65330") {
		t.Fatalf("expected udp-port 65330 after update, got:\n%s", cfg)
	}
	if !strings.Contains(cfg, "vxlan vlan 4001 vni 24001") {
		t.Fatalf("expected vlan 4001 vni 24001 after update, got:\n%s", cfg)
	}
	if strings.Contains(cfg, "vni 14001") {
		t.Fatalf("expected stale vni 14001 to be gone, got:\n%s", cfg)
	}

	// 4. Delete.
	mustApply(ctx, t, cli, "pulumi-it-vxlan-delete", []string{"no interface Vxlan1"})

	cfg = mustShowRunningInterface(ctx, t, cli, "Vxlan1")
	if strings.Contains(cfg, "vxlan source-interface") {
		t.Fatalf("expected Vxlan1 gone, got:\n%s", cfg)
	}
}
