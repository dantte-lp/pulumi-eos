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

// Sentinel errors specific to RouteMap.
var (
	ErrRouteMapNameRequired     = errors.New("routeMap name is required")
	ErrRouteMapEntriesEmpty     = errors.New("routeMap must have at least one sequence")
	ErrRouteMapSeqOutOfRange    = errors.New("routeMap seq must be in 0..65535")
	ErrRouteMapSeqDuplicate     = errors.New("routeMap seq numbers must be unique within the map")
	ErrRouteMapActionInvalid    = errors.New("routeMap action must be 'permit' or 'deny'")
	ErrRouteMapOriginInvalid    = errors.New("routeMap set origin must be one of: igp, egp, incomplete")
	ErrRouteMapSourceProtocol   = errors.New("routeMap match sourceProtocol must be one of: connected, static, isis, ospf, bgp, rip")
	ErrRouteMapMetricFormat     = errors.New("routeMap set metric must be a positive integer or +N / -N delta")
	ErrRouteMapNextHopInvalid   = errors.New("routeMap set ipNextHop must be a valid IPv4 address or 'unchanged' / 'self'")
	ErrRouteMapCommunityCombo   = errors.New("routeMap set community: 'additive' and 'none' are mutually exclusive")
	ErrRouteMapTagOutOfRange    = errors.New("routeMap match/set tag must be in 0..4294967295")
	ErrRouteMapLocalPrefRange   = errors.New("routeMap match/set localPreference must be >= 0")
	ErrRouteMapAsnInvalid       = errors.New("routeMap set asPathPrepend ASNs must be in 1..4294967295")
	ErrRouteMapContinueOutRange = errors.New("routeMap continue must be in 0..65535")
	ErrRouteMapMetricMatchRange = errors.New("routeMap match metric must be >= 0")
)

// validRouteMapOrigin enumerates the BGP origin keywords accepted by
// `set origin`. EOS does NOT accept `match origin` (verified live
// against cEOS 4.36.0.1F via probe-audit — the keyword "Incomplete
// token" rejects every variant). The Cisco IOS `match origin` clause
// has no EOS equivalent in v0; use `match route-type` for routing-
// source filtering.
var validRouteMapOrigin = map[string]struct{}{
	"igp":        {},
	"egp":        {},
	"incomplete": {},
}

// validRouteMapSourceProtocol enumerates the source-protocol keywords
// for `match source-protocol`.
var validRouteMapSourceProtocol = map[string]struct{}{
	"connected": {},
	"static":    {},
	"isis":      {},
	"ospf":      {},
	"bgp":       {},
	"rip":       {},
}

// RouteMap models an EOS named route-map with sequenced match / set
// clauses. Each named map is one Pulumi resource. The Pulumi engine
// reconciles the structured `entries` field; render order is
// deterministic so diffs against `show running-config | section
// route-map` are stable.
//
// Source: EOS User Manual §15.6 (route-map); TOI 14078 (multiple
// community matches in a single sequence); TOI 13855 (ext-community
// 4-octet AS support); validated live against cEOS 4.36.0.1F per the
// per-resource verification rule.
type RouteMap struct{}

// RouteMapMatch carries the match clauses for one sequence. v0 surface:
// the most-used clauses for EVPN/VXLAN inbound + outbound policy.
// `rpki` and `match address dynamic prefix-list` are deferred to v1.
type RouteMapMatch struct {
	IpAddressPrefixList []string `pulumi:"ipAddressPrefixList,optional"`
	Community           []string `pulumi:"community,optional"`
	Extcommunity        []string `pulumi:"extcommunity,optional"`
	AsPath              []string `pulumi:"asPath,optional"`
	Interface           *string  `pulumi:"interface,optional"`
	Tag                 *int     `pulumi:"tag,optional"`
	Metric              *int     `pulumi:"metric,optional"`
	LocalPreference     *int     `pulumi:"localPreference,optional"`
	SourceProtocol      *string  `pulumi:"sourceProtocol,optional"`
}

