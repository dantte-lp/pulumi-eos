//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dantte-lp/pulumi-eos/internal/client/eapi"
)

// TestEAPI_RouteMapLifecycle drives a route-map lifecycle against
// cEOS at the eAPI session layer (the same primitives used by
// internal/resources/l3.RouteMap). Builds a 3-sequence policy
// (permit + deny + continue) referencing a prerequisite prefix-list,
// then verifies read-back, idempotent re-apply, and delete.
func TestEAPI_RouteMapLifecycle(t *testing.T) {
	cli := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	const pfxName = "PULUMI_IT_PFX_RM"
	const rmName = "PULUMI_IT_RM"

	t.Cleanup(func() {
		ctxC, cancelC := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancelC()
		sess, err := cli.OpenSession(ctxC, "pulumi-it-rmap-cleanup")
		if err != nil {
			return
		}
		_ = sess.Stage(ctxC, []string{
			"no route-map " + rmName,
			"no ip prefix-list " + pfxName,
		})
		_ = sess.Commit(ctxC)
	})

	createCmds := []string{
		"no ip prefix-list " + pfxName,
		"ip prefix-list " + pfxName + " seq 10 permit 10.0.0.0/8",
		"no route-map " + rmName,
		"route-map " + rmName + " permit 10",
		"match ip address prefix-list " + pfxName,
		"match metric 100",
		"set local-preference 300",
		"set community 65000:100 additive",
		"exit",
		"route-map " + rmName + " deny 20",
		"match tag 99",
		"exit",
		"route-map " + rmName + " permit 30",
		"continue 50",
		"exit",
	}

	// 1. Create
	mustApplyRouteMap(ctx, t, cli, "pulumi-it-rmap-create", createCmds)

	// 2. Read
	section := readRouteMapSection(ctx, t, cli, rmName)
	if !strings.Contains(section, "route-map "+rmName+" permit 10") {
		t.Fatalf("missing seq 10 header:\n%s", section)
	}
	if !strings.Contains(section, "match ip address prefix-list "+pfxName) {
		t.Fatalf("missing match prefix-list:\n%s", section)
	}
	if !strings.Contains(section, "set community 65000:100 additive") {
		t.Fatalf("missing set community additive:\n%s", section)
	}
	if !strings.Contains(section, "route-map "+rmName+" deny 20") {
		t.Fatalf("missing seq 20 header:\n%s", section)
	}
	if !strings.Contains(section, "continue 50") {
		t.Fatalf("missing continue 50:\n%s", section)
	}

	// 3. Idempotent re-apply
	mustApplyRouteMap(ctx, t, cli, "pulumi-it-rmap-reapply", createCmds)

	// 4. Update — change set metric on seq 10
	updateCmds := []string{
		"no route-map " + rmName,
		"route-map " + rmName + " permit 10",
		"match ip address prefix-list " + pfxName,
		"set metric +50",
		"exit",
	}
	mustApplyRouteMap(ctx, t, cli, "pulumi-it-rmap-update", updateCmds)
	section = readRouteMapSection(ctx, t, cli, rmName)
	if !strings.Contains(section, "set metric +50") {
		t.Fatalf("update didn't take effect:\n%s", section)
	}
	if strings.Contains(section, "set local-preference 300") {
		t.Fatalf("stale set local-preference leaked across re-emit:\n%s", section)
	}

	// 5. Delete
	mustApplyRouteMap(ctx, t, cli, "pulumi-it-rmap-delete", []string{"no route-map " + rmName})
	if section := readRouteMapSection(ctx, t, cli, rmName); strings.Contains(section, "route-map "+rmName+" ") {
		t.Fatalf("route-map persisted after delete:\n%s", section)
	}
}

func mustApplyRouteMap(ctx context.Context, t *testing.T, cli *eapi.Client, sessName string, cmds []string) {
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

func readRouteMapSection(ctx context.Context, t *testing.T, cli *eapi.Client, name string) string {
	t.Helper()
	resp, err := cli.RunCmds(ctx, []string{"show running-config | section route-map " + name}, "text")
	if err != nil {
		t.Fatalf("section route-map %s: %v", name, err)
	}
	if len(resp) == 0 {
		return ""
	}
	out, _ := resp[0]["output"].(string)
	return out
}
