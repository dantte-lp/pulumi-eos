package l3

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/pulumi/pulumi-go-provider/infer"

	"github.com/dantte-lp/pulumi-eos/internal/client/eapi"
	"github.com/dantte-lp/pulumi-eos/internal/config"
)

// Sentinel errors specific to AsPathAccessList.
var (
	ErrAsPathListNameRequired = errors.New("asPathAccessList name is required")
	ErrAsPathListEmptyEntries = errors.New("asPathAccessList must have at least one entry")
	ErrAsPathListAction       = errors.New("asPathAccessList entry action must be 'permit' or 'deny'")
	ErrAsPathListRegexEmpty   = errors.New("asPathAccessList entry regex is required")
)

// AsPathAccessList models an EOS BGP AS-path access-list. EOS treats
// the named list as a flat ordered list of permit/deny regex rules.
//
// cEOS 4.36 auto-appends the `any` AS-set type to every entry on
// render. The bare form `ip as-path access-list <name> permit <regex>`
// is accepted on input; running-config emits it back as
// `ip as-path access-list <name> permit <regex> any`. The Read parser
// strips the trailing ` any` so the structured Entries[].Regex field
// matches the user input verbatim.
//
// Source: EOS User Manual §15.6 (BGP routing policy / AS-path filter);
// validated live against cEOS 4.36.0.1F per the per-resource
// verification rule.
type AsPathAccessList struct{}

// AsPathAccessListEntry is one permit/deny rule.
type AsPathAccessListEntry struct {
	// Action is "permit" or "deny".
	Action string `pulumi:"action"`
	// Regex is the AS-path regular expression (e.g. `^65000$`,
	// `_65001_`, `^65002.*$`).
	Regex string `pulumi:"regex"`
}

// Annotate documents AsPathAccessListEntry fields.
func (e *AsPathAccessListEntry) Annotate(an infer.Annotator) {
	an.Describe(&e.Action, "permit or deny.")
	an.Describe(&e.Regex, "AS-path regular expression.")
}

// AsPathAccessListArgs is the input set.
type AsPathAccessListArgs struct {
	Name    string                  `pulumi:"name"`
	Entries []AsPathAccessListEntry `pulumi:"entries"`

	Host     *string `pulumi:"host,optional"`
	Username *string `pulumi:"username,optional"`
	Password *string `provider:"secret"          pulumi:"password,optional"`
}

// AsPathAccessListState mirrors Args.
type AsPathAccessListState struct {
	AsPathAccessListArgs
}

// Annotate documents the resource.
func (r *AsPathAccessList) Annotate(a infer.Annotator) {
	a.Describe(&r, "An EOS BGP AS-path access-list. Composes with eos:l3:RouteMap match as-path.")
}

// Annotate documents AsPathAccessListArgs fields.
func (a *AsPathAccessListArgs) Annotate(an infer.Annotator) {
	an.Describe(&a.Name, "AS-path access-list name (PK).")
	an.Describe(&a.Entries, "Ordered permit/deny entries.")
	an.Describe(&a.Host, "Optional management hostname override.")
	an.Describe(&a.Username, "Optional AAA username override.")
	an.Describe(&a.Password, "Optional AAA password override.")
}

// Annotate is a no-op; State has no extra fields.
func (s *AsPathAccessListState) Annotate(_ infer.Annotator) {}

// Create configures the AS-path access-list.
func (*AsPathAccessList) Create(ctx context.Context, req infer.CreateRequest[AsPathAccessListArgs]) (infer.CreateResponse[AsPathAccessListState], error) {
	if err := validateAsPathAccessList(req.Inputs); err != nil {
		return infer.CreateResponse[AsPathAccessListState]{}, err
	}
	state := AsPathAccessListState{AsPathAccessListArgs: req.Inputs}
	if req.DryRun {
		return infer.CreateResponse[AsPathAccessListState]{ID: asPathAccessListID(req.Inputs.Name), Output: state}, nil
	}
	if err := applyAsPathAccessList(ctx, req.Inputs, false); err != nil {
		return infer.CreateResponse[AsPathAccessListState]{}, fmt.Errorf("create as-path access-list %s: %w", req.Inputs.Name, err)
	}
	return infer.CreateResponse[AsPathAccessListState]{ID: asPathAccessListID(req.Inputs.Name), Output: state}, nil
}

