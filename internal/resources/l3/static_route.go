package l3

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"strconv"
	"strings"

	"github.com/pulumi/pulumi-go-provider/infer"

	"github.com/dantte-lp/pulumi-eos/internal/client/eapi"
	"github.com/dantte-lp/pulumi-eos/internal/config"
)

// Sentinel errors specific to StaticRoute.
var (
	ErrStaticRouteBadPrefix    = errors.New("staticRoute prefix must be a valid IPv4 CIDR")
	ErrStaticRouteBadNextHop   = errors.New("staticRoute nextHop must be an IPv4 address or an EOS interface name")
	ErrStaticRouteBadDistance  = errors.New("staticRoute distance must be in 1..255")
	ErrStaticRouteBadTag       = errors.New("staticRoute tag must be in 0..4294967295")
	ErrStaticRouteBadMetric    = errors.New("staticRoute metric must be > 0")
	ErrStaticRouteEmptyText    = errors.New("staticRoute name / track must be non-empty when set")
	ErrStaticRouteSpacesInText = errors.New("staticRoute name / track must not contain spaces")
)

// staticRouteIfaceRe matches the EOS interface forms accepted as
// next-hop on `ip route`. Verified live against cEOS 4.36.0.1F:
// `Null0`, `Ethernet<N>[/<m>...]`, `Loopback<N>`, `Management<N>`,
// `Port-Channel<N>`, `Vlan<N>`, `Vxlan<N>`. Sub-interfaces (`.<sub>`)
// are accepted when the parent already exists.
var staticRouteIfaceRe = regexp.MustCompile(
	`^(Null0|Ethernet\d+(\/\d+)*(\.\d+)?|Loopback\d+|Management\d+|Port-Channel\d+(\.\d+)?|Vlan\d+|Vxlan\d+)$`)

// StaticRoute models an EOS IPv4 static route — `ip route [vrf X]
// <prefix> <next-hop> [<distance>] [tag N] [name S] [metric M]
// [track X]`. Multiple routes with the same destination but different
// next-hop or distance comprise an ECMP / floating-route set; the
// composite ID encodes that identity so each row tracks separately.
//
// Source: EOS User Manual §14.1.14.65 (`ip route`); validated live
// against cEOS 4.36.0.1F.
type StaticRoute struct{}

// StaticRouteArgs is the input set.
type StaticRouteArgs struct {
	// Prefix is the destination network in IPv4 CIDR notation
	// (e.g. `10.99.0.0/24`).
	Prefix string `pulumi:"prefix"`
	// NextHop is the next-hop IPv4 address or EOS interface name
	// (e.g. `192.0.2.1`, `Null0`, `Ethernet1`, `Loopback0`).
	NextHop string `pulumi:"nextHop"`
	// Vrf places the route in a non-default VRF. The VRF must exist
	// (created by `eos:l3:Vrf`) before EOS will accept the route.
	Vrf *string `pulumi:"vrf,optional"`
	// Distance is the administrative distance (1..255). Defaults to 1.
	Distance *int `pulumi:"distance,optional"`
	// Tag is an optional route tag (0..4294967295). Defaults to 0 and
	// is not rendered when zero.
	Tag *int `pulumi:"tag,optional"`
	// Name is an optional descriptive label. Spaces are rejected.
	Name *string `pulumi:"name,optional"`
	// Metric is an optional metric (> 0).
	Metric *int `pulumi:"metric,optional"`
	// Track is an optional object-tracking name (no spaces).
	Track *string `pulumi:"track,optional"`

	// Host overrides the provider-level eosUrl host for this resource.
	Host *string `pulumi:"host,optional"`
	// Username overrides the provider-level eosUsername for this resource.
	Username *string `pulumi:"username,optional"`
	// Password overrides the provider-level eosPassword for this resource.
	Password *string `provider:"secret" pulumi:"password,optional"`
}

// StaticRouteState mirrors Args.
type StaticRouteState struct {
	StaticRouteArgs
}

// Annotate documents the resource.
func (r *StaticRoute) Annotate(a infer.Annotator) {
	a.Describe(&r, "An EOS IPv4 static route. Composite identity (vrf + prefix + nextHop + distance) lets ECMP / floating-route sets coexist.")
}

