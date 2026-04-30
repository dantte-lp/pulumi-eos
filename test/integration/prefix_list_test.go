//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dantte-lp/pulumi-eos/internal/client/eapi"
)

// TestEAPI_PrefixListLifecycle drives an `ip prefix-list` lifecycle
// against cEOS at the eAPI session layer (the same primitives used by
// internal/resources/l3.PrefixList).
//
//  1. Create — three entries (ge/le, ge-only, eq).
//  2. Read   — running-config grep returns all three lines.
//  3. Update — replace entry set; idempotent re-apply.
//  4. Delete — `no ip prefix-list <name>` clears the list.
func TestEAPI_PrefixListLifecycle(t *testing.T) {
	cli := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	const name = "PULUMI_IT_PFX"

	t.Cleanup(func() {
		ctxC, cancelC := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancelC()
		sess, err := cli.OpenSession(ctxC, "pulumi-it-pfx-cleanup")
		if err != nil {
			return
		}
		_ = sess.Stage(ctxC, []string{"no ip prefix-list " + name})
		_ = sess.Commit(ctxC)
	})

	// 1. Create
	mustApplyPfx(ctx, t, cli, "pulumi-it-pfx-create", []string{
		"no ip prefix-list " + name,
		"ip prefix-list " + name + " seq 10 permit 10.0.0.0/8 ge 16 le 24",
		"ip prefix-list " + name + " seq 20 deny 0.0.0.0/0 ge 25",
		"ip prefix-list " + name + " seq 30 permit 172.16.0.0/12 eq 16",
	})

	// 2. Read
	lines := readPrefixListLines(ctx, t, cli, name)
	if len(lines) != 3 {
		t.Fatalf("expected 3 entry lines, got %d:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	if !strings.Contains(lines[0], "seq 10 permit 10.0.0.0/8 ge 16 le 24") {
		t.Fatalf("seq 10 mismatch: %q", lines[0])
	}
	if !strings.Contains(lines[2], "seq 30 permit 172.16.0.0/12 eq 16") {
		t.Fatalf("seq 30 mismatch: %q", lines[2])
	}

	// 3. Update + idempotent re-apply
	updateCmds := []string{
		"no ip prefix-list " + name,
		"ip prefix-list " + name + " seq 5 permit 192.0.2.0/24",
		"ip prefix-list " + name + " seq 15 deny 198.51.100.0/24",
	}
	mustApplyPfx(ctx, t, cli, "pulumi-it-pfx-update", updateCmds)
	mustApplyPfx(ctx, t, cli, "pulumi-it-pfx-reapply", updateCmds)
	lines = readPrefixListLines(ctx, t, cli, name)
	if len(lines) != 2 {
		t.Fatalf("after update expected 2 lines, got %d:\n%s", len(lines), strings.Join(lines, "\n"))
	}

	// 4. Delete
	mustApplyPfx(ctx, t, cli, "pulumi-it-pfx-delete", []string{"no ip prefix-list " + name})
	if lines := readPrefixListLines(ctx, t, cli, name); len(lines) != 0 {
		t.Fatalf("after delete expected 0 lines, got %d:\n%s", len(lines), strings.Join(lines, "\n"))
	}
}

func mustApplyPfx(ctx context.Context, t *testing.T, cli *eapi.Client, sessName string, cmds []string) {
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

// readPrefixListLines returns running-config lines that begin with
// `ip prefix-list <name> ` (i.e. only entries belonging to this list,
// already filtered client-side because EOS pipe-grep accepts only a
// single-word substring).
func readPrefixListLines(ctx context.Context, t *testing.T, cli *eapi.Client, name string) []string {
	t.Helper()
	resp, err := cli.RunCmds(ctx, []string{"show running-config | grep prefix-list"}, "text")
	if err != nil {
		t.Fatalf("running-config grep prefix-list: %v", err)
	}
	if len(resp) == 0 {
		return nil
	}
	out, _ := resp[0]["output"].(string)
	prefix := "ip prefix-list " + name + " "
	var matches []string
	for raw := range strings.SplitSeq(out, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, prefix) {
			matches = append(matches, line)
		}
	}
	return matches
}