// Annotate documents RouteMapMatch fields.
func (m *RouteMapMatch) Annotate(an infer.Annotator) {
	an.Describe(&m.IpAddressPrefixList, "`match ip address prefix-list <name> [<name>...]` (multiple names AND-ed per-line, OR-ed across lines).")
	an.Describe(&m.Community, "`match community <list-name> [<list-name>...]`.")
	an.Describe(&m.Extcommunity, "`match extcommunity <list-name> [<list-name>...]`.")
	an.Describe(&m.AsPath, "`match as-path <list-name> [<list-name>...]`.")
	an.Describe(&m.Interface, "`match interface <name>`.")
	an.Describe(&m.Tag, "`match tag <0..4294967295>`.")
	an.Describe(&m.Metric, "`match metric <0..>`.")
	an.Describe(&m.LocalPreference, "`match local-preference <0..>`.")
	an.Describe(&m.SourceProtocol, "`match source-protocol connected|static|isis|ospf|bgp|rip`.")
}

// RouteMapSet carries the set clauses for one sequence. v0 surface
// covers the standard inbound (set community / set local-preference /
// set metric) and outbound (set as-path prepend / set ip next-hop /
// set tag / set origin) policy needs.
type RouteMapSet struct {
	Community              []string `pulumi:"community,optional"`
	CommunityAdditive      *bool    `pulumi:"communityAdditive,optional"`
	CommunityNone          *bool    `pulumi:"communityNone,optional"`
	ExtcommunityRt         []string `pulumi:"extcommunityRt,optional"`
	ExtcommunityRtAdditive *bool    `pulumi:"extcommunityRtAdditive,optional"`
	LocalPreference        *int     `pulumi:"localPreference,optional"`
	Metric                 *string  `pulumi:"metric,optional"`
	AsPathPrepend          []int    `pulumi:"asPathPrepend,optional"`
	IpNextHop              *string  `pulumi:"ipNextHop,optional"`
	Origin                 *string  `pulumi:"origin,optional"`
	Tag                    *int     `pulumi:"tag,optional"`
}

// Annotate documents RouteMapSet fields.
func (s *RouteMapSet) Annotate(an infer.Annotator) {
	an.Describe(&s.Community, "Community values to set (e.g. \"65000:100\", \"no-export\").")
	an.Describe(&s.CommunityAdditive, "Append to existing communities rather than replace (`set community ... additive`).")
	an.Describe(&s.CommunityNone, "`set community none` — strip all communities. Mutually exclusive with `additive`.")
	an.Describe(&s.ExtcommunityRt, "Ext-community RT values (e.g. \"65000:1\").")
	an.Describe(&s.ExtcommunityRtAdditive, "When true, append `additive` to every `set extcommunity rt` line so existing route-targets are preserved.")
	an.Describe(&s.LocalPreference, "`set local-preference <0..>`.")
	an.Describe(&s.Metric, "`set metric <N>` or signed delta `+N` / `-N`.")
	an.Describe(&s.AsPathPrepend, "ASNs to prepend (`set as-path prepend ...`); ordered.")
	an.Describe(&s.IpNextHop, "`set ip next-hop <ip|unchanged|self>`.")
	an.Describe(&s.Origin, "`set origin igp|egp|incomplete`.")
	an.Describe(&s.Tag, "`set tag <0..4294967295>`.")
}

// RouteMapEntry models one `route-map <name> <action> <seq>` block.
type RouteMapEntry struct {
	Seq      int            `pulumi:"seq"`
	Action   string         `pulumi:"action"`
	Match    *RouteMapMatch `pulumi:"match,optional"`
	Set      *RouteMapSet   `pulumi:"set,optional"`
	Continue *int           `pulumi:"continue,optional"`
}

// Annotate documents RouteMapEntry fields.
func (e *RouteMapEntry) Annotate(an infer.Annotator) {
	an.Describe(&e.Seq, "Sequence number (0..65535). Unique within the map.")
	an.Describe(&e.Action, "Action keyword: permit or deny.")
	an.Describe(&e.Match, "match clauses; omitted entries skip the corresponding `match` line.")
	an.Describe(&e.Set, "set clauses; omitted entries skip the corresponding `set` line.")
	an.Describe(&e.Continue, "`continue [seq <n>]` — fall-through to a higher sequence after this one matches.")
}

// RouteMapArgs is the input set.
type RouteMapArgs struct {
	Name    string          `pulumi:"name"`
	Entries []RouteMapEntry `pulumi:"entries"`

	Host     *string `pulumi:"host,optional"`
	Username *string `pulumi:"username,optional"`
	Password *string `provider:"secret"          pulumi:"password,optional"`
}

