package l2

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

// Redundancy modes accepted by `eos:l2:EvpnEthernetSegment`.
//
// Source: EOS TOI 14728 (`redundancy single-active`), TOI 17029
// (`redundancy port-active`), EOS User Manual §18.3.10.3 (`redundancy
// all-active` default).
const (
	EsRedundancyAllActive    = "all-active"
	EsRedundancySingleActive = "single-active"
	EsRedundancyPortActive   = "port-active"
)

// Designated-forwarder election algorithms.
//
// Source: EOS TOI 17029 (`modulus` default, `hrw`, `preference`).
const (
	EsDfAlgorithmModulus    = "modulus"
	EsDfAlgorithmHrw        = "hrw"
	EsDfAlgorithmPreference = "preference"
)

// DfPreferenceMin / Max bound the preference value (0..65535) per
// EOS TOI 14728 / 17029.
const (
	DfPreferenceMin = 0
	DfPreferenceMax = 65535
)

// DefaultDfHoldTime is RFC 7432 §8.5's default DF election hold-time in
// seconds (3 s).
const DefaultDfHoldTime = 3

// Sentinel errors specific to EvpnEthernetSegment.
var (
	ErrEsParentRequired       = errors.New("evpnEthernetSegment parentInterface is required")
	ErrEsIdentifierRequired   = errors.New("evpnEthernetSegment identifier is required")
	ErrEsIdentifierBadFormat  = errors.New("evpnEthernetSegment identifier must be five colon-separated hex groups of four digits, e.g. 0011:1111:1111:1111:1111")
	ErrEsRouteTargetBadFormat = errors.New("evpnEthernetSegment routeTargetImport must be a MAC-style six-group hex string, e.g. 12:23:34:45:56:67")
	ErrEsRedundancyInvalid    = errors.New("evpnEthernetSegment redundancy must be all-active, single-active, or port-active")
	ErrEsDfAlgorithmInvalid   = errors.New("evpnEthernetSegment designatedForwarder.algorithm must be modulus, hrw, or preference")
	ErrEsDfPreferenceRange    = errors.New("evpnEthernetSegment designatedForwarder.preference must be 0..65535")
	ErrEsDfPreferenceWithAlg  = errors.New("evpnEthernetSegment designatedForwarder.preference is only valid when algorithm=preference")
	ErrEsDfHoldTimeRange      = errors.New("evpnEthernetSegment designatedForwarder.holdTime must be greater than 0")
)

var (
	esIdentifierRE  = regexp.MustCompile(`^[0-9a-fA-F]{4}(:[0-9a-fA-F]{4}){4}$`)
	esRouteTargetRE = regexp.MustCompile(`^[0-9a-fA-F]{1,2}(:[0-9a-fA-F]{1,2}){5}$`)
)

// EvpnEthernetSegment models the `evpn ethernet-segment` block configured
// inside a parent interface (Port-Channel, physical Ethernet, or L2
// sub-interface). It enables EVPN multi-homing per RFC 7432 §7.6 and §8.5.
//
// The parent interface must exist (configured separately via
// `eos:l2:PortChannel` or `eos:l2:Interface`). This resource only manages
// the nested ESI block and does not touch parent-level switchport / MTU /
// description directives.
//
// Sources (verified via arista-mcp):
//   - EOS TOI 14236 — EVPN VXLAN A/A multi-homing on Port-Channel /
//     Ethernet (`identifier`, `route-target import`).
//   - EOS TOI 14728 — Single-active multi-homing + preference-based DF
//     election.
//   - EOS TOI 17029 — DF election algorithm: modulus | hrw | preference.
//   - EOS User Manual §18.3.10.3 — RFC 7432 §7.6 (ES import RT), §8.5
//     (DF hold-time, default 3 s).
type EvpnEthernetSegment struct{}

// DesignatedForwarder configures the DF-election algorithm and timing.
type DesignatedForwarder struct {
	// Algorithm is `modulus`, `hrw`, or `preference`. Modulus is the EOS
	// default if omitted.
	Algorithm string `pulumi:"algorithm"`
	// Preference is the per-ES preference value (0..65535). Required when
	// Algorithm is `preference`; rejected otherwise.
	Preference *int `pulumi:"preference,optional"`
	// DontPreempt toggles non-revertive behaviour for preference-based
	// DF election. Optional; only meaningful when Algorithm is
	// `preference`.
	DontPreempt *bool `pulumi:"dontPreempt,optional"`
	// HoldTime overrides the default 3 s wait before the DF is elected.
	HoldTime *int `pulumi:"holdTime,optional"`
}

