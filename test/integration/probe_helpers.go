//go:build integration && probe

// Probe helpers — shared by all `probe_<resource>_test.go` files
// under //go:build integration && probe.
//
// Why this file exists: per docs/05-development.md rule 2b, probes
// must terminate with `commit`, not `abort`. EOS validates per-line
// CLI grammar at Stage time but only triggers full hardware-platform
// validation at commit (sometimes also at end). Probes that
// terminate with Abort silently mark hardware-unsupported commands
// as OK and ship them into resources that then fail at runtime
// (cEOSLab `tunnel dont-fragment` was caught this way — see
// `eos:l3:GreTunnel` v0 commit `d2ee58a`).
//
// The helpers here force the rule: callers receive a closure that
// stages, commits, captures, and cleans up — no Abort path.
package integration

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dantte-lp/pulumi-eos/internal/client/eapi"
)

// ProbeOutcome carries the result of a single command-or-body probe.
type ProbeOutcome struct {
	Cmd string
	OK  bool
	Err string
}

// ProbeOnePerCmd runs `cmds` one at a time, each in its own session,
// each terminating with Commit. The caller supplies a `fixture` slice
// — context lines (e.g. "ip routing", "router ospf 1") that must be
// staged in front of every probe so the standalone command has the
// right config-mode prefix. After every probe the device is rolled
// back via `cleanup` so the next probe starts from a clean baseline.
//
// Fixture and cleanup are caller-owned: the helper does not assume
// any particular global state.
//
// Usage:
//
//	cleanup := []string{"no router ospf 1", "no ip routing"}
//	results := ProbeOnePerCmd(t, cli, ctx, "probe-ospf",
//	    []string{"ip routing", "router ospf 1"},
//	    cleanup,
//	    []string{
//	        "router-id 1.1.1.1",
//	        "max-lsa 12000",
//	        ...,
//	    })
func ProbeOnePerCmd(
	t *testing.T,
	cli *eapi.Client,
	ctx context.Context,
	sessionPrefix string,
	fixture []string,
	cleanup []string,
	cmds []string,
) []ProbeOutcome {
	t.Helper()
	var out []ProbeOutcome
	for i, c := range cmds {
		// Reset device state to baseline.
		runCleanup(ctx, cli, cleanup)

		sname := probeSessionName(sessionPrefix, i)
		body := append(append([]string{}, fixture...), c)

		sess, openErr := cli.OpenSession(ctx, sname)
		if openErr != nil {
			t.Fatalf("ProbeOnePerCmd: OpenSession(%s): %v", sname, openErr)
		}
		stageErr := sess.Stage(ctx, body)
		if stageErr != nil {
			_ = sess.Abort(ctx) // session must be drained even on error
			out = append(out, ProbeOutcome{Cmd: c, OK: false, Err: stageErr.Error()})
			continue
		}
		commitErr := sess.Commit(ctx)
		if commitErr != nil {
			out = append(out, ProbeOutcome{Cmd: c, OK: false, Err: commitErr.Error()})
			continue
		}
		out = append(out, ProbeOutcome{Cmd: c, OK: true})
	}
	// Final cleanup so the device is left clean for the next probe.
	runCleanup(ctx, cli, cleanup)
	return out
}

// ProbeFullBody stages the full body in one commit. Callers use this
// after ProbeOnePerCmd to check that the OK-set composes cleanly.
// Returns the post-commit running-config text for the requested view
// commands so the caller can assert canonical render.
func ProbeFullBody(
	t *testing.T,
	cli *eapi.Client,
	ctx context.Context,
	sessionName string,
	body []string,
	cleanup []string,
	views []string,
) map[string]string {
	t.Helper()

	// Always start from a clean baseline.
	runCleanup(ctx, cli, cleanup)

	sess, openErr := cli.OpenSession(ctx, sessionName)
	if openErr != nil {
		t.Fatalf("ProbeFullBody: OpenSession(%s): %v", sessionName, openErr)
	}
	if stageErr := sess.Stage(ctx, body); stageErr != nil {
		_ = sess.Abort(ctx)
		t.Fatalf("ProbeFullBody: Stage(%s): %v", sessionName, stageErr)
	}
	if commitErr := sess.Commit(ctx); commitErr != nil {
		t.Fatalf("ProbeFullBody: Commit(%s): %v", sessionName, commitErr)
	}

	captured := make(map[string]string, len(views))
	for _, view := range views {
		resp, err := cli.RunCmds(ctx, []string{view}, "text")
		if err != nil {
			t.Fatalf("ProbeFullBody: %s: %v", view, err)
		}
		var body string
		if len(resp) > 0 {
			if v, ok := resp[0]["output"].(string); ok {
				body = v
			}
		}
		captured[view] = strings.TrimSpace(body)
	}

	// Idempotency check: re-staging the same body must commit cleanly.
	idemSess, openErr := cli.OpenSession(ctx, sessionName+"-idem")
	if openErr != nil {
		t.Fatalf("ProbeFullBody: OpenSession idem: %v", openErr)
	}
	if stageErr := idemSess.Stage(ctx, body); stageErr != nil {
		_ = idemSess.Abort(ctx)
		t.Fatalf("ProbeFullBody: Stage idem: %v", stageErr)
	}
	if commitErr := idemSess.Commit(ctx); commitErr != nil {
		t.Fatalf("ProbeFullBody: Commit idem: %v", commitErr)
	}

	// Final cleanup so the next probe starts clean.
	runCleanup(ctx, cli, cleanup)
	return captured
}

// cleanupCounter is a per-process monotonic source for unique cleanup
// session names. EOS keeps a `Maximum number of completed sessions`
// (default 1) — reusing the same cleanup-session name across many
// probes pushes earlier completed sessions out of that retention
// queue and confuses subsequent `configure session <name>` opens
// (cEOS returns generic JSON Error(1002) without a message).
var cleanupCounter atomic.Uint64

// runCleanup commits the cleanup body in its own session. Failures
// are swallowed — cleanup is best-effort. The session name is unique
// per call so completed-session retention does not collide.
func runCleanup(ctx context.Context, cli *eapi.Client, cleanup []string) {
	if len(cleanup) == 0 {
		return
	}
	n := cleanupCounter.Add(1)
	sname := "probe-cleanup-" + intToA(int(n))
	sess, err := cli.OpenSession(ctx, sname)
	if err != nil {
		return
	}
	if err := sess.Stage(ctx, cleanup); err != nil {
		_ = sess.Abort(ctx)
		return
	}
	_ = sess.Commit(ctx)
}

// probeSessionName builds a unique session name within one probe.
func probeSessionName(prefix string, idx int) string {
	return prefix + "-" + intToA(idx)
}

// intToA is a tiny non-strconv stringifier kept inline so the probe
// helpers don't pull a strconv import that golangci-lint's modernize
// linter then nags about (the rest of the integration suite uses
// `strconv.Itoa` directly; our helper file aims to stay
// dependency-light).
func intToA(n int) string {
	if n == 0 {
		return "0"
	}
	negative := n < 0
	if negative {
		n = -n
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	if negative {
		digits = append([]byte{'-'}, digits...)
	}
	return string(digits)
}