// RouteMapState mirrors Args.
type RouteMapState struct {
	RouteMapArgs
}

// Annotate documents the resource.
func (r *RouteMap) Annotate(a infer.Annotator) {
	a.Describe(&r, "An EOS route-map with sequenced match/set clauses. Composes with eos:l3:PrefixList and BGP peer-group inbound/outbound filters.")
}

// Annotate documents RouteMapArgs fields.
func (a *RouteMapArgs) Annotate(an infer.Annotator) {
	an.Describe(&a.Name, "Route-map name (PK).")
	an.Describe(&a.Entries, "Ordered sequenced rules.")
	an.Describe(&a.Host, "Optional management hostname override.")
	an.Describe(&a.Username, "Optional AAA username override.")
	an.Describe(&a.Password, "Optional AAA password override.")
}

// Annotate is a no-op; State has no extra fields.
func (s *RouteMapState) Annotate(_ infer.Annotator) {}

// Create configures the route-map.
func (*RouteMap) Create(ctx context.Context, req infer.CreateRequest[RouteMapArgs]) (infer.CreateResponse[RouteMapState], error) {
	if err := validateRouteMap(req.Inputs); err != nil {
		return infer.CreateResponse[RouteMapState]{}, err
	}
	state := RouteMapState{RouteMapArgs: req.Inputs}
	if req.DryRun {
		return infer.CreateResponse[RouteMapState]{ID: routeMapID(req.Inputs.Name), Output: state}, nil
	}
	if err := applyRouteMap(ctx, req.Inputs, false); err != nil {
		return infer.CreateResponse[RouteMapState]{}, fmt.Errorf("create route-map %s: %w", req.Inputs.Name, err)
	}
	return infer.CreateResponse[RouteMapState]{ID: routeMapID(req.Inputs.Name), Output: state}, nil
}

// Read refreshes route-map state from the device.
func (*RouteMap) Read(ctx context.Context, req infer.ReadRequest[RouteMapArgs, RouteMapState]) (infer.ReadResponse[RouteMapArgs, RouteMapState], error) {
	cli, err := newClient(ctx, req.Inputs.Host, req.Inputs.Username, req.Inputs.Password)
	if err != nil {
		return infer.ReadResponse[RouteMapArgs, RouteMapState]{}, err
	}
	current, found, err := readRouteMap(ctx, cli, req.Inputs.Name)
	if err != nil {
		return infer.ReadResponse[RouteMapArgs, RouteMapState]{}, err
	}
	if !found {
		return infer.ReadResponse[RouteMapArgs, RouteMapState]{}, nil
	}
	state := RouteMapState{RouteMapArgs: req.Inputs}
	state.Entries = current
	return infer.ReadResponse[RouteMapArgs, RouteMapState]{
		ID:     routeMapID(req.Inputs.Name),
		Inputs: req.Inputs,
		State:  state,
	}, nil
}

// Update re-applies the route-map: negate-then-rebuild ensures stale
// sequence blocks from a previous version are gone before the new set
// renders.
func (*RouteMap) Update(ctx context.Context, req infer.UpdateRequest[RouteMapArgs, RouteMapState]) (infer.UpdateResponse[RouteMapState], error) {
	if err := validateRouteMap(req.Inputs); err != nil {
		return infer.UpdateResponse[RouteMapState]{}, err
	}
	state := RouteMapState{RouteMapArgs: req.Inputs}
	if req.DryRun {
		return infer.UpdateResponse[RouteMapState]{Output: state}, nil
	}
	if err := applyRouteMap(ctx, req.Inputs, false); err != nil {
		return infer.UpdateResponse[RouteMapState]{}, fmt.Errorf("update route-map %s: %w", req.Inputs.Name, err)
	}
	return infer.UpdateResponse[RouteMapState]{Output: state}, nil
}

// Delete removes the entire named route-map.
func (*RouteMap) Delete(ctx context.Context, req infer.DeleteRequest[RouteMapState]) (infer.DeleteResponse, error) {
	if err := applyRouteMap(ctx, req.State.RouteMapArgs, true); err != nil {
		return infer.DeleteResponse{}, fmt.Errorf("delete route-map %s: %w", req.State.Name, err)
	}
	return infer.DeleteResponse{}, nil
}

