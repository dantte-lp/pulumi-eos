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

// Sentinel errors specific to RouterOspf.
var (
	ErrRouterOspfInstanceRange      = errors.New("routerOspf instance must be in 1..65535")
	ErrRouterOspfRouterIDInvalid    = errors.New("routerOspf routerId must be a valid IPv4 address")
	ErrRouterOspfPrefixInvalid      = errors.New("routerOspf network prefix must be a valid IPv4 CIDR")
	ErrRouterOspfAreaInvalid        = errors.New("routerOspf area must be a non-negative integer or dotted-quad")
	ErrRouterOspfAreaTypeInvalid    = errors.New("routerOspf area type must be normal | stub | stub-no-summary | nssa | nssa-default-information-originate")
	ErrRouterOspfRedistSrcInvalid   = errors.New("routerOspf redistribute source must be connected | static | bgp | isis")
	ErrRouterOspfMaximumPathsRange  = errors.New("routerOspf maximumPaths must be in 1..128")
	ErrRouterOspfMetricTypeInvalid  = errors.New("routerOspf metric-type must be 1 or 2")
	ErrRouterOspfDistanceRange      = errors.New("routerOspf distance values must be in 1..255")
	ErrRouterOspfLogChangesInvalid  = errors.New("routerOspf logAdjacencyChanges must be empty | off | on | detail")
	ErrRouterOspfTimerNonPositive   = errors.New("routerOspf timer values must be > 0")
	ErrRouterOspfMaxLsaNonPositive  = errors.New("routerOspf maxLsa must be > 0")
	ErrRouterOspfRefBwNonPositive   = errors.New("routerOspf autoCostReferenceBandwidth must be > 0 Mbps")
	ErrRouterOspfDefaultCostRange   = errors.New("routerOspf area defaultCost must be in 1..65535")
	ErrRouterOspfSummaryPrefixInvld = errors.New("routerOspf summaryAddress / area range entries must be valid IPv4 CIDRs")
)

// validRouterOspfAreaTypes enumerates the area-type tokens accepted on
// `area <id> ...`. v0 supports stub / NSSA variants verified against
// cEOS 4.36.0.1F via the eAPI probe (see test/integration/probe_ospf
// _test.go); mixed forms (`nssa no-redistribution`, `nssa no-summary
// default-information-originate`) were rejected on cEOS 4.36 and are
// deferred to v1.
var validRouterOspfAreaTypes = map[string]struct{}{
	"normal":                             {},
	"stub":                               {},
	"stub-no-summary":                    {},
	"nssa":                               {},
	"nssa-default-information-originate": {},
}

// validRouterOspfRedistSrc enumerates the source protocols permitted in
// `redistribute <source>` for v0.
var validRouterOspfRedistSrc = map[string]struct{}{
	"connected": {},
	"static":    {},
	"bgp":       {},
	"isis":      {},
}

// validRouterOspfLogChanges accepts an empty string (= leave default),
// "off", "on", or "detail".
var validRouterOspfLogChanges = map[string]struct{}{
	"":       {},
	"off":    {},
	"on":     {},
	"detail": {},
}

// dottedQuadRe matches an OSPF area identifier in dotted-quad form.
// Numeric area ids are also accepted at the input layer; the
// canonicaliser converts them to dotted-quad before render so the
// resource state matches cEOS' running-config representation.
var dottedQuadRe = regexp.MustCompile(`^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$`)

// RouterOspf models an OSPFv2 process.
//
// Identity is `router ospf <instance> [vrf <vrf>]`. v0 surface covers
// the day-zero leaf-spine fabric set:
//
//   - Process: instance (PK), vrf (optional, default VRF when empty),
//     routerId, shutdown, maxLsa, maximumPaths, autoCostReferenceBandwidth.
//   - Topology: networks [{prefix, area}], areas [{id, type, defaultCost,
//     ranges}], passiveInterfaceDefault + (no)passiveInterfaces.
//   - Information distribution: redistribute [{source, routeMap}],
//     defaultInformationOriginate, summaryAddresses.
//   - Behaviour: distance, logAdjacencyChanges, gracefulRestartHelper,
//     timers (spf delay initial, out-delay, pacing flood).
//
// Deferred for v1 (probed against cEOS 4.36 — flagged for follow-up):
// `area <id> nssa no-redistribution`, mixed `nssa no-summary default-
// information-originate`, `bfd all-interfaces` (different render form),
// `graceful-restart restart-period`, `area virtual-link`, `area filter
// prefix-list`, OSPFv3 (separate `eos:l3:RouterOspfv3` resource).
//
// Source: EOS User Manual §16.2 (OSPFv2 Commands); cEOS 4.36.0.1F live
// probe (see commit `3c13006`) for accepted line forms; double
// validation per docs/05-development.md rule 2.
type RouterOspf struct{}

