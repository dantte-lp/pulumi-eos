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

// Sentinel errors specific to Rpki.
var (
	ErrRpkiNameRequired        = errors.New("rpki cache name is required")
	ErrRpkiBadName             = errors.New("rpki cache name must match [A-Za-z][A-Za-z0-9_-]*")
	ErrRpkiAsnInvalid          = errors.New("rpki bgpAsn must be in 1..4294967295")
	ErrRpkiCacheHostBadIPv4    = errors.New("rpki cacheHost must be a valid IPv4 address")
	ErrRpkiPortOutOfRange      = errors.New("rpki port must be in 1..65535")
	ErrRpkiPreferenceRange     = errors.New("rpki preference must be in 1..10")
	ErrRpkiIntervalNonPositive = errors.New("rpki refreshInterval / retryInterval / expireInterval must be > 0 seconds")
	ErrRpkiTransportInvalid    = errors.New("rpki transport must be 'tcp' or 'ssh'")
)

// rpkiCacheNameRe enforces the EOS-accepted RPKI cache name grammar.
// Verified live against cEOS 4.36.0.1F.
var rpkiCacheNameRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)

// validRpkiTransport enumerates the transport keywords EOS accepts on
// `transport <kind>` inside an `rpki cache` block.
var validRpkiTransport = map[string]struct{}{
	"tcp": {},
	"ssh": {},
}

// Rpki models a single EOS RPKI ROA cache configuration. Caches live
// inside the `router bgp <asn>` block as an `rpki cache <name>` sub-
// section. Multiple caches per BGP instance are supported (the EOS
// design guide recommends two caches for redundancy with different
// preference values).
//
// Composition: this resource is independent of `eos:l3:RouterBgp` —
// RouterBgp v0 does not model rpki caches in its own surface, so
// Rpki and RouterBgp can coexist on the same ASN without circular
// resource dependencies. EOS' configuration-session diff logic
// preserves rpki cache lines that are not part of the RouterBgp
// re-emit.
//
// The per-cache `match rpki invalid|valid|not-found` route-map clause
// is owned by `eos:l3:RouteMap` (audit-gap follow-up — currently
// listed as a deferred Open commitment).
//
// Source: EOS BGP RPKI Origin Validation Design Guide; TOI 14470
// (BGP prefix origin validation with RPKI); RFC 8210 (RPKI to Router
// Protocol); validated live against cEOS 4.36.0.1F per the
// per-resource verification rule.
type Rpki struct{}

// RpkiArgs is the input set.
type RpkiArgs struct {
	// Name is the cache identifier inside `router bgp <asn>` (PK).
	Name string `pulumi:"name"`
	// BgpAsn is the BGP ASN this cache binds to (PK component, 1..2^32-1).
	BgpAsn int `pulumi:"bgpAsn"`
	// CacheHost is the cache server IPv4 address. Named CacheHost (not
	// `Host`) to avoid shadowing the per-resource management-host
	// override at the bottom of the args struct.
	CacheHost string `pulumi:"cacheHost"`
	// Vrf places the RTR session in a non-default VRF. Optional.
	Vrf *string `pulumi:"vrf,optional"`
	// Port is the cache TCP/SSH port. Defaults to 323 (RFC 8210);
	// override per environment.
	Port *int `pulumi:"port,optional"`
	// Preference orders cache selection (1..10, lower wins). EOS
	// elides default value 5 from running-config; the resource only
	// renders explicit values to keep diffs stable.
	Preference *int `pulumi:"preference,optional"`
	// RefreshInterval is the steady-state ROA refresh in seconds.
	RefreshInterval *int `pulumi:"refreshInterval,optional"`
	// RetryInterval is the connect-retry interval in seconds.
	RetryInterval *int `pulumi:"retryInterval,optional"`
	// ExpireInterval is the ROA stale-time in seconds.
	ExpireInterval *int `pulumi:"expireInterval,optional"`
	// LocalInterface pins the source interface for the RTR session.
	LocalInterface *string `pulumi:"localInterface,optional"`
	// Transport is "tcp" (default) or "ssh".
	Transport *string `pulumi:"transport,optional"`

	// Host overrides the provider-level eosUrl host for this resource.
	Host *string `pulumi:"host,optional"`
	// Username overrides the provider-level eosUsername for this resource.
	Username *string `pulumi:"username,optional"`
	// Password overrides the provider-level eosPassword for this resource.
	Password *string `provider:"secret" pulumi:"password,optional"`
}

