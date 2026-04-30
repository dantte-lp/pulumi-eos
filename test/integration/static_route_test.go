//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dantte-lp/pulumi-eos/internal/client/eapi"
)

// TestEAPI_StaticRouteLifecycle drives an `ip route` lifecycle against
// cEOS at the eAPI session layer (the same primitives used by
// internal/resources/l3.StaticRoute).
//
//  1. Create — `ip route 10.99.0.0/24 192.0.2.1 200 tag 42 name pulumi-it metric 99`.
//  2. Read   — running-config reflects the full knob set.
//  3. Update — change tag; idempotent re-apply.
//  4. Add a Null0 floating route at distance 230 to verify the
//     composite-identity contract (same prefix, different next-hop +
//     distance, both coexist).
//  5. Delete — `no ip route 10.99.0.0/24 192.0.2.1 200` removes the
//     primary; the floating route survives until its own delete.
func TestEAPI_StaticRouteLifecycle(t *testing.T) {
	cli := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Wipe any leftover routes for the test prefix from prior runs or
	// manual debugging. `no ip route <prefix>` (no next-hop) removes
	// every entry for the prefix.
	mustApplyRoute(ctx, t, cli, "pulumi-it-route-prologue", []string{
		"no ip route 10.99.0.0/24",
	})

	t.Cleanup(func() {
		ctxC, cancelC := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancelC()
		sess, err := cli.OpenSession(ctxC, "pulumi-it-route-cleanup")
		if err != nil {
			return
		}
		_ = sess.Stage(ctxC, []string{"no ip route 10.99.0.0/24"})
		_ = sess.Commit(ctxC)
	})

	// 1. Create primary
	mustApplyRoute(ctx, t, cli, "pulumi-it-route-create", []string{
		"ip route 10.99.0.0/24 192.0.2.1 200 tag 42 name pulumi-it metric 99",
	})

	// 2. Read
	if line := findRoute(ctx, t, cli, "10.99.0.0/24", "192.0.2.1"); line == "" {
		t.Fatal("primary route missing after create")
	} else if !strings.Contains(line, "tag 42") || !strings.Contains(line, "name pulumi-it") {
		t.Fatalf("running-config line missing expected fields: %q", line)
	}

	// 3. Update tag + idempotent re-apply
	mustApplyRoute(ctx, t, cli, "pulumi-it-route-update", []string{
		"ip route 10.99.0.0/24 192.0.2.1 200 tag 1234 name pulumi-it metric 99",
	})
	mustApplyRoute(ctx, t, cli, "pulumi-it-route-reapply", []string{
		"ip route 10.99.0.0/24 192.0.2.1 200 tag 1234 name pulumi-it metric 99",
	})
	if line := findRoute(ctx, t, cli, "10.99.0.0/24", "192.0.2.1"); !strings.Contains(line, "tag 1234") {
		t.Fatalf("expected updated tag 1234, got %q", line)
	}

	// 4. Floating route on the same prefix (different next-hop + distance)
	mustApplyRoute(ctx, t, cli, "pulumi-it-route-floating", []string{
		"ip route 10.99.0.0/24 Null0 230",
	})
	if line := findRoute(ctx, t, cli, "10.99.0.0/24", "Null0"); line == "" {
		t.Fatal("floating Null0 route missing after create")
	}
	if line := findRoute(ctx, t, cli, "10.99.0.0/24", "192.0.2.1"); line == "" {
		t.Fatal("primary route disappeared when floating route was added — composite identity contract broken")
	}

	// 5. Delete primary; floating route survives
	mustApplyRoute(ctx, t, cli, "pulumi-it-route-delete", []string{
		"no ip route 10.99.0.0/24 192.0.2.1 200",
	})
	if line := findRoute(ctx, t, cli, "10.99.0.0/24", "192.0.2.1"); line != "" {
		t.Fatalf("primary not removed: %q", line)
	}
	if line := findRoute(ctx, t, cli, "10.99.0.0/24", "Null0"); line == "" {
		t.Fatal("floating route should survive primary deletion")
	}
}

func mustApplyRoute(ctx context.Context, t *testing.T, cli *eapi.Client, sessName string, cmds []string) {
	t.Helper()
	sess, err := cli.OpenSession(ctx, sessName)
	if err != nil {
		t.Fatalf("OpenSession(%s): %v", sessName, err)
	}
	if stageErr := sess.Stage(ctx, cmds); stageErr != nil {
		_ = sess.Abort(ctx)
		t.Fatalf("Stage(%s): %v", sessName, stageErr)
	}
	if commitErr := sess.Commit(ctx); commitErr != nil {
		t.Fatalf("Commit(%s): %v", sessName, commitErr)
	}
}

// findRoute returns the first `ip route ... <prefix> <nh> ...` line
// from running-config, or "" when no such route exists. EOS pipe-grep
// only accepts a single-word substring (no `^` anchor, no spaces in
// the pattern), so we grep on `ip` and filter for `ip route ` lines
// client-side.
//
//nolint:unparam // prefix is reusable; current test only exercises one.
func findRoute(ctx context.Context, t *testing.T, cli *eapi.Client, prefix, nh string) string {
	t.Helper()
	resp, err := cli.RunCmds(ctx, []string{"show running-config | grep ip"}, "text")
	if err != nil {
		t.Fatalf("running-config grep ip: %v", err)
	}
	if len(resp) == 0 {
		return ""
	}
	out, _ := resp[0]["output"].(string)
	for raw := range strings.SplitSeq(out, "\n") {
		line := strings.TrimSpace(raw)
		if !strings.HasPrefix(line, "ip route ") {
			continue
		}
		tokens := strings.Fields(line)
		if len(tokens) < 4 {
			continue
		}
		idx := 2
		if tokens[idx] == "vrf" {
			idx += 2
		}
		if idx+1 >= len(tokens) {
			continue
		}
		if tokens[idx] == prefix && tokens[idx+1] == nh {
			return line
		}
	}
	return ""
}
