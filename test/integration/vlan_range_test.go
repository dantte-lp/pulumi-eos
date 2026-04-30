//go:build integration

package integration

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestEAPI_VlanRangeLifecycle drives the bulk VLAN-range allocation
// against cEOS via the same eAPI session primitives the l2.VlanRange
// resource uses.
func TestEAPI_VlanRangeLifecycle(t *testing.T) {
	cli := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	const (
		start = 700
		end   = 705
	)

	t.Cleanup(func() {
		ctxC, cancelC := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancelC()
		sess, err := cli.OpenSession(ctxC, "pulumi-it-vlanrange-cleanup")
		if err != nil {
			return
		}
		_ = sess.Stage(ctxC, []string{
			"no vlan " + strconv.Itoa(start) + "-" + strconv.Itoa(end),
		})
		_ = sess.Commit(ctxC)
	})

	// 1. Create with name.
	mustApply(ctx, t, cli, "pulumi-it-vlanrange-create", []string{
		"vlan " + strconv.Itoa(start) + "-" + strconv.Itoa(end),
		"name pulumi-it-svc",
	})

	cfg := mustShowVlanRange(ctx, t, cli)
	for vid := start; vid <= end; vid++ {
		if !strings.Contains(cfg, strconv.Itoa(vid)) {
			t.Fatalf("expected vlan %d in show vlan output, got:\n%s", vid, cfg)
		}
	}
	if !strings.Contains(cfg, "pulumi-it-svc") {
		t.Fatalf("expected name pulumi-it-svc, got:\n%s", cfg)
	}

	// 2. Update — rename.
	mustApply(ctx, t, cli, "pulumi-it-vlanrange-update", []string{
		"vlan " + strconv.Itoa(start) + "-" + strconv.Itoa(end),
		"name pulumi-it-svc-v2",
	})

	cfg = mustShowVlanRange(ctx, t, cli)
	if !strings.Contains(cfg, "pulumi-it-svc-v2") {
		t.Fatalf("expected updated name, got:\n%s", cfg)
	}

	// 3. Delete.
	mustApply(ctx, t, cli, "pulumi-it-vlanrange-delete", []string{
		"no vlan " + strconv.Itoa(start) + "-" + strconv.Itoa(end),
	})

	cfg = mustShowVlanRange(ctx, t, cli)
	for vid := start; vid <= end; vid++ {
		if strings.Contains(cfg, "  "+strconv.Itoa(vid)+"  ") {
			t.Fatalf("expected vlan %d gone after delete, got:\n%s", vid, cfg)
		}
	}
}

func mustShowVlanRange(ctx context.Context, t *testing.T, cli runCmds) string {
	t.Helper()
	resp, err := cli.RunCmds(ctx, []string{"show vlan"}, "text")
	if err != nil {
		t.Fatalf("show vlan: %v", err)
	}
	if len(resp) == 0 {
		return ""
	}
	if v, ok := resp[0]["output"].(string); ok {
		return v
	}
	return ""
}
