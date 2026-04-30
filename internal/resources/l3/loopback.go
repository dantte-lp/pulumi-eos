// Package l3 contains Pulumi resources whose token starts with `eos:l3:`.
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

// Sentinel errors returned by package l3.
var (
	ErrLoopbackNumberOutOfRange = errors.New("loopback number must be in 0..1000")
	ErrLoopbackBadIPv4          = errors.New("loopback ipAddress must be a valid IPv4 CIDR (e.g. 10.0.0.1/32)")
	ErrLoopbackBadIPv6          = errors.New("loopback ipv6Address must be a valid IPv6 CIDR (e.g. fc00::1/128)")
	ErrLoopbackNoAddress        = errors.New("loopback requires at least one of ipAddress or ipv6Address")
)

// Loopback models an EOS Loopback interface (`interface Loopback<N>`).
//
// Source: EOS User Manual §10 — Layer 3 interfaces. Loopback interfaces are
// the canonical anchor for BGP router-id, EVPN VTEP source IP, and BFD
// multihop sessions, so the resource carries the small surface those
// upper-layer features require: a per-VRF binding, a primary IPv4 (CIDR),
// an optional IPv6 (CIDR), description, and an optional administrative
// shutdown.
type Loopback struct{}

// LoopbackArgs is the input set.
type LoopbackArgs struct {
	// Number is the Loopback identifier (0..1000). EOS conventionally
	// uses Loopback0 for router-id / overlay source and Loopback1 for
	// per-device VTEP IP.
	Number int `pulumi:"number"`
	// IPAddress is the primary IPv4 address in CIDR (e.g. `10.0.0.1/32`).
	// Optional only when `ipv6Address` is set.
	IPAddress *string `pulumi:"ipAddress,optional"`
	// IPv6Address is the primary IPv6 address in CIDR (e.g.
	// `fc00::1/128`). Optional only when `ipAddress` is set.
	IPv6Address *string `pulumi:"ipv6Address,optional"`
	// Vrf places the loopback in a non-default VRF. Optional.
	Vrf *string `pulumi:"vrf,optional"`
	// Description sets the interface description.
	Description *string `pulumi:"description,optional"`
	// Shutdown administratively shuts the interface down.
	Shutdown *bool `pulumi:"shutdown,optional"`

	// Host overrides the provider-level eosUrl host for this resource.
	Host *string `pulumi:"host,optional"`
	// Username overrides the provider-level eosUsername for this resource.
	Username *string `pulumi:"username,optional"`
	// Password overrides the provider-level eosPassword for this resource.
	Password *string `provider:"secret" pulumi:"password,optional"`
}

// LoopbackState mirrors Args.
type LoopbackState struct {
	LoopbackArgs
}

// Annotate documents the resource.
func (r *Loopback) Annotate(a infer.Annotator) {
	a.Describe(&r, "An EOS Loopback interface created over an eAPI configuration session. Suitable as router-id, BGP/BFD source, EVPN VTEP source.")
}

// Annotate documents LoopbackArgs fields.
func (a *LoopbackArgs) Annotate(an infer.Annotator) {
	an.Describe(&a.Number, "Loopback identifier (0..1000). Conventionally 0 for router-id, 1 for VTEP source.")
	an.Describe(&a.IPAddress, "Primary IPv4 address in CIDR notation (e.g. 10.0.0.1/32).")
	an.Describe(&a.IPv6Address, "Primary IPv6 address in CIDR notation (e.g. fc00::1/128).")
	an.Describe(&a.Vrf, "Optional VRF name. Loopback lives in the default VRF when unset.")
	an.Describe(&a.Description, "Interface description.")
	an.Describe(&a.Shutdown, "When true, the loopback is administratively shut down.")
	an.Describe(&a.Host, "Optional management hostname override.")
	an.Describe(&a.Username, "Optional AAA username override.")
	an.Describe(&a.Password, "Optional AAA password override.")
}

// Annotate is a no-op; State has no extra fields.
func (s *LoopbackState) Annotate(_ infer.Annotator) {}

