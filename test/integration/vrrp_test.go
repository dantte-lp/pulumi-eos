//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestEAPI_VrrpLifecycle drives `vrrp <vrid> <subcommand>` lifecycle
// against cEOS at the eAPI session layer (the same primitives used by
// internal/resources/l3.Vrrp). EOS uses inline-form VRRP — every
// per-attribute line is rendered as `vrrp <vrid> <attr> <val>` under
// the parent interface; the bare `vrrp <vrid>` does NOT enter a
// sub-mode (rejected as "Incomplete command").
//
// Verifies:
//   - Apply (idempotent): two-pass commit of the same body leaves the
//     second commit a no-op (`configure session` semantics, EOS User
//     Manual §3.4 + TOI 13648).
//   - Read-back: `show running-config interfaces Vlan<N>` reflects the
//     rendered lines.
//   - Cleanup: `no vrrp <vrid>` removes the group while preserving any
//     other VRRP groups on the same interface.
func TestEAPI_VrrpLifecycle(t *testing.T) {
	cli := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	const vlanID = "555"
	const intf = "Vlan" + vlanID
	const vrid = "10"

	t.Cleanup(func() {
		ctxC, cancelC := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancelC()
		sess, err := cli.OpenSession(ctxC, "pulumi-it-vrrp-cleanup")
		if err != nil {
			return
		}
		_ = sess.Stage(ctxC, []string{
			"no interface " + intf,
			"no vlan " + vlanID,
			"no ip routing",
		})
		_ = sess.Commit(ctxC)
	})

	// Pre-req: ip routing + parent SVI with an IP. This is the
	// minimum context EOS accepts before the per-vrrp-line emits.
	preSess, openErr := cli.OpenSession(ctx, "pulumi-it-vrrp-pre")
	if openErr != nil {
		t.Fatalf("OpenSession pre: %v", openErr)
	}
	if stageErr := preSess.Stage(ctx, []string{
		"ip routing",
		"vlan " + vlanID,
		"name VRRP_TEST",
		"exit",
		"interface " + intf,
		"ip address 10.55.0.1/24",
	}); stageErr != nil {
		t.Fatalf("Stage pre: %v", stageErr)
	}
	if commitErr := preSess.Commit(ctx); commitErr != nil {
		t.Fatalf("Commit pre: %v", commitErr)
	}

	// v0 surface — `tracked-object` lines and BFD/IP are exercised
	// by the unit-test render only; `tracked-object <NAME>` requires
	// a global `track <name> ...` resource (deferred to S6 close-out
	// follow-up `eos:l3:Tracker`), and `bfd ip` requires a peer
	// fabric leg. Integration body covers identity + the always-
	// applicable inline-form attributes.
	body := []string{
		"interface " + intf,
		"vrrp " + vrid + " ipv4 10.55.0.254",
		"vrrp " + vrid + " ipv4 10.55.0.253 secondary",
		"vrrp " + vrid + " priority-level 200",
		"vrrp " + vrid + " preempt",
		"vrrp " + vrid + " preempt delay minimum 30",
		"vrrp " + vrid + " advertisement interval 5",
		"vrrp " + vrid + " session description IT-VRRP",
		"no vrrp " + vrid + " disabled",
	}

	apply := func(name string) {
		t.Helper()
		sess, openErr := cli.OpenSession(ctx, "pulumi-it-vrrp-"+name)
		if openErr != nil {
			t.Fatalf("OpenSession %s: %v", name, openErr)
		}
		if stageErr := sess.Stage(ctx, body); stageErr != nil {
			t.Fatalf("Stage %s: %v", name, stageErr)
		}
		if commitErr := sess.Commit(ctx); commitErr != nil {
			t.Fatalf("Commit %s: %v", name, commitErr)
		}
	}
	apply("apply1")
	apply("apply2") // idempotent re-emit

	resp, runErr := cli.RunCmds(ctx,
		[]string{"show running-config interfaces " + intf}, "text")
	if runErr != nil {
		t.Fatalf("show running-config: %v", runErr)
	}
	out, _ := resp[0]["output"].(string)

	// EOS elides defaults from `show running-config` (trimmed view).
	// `vrrp <vrid> preempt` is on by default so it never appears
	// unless disabled — we don't assert it.
	wantPresent := []string{
		"interface " + intf,
		"vrrp " + vrid + " ipv4 10.55.0.254",
		"vrrp " + vrid + " ipv4 10.55.0.253 secondary",
		"vrrp " + vrid + " priority-level 200",
		"vrrp " + vrid + " preempt delay minimum 30",
		"vrrp " + vrid + " advertisement interval 5",
		"vrrp " + vrid + " session description IT-VRRP",
	}
	for _, line := range wantPresent {
		if !strings.Contains(out, line) {
			t.Errorf("missing in running-config: %q\n--- got ---\n%s", line, out)
		}
	}

	// Cleanup: drop just the VRRP group; the parent interface stays.
	delSess, delErr := cli.OpenSession(ctx, "pulumi-it-vrrp-del")
	if delErr != nil {
		t.Fatalf("OpenSession del: %v", delErr)
	}
	if stageErr := delSess.Stage(ctx, []string{
		"interface " + intf,
		"no vrrp " + vrid,
	}); stageErr != nil {
		t.Fatalf("Stage del: %v", stageErr)
	}
	if commitErr := delSess.Commit(ctx); commitErr != nil {
		t.Fatalf("Commit del: %v", commitErr)
	}

	resp, runErr = cli.RunCmds(ctx,
		[]string{"show running-config interfaces " + intf}, "text")
	if runErr != nil {
		t.Fatalf("post-clean show: %v", runErr)
	}
	out, _ = resp[0]["output"].(string)
	if strings.Contains(out, "vrrp "+vrid+" ") {
		t.Errorf("vrrp %s still present after delete:\n%s", vrid, out)
	}
	// Parent interface must still exist.
	if !strings.Contains(out, "interface "+intf) {
		t.Errorf("parent %s should still exist:\n%s", intf, out)
	}
}