// --- helpers ---------------------------------------------------------------

func validateRouteMap(args RouteMapArgs) error {
	if strings.TrimSpace(args.Name) == "" {
		return ErrRouteMapNameRequired
	}
	if len(args.Entries) == 0 {
		return ErrRouteMapEntriesEmpty
	}
	seen := make(map[int]struct{}, len(args.Entries))
	for i := range args.Entries {
		e := &args.Entries[i]
		if err := validateRouteMapEntry(e); err != nil {
			return fmt.Errorf("seq %d: %w", e.Seq, err)
		}
		if _, dup := seen[e.Seq]; dup {
			return fmt.Errorf("%w: %d", ErrRouteMapSeqDuplicate, e.Seq)
		}
		seen[e.Seq] = struct{}{}
	}
	return nil
}

func validateRouteMapEntry(e *RouteMapEntry) error {
	if e.Seq < 0 || e.Seq > 65535 {
		return fmt.Errorf("%w: got %d", ErrRouteMapSeqOutOfRange, e.Seq)
	}
	if e.Action != "permit" && e.Action != "deny" {
		return fmt.Errorf("%w: got %q", ErrRouteMapActionInvalid, e.Action)
	}
	if e.Continue != nil && (*e.Continue < 0 || *e.Continue > 65535) {
		return fmt.Errorf("%w: got %d", ErrRouteMapContinueOutRange, *e.Continue)
	}
	if e.Match != nil {
		if err := validateRouteMapMatch(e.Match); err != nil {
			return err
		}
	}
	if e.Set != nil {
		if err := validateRouteMapSet(e.Set); err != nil {
			return err
		}
	}
	return nil
}

func validateRouteMapMatch(m *RouteMapMatch) error {
	if m.Tag != nil && (*m.Tag < 0 || *m.Tag > 4294967295) {
		return fmt.Errorf("%w: got %d", ErrRouteMapTagOutOfRange, *m.Tag)
	}
	if m.Metric != nil && *m.Metric < 0 {
		return fmt.Errorf("%w: got %d", ErrRouteMapMetricMatchRange, *m.Metric)
	}
	if m.LocalPreference != nil && *m.LocalPreference < 0 {
		return fmt.Errorf("%w: got %d", ErrRouteMapLocalPrefRange, *m.LocalPreference)
	}
	if m.SourceProtocol != nil && *m.SourceProtocol != "" {
		if _, ok := validRouteMapSourceProtocol[*m.SourceProtocol]; !ok {
			return fmt.Errorf("%w: got %q", ErrRouteMapSourceProtocol, *m.SourceProtocol)
		}
	}
	return nil
}

func validateRouteMapSet(s *RouteMapSet) error {
	additive := s.CommunityAdditive != nil && *s.CommunityAdditive
	none := s.CommunityNone != nil && *s.CommunityNone
	if additive && none {
		return ErrRouteMapCommunityCombo
	}
	if s.LocalPreference != nil && *s.LocalPreference < 0 {
		return fmt.Errorf("%w: got %d", ErrRouteMapLocalPrefRange, *s.LocalPreference)
	}
	if s.Tag != nil && (*s.Tag < 0 || *s.Tag > 4294967295) {
		return fmt.Errorf("%w: got %d", ErrRouteMapTagOutOfRange, *s.Tag)
	}
	for _, asn := range s.AsPathPrepend {
		if asn < 1 || asn > 4294967295 {
			return fmt.Errorf("%w: got %d", ErrRouteMapAsnInvalid, asn)
		}
	}
	if s.IpNextHop != nil && *s.IpNextHop != "" {
		if !validRouteMapNextHop(*s.IpNextHop) {
			return fmt.Errorf("%w: %q", ErrRouteMapNextHopInvalid, *s.IpNextHop)
		}
	}
	if s.Metric != nil && *s.Metric != "" {
		if !validRouteMapSetMetric(*s.Metric) {
			return fmt.Errorf("%w: got %q", ErrRouteMapMetricFormat, *s.Metric)
		}
	}
	if s.Origin != nil && *s.Origin != "" {
		if _, ok := validRouteMapOrigin[*s.Origin]; !ok {
			return fmt.Errorf("%w: got %q", ErrRouteMapOriginInvalid, *s.Origin)
		}
	}
	return nil
}