// Create configures the loopback.
func (*Loopback) Create(ctx context.Context, req infer.CreateRequest[LoopbackArgs]) (infer.CreateResponse[LoopbackState], error) {
	if err := validateLoopback(req.Inputs); err != nil {
		return infer.CreateResponse[LoopbackState]{}, err
	}
	state := LoopbackState{LoopbackArgs: req.Inputs}
	if req.DryRun {
		return infer.CreateResponse[LoopbackState]{ID: loopbackID(req.Inputs.Number), Output: state}, nil
	}
	if err := applyLoopback(ctx, req.Inputs, false); err != nil {
		return infer.CreateResponse[LoopbackState]{}, fmt.Errorf("create loopback %d: %w", req.Inputs.Number, err)
	}
	return infer.CreateResponse[LoopbackState]{ID: loopbackID(req.Inputs.Number), Output: state}, nil
}

// Read refreshes loopback state from the device.
func (*Loopback) Read(ctx context.Context, req infer.ReadRequest[LoopbackArgs, LoopbackState]) (infer.ReadResponse[LoopbackArgs, LoopbackState], error) {
	cli, err := newClient(ctx, req.Inputs.Host, req.Inputs.Username, req.Inputs.Password)
	if err != nil {
		return infer.ReadResponse[LoopbackArgs, LoopbackState]{}, err
	}
	current, found, err := readLoopback(ctx, cli, req.Inputs.Number)
	if err != nil {
		return infer.ReadResponse[LoopbackArgs, LoopbackState]{}, err
	}
	if !found {
		return infer.ReadResponse[LoopbackArgs, LoopbackState]{}, nil
	}
	state := LoopbackState{LoopbackArgs: req.Inputs}
	current.fillState(&state)
	return infer.ReadResponse[LoopbackArgs, LoopbackState]{
		ID:     loopbackID(req.Inputs.Number),
		Inputs: req.Inputs,
		State:  state,
	}, nil
}

// Update re-applies the loopback configuration.
func (*Loopback) Update(ctx context.Context, req infer.UpdateRequest[LoopbackArgs, LoopbackState]) (infer.UpdateResponse[LoopbackState], error) {
	if err := validateLoopback(req.Inputs); err != nil {
		return infer.UpdateResponse[LoopbackState]{}, err
	}
	state := LoopbackState{LoopbackArgs: req.Inputs}
	if req.DryRun {
		return infer.UpdateResponse[LoopbackState]{Output: state}, nil
	}
	if err := applyLoopback(ctx, req.Inputs, false); err != nil {
		return infer.UpdateResponse[LoopbackState]{}, fmt.Errorf("update loopback %d: %w", req.Inputs.Number, err)
	}
	return infer.UpdateResponse[LoopbackState]{Output: state}, nil
}

// Delete removes the loopback.
func (*Loopback) Delete(ctx context.Context, req infer.DeleteRequest[LoopbackState]) (infer.DeleteResponse, error) {
	if err := applyLoopback(ctx, req.State.LoopbackArgs, true); err != nil {
		return infer.DeleteResponse{}, fmt.Errorf("delete loopback %d: %w", req.State.Number, err)
	}
	return infer.DeleteResponse{}, nil
}

// --- helpers ---------------------------------------------------------------

func validateLoopback(args LoopbackArgs) error {
	if args.Number < 0 || args.Number > 1000 {
		return fmt.Errorf("%w: got %d", ErrLoopbackNumberOutOfRange, args.Number)
	}
	hasV4 := args.IPAddress != nil && *args.IPAddress != ""
	hasV6 := args.IPv6Address != nil && *args.IPv6Address != ""
	if !hasV4 && !hasV6 {
		return ErrLoopbackNoAddress
	}
	if hasV4 {
		if pfx, err := netip.ParsePrefix(*args.IPAddress); err != nil || !pfx.Addr().Is4() {
			return fmt.Errorf("%w: %q", ErrLoopbackBadIPv4, *args.IPAddress)
		}
	}
	if hasV6 {
		if pfx, err := netip.ParsePrefix(*args.IPv6Address); err != nil || !pfx.Addr().Is6() || pfx.Addr().Is4In6() {
			return fmt.Errorf("%w: %q", ErrLoopbackBadIPv6, *args.IPv6Address)
		}
	}
	return nil
}

func loopbackID(n int) string { return "loopback/" + strconv.Itoa(n) }

func loopbackName(n int) string { return "Loopback" + strconv.Itoa(n) }

