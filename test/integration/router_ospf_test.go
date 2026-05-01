//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestEAPI_RouterOspfLifecycle drives `router ospf` lifecycle against
// cEOS at the eAPI session layer (the same primitives used by
// internal/resources/l3.RouterOspf). Verifies:
//
//   - Apply (idempotent re-emit): two-pass commit of the same body
//     leaves the second commit a no-op (per
//     `configure session` semantics from EOS User Manual §3.4 +
//     TOI 13648).
//   - Read-back: `show running-config section ospf` reflects the
//     staged lines (router-id, network, redistribute, area type).
//   - Cleanup: `no router ospf <id>` removes the process atomically.
func TestEAPI_RouterOspfLifecycle(t *testing.T) {
	cli := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	const instance = "777"

	// Cleanup fires regardless of mid-test failure.
	t.Cleanup(func() {
		ctxC, cancelC := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancelC()
		sess, err := cli.OpenSession(ctxC, "pulumi-it-ospf-cleanup")
		if err != nil {
			return
		}
		_ = sess.Stage(ctxC, []string{"no router ospf " + instance})
		_ = sess.Commit(ctxC)
	})

	// Pre-req: ip routing must be on. This is bootstrapped by
	// scripts/integration-bootstrap.sh in CI, but be defensive in
	// case a developer ran the test without it.
	ipSess, openErr := cli.OpenSession(ctx, "pulumi-it-ospf-pre")
	if openErr != nil {
		t.Fatalf("ip-routing pre-session: %v", openErr)
	}
	if stageErr := ipSess.Stage(ctx, []string{"ip routing"}); stageErr != nil {
		t.Fatalf("stage ip routing: %v", stageErr)
	}
	if commitErr := ipSess.Commit(ctx); commitErr != nil {
		t.Fatalf("commit ip routing: %v", commitErr)
	}

	body := []string{
		"router ospf " + instance,
		"router-id 7.7.7.7",
		"no shutdown",
		"passive-interface default",
		"redistribute connected",
		"area 0.0.0.1 stub",
		"network 10.77.0.0/24 area 0.0.0.0",
		"network 10.77.1.0/24 area 0.0.0.1",
		"max-lsa 12000",
		"maximum-paths 8",
		"exit",
	}

	// Pass 1: apply.
	apply := func(name string) {
		t.Helper()
		sess, openErr := cli.OpenSession(ctx, "pulumi-it-ospf-"+name)
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

	// Pass 2: re-apply same body. Per `configure session` idempotency
	// semantics, the second commit must be a no-op (no error).
	apply("apply2")

	// Read-back: every line we requested (or its semantic equivalent
	// — cEOS canonicalises `area 0` to `area 0.0.0.0` and reformats a
	// few timer lines) must appear in the section output.
	resp, err := cli.RunCmds(ctx,
		[]string{"show running-config section ospf"}, "text")
	if err != nil {
		t.Fatalf("show running-config: %v", err)
	}
	out, _ := resp[0]["output"].(string)

	wantPresent := []string{
		"router ospf " + instance,
		"router-id 7.7.7.7",
		"passive-interface default",
		"redistribute connected",
		"area 0.0.0.1 stub",
		"network 10.77.0.0/24 area 0.0.0.0",
		"network 10.77.1.0/24 area 0.0.0.1",
		"max-lsa 12000",
		"maximum-paths 8",
	}
	for _, line := range wantPresent {
		if !strings.Contains(out, line) {
			t.Errorf("missing in running-config: %q\n--- got ---\n%s", line, out)
		}
	}

	// Cleanup: drop the process.
	delSess, delErr := cli.OpenSession(ctx, "pulumi-it-ospf-del")
	if delErr != nil {
		t.Fatalf("OpenSession del: %v", delErr)
	}
	if stageErr := delSess.Stage(ctx, []string{"no router ospf " + instance}); stageErr != nil {
		t.Fatalf("Stage del: %v", stageErr)
	}
	if commitErr := delSess.Commit(ctx); commitErr != nil {
		t.Fatalf("Commit del: %v", commitErr)
	}

	// Post-cleanup: process must be gone.
	resp, err = cli.RunCmds(ctx,
		[]string{"show running-config section ospf"}, "text")
	if err != nil {
		t.Fatalf("post-clean show: %v", err)
	}
	out, _ = resp[0]["output"].(string)
	if strings.Contains(out, "router ospf "+instance+"\n") {
		t.Errorf("router ospf %s still present after delete:\n%s", instance, out)
	}

	// Sanity: client still healthy.
	if _, sanityErr := cli.RunCmds(ctx, []string{"show version"}, "json"); sanityErr != nil {
		t.Errorf("post-test show version: %v", sanityErr)
	}
}