// EvpnEthernetSegmentArgs is the input set.
type EvpnEthernetSegmentArgs struct {
	// ParentInterface is the EOS interface that hosts the ESI block.
	// Examples: `Port-Channel100`, `Ethernet5`, `Ethernet5.10`.
	ParentInterface string `pulumi:"parentInterface"`
	// Identifier is the 10-byte ESI in colon-separated hex form
	// (`0011:1111:1111:1111:1111`).
	Identifier string `pulumi:"identifier"`
	// Redundancy is `all-active` (default), `single-active`, or
	// `port-active`.
	Redundancy *string `pulumi:"redundancy,optional"`
	// RouteTargetImport is the ES import route target (MAC-style 6-byte
	// hex), per RFC 7432 §7.6. Optional; if unset, EOS auto-derives it.
	RouteTargetImport *string `pulumi:"routeTargetImport,optional"`
	// DesignatedForwarder configures the DF-election algorithm and
	// timing.
	DesignatedForwarder *DesignatedForwarder `pulumi:"designatedForwarder,optional"`

	// Host overrides the provider-level eosUrl host for this resource.
	Host *string `pulumi:"host,optional"`
	// Username overrides the provider-level eosUsername for this resource.
	Username *string `pulumi:"username,optional"`
	// Password overrides the provider-level eosPassword for this resource.
	Password *string `provider:"secret" pulumi:"password,optional"`
}

// EvpnEthernetSegmentState mirrors Args.
type EvpnEthernetSegmentState struct {
	EvpnEthernetSegmentArgs
}

// Annotate documents the resource.
func (r *EvpnEthernetSegment) Annotate(a infer.Annotator) {
	a.Describe(&r, "An EVPN Ethernet Segment (`evpn ethernet-segment` block) configured inside a parent EOS interface (Port-Channel, physical Ethernet, or L2 sub-interface). Enables EVPN multi-homing per RFC 7432 §7.6 / §8.5.")
}

// Annotate documents EvpnEthernetSegmentArgs fields.
func (a *EvpnEthernetSegmentArgs) Annotate(an infer.Annotator) {
	an.Describe(&a.ParentInterface, "Interface that hosts the ESI block (e.g. `Port-Channel100`, `Ethernet5`, `Ethernet5.10`). The parent interface must exist; this resource only manages the nested ESI block.")
	an.Describe(&a.Identifier, "10-byte ESI in colon-separated hex (e.g. `0011:1111:1111:1111:1111`).")
	an.Describe(&a.Redundancy, "`all-active` (default), `single-active`, or `port-active`.")
	an.Describe(&a.RouteTargetImport, "ES import route target (MAC-style 6-byte hex, e.g. `12:23:34:45:56:67`) per RFC 7432 §7.6. EOS auto-derives when unset.")
	an.Describe(&a.DesignatedForwarder, "DF-election algorithm and timing.")
	an.Describe(&a.Host, "Optional management hostname override.")
	an.Describe(&a.Username, "Optional AAA username override.")
	an.Describe(&a.Password, "Optional AAA password override.")
}

// Annotate is a no-op; State has no extra fields.
func (s *EvpnEthernetSegmentState) Annotate(_ infer.Annotator) {}

// Annotate documents DesignatedForwarder fields.
func (d *DesignatedForwarder) Annotate(an infer.Annotator) {
	an.Describe(&d.Algorithm, "`modulus` (default), `hrw`, or `preference`.")
	an.Describe(&d.Preference, "Per-ES preference 0..65535. Required when algorithm=`preference`; rejected otherwise.")
	an.Describe(&d.DontPreempt, "Non-revertive behaviour for preference-based DF election. Only meaningful when algorithm=`preference`.")
	an.Describe(&d.HoldTime, "DF election hold time in seconds (default 3 per RFC 7432 §8.5).")
}

