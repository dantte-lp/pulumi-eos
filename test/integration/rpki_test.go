//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dantte-lp/pulumi-eos/internal/client/eapi"
)

// TestEAPI_RpkiLifecycle drives an `rpki cache` lifecycle inside
// `router bgp <asn>`. Verifies multiple caches coexist (the canonical
// 2-cache redundant pattern from the EOS BGP RPKI Origin Validation
// Design Guide) and that single-cache delete leaves the others
// intact.
func TestEAPI_RpkiLifecycle(t *testing.T) {
	cli := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	const asn = "65000"

	t.Cleanup(func() {
		ctxC, cancelC := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancelC()
		sess, err := cli.OpenSession(ctxC, "pulumi-it-rpki-cleanup")
		if err != nil {
			return
		}
		_ = sess.Stage(ctxC, []string{"no router bgp " + asn})
		_ = sess.Commit(ctxC)
	})

	// 1. Create router bgp + cache 1 + cache 2
	mustApplyRpki(ctx, t, cli, "pulumi-it-rpki-create", []string{
		"router bgp " + asn,
		"rpki cache PULUMI_C1",
		"host 192.168.0.227 port 3323",
		"preference 4",
		"refresh-interval 30",
		"retry-interval 10",
		"expire-interval 600",
		"transport tcp",
		"exit",
		"rpki cache PULUMI_C2",
		"host 192.168.0.126 port 3323",
		"preference 6",
		"transport tcp",
		"exit",
		"exit",
	})

	// 2. Read — both caches present
	section := readBgpSection(ctx, t, cli)
	if !strings.Contains(section, "rpki cache PULUMI_C1") {
		t.Fatalf("C1 missing:\n%s", section)
	}
	if !strings.Contains(section, "host 192.168.0.227 port 3323") {
		t.Fatalf("C1 host line missing:\n%s", section)
	}
	if !strings.Contains(section, "rpki cache PULUMI_C2") {
		t.Fatalf("C2 missing:\n%s", section)
	}

	// 3. Update — change C1 preference; idempotent re-apply
	mustApplyRpki(ctx, t, cli, "pulumi-it-rpki-update", []string{
		"router bgp " + asn,
		"rpki cache PULUMI_C1",
		"preference 3",
		"exit",
		"exit",
	})
	mustApplyRpki(ctx, t, cli, "pulumi-it-rpki-reapply", []string{
		"router bgp " + asn,
		"rpki cache PULUMI_C1",
		"preference 3",
		"exit",
		"exit",
	})
	section = readBgpSection(ctx, t, cli)
	if !strings.Contains(section, "preference 3") {
		t.Fatalf("preference update didn't take effect:\n%s", section)
	}

	// 4. Delete C1 only — C2 must survive
	mustApplyRpki(ctx, t, cli, "pulumi-it-rpki-delete-c1", []string{
		"router bgp " + asn,
		"no rpki cache PULUMI_C1",
		"exit",
	})
	section = readBgpSection(ctx, t, cli)
	if strings.Contains(section, "rpki cache PULUMI_C1") {
		t.Fatalf("C1 persisted after delete:\n%s", section)
	}
	if !strings.Contains(section, "rpki cache PULUMI_C2") {
		t.Fatalf("C2 disappeared when C1 was deleted:\n%s", section)
	}
}

func mustApplyRpki(ctx context.Context, t *testing.T, cli *eapi.Client, sessName string, cmds []string) {
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

func readBgpSection(ctx context.Context, t *testing.T, cli *eapi.Client) string {
	t.Helper()
	resp, err := cli.RunCmds(ctx, []string{"show running-config | section router bgp"}, "text")
	if err != nil {
		t.Fatalf("section router bgp: %v", err)
	}
	if len(resp) == 0 {
		return ""
	}
	out, _ := resp[0]["output"].(string)
	return out
}