// Annotate documents StaticRouteArgs fields.
func (a *StaticRouteArgs) Annotate(an infer.Annotator) {
	an.Describe(&a.Prefix, "Destination network in IPv4 CIDR (e.g. 10.99.0.0/24).")
	an.Describe(&a.NextHop, "Next-hop IPv4 address or EOS interface (e.g. 192.0.2.1, Null0, Ethernet1, Loopback0).")
	an.Describe(&a.Vrf, "Optional non-default VRF binding. The VRF must exist before the route applies.")
	an.Describe(&a.Distance, "Administrative distance (1..255). Defaults to 1.")
	an.Describe(&a.Tag, "Route tag (0..4294967295).")
	an.Describe(&a.Name, "Descriptive label (no spaces).")
	an.Describe(&a.Metric, "Route metric (> 0).")
	an.Describe(&a.Track, "Object-tracking name (no spaces).")
	an.Describe(&a.Host, "Optional management hostname override.")
	an.Describe(&a.Username, "Optional AAA username override.")
	an.Describe(&a.Password, "Optional AAA password override.")
}

// Annotate is a no-op; State has no extra fields.
func (s *StaticRouteState) Annotate(_ infer.Annotator) {}

// Create configures the static route.
func (*StaticRoute) Create(ctx context.Context, req infer.CreateRequest[StaticRouteArgs]) (infer.CreateResponse[StaticRouteState], error) {
	if err := validateStaticRoute(req.Inputs); err != nil {
		return infer.CreateResponse[StaticRouteState]{}, err
	}
	state := StaticRouteState{StaticRouteArgs: req.Inputs}
	if req.DryRun {
		return infer.CreateResponse[StaticRouteState]{ID: staticRouteID(req.Inputs), Output: state}, nil
	}
	if err := applyStaticRoute(ctx, req.Inputs, false); err != nil {
		return infer.CreateResponse[StaticRouteState]{}, fmt.Errorf("create static route %s: %w", staticRouteID(req.Inputs), err)
	}
	return infer.CreateResponse[StaticRouteState]{ID: staticRouteID(req.Inputs), Output: state}, nil
}

// Read refreshes static-route state from the device.
func (*StaticRoute) Read(ctx context.Context, req infer.ReadRequest[StaticRouteArgs, StaticRouteState]) (infer.ReadResponse[StaticRouteArgs, StaticRouteState], error) {
	cli, err := newClient(ctx, req.Inputs.Host, req.Inputs.Username, req.Inputs.Password)
	if err != nil {
		return infer.ReadResponse[StaticRouteArgs, StaticRouteState]{}, err
	}
	current, found, err := readStaticRoute(ctx, cli, req.Inputs)
	if err != nil {
		return infer.ReadResponse[StaticRouteArgs, StaticRouteState]{}, err
	}
	if !found {
		return infer.ReadResponse[StaticRouteArgs, StaticRouteState]{}, nil
	}
	state := StaticRouteState{StaticRouteArgs: req.Inputs}
	current.fillState(&state)
	return infer.ReadResponse[StaticRouteArgs, StaticRouteState]{
		ID:     staticRouteID(req.Inputs),
		Inputs: req.Inputs,
		State:  state,
	}, nil
}

// Update re-applies the static route. Because composite identity
// includes prefix/next-hop/distance/vrf, structural changes flow
// through replace, not update; non-structural fields (tag, name,
// metric, track) are mutable in place.
func (*StaticRoute) Update(ctx context.Context, req infer.UpdateRequest[StaticRouteArgs, StaticRouteState]) (infer.UpdateResponse[StaticRouteState], error) {
	if err := validateStaticRoute(req.Inputs); err != nil {
		return infer.UpdateResponse[StaticRouteState]{}, err
	}
	state := StaticRouteState{StaticRouteArgs: req.Inputs}
	if req.DryRun {
		return infer.UpdateResponse[StaticRouteState]{Output: state}, nil
	}
	if err := applyStaticRoute(ctx, req.Inputs, false); err != nil {
		return infer.UpdateResponse[StaticRouteState]{}, fmt.Errorf("update static route %s: %w", staticRouteID(req.Inputs), err)
	}
	return infer.UpdateResponse[StaticRouteState]{Output: state}, nil
}

// Delete removes the static route.
func (*StaticRoute) Delete(ctx context.Context, req infer.DeleteRequest[StaticRouteState]) (infer.DeleteResponse, error) {
	if err := applyStaticRoute(ctx, req.State.StaticRouteArgs, true); err != nil {
		return infer.DeleteResponse{}, fmt.Errorf("delete static route %s: %w", staticRouteID(req.State.StaticRouteArgs), err)
	}
	return infer.DeleteResponse{}, nil
}

// --- helpers ---------------------------------------------------------------

