package l3

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/pulumi/pulumi-go-provider/infer"

	"github.com/dantte-lp/pulumi-eos/internal/client/eapi"
	"github.com/dantte-lp/pulumi-eos/internal/config"
)

// Sentinel errors specific to CommunityList.
var (
	ErrCommunityListNameRequired = errors.New("communityList name is required")
	ErrCommunityListTypeInvalid  = errors.New("communityList type must be 'standard' or 'regexp'")
	ErrCommunityListEmptyEntries = errors.New("communityList must have at least one entry")
	ErrCommunityListAction       = errors.New("communityList entry action must be 'permit' or 'deny'")
	ErrCommunityListValueEmpty   = errors.New("communityList entry value is required")
	ErrCommunityListValueStd     = errors.New("communityList standard entry must be a well-known keyword (internet|local-as|no-advertise|no-export|GSHUT), aa:nn pair, or 0..4294967040")
)

// communityWellKnown is the set of well-known community keywords EOS
// accepts under `ip community-list <name> permit|deny <value>` for
// standard lists. Verified live against cEOS 4.36.0.1F.
var communityWellKnown = map[string]struct{}{
	"internet":     {},
	"local-as":     {},
	"no-advertise": {},
	"no-export":    {},
	"GSHUT":        {},
}

// communityAANN matches `aa:nn` where each side is 0..65535.
//
// EOS also accepts a single 0..4294967040 numeric value; that case is
// validated separately in validCommunityStandardValue.
var communityAANN = regexp.MustCompile(`^([0-9]{1,5}):([0-9]{1,5})$`)

// CommunityList models an EOS BGP community-list. cEOS 4.36 uses the
// `regexp` keyword for the regular-expression form (not `expanded` as
// the EOS User Manual §15.5.x suggests — `ip community-list expanded`
// returns "Ambiguous command (at token 2: 'expanded')" on cEOS 4.36).
// The discrepancy is documented here so the resource always renders
// the form the device actually accepts.
//
// Source: EOS User Manual §15.5 (community-list); TOI 14078 (multiple
// community matches in route-map); TOI 13855 (4-octet ext-community —
// related, ext-community list lives in eos:l3:ExtCommunityList).
type CommunityList struct{}

// CommunityListEntry is one permit/deny rule inside a list.
type CommunityListEntry struct {
	// Action is "permit" or "deny".
	Action string `pulumi:"action"`
	// Value is the matched community. For standard lists this is one
	// of internet | local-as | no-advertise | no-export | GSHUT, or
	// `aa:nn` where each side is 0..65535, or a numeric value
	// 0..4294967040. For regexp lists it is any regex string.
	Value string `pulumi:"value"`
}

// Annotate documents CommunityListEntry fields.
func (e *CommunityListEntry) Annotate(an infer.Annotator) {
	an.Describe(&e.Action, "permit or deny.")
	an.Describe(&e.Value, "Community value (well-known keyword, aa:nn, numeric, or regex for regexp lists).")
}

// CommunityListArgs is the input set.
type CommunityListArgs struct {
	// Name is the list identifier (PK).
	Name string `pulumi:"name"`
	// Type is "standard" (default) or "regexp" (regex match).
	Type *string `pulumi:"type,optional"`
	// Entries is the ordered list of permit/deny rules.
	Entries []CommunityListEntry `pulumi:"entries"`

	// Host overrides the provider-level eosUrl host for this resource.
	Host *string `pulumi:"host,optional"`
	// Username overrides the provider-level eosUsername for this resource.
	Username *string `pulumi:"username,optional"`
	// Password overrides the provider-level eosPassword for this resource.
	Password *string `provider:"secret" pulumi:"password,optional"`
}

// CommunityListState mirrors Args.
type CommunityListState struct {
	CommunityListArgs
}

// Annotate documents the resource.
func (r *CommunityList) Annotate(a infer.Annotator) {
	a.Describe(&r, "An EOS BGP community-list (standard or regexp). Composes with eos:l3:RouteMap match community.")
}

// Annotate documents CommunityListArgs fields.
func (a *CommunityListArgs) Annotate(an infer.Annotator) {
	an.Describe(&a.Name, "Community-list name (PK).")
	an.Describe(&a.Type, "List type: standard (default) or regexp.")
	an.Describe(&a.Entries, "Ordered permit/deny entries.")
	an.Describe(&a.Host, "Optional management hostname override.")
	an.Describe(&a.Username, "Optional AAA username override.")
	an.Describe(&a.Password, "Optional AAA password override.")
}

// Annotate is a no-op; State has no extra fields.
func (s *CommunityListState) Annotate(_ infer.Annotator) {}

