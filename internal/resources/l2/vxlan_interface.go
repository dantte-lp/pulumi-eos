package l2

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"

	"github.com/pulumi/pulumi-go-provider/infer"

	"github.com/dantte-lp/pulumi-eos/internal/client/eapi"
	"github.com/dantte-lp/pulumi-eos/internal/config"
)

// DefaultVxlanUDPPort is the IANA-assigned VXLAN port (RFC 7348 §5).
// EOS defaults to 4789 when `vxlan udp-port` is omitted.
const DefaultVxlanUDPPort = 4789

// VniMin / VniMax bound the 24-bit VNI namespace (RFC 7348 §5).
const (
	VniMin = 1
	VniMax = 16777214
)

// Sentinel errors specific to VxlanInterface.
var (
	ErrVxlanIDOutOfRange      = errors.New("vxlanInterface id must be > 0")
	ErrVxlanSourceMissing     = errors.New("vxlanInterface sourceInterface is required")
	ErrVxlanVniOutOfRange     = errors.New("vni must be in 1..16777214 (RFC 7348)")
	ErrVxlanUDPPortOutOfRange = errors.New("udpPort must be in 1..65535")
	ErrVxlanVlanVniDuplicate  = errors.New("vlanVniMap contains duplicate vlan id")
	ErrVxlanVrfVniDuplicate   = errors.New("vrfVniMap contains duplicate vrf name")
	ErrVxlanFloodVtepNotIP    = errors.New("floodVteps entry is not a valid IP address")
)

// VxlanInterface models the singleton-ish EOS Vxlan interface (`interface
// VxlanN`). It is the overlay-defining resource: source-interface anchor,
// UDP port, VLAN→VNI and VRF→VNI maps, optional head-end replication list.
//
// Sources (verified via arista-mcp):
//   - EOS TOI 14304 — EVPN-S IRB in default VRF (Vxlan / VRF→VNI map).
//   - EOS TOI 14402 — EVPN VXLAN IPv6 overlay (VLAN→VNI / VRF→VNI map).
//   - EOS TOI 14448 — VXLAN bridging / routing on 7500R3 (`vxlan udp-port`,
//     `vxlan flood vtep`).
type VxlanInterface struct{}

// VlanVniEntry maps one VLAN to one VNI.
type VlanVniEntry struct {
	// VlanId is the local VLAN that traffic for the VNI is bridged into.
	VlanId int `pulumi:"vlanId"`
	// Vni is the 24-bit VXLAN Network Identifier.
	Vni int `pulumi:"vni"`
}

// VrfVniEntry maps one VRF to its L3 VNI for symmetric IRB.
type VrfVniEntry struct {
	// Vrf is the VRF name (must exist in `vrf instance` separately).
	Vrf string `pulumi:"vrf"`
	// Vni is the 24-bit L3 VNI used for inter-subnet routing.
	Vni int `pulumi:"vni"`
}

// VxlanInterfaceArgs is the input set.
type VxlanInterfaceArgs struct {
	// Id is the Vxlan interface number. EOS supports a single Vxlan
	// interface per device but the index is configurable; convention is 1.
	Id int `pulumi:"id"`
	// Description sets `description …`.
	Description *string `pulumi:"description,optional"`
	// SourceInterface is the underlay anchor (typically `Loopback1`).
	// Required for the overlay to come up.
	SourceInterface string `pulumi:"sourceInterface"`
	// UdpPort overrides the default VXLAN destination port (4789).
	UdpPort *int `pulumi:"udpPort,optional"`
	// VlanVniMap is the explicit VLAN→VNI binding list. Order is
	// significant only for diff stability — EOS sorts by VLAN id on read.
	VlanVniMap []VlanVniEntry `pulumi:"vlanVniMap,optional"`
	// VrfVniMap is the VRF→L3-VNI binding list (symmetric IRB).
	VrfVniMap []VrfVniEntry `pulumi:"vrfVniMap,optional"`
	// FloodVteps is the static head-end replication list. Use only when
	// EVPN control-plane is not in play; otherwise leave empty.
	FloodVteps []string `pulumi:"floodVteps,optional"`

	// Host overrides the provider-level eosUrl host for this resource.
	Host *string `pulumi:"host,optional"`
	// Username overrides the provider-level eosUsername for this resource.
	Username *string `pulumi:"username,optional"`
	// Password overrides the provider-level eosPassword for this resource.
	Password *string `provider:"secret" pulumi:"password,optional"`
}