// Create configures the EVPN Ethernet Segment block.
func (*EvpnEthernetSegment) Create(ctx context.Context, req infer.CreateRequest[EvpnEthernetSegmentArgs]) (infer.CreateResponse[EvpnEthernetSegmentState], error) {
	if err := validateEvpnEthernetSegment(req.Inputs); err != nil {
		return infer.CreateResponse[EvpnEthernetSegmentState]{}, err
	}
	state := EvpnEthernetSegmentState{EvpnEthernetSegmentArgs: req.Inputs}
	if req.DryRun {
		return infer.CreateResponse[EvpnEthernetSegmentState]{ID: evpnEthernetSegmentID(req.Inputs.ParentInterface), Output: state}, nil
	}
	if err := applyEvpnEthernetSegment(ctx, req.Inputs, false); err != nil {
		return infer.CreateResponse[EvpnEthernetSegmentState]{}, fmt.Errorf("create evpn-ethernet-segment %s: %w", req.Inputs.ParentInterface, err)
	}
	return infer.CreateResponse[EvpnEthernetSegmentState]{ID: evpnEthernetSegmentID(req.Inputs.ParentInterface), Output: state}, nil
}

// Read refreshes the ESI block from the device.
func (*EvpnEthernetSegment) Read(ctx context.Context, req infer.ReadRequest[EvpnEthernetSegmentArgs, EvpnEthernetSegmentState]) (infer.ReadResponse[EvpnEthernetSegmentArgs, EvpnEthernetSegmentState], error) {
	cli, err := newClient(ctx, req.Inputs.Host, req.Inputs.Username, req.Inputs.Password)
	if err != nil {
		return infer.ReadResponse[EvpnEthernetSegmentArgs, EvpnEthernetSegmentState]{}, err
	}
	row, found, err := readEvpnEthernetSegment(ctx, cli, req.Inputs.ParentInterface)
	if err != nil {
		return infer.ReadResponse[EvpnEthernetSegmentArgs, EvpnEthernetSegmentState]{}, err
	}
	if !found {
		return infer.ReadResponse[EvpnEthernetSegmentArgs, EvpnEthernetSegmentState]{}, nil
	}
	state := EvpnEthernetSegmentState{EvpnEthernetSegmentArgs: req.Inputs}
	row.fillState(&state)
	return infer.ReadResponse[EvpnEthernetSegmentArgs, EvpnEthernetSegmentState]{
		ID:     evpnEthernetSegmentID(req.Inputs.ParentInterface),
		Inputs: req.Inputs,
		State:  state,
	}, nil
}

// Update re-applies the ESI block.
//
// EOS keeps the existing identifier / redundancy / RT lines until they are
// either replaced or explicitly negated. To clear stale rows on update we
// first issue `no evpn ethernet-segment` then re-render the desired block.
func (*EvpnEthernetSegment) Update(ctx context.Context, req infer.UpdateRequest[EvpnEthernetSegmentArgs, EvpnEthernetSegmentState]) (infer.UpdateResponse[EvpnEthernetSegmentState], error) {
	if err := validateEvpnEthernetSegment(req.Inputs); err != nil {
		return infer.UpdateResponse[EvpnEthernetSegmentState]{}, err
	}
	state := EvpnEthernetSegmentState{EvpnEthernetSegmentArgs: req.Inputs}
	if req.DryRun {
		return infer.UpdateResponse[EvpnEthernetSegmentState]{Output: state}, nil
	}
	if err := applyEvpnEthernetSegment(ctx, req.Inputs, false); err != nil {
		return infer.UpdateResponse[EvpnEthernetSegmentState]{}, fmt.Errorf("update evpn-ethernet-segment %s: %w", req.Inputs.ParentInterface, err)
	}
	return infer.UpdateResponse[EvpnEthernetSegmentState]{Output: state}, nil
}

// Delete removes the ESI block via `no evpn ethernet-segment` under the
// parent interface.
func (*EvpnEthernetSegment) Delete(ctx context.Context, req infer.DeleteRequest[EvpnEthernetSegmentState]) (infer.DeleteResponse, error) {
	if err := applyEvpnEthernetSegment(ctx, req.State.EvpnEthernetSegmentArgs, true); err != nil {
		return infer.DeleteResponse{}, fmt.Errorf("delete evpn-ethernet-segment %s: %w", req.State.ParentInterface, err)
	}
	return infer.DeleteResponse{}, nil
}

