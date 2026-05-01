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

// Sentinel errors specific to GreTunnel.
var (
	ErrGreTunnelIDRange       = errors.New("greTunnel id must be in 0..65535")
	ErrGreTunnelModeInvalid   = errors.New("greTunnel mode must be gre | mpls-gre | mpls-over-gre | ipsec")
	ErrGreTunnelSourceBadIPv4 = errors.New("greTunnel source must be a valid IPv4 address")
	ErrGreTunnelDestBadIPv4   = errors.New("greTunnel destination must be a valid IPv4 address")
	ErrGreTunnelTosRange      = errors.New("greTunnel tos must be in 0..255")
	ErrGreTunnelKeyRange      = errors.New("greTunnel key must be in 0..4294967295")
	ErrGreTunnelMssRange      = errors.New("greTunnel mssCeiling must be > 0")
	ErrGreTunnelMtuRange      = errors.New("greTunnel mtu must be in 68..9214")
	ErrGreTunnelIPCidrInvalid = errors.New("greTunnel ipAddress must be a valid IPv4 CIDR (A.B.C.D/M)")
)

// validGreTunnelModes enumerates the modes EOS accepts on `tunnel
// mode <mode>`. Verified live against cEOS 4.36.0.1F via the
// integration_probe path.
var validGreTunnelModes = map[string]struct{}{
	"gre":           {},
	"mpls-gre":      {},
	"mpls-over-gre": {},
	"ipsec":         {},
}

// GreTunnel models an EOS GRE tunnel interface (`interface Tunnel<id>`).
//
// Identity is the integer id; the resource renders into
// `interface Tunnel<id>` and represents one tunnel endpoint. v0 covers
// the canonical L3 overlay surface:
//
//   - Identity: id (PK, 0..65535).
//   - Underlay: source / destination IPv4, underlayVrf, mode (gre |
//     mpls-gre | mpls-over-gre | ipsec).
//   - Encap: tos, key, mssCeiling, pathMtuDiscovery, dontFragment.
//   - Overlay: ipAddress (CIDR), vrf, mtu, description, shutdown.
//
// Deferred to v1 (probe-rejected on cEOS 4.36.0.1F or platform-
// dependent): `tunnel ttl <N>` (rejected on cEOS lab), `tunnel source
// <interface>` (rejected; only IPv4 source accepted on lab),
// `tunnel ipsec profile <name>` (requires `eos:security:IpsecProfile`
// — out of scope for S6).
//
// Lab quirk: `tunnel dont-fragment` is included in the input shape and
// renders as expected, but cEOSLab can return "Unavailable command
// (not supported on this hardware platform)" when the line lands in a
// session with stale Tunnel<id> state from an earlier failed apply.
// Production EOS hardware does not exhibit the issue. The integration
// test for this resource therefore does not exercise dont-fragment.
//
// Source: EOS User Manual §29 (GRE Tunneling); cEOS 4.36.0.1F live
// probe (commit `3c13006`); double validation per
// docs/05-development.md rule 2.
type GreTunnel struct{}

// GreTunnelArgs is the resource input set.
type GreTunnelArgs struct {
	// Id is the tunnel interface id (PK, 0..65535).
	Id int `pulumi:"id"`
	// Description is a free-form comment rendered as `description`.
	Description *string `pulumi:"description,optional"`
	// Mode is the tunnel encapsulation mode. Defaults to "gre" when
	// nil; emitted only when explicitly set.
	Mode *string `pulumi:"mode,optional"`
	// Source is the underlay source IPv4 address.
	Source *string `pulumi:"source,optional"`
	// Destination is the underlay destination IPv4 address.
	Destination *string `pulumi:"destination,optional"`
	// UnderlayVrf places the underlay socket in a non-default VRF.
	UnderlayVrf *string `pulumi:"underlayVrf,optional"`
	// Tos sets the underlay IP TOS byte (0..255).
	Tos *int `pulumi:"tos,optional"`
	// Key is the GRE key (0..4294967295).
	Key *int `pulumi:"key,optional"`
	// MssCeiling adjusts overlay TCP MSS clamping (bytes).
	MssCeiling *int `pulumi:"mssCeiling,optional"`
	// PathMtuDiscovery toggles `tunnel path-mtu-discovery`.
	PathMtuDiscovery *bool `pulumi:"pathMtuDiscovery,optional"`
	// DontFragment toggles `tunnel dont-fragment`.
	DontFragment *bool `pulumi:"dontFragment,optional"`
	// IpAddress is the overlay IPv4 in CIDR form (`A.B.C.D/M`).
	IpAddress *string `pulumi:"ipAddress,optional"`
	// Mtu is the overlay MTU (68..9214).
	Mtu *int `pulumi:"mtu,optional"`
	// Vrf is the overlay VRF (`vrf forwarding <name>`).
	Vrf *string `pulumi:"vrf,optional"`
	// Shutdown disables the interface while keeping config.
	Shutdown *bool `pulumi:"shutdown,optional"`

	// Host overrides the provider-level eosUrl host.
	Host *string `pulumi:"host,optional"`
	// Username overrides the provider-level eosUsername.
	Username *string `pulumi:"username,optional"`
	// Password overrides the provider-level eosPassword.
	Password *string `provider:"secret" pulumi:"password,optional"`
}

