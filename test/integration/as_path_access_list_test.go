//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dantte-lp/pulumi-eos/internal/client/eapi"
)

// TestEAPI_AsPathAccessListLifecycle drives an `ip as-path access-list`
// lifecycle against cEOS. EOS auto-appends ` any` to every running-
// config entry; the test asserts the entries land + the auto-suffix
// is stable + delete is clean.
func TestEAPI_AsPathAccessListLifecycle(t *testing.T) {
	cli := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	const name = "PULUMI_IT_ASPATH"

	t.Cleanup(func() {
		ctxC, cancelC := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancelC()
		sess, err := cli.OpenSession(ctxC, "pulumi-it-aspath-cleanup")
		if err != nil {
			return
		}
		_ = sess.Stage(ctxC, []string{"no ip as-path access-list " + name})
		_ = sess.Commit(ctxC)
	})

	createCmds := []string{
		"no ip as-path access-list " + name,
		"ip as-path access-list " + name + " permit ^65000$",
		"ip as-path access-list " + name + " permit _65001_",
		"ip as-path access-list " + name + " deny ^65002.*$",
	}

	// 1. Create
	mustApplyAsPath(ctx, t, cli, "pulumi-it-aspath-create", createCmds)

	// 2. Read — EOS auto-appends ` any` to each line.
	lines := readAsPathLines(ctx, t, cli, name)
	if len(lines) != 3 {
		t.Fatalf("expected 3 entries, got %d:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	for _, line := range lines {
		if !strings.HasSuffix(line, " any") {
			t.Fatalf("line missing auto-appended ` any`: %q", line)
		}
	}
	if !strings.Contains(lines[0], "permit ^65000$") {
		t.Fatalf("entry[0]: %q", lines[0])
	}
	if !strings.Contains(lines[2], "deny ^65002.*$") {
		t.Fatalf("entry[2]: %q", lines[2])
	}

	// 3. Idempotent re-apply
	mustApplyAsPath(ctx, t, cli, "pulumi-it-aspath-reapply", createCmds)

	// 4. Update — replace entry set
	updateCmds := []string{
		"no ip as-path access-list " + name,
		"ip as-path access-list " + name + " permit ^65010$",
		"ip as-path access-list " + name + " deny _666_",
	}
	mustApplyAsPath(ctx, t, cli, "pulumi-it-aspath-update", updateCmds)
	updated := readAsPathLines(ctx, t, cli, name)
	if len(updated) != 2 {
		t.Fatalf("after update expected 2 entries, got %d:\n%s", len(updated), strings.Join(updated, "\n"))
	}

	// 5. Delete
	mustApplyAsPath(ctx, t, cli, "pulumi-it-aspath-delete", []string{"no ip as-path access-list " + name})
	if lines := readAsPathLines(ctx, t, cli, name); len(lines) != 0 {
		t.Fatalf("after delete expected empty, got %d:\n%s", len(lines), strings.Join(lines, "\n"))
	}
}

func mustApplyAsPath(ctx context.Context, t *testing.T, cli *eapi.Client, sessName string, cmds []string) {
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

// readAsPathLines pulls the running-config rows belonging to `name`.
// Single-word EOS pipe-grep — match on `as-path` and filter
// client-side.
func readAsPathLines(ctx context.Context, t *testing.T, cli *eapi.Client, name string) []string {
	t.Helper()
	resp, err := cli.RunCmds(ctx, []string{"show running-config | grep as-path"}, "text")
	if err != nil {
		t.Fatalf("running-config grep as-path: %v", err)
	}
	if len(resp) == 0 {
		return nil
	}
	out, _ := resp[0]["output"].(string)
	prefix := "ip as-path access-list " + name + " "
	var matches []string
	for raw := range strings.SplitSeq(out, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, prefix) {
			matches = append(matches, line)
		}
	}
	return matches
}
