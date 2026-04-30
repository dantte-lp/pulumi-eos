package device

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/pulumi/pulumi-go-provider/infer"

	"github.com/dantte-lp/pulumi-eos/internal/client/eapi"
	"github.com/dantte-lp/pulumi-eos/internal/config"
)

// Sentinel errors specific to RawCli.
var (
	ErrRawCliNameRequired    = errors.New("rawCli name is required")
	ErrRawCliContentRequired = errors.New("rawCli body is required (at least one non-blank line)")
)

// RawCli stages a user-supplied CLI block inside an atomic configuration
// session and applies it only when the session diff against running-config
// is non-empty. It is the v0.x escape hatch for features that do not yet
// have a typed `eos:*` resource.
//
// Compared with `eos:device:Configlet`:
//   - Configlet always commits the staged body, idempotent or not.
//   - RawCli inspects `show session-config named … diffs` and aborts when
//     the diff is empty, so a re-apply against an already-converged device
//     never touches running-config.
//
// An optional inverse `DeleteBody` is staged and committed on Delete; if
// absent, Delete is a no-op (matches the Configlet semantic).
//
// Source: EOS User Manual §3 — `configure session` semantics; verified
// against the cEOS 4.36.0.1F integration test harness.
type RawCli struct{}

// RawCliArgs is the input set.
type RawCliArgs struct {
	// Name uniquely identifies the resource. Used in the resource ID and
	// in the EOS configuration-session name.
	Name string `pulumi:"name"`
	// Description is an optional human-readable description shown only
	// in Pulumi state and resource docs (it does not appear on the
	// device).
	Description *string `pulumi:"description,optional"`
	// Body is the raw multi-line CLI block. Each non-blank line is
	// staged inside `configure session pulumi-<name>`.
	Body string `pulumi:"body"`
	// DeleteBody is an optional inverse CLI block applied on Delete.
	// When unset, Delete is a no-op on the device.
	DeleteBody *string `pulumi:"deleteBody,optional"`

	// Host overrides the provider-level eosUrl host for this resource.
	Host *string `pulumi:"host,optional"`
	// Username overrides the provider-level eosUsername for this resource.
	Username *string `pulumi:"username,optional"`
	// Password overrides the provider-level eosPassword for this resource.
	Password *string `provider:"secret" pulumi:"password,optional"`
}

// RawCliState is Args plus the body and diff digests so callers can detect
// drift on the body and observe whether the last apply was a no-op.
type RawCliState struct {
	RawCliArgs

	// BodySha256 is the lowercase hex SHA-256 digest of the canonicalised
	// body (lines trimmed, blank lines removed, joined with `\n`).
	BodySha256 string `pulumi:"bodySha256"`
	// LastDiffSha256 is the lowercase hex SHA-256 digest of the diff
	// committed on the last non-empty apply. Empty when the last apply
	// detected no change.
	LastDiffSha256 string `pulumi:"lastDiffSha256"`
	// LastDiffEmpty records whether the most recent apply was a no-op
	// (session diff was empty).
	LastDiffEmpty bool `pulumi:"lastDiffEmpty"`
}

// Annotate documents the resource.
func (r *RawCli) Annotate(a infer.Annotator) {
	a.Describe(&r, "Idempotent escape hatch: stage a raw CLI block inside a config-session and commit only when the diff against running-config is non-empty.")
}

// Annotate documents RawCliArgs fields.
func (a *RawCliArgs) Annotate(an infer.Annotator) {
	an.Describe(&a.Name, "Stable identifier; appears in the EOS configuration-session name and the Pulumi resource ID.")
	an.Describe(&a.Description, "Pulumi-side description; not pushed to the device.")
	an.Describe(&a.Body, "Multi-line CLI block staged inside `configure session pulumi-<name>`. Applied only when the session diff is non-empty.")
	an.Describe(&a.DeleteBody, "Optional inverse CLI block applied on Delete. When unset, Delete is a no-op on the device.")
	an.Describe(&a.Host, "Optional management hostname override.")
	an.Describe(&a.Username, "Optional AAA username override.")
	an.Describe(&a.Password, "Optional AAA password override.")
}

// Annotate documents the RawCliState fields not already covered by Args.
func (s *RawCliState) Annotate(an infer.Annotator) {
	an.Describe(&s.BodySha256, "Lowercase hex SHA-256 digest of the canonicalised body. Stable across re-applies.")
	an.Describe(&s.LastDiffSha256, "Lowercase hex SHA-256 digest of the diff committed on the last non-empty apply.")
	an.Describe(&s.LastDiffEmpty, "True when the most recent apply was a no-op (session diff was empty).")
}

// Create stages the body and commits when the diff is non-empty.
func (*RawCli) Create(ctx context.Context, req infer.CreateRequest[RawCliArgs]) (infer.CreateResponse[RawCliState], error) {
	cmds, digest, err := prepareRawCli(req.Inputs)
	if err != nil {
		return infer.CreateResponse[RawCliState]{}, err
	}
	state := RawCliState{RawCliArgs: req.Inputs, BodySha256: digest}
	if req.DryRun {
		return infer.CreateResponse[RawCliState]{ID: rawCliID(req.Inputs.Name), Output: state}, nil
	}
	diffSha, empty, err := applyRawCli(ctx, req.Inputs, cmds, "create")
	if err != nil {
		return infer.CreateResponse[RawCliState]{}, fmt.Errorf("create rawCli %s: %w", req.Inputs.Name, err)
	}
	state.LastDiffSha256 = diffSha
	state.LastDiffEmpty = empty
	return infer.CreateResponse[RawCliState]{ID: rawCliID(req.Inputs.Name), Output: state}, nil
}

