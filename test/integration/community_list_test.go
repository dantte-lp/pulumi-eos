//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dantte-lp/pulumi-eos/internal/client/eapi"
)

// TestEAPI_CommunityListLifecycle drives an `ip community-list`
// lifecycle against cEOS at the eAPI session layer. Covers both the
// standard and regexp forms, since cEOS 4.36 uses `regexp` rather
// than the `expanded` keyword shown in the EOS User Manual.
func TestEAPI_CommunityListLifecycle(t *testing.T) {
	cli := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	const stdName = "PULUMI_IT_COMM_STD"
	const expName = "PULUMI_IT_COMM_EXP"

	t.Cleanup(func() {
		ctxC, cancelC := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancelC()
		sess, err := cli.OpenSession(ctxC, "pulumi-it-comm-cleanup")
		if err != nil {
			return
		}
		_ = sess.Stage(ctxC, []string{
			"no ip community-list " + stdName,
			"no ip community-list " + expName,
		})
		_ = sess.Commit(ctxC)
	})

	// 1. Create — both forms in one session.
	mustApplyComm(ctx, t, cli, "pulumi-it-comm-create", []string{
		"no ip community-list " + stdName,
		"no ip community-list " + expName,
		"ip community-list " + stdName + " permit 65000:100",
		"ip community-list " + stdName + " permit no-export",
		"ip community-list " + stdName + " deny 65001:200",
		"ip community-list regexp " + expName + " permit ^65000:.*$",
		"ip community-list regexp " + expName + " deny .*:666$",
	})

	// 2. Read
	stdLines := readCommunityListLines(ctx, t, cli, stdName, false)
	if len(stdLines) != 3 {
		t.Fatalf("expected 3 standard lines, got %d:\n%s", len(stdLines), strings.Join(stdLines, "\n"))
	}
	if !strings.Contains(stdLines[0], "permit 65000:100") {
		t.Fatalf("first standard entry: %q", stdLines[0])
	}
	if !strings.Contains(stdLines[2], "deny 65001:200") {
		t.Fatalf("third standard entry: %q", stdLines[2])
	}

	expLines := readCommunityListLines(ctx, t, cli, expName, true)
	if len(expLines) != 2 {
		t.Fatalf("expected 2 regexp lines, got %d:\n%s", len(expLines), strings.Join(expLines, "\n"))
	}
	if !strings.Contains(expLines[0], "^65000:.*$") {
		t.Fatalf("first regexp entry: %q", expLines[0])
	}

	// 3. Idempotent re-apply for both
	mustApplyComm(ctx, t, cli, "pulumi-it-comm-reapply", []string{
		"no ip community-list " + stdName,
		"ip community-list " + stdName + " permit 65000:100",
		"ip community-list " + stdName + " permit no-export",
		"ip community-list " + stdName + " deny 65001:200",
	})

	// 4. Delete both
	mustApplyComm(ctx, t, cli, "pulumi-it-comm-delete", []string{
		"no ip community-list " + stdName,
		"no ip community-list " + expName,
	})
	if lines := readCommunityListLines(ctx, t, cli, stdName, false); len(lines) != 0 {
		t.Fatalf("standard list persisted after delete: %v", lines)
	}
	if lines := readCommunityListLines(ctx, t, cli, expName, true); len(lines) != 0 {
		t.Fatalf("regexp list persisted after delete: %v", lines)
	}
}

func mustApplyComm(ctx context.Context, t *testing.T, cli *eapi.Client, sessName string, cmds []string) {
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

// readCommunityListLines pulls the running-config rows belonging to
// `name`. When isRegexp is true, only `ip community-list regexp <name>
// ...` lines are returned; otherwise standard ones (without the
// `regexp` keyword).
func readCommunityListLines(ctx context.Context, t *testing.T, cli *eapi.Client, name string, isRegexp bool) []string {
	t.Helper()
	resp, err := cli.RunCmds(ctx, []string{"show running-config | grep community-list"}, "text")
	if err != nil {
		t.Fatalf("running-config grep community-list: %v", err)
	}
	if len(resp) == 0 {
		return nil
	}
	out, _ := resp[0]["output"].(string)
	wantPrefix := "ip community-list " + name + " "
	if isRegexp {
		wantPrefix = "ip community-list regexp " + name + " "
	}
	var matches []string
	for raw := range strings.SplitSeq(out, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, wantPrefix) {
			matches = append(matches, line)
		}
	}
	return matches
}