// validRouteMapNextHop accepts an IPv4 address or the special keywords
// `unchanged` / `self`.
func validRouteMapNextHop(s string) bool {
	if s == "unchanged" || s == "self" {
		return true
	}
	addr, err := netip.ParseAddr(s)
	return err == nil && addr.Is4()
}

// validRouteMapSetMetric accepts a positive integer or a signed delta
// (`+N` / `-N`).
func validRouteMapSetMetric(s string) bool {
	if s == "" {
		return false
	}
	body, signed := strings.CutPrefix(s, "+")
	if !signed {
		body, signed = strings.CutPrefix(s, "-")
	}
	v, err := strconv.Atoi(body)
	if err != nil {
		return false
	}
	if signed {
		return v >= 0
	}
	return v >= 0
}

func routeMapID(name string) string { return "route-map/" + name }

func applyRouteMap(ctx context.Context, args RouteMapArgs, remove bool) error {
	cli, err := newClient(ctx, args.Host, args.Username, args.Password)
	if err != nil {
		return err
	}
	cfg := config.FromContext(ctx)
	sessName := cfg.SessionPrefix() + "rmap-" + sanitizePrefixListName(args.Name)

	sess, err := cli.OpenSession(ctx, sessName)
	if err != nil {
		return err
	}
	cmds := buildRouteMapCmds(args, remove)
	if stageErr := sess.Stage(ctx, cmds); stageErr != nil {
		if abortErr := sess.Abort(ctx); abortErr != nil {
			return errors.Join(stageErr, abortErr)
		}
		return stageErr
	}
	return sess.Commit(ctx)
}

// buildRouteMapCmds renders the staged CLI block.
//
// Order: negate-then-rebuild for the same reason as PrefixList — stale
// sequence numbers from a previous version cannot leak. Inside each
// sequence, match clauses render before set clauses, with each clause
// emitted in a fixed canonical order so the diff against
// `show running-config | section route-map` is deterministic.
func buildRouteMapCmds(args RouteMapArgs, remove bool) []string {
	if remove {
		return []string{"no route-map " + args.Name}
	}
	cmds := []string{"no route-map " + args.Name}
	entries := append([]RouteMapEntry(nil), args.Entries...)
	sort.Slice(entries, func(i, j int) bool { return entries[i].Seq < entries[j].Seq })
	for _, e := range entries {
		cmds = append(cmds, fmt.Sprintf("route-map %s %s %d", args.Name, e.Action, e.Seq))
		cmds = append(cmds, routeMapMatchCmds(e.Match)...)
		cmds = append(cmds, routeMapSetCmds(e.Set)...)
		if e.Continue != nil {
			cmds = append(cmds, "continue "+strconv.Itoa(*e.Continue))
		}
		cmds = append(cmds, "exit")
	}
	return cmds
}

func routeMapMatchCmds(m *RouteMapMatch) []string {
	if m == nil {
		return nil
	}
	var cmds []string
	if len(m.IpAddressPrefixList) > 0 {
		cmds = append(cmds, "match ip address prefix-list "+strings.Join(m.IpAddressPrefixList, " "))
	}
	if len(m.AsPath) > 0 {
		cmds = append(cmds, "match as-path "+strings.Join(m.AsPath, " "))
	}
	if len(m.Community) > 0 {
		cmds = append(cmds, "match community "+strings.Join(m.Community, " "))
	}
	if len(m.Extcommunity) > 0 {
		cmds = append(cmds, "match extcommunity "+strings.Join(m.Extcommunity, " "))
	}
	if m.Interface != nil && *m.Interface != "" {
		cmds = append(cmds, "match interface "+*m.Interface)
	}
	if m.LocalPreference != nil {
		cmds = append(cmds, "match local-preference "+strconv.Itoa(*m.LocalPreference))
	}
	if m.Metric != nil {
		cmds = append(cmds, "match metric "+strconv.Itoa(*m.Metric))
	}
	if m.SourceProtocol != nil && *m.SourceProtocol != "" {
		cmds = append(cmds, "match source-protocol "+*m.SourceProtocol)
	}
	if m.Tag != nil {
		cmds = append(cmds, "match tag "+strconv.Itoa(*m.Tag))
	}
	return cmds
}