// VxlanInterfaceState mirrors Args.
type VxlanInterfaceState struct {
	VxlanInterfaceArgs
}

// Annotate documents the resource.
func (r *VxlanInterface) Annotate(a infer.Annotator) {
	a.Describe(&r, "An EOS Vxlan tunnel interface (`interface VxlanN`). Configures the overlay source-interface, UDP port, VLAN→VNI and VRF→VNI bindings, and optional static head-end replication list. Configured through atomic configuration sessions over eAPI.")
}

// Annotate documents VxlanInterfaceArgs fields.
func (a *VxlanInterfaceArgs) Annotate(an infer.Annotator) {
	an.Describe(&a.Id, "Vxlan interface index (typically 1).")
	an.Describe(&a.Description, "Interface description.")
	an.Describe(&a.SourceInterface, "Underlay anchor interface (typically `Loopback1`). Required.")
	an.Describe(&a.UdpPort, "VXLAN destination UDP port. Defaults to 4789 (RFC 7348 §5).")
	an.Describe(&a.VlanVniMap, "Explicit VLAN→VNI binding list.")
	an.Describe(&a.VrfVniMap, "VRF→L3-VNI binding list (symmetric IRB).")
	an.Describe(&a.FloodVteps, "Static head-end replication VTEP list. Use only without an EVPN control plane.")
	an.Describe(&a.Host, "Optional management hostname override.")
	an.Describe(&a.Username, "Optional AAA username override.")
	an.Describe(&a.Password, "Optional AAA password override.")
}

// Annotate is a no-op; State has no extra fields.
func (s *VxlanInterfaceState) Annotate(_ infer.Annotator) {}

// Annotate documents VlanVniEntry fields.
func (e *VlanVniEntry) Annotate(an infer.Annotator) {
	an.Describe(&e.VlanId, "Local VLAN identifier (1..4094).")
	an.Describe(&e.Vni, "VXLAN Network Identifier (1..16777214 per RFC 7348).")
}

// Annotate documents VrfVniEntry fields.
func (e *VrfVniEntry) Annotate(an infer.Annotator) {
	an.Describe(&e.Vrf, "VRF name. The VRF must be configured separately via `vrf instance`.")
	an.Describe(&e.Vni, "L3 VXLAN Network Identifier for symmetric IRB (1..16777214 per RFC 7348).")
}

// Create configures the Vxlan interface.
func (*VxlanInterface) Create(ctx context.Context, req infer.CreateRequest[VxlanInterfaceArgs]) (infer.CreateResponse[VxlanInterfaceState], error) {
	if err := validateVxlanInterface(req.Inputs); err != nil {
		return infer.CreateResponse[VxlanInterfaceState]{}, err
	}
	state := VxlanInterfaceState{VxlanInterfaceArgs: req.Inputs}
	if req.DryRun {
		return infer.CreateResponse[VxlanInterfaceState]{ID: vxlanInterfaceID(req.Inputs.Id), Output: state}, nil
	}
	if err := applyVxlanInterface(ctx, req.Inputs, false); err != nil {
		return infer.CreateResponse[VxlanInterfaceState]{}, fmt.Errorf("create vxlan-interface %d: %w", req.Inputs.Id, err)
	}
	return infer.CreateResponse[VxlanInterfaceState]{ID: vxlanInterfaceID(req.Inputs.Id), Output: state}, nil
}

