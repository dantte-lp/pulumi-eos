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

// LACP / static channel-group modes.
const (
	ChannelGroupModeActive  = "active"
	ChannelGroupModePassive = "passive"
	ChannelGroupModeOn      = "on"
)

// Sentinel errors specific to Interface.
//
// The switchport-related sentinels (mode invalid, access-vs-trunk conflict)
// live in switchport.go and are reused here.
var (
	ErrInterfaceNameRequired  = errors.New("interface name is required")
	ErrInterfaceCgModeInvalid = errors.New("channelGroup.mode must be active, passive, or on")
)

// ChannelGroup binds the interface to a Port-Channel.
type ChannelGroup struct {
	// Id is the channel-group ID (1..2000).
	Id int `pulumi:"id"`
	// Mode selects LACP behaviour: `active`, `passive`, or static `on`.
	Mode string `pulumi:"mode"`
}

// Interface models a physical EOS Ethernet interface (or a Management
// interface) configured through eAPI.
type Interface struct{}

// InterfaceArgs is the input set.
type InterfaceArgs struct {
	// Name is the interface identifier (e.g. `Ethernet1`, `Ethernet1/1`,
	// `Management1`). EOS short forms (`Et1`) are NOT canonical and will
	// be normalised to the long form by the device — provider stores
	// what the user typed.
	Name string `pulumi:"name"`
	// Description sets `description …` on the interface.
	Description *string `pulumi:"description,optional"`
	// Mtu sets the L2/L3 MTU.
	Mtu *int `pulumi:"mtu,optional"`
	// Shutdown brings the interface administratively down.
	Shutdown *bool `pulumi:"shutdown,optional"`
	// SwitchportMode is `access`, `trunk`, or `routed` (`no switchport`).
	// When unset the interface keeps its current mode (or platform
	// default).
	SwitchportMode *string `pulumi:"switchportMode,optional"`
	// AccessVlan applies in `access` mode.
	AccessVlan *int `pulumi:"accessVlan,optional"`
	// TrunkAllowedVlans applies in `trunk` mode. Accept EOS list syntax
	// (e.g. `1-100,200-300`, `all`, `none`).
	TrunkAllowedVlans *string `pulumi:"trunkAllowedVlans,optional"`
	// TrunkNativeVlan applies in `trunk` mode. Default 1 on the device.
	TrunkNativeVlan *int `pulumi:"trunkNativeVlan,optional"`
	// ChannelGroup binds the interface to a Port-Channel via LACP.
	ChannelGroup *ChannelGroup `pulumi:"channelGroup,optional"`

	// Host overrides the provider-level eosUrl host for this resource.
	Host *string `pulumi:"host,optional"`
	// Username overrides the provider-level eosUsername for this resource.
	Username *string `pulumi:"username,optional"`
	// Password overrides the provider-level eosPassword for this resource.
	Password *string `provider:"secret" pulumi:"password,optional"`
}

// InterfaceState mirrors Args.
type InterfaceState struct {
	InterfaceArgs
}

// Annotate documents the resource.
func (r *Interface) Annotate(a infer.Annotator) {
	a.Describe(&r, "An EOS Ethernet (or Management) interface configured through atomic configuration sessions over eAPI.")
}

// Annotate documents InterfaceArgs fields.
func (a *InterfaceArgs) Annotate(an infer.Annotator) {
	an.Describe(&a.Name, "Interface identifier (e.g. `Ethernet1`, `Ethernet1/1`, `Management1`).")
	an.Describe(&a.Description, "Interface description.")
	an.Describe(&a.Mtu, "L2/L3 MTU in bytes.")
	an.Describe(&a.Shutdown, "When true, the interface is administratively shut down.")
	an.Describe(&a.SwitchportMode, "`access`, `trunk`, or `routed` (`no switchport`).")
	an.Describe(&a.AccessVlan, "Access VLAN (1..4094). Only valid when switchportMode is `access`.")
	an.Describe(&a.TrunkAllowedVlans, "Trunk allowed-VLAN list (e.g. `1-100,200-300`, `all`, `none`). Only valid when switchportMode is `trunk`.")
	an.Describe(&a.TrunkNativeVlan, "Trunk native VLAN (1..4094). Default 1 on the device.")
	an.Describe(&a.ChannelGroup, "LACP / static channel-group binding. Mutually exclusive with manual port-channel join.")
	an.Describe(&a.Host, "Optional management hostname override.")
	an.Describe(&a.Username, "Optional AAA username override.")
	an.Describe(&a.Password, "Optional AAA password override.")
}