// RouterOspfNetwork is one `network <prefix> area <area>` line.
type RouterOspfNetwork struct {
	// Prefix is the IPv4 network in CIDR form (`A.B.C.D/M`).
	Prefix string `pulumi:"prefix"`
	// Area is the OSPF area as a non-negative integer or dotted-quad;
	// rendered as dotted-quad to match running-config.
	Area string `pulumi:"area"`
}

// Annotate documents the shape.
func (n *RouterOspfNetwork) Annotate(an infer.Annotator) {
	an.Describe(&n.Prefix, "IPv4 CIDR (A.B.C.D/M).")
	an.Describe(&n.Area, "OSPF area id (integer or dotted-quad).")
}

// RouterOspfArea models per-area config beyond `network`.
type RouterOspfArea struct {
	// Id is the area identifier (integer or dotted-quad).
	Id string `pulumi:"id"`
	// Type controls area behaviour: normal (default), stub,
	// stub-no-summary (totally-stubby), nssa,
	// nssa-default-information-originate. nssa-no-summary mixed forms
	// rejected on cEOS 4.36 — deferred to v1.
	Type *string `pulumi:"type,optional"`
	// DefaultCost overrides the ABR-injected default summary cost
	// (1..65535).
	DefaultCost *int `pulumi:"defaultCost,optional"`
	// Ranges aggregates intra-area prefixes; rendered as
	// `area <id> range <prefix>`.
	Ranges []string `pulumi:"ranges,optional"`
	// NssaMetric pairs with type=nssa-default-information-originate.
	NssaMetric *int `pulumi:"nssaMetric,optional"`
	// NssaMetricType pairs with type=nssa-default-information-originate.
	NssaMetricType *int `pulumi:"nssaMetricType,optional"`
	// NssaOnly suppresses the type-7 LSA injection upstream when
	// type=nssa-default-information-originate.
	NssaOnly *bool `pulumi:"nssaOnly,optional"`
}

// Annotate documents the shape.
func (a *RouterOspfArea) Annotate(an infer.Annotator) {
	an.Describe(&a.Id, "Area identifier (integer or dotted-quad).")
	an.Describe(&a.Type, "Area type: normal (default), stub, stub-no-summary, nssa, nssa-default-information-originate.")
	an.Describe(&a.DefaultCost, "ABR default-route summary cost (1..65535).")
	an.Describe(&a.Ranges, "Intra-area prefix aggregates (CIDR list).")
	an.Describe(&a.NssaMetric, "type-7 metric for nssa-default-information-originate.")
	an.Describe(&a.NssaMetricType, "type-7 metric-type (1|2) for nssa-default-information-originate.")
	an.Describe(&a.NssaOnly, "Suppress type-5 leak upstream for nssa-default-information-originate.")
}

// RouterOspfRedistribute is one `redistribute <source>` line.
type RouterOspfRedistribute struct {
	// Source must be one of: connected | static | bgp | isis.
	Source string `pulumi:"source"`
	// RouteMap optionally filters the redistribution.
	RouteMap *string `pulumi:"routeMap,optional"`
}

// Annotate documents the shape.
func (r *RouterOspfRedistribute) Annotate(an infer.Annotator) {
	an.Describe(&r.Source, "Source protocol: connected | static | bgp | isis.")
	an.Describe(&r.RouteMap, "Optional route-map name to filter the redistribution.")
}

// RouterOspfDefaultInfoOrigin renders `default-information originate`.
type RouterOspfDefaultInfoOrigin struct {
	Metric     *int    `pulumi:"metric,optional"`
	MetricType *int    `pulumi:"metricType,optional"`
	RouteMap   *string `pulumi:"routeMap,optional"`
}

// Annotate documents the shape.
func (d *RouterOspfDefaultInfoOrigin) Annotate(an infer.Annotator) {
	an.Describe(&d.Metric, "Metric for the originated default route.")
	an.Describe(&d.MetricType, "Metric type: 1 or 2.")
	an.Describe(&d.RouteMap, "Conditional route-map name.")
}

// RouterOspfDistance carries optional administrative-distance overrides.
// Rendered as three separate lines on cEOS 4.36 (the combined
// single-line form is rejected — verified via probe).
type RouterOspfDistance struct {
	IntraArea *int `pulumi:"intraArea,optional"`
	InterArea *int `pulumi:"interArea,optional"`
	External  *int `pulumi:"external,optional"`
}

// Annotate documents the shape.
func (d *RouterOspfDistance) Annotate(an infer.Annotator) {
	an.Describe(&d.IntraArea, "Intra-area distance (1..255).")
	an.Describe(&d.InterArea, "Inter-area distance (1..255).")
	an.Describe(&d.External, "External distance (1..255).")
}

// RouterOspfTimers carries optional scheduling tweaks.
type RouterOspfTimers struct {
	// SpfDelayInitial / SpfHold / SpfMax form `timers spf delay
	// initial <init> <hold> <max>` (milliseconds).
	SpfDelayInitial *int `pulumi:"spfDelayInitial,optional"`
	SpfHold         *int `pulumi:"spfHold,optional"`
	SpfMax          *int `pulumi:"spfMax,optional"`
	// OutDelay throttles outgoing LSA pacing in milliseconds.
	OutDelay *int `pulumi:"outDelay,optional"`
	// PacingFlood throttles outgoing flood pacing in milliseconds.
	PacingFlood *int `pulumi:"pacingFlood,optional"`
}