// RpkiState mirrors Args.
type RpkiState struct {
	RpkiArgs
}

// Annotate documents the resource.
func (r *Rpki) Annotate(a infer.Annotator) {
	a.Describe(&r, "An EOS RPKI ROA cache configured under `router bgp <asn> / rpki cache <name>`. Independent of eos:l3:RouterBgp — multiple caches per ASN supported.")
}

// Annotate documents RpkiArgs fields.
func (a *RpkiArgs) Annotate(an infer.Annotator) {
	an.Describe(&a.Name, "Cache name (PK). Must match [A-Za-z][A-Za-z0-9_-]*.")
	an.Describe(&a.BgpAsn, "BGP ASN this cache binds to (1..4294967295). Together with name, forms the resource identity.")
	an.Describe(&a.CacheHost, "Cache server IPv4 address.")
	an.Describe(&a.Vrf, "Non-default VRF binding for the RTR session.")
	an.Describe(&a.Port, "Cache TCP/SSH port (1..65535). Defaults to 323 per RFC 8210.")
	an.Describe(&a.Preference, "Cache preference (1..10, lower wins). EOS elides default 5 from running-config.")
	an.Describe(&a.RefreshInterval, "ROA refresh interval in seconds.")
	an.Describe(&a.RetryInterval, "Connect-retry interval in seconds.")
	an.Describe(&a.ExpireInterval, "ROA stale-time in seconds.")
	an.Describe(&a.LocalInterface, "Source interface pin for the RTR session (e.g. Management1).")
	an.Describe(&a.Transport, "Transport keyword: tcp (default) or ssh.")
	an.Describe(&a.Host, "Optional management hostname override.")
	an.Describe(&a.Username, "Optional AAA username override.")
	an.Describe(&a.Password, "Optional AAA password override.")
}

// Annotate is a no-op; State has no extra fields.
func (s *RpkiState) Annotate(_ infer.Annotator) {}

// Create configures the rpki cache.
func (*Rpki) Create(ctx context.Context, req infer.CreateRequest[RpkiArgs]) (infer.CreateResponse[RpkiState], error) {
	if err := validateRpki(req.Inputs); err != nil {
		return infer.CreateResponse[RpkiState]{}, err
	}
	state := RpkiState{RpkiArgs: req.Inputs}
	if req.DryRun {
		return infer.CreateResponse[RpkiState]{ID: rpkiID(req.Inputs.BgpAsn, req.Inputs.Name), Output: state}, nil
	}
	if err := applyRpki(ctx, req.Inputs, false); err != nil {
		return infer.CreateResponse[RpkiState]{}, fmt.Errorf("create rpki cache %s: %w", req.Inputs.Name, err)
	}
	return infer.CreateResponse[RpkiState]{ID: rpkiID(req.Inputs.BgpAsn, req.Inputs.Name), Output: state}, nil
}