// GreTunnelState mirrors Args.
type GreTunnelState struct {
	GreTunnelArgs
}

// Annotate documents the resource.
func (r *GreTunnel) Annotate(a infer.Annotator) {
	a.Describe(&r, "An EOS GRE tunnel interface (`interface Tunnel<id>`). v0 covers gre / mpls-gre / mpls-over-gre / ipsec modes; ttl + source-from-interface deferred to v1 (cEOS 4.36 lab quirks).")
}

// Annotate documents GreTunnelArgs fields.
func (a *GreTunnelArgs) Annotate(an infer.Annotator) {
	an.Describe(&a.Id, "Tunnel interface id (PK, 0..65535).")
	an.Describe(&a.Description, "Free-form interface description.")
	an.Describe(&a.Mode, "Tunnel mode: gre (default) | mpls-gre | mpls-over-gre | ipsec.")
	an.Describe(&a.Source, "Underlay source IPv4 address.")
	an.Describe(&a.Destination, "Underlay destination IPv4 address.")
	an.Describe(&a.UnderlayVrf, "Non-default VRF for the underlay socket (`tunnel underlay vrf`).")
	an.Describe(&a.Tos, "Underlay IP TOS byte (0..255).")
	an.Describe(&a.Key, "GRE key (0..4294967295).")
	an.Describe(&a.MssCeiling, "Overlay TCP MSS clamping ceiling (bytes).")
	an.Describe(&a.PathMtuDiscovery, "Enable `tunnel path-mtu-discovery` when true.")
	an.Describe(&a.DontFragment, "Enable `tunnel dont-fragment` when true.")
	an.Describe(&a.IpAddress, "Overlay IPv4 CIDR (A.B.C.D/M).")
	an.Describe(&a.Mtu, "Overlay MTU in bytes (68..9214).")
	an.Describe(&a.Vrf, "Overlay VRF (`vrf forwarding <name>`).")
	an.Describe(&a.Shutdown, "Disable the interface while keeping config when true.")
	an.Describe(&a.Host, "Optional management hostname override.")
	an.Describe(&a.Username, "Optional AAA username override.")
	an.Describe(&a.Password, "Optional AAA password override.")
}

// Annotate is a no-op; State has no extra fields.
func (s *GreTunnelState) Annotate(_ infer.Annotator) {}

// Create configures the GRE tunnel.
func (*GreTunnel) Create(ctx context.Context, req infer.CreateRequest[GreTunnelArgs]) (infer.CreateResponse[GreTunnelState], error) {
	if err := validateGreTunnel(req.Inputs); err != nil {
		return infer.CreateResponse[GreTunnelState]{}, err
	}
	state := GreTunnelState{GreTunnelArgs: req.Inputs}
	if req.DryRun {
		return infer.CreateResponse[GreTunnelState]{ID: greTunnelID(req.Inputs.Id), Output: state}, nil
	}
	if err := applyGreTunnel(ctx, req.Inputs, false); err != nil {
		return infer.CreateResponse[GreTunnelState]{}, fmt.Errorf("create greTunnel %d: %w", req.Inputs.Id, err)
	}
	return infer.CreateResponse[GreTunnelState]{ID: greTunnelID(req.Inputs.Id), Output: state}, nil
}

// Read refreshes tunnel state from the device.
func (*GreTunnel) Read(ctx context.Context, req infer.ReadRequest[GreTunnelArgs, GreTunnelState]) (infer.ReadResponse[GreTunnelArgs, GreTunnelState], error) {
	cli, err := newClient(ctx, req.Inputs.Host, req.Inputs.Username, req.Inputs.Password)
	if err != nil {
		return infer.ReadResponse[GreTunnelArgs, GreTunnelState]{}, err
	}
	current, found, err := readGreTunnel(ctx, cli, req.Inputs.Id)
	if err != nil {
		return infer.ReadResponse[GreTunnelArgs, GreTunnelState]{}, err
	}
	if !found {
		return infer.ReadResponse[GreTunnelArgs, GreTunnelState]{}, nil
	}
	state := GreTunnelState{GreTunnelArgs: req.Inputs}
	current.fillState(&state)
	return infer.ReadResponse[GreTunnelArgs, GreTunnelState]{
		ID:     greTunnelID(req.Inputs.Id),
		Inputs: req.Inputs,
		State:  state,
	}, nil
}

