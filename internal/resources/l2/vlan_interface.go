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

// Sentinel errors specific to VlanInterface.
var (
	ErrVlanInterfaceVlanIDMissing = errors.New("vlanInterface vlanId is required")
	ErrVlanInterfaceAddrConflict  = errors.New("vlanInterface ipAddress and ipAddressVirtual are mutually exclusive")
)

// VlanInterface models a Switched Virtual Interface (SVI) — `interface VlanN`
// in EOS. Supports both regular per-device IPs and anycast (`ip address
// virtual ...`) deployments.
type VlanInterface struct{}

// VlanInterfaceArgs is the input set.
type VlanInterfaceArgs struct {
	// VlanId binds the SVI to a VLAN (1..4094).
	VlanId int `pulumi:"vlanId"`
	// Vrf places the SVI in a non-default VRF. Optional.
	Vrf *string `pulumi:"vrf,optional"`
	// IpAddress is a per-device IP (e.g. `10.0.0.1/24`). Mutually exclusive
	// with `ipAddressVirtual`.
	IpAddress *string `pulumi:"ipAddress,optional"`
	// IpAddressVirtual is the anycast IP (`ip address virtual ...`).
	// Mutually exclusive with `ipAddress`.
	IpAddressVirtual *string `pulumi:"ipAddressVirtual,optional"`
	// Mtu sets the IP MTU on the SVI. Optional.
	Mtu *int `pulumi:"mtu,optional"`
	// Description sets the interface description.
	Description *string `pulumi:"description,optional"`
	// NoAutostate forces the SVI up regardless of underlying VLAN member
	// port state.
	NoAutostate *bool `pulumi:"noAutostate,optional"`
	// Shutdown administratively shuts the interface down.
	Shutdown *bool `pulumi:"shutdown,optional"`

	// Host overrides the provider-level eosUrl host for this resource.
	Host *string `pulumi:"host,optional"`
	// Username overrides the provider-level eosUsername for this resource.
	Username *string `pulumi:"username,optional"`
	// Password overrides the provider-level eosPassword for this resource.
	Password *string `provider:"secret" pulumi:"password,optional"`
}

// VlanInterfaceState mirrors Args.
type VlanInterfaceState struct {
	VlanInterfaceArgs
}

// Annotate documents the resource.
func (r *VlanInterface) Annotate(a infer.Annotator) {
	a.Describe(&r, "An EOS Switched Virtual Interface (SVI). Created and updated through atomic configuration sessions over eAPI.")
}

// Annotate documents VlanInterfaceArgs fields.
func (a *VlanInterfaceArgs) Annotate(an infer.Annotator) {
	an.Describe(&a.VlanId, "VLAN ID this SVI is bound to (1..4094).")
	an.Describe(&a.Vrf, "Optional VRF name. SVI lives in the default VRF when unset.")
	an.Describe(&a.IpAddress, "Per-device IP address (CIDR). Mutually exclusive with ipAddressVirtual.")
	an.Describe(&a.IpAddressVirtual, "Anycast IP address (`ip address virtual`). Mutually exclusive with ipAddress.")
	an.Describe(&a.Mtu, "IP MTU on the SVI.")
	an.Describe(&a.Description, "Interface description.")
	an.Describe(&a.NoAutostate, "When true, the SVI stays up regardless of member-port state (`no autostate`).")
	an.Describe(&a.Shutdown, "When true, the SVI is administratively shut down.")
	an.Describe(&a.Host, "Optional management hostname override.")
	an.Describe(&a.Username, "Optional AAA username override.")
	an.Describe(&a.Password, "Optional AAA password override.")
}

// Annotate is a no-op; State has no extra fields.
func (s *VlanInterfaceState) Annotate(_ infer.Annotator) {}

// Create configures the SVI.
func (*VlanInterface) Create(ctx context.Context, req infer.CreateRequest[VlanInterfaceArgs]) (infer.CreateResponse[VlanInterfaceState], error) {
	if err := validateVlanInterface(req.Inputs); err != nil {
		return infer.CreateResponse[VlanInterfaceState]{}, err
	}
	state := VlanInterfaceState{VlanInterfaceArgs: req.Inputs}
	if req.DryRun {
		return infer.CreateResponse[VlanInterfaceState]{ID: vlanInterfaceID(req.Inputs.VlanId), Output: state}, nil
	}
	if err := applyVlanInterface(ctx, req.Inputs, false); err != nil {
		return infer.CreateResponse[VlanInterfaceState]{}, fmt.Errorf("create vlan-interface %d: %w", req.Inputs.VlanId, err)
	}
	return infer.CreateResponse[VlanInterfaceState]{ID: vlanInterfaceID(req.Inputs.VlanId), Output: state}, nil
}

