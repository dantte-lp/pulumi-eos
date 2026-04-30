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

// TestEAPI_ConfigletLifecycle drives the eos:device:Configlet command flow
// against cEOS at the eAPI session layer (the same primitives used by
// internal/resources/device.Configlet).
//
//  1. Apply — open session, stage a 2-line VLAN block, commit.
//  2. Read  — `show vlan <id>` returns the configured name.
//  3. Re-apply with a renamed body — idempotent on second commit.
//  4. Cleanup — `no vlan <id>` removes the lines.
func TestEAPI_ConfigletLifecycle(t *testing.T) {
	cli := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	const id = 998
	idStr := strconv.Itoa(id)

	t.Cleanup(func() {
		ctxC, cancelC := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancelC()
		sess, err := cli.OpenSession(ctxC, "pulumi-it-configlet-cleanup")
		if err != nil {
			return
		}
		_ = sess.Stage(ctxC, []string{"no vlan " + idStr})
		_ = sess.Commit(ctxC)
	})

	body := strings.Join([]string{
		"vlan " + idStr,
		"   name pulumi-it-configlet",
	}, "\n")

	mustApplyConfiglet(ctx, t, cli, "pulumi-it-configlet-create", body)

	if name := vlanName(ctx, t, cli, id); name != "pulumi-it-configlet" {
		t.Fatalf("expected vlan %d name=pulumi-it-configlet, got %q", id, name)
	}

	body2 := strings.Join([]string{
		"vlan " + idStr,
		"   name pulumi-it-configlet-renamed",
	}, "\n")
	mustApplyConfiglet(ctx, t, cli, "pulumi-it-configlet-update", body2)
	mustApplyConfiglet(ctx, t, cli, "pulumi-it-configlet-reapply", body2)
	if name := vlanName(ctx, t, cli, id); name != "pulumi-it-configlet-renamed" {
		t.Fatalf("expected vlan %d name=pulumi-it-configlet-renamed, got %q", id, name)
	}
}

// mustApplyConfiglet stages a configlet body inside a fresh config session
// using the same canonicalisation rules as
// internal/resources/device.canonicalConfigletLines.
func mustApplyConfiglet(ctx context.Context, t *testing.T, cli *eapi.Client, sessName, body string) {
	t.Helper()
	cmds := canonicalConfigletLinesIT(body)
	if len(cmds) == 0 {
		t.Fatalf("empty configlet body")
	}
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

// canonicalConfigletLinesIT mirrors device.canonicalConfigletLines so the
// integration test exercises the same canonicalisation contract without
// importing the resource package across module boundaries.
func canonicalConfigletLinesIT(body string) []string {
	out := make([]string, 0)
	for raw := range strings.SplitSeq(body, "\n") {
		line := strings.TrimRight(raw, " \t\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}
