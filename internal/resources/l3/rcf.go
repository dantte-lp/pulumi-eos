package l3

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/pulumi/pulumi-go-provider/infer"

	"github.com/dantte-lp/pulumi-eos/internal/client/eapi"
	"github.com/dantte-lp/pulumi-eos/internal/config"
)

// Sentinel errors specific to Rcf.
var (
	ErrRcfNameRequired      = errors.New("rcf name is required ('default' for the unnamed unit, otherwise a unique unit name)")
	ErrRcfBadName           = errors.New("rcf name must match [A-Za-z][A-Za-z0-9_-]* (or be 'default' for the unnamed unit)")
	ErrRcfDeliveryRequired  = errors.New("rcf requires exactly one of code | sourceFile | sourceUrl")
	ErrRcfDeliveryConflict  = errors.New("rcf code | sourceFile | sourceUrl are mutually exclusive — set only one")
	ErrRcfBadSourcePath     = errors.New("rcf sourceFile must be of the form <storage>:<path> (e.g. flash:rcf-evpn.txt)")
	ErrRcfBadSourceURL      = errors.New("rcf sourceUrl must be a valid http(s)://... or ftp://... URL EOS can pull from")
	ErrRcfCodeMissingTrailN = errors.New("rcf code body must end with a newline (EOS expects a complete line before the EOF terminator)")
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

// rcfPullSchemes enumerates the URL schemes EOS' `pull unit X replace
// <url>` accepts. Verified against EOS User Manual §16.8.3.
var rcfPullSchemes = map[string]struct{}{
	"http":  {},
	"https": {},
	"ftp":   {},
	"tftp":  {},
	"scp":   {},
}

// Rcf models an EOS Routing Control Function code unit. RCF is the
// programmable alternative to route-maps; per peer-group, EOS accepts
// either `route-map RM in|out` OR `rcf F() in|out` — not both
// (TOI 15099).
//
// v1 surface — three mutually-exclusive delivery modes:
//
//  1. **Code (Pulumi-native)** — inline RCF source string. Pushed via
//     the eAPI complex-command form (`{"cmd": "code unit X",
//     "input": "<source>\nEOF\n"}`) per EOS Command API Guide §1.2.3.
//     Per TOI 19238 ("eAPI with RCF") EOS auto-compiles and commits
//     the code on session exit. This is the canonical Pulumi
//     workflow: the IaC program owns the source code.
//
//  2. **SourceFile** — reference a file already present on the
//     device's filesystem (e.g. `flash:rcf-evpn.txt`). Renders as
//     `code [unit X] source pulled-from <path>`. Useful when the
//     RCF code is delivered out-of-band (SCP, ZTP, image bundle).
//
//  3. **SourceUrl** — `pull unit X replace <url>` against an
//     external HTTP/HTTPS/FTP/TFTP/SCP endpoint. EOS fetches the
//     content into running-config and auto-commits. Useful for
//     environments with a central RCF distribution endpoint.
//
// Source: EOS User Manual §16.8 (Routing Control Functions); TOI
// 15102 (RCF Language and configuration); TOI 19238 (eAPI with RCF);
// EOS Command API Guide §1.2.3 (Command Specification — `input`
// field); validated live against cEOS 4.36.0.1F per the per-resource
// verification rule.
type Rcf struct{}

// RcfArgs is the input set.
type RcfArgs struct {
	// Name is the RCF unit identifier (PK). Use the literal string
	// "default" to target the unnamed `code` unit; any other value
	// is rendered as `code unit <name>`.
	Name string `pulumi:"name"`
	// Code is the inline RCF source string. Pushed via the eAPI
	// complex-command form. Must end with a newline; the resource
	// appends the `EOF` terminator automatically. Mutually exclusive
	// with `SourceFile` and `SourceUrl`.
	Code *string `pulumi:"code,optional"`
	// SourceFile references a file already on the device's flash
	// (e.g. `flash:rcf-evpn.txt`). Mutually exclusive with `Code`
	// and `SourceUrl`.
	SourceFile *string `pulumi:"sourceFile,optional"`
	// SourceUrl is a fetchable URL EOS pulls the source from. Accepts
	// http / https / ftp / tftp / scp schemes. Mutually exclusive
	// with `Code` and `SourceFile`.
	SourceUrl *string `pulumi:"sourceUrl,optional"`

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
	a.Describe(&r, "An EOS Routing Control Function code unit. Three delivery modes: inline `code` (Pulumi-native via eAPI input field), `sourceFile` (pre-staged on flash), or `sourceUrl` (pull from HTTP/HTTPS/FTP/TFTP/SCP).")
}

// Annotate documents RcfArgs fields.
func (a *RcfArgs) Annotate(an infer.Annotator) {
	an.Describe(&a.Name, "Unit name (PK). Use 'default' for the unnamed `code` unit, or any [A-Za-z][A-Za-z0-9_-]* for a named unit.")
	an.Describe(&a.Code, "Inline RCF source. Pushed via eAPI complex-command form. Must end with a newline.")
	an.Describe(&a.SourceFile, "Storage-prefixed source path (e.g. flash:rcf-evpn.txt). The file must already exist on the device.")
	an.Describe(&a.SourceUrl, "URL EOS pulls the source from (http/https/ftp/tftp/scp).")
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

// rcfDeliveryCount returns the number of delivery fields set.
func rcfDeliveryCount(args RcfArgs) int {
	n := 0
	if args.Code != nil && *args.Code != "" {
		n++
	}
	if args.SourceFile != nil && *args.SourceFile != "" {
		n++
	}
	if args.SourceUrl != nil && *args.SourceUrl != "" {
		n++
	}
	return n
}

func validateRcf(args RcfArgs) error {
	if strings.TrimSpace(args.Name) == "" {
		return ErrRcfNameRequired
	}
	if args.Name != rcfDefaultName && !rcfUnitNameRe.MatchString(args.Name) {
		return fmt.Errorf("%w: %q", ErrRcfBadName, args.Name)
	}
	switch rcfDeliveryCount(args) {
	case 0:
		return ErrRcfDeliveryRequired
	case 1:
		// continue
	default:
		return ErrRcfDeliveryConflict
	}
	if args.SourceFile != nil && *args.SourceFile != "" {
		if !rcfSourceFileRe.MatchString(*args.SourceFile) {
			return fmt.Errorf("%w: %q", ErrRcfBadSourcePath, *args.SourceFile)
		}
	}
	if args.SourceUrl != nil && *args.SourceUrl != "" {
		if err := validateRcfSourceURL(*args.SourceUrl); err != nil {
			return err
		}
	}
	if args.Code != nil && *args.Code != "" && !strings.HasSuffix(*args.Code, "\n") {
		return ErrRcfCodeMissingTrailN
	}
	return nil
}

// validateRcfSourceURL accepts http/https/ftp/tftp/scp URLs.
func validateRcfSourceURL(s string) error {
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return fmt.Errorf("%w: %q", ErrRcfBadSourceURL, s)
	}
	if _, ok := rcfPullSchemes[strings.ToLower(u.Scheme)]; !ok {
		return fmt.Errorf("%w: scheme %q", ErrRcfBadSourceURL, u.Scheme)
	}
	return nil
}

func rcfID(name string) string { return "rcf/" + name }

// rcfTargetClause returns the CLI target clause for the given unit
// name — `code` for the unnamed unit, or `code unit <name>` otherwise.
func rcfTargetClause(name string) string {
	if name == rcfDefaultName {
		return "code"
	}
	return "code unit " + name
}

// rcfPullClause returns the `pull` verb form for the URL delivery
// mode — `pull` for the unnamed unit, or `pull unit <name>` otherwise.
func rcfPullClause(name string) string {
	if name == rcfDefaultName {
		return "pull"
	}
	return "pull unit " + name
}

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
	if remove || (args.Code == nil || *args.Code == "") {
		// Plain-CLI path: delete + SourceFile + SourceUrl modes do
		// not need the eAPI rich `input` field.
		cmds := buildRcfPlainCmds(args, remove)
		if stageErr := sess.Stage(ctx, cmds); stageErr != nil {
			if abortErr := sess.Abort(ctx); abortErr != nil {
				return errors.Join(stageErr, abortErr)
			}
			return stageErr
		}
		return sess.Commit(ctx)
	}
	// Inline-code path: stream the RCF source via the eAPI rich form.
	rich := buildRcfRichCmds(args)
	if stageErr := sess.StageRich(ctx, rich); stageErr != nil {
		if abortErr := sess.Abort(ctx); abortErr != nil {
			return errors.Join(stageErr, abortErr)
		}
		return stageErr
	}
	return sess.Commit(ctx)
}

