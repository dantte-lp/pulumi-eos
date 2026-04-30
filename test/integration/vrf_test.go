//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dantte-lp/pulumi-eos/internal/client/eapi"
)

// TestEAPI_VrfLifecycle drives a Vrf resource lifecycle end-to-end against
// cEOS at the eAPI session layer (the same primitives used by
// internal/resources/l3.Vrf).
//
//  1. Create — `vrf instance PULUMI_IT`, description, `ip routing vrf`.
//  2. Read   — section view returns the description.
//  3. Update — change description; idempotent re-apply.
//  4. Toggle IPv6 — enable + verify; disable + verify.
//  5. Delete — `no vrf instance` cascades.
//  6. Read after delete — VRF gone.
func TestEAPI_VrfLifecycle(t *testing.T) {
	cli := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	const name = "PULUMI_IT"

	t.Cleanup(func() {
		ctxC, cancelC := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancelC()
		sess, err := cli.OpenSession(ctxC, "pulumi-it-vrf-cleanup")
		if err != nil {
			return
		}
		_ = sess.Stage(ctxC, []string{
			"no ip routing vrf " + name,
			"no ipv6 unicast-routing vrf " + name,
			"no vrf instance " + name,
		})
		_ = sess.Commit(ctxC)
	})

	// 1. Create
	mustApplyVrf(ctx, t, cli, "pulumi-it-vrf-create", []string{
		"vrf instance " + name,
		"description pulumi-it tenant",
		"exit",
		"ip routing vrf " + name,
	})

	// 2. Read description
	if got := readVrfDescription(ctx, t, cli, name); got != "pulumi-it tenant" {
		t.Fatalf("expected description=pulumi-it tenant, got %q", got)
	}

	// 3. Update + idempotent re-apply
	mustApplyVrf(ctx, t, cli, "pulumi-it-vrf-update", []string{
		"vrf instance " + name,
		"description pulumi-it tenant renamed",
		"exit",
	})
	mustApplyVrf(ctx, t, cli, "pulumi-it-vrf-reapply", []string{
		"vrf instance " + name,
		"description pulumi-it tenant renamed",
		"exit",
	})
	if got := readVrfDescription(ctx, t, cli, name); got != "pulumi-it tenant renamed" {
		t.Fatalf("expected renamed description, got %q", got)
	}

	// 4a. Enable IPv6 routing
	mustApplyVrf(ctx, t, cli, "pulumi-it-vrf-v6-on", []string{
		"ipv6 unicast-routing vrf " + name,
	})
	if !readVrfRoutingEnabled(ctx, t, cli, name, true) {
		t.Fatalf("expected ipv6 unicast-routing vrf %s after enable", name)
	}

	// 4b. Disable IPv6 routing
	mustApplyVrf(ctx, t, cli, "pulumi-it-vrf-v6-off", []string{
		"no ipv6 unicast-routing vrf " + name,
	})
	if readVrfRoutingEnabled(ctx, t, cli, name, true) {
		t.Fatalf("expected no ipv6 unicast-routing vrf %s after disable", name)
	}

	// 5. Delete
	mustApplyVrf(ctx, t, cli, "pulumi-it-vrf-delete", []string{
		"no ip routing vrf " + name,
		"no ipv6 unicast-routing vrf " + name,
		"no vrf instance " + name,
	})

	// 6. Read after delete — section absent.
	if got := readVrfDescription(ctx, t, cli, name); got != "" {
		t.Fatalf("expected vrf to be absent after delete, got description=%q", got)
	}
}

func mustApplyVrf(ctx context.Context, t *testing.T, cli *eapi.Client, sessName string, cmds []string) {
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

// readVrfDescription parses the description from the running-config
// section view of the VRF instance.
func readVrfDescription(ctx context.Context, t *testing.T, cli *eapi.Client, name string) string {
	t.Helper()
	resp, err := cli.RunCmds(ctx,
		[]string{"show running-config | section vrf instance " + name},
		"text")
	if err != nil {
		t.Fatalf("section vrf instance: %v", err)
	}
	if len(resp) == 0 {
		return ""
	}
	out, _ := resp[0]["output"].(string)
	if !strings.Contains(out, "vrf instance "+name) {
		return ""
	}
	for raw := range strings.SplitSeq(out, "\n") {
		line := strings.TrimSpace(raw)
		if v, ok := strings.CutPrefix(line, "description "); ok {
			return v
		}
	}
	return ""
}

// readVrfRoutingEnabled probes whether `(ip|ipv6) routing vrf <name>` is
// present in running-config.
func readVrfRoutingEnabled(ctx context.Context, t *testing.T, cli *eapi.Client, name string, ipv6 bool) bool {
	t.Helper()
	needle := "ip routing vrf " + name
	if ipv6 {
		needle = "ipv6 unicast-routing vrf " + name
	}
	resp, err := cli.RunCmds(ctx,
		[]string{"show running-config | include " + needle},
		"text")
	if err != nil {
		t.Fatalf("include %s: %v", needle, err)
	}
	if len(resp) == 0 {
		return false
	}
	out, _ := resp[0]["output"].(string)
	for raw := range strings.SplitSeq(out, "\n") {
		if strings.TrimSpace(raw) == needle {
			return true
		}
	}
	return false
}