// Read refreshes rpki cache state from the device.
func (*Rpki) Read(ctx context.Context, req infer.ReadRequest[RpkiArgs, RpkiState]) (infer.ReadResponse[RpkiArgs, RpkiState], error) {
	cli, err := newClient(ctx, req.Inputs.Host, req.Inputs.Username, req.Inputs.Password)
	if err != nil {
		return infer.ReadResponse[RpkiArgs, RpkiState]{}, err
	}
	current, found, err := readRpki(ctx, cli, req.Inputs.BgpAsn, req.Inputs.Name)
	if err != nil {
		return infer.ReadResponse[RpkiArgs, RpkiState]{}, err
	}
	if !found {
		return infer.ReadResponse[RpkiArgs, RpkiState]{}, nil
	}
	state := RpkiState{RpkiArgs: req.Inputs}
	current.fillState(&state)
	return infer.ReadResponse[RpkiArgs, RpkiState]{
		ID:     rpkiID(req.Inputs.BgpAsn, req.Inputs.Name),
		Inputs: req.Inputs,
		State:  state,
	}, nil
}

// Update re-applies the cache. EOS replaces the whole `rpki cache
// <name>` sub-block on re-emit; staged session diff covers the delta.
func (*Rpki) Update(ctx context.Context, req infer.UpdateRequest[RpkiArgs, RpkiState]) (infer.UpdateResponse[RpkiState], error) {
	if err := validateRpki(req.Inputs); err != nil {
		return infer.UpdateResponse[RpkiState]{}, err
	}
	state := RpkiState{RpkiArgs: req.Inputs}
	if req.DryRun {
		return infer.UpdateResponse[RpkiState]{Output: state}, nil
	}
	if err := applyRpki(ctx, req.Inputs, false); err != nil {
		return infer.UpdateResponse[RpkiState]{}, fmt.Errorf("update rpki cache %s: %w", req.Inputs.Name, err)
	}
	return infer.UpdateResponse[RpkiState]{Output: state}, nil
}

// Delete removes the rpki cache.
func (*Rpki) Delete(ctx context.Context, req infer.DeleteRequest[RpkiState]) (infer.DeleteResponse, error) {
	if err := applyRpki(ctx, req.State.RpkiArgs, true); err != nil {
		return infer.DeleteResponse{}, fmt.Errorf("delete rpki cache %s: %w", req.State.Name, err)
	}
	return infer.DeleteResponse{}, nil
}

// --- helpers ---------------------------------------------------------------

func validateRpki(args RpkiArgs) error {
	if strings.TrimSpace(args.Name) == "" {
		return ErrRpkiNameRequired
	}
	if !rpkiCacheNameRe.MatchString(args.Name) {
		return fmt.Errorf("%w: %q", ErrRpkiBadName, args.Name)
	}
	if args.BgpAsn < 1 || args.BgpAsn > 4294967295 {
		return fmt.Errorf("%w: got %d", ErrRpkiAsnInvalid, args.BgpAsn)
	}
	if addr, err := netip.ParseAddr(args.CacheHost); err != nil || !addr.Is4() {
		return fmt.Errorf("%w: %q", ErrRpkiCacheHostBadIPv4, args.CacheHost)
	}
	if args.Port != nil && (*args.Port < 1 || *args.Port > 65535) {
		return fmt.Errorf("%w: got %d", ErrRpkiPortOutOfRange, *args.Port)
	}
	if args.Preference != nil && (*args.Preference < 1 || *args.Preference > 10) {
		return fmt.Errorf("%w: got %d", ErrRpkiPreferenceRange, *args.Preference)
	}
	for _, p := range []*int{args.RefreshInterval, args.RetryInterval, args.ExpireInterval} {
		if p != nil && *p <= 0 {
			return fmt.Errorf("%w: got %d", ErrRpkiIntervalNonPositive, *p)
		}
	}
	if args.Transport != nil && *args.Transport != "" {
		if _, ok := validRpkiTransport[*args.Transport]; !ok {
			return fmt.Errorf("%w: got %q", ErrRpkiTransportInvalid, *args.Transport)
		}
	}
	return nil
}

func rpkiID(asn int, name string) string {
	return fmt.Sprintf("rpki/%d/%s", asn, name)
}

