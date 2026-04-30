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

// TestEAPI_VlanInterfaceLifecycle drives an SVI lifecycle through the same
// eAPI session primitives the l2.VlanInterface resource uses.
//
// Steps:
//
//  1. Create vlan 998 + interface Vlan998 with description and ip address.
//  2. Read — assert description and address match.
//  3. Update — change description.
//  4. Delete the SVI (vlan stays).
//  5. Confirm absence.
func TestEAPI_VlanInterfaceLifecycle(t *testing.T) {
	cli := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	const id = 998
	idStr := strconv.Itoa(id)

	t.Cleanup(func() {
		ctxC, cancelC := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancelC()
		sess, err := cli.OpenSession(ctxC, "pulumi-it-svi-cleanup")
		if err != nil {
			return
		}
		_ = sess.Stage(ctxC, []string{
			"no interface Vlan" + idStr,
			"no vlan " + idStr,
		})
		_ = sess.Commit(ctxC)
	})

	// 1. Create vlan + svi.
	mustApply(ctx, t, cli, "pulumi-it-svi-create", []string{
		"vlan " + idStr,
		"name pulumi-it-svi",
		"interface Vlan" + idStr,
		"description initial",
		"ip address 10.250.0.1/24",
	})

	// 2. Read — running-config has both lines.
	cfg := mustShowRunning(ctx, t, cli, id)
	if !strings.Contains(cfg, "description initial") {
		t.Fatalf("expected description=initial, got:\n%s", cfg)
	}
	if !strings.Contains(cfg, "ip address 10.250.0.1/24") {
		t.Fatalf("expected ip address 10.250.0.1/24, got:\n%s", cfg)
	}

	// 3. Update — change description.
	mustApply(ctx, t, cli, "pulumi-it-svi-update", []string{
		"interface Vlan" + idStr,
		"description renamed",
	})
	cfg = mustShowRunning(ctx, t, cli, id)
	if !strings.Contains(cfg, "description renamed") {
		t.Fatalf("expected description=renamed, got:\n%s", cfg)
	}

	// 4. Delete the SVI.
	mustApply(ctx, t, cli, "pulumi-it-svi-delete", []string{
		"no interface Vlan" + idStr,
	})

	// 5. Confirm absence.
	cfg = mustShowRunning(ctx, t, cli, id)
	if strings.Contains(cfg, "interface Vlan"+idStr) {
		t.Fatalf("expected SVI gone, got:\n%s", cfg)
	}
}

func mustApply(ctx context.Context, t *testing.T, cli *eapi.Client, sessName string, cmds []string) {
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

func mustShowRunning(ctx context.Context, t *testing.T, cli *eapi.Client, id int) string {
	t.Helper()
	resp, err := cli.RunCmds(ctx,
		[]string{"show running-config interfaces Vlan" + strconv.Itoa(id)},
		"text")
	if err != nil {
		t.Fatalf("show running-config: %v", err)
	}
	if len(resp) == 0 {
		return ""
	}
	if v, ok := resp[0]["output"].(string); ok {
		return v
	}
	return ""
}