// validateEvpnEthernetSegment enforces required fields, format, and
// algorithm-specific constraints.
func validateEvpnEthernetSegment(args EvpnEthernetSegmentArgs) error {
	if strings.TrimSpace(args.ParentInterface) == "" {
		return ErrEsParentRequired
	}
	if strings.TrimSpace(args.Identifier) == "" {
		return ErrEsIdentifierRequired
	}
	if !esIdentifierRE.MatchString(args.Identifier) {
		return fmt.Errorf("%w (got %q)", ErrEsIdentifierBadFormat, args.Identifier)
	}
	if args.RouteTargetImport != nil && *args.RouteTargetImport != "" &&
		!esRouteTargetRE.MatchString(*args.RouteTargetImport) {
		return fmt.Errorf("%w (got %q)", ErrEsRouteTargetBadFormat, *args.RouteTargetImport)
	}
	if args.Redundancy != nil {
		switch *args.Redundancy {
		case "", EsRedundancyAllActive, EsRedundancySingleActive, EsRedundancyPortActive:
		default:
			return fmt.Errorf("%w (got %q)", ErrEsRedundancyInvalid, *args.Redundancy)
		}
	}
	if args.DesignatedForwarder != nil {
		if err := validateDesignatedForwarder(*args.DesignatedForwarder); err != nil {
			return err
		}
	}
	return nil
}

func validateDesignatedForwarder(d DesignatedForwarder) error {
	switch d.Algorithm {
	case "", EsDfAlgorithmModulus, EsDfAlgorithmHrw, EsDfAlgorithmPreference:
	default:
		return fmt.Errorf("%w (got %q)", ErrEsDfAlgorithmInvalid, d.Algorithm)
	}
	if d.Preference != nil {
		if d.Algorithm != EsDfAlgorithmPreference {
			return ErrEsDfPreferenceWithAlg
		}
		if *d.Preference < DfPreferenceMin || *d.Preference > DfPreferenceMax {
			return fmt.Errorf("%w (got %d)", ErrEsDfPreferenceRange, *d.Preference)
		}
	}
	if d.HoldTime != nil && *d.HoldTime <= 0 {
		return fmt.Errorf("%w (got %d)", ErrEsDfHoldTimeRange, *d.HoldTime)
	}
	return nil
}

func evpnEthernetSegmentID(parent string) string {
	return "evpn-ethernet-segment/" + parent
}

func applyEvpnEthernetSegment(ctx context.Context, args EvpnEthernetSegmentArgs, remove bool) error {
	cli, err := newClient(ctx, args.Host, args.Username, args.Password)
	if err != nil {
		return err
	}
	cfg := config.FromContext(ctx)
	sessName := cfg.SessionPrefix() + "es-" + sanitizeForSession(args.ParentInterface)

	sess, err := cli.OpenSession(ctx, sessName)
	if err != nil {
		return err
	}
	cmds := buildEvpnEthernetSegmentCmds(args, remove)
	if stageErr := sess.Stage(ctx, cmds); stageErr != nil {
		if abortErr := sess.Abort(ctx); abortErr != nil {
			return errors.Join(stageErr, abortErr)
		}
		return stageErr
	}
	return sess.Commit(ctx)
}

// buildEvpnEthernetSegmentCmds renders the ordered command list.
//
// Update-flow rationale: we always emit `no evpn ethernet-segment` first
// (when not removing) so stale `route-target import`, `redundancy`, and
// `designated-forwarder` lines from an earlier apply are guaranteed gone.
// EOS treats the inner block as a list-of-knobs that is only fully cleared
// by negating the parent.
func buildEvpnEthernetSegmentCmds(args EvpnEthernetSegmentArgs, remove bool) []string {
	cmds := []string{"interface " + args.ParentInterface, "no evpn ethernet-segment"}
	if remove {
		return cmds
	}
	cmds = append(cmds,
		"evpn ethernet-segment",
		"identifier "+args.Identifier,
	)
	if args.Redundancy != nil && *args.Redundancy != "" {
		cmds = append(cmds, "redundancy "+*args.Redundancy)
	}
	if args.RouteTargetImport != nil && *args.RouteTargetImport != "" {
		cmds = append(cmds, "route-target import "+*args.RouteTargetImport)
	}
	if args.DesignatedForwarder != nil {
		cmds = append(cmds, dfElectionCmds(*args.DesignatedForwarder)...)
	}
	return cmds
}

// dfElectionCmds renders the `designated-forwarder ...` directives.
func dfElectionCmds(d DesignatedForwarder) []string {
	var cmds []string
	if d.Algorithm != "" {
		line := "designated-forwarder election algorithm " + d.Algorithm
		if d.Algorithm == EsDfAlgorithmPreference && d.Preference != nil {
			line += " " + strconv.Itoa(*d.Preference)
			if d.DontPreempt != nil && *d.DontPreempt {
				line += " dont-preempt"
			}
		}
		cmds = append(cmds, line)
	}
	if d.HoldTime != nil && *d.HoldTime > 0 {
		cmds = append(cmds, "designated-forwarder election hold-time "+strconv.Itoa(*d.HoldTime))
	}
	return cmds
}