// Read refreshes the AS-path access-list state from the device.
func (*AsPathAccessList) Read(ctx context.Context, req infer.ReadRequest[AsPathAccessListArgs, AsPathAccessListState]) (infer.ReadResponse[AsPathAccessListArgs, AsPathAccessListState], error) {
	cli, err := newClient(ctx, req.Inputs.Host, req.Inputs.Username, req.Inputs.Password)
	if err != nil {
		return infer.ReadResponse[AsPathAccessListArgs, AsPathAccessListState]{}, err
	}
	current, found, err := readAsPathAccessList(ctx, cli, req.Inputs.Name)
	if err != nil {
		return infer.ReadResponse[AsPathAccessListArgs, AsPathAccessListState]{}, err
	}
	if !found {
		return infer.ReadResponse[AsPathAccessListArgs, AsPathAccessListState]{}, nil
	}
	state := AsPathAccessListState{AsPathAccessListArgs: req.Inputs}
	current.fillState(&state)
	return infer.ReadResponse[AsPathAccessListArgs, AsPathAccessListState]{
		ID:     asPathAccessListID(req.Inputs.Name),
		Inputs: req.Inputs,
		State:  state,
	}, nil
}

// Update re-applies the list (negate-then-rebuild).
func (*AsPathAccessList) Update(ctx context.Context, req infer.UpdateRequest[AsPathAccessListArgs, AsPathAccessListState]) (infer.UpdateResponse[AsPathAccessListState], error) {
	if err := validateAsPathAccessList(req.Inputs); err != nil {
		return infer.UpdateResponse[AsPathAccessListState]{}, err
	}
	state := AsPathAccessListState{AsPathAccessListArgs: req.Inputs}
	if req.DryRun {
		return infer.UpdateResponse[AsPathAccessListState]{Output: state}, nil
	}
	if err := applyAsPathAccessList(ctx, req.Inputs, false); err != nil {
		return infer.UpdateResponse[AsPathAccessListState]{}, fmt.Errorf("update as-path access-list %s: %w", req.Inputs.Name, err)
	}
	return infer.UpdateResponse[AsPathAccessListState]{Output: state}, nil
}

// Delete removes the AS-path access-list.
func (*AsPathAccessList) Delete(ctx context.Context, req infer.DeleteRequest[AsPathAccessListState]) (infer.DeleteResponse, error) {
	if err := applyAsPathAccessList(ctx, req.State.AsPathAccessListArgs, true); err != nil {
		return infer.DeleteResponse{}, fmt.Errorf("delete as-path access-list %s: %w", req.State.Name, err)
	}
	return infer.DeleteResponse{}, nil
}

// --- helpers ---------------------------------------------------------------

func validateAsPathAccessList(args AsPathAccessListArgs) error {
	if strings.TrimSpace(args.Name) == "" {
		return ErrAsPathListNameRequired
	}
	if len(args.Entries) == 0 {
		return ErrAsPathListEmptyEntries
	}
	for i := range args.Entries {
		e := &args.Entries[i]
		if e.Action != "permit" && e.Action != "deny" {
			return fmt.Errorf("%w: got %q", ErrAsPathListAction, e.Action)
		}
		if strings.TrimSpace(e.Regex) == "" {
			return ErrAsPathListRegexEmpty
		}
	}
	return nil
}

func asPathAccessListID(name string) string { return "as-path-access-list/" + name }

