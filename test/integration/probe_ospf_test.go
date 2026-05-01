//go:build integration && probe

// OSPF probe — uses the shared `ProbeOnePerCmd` / `ProbeFullBody`
// helpers (probe_helpers.go) which enforce rule 2b: every probe
// terminates with `commit`, not `abort`. EOS only triggers full
// hardware-platform validation on commit, so abort-only probes can
// silently mark unsupported commands as OK.
//
// Run on demand:
//
//	go test -tags="integration probe" -run TestProbe_Ospf -v ./test/integration/...

package integration

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestProbe_Ospf(t *testing.T) {
	cli := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()

	cleanup := []string{"no router ospf 1", "no ip routing"}

	probes := []string{
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

	results := ProbeOnePerCmd(t, cli, ctx, "probe-ospf",
		[]string{"ip routing", "router ospf 1"},
		cleanup,
		probes,
	)

	t.Log("---- per-command probe results (commit-terminated) ----")
	for _, r := range results {
		if r.OK {
			t.Logf("OK : %s", r.Cmd)
		} else {
			msg := r.Err
			if len(msg) > 200 {
				msg = msg[:200]
			}
			t.Logf("ERR: %s\n     -> %s", r.Cmd, msg)
		}
	}

	// Compose the full OK-set into one commit and capture the
	// running-config canonical render. Idempotency is checked inside
	// the helper.
	full := []string{"ip routing", "router ospf 1"}
	for _, r := range results {
		if r.OK {
			full = append(full, r.Cmd)
		}
	}
	captured := ProbeFullBody(t, cli, ctx, "probe-ospf-full", full, cleanup,
		[]string{
			"show running-config section ospf",
			"show running-config all section ospf",
		})
	for view, body := range captured {
		fmt.Printf("==== %s ====\n%s\n", view, body)
	}
}
