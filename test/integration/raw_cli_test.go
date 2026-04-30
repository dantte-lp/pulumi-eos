//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestEAPI_RawCliDiff_NoOpDetection drives the diff-driven idempotency
// contract that backs eos:device:RawCli:
//
//  1. Apply — open session, stage `vlan 997 / name pulumi-it-rawcli`,
//     diff is non-empty, commit succeeds.
//  2. Re-apply same body — diff is empty, session aborts without
//     touching running-config.
//  3. Apply renamed body — diff is non-empty again, commit succeeds.
//  4. Cleanup — `no vlan 997`.
func TestEAPI_RawCliDiff_NoOpDetection(t *testing.T) {
	cli := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	const id = 997
	const idStr = "997"

	t.Cleanup(func() {
		ctxC, cancelC := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancelC()
		sess, err := cli.OpenSession(ctxC, "pulumi-it-rawcli-cleanup")
		if err != nil {
			return
		}
		_ = sess.Stage(ctxC, []string{"no vlan " + idStr})
		_ = sess.Commit(ctxC)
	})

	apply := func(sessName string, body string) (string, bool) {
		t.Helper()
		cmds := canonicalConfigletLinesIT(body)
		sess, err := cli.OpenSession(ctx, sessName)
		if err != nil {
			t.Fatalf("OpenSession(%s): %v", sessName, err)
		}
		if stageErr := sess.Stage(ctx, cmds); stageErr != nil {
			_ = sess.Abort(ctx)
			t.Fatalf("Stage(%s): %v", sessName, stageErr)
		}
		diff, diffErr := sess.Diff(ctx)
		if diffErr != nil {
			_ = sess.Abort(ctx)
			t.Fatalf("Diff(%s): %v", sessName, diffErr)
		}
		empty := isEmptyDiffIT(diff)
		if empty {
			if abortErr := sess.Abort(ctx); abortErr != nil {
				t.Fatalf("Abort(%s): %v", sessName, abortErr)
			}
			return diff, true
		}
		if commitErr := sess.Commit(ctx); commitErr != nil {
			t.Fatalf("Commit(%s): %v", sessName, commitErr)
		}
		return diff, false
	}

	// 1. First apply — diff non-empty, commit.
	body1 := "vlan " + idStr + "\n   name pulumi-it-rawcli"
	if _, empty := apply("pulumi-it-rawcli-1", body1); empty {
		t.Fatal("first apply expected non-empty diff, got empty")
	}

	// 2. Re-apply same body — diff empty, abort.
	if diff, empty := apply("pulumi-it-rawcli-2", body1); !empty {
		t.Fatalf("re-apply expected empty diff, got %q", diff)
	}

	// 3. Apply renamed body — diff non-empty, commit.
	body2 := "vlan " + idStr + "\n   name pulumi-it-rawcli-renamed"
	if _, empty := apply("pulumi-it-rawcli-3", body2); empty {
		t.Fatal("rename apply expected non-empty diff, got empty")
	}

	// 4. Verify the rename landed.
	if name := vlanName(ctx, t, cli, id); name != "pulumi-it-rawcli-renamed" {
		t.Fatalf("expected vlan %d name=pulumi-it-rawcli-renamed, got %q", id, name)
	}
}

// isEmptyDiffIT mirrors device.isEmptyDiff so the integration test
// exercises the same idempotency contract without crossing test/non-test
// build-tag boundaries.
func isEmptyDiffIT(diff string) bool {
	for raw := range strings.SplitSeq(diff, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++") {
			continue
		}
		return false
	}
	return true
}
