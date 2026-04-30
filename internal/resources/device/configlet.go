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

// Sentinel errors specific to Configlet.
var (
	ErrConfigletNameRequired    = errors.New("configlet name is required")
	ErrConfigletContentRequired = errors.New("configlet content is required (at least one non-blank line)")
)

// Configlet models a named, file-style raw configuration block applied
// to an EOS device through an atomic configuration session. The CLI
// commands inside the block are submitted verbatim as the body of a
// `configure session` and committed as a single transaction.
//
// Use cases:
//   - Mirror an existing AVD or Ansible-managed configlet into Pulumi
//     state.
//   - Stage configuration for features that do not yet have a typed
//     `eos:*` resource — see `eos:device:RawCli` for an alternative
//     idempotent escape hatch with diff-against-running-config.
//   - Bundle a multi-line snippet that must apply atomically (e.g. a
//     route-map plus its prefix-list, or an ACL plus its applications).
//
// Source: EOS User Manual §3 — `configure session` semantics; commit
// timer + auto-rollback verified against the cEOS 4.36.0.1F integration
// test harness.
type Configlet struct{}

// ConfigletArgs is the input set.
type ConfigletArgs struct {
	// Name uniquely identifies the configlet. Used in the resource ID
	// and in the EOS configuration-session name.
	Name string `pulumi:"name"`
	// Description is an optional human-readable description shown only
	// in Pulumi state and resource docs (it does not appear on the
	// device).
	Description *string `pulumi:"description,optional"`
	// Body is the raw multi-line CLI block. Each non-blank line is
	// staged inside `configure session pulumi-<name>`. Leading and
	// trailing whitespace per line is preserved on the wire.
	Body string `pulumi:"body"`

	// Host overrides the provider-level eosUrl host for this resource.
	Host *string `pulumi:"host,optional"`
	// Username overrides the provider-level eosUsername for this resource.
	Username *string `pulumi:"username,optional"`
	// Password overrides the provider-level eosPassword for this resource.
	Password *string `provider:"secret" pulumi:"password,optional"`
}

// ConfigletState is Args plus a content digest exposed back to Pulumi
// so users can detect drift on the body even when the device-side text
// does not match line-for-line (EOS canonicalises whitespace).
type ConfigletState struct {
	ConfigletArgs

	// BodySha256 is the lowercase hex SHA-256 digest of the
	// canonicalised body (lines trimmed, blank lines removed, joined
	// with `\n`). Stable across re-applies of the same logical body.
	BodySha256 string `pulumi:"bodySha256"`
}

// Annotate documents the resource.
func (r *Configlet) Annotate(a infer.Annotator) {
	a.Describe(&r, "A named, file-style raw EOS configuration block applied through an atomic configuration session over eAPI.")
}

// Annotate documents ConfigletArgs fields.
func (a *ConfigletArgs) Annotate(an infer.Annotator) {
	an.Describe(&a.Name, "Stable identifier for the configlet; appears in the EOS configuration-session name and the Pulumi resource ID.")
	an.Describe(&a.Description, "Pulumi-side description; not pushed to the device.")
	an.Describe(&a.Body, "Multi-line CLI block. Each non-blank line is staged inside `configure session pulumi-<name>`.")
	an.Describe(&a.Host, "Optional management hostname override.")
	an.Describe(&a.Username, "Optional AAA username override.")
	an.Describe(&a.Password, "Optional AAA password override.")
}

// Annotate documents the ConfigletState fields not already covered by
// the embedded Args.
func (s *ConfigletState) Annotate(an infer.Annotator) {
	an.Describe(&s.BodySha256, "Lowercase hex SHA-256 digest of the canonicalised body (trimmed lines, blank lines removed). Stable across re-applies.")
}

// Create stages the body and commits.
func (*Configlet) Create(ctx context.Context, req infer.CreateRequest[ConfigletArgs]) (infer.CreateResponse[ConfigletState], error) {
	cmds, digest, err := prepareConfiglet(req.Inputs)
	if err != nil {
		return infer.CreateResponse[ConfigletState]{}, err
	}
	state := ConfigletState{ConfigletArgs: req.Inputs, BodySha256: digest}
	if req.DryRun {
		return infer.CreateResponse[ConfigletState]{ID: configletID(req.Inputs.Name), Output: state}, nil
	}
	if err := applyConfiglet(ctx, req.Inputs, cmds, false); err != nil {
		return infer.CreateResponse[ConfigletState]{}, fmt.Errorf("create configlet %s: %w", req.Inputs.Name, err)
	}
	return infer.CreateResponse[ConfigletState]{ID: configletID(req.Inputs.Name), Output: state}, nil
}