func applyLoopback(ctx context.Context, args LoopbackArgs, remove bool) error {
	cli, err := newClient(ctx, args.Host, args.Username, args.Password)
	if err != nil {
		return err
	}
	cfg := config.FromContext(ctx)
	sessName := cfg.SessionPrefix() + "loopback-" + strconv.Itoa(args.Number)

	sess, err := cli.OpenSession(ctx, sessName)
	if err != nil {
		return err
	}
	cmds := buildLoopbackCmds(args, remove)
	if stageErr := sess.Stage(ctx, cmds); stageErr != nil {
		if abortErr := sess.Abort(ctx); abortErr != nil {
			return errors.Join(stageErr, abortErr)
		}
		return stageErr
	}
	return sess.Commit(ctx)
}

func buildLoopbackCmds(args LoopbackArgs, remove bool) []string {
	if remove {
		return []string{"no interface " + loopbackName(args.Number)}
	}
	cmds := []string{"interface " + loopbackName(args.Number)}
	if args.Description != nil && *args.Description != "" {
		cmds = append(cmds, "description "+*args.Description)
	}
	if args.Vrf != nil && *args.Vrf != "" {
		cmds = append(cmds, "vrf "+*args.Vrf)
	}
	if args.IPAddress != nil && *args.IPAddress != "" {
		cmds = append(cmds, "ip address "+*args.IPAddress)
	}
	if args.IPv6Address != nil && *args.IPv6Address != "" {
		cmds = append(cmds, "ipv6 address "+*args.IPv6Address)
	}
	if args.Shutdown != nil && *args.Shutdown {
		cmds = append(cmds, "shutdown")
	} else {
		cmds = append(cmds, "no shutdown")
	}
	return cmds
}

// loopbackRow is the parsed live state we care about.
type loopbackRow struct {
	Description string
	Vrf         string
	Address     string
	IPv6Address string
	Shutdown    bool
}

// readLoopback returns the live loopback state, or (false, nil) when absent.
//
// Source of truth: `show running-config interfaces Loopback<n>`. The
// structured JSON from `show interfaces` does not surface VRF binding or
// IPv6 address-form, so we parse the raw config block.
func readLoopback(ctx context.Context, cli *eapi.Client, n int) (loopbackRow, bool, error) {
	resp, err := cli.RunCmds(ctx,
		[]string{"show running-config interfaces " + loopbackName(n)},
		"text")
	if err != nil {
		return loopbackRow{}, false, err
	}
	if len(resp) == 0 {
		return loopbackRow{}, false, nil
	}
	out, _ := resp[0]["output"].(string)
	if out == "" {
		return loopbackRow{}, false, nil
	}
	return parseLoopbackConfig(out, n)
}

// parseLoopbackConfig extracts the loopback fields from raw `show
// running-config interfaces Loopback<n>` output. Exposed for unit tests.
func parseLoopbackConfig(out string, n int) (loopbackRow, bool, error) {
	header := "interface " + loopbackName(n)
	if !strings.Contains(out, header) {
		return loopbackRow{}, false, nil
	}
	row := loopbackRow{}
	for raw := range strings.SplitSeq(out, "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(line, "description "):
			row.Description = strings.TrimPrefix(line, "description ")
		case strings.HasPrefix(line, "vrf "):
			row.Vrf = strings.TrimPrefix(line, "vrf ")
		case strings.HasPrefix(line, "ipv6 address "):
			row.IPv6Address = strings.TrimPrefix(line, "ipv6 address ")
		case strings.HasPrefix(line, "ip address "):
			row.Address = strings.TrimPrefix(line, "ip address ")
		case line == "shutdown":
			row.Shutdown = true
		}
	}
	return row, true, nil
}

func (r loopbackRow) fillState(s *LoopbackState) {
	if r.Description != "" {
		v := r.Description
		s.Description = &v
	}
	if r.Vrf != "" {
		v := r.Vrf
		s.Vrf = &v
	}
	if r.Address != "" {
		v := r.Address
		s.IPAddress = &v
	}
	if r.IPv6Address != "" {
		v := r.IPv6Address
		s.IPv6Address = &v
	}
	if r.Shutdown {
		v := true
		s.Shutdown = &v
	}
}

// newClient is the per-resource client factory for `eos:l3:*`.
func newClient(ctx context.Context, host, user, pass *string) (*eapi.Client, error) {
	cfg := config.FromContext(ctx)
	return cfg.EAPIClient(ctx, host, user, pass)
}