// Annotate documents the shape.
func (t *RouterOspfTimers) Annotate(an infer.Annotator) {
	an.Describe(&t.SpfDelayInitial, "SPF delay initial (ms). Pairs with hold + max.")
	an.Describe(&t.SpfHold, "SPF hold (ms).")
	an.Describe(&t.SpfMax, "SPF max wait (ms).")
	an.Describe(&t.OutDelay, "Outgoing LSA pacing (ms).")
	an.Describe(&t.PacingFlood, "Outgoing flood pacing (ms).")
}

// RouterOspfArgs is the resource input set.
type RouterOspfArgs struct {
	// Instance is the OSPF process id (1..65535) — PK component.
	Instance int `pulumi:"instance"`
	// Vrf binds the process to a non-default VRF — PK component.
	// Empty string = default VRF.
	Vrf *string `pulumi:"vrf,optional"`
	// RouterId pins the OSPF router-id (IPv4).
	RouterId *string `pulumi:"routerId,optional"`
	// Shutdown disables the process while keeping config.
	Shutdown *bool `pulumi:"shutdown,optional"`
	// MaxLsa is the per-process LSA ceiling.
	MaxLsa *int `pulumi:"maxLsa,optional"`
	// MaximumPaths is the ECMP path count (1..128).
	MaximumPaths *int `pulumi:"maximumPaths,optional"`
	// AutoCostReferenceBandwidth tunes the cost formula (Mbps).
	AutoCostReferenceBandwidth *int `pulumi:"autoCostReferenceBandwidth,optional"`
	// PassiveInterfaceDefault flips global default to passive when true.
	PassiveInterfaceDefault *bool `pulumi:"passiveInterfaceDefault,optional"`
	// PassiveInterfaces is the list of explicit passive interfaces (no
	// effect when PassiveInterfaceDefault is true; use NoPassiveInterfaces
	// to override the default in that case).
	PassiveInterfaces []string `pulumi:"passiveInterfaces,optional"`
	// NoPassiveInterfaces overrides PassiveInterfaceDefault on the
	// listed interfaces.
	NoPassiveInterfaces []string `pulumi:"noPassiveInterfaces,optional"`
	// Networks is the set of `network <prefix> area <area>` lines.
	Networks []RouterOspfNetwork `pulumi:"networks,optional"`
	// Areas is the set of per-area customisations.
	Areas []RouterOspfArea `pulumi:"areas,optional"`
	// Redistribute is the set of `redistribute <source>` lines.
	Redistribute []RouterOspfRedistribute `pulumi:"redistribute,optional"`
	// DefaultInformationOriginate emits `default-information originate`.
	DefaultInformationOriginate *RouterOspfDefaultInfoOrigin `pulumi:"defaultInformationOriginate,optional"`
	// SummaryAddresses is the list of `summary-address <prefix>` lines.
	SummaryAddresses []string `pulumi:"summaryAddresses,optional"`
	// Distance carries the per-route-source distance overrides.
	Distance *RouterOspfDistance `pulumi:"distance,optional"`
	// LogAdjacencyChanges accepts "" | off | on | detail.
	LogAdjacencyChanges *string `pulumi:"logAdjacencyChanges,optional"`
	// GracefulRestartHelper enables `graceful-restart-helper`.
	GracefulRestartHelper *bool `pulumi:"gracefulRestartHelper,optional"`
	// Timers carries optional scheduling tweaks.
	Timers *RouterOspfTimers `pulumi:"timers,optional"`

	// Host overrides the provider-level eosUrl host for this resource.
	Host *string `pulumi:"host,optional"`
	// Username overrides the provider-level eosUsername for this resource.
	Username *string `pulumi:"username,optional"`
	// Password overrides the provider-level eosPassword for this resource.
	Password *string `provider:"secret" pulumi:"password,optional"`
}

// RouterOspfState mirrors Args.
type RouterOspfState struct {
	RouterOspfArgs
}

// Annotate documents the resource.
func (r *RouterOspf) Annotate(a infer.Annotator) {
	a.Describe(&r, "An OSPFv2 process under `router ospf <instance> [vrf <vrf>]`. v0 covers the day-zero leaf-spine surface; mixed-NSSA forms and BFD/GR full surface deferred to v1.")
}