// Read refreshes SVI state from the device.
func (*VlanInterface) Read(ctx context.Context, req infer.ReadRequest[VlanInterfaceArgs, VlanInterfaceState]) (infer.ReadResponse[VlanInterfaceArgs, VlanInterfaceState], error) {
	cli, err := newClient(ctx, req.Inputs.Host, req.Inputs.Username, req.Inputs.Password)
	if err != nil {
		return infer.ReadResponse[VlanInterfaceArgs, VlanInterfaceState]{}, err
	}
	current, found, err := readVlanInterface(ctx, cli, req.Inputs.VlanId)
	if err != nil {
		return infer.ReadResponse[VlanInterfaceArgs, VlanInterfaceState]{}, err
	}
	if !found {
		return infer.ReadResponse[VlanInterfaceArgs, VlanInterfaceState]{}, nil
	}
	state := VlanInterfaceState{VlanInterfaceArgs: req.Inputs}
	current.fillState(&state)
	return infer.ReadResponse[VlanInterfaceArgs, VlanInterfaceState]{
		ID:     vlanInterfaceID(req.Inputs.VlanId),
		Inputs: req.Inputs,
		State:  state,
	}, nil
}

// Update re-applies the SVI configuration.
func (*VlanInterface) Update(ctx context.Context, req infer.UpdateRequest[VlanInterfaceArgs, VlanInterfaceState]) (infer.UpdateResponse[VlanInterfaceState], error) {
	if err := validateVlanInterface(req.Inputs); err != nil {
		return infer.UpdateResponse[VlanInterfaceState]{}, err
	}
	state := VlanInterfaceState{VlanInterfaceArgs: req.Inputs}
	if req.DryRun {
		return infer.UpdateResponse[VlanInterfaceState]{Output: state}, nil
	}
	if err := applyVlanInterface(ctx, req.Inputs, false); err != nil {
		return infer.UpdateResponse[VlanInterfaceState]{}, fmt.Errorf("update vlan-interface %d: %w", req.Inputs.VlanId, err)
	}
	return infer.UpdateResponse[VlanInterfaceState]{Output: state}, nil
}

// Delete removes the SVI.
func (*VlanInterface) Delete(ctx context.Context, req infer.DeleteRequest[VlanInterfaceState]) (infer.DeleteResponse, error) {
	if err := applyVlanInterface(ctx, req.State.VlanInterfaceArgs, true); err != nil {
		return infer.DeleteResponse{}, fmt.Errorf("delete vlan-interface %d: %w", req.State.VlanId, err)
	}
	return infer.DeleteResponse{}, nil
}

// --- helpers ---------------------------------------------------------------

func validateVlanInterface(args VlanInterfaceArgs) error {
	if err := validateVlanID(args.VlanId); err != nil {
		return err
	}
	if args.IpAddress != nil && *args.IpAddress != "" &&
		args.IpAddressVirtual != nil && *args.IpAddressVirtual != "" {
		return ErrVlanInterfaceAddrConflict
	}
	return nil
}

func vlanInterfaceID(id int) string { return "vlan-interface/" + strconv.Itoa(id) }

func applyVlanInterface(ctx context.Context, args VlanInterfaceArgs, remove bool) error {
	cli, err := newClient(ctx, args.Host, args.Username, args.Password)
	if err != nil {
		return err
	}
	cfg := config.FromContext(ctx)
	sessName := cfg.SessionPrefix() + "svi-" + strconv.Itoa(args.VlanId)

	sess, err := cli.OpenSession(ctx, sessName)
	if err != nil {
		return err
	}
	cmds := buildVlanInterfaceCmds(args, remove)
	if stageErr := sess.Stage(ctx, cmds); stageErr != nil {
		if abortErr := sess.Abort(ctx); abortErr != nil {
			return errors.Join(stageErr, abortErr)
		}
		return stageErr
	}
	return sess.Commit(ctx)
}

