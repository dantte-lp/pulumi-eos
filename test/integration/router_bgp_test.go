//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dantte-lp/pulumi-eos/internal/client/eapi"
)

// TestEAPI_RouterBgpLifecycle drives the v0 RouterBgp surface against
// cEOS. Verifies the staged CLI block lands and the section view
// echoes back the canonical ordering EOS expects.
//
//  1. Create — full EVPN-fabric block (router-id, no-default-ipv4,
//     maximum-paths, peer-group SPINE, neighbor binding,
//     AF evpn activate, AF ipv4 deactivate, vrf RED with rd/rt/redist).
//  2. Read   — `show running-config | section router bgp` reflects the
//     globals and the structured sub-blocks.
//  3. Update — flip maximum-paths to 8 ecmp 8; idempotent re-apply.
//  4. Delete — `no router bgp 65001` clears the section.
func TestEAPI_RouterBgpLifecycle(t *testing.T) {
	cli := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	t.Cleanup(func() {
		ctxC, cancelC := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancelC()
		sess, err := cli.OpenSession(ctxC, "pulumi-it-rbgp-cleanup")
		if err != nil {
			return
		}
		_ = sess.Stage(ctxC, []string{"no router bgp 65001"})
		_ = sess.Commit(ctxC)
	})

	// 1. Create
	mustApplyRBgp(ctx, t, cli, "pulumi-it-rbgp-create", []string{
		"router bgp 65001",
		"router-id 1.1.1.1",
		"no bgp default ipv4-unicast",
		"maximum-paths 4 ecmp 4",
		"neighbor SPINE peer group",
		"neighbor SPINE remote-as 65000",
		"neighbor SPINE update-source Loopback42",
		"neighbor SPINE send-community extended",
		"neighbor SPINE maximum-routes 12000",
		"neighbor 10.0.0.1 peer group SPINE",
		"address-family evpn",
		"neighbor SPINE activate",
		"exit",
		"address-family ipv4",
		"no neighbor SPINE activate",
		"exit",
		"vrf RED",
		"rd 1.1.1.1:10",
		"route-target import evpn 64500:10",
		"route-target export evpn 64500:10",
		"redistribute connected",
		"exit",
		"exit",
	})

	// 2. Read
	section := readRouterBgpSection(ctx, t, cli)
	if !strings.Contains(section, "router bgp 65001") {
		t.Fatalf("missing router bgp header:\n%s", section)
	}
	if !strings.Contains(section, "no bgp default ipv4-unicast") {
		t.Fatalf("missing no-default-ipv4 line:\n%s", section)
	}
	if !strings.Contains(section, "maximum-paths 4 ecmp 4") {
		t.Fatalf("missing maximum-paths:\n%s", section)
	}
	if !strings.Contains(section, "neighbor SPINE peer group") {
		t.Fatalf("missing peer-group SPINE:\n%s", section)
	}
	if !strings.Contains(section, "neighbor 10.0.0.1 peer group SPINE") {
		t.Fatalf("missing neighbor binding:\n%s", section)
	}
	if !strings.Contains(section, "address-family evpn") {
		t.Fatalf("missing AF evpn:\n%s", section)
	}
	if !strings.Contains(section, "vrf RED") || !strings.Contains(section, "rd 1.1.1.1:10") {
		t.Fatalf("missing vrf RED block:\n%s", section)
	}

	// 3. Update + idempotent re-apply
	mustApplyRBgp(ctx, t, cli, "pulumi-it-rbgp-update", []string{
		"router bgp 65001",
		"maximum-paths 8 ecmp 8",
		"exit",
	})
	mustApplyRBgp(ctx, t, cli, "pulumi-it-rbgp-reapply", []string{
		"router bgp 65001",
		"maximum-paths 8 ecmp 8",
		"exit",
	})
	if section := readRouterBgpSection(ctx, t, cli); !strings.Contains(section, "maximum-paths 8 ecmp 8") {
		t.Fatalf("update didn't take effect:\n%s", section)
	}

	// 4. Delete
	mustApplyRBgp(ctx, t, cli, "pulumi-it-rbgp-delete", []string{"no router bgp 65001"})
	if section := readRouterBgpSection(ctx, t, cli); strings.Contains(section, "router bgp 65001") {
		t.Fatalf("router bgp persisted after delete:\n%s", section)
	}
}

func mustApplyRBgp(ctx context.Context, t *testing.T, cli *eapi.Client, sessName string, cmds []string) {
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

func readRouterBgpSection(ctx context.Context, t *testing.T, cli *eapi.Client) string {
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
