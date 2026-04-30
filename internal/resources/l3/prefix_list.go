package l3

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"strconv"
	"strings"

	"github.com/pulumi/pulumi-go-provider/infer"

	"github.com/dantte-lp/pulumi-eos/internal/client/eapi"
	"github.com/dantte-lp/pulumi-eos/internal/config"
)

// Sentinel errors specific to PrefixList.
var (
	ErrPrefixListNameRequired   = errors.New("prefixList name is required")
	ErrPrefixListEntriesEmpty   = errors.New("prefixList must have at least one entry")
	ErrPrefixListSeqOutOfRange  = errors.New("prefixList entry seq must be in 0..65535")
	ErrPrefixListSeqDuplicate   = errors.New("prefixList entry seq numbers must be unique within a list")
	ErrPrefixListActionInvalid  = errors.New("prefixList entry action must be 'permit' or 'deny'")
	ErrPrefixListBadPrefix      = errors.New("prefixList entry prefix must be a valid IPv4 CIDR")
	ErrPrefixListMaskOutOfRange = errors.New("prefixList eq / ge / le must be in 1..32")
	ErrPrefixListMaskCombo      = errors.New("prefixList eq is mutually exclusive with ge / le")
	ErrPrefixListGeLeOrder      = errors.New("prefixList ge must be <= le when both are set")
	ErrPrefixListGeAtLeastPfx   = errors.New("prefixList ge must be >= the prefix length")
)

// PrefixList models a named EOS IPv4 prefix-list as a single resource.
// Each named list is one Pulumi resource; ordered Entries map to the
// flat `ip prefix-list <name> seq N {permit|deny} <cidr> [...]`
// directives EOS emits.
//
// Source: EOS User Manual §15.4.1.61 (`ip prefix-list`); validated
// live against cEOS 4.36.0.1F per the per-resource verification rule.
type PrefixList struct{}

// PrefixListEntry models a single sequenced rule.
type PrefixListEntry struct {
	// Seq is the sequence number (0..65535). Unique within the list.
	Seq int `pulumi:"seq"`
	// Action is "permit" or "deny".
	Action string `pulumi:"action"`
	// Prefix is the matched network in CIDR (e.g. "10.0.0.0/8").
	Prefix string `pulumi:"prefix"`
	// Eq matches an exact prefix length. Mutually exclusive with
	// `ge` / `le`.
	Eq *int `pulumi:"eq,optional"`
	// Ge is the lower bound on prefix length (1..32).
	Ge *int `pulumi:"ge,optional"`
	// Le is the upper bound on prefix length (1..32).
	Le *int `pulumi:"le,optional"`
}

// Annotate documents PrefixListEntry fields.
func (e *PrefixListEntry) Annotate(an infer.Annotator) {
	an.Describe(&e.Seq, "Sequence number (0..65535). Must be unique within the list.")
	an.Describe(&e.Action, "Action keyword: permit or deny.")
	an.Describe(&e.Prefix, "Matched network in IPv4 CIDR (e.g. 10.0.0.0/8).")
	an.Describe(&e.Eq, "Exact prefix length. Mutually exclusive with ge / le.")
	an.Describe(&e.Ge, "Lower bound on prefix length (1..32).")
	an.Describe(&e.Le, "Upper bound on prefix length (1..32).")
}

// PrefixListArgs is the input set.
type PrefixListArgs struct {
	// Name is the prefix-list identifier (PK).
	Name string `pulumi:"name"`
	// Remark is an optional remark line attached to the list.
	Remark *string `pulumi:"remark,optional"`
	// Entries is the ordered list of permit/deny rules.
	Entries []PrefixListEntry `pulumi:"entries"`

	// Host overrides the provider-level eosUrl host for this resource.
	Host *string `pulumi:"host,optional"`
	// Username overrides the provider-level eosUsername for this resource.
	Username *string `pulumi:"username,optional"`
	// Password overrides the provider-level eosPassword for this resource.
	Password *string `provider:"secret" pulumi:"password,optional"`
}

// PrefixListState mirrors Args.
type PrefixListState struct {
	PrefixListArgs
}

// Annotate documents the resource.
func (r *PrefixList) Annotate(a infer.Annotator) {
	a.Describe(&r, "An EOS IPv4 prefix-list. Composable building block for route-maps and per-peer-group route filters.")
}