// Read refreshes Vxlan-interface state from the device.
func (*VxlanInterface) Read(ctx context.Context, req infer.ReadRequest[VxlanInterfaceArgs, VxlanInterfaceState]) (infer.ReadResponse[VxlanInterfaceArgs, VxlanInterfaceState], error) {
	cli, err := newClient(ctx, req.Inputs.Host, req.Inputs.Username, req.Inputs.Password)
	if err != nil {
		return infer.ReadResponse[VxlanInterfaceArgs, VxlanInterfaceState]{}, err
	}
	row, found, err := readVxlanInterface(ctx, cli, req.Inputs.Id)
	if err != nil {
		return infer.ReadResponse[VxlanInterfaceArgs, VxlanInterfaceState]{}, err
	}
	if !found {
		return infer.ReadResponse[VxlanInterfaceArgs, VxlanInterfaceState]{}, nil
	}
	state := VxlanInterfaceState{VxlanInterfaceArgs: req.Inputs}
	row.fillState(&state)
	return infer.ReadResponse[VxlanInterfaceArgs, VxlanInterfaceState]{
		ID:     vxlanInterfaceID(req.Inputs.Id),
		Inputs: req.Inputs,
		State:  state,
	}, nil
}

// Update re-applies the Vxlan-interface configuration.
//
// EOS does not support `default interface VxlanN` in the same shape as for
// physical ports, so Update issues an explicit `no interface VxlanN` then
// re-applies. This guarantees stale VLAN→VNI / VRF→VNI rows are removed.
func (*VxlanInterface) Update(ctx context.Context, req infer.UpdateRequest[VxlanInterfaceArgs, VxlanInterfaceState]) (infer.UpdateResponse[VxlanInterfaceState], error) {
	if err := validateVxlanInterface(req.Inputs); err != nil {
		return infer.UpdateResponse[VxlanInterfaceState]{}, err
	}
	state := VxlanInterfaceState{VxlanInterfaceArgs: req.Inputs}
	if req.DryRun {
		return infer.UpdateResponse[VxlanInterfaceState]{Output: state}, nil
	}
	if err := applyVxlanInterface(ctx, req.Inputs, false); err != nil {
		return infer.UpdateResponse[VxlanInterfaceState]{}, fmt.Errorf("update vxlan-interface %d: %w", req.Inputs.Id, err)
	}
	return infer.UpdateResponse[VxlanInterfaceState]{Output: state}, nil
}

// Delete removes the Vxlan interface via `no interface VxlanN`.
func (*VxlanInterface) Delete(ctx context.Context, req infer.DeleteRequest[VxlanInterfaceState]) (infer.DeleteResponse, error) {
	if err := applyVxlanInterface(ctx, req.State.VxlanInterfaceArgs, true); err != nil {
		return infer.DeleteResponse{}, fmt.Errorf("delete vxlan-interface %d: %w", req.State.Id, err)
	}
	return infer.DeleteResponse{}, nil
}

// validateVxlanInterface enforces id, source-interface, UDP-port, VNI range,
// uniqueness, and IP-address validity for the static flood list.
func validateVxlanInterface(args VxlanInterfaceArgs) error {
	if args.Id < 1 {
		return fmt.Errorf("%w (got %d)", ErrVxlanIDOutOfRange, args.Id)
	}
	if strings.TrimSpace(args.SourceInterface) == "" {
		return ErrVxlanSourceMissing
	}
	if args.UdpPort != nil && (*args.UdpPort < 1 || *args.UdpPort > 65535) {
		return fmt.Errorf("%w (got %d)", ErrVxlanUDPPortOutOfRange, *args.UdpPort)
	}
	seenVlan := map[int]struct{}{}
	for _, e := range args.VlanVniMap {
		if err := validateVlanID(e.VlanId); err != nil {
			return err
		}
		if err := validateVni(e.Vni); err != nil {
			return err
		}
		if _, dup := seenVlan[e.VlanId]; dup {
			return fmt.Errorf("%w (vlanId=%d)", ErrVxlanVlanVniDuplicate, e.VlanId)
		}
		seenVlan[e.VlanId] = struct{}{}
	}
	seenVrf := map[string]struct{}{}
	for _, e := range args.VrfVniMap {
		if strings.TrimSpace(e.Vrf) == "" {
			return errors.New("vrfVniMap entry has empty vrf name") //nolint:err113 // bounds-check.
		}
		if err := validateVni(e.Vni); err != nil {
			return err
		}
		if _, dup := seenVrf[e.Vrf]; dup {
			return fmt.Errorf("%w (vrf=%q)", ErrVxlanVrfVniDuplicate, e.Vrf)
		}
		seenVrf[e.Vrf] = struct{}{}
	}
	for _, vtep := range args.FloodVteps {
		if net.ParseIP(vtep) == nil {
			return fmt.Errorf("%w (got %q)", ErrVxlanFloodVtepNotIP, vtep)
		}
	}
	return nil
}

