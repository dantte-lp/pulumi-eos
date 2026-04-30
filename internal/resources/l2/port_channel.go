package l2

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/pulumi/pulumi-go-provider/infer"

	"github.com/dantte-lp/pulumi-eos/internal/client/eapi"
	"github.com/dantte-lp/pulumi-eos/internal/config"
)

// LACP fallback modes accepted by `eos:l2:PortChannel`.
//
// Source: EOS User Manual §11.2.5.18 `port-channel lacp fallback`.
const (
	LacpFallbackStatic     = "static"
	LacpFallbackIndividual = "individual"
)

// DefaultLacpFallbackTimeout is the EOS default for `port-channel lacp
// fallback timeout` when the timer has not been overridden, in seconds.
const DefaultLacpFallbackTimeout = 90

// PortChannelIdMin / Max bound the channel-group (and matching Port-Channel)
// identifier per EOS User Manual §11.2.5 `channel-group`.
const (
	PortChannelIdMin = 1
	PortChannelIdMax = 2000
)

// Sentinel errors specific to PortChannel.
var (
	ErrPortChannelIdRange     = errors.New("portChannel id must be 1..2000")
	ErrLacpFallbackInvalid    = errors.New("lacpFallback must be static or individual")
	ErrLacpFallbackTimeoutBad = errors.New("lacpFallbackTimeout must be greater than 0")
)

// PortChannel models an EOS logical Port-Channel interface. Member ports
// join the channel from the physical side via `eos:l2:Interface`'s
// channelGroup field.
type PortChannel struct{}

// PortChannelArgs is the input set.
type PortChannelArgs struct {
	// Id is the Port-Channel identifier (1..2000).
	Id int `pulumi:"id"`
	// Description sets the interface description.
	Description *string `pulumi:"description,optional"`
	// Mtu sets the L2/L3 MTU on the port-channel.
	Mtu *int `pulumi:"mtu,optional"`
	// Shutdown brings the port-channel administratively down.
	Shutdown *bool `pulumi:"shutdown,optional"`
	// SwitchportMode is `access`, `trunk`, or `routed` (`no switchport`).
	SwitchportMode *string `pulumi:"switchportMode,optional"`
	// AccessVlan applies in `access` mode.
	AccessVlan *int `pulumi:"accessVlan,optional"`
	// TrunkAllowedVlans applies in `trunk` mode.
	TrunkAllowedVlans *string `pulumi:"trunkAllowedVlans,optional"`
	// TrunkNativeVlan applies in `trunk` mode.
	TrunkNativeVlan *int `pulumi:"trunkNativeVlan,optional"`
	// LacpFallback selects the fallback mode (`static` or `individual`).
	// Disabled when unset.
	LacpFallback *string `pulumi:"lacpFallback,optional"`
	// LacpFallbackTimeout overrides the default 90 s fallback timer.
	LacpFallbackTimeout *int `pulumi:"lacpFallbackTimeout,optional"`

	// Host overrides the provider-level eosUrl host for this resource.
	Host *string `pulumi:"host,optional"`
	// Username overrides the provider-level eosUsername for this resource.
	Username *string `pulumi:"username,optional"`
	// Password overrides the provider-level eosPassword for this resource.
	Password *string `provider:"secret" pulumi:"password,optional"`
}

// PortChannelState mirrors Args.
type PortChannelState struct {
	PortChannelArgs
}

// Annotate documents the resource.
func (r *PortChannel) Annotate(a infer.Annotator) {
	a.Describe(&r, "An EOS Port-Channel logical interface configured through atomic configuration sessions over eAPI.")
}

// Annotate documents PortChannelArgs fields.
func (a *PortChannelArgs) Annotate(an infer.Annotator) {
	an.Describe(&a.Id, "Port-Channel ID (1..2000).")
	an.Describe(&a.Description, "Interface description.")
	an.Describe(&a.Mtu, "L2/L3 MTU in bytes.")
	an.Describe(&a.Shutdown, "When true, the port-channel is administratively shut down.")
	an.Describe(&a.SwitchportMode, "`access`, `trunk`, or `routed` (`no switchport`).")
	an.Describe(&a.AccessVlan, "Access VLAN (1..4094). Only valid when switchportMode is `access`.")
	an.Describe(&a.TrunkAllowedVlans, "Trunk allowed-VLAN list. Only valid when switchportMode is `trunk`.")
	an.Describe(&a.TrunkNativeVlan, "Trunk native VLAN (1..4094). Default 1.")
	an.Describe(&a.LacpFallback, "LACP fallback mode (`static` or `individual`). When unset, fallback is disabled.")
	an.Describe(&a.LacpFallbackTimeout, "LACP fallback timeout in seconds. Defaults to 90 s on the device.")
	an.Describe(&a.Host, "Optional management hostname override.")
	an.Describe(&a.Username, "Optional AAA username override.")
	an.Describe(&a.Password, "Optional AAA password override.")
}

// Annotate is a no-op; State has no extra fields.
func (s *PortChannelState) Annotate(_ infer.Annotator) {}