// Create configures the community-list.
func (*CommunityList) Create(ctx context.Context, req infer.CreateRequest[CommunityListArgs]) (infer.CreateResponse[CommunityListState], error) {
	if err := validateCommunityList(req.Inputs); err != nil {
		return infer.CreateResponse[CommunityListState]{}, err
	}
	state := CommunityListState{CommunityListArgs: req.Inputs}
	if req.DryRun {
		return infer.CreateResponse[CommunityListState]{ID: communityListID(req.Inputs.Name), Output: state}, nil
	}
	if err := applyCommunityList(ctx, req.Inputs, false); err != nil {
		return infer.CreateResponse[CommunityListState]{}, fmt.Errorf("create community-list %s: %w", req.Inputs.Name, err)
	}
	return infer.CreateResponse[CommunityListState]{ID: communityListID(req.Inputs.Name), Output: state}, nil
}

// Read refreshes community-list state from the device.
func (*CommunityList) Read(ctx context.Context, req infer.ReadRequest[CommunityListArgs, CommunityListState]) (infer.ReadResponse[CommunityListArgs, CommunityListState], error) {
	cli, err := newClient(ctx, req.Inputs.Host, req.Inputs.Username, req.Inputs.Password)
	if err != nil {
		return infer.ReadResponse[CommunityListArgs, CommunityListState]{}, err
	}
	current, found, err := readCommunityList(ctx, cli, req.Inputs.Name)
	if err != nil {
		return infer.ReadResponse[CommunityListArgs, CommunityListState]{}, err
	}
	if !found {
		return infer.ReadResponse[CommunityListArgs, CommunityListState]{}, nil
	}
	state := CommunityListState{CommunityListArgs: req.Inputs}
	current.fillState(&state)
	return infer.ReadResponse[CommunityListArgs, CommunityListState]{
		ID:     communityListID(req.Inputs.Name),
		Inputs: req.Inputs,
		State:  state,
	}, nil
}

// Update re-applies the community-list (negate-then-rebuild).
func (*CommunityList) Update(ctx context.Context, req infer.UpdateRequest[CommunityListArgs, CommunityListState]) (infer.UpdateResponse[CommunityListState], error) {
	if err := validateCommunityList(req.Inputs); err != nil {
		return infer.UpdateResponse[CommunityListState]{}, err
	}
	state := CommunityListState{CommunityListArgs: req.Inputs}
	if req.DryRun {
		return infer.UpdateResponse[CommunityListState]{Output: state}, nil
	}
	if err := applyCommunityList(ctx, req.Inputs, false); err != nil {
		return infer.UpdateResponse[CommunityListState]{}, fmt.Errorf("update community-list %s: %w", req.Inputs.Name, err)
	}
	return infer.UpdateResponse[CommunityListState]{Output: state}, nil
}

// Delete removes the community-list.
func (*CommunityList) Delete(ctx context.Context, req infer.DeleteRequest[CommunityListState]) (infer.DeleteResponse, error) {
	if err := applyCommunityList(ctx, req.State.CommunityListArgs, true); err != nil {
		return infer.DeleteResponse{}, fmt.Errorf("delete community-list %s: %w", req.State.Name, err)
	}
	return infer.DeleteResponse{}, nil
}

// --- helpers ---------------------------------------------------------------

// communityListType returns the resolved type, defaulting to "standard".
func communityListType(p *string) string {
	if p == nil || *p == "" {
		return listTypeStandard
	}
	return *p
}

func validateCommunityList(args CommunityListArgs) error {
	if strings.TrimSpace(args.Name) == "" {
		return ErrCommunityListNameRequired
	}
	t := communityListType(args.Type)
	if t != listTypeStandard && t != listTypeRegexp {
		return fmt.Errorf("%w: got %q", ErrCommunityListTypeInvalid, t)
	}
	if len(args.Entries) == 0 {
		return ErrCommunityListEmptyEntries
	}
	for i := range args.Entries {
		e := &args.Entries[i]
		if e.Action != "permit" && e.Action != "deny" {
			return fmt.Errorf("%w: got %q", ErrCommunityListAction, e.Action)
		}
		if strings.TrimSpace(e.Value) == "" {
			return ErrCommunityListValueEmpty
		}
		if t == listTypeStandard && !validCommunityStandardValue(e.Value) {
			return fmt.Errorf("%w: %q", ErrCommunityListValueStd, e.Value)
		}
	}
	return nil
}

// validCommunityStandardValue accepts a well-known keyword, an
// `aa:nn` pair (each side 0..65535), or a single numeric value
// 0..4294967040.
func validCommunityStandardValue(v string) bool {
	if _, ok := communityWellKnown[v]; ok {
		return true
	}
	if m := communityAANN.FindStringSubmatch(v); m != nil {
		aa, err1 := strconv.Atoi(m[1])
		nn, err2 := strconv.Atoi(m[2])
		return err1 == nil && err2 == nil && aa >= 0 && aa <= 65535 && nn >= 0 && nn <= 65535
	}
	if n, err := strconv.Atoi(v); err == nil {
		return n >= 0 && n <= 4294967040
	}
	return false
}