// Annotate is a no-op; State has no extra fields.
func (s *InterfaceState) Annotate(_ infer.Annotator) {}

// Annotate documents ChannelGroup fields.
func (c *ChannelGroup) Annotate(a infer.Annotator) {
	a.Describe(&c.Id, "Channel-group / Port-Channel ID (1..2000).")
	a.Describe(&c.Mode, "LACP mode: `active`, `passive`, or static `on`.")
}

// Create configures the interface.
func (*Interface) Create(ctx context.Context, req infer.CreateRequest[InterfaceArgs]) (infer.CreateResponse[InterfaceState], error) {
	if err := validateInterface(req.Inputs); err != nil {
		return infer.CreateResponse[InterfaceState]{}, err
	}
	state := InterfaceState{InterfaceArgs: req.Inputs}
	if req.DryRun {
		return infer.CreateResponse[InterfaceState]{ID: interfaceID(req.Inputs.Name), Output: state}, nil
	}
	if err := applyInterface(ctx, req.Inputs, false); err != nil {
		return infer.CreateResponse[InterfaceState]{}, fmt.Errorf("create interface %s: %w", req.Inputs.Name, err)
	}
	return infer.CreateResponse[InterfaceState]{ID: interfaceID(req.Inputs.Name), Output: state}, nil
}

// Read refreshes interface state from the device.
func (*Interface) Read(ctx context.Context, req infer.ReadRequest[InterfaceArgs, InterfaceState]) (infer.ReadResponse[InterfaceArgs, InterfaceState], error) {
	cli, err := newClient(ctx, req.Inputs.Host, req.Inputs.Username, req.Inputs.Password)
	if err != nil {
		return infer.ReadResponse[InterfaceArgs, InterfaceState]{}, err
	}
	row, found, err := readInterface(ctx, cli, req.Inputs.Name)
	if err != nil {
		return infer.ReadResponse[InterfaceArgs, InterfaceState]{}, err
	}
	if !found {
		return infer.ReadResponse[InterfaceArgs, InterfaceState]{}, nil
	}
	state := InterfaceState{InterfaceArgs: req.Inputs}
	row.fillState(&state)
	return infer.ReadResponse[InterfaceArgs, InterfaceState]{
		ID:     interfaceID(req.Inputs.Name),
		Inputs: req.Inputs,
		State:  state,
	}, nil
}

// Update re-applies the interface configuration.
//
// Physical interfaces are never deleted by Pulumi; Update with the
// `default interface <name>` reset is left to S6+ when we have a clear
// rollback model.
func (*Interface) Update(ctx context.Context, req infer.UpdateRequest[InterfaceArgs, InterfaceState]) (infer.UpdateResponse[InterfaceState], error) {
	if err := validateInterface(req.Inputs); err != nil {
		return infer.UpdateResponse[InterfaceState]{}, err
	}
	state := InterfaceState{InterfaceArgs: req.Inputs}
	if req.DryRun {
		return infer.UpdateResponse[InterfaceState]{Output: state}, nil
	}
	if err := applyInterface(ctx, req.Inputs, false); err != nil {
		return infer.UpdateResponse[InterfaceState]{}, fmt.Errorf("update interface %s: %w", req.Inputs.Name, err)
	}
	return infer.UpdateResponse[InterfaceState]{Output: state}, nil
}

// Delete resets the interface to defaults via `default interface <name>`.
//
// Physical interfaces persist in hardware; we cannot truly remove them, so
// Delete returns the interface to its at-boot defaults (clears description,
// switchport, channel-group, etc.).
func (*Interface) Delete(ctx context.Context, req infer.DeleteRequest[InterfaceState]) (infer.DeleteResponse, error) {
	if err := applyInterface(ctx, req.State.InterfaceArgs, true); err != nil {
		return infer.DeleteResponse{}, fmt.Errorf("delete (default) interface %s: %w", req.State.Name, err)
	}
	return infer.DeleteResponse{}, nil
}

// --- helpers ---------------------------------------------------------------

func validateInterface(args InterfaceArgs) error {
	if strings.TrimSpace(args.Name) == "" {
		return ErrInterfaceNameRequired
	}
	sp := args.switchport()
	if err := validateSwitchport(sp); err != nil {
		return err
	}
	if args.ChannelGroup != nil {
		switch args.ChannelGroup.Mode {
		case ChannelGroupModeActive, ChannelGroupModePassive, ChannelGroupModeOn:
		default:
			return fmt.Errorf("%w (got %q)", ErrInterfaceCgModeInvalid, args.ChannelGroup.Mode)
		}
		if args.ChannelGroup.Id < 1 || args.ChannelGroup.Id > 2000 {
			return fmt.Errorf("channelGroup.id must be 1..2000 (got %d)", args.ChannelGroup.Id) //nolint:err113 // bounds-check, not sentinel.
		}
	}
	return nil
}