// Create configures the Port-Channel.
func (*PortChannel) Create(ctx context.Context, req infer.CreateRequest[PortChannelArgs]) (infer.CreateResponse[PortChannelState], error) {
	if err := validatePortChannel(req.Inputs); err != nil {
		return infer.CreateResponse[PortChannelState]{}, err
	}
	state := PortChannelState{PortChannelArgs: req.Inputs}
	if req.DryRun {
		return infer.CreateResponse[PortChannelState]{ID: portChannelID(req.Inputs.Id), Output: state}, nil
	}
	if err := applyPortChannel(ctx, req.Inputs, false); err != nil {
		return infer.CreateResponse[PortChannelState]{}, fmt.Errorf("create port-channel %d: %w", req.Inputs.Id, err)
	}
	return infer.CreateResponse[PortChannelState]{ID: portChannelID(req.Inputs.Id), Output: state}, nil
}

// Read refreshes Port-Channel state from the device.
func (*PortChannel) Read(ctx context.Context, req infer.ReadRequest[PortChannelArgs, PortChannelState]) (infer.ReadResponse[PortChannelArgs, PortChannelState], error) {
	cli, err := newClient(ctx, req.Inputs.Host, req.Inputs.Username, req.Inputs.Password)
	if err != nil {
		return infer.ReadResponse[PortChannelArgs, PortChannelState]{}, err
	}
	row, found, err := readPortChannel(ctx, cli, req.Inputs.Id)
	if err != nil {
		return infer.ReadResponse[PortChannelArgs, PortChannelState]{}, err
	}
	if !found {
		return infer.ReadResponse[PortChannelArgs, PortChannelState]{}, nil
	}
	state := PortChannelState{PortChannelArgs: req.Inputs}
	row.fillState(&state)
	return infer.ReadResponse[PortChannelArgs, PortChannelState]{
		ID:     portChannelID(req.Inputs.Id),
		Inputs: req.Inputs,
		State:  state,
	}, nil
}

// Update re-applies the Port-Channel configuration.
func (*PortChannel) Update(ctx context.Context, req infer.UpdateRequest[PortChannelArgs, PortChannelState]) (infer.UpdateResponse[PortChannelState], error) {
	if err := validatePortChannel(req.Inputs); err != nil {
		return infer.UpdateResponse[PortChannelState]{}, err
	}
	state := PortChannelState{PortChannelArgs: req.Inputs}
	if req.DryRun {
		return infer.UpdateResponse[PortChannelState]{Output: state}, nil
	}
	if err := applyPortChannel(ctx, req.Inputs, false); err != nil {
		return infer.UpdateResponse[PortChannelState]{}, fmt.Errorf("update port-channel %d: %w", req.Inputs.Id, err)
	}
	return infer.UpdateResponse[PortChannelState]{Output: state}, nil
}

// Delete removes the Port-Channel via `no interface Port-Channel<id>`.
//
// Member ports retain their `channel-group` lines until cleared at the
// member side; that is by design and matches the EOS CLI semantics.
func (*PortChannel) Delete(ctx context.Context, req infer.DeleteRequest[PortChannelState]) (infer.DeleteResponse, error) {
	if err := applyPortChannel(ctx, req.State.PortChannelArgs, true); err != nil {
		return infer.DeleteResponse{}, fmt.Errorf("delete port-channel %d: %w", req.State.Id, err)
	}
	return infer.DeleteResponse{}, nil
}

// validatePortChannel enforces id range, switchport rules, and LACP-fallback
// constraints.
func validatePortChannel(args PortChannelArgs) error {
	if args.Id < PortChannelIdMin || args.Id > PortChannelIdMax {
		return fmt.Errorf("%w (got %d)", ErrPortChannelIdRange, args.Id)
	}
	sp := args.switchport()
	if err := validateSwitchport(sp); err != nil {
		return err
	}
	if args.LacpFallback != nil {
		switch *args.LacpFallback {
		case "", LacpFallbackStatic, LacpFallbackIndividual:
		default:
			return fmt.Errorf("%w (got %q)", ErrLacpFallbackInvalid, *args.LacpFallback)
		}
	}
	if args.LacpFallbackTimeout != nil && *args.LacpFallbackTimeout <= 0 {
		return fmt.Errorf("%w (got %d)", ErrLacpFallbackTimeoutBad, *args.LacpFallbackTimeout)
	}
	return nil
}

// switchport returns the SwitchportFields view of the args.
func (a *PortChannelArgs) switchport() SwitchportFields {
	return SwitchportFields{
		Mode:              a.SwitchportMode,
		AccessVlan:        a.AccessVlan,
		TrunkAllowedVlans: a.TrunkAllowedVlans,
		TrunkNativeVlan:   a.TrunkNativeVlan,
	}
}

func portChannelID(id int) string   { return "port-channel/" + strconv.Itoa(id) }
func portChannelName(id int) string { return "Port-Channel" + strconv.Itoa(id) }

