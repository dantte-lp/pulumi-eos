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

// Sentinel errors specific to ExtCommunityList.
var (
	ErrExtCommunityListNameRequired = errors.New("extCommunityList name is required")
	ErrExtCommunityListTypeInvalid  = errors.New("extCommunityList listType must be 'standard' or 'regexp'")
	ErrExtCommunityListEmptyEntries = errors.New("extCommunityList must have at least one entry")
	ErrExtCommunityListAction       = errors.New("extCommunityList entry action must be 'permit' or 'deny'")
	ErrExtCommunityListValueEmpty   = errors.New("extCommunityList entry value is required")
	ErrExtCommunityListEntryType    = errors.New("extCommunityList standard entry type must be 'rt' or 'soo'")
	ErrExtCommunityListStdValue     = errors.New("extCommunityList standard entry value must be aa:nn or A.B.C.D:nn")
	ErrExtCommunityListRegexpHasTyp = errors.New("extCommunityList regexp entry must not set type — the type prefix is part of the regex")
)

// extCommunityValueRe matches the body of a standard ext-community
// entry: `aa:nn` or `A.B.C.D:nn`. Range checks are not enforced — EOS
// itself rejects out-of-range values; we just shape-check.
var extCommunityValueRe = regexp.MustCompile(`^([0-9]+(\.[0-9]+){0,3}):[0-9]+$`)

// extCommunityValidEntryType enumerates the type prefixes EOS accepts
// in a standard ext-community-list entry. v0 covers RT (route-target)
// and SOO (site-of-origin); other types (lbw, encap, ...) follow when
// consumers require them.
var extCommunityValidEntryType = map[string]struct{}{
	"rt":  {},
	"soo": {},
}

// ExtCommunityList models an EOS BGP ext-community-list. Per cEOS 4.36
// the regex form uses the `regexp` keyword (mirrors CommunityList's
// EOS User Manual vs. live-device divergence). Standard entries carry
// an explicit type prefix (`rt` / `soo`) which is part of the EOS CLI
// shape; regexp entries do not — the type prefix is captured inside
// the regex string.
//
// Source: EOS User Manual §15.5 (ext-community-list); TOI 13855
// (4-octet AS-specific ext-communities); validated live against cEOS
// 4.36.0.1F per the per-resource verification rule.
type ExtCommunityList struct{}

// ExtCommunityListEntry is one permit/deny rule.
type ExtCommunityListEntry struct {
	// Action is "permit" or "deny".
	Action string `pulumi:"action"`
	// Type is "rt" or "soo". Required for standard lists; rejected
	// for regexp lists.
	Type *string `pulumi:"type,optional"`
	// Value is the matched ext-community body for standard lists
	// (`aa:nn` or `A.B.C.D:nn`) or the regex for regexp lists.
	Value string `pulumi:"value"`
}

// Annotate documents ExtCommunityListEntry fields.
func (e *ExtCommunityListEntry) Annotate(an infer.Annotator) {
	an.Describe(&e.Action, "permit or deny.")
	an.Describe(&e.Type, "Type prefix: rt or soo. Required for standard lists; not allowed for regexp lists.")
	an.Describe(&e.Value, "Standard: aa:nn or A.B.C.D:nn. Regexp: a regular expression.")
}

// ExtCommunityListArgs is the input set.
type ExtCommunityListArgs struct {
	Name     string                  `pulumi:"name"`
	ListType *string                 `pulumi:"listType,optional"`
	Entries  []ExtCommunityListEntry `pulumi:"entries"`

	Host     *string `pulumi:"host,optional"`
	Username *string `pulumi:"username,optional"`
	Password *string `provider:"secret"          pulumi:"password,optional"`
}

// ExtCommunityListState mirrors Args.
type ExtCommunityListState struct {
	ExtCommunityListArgs
}

// Annotate documents the resource.
func (r *ExtCommunityList) Annotate(a infer.Annotator) {
	a.Describe(&r, "An EOS BGP extended-community-list (standard or regexp). Composes with eos:l3:RouteMap match extcommunity.")
}

// Annotate documents ExtCommunityListArgs fields.
func (a *ExtCommunityListArgs) Annotate(an infer.Annotator) {
	an.Describe(&a.Name, "Ext-community-list name (PK).")
	an.Describe(&a.ListType, "List type: standard (default) or regexp.")
	an.Describe(&a.Entries, "Ordered permit/deny entries.")
	an.Describe(&a.Host, "Optional management hostname override.")
	an.Describe(&a.Username, "Optional AAA username override.")
	an.Describe(&a.Password, "Optional AAA password override.")
}

// Annotate is a no-op; State has no extra fields.
func (s *ExtCommunityListState) Annotate(_ infer.Annotator) {}

