//go:build integration

package integration

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dantte-lp/pulumi-eos/internal/client/eapi"
)

// TestEAPI_BfdLifecycle drives the singleton `router bfd` configuration
// against cEOS at the eAPI session layer (the same primitives used by
// internal/resources/l3.Bfd):
//
//  1. Apply — `router bfd` with overlay timers (100/100/3) + slow-timer
//     2000 + no shutdown.
//  2. Read   — section view returns the configured triple.
//  3. Update — flip to underlay timers (300/300/3); idempotent re-apply.
//  4. Reset  — `no router bfd` clears the section.
//  5. Read after reset — timers gone.
func TestEAPI_BfdLifecycle(t *testing.T) {
	cli := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	t.Cleanup(func() {
		ctxC, cancelC := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancelC()
		sess, err := cli.OpenSession(ctxC, "pulumi-it-bfd-cleanup")
		if err != nil {
			return
		}
		_ = sess.Stage(ctxC, []string{"no router bfd"})
		_ = sess.Commit(ctxC)
	})

	// 1. Overlay timers
	mustApplyBfd(ctx, t, cli, "pulumi-it-bfd-overlay", []string{
		"router bfd",
		"interval 100 min-rx 100 multiplier 3 default",
		"slow-timer 2000",
		"no shutdown",
		"exit",
	})

	if iv, mr, mu := readBfdTimers(ctx, t, cli); iv != 100 || mr != 100 || mu != 3 {
		t.Fatalf("expected overlay 100/100/3, got %d/%d/%d", iv, mr, mu)
	}

	// 2. Underlay + idempotent re-apply
	mustApplyBfd(ctx, t, cli, "pulumi-it-bfd-underlay", []string{
		"router bfd",
		"interval 300 min-rx 300 multiplier 3 default",
		"exit",
	})
	mustApplyBfd(ctx, t, cli, "pulumi-it-bfd-reapply", []string{
		"router bfd",
		"interval 300 min-rx 300 multiplier 3 default",
		"exit",
	})
	if iv, mr, mu := readBfdTimers(ctx, t, cli); iv != 300 || mr != 300 || mu != 3 {
		t.Fatalf("expected underlay 300/300/3, got %d/%d/%d", iv, mr, mu)
	}

	// 3. Move to a non-default profile (200/200/3) so we can prove the
	//    reset removes it from running-config (default values are elided).
	mustApplyBfd(ctx, t, cli, "pulumi-it-bfd-200", []string{
		"router bfd",
		"interval 200 min-rx 200 multiplier 3 default",
		"exit",
	})
	if iv, mr, mu := readBfdTimers(ctx, t, cli); iv != 200 || mr != 200 || mu != 3 {
		t.Fatalf("expected 200/200/3, got %d/%d/%d", iv, mr, mu)
	}

	// 4. Reset
	mustApplyBfd(ctx, t, cli, "pulumi-it-bfd-reset", []string{"no router bfd"})

	// 5. Read after reset via the non-`all` view — the section becomes
	//    empty because the timers fell back to the factory defaults.
	if hasRouterBfdSection(ctx, t, cli) {
		t.Fatalf("expected empty `router bfd` section after `no router bfd`")
	}
}

// hasRouterBfdSection reports whether `show running-config | section
// router bfd` (without `all`) emits any non-empty body. EOS elides any
// sub-command set to the factory default, so a clean reset yields an
// empty section.
func hasRouterBfdSection(ctx context.Context, t *testing.T, cli *eapi.Client) bool {
	t.Helper()
	resp, err := cli.RunCmds(ctx, []string{"show running-config | section router bfd"}, "text")
	if err != nil {
		t.Fatalf("section router bfd: %v", err)
	}
	if len(resp) == 0 {
		return false
	}
	out, _ := resp[0]["output"].(string)
	return strings.TrimSpace(out) != ""
}

func mustApplyBfd(ctx context.Context, t *testing.T, cli *eapi.Client, sessName string, cmds []string) {
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

// readBfdTimers parses `interval N min-rx N multiplier N` from the
// `router bfd` running-config section. Uses `running-config all` so that
// values matching the EOS factory default (300/300/3) still appear —
// the default `running-config` output elides them.
func readBfdTimers(ctx context.Context, t *testing.T, cli *eapi.Client) (int, int, int) {
	t.Helper()
	resp, err := cli.RunCmds(ctx, []string{"show running-config all | section router bfd"}, "text")
	if err != nil {
		t.Fatalf("section router bfd: %v", err)
	}
	if len(resp) == 0 {
		return 0, 0, 0
	}
	out, _ := resp[0]["output"].(string)
	for raw := range strings.SplitSeq(out, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "interval ") {
			tokens := strings.Fields(line)
			var iv, mr, mu int
			for i := range len(tokens) - 1 {
				switch tokens[i] {
				case "interval":
					iv, _ = strconv.Atoi(tokens[i+1])
				case "min-rx":
					mr, _ = strconv.Atoi(tokens[i+1])
				case "multiplier":
					mu, _ = strconv.Atoi(tokens[i+1])
				}
			}
			return iv, mr, mu
		}
	}
	return 0, 0, 0
}