func validateVni(v int) error {
	if v < VniMin || v > VniMax {
		return fmt.Errorf("%w (got %d)", ErrVxlanVniOutOfRange, v)
	}
	return nil
}

func vxlanInterfaceID(id int) string   { return "vxlan-interface/" + strconv.Itoa(id) }
func vxlanInterfaceName(id int) string { return "Vxlan" + strconv.Itoa(id) }

func applyVxlanInterface(ctx context.Context, args VxlanInterfaceArgs, remove bool) error {
	cli, err := newClient(ctx, args.Host, args.Username, args.Password)
	if err != nil {
		return err
	}
	cfg := config.FromContext(ctx)
	sessName := cfg.SessionPrefix() + "vxlan-" + strconv.Itoa(args.Id)

	sess, err := cli.OpenSession(ctx, sessName)
	if err != nil {
		return err
	}
	cmds := buildVxlanInterfaceCmds(args, remove)
	if stageErr := sess.Stage(ctx, cmds); stageErr != nil {
		if abortErr := sess.Abort(ctx); abortErr != nil {
			return errors.Join(stageErr, abortErr)
		}
		return stageErr
	}
	return sess.Commit(ctx)
}

// buildVxlanInterfaceCmds renders the EOS configuration for the Vxlan
// interface.
//
// Order is fixed for diff stability: description, source-interface,
// udp-port, then VLAN→VNI sorted by VLAN id, VRF→VNI sorted by VRF name,
// flood-vtep list sorted lexicographically. Sorting matches what EOS
// reports under `show running-config interfaces VxlanN`, so applying the
// same args twice is a no-op diff.
func buildVxlanInterfaceCmds(args VxlanInterfaceArgs, remove bool) []string {
	name := vxlanInterfaceName(args.Id)
	if remove {
		return []string{"no interface " + name}
	}
	cmds := []string{"interface " + name}
	if args.Description != nil && *args.Description != "" {
		cmds = append(cmds, "description "+*args.Description)
	}
	cmds = append(cmds, "vxlan source-interface "+args.SourceInterface)
	if args.UdpPort != nil && *args.UdpPort > 0 {
		cmds = append(cmds, "vxlan udp-port "+strconv.Itoa(*args.UdpPort))
	}

	vlans := append([]VlanVniEntry(nil), args.VlanVniMap...)
	sort.Slice(vlans, func(i, j int) bool { return vlans[i].VlanId < vlans[j].VlanId })
	for _, e := range vlans {
		cmds = append(cmds, "vxlan vlan "+strconv.Itoa(e.VlanId)+" vni "+strconv.Itoa(e.Vni))
	}

	vrfs := append([]VrfVniEntry(nil), args.VrfVniMap...)
	sort.Slice(vrfs, func(i, j int) bool { return vrfs[i].Vrf < vrfs[j].Vrf })
	for _, e := range vrfs {
		cmds = append(cmds, "vxlan vrf "+e.Vrf+" vni "+strconv.Itoa(e.Vni))
	}

	if len(args.FloodVteps) > 0 {
		flood := append([]string(nil), args.FloodVteps...)
		sort.Strings(flood)
		cmds = append(cmds, "vxlan flood vtep "+strings.Join(flood, " "))
	}
	return cmds
}

// vxlanInterfaceRow holds the parsed live state.
type vxlanInterfaceRow struct {
	Description     string
	SourceInterface string
	UdpPort         int
	VlanVni         []VlanVniEntry
	VrfVni          []VrfVniEntry
	FloodVteps      []string
}