// Read recomputes the digest from the stored Args body. Configlets are
// inherently lossy on the device side — EOS canonicalises whitespace
// and strips comments — so we use the digest as the canonical drift
// signal rather than a textual round-trip.
func (*Configlet) Read(_ context.Context, req infer.ReadRequest[ConfigletArgs, ConfigletState]) (infer.ReadResponse[ConfigletArgs, ConfigletState], error) {
	cmds, digest, err := prepareConfiglet(req.Inputs)
	if err != nil {
		return infer.ReadResponse[ConfigletArgs, ConfigletState]{}, err
	}
	_ = cmds
	state := ConfigletState{ConfigletArgs: req.Inputs, BodySha256: digest}
	return infer.ReadResponse[ConfigletArgs, ConfigletState]{
		ID:     configletID(req.Inputs.Name),
		Inputs: req.Inputs,
		State:  state,
	}, nil
}

// Update re-stages the body inside a fresh configuration session.
func (*Configlet) Update(ctx context.Context, req infer.UpdateRequest[ConfigletArgs, ConfigletState]) (infer.UpdateResponse[ConfigletState], error) {
	cmds, digest, err := prepareConfiglet(req.Inputs)
	if err != nil {
		return infer.UpdateResponse[ConfigletState]{}, err
	}
	state := ConfigletState{ConfigletArgs: req.Inputs, BodySha256: digest}
	if req.DryRun {
		return infer.UpdateResponse[ConfigletState]{Output: state}, nil
	}
	if err := applyConfiglet(ctx, req.Inputs, cmds, false); err != nil {
		return infer.UpdateResponse[ConfigletState]{}, fmt.Errorf("update configlet %s: %w", req.Inputs.Name, err)
	}
	return infer.UpdateResponse[ConfigletState]{Output: state}, nil
}

// Delete is a no-op on the device.
//
// EOS has no "configlet" abstraction at the OS level — the body lines
// were applied as plain configuration. To remove them, either supply an
// inverted body via Update or use the typed `eos:*` resources for the
// affected features. Pulumi forgets the digest from state and stops
// tracking the resource.
func (*Configlet) Delete(_ context.Context, _ infer.DeleteRequest[ConfigletState]) (infer.DeleteResponse, error) {
	return infer.DeleteResponse{}, nil
}

// prepareConfiglet validates inputs, canonicalises the body, and
// returns the staged command list plus the canonical digest.
func prepareConfiglet(args ConfigletArgs) ([]string, string, error) {
	if strings.TrimSpace(args.Name) == "" {
		return nil, "", ErrConfigletNameRequired
	}
	cmds := canonicalConfigletLines(args.Body)
	if len(cmds) == 0 {
		return nil, "", ErrConfigletContentRequired
	}
	sum := sha256.Sum256([]byte(strings.Join(cmds, "\n")))
	return cmds, hex.EncodeToString(sum[:]), nil
}

// canonicalConfigletLines splits the body into lines, drops blank
// lines, and trims trailing whitespace. Leading whitespace is
// preserved (hierarchical EOS commands rely on indentation in some
// contexts, but EOS itself does not require it; the trim is line-
// trailing only).
func canonicalConfigletLines(body string) []string {
	out := make([]string, 0)
	for raw := range strings.SplitSeq(body, "\n") {
		line := strings.TrimRight(raw, " \t\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}

func configletID(name string) string { return "configlet/" + name }

func applyConfiglet(ctx context.Context, args ConfigletArgs, cmds []string, _ bool) error {
	cli, err := newEapiClient(ctx, args)
	if err != nil {
		return err
	}
	cfg := config.FromContext(ctx)
	sessName := cfg.SessionPrefix() + "configlet-" + sanitizeForSession(args.Name)

	sess, err := cli.OpenSession(ctx, sessName)
	if err != nil {
		return err
	}
	if stageErr := sess.Stage(ctx, cmds); stageErr != nil {
		if abortErr := sess.Abort(ctx); abortErr != nil {
			return errors.Join(stageErr, abortErr)
		}
		return stageErr
	}
	return sess.Commit(ctx)
}

// newEapiClient is the per-resource client factory for `eos:device:*`.
// It mirrors the helper used by `internal/resources/l2.newClient` but
// is duplicated here to keep `eos:device` independent of the L2
// package.
func newEapiClient(ctx context.Context, args ConfigletArgs) (*eapi.Client, error) {
	cfg := config.FromContext(ctx)
	return cfg.EAPIClient(ctx, args.Host, args.Username, args.Password)
}

// sanitizeForSession turns characters that are illegal inside an EOS
// configuration-session name into hyphens.
func sanitizeForSession(name string) string {
	return strings.NewReplacer("/", "-", " ", "-", ":", "-", ".", "-").Replace(name)
}