// Annotate documents RouterOspfArgs fields.
func (a *RouterOspfArgs) Annotate(an infer.Annotator) {
	an.Describe(&a.Instance, "OSPF process id (1..65535) — PK component.")
	an.Describe(&a.Vrf, "Non-default VRF binding — PK component. Empty = default VRF.")
	an.Describe(&a.RouterId, "OSPF router-id (IPv4).")
	an.Describe(&a.Shutdown, "Disable the process while keeping config when true.")
	an.Describe(&a.MaxLsa, "Per-process LSA ceiling.")
	an.Describe(&a.MaximumPaths, "ECMP next-hop count (1..128).")
	an.Describe(&a.AutoCostReferenceBandwidth, "Reference bandwidth for cost computation (Mbps).")
	an.Describe(&a.PassiveInterfaceDefault, "Flip the global default to passive when true.")
	an.Describe(&a.PassiveInterfaces, "Interfaces to mark passive (when default is active).")
	an.Describe(&a.NoPassiveInterfaces, "Interfaces to override passive default (when default is passive).")
	an.Describe(&a.Networks, "`network <prefix> area <area>` lines.")
	an.Describe(&a.Areas, "Per-area customisations.")
	an.Describe(&a.Redistribute, "Route-source redistributions.")
	an.Describe(&a.DefaultInformationOriginate, "Generate a default route into OSPF.")
	an.Describe(&a.SummaryAddresses, "`summary-address <prefix>` lines.")
	an.Describe(&a.Distance, "Per-source administrative distance overrides.")
	an.Describe(&a.LogAdjacencyChanges, "Adjacency change logging: '' | off | on | detail.")
	an.Describe(&a.GracefulRestartHelper, "Enable `graceful-restart-helper` when true.")
	an.Describe(&a.Timers, "SPF / out-delay / pacing-flood scheduling tweaks.")
	an.Describe(&a.Host, "Optional management hostname override.")
	an.Describe(&a.Username, "Optional AAA username override.")
	an.Describe(&a.Password, "Optional AAA password override.")
}

// Annotate is a no-op; State has no extra fields.
func (s *RouterOspfState) Annotate(_ infer.Annotator) {}

// Create configures the OSPF process.
func (*RouterOspf) Create(ctx context.Context, req infer.CreateRequest[RouterOspfArgs]) (infer.CreateResponse[RouterOspfState], error) {
	if err := validateRouterOspf(req.Inputs); err != nil {
		return infer.CreateResponse[RouterOspfState]{}, err
	}
	state := RouterOspfState{RouterOspfArgs: req.Inputs}
	if req.DryRun {
		return infer.CreateResponse[RouterOspfState]{ID: routerOspfID(req.Inputs.Instance, req.Inputs.Vrf), Output: state}, nil
	}
	if err := applyRouterOspf(ctx, req.Inputs, false); err != nil {
		return infer.CreateResponse[RouterOspfState]{}, fmt.Errorf("create router ospf %d: %w", req.Inputs.Instance, err)
	}
	return infer.CreateResponse[RouterOspfState]{ID: routerOspfID(req.Inputs.Instance, req.Inputs.Vrf), Output: state}, nil
}

// Read refreshes process state from the device.
func (*RouterOspf) Read(ctx context.Context, req infer.ReadRequest[RouterOspfArgs, RouterOspfState]) (infer.ReadResponse[RouterOspfArgs, RouterOspfState], error) {
	cli, err := newClient(ctx, req.Inputs.Host, req.Inputs.Username, req.Inputs.Password)
	if err != nil {
		return infer.ReadResponse[RouterOspfArgs, RouterOspfState]{}, err
	}
	current, found, err := readRouterOspf(ctx, cli, req.Inputs.Instance, deref(req.Inputs.Vrf))
	if err != nil {
		return infer.ReadResponse[RouterOspfArgs, RouterOspfState]{}, err
	}
	if !found {
		return infer.ReadResponse[RouterOspfArgs, RouterOspfState]{}, nil
	}
	state := RouterOspfState{RouterOspfArgs: req.Inputs}
	current.fillState(&state)
	return infer.ReadResponse[RouterOspfArgs, RouterOspfState]{
		ID:     routerOspfID(req.Inputs.Instance, req.Inputs.Vrf),
		Inputs: req.Inputs,
		State:  state,
	}, nil
}

// Update re-applies the process. Negate-then-rebuild inside one
// config-session: stale `area`/`network`/`redistribute` lines from a
// previous version are guaranteed to be gone before re-emit. EOS'
// session diff applies the minimum delta — the OSPF process does not
// restart even though `no router ospf` is in the staged buffer.
func (*RouterOspf) Update(ctx context.Context, req infer.UpdateRequest[RouterOspfArgs, RouterOspfState]) (infer.UpdateResponse[RouterOspfState], error) {
	if err := validateRouterOspf(req.Inputs); err != nil {
		return infer.UpdateResponse[RouterOspfState]{}, err
	}
	state := RouterOspfState{RouterOspfArgs: req.Inputs}
	if req.DryRun {
		return infer.UpdateResponse[RouterOspfState]{Output: state}, nil
	}
	if err := applyRouterOspf(ctx, req.Inputs, false); err != nil {
		return infer.UpdateResponse[RouterOspfState]{}, fmt.Errorf("update router ospf %d: %w", req.Inputs.Instance, err)
	}
	return infer.UpdateResponse[RouterOspfState]{Output: state}, nil
}