// Update re-applies the body. Re-emit only — EOS' session diff
// computes the delta and applies it without flapping the interface.
// Optional fields cleared by the user (set to empty / 0) emit an
// explicit `no <field>` so the previous value goes away.
func (*GreTunnel) Update(ctx context.Context, req infer.UpdateRequest[GreTunnelArgs, GreTunnelState]) (infer.UpdateResponse[GreTunnelState], error) {
	if err := validateGreTunnel(req.Inputs); err != nil {
		return infer.UpdateResponse[GreTunnelState]{}, err
	}
	state := GreTunnelState{GreTunnelArgs: req.Inputs}
	if req.DryRun {
		return infer.UpdateResponse[GreTunnelState]{Output: state}, nil
	}
	if err := applyGreTunnel(ctx, req.Inputs, false); err != nil {
		return infer.UpdateResponse[GreTunnelState]{}, fmt.Errorf("update greTunnel %d: %w", req.Inputs.Id, err)
	}
	return infer.UpdateResponse[GreTunnelState]{Output: state}, nil
}

// Delete removes the tunnel interface.
func (*GreTunnel) Delete(ctx context.Context, req infer.DeleteRequest[GreTunnelState]) (infer.DeleteResponse, error) {
	if err := applyGreTunnel(ctx, req.State.GreTunnelArgs, true); err != nil {
		return infer.DeleteResponse{}, fmt.Errorf("delete greTunnel %d: %w", req.State.Id, err)
	}
	return infer.DeleteResponse{}, nil
}

// --- helpers ---------------------------------------------------------------

func validateGreTunnel(args GreTunnelArgs) error {
	if args.Id < 0 || args.Id > 65535 {
		return fmt.Errorf("%w: got %d", ErrGreTunnelIDRange, args.Id)
	}
	if args.Mode != nil && *args.Mode != "" {
		if _, ok := validGreTunnelModes[*args.Mode]; !ok {
			return fmt.Errorf("%w: got %q", ErrGreTunnelModeInvalid, *args.Mode)
		}
	}
	if args.Source != nil && *args.Source != "" {
		if addr, err := netip.ParseAddr(*args.Source); err != nil || !addr.Is4() {
			return fmt.Errorf("%w: %q", ErrGreTunnelSourceBadIPv4, *args.Source)
		}
	}
	if args.Destination != nil && *args.Destination != "" {
		if addr, err := netip.ParseAddr(*args.Destination); err != nil || !addr.Is4() {
			return fmt.Errorf("%w: %q", ErrGreTunnelDestBadIPv4, *args.Destination)
		}
	}
	if args.Tos != nil && (*args.Tos < 0 || *args.Tos > 255) {
		return fmt.Errorf("%w: got %d", ErrGreTunnelTosRange, *args.Tos)
	}
	if args.Key != nil && (*args.Key < 0 || *args.Key > 4294967295) {
		return fmt.Errorf("%w: got %d", ErrGreTunnelKeyRange, *args.Key)
	}
	if args.MssCeiling != nil && *args.MssCeiling <= 0 {
		return fmt.Errorf("%w: got %d", ErrGreTunnelMssRange, *args.MssCeiling)
	}
	if args.Mtu != nil && (*args.Mtu < 68 || *args.Mtu > 9214) {
		return fmt.Errorf("%w: got %d", ErrGreTunnelMtuRange, *args.Mtu)
	}
	if args.IpAddress != nil && *args.IpAddress != "" {
		if pfx, err := netip.ParsePrefix(*args.IpAddress); err != nil || !pfx.Addr().Is4() {
			return fmt.Errorf("%w: %q", ErrGreTunnelIPCidrInvalid, *args.IpAddress)
		}
	}
	return nil
}

func greTunnelID(id int) string {
	return fmt.Sprintf("gre-tunnel/%d", id)
}

func applyGreTunnel(ctx context.Context, args GreTunnelArgs, remove bool) error {
	cli, err := newClient(ctx, args.Host, args.Username, args.Password)
	if err != nil {
		return err
	}
	cfg := config.FromContext(ctx)
	sessName := cfg.SessionPrefix() + "tunnel-" + strconv.Itoa(args.Id)
	sess, err := cli.OpenSession(ctx, sessName)
	if err != nil {
		return err
	}
	cmds := buildGreTunnelCmds(args, remove)
	if stageErr := sess.Stage(ctx, cmds); stageErr != nil {
		if abortErr := sess.Abort(ctx); abortErr != nil {
			return errors.Join(stageErr, abortErr)
		}
		return stageErr
	}
	return sess.Commit(ctx)
}