// buildRcfPlainCmds renders the plain-CLI staged block for the
// SourceFile / SourceUrl / delete paths.
//
// SourceFile delete form:
//
//	router general
//	   control-functions
//	      no code [unit X] source pulled-from
//
// SourceUrl form (apply): wraps the same delete in `pull unit X replace
// <url>` — EOS auto-compiles and commits on exit per TOI 19238.
func buildRcfPlainCmds(args RcfArgs, remove bool) []string {
	cmds := []string{"router general", "control-functions"}
	target := rcfTargetClause(args.Name)
	switch {
	case remove:
		// Verified live on cEOS 4.36: the unit may have been created
		// in either delivery mode (file-reference vs inline-code),
		// each with its own negation form. Both `no` forms are
		// idempotent on a non-existent unit, so emitting both
		// guarantees clean removal regardless of the original form.
		cmds = append(cmds,
			"no "+target+" source pulled-from",
			"no "+target,
		)
	case args.SourceFile != nil && *args.SourceFile != "":
		cmds = append(cmds, target+" source pulled-from "+*args.SourceFile)
	case args.SourceUrl != nil && *args.SourceUrl != "":
		// `pull` is the action verb; EOS fetches the source and
		// stages a `code unit X` block in the session-config. Auto-
		// compile + commit kicks in when the session-config exits.
		cmds = append(cmds, rcfPullClause(args.Name)+" replace "+*args.SourceUrl)
	}
	cmds = append(cmds, "exit", "exit")
	return cmds
}

