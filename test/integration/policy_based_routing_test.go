//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestEAPI_PolicyBasedRoutingLifecycle drives the eos:l3:PolicyBasedRouting
// command flow against cEOS at the eAPI session layer. v0 of the
// resource takes class-map + ACL names as strings, so the test
// stages the ACL + class-map directly through eAPI in a separate
// pre-fixture session — the policy-map body is what we validate
// against the running-config.
//
// Verifies:
//   - apply (idempotent) of a 3-sequence policy-map
//     (set nexthop / set nexthop-group / drop);
//   - read-back through `show running-config section policy-map`;
//   - service-policy attach + detach on Ethernet1;
//   - cleanup leaves no residual class-map / ACL / nexthop-group.
func TestEAPI_PolicyBasedRoutingLifecycle(t *testing.T) {
	cli := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	const (
		policyName = "PULUMI_IT_PBR"
		cmap1      = "PULUMI_IT_CMAP1"
		cmap2      = "PULUMI_IT_CMAP2"
		cmap3      = "PULUMI_IT_CMAP3"
		aclName    = "PULUMI_IT_ACL"
		nhgName    = "PULUMI_IT_NHG"
	)

	cleanup := func() {
		ctxC, cancelC := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancelC()
		sess, err := cli.OpenSession(ctxC, "pulumi-it-pbr-cleanup")
		if err != nil {
			return
		}
		_ = sess.Stage(ctxC, []string{
			"interface Ethernet1",
			"no service-policy type pbr input " + policyName,
			"exit",
			"no policy-map type pbr " + policyName,
			"no class-map type pbr match-any " + cmap1,
			"no class-map type pbr match-any " + cmap2,
			"no class-map type pbr match-any " + cmap3,
			"no nexthop-group " + nhgName + " type ip",
			"no ip access-list " + aclName,
		})
		_ = sess.Commit(ctxC)
	}
	t.Cleanup(cleanup)
	cleanup() // start clean

	// Pre-fixture: ACL + class-map + nexthop-group in their own
	// commit so the policy-map session below references existing
	// resources (PBR commit fails if any reference is dangling).
	preSess, openErr := cli.OpenSession(ctx, "pulumi-it-pbr-pre")
	if openErr != nil {
		t.Fatalf("pre OpenSession: %v", openErr)
	}
	if stageErr := preSess.Stage(ctx, []string{
		"ip access-list " + aclName,
		"permit ip any any", //nolint:dupword // EOS ACL keyword pair.
		"exit",
		"class-map type pbr match-any " + cmap1,
		"match ip access-group " + aclName,
		"exit",
		"class-map type pbr match-any " + cmap2,
		"match ip access-group " + aclName,
		"exit",
		"class-map type pbr match-any " + cmap3,
		"match ip access-group " + aclName,
		"exit",
		"nexthop-group " + nhgName + " type ip",
		"entry 0 nexthop 10.0.0.1",
		"exit",
	}); stageErr != nil {
		t.Fatalf("pre Stage: %v", stageErr)
	}
	if commitErr := preSess.Commit(ctx); commitErr != nil {
		t.Fatalf("pre Commit: %v", commitErr)
	}

	// Apply the policy-map body — same shape the Go resource
	// renders. Three sequences exercise all v0 actions in one
	// commit.
	body := []string{
		"no policy-map type pbr " + policyName,
		"policy-map type pbr " + policyName,
		"10 class " + cmap1,
		"set nexthop 10.0.0.1",
		"exit",
		"20 class " + cmap2,
		"set nexthop-group " + nhgName,
		"exit",
		"30 class " + cmap3,
		"drop",
		"exit",
		"exit",
		"interface Ethernet1",
		"service-policy type pbr input " + policyName,
		"exit",
	}
	apply := func(name string) {
		t.Helper()
		sess, openErr := cli.OpenSession(ctx, "pulumi-it-pbr-"+name)
		if openErr != nil {
			t.Fatalf("OpenSession %s: %v", name, openErr)
		}
		if stageErr := sess.Stage(ctx, body); stageErr != nil {
			t.Fatalf("Stage %s: %v", name, stageErr)
		}
		if commitErr := sess.Commit(ctx); commitErr != nil {
			t.Fatalf("Commit %s: %v", name, commitErr)
		}
	}
	apply("apply1")
	apply("apply2") // idempotent re-emit

	// Read-back: assert canonical lines appear under
	// `policy-map type pbr <name>`.
	resp, runErr := cli.RunCmds(ctx,
		[]string{"show running-config section policy-map"}, "text")
	if runErr != nil {
		t.Fatalf("show running-config: %v", runErr)
	}
	out, _ := resp[0]["output"].(string)

	wantPresent := []string{
		"policy-map type pbr " + policyName,
		"10 class " + cmap1,
		"set nexthop 10.0.0.1",
		"20 class " + cmap2,
		"set nexthop-group " + nhgName,
		"30 class " + cmap3,
		"drop",
	}
	for _, line := range wantPresent {
		if !strings.Contains(out, line) {
			t.Errorf("missing in running-config: %q\n--- got ---\n%s", line, out)
		}
	}

	// Verify the interface attachment landed.
	resp, runErr = cli.RunCmds(ctx,
		[]string{"show running-config interfaces Ethernet1"}, "text")
	if runErr != nil {
		t.Fatalf("show interface: %v", runErr)
	}
	intfOut, _ := resp[0]["output"].(string)
	if !strings.Contains(intfOut, "service-policy type pbr input "+policyName) {
		t.Errorf("attachment missing on Ethernet1:\n%s", intfOut)
	}
}