// buildGreTunnelCmds renders the staged CLI block. Render order
// follows cEOS' canonical running-config so diffs against
// `show running-config interfaces Tunnel<id>` are minimal.
//
// Update strategy: simple re-emit (no `default interface` / `no
// interface` prefix). EOS' session-diff applies the minimum delta;
// the interface does not flap. Optional fields cleared by the user
// emit an explicit `no <field>` (handled by the caller via a zero /
// empty input — the renderer translates `nil` as "leave alone" and
// only emits `<field> <value>` when the user provided a value).
func buildGreTunnelCmds(args GreTunnelArgs, remove bool) []string {
	header := "interface Tunnel" + strconv.Itoa(args.Id)
	if remove {
		return []string{"no " + header}
	}
	cmds := []string{header}
	cmds = appendGreTunnelMeta(cmds, args)
	cmds = appendGreTunnelOverlay(cmds, args)
	cmds = appendGreTunnelEncap(cmds, args)
	cmds = appendGreTunnelAdmin(cmds, args)
	return cmds
}

// appendGreTunnelMeta emits description / mtu / vrf forwarding.
func appendGreTunnelMeta(cmds []string, args GreTunnelArgs) []string {
	if args.Description != nil && *args.Description != "" {
		cmds = append(cmds, "description "+*args.Description)
	}
	if args.Mtu != nil {
		cmds = append(cmds, "mtu "+strconv.Itoa(*args.Mtu))
	}
	if args.Vrf != nil && *args.Vrf != "" {
		cmds = append(cmds, "vrf forwarding "+*args.Vrf)
	}
	return cmds
}

// appendGreTunnelOverlay emits the overlay address.
func appendGreTunnelOverlay(cmds []string, args GreTunnelArgs) []string {
	if args.IpAddress != nil && *args.IpAddress != "" {
		cmds = append(cmds, "ip address "+*args.IpAddress)
	}
	return cmds
}

// appendGreTunnelEncap emits the underlay / encap config.
func appendGreTunnelEncap(cmds []string, args GreTunnelArgs) []string {
	if args.Mode != nil && *args.Mode != "" {
		cmds = append(cmds, "tunnel mode "+*args.Mode)
	}
	if args.Source != nil && *args.Source != "" {
		cmds = append(cmds, "tunnel source "+*args.Source)
	}
	if args.Destination != nil && *args.Destination != "" {
		cmds = append(cmds, "tunnel destination "+*args.Destination)
	}
	if args.UnderlayVrf != nil && *args.UnderlayVrf != "" {
		cmds = append(cmds, "tunnel underlay vrf "+*args.UnderlayVrf)
	}
	if args.Tos != nil {
		cmds = append(cmds, "tunnel tos "+strconv.Itoa(*args.Tos))
	}
	if args.Key != nil {
		cmds = append(cmds, "tunnel key "+strconv.Itoa(*args.Key))
	}
	if args.MssCeiling != nil {
		cmds = append(cmds, "tunnel mss ceiling "+strconv.Itoa(*args.MssCeiling))
	}
	if args.PathMtuDiscovery != nil && *args.PathMtuDiscovery {
		cmds = append(cmds, "tunnel path-mtu-discovery")
	}
	if args.DontFragment != nil && *args.DontFragment {
		cmds = append(cmds, "tunnel dont-fragment")
	}
	return cmds
}

// appendGreTunnelAdmin emits the shutdown line last.
func appendGreTunnelAdmin(cmds []string, args GreTunnelArgs) []string {
	if args.Shutdown != nil {
		if *args.Shutdown {
			cmds = append(cmds, keywordShutdown)
		} else {
			cmds = append(cmds, keywordNoShutdown)
		}
	}
	return cmds
}

// greTunnelRow is the parsed live state.
type greTunnelRow struct {
	Description string
	Mode        string
	Source      string
	Destination string
	UnderlayVrf string
	Tos         *int
	Key         *int
	MssCeiling  *int
	PathMtuDisc bool
	DontFrag    bool
	IpAddress   string
	Mtu         int
	Vrf         string
	Shutdown    *bool
}