// readVxlanInterface returns the live Vxlan-interface state, or
// (false, nil) when the interface is absent.
func readVxlanInterface(ctx context.Context, cli *eapi.Client, id int) (vxlanInterfaceRow, bool, error) {
	resp, err := cli.RunCmds(ctx,
		[]string{"show running-config interfaces " + vxlanInterfaceName(id)},
		"text")
	if err != nil {
		return vxlanInterfaceRow{}, false, err
	}
	if len(resp) == 0 {
		return vxlanInterfaceRow{}, false, nil
	}
	out, _ := resp[0]["output"].(string)
	if out == "" {
		return vxlanInterfaceRow{}, false, nil
	}
	return parseVxlanInterfaceConfig(out, id)
}

// parseVxlanInterfaceConfig is exposed for unit tests.
func parseVxlanInterfaceConfig(out string, id int) (vxlanInterfaceRow, bool, error) {
	header := "interface " + vxlanInterfaceName(id)
	if !strings.Contains(out, header) {
		return vxlanInterfaceRow{}, false, nil
	}
	row := vxlanInterfaceRow{}
	for raw := range strings.SplitSeq(out, "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(line, "description "):
			row.Description = strings.TrimPrefix(line, "description ")
		case strings.HasPrefix(line, "vxlan source-interface "):
			row.SourceInterface = strings.TrimPrefix(line, "vxlan source-interface ")
		case strings.HasPrefix(line, "vxlan udp-port "):
			if v, err := strconv.Atoi(strings.TrimPrefix(line, "vxlan udp-port ")); err == nil {
				row.UdpPort = v
			}
		case strings.HasPrefix(line, "vxlan vlan "):
			parseVxlanVlanLine(line, &row)
		case strings.HasPrefix(line, "vxlan vrf "):
			parseVxlanVrfLine(line, &row)
		case strings.HasPrefix(line, "vxlan flood vtep "):
			parts := strings.Fields(strings.TrimPrefix(line, "vxlan flood vtep "))
			row.FloodVteps = append(row.FloodVteps, parts...)
		}
	}
	return row, true, nil
}

// parseVxlanVlanLine extracts one `vxlan vlan <id> vni <vni>` directive.
func parseVxlanVlanLine(line string, row *vxlanInterfaceRow) {
	// `vxlan vlan <vlanId> vni <vni>`
	parts := strings.Fields(line)
	if len(parts) != 5 || parts[3] != "vni" {
		return
	}
	vlanID, err1 := strconv.Atoi(parts[2])
	vni, err2 := strconv.Atoi(parts[4])
	if err1 != nil || err2 != nil {
		return
	}
	row.VlanVni = append(row.VlanVni, VlanVniEntry{VlanId: vlanID, Vni: vni})
}

// parseVxlanVrfLine extracts one `vxlan vrf <name> vni <vni>` directive.
func parseVxlanVrfLine(line string, row *vxlanInterfaceRow) {
	// `vxlan vrf <name> vni <vni>`
	parts := strings.Fields(line)
	if len(parts) != 5 || parts[3] != "vni" {
		return
	}
	vni, err := strconv.Atoi(parts[4])
	if err != nil {
		return
	}
	row.VrfVni = append(row.VrfVni, VrfVniEntry{Vrf: parts[2], Vni: vni})
}

func (r vxlanInterfaceRow) fillState(s *VxlanInterfaceState) {
	if r.Description != "" {
		v := r.Description
		s.Description = &v
	}
	if r.SourceInterface != "" {
		s.SourceInterface = r.SourceInterface
	}
	if r.UdpPort > 0 {
		v := r.UdpPort
		s.UdpPort = &v
	}
	if len(r.VlanVni) > 0 {
		s.VlanVniMap = append(s.VlanVniMap[:0], r.VlanVni...)
	}
	if len(r.VrfVni) > 0 {
		s.VrfVniMap = append(s.VrfVniMap[:0], r.VrfVni...)
	}
	if len(r.FloodVteps) > 0 {
		s.FloodVteps = append(s.FloodVteps[:0], r.FloodVteps...)
	}
}