// switchport returns the SwitchportFields view of the args.
func (a *InterfaceArgs) switchport() SwitchportFields {
	return SwitchportFields{
		Mode:              a.SwitchportMode,
		AccessVlan:        a.AccessVlan,
		TrunkAllowedVlans: a.TrunkAllowedVlans,
		TrunkNativeVlan:   a.TrunkNativeVlan,
	}
}

func interfaceID(name string) string { return "interface/" + name }

func applyInterface(ctx context.Context, args InterfaceArgs, reset bool) error {
	cli, err := newClient(ctx, args.Host, args.Username, args.Password)
	if err != nil {
		return err
	}
	cfg := config.FromContext(ctx)
	sessName := cfg.SessionPrefix() + "iface-" + sanitizeForSession(args.Name)

	sess, err := cli.OpenSession(ctx, sessName)
	if err != nil {
		return err
	}
	cmds := buildInterfaceCmds(args, reset)
	if stageErr := sess.Stage(ctx, cmds); stageErr != nil {
		if abortErr := sess.Abort(ctx); abortErr != nil {
			return errors.Join(stageErr, abortErr)
		}
		return stageErr
	}
	return sess.Commit(ctx)
}

func buildInterfaceCmds(args InterfaceArgs, reset bool) []string {
	if reset {
		return []string{"default interface " + args.Name}
	}
	cmds := []string{"interface " + args.Name}
	if args.Description != nil && *args.Description != "" {
		cmds = append(cmds, "description "+*args.Description)
	}
	if args.Mtu != nil && *args.Mtu > 0 {
		cmds = append(cmds, "mtu "+strconv.Itoa(*args.Mtu))
	}
	sp := args.switchport()
	cmds = append(cmds, buildSwitchportCmds(sp)...)
	if args.ChannelGroup != nil {
		cmds = append(cmds,
			"channel-group "+strconv.Itoa(args.ChannelGroup.Id)+" mode "+args.ChannelGroup.Mode)
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

// sanitizeForSession turns `Ethernet1/1` into `Ethernet1-1` for use inside
// EOS configuration session names (which forbid `/`).
func sanitizeForSession(name string) string {
	return strings.NewReplacer("/", "-", " ", "-", ":", "-").Replace(name)
}

// interfaceRow is the parsed live state.
type interfaceRow struct {
	Description      string
	Mtu              int
	Shutdown         bool
	Switchport       switchportRow
	ChannelGroupID   int
	ChannelGroupMode string
}

// readInterface returns the live interface configuration or (false, nil)
// when the interface is absent.
func readInterface(ctx context.Context, cli *eapi.Client, name string) (interfaceRow, bool, error) {
	resp, err := cli.RunCmds(ctx,
		[]string{"show running-config interfaces " + name},
		"text")
	if err != nil {
		return interfaceRow{}, false, err
	}
	if len(resp) == 0 {
		return interfaceRow{}, false, nil
	}
	out, _ := resp[0]["output"].(string)
	if out == "" {
		return interfaceRow{}, false, nil
	}
	return parseInterfaceConfig(out, name)
}

// parseInterfaceConfig is exposed for unit tests.
func parseInterfaceConfig(out, name string) (interfaceRow, bool, error) {
	header := "interface " + name
	if !strings.Contains(out, header) {
		return interfaceRow{}, false, nil
	}
	row := interfaceRow{}
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
		case strings.HasPrefix(line, "channel-group "):
			parseChannelGroup(line, &row)
		}
	}
	return row, true, nil
}

func parseChannelGroup(line string, row *interfaceRow) {
	// `channel-group <id> mode <m>`
	parts := strings.Fields(line)
	if len(parts) != 4 || parts[2] != "mode" {
		return
	}
	if v, err := strconv.Atoi(parts[1]); err == nil {
		row.ChannelGroupID = v
	}
	row.ChannelGroupMode = parts[3]
}

func (r interfaceRow) fillState(s *InterfaceState) {
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
	if r.ChannelGroupID > 0 {
		s.ChannelGroup = &ChannelGroup{Id: r.ChannelGroupID, Mode: r.ChannelGroupMode}
	}
}
