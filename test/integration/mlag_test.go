//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestEAPI_MlagLifecycle drives the singleton MLAG configuration block
// through the eAPI session primitives the l2.Mlag resource uses.
//
// cEOS does not have a real peer in this single-container test, so MLAG
// will not transition to "Active". The test only verifies that the
// configuration is accepted, persists in running-config, and can be
// updated and removed cleanly.
func TestEAPI_MlagLifecycle(t *testing.T) {
	cli := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	t.Cleanup(func() {
		ctxC, cancelC := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancelC()
		sess, err := cli.OpenSession(ctxC, "pulumi-it-mlag-cleanup")
		if err != nil {
			return
		}
		_ = sess.Stage(ctxC, []string{
			"no mlag configuration",
			"no interface Port-Channel1000",
			"no interface Vlan4094",
			"no vlan 4094",
		})
		_ = sess.Commit(ctxC)
	})

	// 1. Pre-requisites.
	mustApply(ctx, t, cli, "pulumi-it-mlag-prereq", []string{
		"vlan 4094",
		"name MLAG-PEER",
		"trunk group MLAG-PEER",
		"interface Vlan4094",
		"ip address 10.0.0.1/30",
		"interface Port-Channel1000",
		"switchport",
		"switchport mode trunk",
		"switchport trunk group MLAG-PEER",
	})

	// 2. Create MLAG block.
	mustApply(ctx, t, cli, "pulumi-it-mlag-create", []string{
		"no mlag configuration",
		"mlag configuration",
		"domain-id pulumi-it-domain",
		"local-interface Vlan4094",
		"peer-link Port-Channel1000",
		"peer-address 10.0.0.2",
		"dual-primary detection delay 5 action errdisable all-interfaces",
	})

	cfg := mustShowMlag(ctx, t, cli)
	if !strings.Contains(cfg, "mlag configuration") {
		t.Fatalf("expected mlag configuration block, got:\n%s", cfg)
	}
	if !strings.Contains(cfg, "domain-id pulumi-it-domain") {
		t.Fatalf("expected domain-id pulumi-it-domain, got:\n%s", cfg)
	}
	if !strings.Contains(cfg, "dual-primary detection delay 5 action errdisable all-interfaces") {
		t.Fatalf("expected dual-primary detection delay 5, got:\n%s", cfg)
	}

	// 3. Update — change domain id, drop dual-primary detection.
	mustApply(ctx, t, cli, "pulumi-it-mlag-update", []string{
		"no mlag configuration",
		"mlag configuration",
		"domain-id pulumi-it-domain-v2",
		"local-interface Vlan4094",
		"peer-link Port-Channel1000",
		"peer-address 10.0.0.2",
	})

	cfg = mustShowMlag(ctx, t, cli)
	if !strings.Contains(cfg, "domain-id pulumi-it-domain-v2") {
		t.Fatalf("expected updated domain id, got:\n%s", cfg)
	}
	if strings.Contains(cfg, "dual-primary detection") {
		t.Fatalf("expected dual-primary detection gone, got:\n%s", cfg)
	}

	// 4. Delete.
	mustApply(ctx, t, cli, "pulumi-it-mlag-delete", []string{"no mlag configuration"})

	cfg = mustShowMlag(ctx, t, cli)
	if strings.Contains(cfg, "domain-id") {
		t.Fatalf("expected mlag block gone, got:\n%s", cfg)
	}
}

// runCmds is the minimal subset of *eapi.Client used by the show-helpers.
type runCmds interface {
	RunCmds(ctx context.Context, cmds []string, format string) ([]map[string]any, error)
}

func mustShowMlag(ctx context.Context, t *testing.T, cli runCmds) string {
	t.Helper()
	resp, err := cli.RunCmds(ctx, []string{"show running-config | section mlag configuration"}, "text")
	if err != nil {
		t.Fatalf("show running-config | section mlag: %v", err)
	}
	if len(resp) == 0 {
		return ""
	}
	if v, ok := resp[0]["output"].(string); ok {
		return v
	}
	return ""
}
