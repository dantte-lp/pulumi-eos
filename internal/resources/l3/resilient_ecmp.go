package l3

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strconv"
	"strings"

	"github.com/pulumi/pulumi-go-provider/infer"

	"github.com/dantte-lp/pulumi-eos/internal/client/eapi"
	"github.com/dantte-lp/pulumi-eos/internal/config"
)

// Sentinel errors specific to ResilientEcmp.
var (
	ErrResilientEcmpPrefixInvalid   = errors.New("resilientEcmp prefix must be a valid IPv4 or IPv6 CIDR")
	ErrResilientEcmpAFMismatch      = errors.New("resilientEcmp ipFamily must be 'ipv4' or 'ipv6' and match the prefix family")
	ErrResilientEcmpCapacityRange   = errors.New("resilientEcmp capacity must be in 1..1024 (per-platform max varies; see EOS TOI 13938 §Limitations)")
	ErrResilientEcmpRedundancyRange = errors.New("resilientEcmp redundancy must be in 1..1023 (per-platform max varies; see EOS TOI 13938 §Limitations)")
	ErrResilientEcmpProductExceeded = errors.New("resilientEcmp capacity * redundancy exceeds per-platform limit (e.g. 1024 on 7050X3, 2047 on 7280/7500/7800; see EOS TOI 13938 §Limitations)")
)

// validResilientEcmpFamily enumerates accepted address families.
var validResilientEcmpFamily = map[string]struct{}{
	"ipv4": {},
	"ipv6": {},
}

// ResilientEcmp models a top-level
// `[ip|ipv6] hardware fib ecmp resilience <prefix> capacity N
// redundancy M [ordered]` line.
//
// Resilient ECMP keeps the total number of next-hops constant by
// replicating the surviving next-hops into the slots vacated by
// failed peers, which preserves flow-to-slot affinity across link
// flaps. Useful when a fabric uses ECMP for load balancing and a
// re-hash on link flap is undesirable (e.g. stateful service paths
// behind an anycast VIP).
//
// Platform reach (per EOS TOI 13938 §Description): supported on
// DCS-7050, 7160, 7280, 73xx, 75xx, 78xx hardware. cEOSLab 4.36
// returns "Unavailable command (not supported on this hardware
// platform)" at commit — the resource ships as input-shape only and
// the integration body does NOT exercise this command. Production
// deployments on the listed hardware accept it directly.
//
// Per-platform capacity / redundancy maxima (TOI 13938 §Limitations):
//
//   - 7050X3: capacity * redundancy ≤ 1024.
//   - 7050X4: capacity * redundancy ≤ 128.
//   - 7170B:  capacity ≤ 64, redundancy ≤ 32.
//   - 7280 / 7500 / 7800: capacity ≤ 600, redundancy ≤ 1023,
//     product ≤ 2047.
//
// The resource validator caps the per-axis maxima at the most
// permissive value (capacity 1024, redundancy 1023, product unchecked
// — production fabric authors are expected to know the device family
// limits). EOS itself emits a parser error at commit when the value
// exceeds the device limit, surfaced through `pulumi up`.
//
// Source: EOS User Manual §14.1.14.54 (`ip hardware fib ecmp
// resilience`); TOI 13938 (Resilient ECMP); TOI 15844 (consistent
// ordering for resilient ECMP — adds the optional `ordered` keyword).
type ResilientEcmp struct{}

// ResilientEcmpArgs is the resource input set.
type ResilientEcmpArgs struct {
	// Prefix is the IPv4 or IPv6 CIDR scope. PK component (with IpFamily).
	Prefix string `pulumi:"prefix"`
	// IpFamily must match the prefix's family — "ipv4" or "ipv6".
	// PK component. Defaults to the family parsed from `prefix` when
	// empty, but is required for state-shape consistency on Read.
	IpFamily *string `pulumi:"ipFamily,optional"`
	// Capacity is the maximum number of distinct next-hops in the
	// resilient slot table for this prefix.
	Capacity int `pulumi:"capacity"`
	// Redundancy is the slot-replication factor. Total slot table
	// size = capacity × redundancy.
	Redundancy int `pulumi:"redundancy"`
	// Ordered enables the deterministic-ordering variant introduced
	// in TOI 15844. Pairs with `bgp bestpath tie-break router-id`
	// + `rib fib fec ecmp ordered` under `router general`.
	Ordered *bool `pulumi:"ordered,optional"`

	// Host overrides the provider-level eosUrl host for this resource.
	Host *string `pulumi:"host,optional"`
	// Username overrides the provider-level eosUsername for this resource.
	Username *string `pulumi:"username,optional"`
	// Password overrides the provider-level eosPassword for this resource.
	Password *string `provider:"secret" pulumi:"password,optional"`
}

