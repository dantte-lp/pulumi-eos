//go:build integration

package integration

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/dantte-lp/pulumi-eos/internal/client/eapi"
)

// TestEAPI_VlanLifecycle drives a Vlan resource lifecycle end-to-end against
// cEOS at the eAPI session layer (i.e. the same primitives used by
// internal/resources/l2.Vlan). It exercises:
//
//  1. Create — open session, "vlan 999", "name pulumi-it", commit.
//  2. Read   — `show vlan 999` returns name pulumi-it.
//  3. Update — repeat with new name; idempotent re-apply works.
//  4. Delete — "no vlan 999" inside a session.
//  5. Read after delete — VLAN gone.
//
// Because TestMain isn't wired here, the test is self-cleaning: it deletes
// vlan 999 unconditionally on completion.
func TestEAPI_VlanLifecycle(t *testing.T) {
	cli := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	const id = 999
	idStr := strconv.Itoa(id)

	// Best-effort cleanup at the end.
	t.Cleanup(func() {
		ctxC, cancelC := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancelC()
		sess, err := cli.OpenSession(ctxC, "pulumi-it-vlan-cleanup")
		if err != nil {
			return
		}
		_ = sess.Stage(ctxC, []string{"no vlan " + idStr})
		_ = sess.Commit(ctxC)
	})

	// 1. Create
	mustApplyVlan(ctx, t, cli, "pulumi-it-vlan-create", []string{"vlan " + idStr, "name pulumi-it"})

	// 2. Read
	if name := vlanName(ctx, t, cli, id); name != "pulumi-it" {
		t.Fatalf("expected vlan %d name=pulumi-it, got %q", id, name)
	}

	// 3. Update (rename) and idempotent re-apply
	mustApplyVlan(ctx, t, cli, "pulumi-it-vlan-update", []string{"vlan " + idStr, "name pulumi-it-renamed"})
	mustApplyVlan(ctx, t, cli, "pulumi-it-vlan-reapply", []string{"vlan " + idStr, "name pulumi-it-renamed"})
	if name := vlanName(ctx, t, cli, id); name != "pulumi-it-renamed" {
		t.Fatalf("expected vlan %d name=pulumi-it-renamed, got %q", id, name)
	}

	// 4. Delete
	mustApplyVlan(ctx, t, cli, "pulumi-it-vlan-delete", []string{"no vlan " + idStr})

	// 5. Read after delete — vlan absent.
	if name := vlanName(ctx, t, cli, id); name != "" {
		t.Fatalf("expected vlan %d to be absent after delete, got name=%q", id, name)
	}
}

func mustApplyVlan(ctx context.Context, t *testing.T, cli *eapi.Client, sessName string, cmds []string) {
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

// vlanName returns the configured name of vlan id, or "" when the VLAN is
// absent. Uses the unfiltered `show vlan` and looks up the id in the JSON.
func vlanName(ctx context.Context, t *testing.T, cli *eapi.Client, id int) string {
	t.Helper()
	resp, err := cli.RunCmds(ctx, []string{"show vlan"}, "json")
	if err != nil {
		t.Fatalf("show vlan: %v", err)
	}
	if len(resp) == 0 {
		return ""
	}
	vlans, ok := resp[0]["vlans"].(map[string]any)
	if !ok {
		return ""
	}
	entry, ok := vlans[strconv.Itoa(id)].(map[string]any)
	if !ok {
		return ""
	}
	if name, ok := entry["name"].(string); ok {
		return name
	}
	return ""
}