func applyRpki(ctx context.Context, args RpkiArgs, remove bool) error {
	cli, err := newClient(ctx, args.Host, args.Username, args.Password)
	if err != nil {
		return err
	}
	cfg := config.FromContext(ctx)
	sessName := cfg.SessionPrefix() + "rpki-" + strconv.Itoa(args.BgpAsn) + "-" + sanitizePrefixListName(args.Name)

	sess, err := cli.OpenSession(ctx, sessName)
	if err != nil {
		return err
	}
	cmds := buildRpkiCmds(args, remove)
	if stageErr := sess.Stage(ctx, cmds); stageErr != nil {
		if abortErr := sess.Abort(ctx); abortErr != nil {
			return errors.Join(stageErr, abortErr)
		}
		return stageErr
	}
	return sess.Commit(ctx)
}

// buildRpkiCmds renders the staged CLI block.
//
// Render shape (closing `exit` lines exit the rpki-cache and
// router-bgp modes in turn):
//
//	router bgp <asn>
//	   rpki cache <name>
//	      host <ip> [vrf X] [port N]
//	      preference <1..10>
//	      refresh-interval <sec>
//	      retry-interval <sec>
//	      expire-interval <sec>
//	      local-interface <name>
//	      transport tcp|ssh
//
// Negate-then-rebuild is intentionally NOT used: a single `no rpki
// cache <name>` followed by full re-emit would briefly sever the
// RTR session. Plain re-emission lets EOS' session diff compute the
// minimum delta.
func buildRpkiCmds(args RpkiArgs, remove bool) []string {
	cmds := []string{"router bgp " + strconv.Itoa(args.BgpAsn)}
	if remove {
		cmds = append(cmds, "no rpki cache "+args.Name, "exit")
		return cmds
	}
	cmds = append(cmds, "rpki cache "+args.Name)

	hostLine := "host " + args.CacheHost
	if args.Vrf != nil && *args.Vrf != "" {
		hostLine += " vrf " + *args.Vrf
	}
	if args.Port != nil {
		hostLine += " port " + strconv.Itoa(*args.Port)
	}
	cmds = append(cmds, hostLine)

	if args.Preference != nil {
		cmds = append(cmds, "preference "+strconv.Itoa(*args.Preference))
	}
	if args.RefreshInterval != nil {
		cmds = append(cmds, "refresh-interval "+strconv.Itoa(*args.RefreshInterval))
	}
	if args.RetryInterval != nil {
		cmds = append(cmds, "retry-interval "+strconv.Itoa(*args.RetryInterval))
	}
	if args.ExpireInterval != nil {
		cmds = append(cmds, "expire-interval "+strconv.Itoa(*args.ExpireInterval))
	}
	if args.LocalInterface != nil && *args.LocalInterface != "" {
		cmds = append(cmds, "local-interface "+*args.LocalInterface)
	}
	transport := "tcp"
	if args.Transport != nil && *args.Transport != "" {
		transport = *args.Transport
	}
	cmds = append(cmds, "transport "+transport, "exit", "exit")
	return cmds
}

// rpkiRow is the parsed live state we care about.
type rpkiRow struct {
	Name            string
	CacheHost       string
	Vrf             string
	Port            int
	Preference      int
	RefreshInterval int
	RetryInterval   int
	ExpireInterval  int
	LocalInterface  string
	Transport       string
}

// readRpki returns the live cache state, or (false, nil) when the
// cache is absent. Source: `show running-config | section router bgp`.
func readRpki(ctx context.Context, cli *eapi.Client, asn int, name string) (rpkiRow, bool, error) {
	resp, err := cli.RunCmds(ctx,
		[]string{"show running-config | section router bgp"},
		"text")
	if err != nil {
		return rpkiRow{}, false, err
	}
	if len(resp) == 0 {
		return rpkiRow{}, false, nil
	}
	out, _ := resp[0]["output"].(string)
	row, found := parseRpkiSection(out, asn, name)
	return row, found, nil
}