// ResilientEcmpState mirrors Args.
type ResilientEcmpState struct {
	ResilientEcmpArgs
}

// Annotate documents the resource.
func (r *ResilientEcmp) Annotate(a infer.Annotator) {
	a.Describe(&r, "An EOS resilient-ECMP slot-table binding for a prefix scope. Top-level `[ip|ipv6] hardware fib ecmp resilience <prefix> capacity N redundancy M [ordered]`. Platform-conditional: works on DCS-7050/7160/7280/73xx/75xx/78xx hardware; cEOSLab returns 'Unavailable command' at commit (TOI 13938).")
}

// Annotate documents ResilientEcmpArgs fields.
func (a *ResilientEcmpArgs) Annotate(an infer.Annotator) {
	an.Describe(&a.Prefix, "IPv4 or IPv6 CIDR scope (PK component).")
	an.Describe(&a.IpFamily, "'ipv4' or 'ipv6' — must match the prefix family. Defaults from prefix when empty; required on Read for state shape.")
	an.Describe(&a.Capacity, "Maximum distinct next-hops in the slot table (1..1024; per-platform max varies — see TOI 13938 §Limitations).")
	an.Describe(&a.Redundancy, "Slot replication factor (1..1023; per-platform max varies). Total slot table = capacity × redundancy.")
	an.Describe(&a.Ordered, "Enable deterministic ordering (TOI 15844). Pairs with router-bgp `bestpath tie-break router-id` + `router general / rib fib fec ecmp ordered`.")
	an.Describe(&a.Host, "Optional management hostname override.")
	an.Describe(&a.Username, "Optional AAA username override.")
	an.Describe(&a.Password, "Optional AAA password override.")
}

// Annotate is a no-op; State has no extra fields.
func (s *ResilientEcmpState) Annotate(_ infer.Annotator) {}

// Create stages the resilience binding.
func (*ResilientEcmp) Create(ctx context.Context, req infer.CreateRequest[ResilientEcmpArgs]) (infer.CreateResponse[ResilientEcmpState], error) {
	if err := validateResilientEcmp(req.Inputs); err != nil {
		return infer.CreateResponse[ResilientEcmpState]{}, err
	}
	state := ResilientEcmpState{ResilientEcmpArgs: req.Inputs}
	if req.DryRun {
		return infer.CreateResponse[ResilientEcmpState]{ID: resilientEcmpID(req.Inputs.Prefix, deref(req.Inputs.IpFamily)), Output: state}, nil
	}
	if err := applyResilientEcmp(ctx, req.Inputs, false); err != nil {
		return infer.CreateResponse[ResilientEcmpState]{}, fmt.Errorf("create resilientEcmp %s: %w", req.Inputs.Prefix, err)
	}
	return infer.CreateResponse[ResilientEcmpState]{ID: resilientEcmpID(req.Inputs.Prefix, deref(req.Inputs.IpFamily)), Output: state}, nil
}

// Read refreshes binding state from the device.
func (*ResilientEcmp) Read(ctx context.Context, req infer.ReadRequest[ResilientEcmpArgs, ResilientEcmpState]) (infer.ReadResponse[ResilientEcmpArgs, ResilientEcmpState], error) {
	cli, err := newClient(ctx, req.Inputs.Host, req.Inputs.Username, req.Inputs.Password)
	if err != nil {
		return infer.ReadResponse[ResilientEcmpArgs, ResilientEcmpState]{}, err
	}
	current, found, err := readResilientEcmp(ctx, cli, req.Inputs.Prefix, deref(req.Inputs.IpFamily))
	if err != nil {
		return infer.ReadResponse[ResilientEcmpArgs, ResilientEcmpState]{}, err
	}
	if !found {
		return infer.ReadResponse[ResilientEcmpArgs, ResilientEcmpState]{}, nil
	}
	state := ResilientEcmpState{ResilientEcmpArgs: req.Inputs}
	current.fillState(&state)
	return infer.ReadResponse[ResilientEcmpArgs, ResilientEcmpState]{
		ID:     resilientEcmpID(req.Inputs.Prefix, deref(req.Inputs.IpFamily)),
		Inputs: req.Inputs,
		State:  state,
	}, nil
}