func buildVlanInterfaceCmds(args VlanInterfaceArgs, remove bool) []string {
	if remove {
		return []string{"no interface Vlan" + strconv.Itoa(args.VlanId)}
	}
	cmds := []string{"interface Vlan" + strconv.Itoa(args.VlanId)}
	if args.Description != nil && *args.Description != "" {
		cmds = append(cmds, "description "+*args.Description)
	}
	if args.Vrf != nil && *args.Vrf != "" {
		cmds = append(cmds, "vrf "+*args.Vrf)
	}
	if args.IpAddress != nil && *args.IpAddress != "" {
		cmds = append(cmds, "ip address "+*args.IpAddress)
	}
	if args.IpAddressVirtual != nil && *args.IpAddressVirtual != "" {
		cmds = append(cmds, "ip address virtual "+*args.IpAddressVirtual)
	}
	if args.Mtu != nil && *args.Mtu > 0 {
		cmds = append(cmds, "mtu "+strconv.Itoa(*args.Mtu))
	}
	if args.NoAutostate != nil && *args.NoAutostate {
		cmds = append(cmds, "no autostate")
	}
	if args.Shutdown != nil && *args.Shutdown {
		cmds = append(cmds, "shutdown")
	}
	return cmds
}

// vlanInterfaceRow is the parsed live state we care about.
type vlanInterfaceRow struct {
	Description string
	Vrf         string
	Mtu         int
	Address     string // primary IP "a.b.c.d/m"
	Virtual     string // anycast "a.b.c.d/m"
	NoAutostate bool
	Shutdown    bool
}

// readVlanInterface returns the live SVI state, or (false, nil) when absent.
//
// Source of truth: `show running-config interfaces Vlan<id>`. The structured
// JSON returned by `show interfaces Vlan<id>` lacks SVI-specific fields like
// `ip address virtual` and `vrf`, so we parse the raw config block instead.
func readVlanInterface(ctx context.Context, cli *eapi.Client, id int) (vlanInterfaceRow, bool, error) {
	resp, err := cli.RunCmds(ctx,
		[]string{"show running-config interfaces Vlan" + strconv.Itoa(id)},
		"text")
	if err != nil {
		return vlanInterfaceRow{}, false, err
	}
	if len(resp) == 0 {
		return vlanInterfaceRow{}, false, nil
	}
	out, _ := resp[0]["output"].(string)
	if out == "" {
		return vlanInterfaceRow{}, false, nil
	}
	return parseVlanInterfaceConfig(out, id)
}

// parseVlanInterfaceConfig extracts the SVI fields from raw `show
// running-config interfaces Vlan<id>` output. Exposed for unit tests.
func parseVlanInterfaceConfig(out string, id int) (vlanInterfaceRow, bool, error) {
	header := "interface Vlan" + strconv.Itoa(id)
	if !strings.Contains(out, header) {
		return vlanInterfaceRow{}, false, nil
	}
	row := vlanInterfaceRow{}
	for raw := range strings.SplitSeq(out, "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(line, "description "):
			row.Description = strings.TrimPrefix(line, "description ")
		case strings.HasPrefix(line, "vrf "):
			row.Vrf = strings.TrimPrefix(line, "vrf ")
		case strings.HasPrefix(line, "ip address virtual "):
			row.Virtual = strings.TrimPrefix(line, "ip address virtual ")
		case strings.HasPrefix(line, "ip address "):
			row.Address = strings.TrimPrefix(line, "ip address ")
		case strings.HasPrefix(line, "mtu "):
			if v, err := strconv.Atoi(strings.TrimPrefix(line, "mtu ")); err == nil {
				row.Mtu = v
			}
		case line == "no autostate":
			row.NoAutostate = true
		case line == "shutdown":
			row.Shutdown = true
		}
	}
	return row, true, nil
}

func (r vlanInterfaceRow) fillState(s *VlanInterfaceState) {
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
		s.IpAddress = &v
	}
	if r.Virtual != "" {
		v := r.Virtual
		s.IpAddressVirtual = &v
	}
	if r.Mtu > 0 {
		v := r.Mtu
		s.Mtu = &v
	}
	if r.NoAutostate {
		v := true
		s.NoAutostate = &v
	}
	if r.Shutdown {
		v := true
		s.Shutdown = &v
	}
}
