//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dantte-lp/pulumi-eos/internal/client/eapi"
)

// TestEAPI_RcfLifecycle drives an RCF code-unit lifecycle against
// cEOS at the eAPI session layer. v0 references a pre-staged file on
// flash via `code [unit X] source pulled-from <path>` — the file
// itself need not exist for EOS to accept the configuration; EOS
// reports a compilation error at apply time when it tries to load
// the unit, but the running-config row lands either way.
func TestEAPI_RcfLifecycle(t *testing.T) {
	cli := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	const namedUnit = "PULUMI_IT_RCF"

	t.Cleanup(func() {
		ctxC, cancelC := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancelC()
		sess, err := cli.OpenSession(ctxC, "pulumi-it-rcf-cleanup")
		if err != nil {
			return
		}
		_ = sess.Stage(ctxC, []string{
			"router general",
			"control-functions",
			"no code source pulled-from",
			"no code unit " + namedUnit + " source pulled-from",
			"exit",
			"exit",
		})
		_ = sess.Commit(ctxC)
	})

	// 1. Create both: default unit + named unit
	mustApplyRcf(ctx, t, cli, "pulumi-it-rcf-create", []string{
		"router general",
		"control-functions",
		"code source pulled-from flash:pulumi-it-default.txt",
		"code unit " + namedUnit + " source pulled-from flash:pulumi-it-rcf.txt",
		"exit",
		"exit",
	})

	// 2. Read
	section := readRcfSection(ctx, t, cli)
	if !strings.Contains(section, "code source pulled-from flash:pulumi-it-default.txt") {
		t.Fatalf("default unit missing:\n%s", section)
	}
	if !strings.Contains(section, "code unit "+namedUnit+" source pulled-from flash:pulumi-it-rcf.txt") {
		t.Fatalf("named unit missing:\n%s", section)
	}

	// 3. Idempotent re-apply
	mustApplyRcf(ctx, t, cli, "pulumi-it-rcf-reapply", []string{
		"router general",
		"control-functions",
		"code source pulled-from flash:pulumi-it-default.txt",
		"code unit " + namedUnit + " source pulled-from flash:pulumi-it-rcf.txt",
		"exit",
		"exit",
	})

	// 4. Update — re-point named unit at a different file
	mustApplyRcf(ctx, t, cli, "pulumi-it-rcf-update", []string{
		"router general",
		"control-functions",
		"code unit " + namedUnit + " source pulled-from flash:pulumi-it-rcf-v2.txt",
		"exit",
		"exit",
	})
	section = readRcfSection(ctx, t, cli)
	if !strings.Contains(section, "code unit "+namedUnit+" source pulled-from flash:pulumi-it-rcf-v2.txt") {
		t.Fatalf("update didn't take effect:\n%s", section)
	}

	// 5. Delete both units
	mustApplyRcf(ctx, t, cli, "pulumi-it-rcf-delete", []string{
		"router general",
		"control-functions",
		"no code source pulled-from",
		"no code unit " + namedUnit + " source pulled-from",
		"exit",
		"exit",
	})
	section = readRcfSection(ctx, t, cli)
	if strings.Contains(section, "code source pulled-from") {
		t.Fatalf("default unit persisted after delete:\n%s", section)
	}
	if strings.Contains(section, "code unit "+namedUnit) {
		t.Fatalf("named unit persisted after delete:\n%s", section)
	}
}

func mustApplyRcf(ctx context.Context, t *testing.T, cli *eapi.Client, sessName string, cmds []string) {
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

func readRcfSection(ctx context.Context, t *testing.T, cli *eapi.Client) string {
	t.Helper()
	resp, err := cli.RunCmds(ctx, []string{"show running-config | section router general"}, "text")
	if err != nil {
		t.Fatalf("section router general: %v", err)
	}
	if len(resp) == 0 {
		return ""
	}
	out, _ := resp[0]["output"].(string)
	return out
}