// parseRpkiSection extracts the `rpki cache <name>` block from the
// `router bgp <asn>` section.
func parseRpkiSection(out string, asn int, name string) (rpkiRow, bool) {
	row := rpkiRow{Name: name}
	header := "router bgp " + strconv.Itoa(asn)
	cacheHeader := "rpki cache " + name
	inOurAsn, inOurCache := false, false
	for raw := range strings.SplitSeq(out, "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(line, "router bgp "):
			inOurAsn = (line == header)
			inOurCache = false
		case inOurAsn && strings.HasPrefix(line, "rpki cache "):
			inOurCache = (line == cacheHeader)
		case inOurAsn && strings.HasPrefix(line, "vrf ") && !inOurCache:
			// VRF sub-block under router bgp — caches don't live there.
			inOurCache = false
		case inOurCache:
			applyRpkiLine(&row, line)
		}
	}
	if row.CacheHost == "" {
		return rpkiRow{}, false
	}
	return row, true
}

// applyRpkiLine populates one rpki-cache field from a body line.
func applyRpkiLine(row *rpkiRow, line string) {
	switch {
	case strings.HasPrefix(line, "host "):
		parseRpkiHostLine(strings.TrimPrefix(line, "host "), row)
	case strings.HasPrefix(line, "preference "):
		if v, err := strconv.Atoi(strings.TrimPrefix(line, "preference ")); err == nil {
			row.Preference = v
		}
	case strings.HasPrefix(line, "refresh-interval "):
		if v, err := strconv.Atoi(strings.TrimPrefix(line, "refresh-interval ")); err == nil {
			row.RefreshInterval = v
		}
	case strings.HasPrefix(line, "retry-interval "):
		if v, err := strconv.Atoi(strings.TrimPrefix(line, "retry-interval ")); err == nil {
			row.RetryInterval = v
		}
	case strings.HasPrefix(line, "expire-interval "):
		if v, err := strconv.Atoi(strings.TrimPrefix(line, "expire-interval ")); err == nil {
			row.ExpireInterval = v
		}
	case strings.HasPrefix(line, "local-interface "):
		row.LocalInterface = strings.TrimPrefix(line, "local-interface ")
	case strings.HasPrefix(line, "transport "):
		row.Transport = strings.TrimPrefix(line, "transport ")
	}
}

// parseRpkiHostLine extracts host / vrf / port from `host <ip> [vrf
// X] [port N]`.
func parseRpkiHostLine(rest string, row *rpkiRow) {
	tokens := strings.Fields(rest)
	if len(tokens) == 0 {
		return
	}
	row.CacheHost = tokens[0]
	for i := 1; i < len(tokens)-1; i++ {
		switch tokens[i] {
		case "vrf":
			row.Vrf = tokens[i+1]
		case "port":
			if v, err := strconv.Atoi(tokens[i+1]); err == nil {
				row.Port = v
			}
		}
	}
}

func (r rpkiRow) fillState(s *RpkiState) {
	if r.CacheHost != "" {
		s.CacheHost = r.CacheHost
	}
	if r.Vrf != "" {
		v := r.Vrf
		s.Vrf = &v
	}
	if r.Port > 0 {
		v := r.Port
		s.Port = &v
	}
	if r.Preference > 0 {
		v := r.Preference
		s.Preference = &v
	}
	if r.RefreshInterval > 0 {
		v := r.RefreshInterval
		s.RefreshInterval = &v
	}
	if r.RetryInterval > 0 {
		v := r.RetryInterval
		s.RetryInterval = &v
	}
	if r.ExpireInterval > 0 {
		v := r.ExpireInterval
		s.ExpireInterval = &v
	}
	if r.LocalInterface != "" {
		v := r.LocalInterface
		s.LocalInterface = &v
	}
	if r.Transport != "" {
		v := r.Transport
		s.Transport = &v
	}
}