func communityListID(name string) string { return "community-list/" + name }

func applyCommunityList(ctx context.Context, args CommunityListArgs, remove bool) error {
	cli, err := newClient(ctx, args.Host, args.Username, args.Password)
	if err != nil {
		return err
	}
	cfg := config.FromContext(ctx)
	sessName := cfg.SessionPrefix() + "comm-" + sanitizePrefixListName(args.Name)

	sess, err := cli.OpenSession(ctx, sessName)
	if err != nil {
		return err
	}
	cmds := buildCommunityListCmds(args, remove)
	if stageErr := sess.Stage(ctx, cmds); stageErr != nil {
		if abortErr := sess.Abort(ctx); abortErr != nil {
			return errors.Join(stageErr, abortErr)
		}
		return stageErr
	}
	return sess.Commit(ctx)
}

// buildCommunityListCmds renders the staged CLI block.
//
// Render: `no ip community-list <name>` (negate-then-rebuild, same
// pattern as PrefixList / RouteMap so stale entries cannot leak),
// then per-entry `ip community-list [regexp] <name> permit|deny
// <value>`.
//
// The regexp-list `no` form on cEOS 4.36 is the bare
// `no ip community-list <name>` — the type keyword is NOT echoed in
// negate, EOS resolves it from the running-config entry.
func buildCommunityListCmds(args CommunityListArgs, remove bool) []string {
	if remove {
		return []string{"no ip community-list " + args.Name}
	}
	cmds := []string{"no ip community-list " + args.Name}
	t := communityListType(args.Type)
	prefix := "ip community-list "
	if t == listTypeRegexp {
		prefix = "ip community-list regexp "
	}
	for _, e := range args.Entries {
		cmds = append(cmds, prefix+args.Name+" "+e.Action+" "+e.Value)
	}
	return cmds
}

// communityListRow is the parsed live state we care about.
type communityListRow struct {
	Name    string
	Type    string
	Entries []CommunityListEntry
}

// readCommunityList returns the live community-list state, or
// (false, nil) when no list with the given name exists. EOS pipe-grep
// is single-word-only, so we cast a wide net (`grep community-list`)
// and filter `ip community-list [regexp] <name> ` lines client-side.
func readCommunityList(ctx context.Context, cli *eapi.Client, name string) (communityListRow, bool, error) {
	resp, err := cli.RunCmds(ctx,
		[]string{"show running-config | grep community-list"},
		"text")
	if err != nil {
		return communityListRow{}, false, err
	}
	if len(resp) == 0 {
		return communityListRow{}, false, nil
	}
	out, _ := resp[0]["output"].(string)
	row, found := parseCommunityListLines(out, name)
	return row, found, nil
}

// parseCommunityListLines extracts the named list's entries.
// Exposed for unit tests.
func parseCommunityListLines(out, name string) (communityListRow, bool) {
	row := communityListRow{Name: name, Type: listTypeStandard}
	stdPrefix := "ip community-list " + name + " "
	regexpPrefix := "ip community-list regexp " + name + " "
	found := false
	for raw := range strings.SplitSeq(out, "\n") {
		line := strings.TrimSpace(raw)
		var rest string
		var ok bool
		if rest, ok = strings.CutPrefix(line, regexpPrefix); ok {
			row.Type = listTypeRegexp
		} else if rest, ok = strings.CutPrefix(line, stdPrefix); !ok {
			continue
		}
		entry, ok := parseCommunityListEntryRest(rest)
		if !ok {
			continue
		}
		row.Entries = append(row.Entries, entry)
		found = true
	}
	if !found {
		return communityListRow{}, false
	}
	return row, true
}

// parseCommunityListEntryRest parses `permit|deny <value>` after the
// `ip community-list [regexp] <name> ` header has been cut.
func parseCommunityListEntryRest(rest string) (CommunityListEntry, bool) {
	parts := strings.SplitN(rest, " ", 2)
	if len(parts) != 2 {
		return CommunityListEntry{}, false
	}
	action := parts[0]
	if action != "permit" && action != "deny" {
		return CommunityListEntry{}, false
	}
	return CommunityListEntry{Action: action, Value: parts[1]}, true
}

func (r communityListRow) fillState(s *CommunityListState) {
	v := r.Type
	s.Type = &v
	if len(r.Entries) > 0 {
		s.Entries = append([]CommunityListEntry(nil), r.Entries...)
	}
}