func validateStaticRoute(args StaticRouteArgs) error {
	if pfx, err := netip.ParsePrefix(args.Prefix); err != nil || !pfx.Addr().Is4() {
		return fmt.Errorf("%w: %q", ErrStaticRouteBadPrefix, args.Prefix)
	}
	if !validStaticRouteNextHop(args.NextHop) {
		return fmt.Errorf("%w: %q", ErrStaticRouteBadNextHop, args.NextHop)
	}
	if args.Distance != nil && (*args.Distance < 1 || *args.Distance > 255) {
		return fmt.Errorf("%w: got %d", ErrStaticRouteBadDistance, *args.Distance)
	}
	if args.Tag != nil && (*args.Tag < 0 || *args.Tag > 4294967295) {
		return fmt.Errorf("%w: got %d", ErrStaticRouteBadTag, *args.Tag)
	}
	if args.Metric != nil && *args.Metric <= 0 {
		return fmt.Errorf("%w: got %d", ErrStaticRouteBadMetric, *args.Metric)
	}
	if err := validateStaticRouteText(args.Name, "name"); err != nil {
		return err
	}
	if err := validateStaticRouteText(args.Track, "track"); err != nil {
		return err
	}
	return nil
}

// validStaticRouteNextHop accepts an IPv4 address (Is4) or one of the
// recognised EOS interface forms.
func validStaticRouteNextHop(s string) bool {
	if addr, err := netip.ParseAddr(s); err == nil && addr.Is4() {
		return true
	}
	return staticRouteIfaceRe.MatchString(s)
}

func validateStaticRouteText(p *string, field string) error {
	if p == nil {
		return nil
	}
	if *p == "" {
		return fmt.Errorf("%w: %s", ErrStaticRouteEmptyText, field)
	}
	if strings.ContainsAny(*p, " \t") {
		return fmt.Errorf("%w: %s=%q", ErrStaticRouteSpacesInText, field, *p)
	}
	return nil
}

func staticRouteID(args StaticRouteArgs) string {
	vrf := "default"
	if args.Vrf != nil && *args.Vrf != "" {
		vrf = *args.Vrf
	}
	dist := 1
	if args.Distance != nil {
		dist = *args.Distance
	}
	return fmt.Sprintf("route/%s/%s/%s/%d", vrf, args.Prefix, args.NextHop, dist)
}

func applyStaticRoute(ctx context.Context, args StaticRouteArgs, remove bool) error {
	cli, err := newClient(ctx, args.Host, args.Username, args.Password)
	if err != nil {
		return err
	}
	cfg := config.FromContext(ctx)
	sessName := cfg.SessionPrefix() + "route-" + staticRouteSessionToken(args)

	sess, err := cli.OpenSession(ctx, sessName)
	if err != nil {
		return err
	}
	cmds := buildStaticRouteCmds(args, remove)
	if stageErr := sess.Stage(ctx, cmds); stageErr != nil {
		if abortErr := sess.Abort(ctx); abortErr != nil {
			return errors.Join(stageErr, abortErr)
		}
		return stageErr
	}
	return sess.Commit(ctx)
}

// staticRouteSessionToken derives a session-name-safe slug from the
// composite identity. EOS rejects `/`, `:`, ` `, `.` in session names.
func staticRouteSessionToken(args StaticRouteArgs) string {
	vrf := "default"
	if args.Vrf != nil && *args.Vrf != "" {
		vrf = *args.Vrf
	}
	r := strings.NewReplacer("/", "-", ".", "-", ":", "-", " ", "-")
	dist := 1
	if args.Distance != nil {
		dist = *args.Distance
	}
	return r.Replace(vrf) + "-" + r.Replace(args.Prefix) + "-" + r.Replace(args.NextHop) + "-" + strconv.Itoa(dist)
}

// buildStaticRouteCmds renders the staged `ip route` line.
//
// EOS render order verified live against cEOS 4.36.0.1F:
//
//	ip route [vrf X] <prefix> <next-hop> [<distance>] [tag N] [name S] [metric M] [track X]
//
// Distance is a bare positional argument; tag / name / metric / track
// are keyword-prefixed and order-independent on the device but we
// emit them in this canonical order so re-applies are idempotent
// against `show running-config`.
func buildStaticRouteCmds(args StaticRouteArgs, remove bool) []string {
	parts := []string{"ip", "route"}
	if args.Vrf != nil && *args.Vrf != "" {
		parts = append(parts, "vrf", *args.Vrf)
	}
	parts = append(parts, args.Prefix, args.NextHop)
	if args.Distance != nil && *args.Distance != 1 {
		parts = append(parts, strconv.Itoa(*args.Distance))
	}

	if remove {
		// `no ip route [vrf X] <prefix> <next-hop> [<distance>]` is
		// the documented delete form (EOS User Manual §14.1.14.65).
		// Trailing tag/name/metric/track are not echoed in `no`.
		return []string{"no " + strings.Join(parts, " ")}
	}

	if args.Tag != nil && *args.Tag != 0 {
		parts = append(parts, "tag", strconv.Itoa(*args.Tag))
	}
	if args.Name != nil && *args.Name != "" {
		parts = append(parts, "name", *args.Name)
	}
	if args.Metric != nil && *args.Metric > 0 {
		parts = append(parts, "metric", strconv.Itoa(*args.Metric))
	}
	if args.Track != nil && *args.Track != "" {
		parts = append(parts, "track", *args.Track)
	}
	return []string{strings.Join(parts, " ")}
}