// Annotate documents PrefixListArgs fields.
func (a *PrefixListArgs) Annotate(an infer.Annotator) {
	an.Describe(&a.Name, "Prefix-list name (PK).")
	an.Describe(&a.Remark, "Optional remark line; rendered as `ip prefix-list <name> remark <text>`.")
	an.Describe(&a.Entries, "Ordered permit/deny entries.")
	an.Describe(&a.Host, "Optional management hostname override.")
	an.Describe(&a.Username, "Optional AAA username override.")
	an.Describe(&a.Password, "Optional AAA password override.")
}

// Annotate is a no-op; State has no extra fields.
func (s *PrefixListState) Annotate(_ infer.Annotator) {}

// Create configures the prefix-list.
func (*PrefixList) Create(ctx context.Context, req infer.CreateRequest[PrefixListArgs]) (infer.CreateResponse[PrefixListState], error) {
	if err := validatePrefixList(req.Inputs); err != nil {
		return infer.CreateResponse[PrefixListState]{}, err
	}
	state := PrefixListState{PrefixListArgs: req.Inputs}
	if req.DryRun {
		return infer.CreateResponse[PrefixListState]{ID: prefixListID(req.Inputs.Name), Output: state}, nil
	}
	if err := applyPrefixList(ctx, req.Inputs, false); err != nil {
		return infer.CreateResponse[PrefixListState]{}, fmt.Errorf("create prefix-list %s: %w", req.Inputs.Name, err)
	}
	return infer.CreateResponse[PrefixListState]{ID: prefixListID(req.Inputs.Name), Output: state}, nil
}

// Read refreshes prefix-list state from the device.
func (*PrefixList) Read(ctx context.Context, req infer.ReadRequest[PrefixListArgs, PrefixListState]) (infer.ReadResponse[PrefixListArgs, PrefixListState], error) {
	cli, err := newClient(ctx, req.Inputs.Host, req.Inputs.Username, req.Inputs.Password)
	if err != nil {
		return infer.ReadResponse[PrefixListArgs, PrefixListState]{}, err
	}
	current, found, err := readPrefixList(ctx, cli, req.Inputs.Name)
	if err != nil {
		return infer.ReadResponse[PrefixListArgs, PrefixListState]{}, err
	}
	if !found {
		return infer.ReadResponse[PrefixListArgs, PrefixListState]{}, nil
	}
	state := PrefixListState{PrefixListArgs: req.Inputs}
	current.fillState(&state)
	return infer.ReadResponse[PrefixListArgs, PrefixListState]{
		ID:     prefixListID(req.Inputs.Name),
		Inputs: req.Inputs,
		State:  state,
	}, nil
}

// Update re-applies the prefix-list: negate-then-rebuild ensures stale
// `seq` lines from a previous version are gone before the new set
// renders. EOS evaluates the staged session as a unit so the diff
// reaches running-config atomically.
func (*PrefixList) Update(ctx context.Context, req infer.UpdateRequest[PrefixListArgs, PrefixListState]) (infer.UpdateResponse[PrefixListState], error) {
	if err := validatePrefixList(req.Inputs); err != nil {
		return infer.UpdateResponse[PrefixListState]{}, err
	}
	state := PrefixListState{PrefixListArgs: req.Inputs}
	if req.DryRun {
		return infer.UpdateResponse[PrefixListState]{Output: state}, nil
	}
	if err := applyPrefixList(ctx, req.Inputs, false); err != nil {
		return infer.UpdateResponse[PrefixListState]{}, fmt.Errorf("update prefix-list %s: %w", req.Inputs.Name, err)
	}
	return infer.UpdateResponse[PrefixListState]{Output: state}, nil
}

// Delete removes the entire named prefix-list.
func (*PrefixList) Delete(ctx context.Context, req infer.DeleteRequest[PrefixListState]) (infer.DeleteResponse, error) {
	if err := applyPrefixList(ctx, req.State.PrefixListArgs, true); err != nil {
		return infer.DeleteResponse{}, fmt.Errorf("delete prefix-list %s: %w", req.State.Name, err)
	}
	return infer.DeleteResponse{}, nil
}