// Delete removes the process.
func (*RouterOspf) Delete(ctx context.Context, req infer.DeleteRequest[RouterOspfState]) (infer.DeleteResponse, error) {
	if err := applyRouterOspf(ctx, req.State.RouterOspfArgs, true); err != nil {
		return infer.DeleteResponse{}, fmt.Errorf("delete router ospf %d: %w", req.State.Instance, err)
	}
	return infer.DeleteResponse{}, nil
}

// --- helpers ---------------------------------------------------------------

func validateRouterOspf(args RouterOspfArgs) error {
	if err := validateRouterOspfScalars(args); err != nil {
		return err
	}
	if err := validateRouterOspfNetworks(args.Networks); err != nil {
		return err
	}
	if err := validateRouterOspfAreas(args.Areas); err != nil {
		return err
	}
	if err := validateRouterOspfRedistribute(args.Redistribute); err != nil {
		return err
	}
	return validateRouterOspfTail(args)
}

// validateRouterOspfScalars covers process-level scalar fields.
func validateRouterOspfScalars(args RouterOspfArgs) error {
	if args.Instance < 1 || args.Instance > 65535 {
		return fmt.Errorf("%w: got %d", ErrRouterOspfInstanceRange, args.Instance)
	}
	if args.RouterId != nil && *args.RouterId != "" {
		if addr, err := netip.ParseAddr(*args.RouterId); err != nil || !addr.Is4() {
			return fmt.Errorf("%w: %q", ErrRouterOspfRouterIDInvalid, *args.RouterId)
		}
	}
	if args.MaxLsa != nil && *args.MaxLsa <= 0 {
		return fmt.Errorf("%w: got %d", ErrRouterOspfMaxLsaNonPositive, *args.MaxLsa)
	}
	if args.MaximumPaths != nil && (*args.MaximumPaths < 1 || *args.MaximumPaths > 128) {
		return fmt.Errorf("%w: got %d", ErrRouterOspfMaximumPathsRange, *args.MaximumPaths)
	}
	if args.AutoCostReferenceBandwidth != nil && *args.AutoCostReferenceBandwidth <= 0 {
		return fmt.Errorf("%w: got %d", ErrRouterOspfRefBwNonPositive, *args.AutoCostReferenceBandwidth)
	}
	return nil
}

// validateRouterOspfNetworks checks `network <prefix> area <area>`
// rows. Prefix must parse as IPv4 CIDR; area must canonicalise.
func validateRouterOspfNetworks(networks []RouterOspfNetwork) error {
	for _, n := range networks {
		if _, err := netip.ParsePrefix(n.Prefix); err != nil {
			return fmt.Errorf("%w: %q", ErrRouterOspfPrefixInvalid, n.Prefix)
		}
		if _, ok := canonicaliseOspfArea(n.Area); !ok {
			return fmt.Errorf("%w: %q", ErrRouterOspfAreaInvalid, n.Area)
		}
	}
	return nil
}

// validateRouterOspfAreas checks per-area customisations.
func validateRouterOspfAreas(areas []RouterOspfArea) error {
	for _, area := range areas {
		if _, ok := canonicaliseOspfArea(area.Id); !ok {
			return fmt.Errorf("%w: %q", ErrRouterOspfAreaInvalid, area.Id)
		}
		if area.Type != nil && *area.Type != "" {
			if _, ok := validRouterOspfAreaTypes[*area.Type]; !ok {
				return fmt.Errorf("%w: got %q", ErrRouterOspfAreaTypeInvalid, *area.Type)
			}
		}
		if area.DefaultCost != nil && (*area.DefaultCost < 1 || *area.DefaultCost > 65535) {
			return fmt.Errorf("%w: got %d", ErrRouterOspfDefaultCostRange, *area.DefaultCost)
		}
		for _, r := range area.Ranges {
			if _, err := netip.ParsePrefix(r); err != nil {
				return fmt.Errorf("%w: %q", ErrRouterOspfSummaryPrefixInvld, r)
			}
		}
		if area.NssaMetricType != nil && *area.NssaMetricType != 1 && *area.NssaMetricType != 2 {
			return fmt.Errorf("%w: got %d", ErrRouterOspfMetricTypeInvalid, *area.NssaMetricType)
		}
	}
	return nil
}

// validateRouterOspfRedistribute checks `redistribute <source>` rows.
func validateRouterOspfRedistribute(rows []RouterOspfRedistribute) error {
	for _, r := range rows {
		if _, ok := validRouterOspfRedistSrc[r.Source]; !ok {
			return fmt.Errorf("%w: got %q", ErrRouterOspfRedistSrcInvalid, r.Source)
		}
	}
	return nil
}

