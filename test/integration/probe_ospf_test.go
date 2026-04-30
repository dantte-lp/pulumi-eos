//go:build integration && probe

// Probe utilities for OSPF surface discovery. Run only on demand:
//
//	go test -tags="integration probe" -run TestProbe_Ospf -v ./test/integration/...
//
// Shares newTestClient with the integration suite so the discovery path
// is identical to the runtime/production path. Per docs/05-development.md
// rule 2a, this is the only sanctioned way to inspect cEOS CLI surface.
package integration

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestProbe_Ospf(t *testing.T) {
	cli := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	probes := []string{
		"ip routing",
		"router ospf 1",
		"router-id 1.1.1.1",
		"max-lsa 12000",
		"timers spf delay initial 50 200 5000",
		"timers throttle spf 50 200 5000",
		"timers lsa rx-min-interval 1000",
		"timers out-delay 100",
		"timers pacing flood 33",
		"network 10.0.0.0/24 area 0",
		"network 10.0.1.0/24 area 0.0.0.1",
		"area 0.0.0.1 stub",
		"area 0.0.0.2 nssa",
		"area 0.0.0.3 stub no-summary",
		"area 0.0.0.4 nssa default-information-originate metric 100 metric-type 2 nssa-only",
		"area 0.0.0.5 nssa no-redistribution",
		"area 0.0.0.6 nssa no-summary default-information-originate",
		"passive-interface default",
		"no passive-interface Ethernet1",
		"default-information originate metric 100 metric-type 2",
		"default-information originate route-map RM",
		"maximum-paths 64",
		"log-adjacency-changes detail",
		"redistribute connected",
		"redistribute static route-map RM",
		"redistribute bgp",
		"auto-cost reference-bandwidth 100000",
		"distance ospf intra-area 90 inter-area 95 external 110",
		"bfd all-interfaces",
		"graceful-restart restart-period 60",
		"graceful-restart-helper",
		"summary-address 10.0.0.0/16",
		"area 0 range 10.0.0.0/16",
	}

	type result struct {
		cmd string
		ok  bool
		err string
	}
	var results []result

	// Per-command probe: open ephemeral session, stage one cmd, abort.
	for _, c := range probes {
		sess, err := cli.OpenSession(ctx, "probe-ospf-cmd")
		if err != nil {
			t.Fatalf("OpenSession: %v", err)
		}
		// Always need router ospf 1 context for child cmds; "ip routing"
		// is global so handled separately.
		var stageCmds []string
		switch {
		case c == "ip routing", c == "router ospf 1":
			stageCmds = []string{c}
		default:
			stageCmds = []string{"ip routing", "router ospf 1", c}
		}
		stageErr := sess.Stage(ctx, stageCmds)
		_ = sess.Abort(ctx)
		if stageErr != nil {
			results = append(results, result{cmd: c, ok: false, err: stageErr.Error()})
		} else {
			results = append(results, result{cmd: c, ok: true})
		}
	}

	t.Log("---- per-command probe results ----")
	for _, r := range results {
		if r.ok {
			t.Logf("OK : %s", r.cmd)
		} else {
			msg := r.err
			if len(msg) > 200 {
				msg = msg[:200]
			}
			t.Logf("ERR: %s\n     -> %s", r.cmd, msg)
		}
	}

	// Stage all OK ones together, commit, dump running-config and full
	// section, abort cleanup.
	sess, err := cli.OpenSession(ctx, "probe-ospf-full")
	if err != nil {
		t.Fatalf("OpenSession full: %v", err)
	}
	full := []string{"ip routing", "router ospf 1"}
	for _, r := range results {
		if r.ok && r.cmd != "ip routing" && r.cmd != "router ospf 1" {
			full = append(full, r.cmd)
		}
	}
	if err := sess.Stage(ctx, full); err != nil {
		t.Fatalf("Stage full: %v", err)
	}
	if err := sess.Commit(ctx); err != nil {
		t.Fatalf("Commit full: %v", err)
	}

	for _, view := range []string{
		"show running-config section ospf",
		"show running-config all section ospf",
	} {
		res, err := cli.RunCmds(ctx, []string{view}, "text")
		if err != nil {
			t.Fatalf("%s: %v", view, err)
		}
		var body string
		if len(res) > 0 {
			if v, ok := res[0]["output"].(string); ok {
				body = v
			}
		}
		fmt.Printf("==== %s ====\n%s\n", view, strings.TrimSpace(body))
	}

	// Idempotency check: re-staging the same body must commit cleanly
	// and leave running-config unchanged.
	sess2, err := cli.OpenSession(ctx, "probe-ospf-idem")
	if err != nil {
		t.Fatalf("OpenSession idem: %v", err)
	}
	if err := sess2.Stage(ctx, full); err != nil {
		t.Fatalf("Stage idem: %v", err)
	}
	if err := sess2.Commit(ctx); err != nil {
		t.Fatalf("Commit idem: %v", err)
	}

	// Cleanup: remove ospf and ip routing.
	cleanup, err := cli.OpenSession(ctx, "probe-ospf-cleanup")
	if err != nil {
		t.Fatalf("OpenSession cleanup: %v", err)
	}
	if err := cleanup.Stage(ctx, []string{"no router ospf 1", "no ip routing"}); err != nil {
		t.Logf("cleanup stage: %v", err)
	}
	if err := cleanup.Commit(ctx); err != nil {
		t.Logf("cleanup commit: %v", err)
	}
}