func applyPortChannel(ctx context.Context, args PortChannelArgs, remove bool) error {
	cli, err := newClient(ctx, args.Host, args.Username, args.Password)
	if err != nil {
		return err
	}
	cfg := config.FromContext(ctx)
	sessName := cfg.SessionPrefix() + "po-" + strconv.Itoa(args.Id)

	sess, err := cli.OpenSession(ctx, sessName)
	if err != nil {
		return err
	}
	cmds := buildPortChannelCmds(args, remove)
	if stageErr := sess.Stage(ctx, cmds); stageErr != nil {
		if abortErr := sess.Abort(ctx); abortErr != nil {
			return errors.Join(stageErr, abortErr)
		}
		return stageErr
	}
	return sess.Commit(ctx)
}

// buildPortChannelCmds renders the ordered command list for the Port-Channel.
//
// Order matters: `interface Port-ChannelN` first, then descriptive
// attributes, then switchport, then LACP fallback, then admin state. This
// mirrors the order EOS prints in `show running-config`, keeping subsequent
// idempotent re-applies free of phantom diffs.
func buildPortChannelCmds(args PortChannelArgs, remove bool) []string {
	if remove {
		return []string{"no interface " + portChannelName(args.Id)}
	}
	cmds := []string{"interface " + portChannelName(args.Id)}
	if args.Description != nil && *args.Description != "" {
		cmds = append(cmds, "description "+*args.Description)
	}
	if args.Mtu != nil && *args.Mtu > 0 {
		cmds = append(cmds, "mtu "+strconv.Itoa(*args.Mtu))
	}
	sp := args.switchport()
	cmds = append(cmds, buildSwitchportCmds(sp)...)
	if args.LacpFallback != nil && *args.LacpFallback != "" {
		cmds = append(cmds, "port-channel lacp fallback "+*args.LacpFallback)
	}
	if args.LacpFallbackTimeout != nil && *args.LacpFallbackTimeout > 0 {
		cmds = append(cmds, "port-channel lacp fallback timeout "+strconv.Itoa(*args.LacpFallbackTimeout))
	}
	if args.Shutdown != nil {
		if *args.Shutdown {
			cmds = append(cmds, "shutdown")
		} else {
			cmds = append(cmds, "no shutdown")
		}
	}
	return cmds
}

// portChannelRow holds the parsed live state.
type portChannelRow struct {
	Description         string
	Mtu                 int
	Shutdown            bool
	Switchport          switchportRow
	LacpFallback        string
	LacpFallbackTimeout int
}

// readPortChannel returns the live Port-Channel state, or (false, nil)
// when the interface is absent.
func readPortChannel(ctx context.Context, cli *eapi.Client, id int) (portChannelRow, bool, error) {
	resp, err := cli.RunCmds(ctx,
		[]string{"show running-config interfaces " + portChannelName(id)},
		"text")
	if err != nil {
		return portChannelRow{}, false, err
	}
	if len(resp) == 0 {
		return portChannelRow{}, false, nil
	}
	out, _ := resp[0]["output"].(string)
	if out == "" {
		return portChannelRow{}, false, nil
	}
	return parsePortChannelConfig(out, id)
}

// parsePortChannelConfig is exposed for unit tests.
func parsePortChannelConfig(out string, id int) (portChannelRow, bool, error) {
	header := "interface " + portChannelName(id)
	if !strings.Contains(out, header) {
		return portChannelRow{}, false, nil
	}
	row := portChannelRow{}
	for raw := range strings.SplitSeq(out, "\n") {
		line := strings.TrimSpace(raw)
		if parseSwitchportLine(line, &row.Switchport) {
			continue
		}
		switch {
		case strings.HasPrefix(line, "description "):
			row.Description = strings.TrimPrefix(line, "description ")
		case strings.HasPrefix(line, "mtu "):
			if v, err := strconv.Atoi(strings.TrimPrefix(line, "mtu ")); err == nil {
				row.Mtu = v
			}
		case line == "shutdown":
			row.Shutdown = true
		case strings.HasPrefix(line, "port-channel lacp fallback timeout "):
			if v, err := strconv.Atoi(strings.TrimPrefix(line, "port-channel lacp fallback timeout ")); err == nil {
				row.LacpFallbackTimeout = v
			}
		case strings.HasPrefix(line, "port-channel lacp fallback "):
			row.LacpFallback = strings.TrimPrefix(line, "port-channel lacp fallback ")
		}
	}
	return row, true, nil
}

func (r portChannelRow) fillState(s *PortChannelState) {
	if r.Description != "" {
		v := r.Description
		s.Description = &v
	}
	if r.Mtu > 0 {
		v := r.Mtu
		s.Mtu = &v
	}
	if r.Shutdown {
		v := true
		s.Shutdown = &v
	}
	sp := SwitchportFields{}
	fillSwitchport(r.Switchport, &sp)
	s.SwitchportMode = sp.Mode
	s.AccessVlan = sp.AccessVlan
	s.TrunkAllowedVlans = sp.TrunkAllowedVlans
	s.TrunkNativeVlan = sp.TrunkNativeVlan
	if r.LacpFallback != "" {
		v := r.LacpFallback
		s.LacpFallback = &v
	}
	if r.LacpFallbackTimeout > 0 {
		v := r.LacpFallbackTimeout
		s.LacpFallbackTimeout = &v
	}
}