// validateRouterOspfTail checks the remaining optional sections:
// default-information-originate, summary addresses, distance, log
// adjacency changes, timers.
func validateRouterOspfTail(args RouterOspfArgs) error {
	if args.DefaultInformationOriginate != nil {
		if t := args.DefaultInformationOriginate.MetricType; t != nil && *t != 1 && *t != 2 {
			return fmt.Errorf("%w: got %d", ErrRouterOspfMetricTypeInvalid, *t)
		}
	}
	for _, p := range args.SummaryAddresses {
		if _, err := netip.ParsePrefix(p); err != nil {
			return fmt.Errorf("%w: %q", ErrRouterOspfSummaryPrefixInvld, p)
		}
	}
	if args.Distance != nil {
		for _, p := range []*int{args.Distance.IntraArea, args.Distance.InterArea, args.Distance.External} {
			if p != nil && (*p < 1 || *p > 255) {
				return fmt.Errorf("%w: got %d", ErrRouterOspfDistanceRange, *p)
			}
		}
	}
	if args.LogAdjacencyChanges != nil {
		if _, ok := validRouterOspfLogChanges[*args.LogAdjacencyChanges]; !ok {
			return fmt.Errorf("%w: got %q", ErrRouterOspfLogChangesInvalid, *args.LogAdjacencyChanges)
		}
	}
	if args.Timers != nil {
		for _, p := range []*int{
			args.Timers.SpfDelayInitial, args.Timers.SpfHold, args.Timers.SpfMax,
			args.Timers.OutDelay, args.Timers.PacingFlood,
		} {
			if p != nil && *p <= 0 {
				return fmt.Errorf("%w: got %d", ErrRouterOspfTimerNonPositive, *p)
			}
		}
	}
	return nil
}

func routerOspfID(instance int, vrf *string) string {
	if vrf != nil && *vrf != "" {
		return fmt.Sprintf("router-ospf/%d/%s", instance, *vrf)
	}
	return fmt.Sprintf("router-ospf/%d", instance)
}

// canonicaliseOspfArea accepts a non-negative integer or dotted-quad
// and returns the canonical dotted-quad form cEOS uses in
// running-config. Returns (canonical, true) on success.
func canonicaliseOspfArea(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", false
	}
	if dottedQuadRe.MatchString(s) {
		// Validate octets ≤ 255.
		for p := range strings.SplitSeq(s, ".") {
			if v, err := strconv.Atoi(p); err != nil || v < 0 || v > 255 {
				return "", false
			}
		}
		return s, true
	}
	if v, err := strconv.ParseUint(s, 10, 32); err == nil {
		return fmt.Sprintf("%d.%d.%d.%d", (v>>24)&0xff, (v>>16)&0xff, (v>>8)&0xff, v&0xff), true
	}
	return "", false
}

func applyRouterOspf(ctx context.Context, args RouterOspfArgs, remove bool) error {
	cli, err := newClient(ctx, args.Host, args.Username, args.Password)
	if err != nil {
		return err
	}
	cfg := config.FromContext(ctx)
	sessName := cfg.SessionPrefix() + "ospf-" + strconv.Itoa(args.Instance)
	if v := deref(args.Vrf); v != "" {
		sessName += "-" + sanitizePrefixListName(v)
	}
	sess, err := cli.OpenSession(ctx, sessName)
	if err != nil {
		return err
	}
	cmds := buildRouterOspfCmds(args, remove)
	if stageErr := sess.Stage(ctx, cmds); stageErr != nil {
		if abortErr := sess.Abort(ctx); abortErr != nil {
			return errors.Join(stageErr, abortErr)
		}
		return stageErr
	}
	return sess.Commit(ctx)
}

// buildRouterOspfCmds renders the staged CLI block. The body uses the
// negate-then-rebuild pattern (same as PrefixList / RouteMap):
// `no router ospf <id> [vrf X]` evicts stale `area` / `network` /
// `redistribute` lines from the previous version, then a single fresh
// emit replays the current Args. Inside one config-session this is
// applied as a diff — the OSPF process does NOT restart.
func buildRouterOspfCmds(args RouterOspfArgs, remove bool) []string {
	header := "router ospf " + strconv.Itoa(args.Instance)
	if v := deref(args.Vrf); v != "" {
		header += " vrf " + v
	}
	if remove {
		return []string{"no " + header}
	}
	cmds := []string{"no " + header, header}

	cmds = appendRouterOspfBody(cmds, args)
	cmds = append(cmds, "exit")
	return cmds
}

// appendRouterOspfBody emits the body lines under the header. Order
// matches cEOS' canonical running-config render so that diffs against
// `show running-config section ospf` are minimal.
func appendRouterOspfBody(cmds []string, args RouterOspfArgs) []string {
	cmds = appendOspfScalars(cmds, args)
	cmds = appendOspfDistance(cmds, args.Distance)
	cmds = appendOspfPassive(cmds, args)
	cmds = appendOspfRedistribute(cmds, args.Redistribute)
	for _, area := range args.Areas {
		cmds = append(cmds, renderArea(area)...)
	}
	for _, n := range args.Networks {
		canon, _ := canonicaliseOspfArea(n.Area)
		cmds = append(cmds, "network "+n.Prefix+" area "+canon)
	}
	if args.MaxLsa != nil {
		cmds = append(cmds, "max-lsa "+strconv.Itoa(*args.MaxLsa))
	}
	cmds = appendOspfLogChanges(cmds, args.LogAdjacencyChanges)
	cmds = appendOspfTimers(cmds, args.Timers)
	if args.MaximumPaths != nil {
		cmds = append(cmds, "maximum-paths "+strconv.Itoa(*args.MaximumPaths))
	}
	cmds = appendOspfDefaultInfo(cmds, args.DefaultInformationOriginate)
	for _, p := range args.SummaryAddresses {
		cmds = append(cmds, "summary-address "+p)
	}
	if args.GracefulRestartHelper != nil && *args.GracefulRestartHelper {
		cmds = append(cmds, "graceful-restart-helper")
	}
	return cmds
}

