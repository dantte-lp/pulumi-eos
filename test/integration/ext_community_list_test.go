//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dantte-lp/pulumi-eos/internal/client/eapi"
)

// TestEAPI_ExtCommunityListLifecycle drives an `ip extcommunity-list`
// lifecycle against cEOS. Covers both standard (rt + soo prefixes)
// and regexp forms.
func TestEAPI_ExtCommunityListLifecycle(t *testing.T) {
	cli := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	const stdName = "PULUMI_IT_EXT_STD"
	const expName = "PULUMI_IT_EXT_EXP"

	t.Cleanup(func() {
		ctxC, cancelC := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancelC()
		sess, err := cli.OpenSession(ctxC, "pulumi-it-extcomm-cleanup")
		if err != nil {
			return
		}
		_ = sess.Stage(ctxC, []string{
			"no ip extcommunity-list " + stdName,
			"no ip extcommunity-list " + expName,
		})
		_ = sess.Commit(ctxC)
	})

	// 1. Create
	mustApplyExtComm(ctx, t, cli, "pulumi-it-extcomm-create", []string{
		"no ip extcommunity-list " + stdName,
		"no ip extcommunity-list " + expName,
		"ip extcommunity-list " + stdName + " permit rt 65000:100",
		"ip extcommunity-list " + stdName + " permit soo 1.2.3.4:200",
		"ip extcommunity-list " + stdName + " deny rt 65001:200",
		"ip extcommunity-list regexp " + expName + " permit RT:65000:.*",
	})

	// 2. Read
	stdLines := readExtCommunityListLines(ctx, t, cli, stdName, false)
	if len(stdLines) != 3 {
		t.Fatalf("expected 3 standard lines, got %d:\n%s", len(stdLines), strings.Join(stdLines, "\n"))
	}
	if !strings.Contains(stdLines[0], "permit rt 65000:100") {
		t.Fatalf("first standard entry: %q", stdLines[0])
	}
	if !strings.Contains(stdLines[1], "permit soo 1.2.3.4:200") {
		t.Fatalf("soo entry mismatch: %q", stdLines[1])
	}

	expLines := readExtCommunityListLines(ctx, t, cli, expName, true)
	if len(expLines) != 1 {
		t.Fatalf("expected 1 regexp line, got %d:\n%s", len(expLines), strings.Join(expLines, "\n"))
	}
	if !strings.Contains(expLines[0], "RT:65000:.*") {
		t.Fatalf("regexp entry: %q", expLines[0])
	}

	// 3. Idempotent re-apply
	mustApplyExtComm(ctx, t, cli, "pulumi-it-extcomm-reapply", []string{
		"no ip extcommunity-list " + stdName,
		"ip extcommunity-list " + stdName + " permit rt 65000:100",
		"ip extcommunity-list " + stdName + " permit soo 1.2.3.4:200",
		"ip extcommunity-list " + stdName + " deny rt 65001:200",
	})

	// 4. Delete
	mustApplyExtComm(ctx, t, cli, "pulumi-it-extcomm-delete", []string{
		"no ip extcommunity-list " + stdName,
		"no ip extcommunity-list " + expName,
	})
	if lines := readExtCommunityListLines(ctx, t, cli, stdName, false); len(lines) != 0 {
		t.Fatalf("standard list persisted after delete: %v", lines)
	}
	if lines := readExtCommunityListLines(ctx, t, cli, expName, true); len(lines) != 0 {
		t.Fatalf("regexp list persisted after delete: %v", lines)
	}
}

func mustApplyExtComm(ctx context.Context, t *testing.T, cli *eapi.Client, sessName string, cmds []string) {
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

// readExtCommunityListLines pulls running-config rows for `name` from
// `show running-config | grep extcommunity` (single-word EOS
// pipe-grep), filtered client-side.
func readExtCommunityListLines(ctx context.Context, t *testing.T, cli *eapi.Client, name string, isRegexp bool) []string {
	t.Helper()
	resp, err := cli.RunCmds(ctx, []string{"show running-config | grep extcommunity"}, "text")
	if err != nil {
		t.Fatalf("running-config grep extcommunity: %v", err)
	}
	if len(resp) == 0 {
		return nil
	}
	out, _ := resp[0]["output"].(string)
	wantPrefix := "ip extcommunity-list " + name + " "
	if isRegexp {
		wantPrefix = "ip extcommunity-list regexp " + name + " "
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