// staticRouteRow is the parsed live state we care about.
type staticRouteRow struct {
	Distance int
	Tag      int
	Name     string
	Metric   int
	Track    string
}

// readStaticRoute returns the live route state, or (false, nil) when
// absent. Source: `show running-config | grep ip` text — EOS's pipe
// filter accepts only a single-word substring so we cast a wider net
// and filter the `ip route ...` rows in `parseStaticRouteLines`.
func readStaticRoute(ctx context.Context, cli *eapi.Client, args StaticRouteArgs) (staticRouteRow, bool, error) {
	resp, err := cli.RunCmds(ctx,
		[]string{"show running-config | grep ip"},
		"text")
	if err != nil {
		return staticRouteRow{}, false, err
	}
	if len(resp) == 0 {
		return staticRouteRow{}, false, nil
	}
	out, _ := resp[0]["output"].(string)
	row, found := parseStaticRouteLines(out, args)
	return row, found, nil
}

// parseStaticRouteLines walks `show running-config | grep ^ip route`
// output and returns the row whose vrf+prefix+next-hop matches.
// Exposed for unit tests.
func parseStaticRouteLines(out string, args StaticRouteArgs) (staticRouteRow, bool) {
	wantVrf := ""
	if args.Vrf != nil && *args.Vrf != "" {
		wantVrf = *args.Vrf
	}
	for raw := range strings.SplitSeq(out, "\n") {
		line := strings.TrimSpace(raw)
		if !strings.HasPrefix(line, "ip route ") {
			continue
		}
		row, vrf, prefix, nh, ok := parseStaticRouteLine(line)
		if !ok {
			continue
		}
		if vrf != wantVrf || prefix != args.Prefix || nh != args.NextHop {
			continue
		}
		return row, true
	}
	return staticRouteRow{}, false
}

// parseStaticRouteLine pulls (vrf, prefix, next-hop) plus the trailing
// optional knobs out of a single `ip route` line.
func parseStaticRouteLine(line string) (staticRouteRow, string, string, string, bool) {
	tokens := strings.Fields(line)
	// "ip route [vrf X] <prefix> <next-hop> [<dist>] [tag N] [name S] [metric M] [track X]"
	if len(tokens) < 4 || tokens[0] != "ip" || tokens[1] != "route" {
		return staticRouteRow{}, "", "", "", false
	}
	idx := 2
	vrf := ""
	if tokens[idx] == "vrf" && idx+1 < len(tokens) {
		vrf = tokens[idx+1]
		idx += 2
	}
	if idx+1 >= len(tokens) {
		return staticRouteRow{}, "", "", "", false
	}
	prefix := tokens[idx]
	nh := tokens[idx+1]
	idx += 2

	row := staticRouteRow{Distance: 1}
	if idx < len(tokens) {
		if v, err := strconv.Atoi(tokens[idx]); err == nil && v >= 1 && v <= 255 {
			row.Distance = v
			idx++
		}
	}
	for idx < len(tokens)-1 {
		key := tokens[idx]
		val := tokens[idx+1]
		switch key {
		case "tag":
			if v, err := strconv.Atoi(val); err == nil {
				row.Tag = v
			}
		case "name":
			row.Name = val
		case "metric":
			if v, err := strconv.Atoi(val); err == nil {
				row.Metric = v
			}
		case "track":
			row.Track = val
		}
		idx += 2
	}
	return row, vrf, prefix, nh, true
}

func (r staticRouteRow) fillState(s *StaticRouteState) {
	if r.Distance > 0 && r.Distance != 1 {
		v := r.Distance
		s.Distance = &v
	}
	if r.Tag > 0 {
		v := r.Tag
		s.Tag = &v
	}
	if r.Name != "" {
		v := r.Name
		s.Name = &v
	}
	if r.Metric > 0 {
		v := r.Metric
		s.Metric = &v
	}
	if r.Track != "" {
		v := r.Track
		s.Track = &v
	}
}
