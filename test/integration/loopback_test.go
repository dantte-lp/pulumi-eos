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

// TestEAPI_LoopbackLifecycle drives a Loopback resource lifecycle end-to-end
// against cEOS at the eAPI session layer (the same primitives used by
// internal/resources/l3.Loopback):
//
//  1. Create — open session, "interface Loopback42", "ip address 10.42.0.1/32",
//     "no shutdown", commit.
//  2. Read   — `show running-config interfaces Loopback42` returns the address.
//  3. Update — repeat with a renamed description; idempotent re-apply works.
//  4. Delete — "no interface Loopback42" inside a session.
//  5. Read after delete — Loopback gone.
func TestEAPI_LoopbackLifecycle(t *testing.T) {
	cli := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	const id = 42
	idStr := strconv.Itoa(id)
	name := "Loopback" + idStr

	t.Cleanup(func() {
		ctxC, cancelC := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancelC()
		sess, err := cli.OpenSession(ctxC, "pulumi-it-loopback-cleanup")
		if err != nil {
			return
		}
		_ = sess.Stage(ctxC, []string{"no interface " + name})
		_ = sess.Commit(ctxC)
	})

	// 1. Create
	mustApplyLoopback(ctx, t, cli, "pulumi-it-loopback-create", []string{
		"interface " + name,
		"description pulumi-it",
		"ip address 10.42.0.1/32",
		"no shutdown",
	})

	// 2. Read
	if got := readLoopbackDescription(ctx, t, cli, name); got != "pulumi-it" {
		t.Fatalf("expected description=pulumi-it, got %q", got)
	}
	if got := readLoopbackIP(ctx, t, cli, name); got != "10.42.0.1/32" {
		t.Fatalf("expected ip=10.42.0.1/32, got %q", got)
	}

	// 3. Update + idempotent re-apply
	mustApplyLoopback(ctx, t, cli, "pulumi-it-loopback-update", []string{
		"interface " + name,
		"description pulumi-it-renamed",
		"ip address 10.42.0.1/32",
	})
	mustApplyLoopback(ctx, t, cli, "pulumi-it-loopback-reapply", []string{
		"interface " + name,
		"description pulumi-it-renamed",
		"ip address 10.42.0.1/32",
	})
	if got := readLoopbackDescription(ctx, t, cli, name); got != "pulumi-it-renamed" {
		t.Fatalf("expected description=pulumi-it-renamed, got %q", got)
	}

	// 4. Delete
	mustApplyLoopback(ctx, t, cli, "pulumi-it-loopback-delete", []string{"no interface " + name})

	// 5. Read after delete — interface absent.
	if got := readLoopbackIP(ctx, t, cli, name); got != "" {
		t.Fatalf("expected loopback to be absent after delete, got ip=%q", got)
	}
}

func mustApplyLoopback(ctx context.Context, t *testing.T, cli *eapi.Client, sessName string, cmds []string) {
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

// readLoopbackDescription pulls the configured description from
// `show running-config interfaces <name>`. Returns "" when absent.
func readLoopbackDescription(ctx context.Context, t *testing.T, cli *eapi.Client, name string) string {
	t.Helper()
	resp, err := cli.RunCmds(ctx, []string{"show running-config interfaces " + name}, "text")
	if err != nil {
		t.Fatalf("show running-config: %v", err)
	}
	if len(resp) == 0 {
		return ""
	}
	out, _ := resp[0]["output"].(string)
	for raw := range strings.SplitSeq(out, "\n") {
		line := strings.TrimSpace(raw)
		if v, ok := strings.CutPrefix(line, "description "); ok {
			return v
		}
	}
	return ""
}

// readLoopbackIP pulls the configured `ip address` from running-config.
func readLoopbackIP(ctx context.Context, t *testing.T, cli *eapi.Client, name string) string {
	t.Helper()
	resp, err := cli.RunCmds(ctx, []string{"show running-config interfaces " + name}, "text")
	if err != nil {
		t.Fatalf("show running-config: %v", err)
	}
	if len(resp) == 0 {
		return ""
	}
	out, _ := resp[0]["output"].(string)
	for raw := range strings.SplitSeq(out, "\n") {
		line := strings.TrimSpace(raw)
		if v, ok := strings.CutPrefix(line, "ip address "); ok {
			return v
		}
	}
	return ""
}