func routeMapSetCmds(s *RouteMapSet) []string {
	if s == nil {
		return nil
	}
	var cmds []string
	if len(s.AsPathPrepend) > 0 {
		parts := make([]string, 0, len(s.AsPathPrepend))
		for _, asn := range s.AsPathPrepend {
			parts = append(parts, strconv.Itoa(asn))
		}
		cmds = append(cmds, "set as-path prepend "+strings.Join(parts, " "))
	}
	switch {
	case s.CommunityNone != nil && *s.CommunityNone:
		cmds = append(cmds, "set community none")
	case len(s.Community) > 0:
		line := "set community " + strings.Join(s.Community, " ")
		if s.CommunityAdditive != nil && *s.CommunityAdditive {
			line += " additive"
		}
		cmds = append(cmds, line)
	}
	rtAdditive := s.ExtcommunityRtAdditive != nil && *s.ExtcommunityRtAdditive
	for _, rt := range s.ExtcommunityRt {
		line := "set extcommunity rt " + rt
		if rtAdditive {
			line += " additive"
		}
		cmds = append(cmds, line)
	}
	if s.IpNextHop != nil && *s.IpNextHop != "" {
		cmds = append(cmds, "set ip next-hop "+*s.IpNextHop)
	}
	if s.LocalPreference != nil {
		cmds = append(cmds, "set local-preference "+strconv.Itoa(*s.LocalPreference))
	}
	if s.Metric != nil && *s.Metric != "" {
		cmds = append(cmds, "set metric "+*s.Metric)
	}
	if s.Origin != nil && *s.Origin != "" {
		cmds = append(cmds, "set origin "+*s.Origin)
	}
	if s.Tag != nil {
		cmds = append(cmds, "set tag "+strconv.Itoa(*s.Tag))
	}
	return cmds
}

// readRouteMap returns the live route-map sequences, or (nil, false,
// nil) when no map with the given name exists.
func readRouteMap(ctx context.Context, cli *eapi.Client, name string) ([]RouteMapEntry, bool, error) {
	resp, err := cli.RunCmds(ctx,
		[]string{"show running-config | section route-map " + name},
		"text")
	if err != nil {
		return nil, false, err
	}
	if len(resp) == 0 {
		return nil, false, nil
	}
	out, _ := resp[0]["output"].(string)
	if out == "" || !strings.Contains(out, "route-map "+name+" ") {
		return nil, false, nil
	}
	return parseRouteMapSection(out, name), true, nil
}

// parseRouteMapSection extracts entries from a `route-map <name> ...`
// section block. v0 read covers the same scalar fields as the render;
// list-valued match/set lines are stored as slice values.
func parseRouteMapSection(out, name string) []RouteMapEntry {
	header := "route-map " + name + " "
	var entries []RouteMapEntry
	var cur *RouteMapEntry
	for raw := range strings.SplitSeq(out, "\n") {
		line := strings.TrimSpace(raw)
		if rest, ok := strings.CutPrefix(line, header); ok {
			if cur != nil {
				entries = append(entries, *cur)
			}
			cur = parseRouteMapHeader(rest)
			continue
		}
		if cur == nil {
			continue
		}
		applyRouteMapClauseLine(cur, line)
	}
	if cur != nil {
		entries = append(entries, *cur)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Seq < entries[j].Seq })
	return entries
}

// parseRouteMapHeader pulls action + seq out of `<action> <seq>`.
func parseRouteMapHeader(rest string) *RouteMapEntry {
	tokens := strings.Fields(rest)
	if len(tokens) < 2 {
		return nil
	}
	seq, err := strconv.Atoi(tokens[1])
	if err != nil {
		return nil
	}
	if tokens[0] != "permit" && tokens[0] != "deny" {
		return nil
	}
	return &RouteMapEntry{Seq: seq, Action: tokens[0]}
}

// applyRouteMapClauseLine routes one in-block line into the right
// match/set/continue field on cur.
func applyRouteMapClauseLine(cur *RouteMapEntry, line string) {
	switch {
	case strings.HasPrefix(line, "match "):
		if cur.Match == nil {
			cur.Match = &RouteMapMatch{}
		}
		applyRouteMapMatchLine(cur.Match, strings.TrimPrefix(line, "match "))
	case strings.HasPrefix(line, "set "):
		if cur.Set == nil {
			cur.Set = &RouteMapSet{}
		}
		applyRouteMapSetLine(cur.Set, strings.TrimPrefix(line, "set "))
	case strings.HasPrefix(line, "continue"):
		if rest, ok := strings.CutPrefix(line, "continue "); ok {
			if v, err := strconv.Atoi(rest); err == nil {
				cur.Continue = &v
			}
		}
	}
}