// appendOspfScalars emits router-id, shutdown, auto-cost ref bw.
func appendOspfScalars(cmds []string, args RouterOspfArgs) []string {
	if args.RouterId != nil && *args.RouterId != "" {
		cmds = append(cmds, "router-id "+*args.RouterId)
	}
	if args.Shutdown != nil {
		if *args.Shutdown {
			cmds = append(cmds, "shutdown")
		} else {
			cmds = append(cmds, "no shutdown")
		}
	}
	if args.AutoCostReferenceBandwidth != nil {
		cmds = append(cmds, "auto-cost reference-bandwidth "+strconv.Itoa(*args.AutoCostReferenceBandwidth))
	}
	return cmds
}

// appendOspfDistance emits the three `distance ospf <kind> N` lines —
// cEOS 4.36 rejects the combined single-line form (probe-verified).
func appendOspfDistance(cmds []string, d *RouterOspfDistance) []string {
	if d == nil {
		return cmds
	}
	if v := d.IntraArea; v != nil {
		cmds = append(cmds, "distance ospf intra-area "+strconv.Itoa(*v))
	}
	if v := d.InterArea; v != nil {
		cmds = append(cmds, "distance ospf inter-area "+strconv.Itoa(*v))
	}
	if v := d.External; v != nil {
		cmds = append(cmds, "distance ospf external "+strconv.Itoa(*v))
	}
	return cmds
}

// appendOspfPassive emits passive-interface default + per-interface
// overrides. Order: default → no-passive overrides → explicit passive.
func appendOspfPassive(cmds []string, args RouterOspfArgs) []string {
	if args.PassiveInterfaceDefault != nil && *args.PassiveInterfaceDefault {
		cmds = append(cmds, "passive-interface default")
	}
	for _, intf := range args.NoPassiveInterfaces {
		cmds = append(cmds, "no passive-interface "+intf)
	}
	for _, intf := range args.PassiveInterfaces {
		cmds = append(cmds, "passive-interface "+intf)
	}
	return cmds
}

// appendOspfRedistribute emits one line per row.
func appendOspfRedistribute(cmds []string, rows []RouterOspfRedistribute) []string {
	for _, r := range rows {
		line := "redistribute " + r.Source
		if r.RouteMap != nil && *r.RouteMap != "" {
			line += " route-map " + *r.RouteMap
		}
		cmds = append(cmds, line)
	}
	return cmds
}

// appendOspfLogChanges emits the log-adjacency-changes line.
func appendOspfLogChanges(cmds []string, v *string) []string {
	if v == nil || *v == "" {
		return cmds
	}
	switch *v {
	case "off":
		return append(cmds, "no log-adjacency-changes")
	case "detail":
		return append(cmds, "log-adjacency-changes detail")
	default:
		return append(cmds, "log-adjacency-changes")
	}
}

// appendOspfTimers emits the SPF / out-delay / pacing-flood lines.
func appendOspfTimers(cmds []string, t *RouterOspfTimers) []string {
	if t == nil {
		return cmds
	}
	if a, b, c := t.SpfDelayInitial, t.SpfHold, t.SpfMax; a != nil && b != nil && c != nil {
		cmds = append(cmds, fmt.Sprintf("timers spf delay initial %d %d %d", *a, *b, *c))
	}
	if v := t.OutDelay; v != nil {
		cmds = append(cmds, "timers out-delay "+strconv.Itoa(*v))
	}
	if v := t.PacingFlood; v != nil {
		cmds = append(cmds, "timers pacing flood "+strconv.Itoa(*v))
	}
	return cmds
}

// appendOspfDefaultInfo emits the default-information-originate line.
func appendOspfDefaultInfo(cmds []string, d *RouterOspfDefaultInfoOrigin) []string {
	if d == nil {
		return cmds
	}
	line := "default-information originate"
	if d.Metric != nil {
		line += " metric " + strconv.Itoa(*d.Metric)
	}
	if d.MetricType != nil {
		line += " metric-type " + strconv.Itoa(*d.MetricType)
	}
	if d.RouteMap != nil && *d.RouteMap != "" {
		line += " route-map " + *d.RouteMap
	}
	return append(cmds, line)
}