// Create configures the ext-community-list.
func (*ExtCommunityList) Create(ctx context.Context, req infer.CreateRequest[ExtCommunityListArgs]) (infer.CreateResponse[ExtCommunityListState], error) {
	if err := validateExtCommunityList(req.Inputs); err != nil {
		return infer.CreateResponse[ExtCommunityListState]{}, err
	}
	state := ExtCommunityListState{ExtCommunityListArgs: req.Inputs}
	if req.DryRun {
		return infer.CreateResponse[ExtCommunityListState]{ID: extCommunityListID(req.Inputs.Name), Output: state}, nil
	}
	if err := applyExtCommunityList(ctx, req.Inputs, false); err != nil {
		return infer.CreateResponse[ExtCommunityListState]{}, fmt.Errorf("create ext-community-list %s: %w", req.Inputs.Name, err)
	}
	return infer.CreateResponse[ExtCommunityListState]{ID: extCommunityListID(req.Inputs.Name), Output: state}, nil
}

// Read refreshes ext-community-list state from the device.
func (*ExtCommunityList) Read(ctx context.Context, req infer.ReadRequest[ExtCommunityListArgs, ExtCommunityListState]) (infer.ReadResponse[ExtCommunityListArgs, ExtCommunityListState], error) {
	cli, err := newClient(ctx, req.Inputs.Host, req.Inputs.Username, req.Inputs.Password)
	if err != nil {
		return infer.ReadResponse[ExtCommunityListArgs, ExtCommunityListState]{}, err
	}
	current, found, err := readExtCommunityList(ctx, cli, req.Inputs.Name)
	if err != nil {
		return infer.ReadResponse[ExtCommunityListArgs, ExtCommunityListState]{}, err
	}
	if !found {
		return infer.ReadResponse[ExtCommunityListArgs, ExtCommunityListState]{}, nil
	}
	state := ExtCommunityListState{ExtCommunityListArgs: req.Inputs}
	current.fillState(&state)
	return infer.ReadResponse[ExtCommunityListArgs, ExtCommunityListState]{
		ID:     extCommunityListID(req.Inputs.Name),
		Inputs: req.Inputs,
		State:  state,
	}, nil
}

// Update re-applies the ext-community-list (negate-then-rebuild).
func (*ExtCommunityList) Update(ctx context.Context, req infer.UpdateRequest[ExtCommunityListArgs, ExtCommunityListState]) (infer.UpdateResponse[ExtCommunityListState], error) {
	if err := validateExtCommunityList(req.Inputs); err != nil {
		return infer.UpdateResponse[ExtCommunityListState]{}, err
	}
	state := ExtCommunityListState{ExtCommunityListArgs: req.Inputs}
	if req.DryRun {
		return infer.UpdateResponse[ExtCommunityListState]{Output: state}, nil
	}
	if err := applyExtCommunityList(ctx, req.Inputs, false); err != nil {
		return infer.UpdateResponse[ExtCommunityListState]{}, fmt.Errorf("update ext-community-list %s: %w", req.Inputs.Name, err)
	}
	return infer.UpdateResponse[ExtCommunityListState]{Output: state}, nil
}

// Delete removes the ext-community-list.
func (*ExtCommunityList) Delete(ctx context.Context, req infer.DeleteRequest[ExtCommunityListState]) (infer.DeleteResponse, error) {
	if err := applyExtCommunityList(ctx, req.State.ExtCommunityListArgs, true); err != nil {
		return infer.DeleteResponse{}, fmt.Errorf("delete ext-community-list %s: %w", req.State.Name, err)
	}
	return infer.DeleteResponse{}, nil
}

// --- helpers ---------------------------------------------------------------

// extCommunityListType returns the resolved list type, defaulting to
// "standard".
func extCommunityListType(p *string) string {
	if p == nil || *p == "" {
		return "standard"
	}
	return *p
}

func validateExtCommunityList(args ExtCommunityListArgs) error {
	if strings.TrimSpace(args.Name) == "" {
		return ErrExtCommunityListNameRequired
	}
	t := extCommunityListType(args.ListType)
	if t != "standard" && t != "regexp" {
		return fmt.Errorf("%w: got %q", ErrExtCommunityListTypeInvalid, t)
	}
	if len(args.Entries) == 0 {
		return ErrExtCommunityListEmptyEntries
	}
	for i := range args.Entries {
		e := &args.Entries[i]
		if e.Action != "permit" && e.Action != "deny" {
			return fmt.Errorf("%w: got %q", ErrExtCommunityListAction, e.Action)
		}
		if strings.TrimSpace(e.Value) == "" {
			return ErrExtCommunityListValueEmpty
		}
		if t == "standard" {
			if e.Type == nil || *e.Type == "" {
				return fmt.Errorf("%w: missing type for value %q", ErrExtCommunityListEntryType, e.Value)
			}
			if _, ok := extCommunityValidEntryType[*e.Type]; !ok {
				return fmt.Errorf("%w: got %q", ErrExtCommunityListEntryType, *e.Type)
			}
			if !extCommunityValueRe.MatchString(e.Value) {
				return fmt.Errorf("%w: %q", ErrExtCommunityListStdValue, e.Value)
			}
		}
		if t == "regexp" && e.Type != nil && *e.Type != "" {
			return fmt.Errorf("%w: got type=%q", ErrExtCommunityListRegexpHasTyp, *e.Type)
		}
	}
	return nil
}