func applyAsPathAccessList(ctx context.Context, args AsPathAccessListArgs, remove bool) error {
	cli, err := newClient(ctx, args.Host, args.Username, args.Password)
	if err != nil {
		return err
	}
	cfg := config.FromContext(ctx)
	sessName := cfg.SessionPrefix() + "aspath-" + sanitizePrefixListName(args.Name)

	sess, err := cli.OpenSession(ctx, sessName)
	if err != nil {
		return err
	}
	cmds := buildAsPathAccessListCmds(args, remove)
	if stageErr := sess.Stage(ctx, cmds); stageErr != nil {
		if abortErr := sess.Abort(ctx); abortErr != nil {
			return errors.Join(stageErr, abortErr)
		}
		return stageErr
	}
	return sess.Commit(ctx)
}

// buildAsPathAccessListCmds renders the staged CLI block.
//
// `no ip as-path access-list <name>` clears all entries; the per-line
// rebuild emits one `permit|deny <regex>` per entry. EOS appends the
// `any` AS-set type token automatically on render — we do not emit
// it ourselves.
func buildAsPathAccessListCmds(args AsPathAccessListArgs, remove bool) []string {
	if remove {
		return []string{"no ip as-path access-list " + args.Name}
	}
	cmds := []string{"no ip as-path access-list " + args.Name}
	for _, e := range args.Entries {
		cmds = append(cmds, "ip as-path access-list "+args.Name+" "+e.Action+" "+e.Regex)
	}
	return cmds
}

// asPathAccessListRow is the parsed live state we care about.
type asPathAccessListRow struct {
	Name    string
	Entries []AsPathAccessListEntry
}

// readAsPathAccessList pulls the named list out of running-config.
// Single-word EOS pipe-grep limitation applies — we match on
// `as-path` and filter `ip as-path access-list <name> ` lines
// client-side.
func readAsPathAccessList(ctx context.Context, cli *eapi.Client, name string) (asPathAccessListRow, bool, error) {
	resp, err := cli.RunCmds(ctx,
		[]string{"show running-config | grep as-path"},
		"text")
	if err != nil {
		return asPathAccessListRow{}, false, err
	}
	if len(resp) == 0 {
		return asPathAccessListRow{}, false, nil
	}
	out, _ := resp[0]["output"].(string)
	row, found := parseAsPathAccessListLines(out, name)
	return row, found, nil
}

// parseAsPathAccessListLines walks the grepped output and assembles
// the row matching `name`. EOS appends ` any` to every emitted entry —
// the parser strips that suffix so the structured Entries[].Regex
// field stays equal to user input.
func parseAsPathAccessListLines(out, name string) (asPathAccessListRow, bool) {
	row := asPathAccessListRow{Name: name}
	prefix := "ip as-path access-list " + name + " "
	found := false
	for raw := range strings.SplitSeq(out, "\n") {
		line := strings.TrimSpace(raw)
		rest, ok := strings.CutPrefix(line, prefix)
		if !ok {
			continue
		}
		entry, ok := parseAsPathAccessListEntryRest(rest)
		if !ok {
			continue
		}
		row.Entries = append(row.Entries, entry)
		found = true
	}
	if !found {
		return asPathAccessListRow{}, false
	}
	return row, true
}

// parseAsPathAccessListEntryRest parses `permit|deny <regex> [any]`
// after the header has been cut.
func parseAsPathAccessListEntryRest(rest string) (AsPathAccessListEntry, bool) {
	parts := strings.SplitN(rest, " ", 2)
	if len(parts) != 2 {
		return AsPathAccessListEntry{}, false
	}
	action := parts[0]
	if action != "permit" && action != "deny" {
		return AsPathAccessListEntry{}, false
	}
	body := parts[1]
	// Strip the trailing ` any` AS-set type EOS auto-renders.
	if trimmed, hasAny := strings.CutSuffix(body, " any"); hasAny {
		body = trimmed
	}
	if body == "" {
		return AsPathAccessListEntry{}, false
	}
	return AsPathAccessListEntry{Action: action, Regex: body}, true
}

func (r asPathAccessListRow) fillState(s *AsPathAccessListState) {
	if len(r.Entries) > 0 {
		s.Entries = append([]AsPathAccessListEntry(nil), r.Entries...)
	}
}
