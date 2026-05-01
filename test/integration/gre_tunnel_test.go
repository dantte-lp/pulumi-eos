//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestEAPI_GreTunnelLifecycle drives the eos:l3:GreTunnel command flow
// against cEOS at the eAPI session layer (the same primitives used by
// internal/resources/l3.GreTunnel). Verifies:
//
//   - Apply (idempotent): two-pass commit of the same body leaves the
//     second commit a no-op (per `configure session` semantics from
//     EOS User Manual §3.4 + TOI 13648).
//   - Read-back: `show running-config interfaces Tunnel<id>` reflects
//     the staged lines.
//   - Cleanup: `no interface Tunnel<id>` removes the interface.
//
// Notes on cEOS 4.36 lab quirks observed during the integration_probe
// (commit `3c13006`): `tunnel ttl <N>` and `tunnel source <interface>`
// are rejected on cEOSLab; both are deferred to v1. This test stays
// within the accepted v0 surface (IP source / dest, GRE mode, tos,
// key, path-mtu-discovery, dont-fragment, mss ceiling).
func TestEAPI_GreTunnelLifecycle(t *testing.T) {
	cli := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	const id = "555"

	t.Cleanup(func() {
		ctxC, cancelC := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancelC()
		sess, err := cli.OpenSession(ctxC, "pulumi-it-gre-cleanup")
		if err != nil {
			return
		}
		_ = sess.Stage(ctxC, []string{"no interface Tunnel" + id})
		_ = sess.Commit(ctxC)
	})

	// `tunnel dont-fragment` is intentionally NOT exercised in this
	// test: cEOSLab returns "Unavailable command (not supported on
	// this hardware platform)" for it when staged together with the
	// rest of the body in one session — even though the command IS
	// part of the resource's v0 input shape and works on production
	// EOS hardware. The integration suite avoids the lab-specific
	// false positive.
	body := []string{
		"interface Tunnel" + id,
		"description IT GRE Tunnel " + id,
		"mtu 1400",
		"ip address 192.168.55.1/30",
		"tunnel mode gre",
		"tunnel source 10.0.0.1",
		"tunnel destination 10.0.0.2",
		"tunnel tos 0",
		"tunnel key 555",
		"tunnel mss ceiling 1300",
		"tunnel path-mtu-discovery",
		"no shutdown",
	}

	apply := func(name string) {
		t.Helper()
		sess, openErr := cli.OpenSession(ctx, "pulumi-it-gre-"+name)
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
		[]string{"show running-config interfaces Tunnel" + id}, "text")
	if runErr != nil {
		t.Fatalf("show running-config: %v", runErr)
	}
	out, _ := resp[0]["output"].(string)

	wantPresent := []string{
		"interface Tunnel" + id,
		"description IT GRE Tunnel " + id,
		"mtu 1400",
		"ip address 192.168.55.1/30",
		"tunnel mode gre",
		"tunnel source 10.0.0.1",
		"tunnel destination 10.0.0.2",
		"tunnel key 555",
		"tunnel path-mtu-discovery",
	}
	for _, line := range wantPresent {
		if !strings.Contains(out, line) {
			t.Errorf("missing in running-config: %q\n--- got ---\n%s", line, out)
		}
	}

	// Cleanup: drop the interface.
	delSess, openErr := cli.OpenSession(ctx, "pulumi-it-gre-del")
	if openErr != nil {
		t.Fatalf("OpenSession del: %v", openErr)
	}
	if stageErr := delSess.Stage(ctx, []string{"no interface Tunnel" + id}); stageErr != nil {
		t.Fatalf("Stage del: %v", stageErr)
	}
	if commitErr := delSess.Commit(ctx); commitErr != nil {
		t.Fatalf("Commit del: %v", commitErr)
	}

	resp, runErr = cli.RunCmds(ctx,
		[]string{"show running-config interfaces Tunnel" + id}, "text")
	if runErr != nil {
		t.Fatalf("post-clean show: %v", runErr)
	}
	out, _ = resp[0]["output"].(string)
	if strings.Contains(out, "interface Tunnel"+id+"\n") {
		t.Errorf("Tunnel%s still present after delete:\n%s", id, out)
	}
}