// renderArea emits the lines for one RouterOspfArea. Multiple lines
// per area are common — area type, default-cost, and ranges each emit
// their own row.
func renderArea(a RouterOspfArea) []string {
	canon, _ := canonicaliseOspfArea(a.Id)
	var out []string
	if a.Type != nil && *a.Type != "" && *a.Type != "normal" {
		switch *a.Type {
		case "stub":
			out = append(out, "area "+canon+" stub")
		case "stub-no-summary":
			out = append(out, "area "+canon+" stub no-summary")
		case "nssa":
			out = append(out, "area "+canon+" nssa")
		case "nssa-default-information-originate":
			line := "area " + canon + " nssa default-information-originate"
			if a.NssaMetric != nil {
				line += " metric " + strconv.Itoa(*a.NssaMetric)
			}
			if a.NssaMetricType != nil {
				line += " metric-type " + strconv.Itoa(*a.NssaMetricType)
			}
			if a.NssaOnly != nil && *a.NssaOnly {
				line += " nssa-only"
			}
			out = append(out, line)
		}
	}
	if a.DefaultCost != nil {
		out = append(out, "area "+canon+" default-cost "+strconv.Itoa(*a.DefaultCost))
	}
	for _, r := range a.Ranges {
		out = append(out, "area "+canon+" range "+r)
	}
	return out
}

// routerOspfRow is the parsed live state we care about. v0 reads the
// minimal subset users care about for diff-tracking; full Read-back
// is a v1 follow-up. Everything beyond router-id / shutdown /
// max-lsa / maximum-paths / passive-interface default is left as
// pulumi-managed-only — drift in those rows surfaces on next refresh
// via a dirty-config plan but the structured state matches the
// last-applied Args.
type routerOspfRow struct {
	Instance     int
	Vrf          string
	RouterID     string
	Shutdown     *bool
	MaxLsa       int
	MaximumPaths int
	PassiveDflt  *bool
}

// readRouterOspf returns the live process state, or (false, nil) when
// absent. EOS pipe-grep is single-word; we pull the full ospf section
// and parse client-side.
func readRouterOspf(ctx context.Context, cli *eapi.Client, instance int, vrf string) (routerOspfRow, bool, error) {
	resp, err := cli.RunCmds(ctx,
		[]string{"show running-config section ospf"},
		"text")
	if err != nil {
		return routerOspfRow{}, false, err
	}
	if len(resp) == 0 {
		return routerOspfRow{}, false, nil
	}
	out, _ := resp[0]["output"].(string)
	row, found := parseRouterOspfSection(out, instance, vrf)
	return row, found, nil
}

// parseRouterOspfSection extracts the named OSPF process body.
// Exposed for unit tests.
func parseRouterOspfSection(out string, instance int, vrf string) (routerOspfRow, bool) {
	header := "router ospf " + strconv.Itoa(instance)
	if vrf != "" {
		header += " vrf " + vrf
	}
	row := routerOspfRow{Instance: instance, Vrf: vrf}
	inOurs := false
	found := false
	for raw := range strings.SplitSeq(out, "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(line, "router ospf"):
			inOurs = (line == header)
			if inOurs {
				found = true
			}
		case inOurs:
			applyRouterOspfLine(&row, line)
		}
	}
	if !found {
		return routerOspfRow{}, false
	}
	return row, true
}

// applyRouterOspfLine populates one process field from a body line.
func applyRouterOspfLine(row *routerOspfRow, line string) {
	switch {
	case strings.HasPrefix(line, "router-id "):
		row.RouterID = strings.TrimPrefix(line, "router-id ")
	case line == "shutdown":
		v := true
		row.Shutdown = &v
	case line == "no shutdown":
		v := false
		row.Shutdown = &v
	case strings.HasPrefix(line, "max-lsa "):
		// `max-lsa <num> ...` — only the first field is the ceiling.
		fields := strings.Fields(strings.TrimPrefix(line, "max-lsa "))
		if len(fields) > 0 {
			if v, err := strconv.Atoi(fields[0]); err == nil {
				row.MaxLsa = v
			}
		}
	case strings.HasPrefix(line, "maximum-paths "):
		if v, err := strconv.Atoi(strings.TrimPrefix(line, "maximum-paths ")); err == nil {
			row.MaximumPaths = v
		}
	case line == "passive-interface default":
		v := true
		row.PassiveDflt = &v
	}
}

// fillState writes the parsed row back to RouterOspfState fields.
func (r routerOspfRow) fillState(s *RouterOspfState) {
	if r.RouterID != "" {
		v := r.RouterID
		s.RouterId = &v
	}
	if r.Shutdown != nil {
		v := *r.Shutdown
		s.Shutdown = &v
	}
	if r.MaxLsa > 0 {
		v := r.MaxLsa
		s.MaxLsa = &v
	}
	if r.MaximumPaths > 0 {
		v := r.MaximumPaths
		s.MaximumPaths = &v
	}
	if r.PassiveDflt != nil {
		v := *r.PassiveDflt
		s.PassiveInterfaceDefault = &v
	}
}

// deref returns the value behind a *string or "" when nil.
func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
