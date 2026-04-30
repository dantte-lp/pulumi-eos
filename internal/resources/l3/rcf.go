package l3

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/pulumi/pulumi-go-provider/infer"

	"github.com/dantte-lp/pulumi-eos/internal/client/eapi"
	"github.com/dantte-lp/pulumi-eos/internal/config"
)

// Sentinel errors specific to Rcf.
var (
	ErrRcfNameRequired    = errors.New("rcf name is required ('default' for the unnamed unit, otherwise a unique unit name)")
	ErrRcfSourceFileEmpty = errors.New("rcf sourceFile is required (e.g. flash:rcf-evpn.txt) — inline code requires gNOI File support and is deferred to S9")
	ErrRcfBadSourcePath   = errors.New("rcf sourceFile must be of the form <storage>:<path> (e.g. flash:rcf-evpn.txt)")
	ErrRcfBadName         = errors.New("rcf name must match [A-Za-z][A-Za-z0-9_-]* (or be 'default' for the unnamed unit)")
)

// rcfSourceFileRe enforces the EOS storage-prefixed path shape EOS
// expects under `code [unit X] source pulled-from <path>`. Verified
// live against cEOS 4.36.0.1F: `flash:rcf-evpn.txt` is accepted;
// bare `rcf-evpn.txt` is not.
var rcfSourceFileRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*:[^\s]+$`)

// rcfUnitNameRe enforces the EOS-accepted RCF unit-name grammar.
var rcfUnitNameRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)

// rcfDefaultName is the sentinel that maps to the unnamed `code` unit
// (rendered without `unit <name>`).
const rcfDefaultName = "default"

// Rcf models an EOS Routing Control Function code unit. RCF is the
// programmable alternative to route-maps; per peer-group, EOS accepts
// either `route-map RM in|out` OR `rcf F() in|out` — not both
// (TOI 15099).
//
// v0 scope: file-reference form only — the resource emits
//
//	router general
//	   control-functions
//	      code [unit <name>] source pulled-from <storage:path>
//
// Inline RCF code is intentionally deferred. eAPI configuration
// sessions parse every staged line as a configure-mode CLI command,
// so the bare `code unit X` mode rejects RCF source lines as
// "Invalid input". The two paths to inline source — uploading the
// RCF text to flash via gNOI File transfer or via SCP/HTTPS to
// `management api file` — both depend on infrastructure that is on
// the S9 roadmap. v0 references a pre-staged file; users push the
// file content out-of-band today.
//
// Source: EOS User Manual §16.8 (Routing Control Functions); TOI
// 15102 (RCF Language and configuration); validated live against
// cEOS 4.36.0.1F per the per-resource verification rule.
type Rcf struct{}

// RcfArgs is the input set.
type RcfArgs struct {
	// Name is the RCF unit identifier (PK). Use the literal string
	// "default" to target the unnamed `code` unit; any other value
	// is rendered as `code unit <name>`.
	Name string `pulumi:"name"`
	// SourceFile is the storage-prefixed path on the device (e.g.
	// `flash:rcf-evpn.txt`). The file is referenced via
	// `code [unit X] source pulled-from <path>`.
	SourceFile string `pulumi:"sourceFile"`

	// Host overrides the provider-level eosUrl host for this resource.
	Host *string `pulumi:"host,optional"`
	// Username overrides the provider-level eosUsername for this resource.
	Username *string `pulumi:"username,optional"`
	// Password overrides the provider-level eosPassword for this resource.
	Password *string `provider:"secret" pulumi:"password,optional"`
}

// RcfState mirrors Args.
type RcfState struct {
	RcfArgs
}

// Annotate documents the resource.
func (r *Rcf) Annotate(a infer.Annotator) {
	a.Describe(&r, "An EOS Routing Control Function code unit. v0 references a pre-staged file on flash; inline-code support follows gNOI File transfer in S9.")
}

// Annotate documents RcfArgs fields.
func (a *RcfArgs) Annotate(an infer.Annotator) {
	an.Describe(&a.Name, "Unit name (PK). Use 'default' for the unnamed `code` unit, or any [A-Za-z][A-Za-z0-9_-]* for a named unit.")
	an.Describe(&a.SourceFile, "Storage-prefixed source path (e.g. flash:rcf-evpn.txt). The file must already exist on the device.")
	an.Describe(&a.Host, "Optional management hostname override.")
	an.Describe(&a.Username, "Optional AAA username override.")
	an.Describe(&a.Password, "Optional AAA password override.")
}

// Annotate is a no-op; State has no extra fields.
func (s *RcfState) Annotate(_ infer.Annotator) {}

// Create configures the RCF unit.
func (*Rcf) Create(ctx context.Context, req infer.CreateRequest[RcfArgs]) (infer.CreateResponse[RcfState], error) {
	if err := validateRcf(req.Inputs); err != nil {
		return infer.CreateResponse[RcfState]{}, err
	}
	state := RcfState{RcfArgs: req.Inputs}
	if req.DryRun {
		return infer.CreateResponse[RcfState]{ID: rcfID(req.Inputs.Name), Output: state}, nil
	}
	if err := applyRcf(ctx, req.Inputs, false); err != nil {
		return infer.CreateResponse[RcfState]{}, fmt.Errorf("create rcf %s: %w", req.Inputs.Name, err)
	}
	return infer.CreateResponse[RcfState]{ID: rcfID(req.Inputs.Name), Output: state}, nil
}

// Read refreshes RCF state from the device.
func (*Rcf) Read(ctx context.Context, req infer.ReadRequest[RcfArgs, RcfState]) (infer.ReadResponse[RcfArgs, RcfState], error) {
	cli, err := newClient(ctx, req.Inputs.Host, req.Inputs.Username, req.Inputs.Password)
	if err != nil {
		return infer.ReadResponse[RcfArgs, RcfState]{}, err
	}
	current, found, err := readRcf(ctx, cli, req.Inputs.Name)
	if err != nil {
		return infer.ReadResponse[RcfArgs, RcfState]{}, err
	}
	if !found {
		return infer.ReadResponse[RcfArgs, RcfState]{}, nil
	}
	state := RcfState{RcfArgs: req.Inputs}
	current.fillState(&state)
	return infer.ReadResponse[RcfArgs, RcfState]{
		ID:     rcfID(req.Inputs.Name),
		Inputs: req.Inputs,
		State:  state,
	}, nil
}

// Update re-applies the RCF unit.
func (*Rcf) Update(ctx context.Context, req infer.UpdateRequest[RcfArgs, RcfState]) (infer.UpdateResponse[RcfState], error) {
	if err := validateRcf(req.Inputs); err != nil {
		return infer.UpdateResponse[RcfState]{}, err
	}
	state := RcfState{RcfArgs: req.Inputs}
	if req.DryRun {
		return infer.UpdateResponse[RcfState]{Output: state}, nil
	}
	if err := applyRcf(ctx, req.Inputs, false); err != nil {
		return infer.UpdateResponse[RcfState]{}, fmt.Errorf("update rcf %s: %w", req.Inputs.Name, err)
	}
	return infer.UpdateResponse[RcfState]{Output: state}, nil
}

// Delete removes the RCF unit.
func (*Rcf) Delete(ctx context.Context, req infer.DeleteRequest[RcfState]) (infer.DeleteResponse, error) {
	if err := applyRcf(ctx, req.State.RcfArgs, true); err != nil {
		return infer.DeleteResponse{}, fmt.Errorf("delete rcf %s: %w", req.State.Name, err)
	}
	return infer.DeleteResponse{}, nil
}

// --- helpers ---------------------------------------------------------------

func validateRcf(args RcfArgs) error {
	if strings.TrimSpace(args.Name) == "" {
		return ErrRcfNameRequired
	}
	if args.Name != rcfDefaultName && !rcfUnitNameRe.MatchString(args.Name) {
		return fmt.Errorf("%w: %q", ErrRcfBadName, args.Name)
	}
	if strings.TrimSpace(args.SourceFile) == "" {
		return ErrRcfSourceFileEmpty
	}
	if !rcfSourceFileRe.MatchString(args.SourceFile) {
		return fmt.Errorf("%w: %q", ErrRcfBadSourcePath, args.SourceFile)
	}
	return nil
}

func rcfID(name string) string { return "rcf/" + name }

func applyRcf(ctx context.Context, args RcfArgs, remove bool) error {
	cli, err := newClient(ctx, args.Host, args.Username, args.Password)
	if err != nil {
		return err
	}
	cfg := config.FromContext(ctx)
	sessName := cfg.SessionPrefix() + "rcf-" + sanitizePrefixListName(args.Name)

	sess, err := cli.OpenSession(ctx, sessName)
	if err != nil {
		return err
	}
	cmds := buildRcfCmds(args, remove)
	if stageErr := sess.Stage(ctx, cmds); stageErr != nil {
		if abortErr := sess.Abort(ctx); abortErr != nil {
			return errors.Join(stageErr, abortErr)
		}
		return stageErr
	}
	return sess.Commit(ctx)
}

// buildRcfCmds renders the staged CLI block.
//
// Render shape (closing `exit` lines exit control-functions and
// router-general modes in turn):
//
//	router general
//	   control-functions
//	      [no] code [unit <name>] [source pulled-from <path>]
//
// `default` Name maps to the unnamed `code` unit.
func buildRcfCmds(args RcfArgs, remove bool) []string {
	cmds := []string{"router general", "control-functions"}
	target := rcfTargetClause(args.Name)
	if remove {
		// Verified live on cEOS 4.36: bare `no code [unit X]` does
		// not remove the unit; the negation form is `no code [unit
		// X] source pulled-from` (without the path argument).
		cmds = append(cmds, "no "+target+" source pulled-from")
	} else {
		cmds = append(cmds, target+" source pulled-from "+args.SourceFile)
	}
	cmds = append(cmds, "exit", "exit")
	return cmds
}