// Read recomputes the body digest from the stored Args body. The diff
// digest is preserved verbatim from prior state because it reflects a
// historical apply, not the current device.
func (*RawCli) Read(_ context.Context, req infer.ReadRequest[RawCliArgs, RawCliState]) (infer.ReadResponse[RawCliArgs, RawCliState], error) {
	_, digest, err := prepareRawCli(req.Inputs)
	if err != nil {
		return infer.ReadResponse[RawCliArgs, RawCliState]{}, err
	}
	state := req.State
	state.RawCliArgs = req.Inputs
	state.BodySha256 = digest
	return infer.ReadResponse[RawCliArgs, RawCliState]{
		ID:     rawCliID(req.Inputs.Name),
		Inputs: req.Inputs,
		State:  state,
	}, nil
}

// Update re-stages the body and commits only when the diff is non-empty.
func (*RawCli) Update(ctx context.Context, req infer.UpdateRequest[RawCliArgs, RawCliState]) (infer.UpdateResponse[RawCliState], error) {
	cmds, digest, err := prepareRawCli(req.Inputs)
	if err != nil {
		return infer.UpdateResponse[RawCliState]{}, err
	}
	state := RawCliState{RawCliArgs: req.Inputs, BodySha256: digest}
	if req.DryRun {
		return infer.UpdateResponse[RawCliState]{Output: state}, nil
	}
	diffSha, empty, err := applyRawCli(ctx, req.Inputs, cmds, "update")
	if err != nil {
		return infer.UpdateResponse[RawCliState]{}, fmt.Errorf("update rawCli %s: %w", req.Inputs.Name, err)
	}
	state.LastDiffSha256 = diffSha
	state.LastDiffEmpty = empty
	return infer.UpdateResponse[RawCliState]{Output: state}, nil
}

// Delete applies the optional inverse body. When DeleteBody is unset the
// resource is forgotten without touching running-config.
func (*RawCli) Delete(ctx context.Context, req infer.DeleteRequest[RawCliState]) (infer.DeleteResponse, error) {
	if req.State.DeleteBody == nil {
		return infer.DeleteResponse{}, nil
	}
	cmds := canonicalConfigletLines(*req.State.DeleteBody)
	if len(cmds) == 0 {
		return infer.DeleteResponse{}, nil
	}
	if _, _, err := applyRawCli(ctx, req.State.RawCliArgs, cmds, "delete"); err != nil {
		return infer.DeleteResponse{}, fmt.Errorf("delete rawCli %s: %w", req.State.Name, err)
	}
	return infer.DeleteResponse{}, nil
}

// prepareRawCli validates inputs, canonicalises the body, and returns the
// staged command list plus the canonical body digest.
func prepareRawCli(args RawCliArgs) ([]string, string, error) {
	if strings.TrimSpace(args.Name) == "" {
		return nil, "", ErrRawCliNameRequired
	}
	cmds := canonicalConfigletLines(args.Body)
	if len(cmds) == 0 {
		return nil, "", ErrRawCliContentRequired
	}
	sum := sha256.Sum256([]byte(strings.Join(cmds, "\n")))
	return cmds, hex.EncodeToString(sum[:]), nil
}

// applyRawCli stages cmds inside a fresh configuration session. If the
// diff against running-config is empty the session is aborted and the
// helper returns ("", true, nil). Otherwise the session is committed and
// the diff digest is returned with empty=false.
func applyRawCli(ctx context.Context, args RawCliArgs, cmds []string, op string) (string, bool, error) {
	cli, err := newRawCliClient(ctx, args)
	if err != nil {
		return "", false, err
	}
	cfg := config.FromContext(ctx)
	sessName := cfg.SessionPrefix() + "rawcli-" + op + "-" + sanitizeForSession(args.Name)

	sess, err := cli.OpenSession(ctx, sessName)
	if err != nil {
		return "", false, err
	}
	if stageErr := sess.Stage(ctx, cmds); stageErr != nil {
		if abortErr := sess.Abort(ctx); abortErr != nil {
			return "", false, errors.Join(stageErr, abortErr)
		}
		return "", false, stageErr
	}
	diff, diffErr := sess.Diff(ctx)
	if diffErr != nil {
		if abortErr := sess.Abort(ctx); abortErr != nil {
			return "", false, errors.Join(diffErr, abortErr)
		}
		return "", false, diffErr
	}
	if isEmptyDiff(diff) {
		if abortErr := sess.Abort(ctx); abortErr != nil {
			return "", true, abortErr
		}
		return "", true, nil
	}
	if commitErr := sess.Commit(ctx); commitErr != nil {
		return "", false, commitErr
	}
	sum := sha256.Sum256([]byte(diff))
	return hex.EncodeToString(sum[:]), false, nil
}

// isEmptyDiff returns true when the textual session diff describes no
// configuration change. EOS prints a header-only block (e.g. just
// `--- system:/running-config` / `+++ session:…`) when nothing changed;
// stripping diff metadata and whitespace lets a single check cover all
// observed shapes (empty string, header-only, single-newline output).
func isEmptyDiff(diff string) bool {
	for raw := range strings.SplitSeq(diff, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++") {
			continue
		}
		return false
	}
	return true
}

func rawCliID(name string) string { return "rawCli/" + name }

// newRawCliClient is the per-resource client factory for `eos:device:RawCli`.
func newRawCliClient(ctx context.Context, args RawCliArgs) (*eapi.Client, error) {
	cfg := config.FromContext(ctx)
	return cfg.EAPIClient(ctx, args.Host, args.Username, args.Password)
}