// --- helpers ---------------------------------------------------------------

func validatePrefixList(args PrefixListArgs) error {
	if strings.TrimSpace(args.Name) == "" {
		return ErrPrefixListNameRequired
	}
	if len(args.Entries) == 0 {
		return ErrPrefixListEntriesEmpty
	}
	seen := make(map[int]struct{}, len(args.Entries))
	for i := range args.Entries {
		e := &args.Entries[i]
		if err := validatePrefixListEntry(e); err != nil {
			return fmt.Errorf("entry %d: %w", e.Seq, err)
		}
		if _, dup := seen[e.Seq]; dup {
			return fmt.Errorf("%w: %d", ErrPrefixListSeqDuplicate, e.Seq)
		}
		seen[e.Seq] = struct{}{}
	}
	return nil
}

func validatePrefixListEntry(e *PrefixListEntry) error {
	if e.Seq < 0 || e.Seq > 65535 {
		return fmt.Errorf("%w: got %d", ErrPrefixListSeqOutOfRange, e.Seq)
	}
	if e.Action != "permit" && e.Action != "deny" {
		return fmt.Errorf("%w: got %q", ErrPrefixListActionInvalid, e.Action)
	}
	pfx, err := netip.ParsePrefix(e.Prefix)
	if err != nil || !pfx.Addr().Is4() {
		return fmt.Errorf("%w: %q", ErrPrefixListBadPrefix, e.Prefix)
	}
	if e.Eq != nil && (e.Ge != nil || e.Le != nil) {
		return ErrPrefixListMaskCombo
	}
	for _, m := range []*int{e.Eq, e.Ge, e.Le} {
		if m != nil && (*m < 1 || *m > 32) {
			return fmt.Errorf("%w: got %d", ErrPrefixListMaskOutOfRange, *m)
		}
	}
	if e.Ge != nil && e.Le != nil && *e.Ge > *e.Le {
		return fmt.Errorf("%w: ge=%d le=%d", ErrPrefixListGeLeOrder, *e.Ge, *e.Le)
	}
	if e.Ge != nil && *e.Ge < pfx.Bits() {
		return fmt.Errorf("%w: prefix-len=%d ge=%d", ErrPrefixListGeAtLeastPfx, pfx.Bits(), *e.Ge)
	}
	return nil
}

func prefixListID(name string) string { return "prefix-list/" + name }

func applyPrefixList(ctx context.Context, args PrefixListArgs, remove bool) error {
	cli, err := newClient(ctx, args.Host, args.Username, args.Password)
	if err != nil {
		return err
	}
	cfg := config.FromContext(ctx)
	sessName := cfg.SessionPrefix() + "pfxlist-" + sanitizePrefixListName(args.Name)

	sess, err := cli.OpenSession(ctx, sessName)
	if err != nil {
		return err
	}
	cmds := buildPrefixListCmds(args, remove)
	if stageErr := sess.Stage(ctx, cmds); stageErr != nil {
		if abortErr := sess.Abort(ctx); abortErr != nil {
			return errors.Join(stageErr, abortErr)
		}
		return stageErr
	}
	return sess.Commit(ctx)
}

func sanitizePrefixListName(name string) string {
	r := strings.NewReplacer(".", "-", " ", "-", ":", "-", "/", "-")
	return r.Replace(name)
}

// buildPrefixListCmds renders the staged CLI block.
//
// On apply we always emit `no ip prefix-list <name>` first to
// guarantee stale `seq N` lines from a previous version are gone
// before the new set renders. The negate-then-rebuild pattern is the
// same one used in `eos:l2:Stp` and `eos:l2:EvpnEthernetSegment` for
// nested-block resources.
func buildPrefixListCmds(args PrefixListArgs, remove bool) []string {
	if remove {
		return []string{"no ip prefix-list " + args.Name}
	}
	cmds := []string{"no ip prefix-list " + args.Name}
	if args.Remark != nil && *args.Remark != "" {
		cmds = append(cmds, "ip prefix-list "+args.Name+" remark "+*args.Remark)
	}
	entries := append([]PrefixListEntry(nil), args.Entries...)
	sort.Slice(entries, func(i, j int) bool { return entries[i].Seq < entries[j].Seq })
	for _, e := range entries {
		cmds = append(cmds, prefixListEntryCmd(args.Name, e))
	}
	return cmds
}