// Update re-applies the binding. Re-emit only — EOS' session diff
// computes the delta against the current line.
func (*ResilientEcmp) Update(ctx context.Context, req infer.UpdateRequest[ResilientEcmpArgs, ResilientEcmpState]) (infer.UpdateResponse[ResilientEcmpState], error) {
	if err := validateResilientEcmp(req.Inputs); err != nil {
		return infer.UpdateResponse[ResilientEcmpState]{}, err
	}
	state := ResilientEcmpState{ResilientEcmpArgs: req.Inputs}
	if req.DryRun {
		return infer.UpdateResponse[ResilientEcmpState]{Output: state}, nil
	}
	if err := applyResilientEcmp(ctx, req.Inputs, false); err != nil {
		return infer.UpdateResponse[ResilientEcmpState]{}, fmt.Errorf("update resilientEcmp %s: %w", req.Inputs.Prefix, err)
	}
	return infer.UpdateResponse[ResilientEcmpState]{Output: state}, nil
}

// Delete removes the binding.
func (*ResilientEcmp) Delete(ctx context.Context, req infer.DeleteRequest[ResilientEcmpState]) (infer.DeleteResponse, error) {
	if err := applyResilientEcmp(ctx, req.State.ResilientEcmpArgs, true); err != nil {
		return infer.DeleteResponse{}, fmt.Errorf("delete resilientEcmp %s: %w", req.State.Prefix, err)
	}
	return infer.DeleteResponse{}, nil
}

// --- helpers ---------------------------------------------------------------

// canonicaliseFamily picks the family from args; falls back to
// inferring from the prefix.
func canonicaliseFamily(args ResilientEcmpArgs) (string, error) {
	pfx, err := netip.ParsePrefix(args.Prefix)
	if err != nil {
		return "", fmt.Errorf("%w: %q", ErrResilientEcmpPrefixInvalid, args.Prefix)
	}
	wantFamily := "ipv4"
	if pfx.Addr().Is6() {
		wantFamily = "ipv6"
	}
	if args.IpFamily == nil || *args.IpFamily == "" {
		return wantFamily, nil
	}
	if _, ok := validResilientEcmpFamily[*args.IpFamily]; !ok {
		return "", fmt.Errorf("%w: got %q", ErrResilientEcmpAFMismatch, *args.IpFamily)
	}
	if *args.IpFamily != wantFamily {
		return "", fmt.Errorf("%w: prefix is %s but ipFamily=%q", ErrResilientEcmpAFMismatch, wantFamily, *args.IpFamily)
	}
	return wantFamily, nil
}

func validateResilientEcmp(args ResilientEcmpArgs) error {
	if _, err := canonicaliseFamily(args); err != nil {
		return err
	}
	if args.Capacity < 1 || args.Capacity > 1024 {
		return fmt.Errorf("%w: got %d", ErrResilientEcmpCapacityRange, args.Capacity)
	}
	if args.Redundancy < 1 || args.Redundancy > 1023 {
		return fmt.Errorf("%w: got %d", ErrResilientEcmpRedundancyRange, args.Redundancy)
	}
	// Soft cap on the product — the most permissive platform allows
	// 2047. EOS itself rejects bigger values at commit on stricter
	// platforms; we surface that runtime error rather than baking
	// the per-platform table into validation.
	if args.Capacity*args.Redundancy > 2047 {
		return fmt.Errorf("%w: %d * %d = %d", ErrResilientEcmpProductExceeded, args.Capacity, args.Redundancy, args.Capacity*args.Redundancy)
	}
	return nil
}

func resilientEcmpID(prefix, family string) string {
	if family == "" {
		family = "ipv4"
	}
	return fmt.Sprintf("resilient-ecmp/%s/%s", family, prefix)
}

// keyword returns "ip" or "ipv6" depending on the family token.
func resilientEcmpKeyword(family string) string {
	if family == "ipv6" {
		return "ipv6"
	}
	return "ip"
}

