//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dantte-lp/pulumi-eos/internal/client/eapi"
)

// TestEAPI_RcfLifecycle_FileReference drives the SourceFile delivery
// mode (`code [unit X] source pulled-from <path>`).
func TestEAPI_RcfLifecycle_FileReference(t *testing.T) {
	cli := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	const namedUnit = "PULUMI_IT_RCF_FILE"

	t.Cleanup(func() {
		ctxC, cancelC := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancelC()
		sess, err := cli.OpenSession(ctxC, "pulumi-it-rcf-file-cleanup")
		if err != nil {
			return
		}
		_ = sess.Stage(ctxC, []string{
			"router general",
			"control-functions",
			"no code source pulled-from",
			"no code",
			"no code unit " + namedUnit + " source pulled-from",
			"no code unit " + namedUnit,
			"exit",
			"exit",
		})
		_ = sess.Commit(ctxC)
	})

	mustApplyRcf(ctx, t, cli, "pulumi-it-rcf-file-create", []string{
		"router general",
		"control-functions",
		"code source pulled-from flash:pulumi-it-default.txt",
		"code unit " + namedUnit + " source pulled-from flash:pulumi-it-named.txt",
		"exit",
		"exit",
	})

	section := readRcfSection(ctx, t, cli)
	if !strings.Contains(section, "code source pulled-from flash:pulumi-it-default.txt") {
		t.Fatalf("default unit missing:\n%s", section)
	}
	if !strings.Contains(section, "code unit "+namedUnit+" source pulled-from flash:pulumi-it-named.txt") {
		t.Fatalf("named unit missing:\n%s", section)
	}

	// Update — re-point named unit at a different file.
	mustApplyRcf(ctx, t, cli, "pulumi-it-rcf-file-update", []string{
		"router general",
		"control-functions",
		"code unit " + namedUnit + " source pulled-from flash:pulumi-it-named-v2.txt",
		"exit",
		"exit",
	})
	section = readRcfSection(ctx, t, cli)
	if !strings.Contains(section, "flash:pulumi-it-named-v2.txt") {
		t.Fatalf("update didn't take effect:\n%s", section)
	}

	// Delete
	mustApplyRcf(ctx, t, cli, "pulumi-it-rcf-file-delete", []string{
		"router general",
		"control-functions",
		"no code source pulled-from",
		"no code",
		"no code unit " + namedUnit + " source pulled-from",
		"no code unit " + namedUnit,
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

// TestEAPI_RcfLifecycle_InlineCode drives the Code delivery mode
// (eAPI complex command with `input` field). This is the canonical
// Pulumi-native workflow per the v1 Rcf design.
func TestEAPI_RcfLifecycle_InlineCode(t *testing.T) {
	cli := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	const namedUnit = "PULUMI_IT_RCF_INLINE"

	t.Cleanup(func() {
		ctxC, cancelC := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancelC()
		sess, err := cli.OpenSession(ctxC, "pulumi-it-rcf-inline-cleanup")
		if err != nil {
			return
		}
		_ = sess.Stage(ctxC, []string{
			"router general",
			"control-functions",
			"no code unit " + namedUnit + " source pulled-from",
			"exit",
			"exit",
		})
		_ = sess.Commit(ctxC)
	})

	// 1. Create — push inline RCF source via the rich-command path.
	rcfBody := "function ACCEPT_ALL() {\n  return true;\n}\nfunction DENY_ALL() {\n  return false;\n}\nEOF\n"
	mustApplyRcfRich(ctx, t, cli, "pulumi-it-rcf-inline-create", []eapi.Command{
		{Cmd: "router general"},
		{Cmd: "control-functions"},
		{Cmd: "code unit " + namedUnit, Input: rcfBody},
		{Cmd: "exit"},
		{Cmd: "exit"},
	})

	// 2. Read — running-config emits the inline body verbatim.
	section := readRcfSection(ctx, t, cli)
	if !strings.Contains(section, "code unit "+namedUnit) {
		t.Fatalf("named unit missing:\n%s", section)
	}
	if !strings.Contains(section, "function ACCEPT_ALL()") {
		t.Fatalf("inline body ACCEPT_ALL missing:\n%s", section)
	}
	if !strings.Contains(section, "function DENY_ALL()") {
		t.Fatalf("inline body DENY_ALL missing:\n%s", section)
	}

	// 3. Update — replace the body with a single function.
	updatedBody := "function MARK_INTERNAL() {\n  community add { 65000:100 };\n  return true;\n}\nEOF\n"
	mustApplyRcfRich(ctx, t, cli, "pulumi-it-rcf-inline-update", []eapi.Command{
		{Cmd: "router general"},
		{Cmd: "control-functions"},
		{Cmd: "code unit " + namedUnit, Input: updatedBody},
		{Cmd: "exit"},
		{Cmd: "exit"},
	})
	section = readRcfSection(ctx, t, cli)
	if !strings.Contains(section, "function MARK_INTERNAL()") {
		t.Fatalf("update didn't replace the body:\n%s", section)
	}
	if strings.Contains(section, "function ACCEPT_ALL()") {
		t.Fatalf("stale ACCEPT_ALL leaked across re-emit:\n%s", section)
	}

	// 4. Delete via plain CLI form. cEOS distinguishes negation by
	// delivery mode: `no code unit X` removes inline-form units;
	// `no code unit X source pulled-from` removes file-reference
	// units. Both forms are idempotent on a non-existent unit, so
	// the resource emits both during Delete.
	mustApplyRcf(ctx, t, cli, "pulumi-it-rcf-inline-delete", []string{
		"router general",
		"control-functions",
		"no code unit " + namedUnit + " source pulled-from",
		"no code unit " + namedUnit,
		"exit",
		"exit",
	})
	section = readRcfSection(ctx, t, cli)
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

func mustApplyRcfRich(ctx context.Context, t *testing.T, cli *eapi.Client, sessName string, cmds []eapi.Command) {
	t.Helper()
	sess, err := cli.OpenSession(ctx, sessName)
	if err != nil {
		t.Fatalf("OpenSession(%s): %v", sessName, err)
	}
	if stageErr := sess.StageRich(ctx, cmds); stageErr != nil {
		_ = sess.Abort(ctx)
		t.Fatalf("StageRich(%s): %v", sessName, stageErr)
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