// prefixListEntryCmd renders one `ip prefix-list <name> seq N action <cidr> [eq|ge[le]|le]`.
func prefixListEntryCmd(name string, e PrefixListEntry) string {
	parts := []string{"ip", "prefix-list", name, "seq", strconv.Itoa(e.Seq), e.Action, e.Prefix}
	switch {
	case e.Eq != nil:
		parts = append(parts, "eq", strconv.Itoa(*e.Eq))
	case e.Ge != nil && e.Le != nil:
		parts = append(parts, "ge", strconv.Itoa(*e.Ge), "le", strconv.Itoa(*e.Le))
	case e.Ge != nil:
		parts = append(parts, "ge", strconv.Itoa(*e.Ge))
	case e.Le != nil:
		parts = append(parts, "le", strconv.Itoa(*e.Le))
	}
	return strings.Join(parts, " ")
}

// prefixListRow is the parsed live state we care about.
type prefixListRow struct {
	Name    string
	Remark  string
	Entries []PrefixListEntry
}

// readPrefixList returns the live prefix-list state, or (false, nil)
// when no list with the given name exists. EOS pipe-grep is
// single-word-only, so we cast a wide net (`grep prefix-list`) and
// filter `ip prefix-list <name>` lines client-side.
func readPrefixList(ctx context.Context, cli *eapi.Client, name string) (prefixListRow, bool, error) {
	resp, err := cli.RunCmds(ctx,
		[]string{"show running-config | grep prefix-list"},
		"text")
	if err != nil {
		return prefixListRow{}, false, err
	}
	if len(resp) == 0 {
		return prefixListRow{}, false, nil
	}
	out, _ := resp[0]["output"].(string)
	row, found := parsePrefixListLines(out, name)
	return row, found, nil
}

// parsePrefixListLines walks the grepped output and assembles the
// row matching `name`. Exposed for unit tests.
func parsePrefixListLines(out, name string) (prefixListRow, bool) {
	row := prefixListRow{Name: name}
	prefix := "ip prefix-list " + name + " "
	found := false
	for raw := range strings.SplitSeq(out, "\n") {
		line := strings.TrimSpace(raw)
		rest, ok := strings.CutPrefix(line, prefix)
		if !ok {
			continue
		}
		found = true
		if remark, ok := strings.CutPrefix(rest, "remark "); ok {
			row.Remark = remark
			continue
		}
		if entry, ok := parsePrefixListEntryRest(rest); ok {
			row.Entries = append(row.Entries, entry)
		}
	}
	if !found {
		return prefixListRow{}, false
	}
	sort.Slice(row.Entries, func(i, j int) bool { return row.Entries[i].Seq < row.Entries[j].Seq })
	return row, true
}

// parsePrefixListEntryRest parses the trailing `seq N action <cidr>
// [eq M | ge M [le L] | le L]` after the `ip prefix-list <name> `
// header has been cut.
func parsePrefixListEntryRest(rest string) (PrefixListEntry, bool) {
	tokens := strings.Fields(rest)
	if len(tokens) < 4 || tokens[0] != "seq" {
		return PrefixListEntry{}, false
	}
	seq, err := strconv.Atoi(tokens[1])
	if err != nil {
		return PrefixListEntry{}, false
	}
	action := tokens[2]
	if action != "permit" && action != "deny" {
		return PrefixListEntry{}, false
	}
	entry := PrefixListEntry{Seq: seq, Action: action, Prefix: tokens[3]}
	for i := 4; i < len(tokens)-1; i += 2 {
		val, err := strconv.Atoi(tokens[i+1])
		if err != nil {
			continue
		}
		switch tokens[i] {
		case "eq":
			entry.Eq = &val
		case "ge":
			entry.Ge = &val
		case "le":
			entry.Le = &val
		}
	}
	return entry, true
}

func (r prefixListRow) fillState(s *PrefixListState) {
	if r.Remark != "" {
		v := r.Remark
		s.Remark = &v
	}
	if len(r.Entries) > 0 {
		s.Entries = append([]PrefixListEntry(nil), r.Entries...)
	}
}