// buildRcfRichCmds renders the rich (eAPI `input` field) staged block
// for the inline Code path.
//
// The complex command shape EOS accepts:
//
//	{"cmd": "code unit X", "input": "<rcf source>\nEOF\n"}
//
// The wrapping `router general` / `control-functions` modes are
// plain CLI; only the body-bearing line uses the rich form.
func buildRcfRichCmds(args RcfArgs) []eapi.Command {
	body := *args.Code
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	body += "EOF\n"
	return []eapi.Command{
		{Cmd: "router general"},
		{Cmd: "control-functions"},
		{Cmd: rcfTargetClause(args.Name), Input: body},
		{Cmd: "exit"},
		{Cmd: "exit"},
	}
}

// rcfRow is the parsed live state we care about. v1 captures both
// inline code and source-pulled-from references so drift detection
// works across all three delivery modes.
type rcfRow struct {
	Name       string
	Code       string // present when EOS stored inline RCF source
	SourceFile string // present when the unit references a file
}

// readRcf returns the live RCF state, or (false, nil) when no unit
// with the given name is configured. v1 reads the named (or default)
// unit and its inline body if present.
//
// EOS' `show running-config | section router general` emits one of:
//
//	code [unit X]
//	   <inline body>
//	   EOF
//
// or
//
//	code [unit X] source pulled-from <storage:path> [edited]
//
// depending on which delivery mode was used.
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
// `router general` section. Walks the indented block under the unit
// header and reconstructs either the inline body or the source path.
func parseRcfSection(out, name string) (rcfRow, bool) {
	row := rcfRow{Name: name}
	header := "code"
	if name != rcfDefaultName {
		header = "code unit " + name
	}
	lines := strings.Split(out, "\n")
	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		if !rcfHeaderMatch(line, header) {
			continue
		}
		// Source-pulled-from form: header + source-pulled-from on the
		// same line.
		if rest, ok := strings.CutPrefix(line, header+" source pulled-from "); ok {
			if trimmed, hasEdited := strings.CutSuffix(rest, " edited"); hasEdited {
				rest = trimmed
			}
			row.SourceFile = rest
			return row, true
		}
		// Inline-code form: header on its own line, body follows in
		// indented lines until `EOF`.
		if line == header {
			row.Code = collectInlineBody(lines[i+1:])
			return row, true
		}
	}
	return rcfRow{}, false
}

// rcfHeaderMatch reports whether `line` is the header for `header`.
// Exact match and any prefix that adds ` source ...` count as a hit;
// the caller's CutPrefix decides which form was used.
func rcfHeaderMatch(line, header string) bool {
	return line == header || strings.HasPrefix(line, header+" ")
}

// collectInlineBody walks the indented body lines after the header
// until a line equal to `EOF` (after trim) marks the end. Returns
// the body with original line breaks but with the leading indentation
// removed so the value matches what users feed into Code.
func collectInlineBody(rest []string) string {
	var b strings.Builder
	indent := ""
	for i, raw := range rest {
		line := strings.TrimSpace(raw)
		if line == "EOF" {
			break
		}
		// Lock onto the first body line's indentation so we strip the
		// same amount on every line.
		if i == 0 {
			indent = leadingWhitespace(raw)
		}
		stripped, _ := strings.CutPrefix(raw, indent)
		b.WriteString(stripped)
		b.WriteByte('\n')
	}
	return b.String()
}

// leadingWhitespace returns the prefix of `s` consisting solely of
// space / tab characters.
func leadingWhitespace(s string) string {
	for i := range len(s) {
		if s[i] != ' ' && s[i] != '\t' {
			return s[:i]
		}
	}
	return s
}

func (r rcfRow) fillState(s *RcfState) {
	if r.Code != "" {
		v := r.Code
		s.Code = &v
	}
	if r.SourceFile != "" {
		v := r.SourceFile
		s.SourceFile = &v
	}
}
