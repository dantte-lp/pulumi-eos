//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dantte-lp/pulumi-eos/internal/client/eapi"
)

// TestEAPI_SubinterfaceLifecycle drives an L3 sub-interface lifecycle
// against cEOS at the eAPI session layer (the same primitives used by
// internal/resources/l3.Subinterface):
//
//  1. Configure a routed parent (`Ethernet1`, no switchport, no shutdown).
//  2. Create — `interface Ethernet1.4011` with encapsulation + IP.
//  3. Read   — section view returns the configured fields.
//  4. Update — change description; idempotent re-apply.
//  5. Delete — `no interface Ethernet1.4011`.
//  6. Read after delete — sub-interface absent.
func TestEAPI_SubinterfaceLifecycle(t *testing.T) {
	cli := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	const parent = "Ethernet1"
	const sub = "Ethernet1.4011"

	t.Cleanup(func() {
		ctxC, cancelC := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancelC()
		sess, err := cli.OpenSession(ctxC, "pulumi-it-subif-cleanup")
		if err != nil {
			return
		}
		_ = sess.Stage(ctxC, []string{
			"no interface " + sub,
			"interface " + parent,
			"switchport",
			"shutdown",
		})
		_ = sess.Commit(ctxC)
	})

	// 1. Routed parent
	mustApplySubif(ctx, t, cli, "pulumi-it-subif-parent", []string{
		"interface " + parent,
		"no switchport",
		"no shutdown",
	})

	// 2. Create sub-interface
	mustApplySubif(ctx, t, cli, "pulumi-it-subif-create", []string{
		"interface " + sub,
		"encapsulation dot1q vlan 4011",
		"description pulumi-it",
		"ip address 10.40.11.1/30",
		"no shutdown",
	})

	// 3. Read
	if got := readSubifField(ctx, t, cli, sub, "encapsulation dot1q vlan "); got != "4011" {
		t.Fatalf("vlan = %q", got)
	}
	if got := readSubifField(ctx, t, cli, sub, "ip address "); got != "10.40.11.1/30" {
		t.Fatalf("ip = %q", got)
	}
	if got := readSubifField(ctx, t, cli, sub, "description "); got != "pulumi-it" {
		t.Fatalf("desc = %q", got)
	}

	// 4. Update + idempotent re-apply
	mustApplySubif(ctx, t, cli, "pulumi-it-subif-update", []string{
		"interface " + sub,
		"description pulumi-it-renamed",
	})
	mustApplySubif(ctx, t, cli, "pulumi-it-subif-reapply", []string{
		"interface " + sub,
		"description pulumi-it-renamed",
	})
	if got := readSubifField(ctx, t, cli, sub, "description "); got != "pulumi-it-renamed" {
		t.Fatalf("renamed desc = %q", got)
	}

	// 5. Delete
	mustApplySubif(ctx, t, cli, "pulumi-it-subif-delete", []string{"no interface " + sub})

	// 6. Read after delete — section absent.
	if got := readSubifField(ctx, t, cli, sub, "encapsulation dot1q vlan "); got != "" {
		t.Fatalf("expected sub-interface absent, got vlan=%q", got)
	}
}

func mustApplySubif(ctx context.Context, t *testing.T, cli *eapi.Client, sessName string, cmds []string) {
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

// readSubifField returns the value of `<prefix><value>` from
// running-config interfaces <name>, or "" when the field is absent.
//
//nolint:unparam // helper is reusable; current test only calls it with one name.
func readSubifField(ctx context.Context, t *testing.T, cli *eapi.Client, name, prefix string) string {
	t.Helper()
	resp, err := cli.RunCmds(ctx, []string{"show running-config interfaces " + name}, "text")
	if err != nil {
		t.Fatalf("show running-config interfaces %s: %v", name, err)
	}
	if len(resp) == 0 {
		return ""
	}
	out, _ := resp[0]["output"].(string)
	for raw := range strings.SplitSeq(out, "\n") {
		line := strings.TrimSpace(raw)
		if v, ok := strings.CutPrefix(line, prefix); ok {
			return v
		}
	}
	return ""
}