func applyResilientEcmp(ctx context.Context, args ResilientEcmpArgs, remove bool) error {
	family, err := canonicaliseFamily(args)
	if err != nil {
		return err
	}
	cli, err := newClient(ctx, args.Host, args.Username, args.Password)
	if err != nil {
		return err
	}
	cfg := config.FromContext(ctx)
	sessName := cfg.SessionPrefix() + "recmp-" + sanitizePrefixListName(args.Prefix)
	sess, err := cli.OpenSession(ctx, sessName)
	if err != nil {
		return err
	}
	cmds := buildResilientEcmpCmds(args, family, remove)
	if stageErr := sess.Stage(ctx, cmds); stageErr != nil {
		if abortErr := sess.Abort(ctx); abortErr != nil {
			return errors.Join(stageErr, abortErr)
		}
		return stageErr
	}
	return sess.Commit(ctx)
}

// buildResilientEcmpCmds renders the staged CLI block.
//
//	[ip|ipv6] hardware fib ecmp resilience <prefix> capacity N redundancy M [ordered]
//
// Delete uses `no [ip|ipv6] hardware fib ecmp resilience <prefix>`.
func buildResilientEcmpCmds(args ResilientEcmpArgs, family string, remove bool) []string {
	keyword := resilientEcmpKeyword(family)
	header := keyword + " hardware fib ecmp resilience " + args.Prefix
	if remove {
		return []string{"no " + header}
	}
	line := header +
		" capacity " + strconv.Itoa(args.Capacity) +
		" redundancy " + strconv.Itoa(args.Redundancy)
	if args.Ordered != nil && *args.Ordered {
		line += " ordered"
	}
	return []string{line}
}

// resilientEcmpRow is the parsed live state.
type resilientEcmpRow struct {
	Prefix     string
	Family     string
	Capacity   int
	Redundancy int
	Ordered    bool
}

// readResilientEcmp returns the live binding or (false, nil) when
// absent. Source: `show running-config | grep "fib ecmp resilience"`.
func readResilientEcmp(ctx context.Context, cli *eapi.Client, prefix, family string) (resilientEcmpRow, bool, error) {
	resp, err := cli.RunCmds(ctx,
		[]string{"show running-config | grep ecmp"},
		"text")
	if err != nil {
		return resilientEcmpRow{}, false, err
	}
	if len(resp) == 0 {
		return resilientEcmpRow{}, false, nil
	}
	out, _ := resp[0]["output"].(string)
	if family == "" {
		family = "ipv4"
	}
	row, found := parseResilientEcmpLines(out, prefix, family)
	return row, found, nil
}

// parseResilientEcmpLines walks the grep output for one specific
// (family, prefix) row.
func parseResilientEcmpLines(out, prefix, family string) (resilientEcmpRow, bool) {
	keyword := resilientEcmpKeyword(family)
	target := keyword + " hardware fib ecmp resilience " + prefix + " "
	for raw := range strings.SplitSeq(out, "\n") {
		line := strings.TrimSpace(raw)
		rest, ok := strings.CutPrefix(line, target)
		if !ok {
			continue
		}
		row := resilientEcmpRow{Prefix: prefix, Family: family}
		applyResilientEcmpLine(&row, rest)
		return row, true
	}
	return resilientEcmpRow{}, false
}

// applyResilientEcmpLine populates the trailing
// `capacity N redundancy M [ordered]` segment.
func applyResilientEcmpLine(row *resilientEcmpRow, rest string) {
	tokens := strings.Fields(rest)
	for i := range tokens {
		switch tokens[i] {
		case "capacity":
			if i+1 < len(tokens) {
				if v, err := strconv.Atoi(tokens[i+1]); err == nil {
					row.Capacity = v
				}
			}
		case "redundancy":
			if i+1 < len(tokens) {
				if v, err := strconv.Atoi(tokens[i+1]); err == nil {
					row.Redundancy = v
				}
			}
		case "ordered":
			row.Ordered = true
		}
	}
}

// fillState writes the parsed row back to State.
func (r resilientEcmpRow) fillState(s *ResilientEcmpState) {
	if r.Family != "" {
		v := r.Family
		s.IpFamily = &v
	}
	if r.Capacity > 0 {
		s.Capacity = r.Capacity
	}
	if r.Redundancy > 0 {
		s.Redundancy = r.Redundancy
	}
	if r.Ordered {
		v := true
		s.Ordered = &v
	}
}