func extCommunityListID(name string) string { return "ext-community-list/" + name }

func applyExtCommunityList(ctx context.Context, args ExtCommunityListArgs, remove bool) error {
	cli, err := newClient(ctx, args.Host, args.Username, args.Password)
	if err != nil {
		return err
	}
	cfg := config.FromContext(ctx)
	sessName := cfg.SessionPrefix() + "extcomm-" + sanitizePrefixListName(args.Name)

	sess, err := cli.OpenSession(ctx, sessName)
	if err != nil {
		return err
	}
	cmds := buildExtCommunityListCmds(args, remove)
	if stageErr := sess.Stage(ctx, cmds); stageErr != nil {
		if abortErr := sess.Abort(ctx); abortErr != nil {
			return errors.Join(stageErr, abortErr)
		}
		return stageErr
	}
	return sess.Commit(ctx)
}

// buildExtCommunityListCmds renders the staged CLI block.
//
// Render: `no ip extcommunity-list <name>` (negate-then-rebuild),
// then per-entry:
//   - standard: `ip extcommunity-list <name> permit|deny <type>
//     <value>` where <type> is rt | soo.
//   - regexp:   `ip extcommunity-list regexp <name> permit|deny
//     <regex>` — the type prefix is captured inside <regex>.
func buildExtCommunityListCmds(args ExtCommunityListArgs, remove bool) []string {
	if remove {
		return []string{"no ip extcommunity-list " + args.Name}
	}
	cmds := []string{"no ip extcommunity-list " + args.Name}
	t := extCommunityListType(args.ListType)
	for _, e := range args.Entries {
		var line string
		if t == "regexp" {
			line = "ip extcommunity-list regexp " + args.Name + " " + e.Action + " " + e.Value
		} else {
			line = "ip extcommunity-list " + args.Name + " " + e.Action + " " + *e.Type + " " + e.Value
		}
		cmds = append(cmds, line)
	}
	return cmds
}

// extCommunityListRow is the parsed live state we care about.
type extCommunityListRow struct {
	Name    string
	Type    string
	Entries []ExtCommunityListEntry
}

// readExtCommunityList returns the live ext-community-list state, or
// (false, nil) when no list with the given name exists.
func readExtCommunityList(ctx context.Context, cli *eapi.Client, name string) (extCommunityListRow, bool, error) {
	resp, err := cli.RunCmds(ctx,
		[]string{"show running-config | grep extcommunity"},
		"text")
	if err != nil {
		return extCommunityListRow{}, false, err
	}
	if len(resp) == 0 {
		return extCommunityListRow{}, false, nil
	}
	out, _ := resp[0]["output"].(string)
	row, found := parseExtCommunityListLines(out, name)
	return row, found, nil
}

// parseExtCommunityListLines walks the grepped output and assembles the
// row matching `name`.
func parseExtCommunityListLines(out, name string) (extCommunityListRow, bool) {
	row := extCommunityListRow{Name: name, Type: "standard"}
	stdPrefix := "ip extcommunity-list " + name + " "
	regexpPrefix := "ip extcommunity-list regexp " + name + " "
	found := false
	for raw := range strings.SplitSeq(out, "\n") {
		line := strings.TrimSpace(raw)
		var rest string
		var ok bool
		isRegexp := false
		if rest, ok = strings.CutPrefix(line, regexpPrefix); ok {
			row.Type = "regexp"
			isRegexp = true
		} else if rest, ok = strings.CutPrefix(line, stdPrefix); !ok {
			continue
		}
		entry, ok := parseExtCommunityListEntryRest(rest, isRegexp)
		if !ok {
			continue
		}
		row.Entries = append(row.Entries, entry)
		found = true
	}
	if !found {
		return extCommunityListRow{}, false
	}
	return row, true
}

// parseExtCommunityListEntryRest parses the trailing `permit|deny ...`
// after the header has been cut.
//
// Standard form: `permit|deny <type> <value>`.
// Regexp form:   `permit|deny <regex>`.
func parseExtCommunityListEntryRest(rest string, isRegexp bool) (ExtCommunityListEntry, bool) {
	if isRegexp {
		parts := strings.SplitN(rest, " ", 2)
		if len(parts) != 2 {
			return ExtCommunityListEntry{}, false
		}
		if parts[0] != "permit" && parts[0] != "deny" {
			return ExtCommunityListEntry{}, false
		}
		return ExtCommunityListEntry{Action: parts[0], Value: parts[1]}, true
	}
	parts := strings.SplitN(rest, " ", 3)
	if len(parts) != 3 {
		return ExtCommunityListEntry{}, false
	}
	if parts[0] != "permit" && parts[0] != "deny" {
		return ExtCommunityListEntry{}, false
	}
	typ := parts[1]
	return ExtCommunityListEntry{Action: parts[0], Type: &typ, Value: parts[2]}, true
}

func (r extCommunityListRow) fillState(s *ExtCommunityListState) {
	v := r.Type
	s.ListType = &v
	if len(r.Entries) > 0 {
		s.Entries = append([]ExtCommunityListEntry(nil), r.Entries...)
	}
}