// applyRouteMapMatchLine populates one Match field from a `match ...`
// trailing payload.
func applyRouteMapMatchLine(m *RouteMapMatch, payload string) {
	switch {
	case strings.HasPrefix(payload, "ip address prefix-list "):
		m.IpAddressPrefixList = strings.Fields(strings.TrimPrefix(payload, "ip address prefix-list "))
	case strings.HasPrefix(payload, "as-path "):
		m.AsPath = strings.Fields(strings.TrimPrefix(payload, "as-path "))
	case strings.HasPrefix(payload, "community "):
		m.Community = strings.Fields(strings.TrimPrefix(payload, "community "))
	case strings.HasPrefix(payload, "extcommunity "):
		m.Extcommunity = strings.Fields(strings.TrimPrefix(payload, "extcommunity "))
	case strings.HasPrefix(payload, "interface "):
		v := strings.TrimPrefix(payload, "interface ")
		m.Interface = &v
	case strings.HasPrefix(payload, "tag "):
		if v, err := strconv.Atoi(strings.TrimPrefix(payload, "tag ")); err == nil {
			m.Tag = &v
		}
	case strings.HasPrefix(payload, "metric "):
		if v, err := strconv.Atoi(strings.TrimPrefix(payload, "metric ")); err == nil {
			m.Metric = &v
		}
	case strings.HasPrefix(payload, "local-preference "):
		if v, err := strconv.Atoi(strings.TrimPrefix(payload, "local-preference ")); err == nil {
			m.LocalPreference = &v
		}
	case strings.HasPrefix(payload, "source-protocol "):
		v := strings.TrimPrefix(payload, "source-protocol ")
		m.SourceProtocol = &v
	}
}

// applyRouteMapSetLine populates one Set field from a `set ...`
// trailing payload.
func applyRouteMapSetLine(s *RouteMapSet, payload string) {
	switch {
	case strings.HasPrefix(payload, "as-path prepend "):
		tokens := strings.Fields(strings.TrimPrefix(payload, "as-path prepend "))
		s.AsPathPrepend = make([]int, 0, len(tokens))
		for _, t := range tokens {
			if v, err := strconv.Atoi(t); err == nil {
				s.AsPathPrepend = append(s.AsPathPrepend, v)
			}
		}
	case payload == "community none":
		t := true
		s.CommunityNone = &t
	case strings.HasPrefix(payload, "community "):
		body := strings.TrimPrefix(payload, "community ")
		if rest, hasAdd := strings.CutSuffix(body, " additive"); hasAdd {
			s.Community = strings.Fields(rest)
			t := true
			s.CommunityAdditive = &t
		} else {
			s.Community = strings.Fields(body)
		}
	case strings.HasPrefix(payload, "extcommunity rt "):
		body := strings.TrimPrefix(payload, "extcommunity rt ")
		if rt, hasAdd := strings.CutSuffix(body, " additive"); hasAdd {
			s.ExtcommunityRt = append(s.ExtcommunityRt, rt)
			t := true
			s.ExtcommunityRtAdditive = &t
		} else {
			s.ExtcommunityRt = append(s.ExtcommunityRt, body)
		}
	case strings.HasPrefix(payload, "ip next-hop "):
		v := strings.TrimPrefix(payload, "ip next-hop ")
		s.IpNextHop = &v
	case strings.HasPrefix(payload, "local-preference "):
		if v, err := strconv.Atoi(strings.TrimPrefix(payload, "local-preference ")); err == nil {
			s.LocalPreference = &v
		}
	case strings.HasPrefix(payload, "metric "):
		v := strings.TrimPrefix(payload, "metric ")
		s.Metric = &v
	case strings.HasPrefix(payload, "origin "):
		v := strings.TrimPrefix(payload, "origin ")
		s.Origin = &v
	case strings.HasPrefix(payload, "tag "):
		if v, err := strconv.Atoi(strings.TrimPrefix(payload, "tag ")); err == nil {
			s.Tag = &v
		}
	}
}