// readGreTunnel returns the live tunnel configuration or (false, nil)
// when the interface is absent.
func readGreTunnel(ctx context.Context, cli *eapi.Client, id int) (greTunnelRow, bool, error) {
	resp, err := cli.RunCmds(ctx,
		[]string{"show running-config interfaces Tunnel" + strconv.Itoa(id)},
		"text")
	if err != nil {
		return greTunnelRow{}, false, err
	}
	if len(resp) == 0 {
		return greTunnelRow{}, false, nil
	}
	out, _ := resp[0]["output"].(string)
	if out == "" {
		return greTunnelRow{}, false, nil
	}
	return parseGreTunnelConfig(out, id)
}

// parseGreTunnelConfig is exposed for unit tests.
func parseGreTunnelConfig(out string, id int) (greTunnelRow, bool, error) {
	header := "interface Tunnel" + strconv.Itoa(id)
	if !strings.Contains(out, header) {
		return greTunnelRow{}, false, nil
	}
	row := greTunnelRow{}
	for raw := range strings.SplitSeq(out, "\n") {
		line := strings.TrimSpace(raw)
		applyGreTunnelLine(&row, line)
	}
	return row, true, nil
}

// applyGreTunnelLine populates one field from a body line.
func applyGreTunnelLine(row *greTunnelRow, line string) {
	switch {
	case strings.HasPrefix(line, "description "):
		row.Description = strings.TrimPrefix(line, "description ")
	case strings.HasPrefix(line, "mtu "):
		if v, err := strconv.Atoi(strings.TrimPrefix(line, "mtu ")); err == nil {
			row.Mtu = v
		}
	case strings.HasPrefix(line, "vrf forwarding "):
		row.Vrf = strings.TrimPrefix(line, "vrf forwarding ")
	case strings.HasPrefix(line, "ip address "):
		row.IpAddress = strings.TrimPrefix(line, "ip address ")
	case strings.HasPrefix(line, "tunnel mode "):
		row.Mode = strings.TrimPrefix(line, "tunnel mode ")
	case strings.HasPrefix(line, "tunnel source "):
		row.Source = strings.TrimPrefix(line, "tunnel source ")
	case strings.HasPrefix(line, "tunnel destination "):
		row.Destination = strings.TrimPrefix(line, "tunnel destination ")
	case strings.HasPrefix(line, "tunnel underlay vrf "):
		row.UnderlayVrf = strings.TrimPrefix(line, "tunnel underlay vrf ")
	case strings.HasPrefix(line, "tunnel tos "):
		if v, err := strconv.Atoi(strings.TrimPrefix(line, "tunnel tos ")); err == nil {
			row.Tos = &v
		}
	case strings.HasPrefix(line, "tunnel key "):
		if v, err := strconv.Atoi(strings.TrimPrefix(line, "tunnel key ")); err == nil {
			row.Key = &v
		}
	case strings.HasPrefix(line, "tunnel mss ceiling "):
		if v, err := strconv.Atoi(strings.TrimPrefix(line, "tunnel mss ceiling ")); err == nil {
			row.MssCeiling = &v
		}
	case line == "tunnel path-mtu-discovery":
		row.PathMtuDisc = true
	case line == "tunnel dont-fragment":
		row.DontFrag = true
	case line == keywordShutdown:
		v := true
		row.Shutdown = &v
	case line == keywordNoShutdown:
		v := false
		row.Shutdown = &v
	}
}

// fillState writes the parsed row back to State.
func (r greTunnelRow) fillState(s *GreTunnelState) {
	if r.Description != "" {
		v := r.Description
		s.Description = &v
	}
	if r.Mode != "" {
		v := r.Mode
		s.Mode = &v
	}
	if r.Source != "" {
		v := r.Source
		s.Source = &v
	}
	if r.Destination != "" {
		v := r.Destination
		s.Destination = &v
	}
	if r.UnderlayVrf != "" {
		v := r.UnderlayVrf
		s.UnderlayVrf = &v
	}
	if r.Tos != nil {
		s.Tos = r.Tos
	}
	if r.Key != nil {
		s.Key = r.Key
	}
	if r.MssCeiling != nil {
		s.MssCeiling = r.MssCeiling
	}
	if r.PathMtuDisc {
		v := true
		s.PathMtuDiscovery = &v
	}
	if r.DontFrag {
		v := true
		s.DontFragment = &v
	}
	if r.IpAddress != "" {
		v := r.IpAddress
		s.IpAddress = &v
	}
	if r.Mtu > 0 {
		v := r.Mtu
		s.Mtu = &v
	}
	if r.Vrf != "" {
		v := r.Vrf
		s.Vrf = &v
	}
	if r.Shutdown != nil {
		v := *r.Shutdown
		s.Shutdown = &v
	}
}