// evpnEthernetSegmentRow holds the parsed live state.
type evpnEthernetSegmentRow struct {
	Identifier        string
	Redundancy        string
	RouteTargetImport string
	DfAlgorithm       string
	DfPreference      int
	DfDontPreempt     bool
	DfHoldTime        int
}

// readEvpnEthernetSegment returns the live ESI state, or (false, nil) when
// the parent interface lacks an `evpn ethernet-segment` block.
func readEvpnEthernetSegment(ctx context.Context, cli *eapi.Client, parent string) (evpnEthernetSegmentRow, bool, error) {
	resp, err := cli.RunCmds(ctx,
		[]string{"show running-config interfaces " + parent},
		"text")
	if err != nil {
		return evpnEthernetSegmentRow{}, false, err
	}
	if len(resp) == 0 {
		return evpnEthernetSegmentRow{}, false, nil
	}
	out, _ := resp[0]["output"].(string)
	if out == "" {
		return evpnEthernetSegmentRow{}, false, nil
	}
	return parseEvpnEthernetSegmentConfig(out, parent)
}

// parseEvpnEthernetSegmentConfig is exposed for unit tests.
func parseEvpnEthernetSegmentConfig(out, parent string) (evpnEthernetSegmentRow, bool, error) {
	header := "interface " + parent
	if !strings.Contains(out, header) {
		return evpnEthernetSegmentRow{}, false, nil
	}
	row := evpnEthernetSegmentRow{}
	inEs := false
	found := false
	for raw := range strings.SplitSeq(out, "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case line == "evpn ethernet-segment":
			inEs = true
			found = true
			continue
		case strings.HasPrefix(line, "interface ") && line != header:
			inEs = false
		}
		if !inEs {
			continue
		}
		switch {
		case strings.HasPrefix(line, "identifier "):
			row.Identifier = strings.TrimPrefix(line, "identifier ")
		case strings.HasPrefix(line, "redundancy "):
			row.Redundancy = strings.TrimPrefix(line, "redundancy ")
		case strings.HasPrefix(line, "route-target import "):
			row.RouteTargetImport = strings.TrimPrefix(line, "route-target import ")
		case strings.HasPrefix(line, "designated-forwarder election algorithm "):
			parseDfAlgorithmLine(line, &row)
		case strings.HasPrefix(line, "designated-forwarder election hold-time "):
			if v, err := strconv.Atoi(strings.TrimPrefix(line, "designated-forwarder election hold-time ")); err == nil {
				row.DfHoldTime = v
			}
		}
	}
	if !found {
		return evpnEthernetSegmentRow{}, false, nil
	}
	return row, true, nil
}

// parseDfAlgorithmLine extracts the algorithm + optional preference value
// + dont-preempt flag from `designated-forwarder election algorithm ...`.
func parseDfAlgorithmLine(line string, row *evpnEthernetSegmentRow) {
	rest := strings.TrimPrefix(line, "designated-forwarder election algorithm ")
	parts := strings.Fields(rest)
	if len(parts) == 0 {
		return
	}
	row.DfAlgorithm = parts[0]
	if row.DfAlgorithm != EsDfAlgorithmPreference {
		return
	}
	if len(parts) >= 2 {
		if v, err := strconv.Atoi(parts[1]); err == nil {
			row.DfPreference = v
		}
	}
	for _, tok := range parts[2:] {
		if tok == "dont-preempt" {
			row.DfDontPreempt = true
		}
	}
}

func (r evpnEthernetSegmentRow) fillState(s *EvpnEthernetSegmentState) {
	if r.Identifier != "" {
		s.Identifier = r.Identifier
	}
	if r.Redundancy != "" {
		v := r.Redundancy
		s.Redundancy = &v
	}
	if r.RouteTargetImport != "" {
		v := r.RouteTargetImport
		s.RouteTargetImport = &v
	}
	if r.DfAlgorithm != "" || r.DfHoldTime > 0 {
		df := DesignatedForwarder{Algorithm: r.DfAlgorithm}
		if r.DfPreference > 0 || r.DfAlgorithm == EsDfAlgorithmPreference {
			pref := r.DfPreference
			df.Preference = &pref
		}
		if r.DfDontPreempt {
			b := true
			df.DontPreempt = &b
		}
		if r.DfHoldTime > 0 {
			h := r.DfHoldTime
			df.HoldTime = &h
		}
		s.DesignatedForwarder = &df
	}
}