// rcfTargetClause returns the CLI target clause for the given unit
// name — `code` for the unnamed unit, or `code unit <name>` otherwise.
func rcfTargetClause(name string) string {
	if name == rcfDefaultName {
		return "code"
	}
	return "code unit " + name
}

// rcfRow is the parsed live state we care about.
type rcfRow struct {
	Name       string
	SourceFile string
}

// readRcf returns the live RCF state, or (false, nil) when no unit
// with the given name is configured. The running-config section emits:
//
//	router general
//	   control-functions
//	      code source pulled-from <path>
//	      code unit <name> source pulled-from <path> [edited]
//
// The `edited` suffix appears when the on-flash file has been
// modified after the pull-from operation; the parser ignores it so
// drift detection compares only the configured source path.
func readRcf(ctx context.Context, cli *eapi.Client, name string) (rcfRow, bool, error) {
	resp, err := cli.RunCmds(ctx,
		[]string{"show running-config | section router general"},
		"text")
	if err != nil {
		return rcfRow{}, false, err
	}
	if len(resp) == 0 {
		return rcfRow{}, false, nil
	}
	out, _ := resp[0]["output"].(string)
	row, found := parseRcfSection(out, name)
	return row, found, nil
}

// parseRcfSection extracts the RCF unit matching `name` from the
// `router general` section. Exposed for unit tests.
func parseRcfSection(out, name string) (rcfRow, bool) {
	row := rcfRow{Name: name}
	header := "code source pulled-from "
	if name != rcfDefaultName {
		header = "code unit " + name + " source pulled-from "
	}
	for raw := range strings.SplitSeq(out, "\n") {
		line := strings.TrimSpace(raw)
		rest, ok := strings.CutPrefix(line, header)
		if !ok {
			continue
		}
		// Strip a trailing ` edited` flag EOS appends after on-flash
		// file modification.
		if trimmed, hasEdited := strings.CutSuffix(rest, " edited"); hasEdited {
			rest = trimmed
		}
		if rest == "" {
			continue
		}
		row.SourceFile = rest
		return row, true
	}
	return rcfRow{}, false
}

func (r rcfRow) fillState(s *RcfState) {
	if r.SourceFile != "" {
		s.SourceFile = r.SourceFile
	}
}
