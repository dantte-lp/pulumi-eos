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

// Sentinel errors specific to Vrrp.
var (
	ErrVrrpInterfaceRequired   = errors.New("vrrp interface is required")
	ErrVrrpInterfaceBadName    = errors.New("vrrp interface must match an EOS interface name (e.g. Ethernet1, Vlan100, Port-Channel10)")
	ErrVrrpVridRange           = errors.New("vrrp vrid must be in 1..255")
	ErrVrrpPriorityRange       = errors.New("vrrp priority must be in 1..254")
	ErrVrrpAdvertiseRange      = errors.New("vrrp timersAdvertise must be > 0 seconds")
	ErrVrrpPreemptDelayRange   = errors.New("vrrp preemptDelayMinimum must be >= 0 seconds")
	ErrVrrpVirtualAddrInvalid  = errors.New("vrrp virtualAddress must be a valid IPv4 or IPv6 literal")
	ErrVrrpTrackInterfaceEmpty = errors.New("vrrp track entry interface name is required")
	ErrVrrpTrackActionInvalid  = errors.New("vrrp track entry action must be 'decrement' or 'shutdown'")
	ErrVrrpTrackDecrementRange = errors.New("vrrp track entry decrement must be in 1..254")
	ErrVrrpBfdPeerBadIPv4      = errors.New("vrrp bfdPeer must be a valid IPv4 address")
)

// vrrpInterfaceRe enforces the EOS-accepted L3 interface name grammar.
// Verified live against cEOS 4.36.0.1F. v0 supports the leaf-spine
// fabric set: Ethernet, Vlan, Port-Channel, Loopback (HSRP-style on
// loopback is rare but legal).
var vrrpInterfaceRe = regexp.MustCompile(`^(Ethernet|Port-Channel|Vlan|Loopback)\d[\d/]*$`)

// validVrrpTrackActions enumerates the actions accepted on
// `vrrp <vrid> track <intf> <action> [N]`.
var validVrrpTrackActions = map[string]struct{}{
	"decrement": {},
	"shutdown":  {},
}

// Vrrp models a single VRRP virtual router under an interface.
//
// Identity is `(interface, vrid)`; one Pulumi resource maps to one
// VRRP group on one interface. Multiple groups per interface (e.g. a
// VRRPv2 group + a VRRPv3 group, or per-VLAN gateways) coexist as
// distinct resources sharing the same `interface` PK component.
//
// v0 surface uses EOS' inline `vrrp <vrid> <subcommand>` form
// (verified live against cEOS 4.36.0.1F via the integration_probe
// path). Each Args field renders as one or more inline lines under
// the parent `interface` block. The address-family sub-mode form
// (`vrrp <vrid> address-family ipv4` / `address-family ipv6`) is
// deferred to v1 and a future `eos:l3:VrrpV3` resource if the
// inline form proves insufficient.
//
// Render order matches cEOS' canonical running-config so diffs
// against `show running-config interfaces <X>` are minimal.
//
// Source: EOS User Manual §17 (VRRP); cEOS 4.36.0.1F live probe;
// double validation per docs/05-development.md rule 2 + rule 2b.
type Vrrp struct{}

// VrrpTrackEntry is one `vrrp <vrid> track <intf> <action> [N]` line.
type VrrpTrackEntry struct {
	// Interface is the tracked interface name.
	Interface string `pulumi:"interface"`
	// Action must be one of: decrement | shutdown.
	Action string `pulumi:"action"`
	// Decrement is the priority decrement applied when action=decrement.
	Decrement *int `pulumi:"decrement,optional"`
}

// Annotate documents the shape.
func (e *VrrpTrackEntry) Annotate(an infer.Annotator) {
	an.Describe(&e.Interface, "Tracked interface name (e.g. Ethernet1, Vlan100).")
	an.Describe(&e.Action, "Action on tracked link down: 'decrement' or 'shutdown'.")
	an.Describe(&e.Decrement, "Priority decrement for action=decrement (1..254).")
}

// VrrpArgs is the resource input set.
type VrrpArgs struct {
	// Interface is the parent interface name (PK component).
	Interface string `pulumi:"interface"`
	// Vrid is the virtual-router id (PK component, 1..255).
	Vrid int `pulumi:"vrid"`
	// Description is a free-form group description.
	Description *string `pulumi:"description,optional"`
	// Priority is the VRRP priority (1..254). Default 100.
	Priority *int `pulumi:"priority,optional"`
	// Preempt enables preemption when true.
	Preempt *bool `pulumi:"preempt,optional"`
	// PreemptDelayMinimum is the minimum preempt delay in seconds.
	PreemptDelayMinimum *int `pulumi:"preemptDelayMinimum,optional"`
	// TimersAdvertise is the advertisement interval in seconds.
	TimersAdvertise *int `pulumi:"timersAdvertise,optional"`
	// VirtualAddresses is the set of virtual IPs (IPv4 or IPv6).
	// First entry renders as `vrrp <vrid> ip <addr>` (or `ipv6`),
	// subsequent IPv4 entries render with the `secondary` keyword.
	VirtualAddresses []string `pulumi:"virtualAddresses,optional"`
	// Tracks is the set of `vrrp <vrid> track <intf> ...` lines.
	Tracks []VrrpTrackEntry `pulumi:"tracks,optional"`
	// BfdPeer enables `vrrp <vrid> bfd peer <ip>`.
	BfdPeer *string `pulumi:"bfdPeer,optional"`
	// Shutdown disables this group while keeping config.
	Shutdown *bool `pulumi:"shutdown,optional"`

	// Host overrides the provider-level eosUrl host for this resource.
	Host *string `pulumi:"host,optional"`
	// Username overrides the provider-level eosUsername for this resource.
	Username *string `pulumi:"username,optional"`
	// Password overrides the provider-level eosPassword for this resource.
	Password *string `provider:"secret" pulumi:"password,optional"`
}

// VrrpState mirrors Args.
type VrrpState struct {
	VrrpArgs
}

// Annotate documents the resource.
func (r *Vrrp) Annotate(a infer.Annotator) {
	a.Describe(&r, "A VRRP virtual router on an EOS interface (`vrrp <vrid> ...` inline form). v0 covers IPv4 / IPv6 virtual addresses, priority, preempt, timers, description, tracking, BFD peer, shutdown.")
}

// Annotate documents VrrpArgs fields. The structure mirrors
// RpkiArgs.Annotate but the field set and identity model are
// independent — extracting a shared helper would hide per-resource
// semantics that the schema generator surfaces.
//
//nolint:dupl // see comment above
func (a *VrrpArgs) Annotate(an infer.Annotator) {
	an.Describe(&a.Interface, "Parent interface name (PK component, e.g. Ethernet1, Vlan100).")
	an.Describe(&a.Vrid, "Virtual-router id (PK component, 1..255).")
	an.Describe(&a.Description, "Free-form group description.")
	an.Describe(&a.Priority, "VRRP priority (1..254). Default 100 on EOS.")
	an.Describe(&a.Preempt, "Enable preemption when true.")
	an.Describe(&a.PreemptDelayMinimum, "Minimum preempt delay (seconds).")
	an.Describe(&a.TimersAdvertise, "Advertisement interval (seconds).")
	an.Describe(&a.VirtualAddresses, "Virtual IPs (IPv4 or IPv6 literals). First IPv4 renders as primary; subsequent IPv4s as secondary; IPv6 entries render via `vrrp <vrid> ipv6 <addr>`.")
	an.Describe(&a.Tracks, "Track-and-decrement / track-and-shutdown entries.")
	an.Describe(&a.BfdPeer, "BFD peer IPv4 for fast failover (`vrrp <vrid> bfd peer <ip>`).")
	an.Describe(&a.Shutdown, "Disable this group while keeping config when true.")
	an.Describe(&a.Host, "Optional management hostname override.")
	an.Describe(&a.Username, "Optional AAA username override.")
	an.Describe(&a.Password, "Optional AAA password override.")
}

// Annotate is a no-op; State has no extra fields.
func (s *VrrpState) Annotate(_ infer.Annotator) {}

// Create configures the VRRP group.
func (*Vrrp) Create(ctx context.Context, req infer.CreateRequest[VrrpArgs]) (infer.CreateResponse[VrrpState], error) {
	if err := validateVrrp(req.Inputs); err != nil {
		return infer.CreateResponse[VrrpState]{}, err
	}
	state := VrrpState{VrrpArgs: req.Inputs}
	if req.DryRun {
		return infer.CreateResponse[VrrpState]{ID: vrrpID(req.Inputs.Interface, req.Inputs.Vrid), Output: state}, nil
	}
	if err := applyVrrp(ctx, req.Inputs, false); err != nil {
		return infer.CreateResponse[VrrpState]{}, fmt.Errorf("create vrrp %s/%d: %w", req.Inputs.Interface, req.Inputs.Vrid, err)
	}
	return infer.CreateResponse[VrrpState]{ID: vrrpID(req.Inputs.Interface, req.Inputs.Vrid), Output: state}, nil
}

// Read refreshes group state from the device.
func (*Vrrp) Read(ctx context.Context, req infer.ReadRequest[VrrpArgs, VrrpState]) (infer.ReadResponse[VrrpArgs, VrrpState], error) {
	cli, err := newClient(ctx, req.Inputs.Host, req.Inputs.Username, req.Inputs.Password)
	if err != nil {
		return infer.ReadResponse[VrrpArgs, VrrpState]{}, err
	}
	current, found, err := readVrrp(ctx, cli, req.Inputs.Interface, req.Inputs.Vrid)
	if err != nil {
		return infer.ReadResponse[VrrpArgs, VrrpState]{}, err
	}
	if !found {
		return infer.ReadResponse[VrrpArgs, VrrpState]{}, nil
	}
	state := VrrpState{VrrpArgs: req.Inputs}
	current.fillState(&state)
	return infer.ReadResponse[VrrpArgs, VrrpState]{
		ID:     vrrpID(req.Inputs.Interface, req.Inputs.Vrid),
		Inputs: req.Inputs,
		State:  state,
	}, nil
}

// Update re-applies the body. Re-emit only — EOS' session diff
// computes the delta; the VRRP group does not flap.
func (*Vrrp) Update(ctx context.Context, req infer.UpdateRequest[VrrpArgs, VrrpState]) (infer.UpdateResponse[VrrpState], error) {
	if err := validateVrrp(req.Inputs); err != nil {
		return infer.UpdateResponse[VrrpState]{}, err
	}
	state := VrrpState{VrrpArgs: req.Inputs}
	if req.DryRun {
		return infer.UpdateResponse[VrrpState]{Output: state}, nil
	}
	if err := applyVrrp(ctx, req.Inputs, false); err != nil {
		return infer.UpdateResponse[VrrpState]{}, fmt.Errorf("update vrrp %s/%d: %w", req.Inputs.Interface, req.Inputs.Vrid, err)
	}
	return infer.UpdateResponse[VrrpState]{Output: state}, nil
}

// Delete removes the VRRP group via `no vrrp <vrid>` under the parent
// interface. Other groups on the same interface are unaffected.
func (*Vrrp) Delete(ctx context.Context, req infer.DeleteRequest[VrrpState]) (infer.DeleteResponse, error) {
	if err := applyVrrp(ctx, req.State.VrrpArgs, true); err != nil {
		return infer.DeleteResponse{}, fmt.Errorf("delete vrrp %s/%d: %w", req.State.Interface, req.State.Vrid, err)
	}
	return infer.DeleteResponse{}, nil
}

// --- helpers ---------------------------------------------------------------

func validateVrrp(args VrrpArgs) error {
	if strings.TrimSpace(args.Interface) == "" {
		return ErrVrrpInterfaceRequired
	}
	if !vrrpInterfaceRe.MatchString(args.Interface) {
		return fmt.Errorf("%w: %q", ErrVrrpInterfaceBadName, args.Interface)
	}
	if args.Vrid < 1 || args.Vrid > 255 {
		return fmt.Errorf("%w: got %d", ErrVrrpVridRange, args.Vrid)
	}
	if args.Priority != nil && (*args.Priority < 1 || *args.Priority > 254) {
		return fmt.Errorf("%w: got %d", ErrVrrpPriorityRange, *args.Priority)
	}
	if args.TimersAdvertise != nil && *args.TimersAdvertise <= 0 {
		return fmt.Errorf("%w: got %d", ErrVrrpAdvertiseRange, *args.TimersAdvertise)
	}
	if args.PreemptDelayMinimum != nil && *args.PreemptDelayMinimum < 0 {
		return fmt.Errorf("%w: got %d", ErrVrrpPreemptDelayRange, *args.PreemptDelayMinimum)
	}
	for _, v := range args.VirtualAddresses {
		if _, err := netip.ParseAddr(v); err != nil {
			return fmt.Errorf("%w: %q", ErrVrrpVirtualAddrInvalid, v)
		}
	}
	for _, e := range args.Tracks {
		if strings.TrimSpace(e.Interface) == "" {
			return ErrVrrpTrackInterfaceEmpty
		}
		if _, ok := validVrrpTrackActions[e.Action]; !ok {
			return fmt.Errorf("%w: got %q", ErrVrrpTrackActionInvalid, e.Action)
		}
		if e.Action == "decrement" {
			if e.Decrement == nil || *e.Decrement < 1 || *e.Decrement > 254 {
				return fmt.Errorf("%w: got %v", ErrVrrpTrackDecrementRange, e.Decrement)
			}
		}
	}
	if args.BfdPeer != nil && *args.BfdPeer != "" {
		if addr, err := netip.ParseAddr(*args.BfdPeer); err != nil || !addr.Is4() {
			return fmt.Errorf("%w: %q", ErrVrrpBfdPeerBadIPv4, *args.BfdPeer)
		}
	}
	return nil
}

func vrrpID(intf string, vrid int) string {
	return fmt.Sprintf("vrrp/%s/%d", intf, vrid)
}

func applyVrrp(ctx context.Context, args VrrpArgs, remove bool) error {
	cli, err := newClient(ctx, args.Host, args.Username, args.Password)
	if err != nil {
		return err
	}
	cfg := config.FromContext(ctx)
	sessName := cfg.SessionPrefix() + "vrrp-" + sanitizePrefixListName(args.Interface) + "-" + strconv.Itoa(args.Vrid)
	sess, err := cli.OpenSession(ctx, sessName)
	if err != nil {
		return err
	}
	cmds := buildVrrpCmds(args, remove)
	if stageErr := sess.Stage(ctx, cmds); stageErr != nil {
		if abortErr := sess.Abort(ctx); abortErr != nil {
			return errors.Join(stageErr, abortErr)
		}
		return stageErr
	}
	return sess.Commit(ctx)
}

// buildVrrpCmds renders the staged CLI block. Render shape:
//
//	interface <args.Interface>
//	   vrrp <vrid> ip <addr>            # primary IPv4
//	   vrrp <vrid> ip <addr> secondary  # subsequent IPv4
//	   vrrp <vrid> ipv6 <addr>          # IPv6
//	   vrrp <vrid> priority <N>
//	   vrrp <vrid> preempt
//	   vrrp <vrid> preempt delay minimum <N>
//	   vrrp <vrid> timers advertise <N>
//	   vrrp <vrid> description <text>
//	   vrrp <vrid> track <intf> decrement <N>
//	   vrrp <vrid> track <intf> shutdown
//	   vrrp <vrid> bfd peer <ip>
//	   vrrp <vrid> shutdown / no vrrp <vrid> shutdown
//
// Update strategy: re-emit (same as `eos:l3:GreTunnel`). EOS' session
// diff computes the minimum delta; the VRRP group does not flap.
// Delete: a single `no vrrp <vrid>` removes the entire group while
// leaving other VRRP groups on the same interface intact.
func buildVrrpCmds(args VrrpArgs, remove bool) []string {
	cmds := []string{"interface " + args.Interface}
	vrid := strconv.Itoa(args.Vrid)
	if remove {
		return append(cmds, "no vrrp "+vrid)
	}
	cmds = appendVrrpAddresses(cmds, args.VirtualAddresses, vrid)
	cmds = appendVrrpScalars(cmds, args, vrid)
	// Tracks render as `vrrp <vrid> tracked-object <NAME> ...` per
	// EOS User Manual §14.5.4.24 — the argument is the NAME of an
	// object created by the global `track` command, not an
	// interface name. v0 accepts the input shape but renders only
	// when an `eos:l3:Tracker` resource is shipped (S6 follow-up).
	if len(args.Tracks) > 0 {
		cmds = appendVrrpTracks(cmds, args.Tracks, vrid)
	}
	if args.BfdPeer != nil && *args.BfdPeer != "" {
		// EOS keyword is `bfd ip` per User Manual §16.7.4.22
		// (vrrp bfd ip); not `bfd peer`.
		cmds = append(cmds, "vrrp "+vrid+" bfd ip "+*args.BfdPeer)
	}
	if args.Shutdown != nil {
		// EOS keyword is `disabled` per User Manual §14.5.2.1.3
		// (vrrp disabled / no vrrp disabled). Bare `shutdown` is
		// rejected on cEOS 4.36 inside the `vrrp <vrid>` namespace
		// (the unqualified `shutdown` is reserved for the parent
		// interface).
		if *args.Shutdown {
			cmds = append(cmds, "vrrp "+vrid+" disabled")
		} else {
			cmds = append(cmds, "no vrrp "+vrid+" disabled")
		}
	}
	return cmds
}

// appendVrrpAddresses emits the IPv4 / IPv6 virtual-address lines.
// First IPv4 in the slice is the primary; subsequent IPv4s carry the
// `secondary` suffix. IPv6 addresses always render via `vrrp <vrid>
// ipv6 <addr>`.
//
// IPv4 uses the unambiguous `ipv4` keyword (the legacy `ip` form is
// flagged "Ambiguous command (at token 2: 'ip')" by cEOS 4.36 when
// the same line is staged alongside other `vrrp <vrid> ...`
// subcommands inside a single configure-session — verified live).
func appendVrrpAddresses(cmds []string, addrs []string, vrid string) []string {
	v4Primary := false
	for _, a := range addrs {
		ipa, err := netip.ParseAddr(a)
		if err != nil {
			continue
		}
		switch {
		case ipa.Is6():
			cmds = append(cmds, "vrrp "+vrid+" ipv6 "+a)
		case !v4Primary:
			cmds = append(cmds, "vrrp "+vrid+" ipv4 "+a)
			v4Primary = true
		default:
			cmds = append(cmds, "vrrp "+vrid+" ipv4 "+a+" secondary")
		}
	}
	return cmds
}

// appendVrrpScalars emits priority / preempt / timers / description.
func appendVrrpScalars(cmds []string, args VrrpArgs, vrid string) []string {
	if args.Priority != nil {
		// EOS keyword is `priority-level` per User Manual §14.5.4.21;
		// the legacy `priority` form is "Incomplete token" on cEOS 4.36.
		cmds = append(cmds, "vrrp "+vrid+" priority-level "+strconv.Itoa(*args.Priority))
	}
	if args.Preempt != nil && *args.Preempt {
		cmds = append(cmds, "vrrp "+vrid+" preempt")
	}
	if args.PreemptDelayMinimum != nil {
		cmds = append(cmds, "vrrp "+vrid+" preempt delay minimum "+strconv.Itoa(*args.PreemptDelayMinimum))
	}
	if args.TimersAdvertise != nil {
		// EOS keyword is `advertisement interval` per User Manual
		// §14.5.4.10. The legacy `timers advertise` form is rejected
		// on cEOS 4.36 with `invalid command`.
		cmds = append(cmds, "vrrp "+vrid+" advertisement interval "+strconv.Itoa(*args.TimersAdvertise))
	}
	if args.Description != nil && *args.Description != "" {
		// EOS keyword is `session description` per User Manual
		// §14.5.4.22 (vrrp session description); the bare
		// `description` form is rejected on cEOS 4.36.
		cmds = append(cmds, "vrrp "+vrid+" session description "+*args.Description)
	}
	return cmds
}

// appendVrrpTracks emits `vrrp <vrid> tracked-object <name> ...`
// lines. The `Interface` field of `VrrpTrackEntry` is reused as the
// tracked-object NAME for v0 — EOS' tracked-object names are free-
// form strings ([\w-]+) created by the global `track` command, so
// passing an interface name is wrong in general but lets users
// supply existing tracked-object names directly.
func appendVrrpTracks(cmds []string, tracks []VrrpTrackEntry, vrid string) []string {
	for _, e := range tracks {
		switch e.Action {
		case "decrement":
			d := 0
			if e.Decrement != nil {
				d = *e.Decrement
			}
			cmds = append(cmds, "vrrp "+vrid+" tracked-object "+e.Interface+" decrement "+strconv.Itoa(d))
		case "shutdown":
			cmds = append(cmds, "vrrp "+vrid+" tracked-object "+e.Interface+" shutdown")
		}
	}
	return cmds
}

// vrrpRow is the parsed live state.
type vrrpRow struct {
	Description string
	Priority    int
	Preempt     bool
	PreemptDly  int
	Advertise   int
	Addresses   []string
	Tracks      []VrrpTrackEntry
	BfdPeer     string
	Shutdown    *bool
}

// readVrrp returns the live group state, or (false, nil) when the
// `vrrp <vrid>` body is absent under the named interface.
func readVrrp(ctx context.Context, cli *eapi.Client, intf string, vrid int) (vrrpRow, bool, error) {
	resp, err := cli.RunCmds(ctx,
		[]string{"show running-config interfaces " + intf},
		"text")
	if err != nil {
		return vrrpRow{}, false, err
	}
	if len(resp) == 0 {
		return vrrpRow{}, false, nil
	}
	out, _ := resp[0]["output"].(string)
	if out == "" {
		return vrrpRow{}, false, nil
	}
	return parseVrrpSection(out, intf, vrid)
}

// parseVrrpSection walks the `interface <X>` body and collects every
// `vrrp <vrid> ...` line whose vrid matches.
func parseVrrpSection(out, intf string, vrid int) (vrrpRow, bool, error) {
	header := "interface " + intf
	if !strings.Contains(out, header) {
		return vrrpRow{}, false, nil
	}
	prefix := "vrrp " + strconv.Itoa(vrid) + " "
	row := vrrpRow{}
	found := false
	for raw := range strings.SplitSeq(out, "\n") {
		line := strings.TrimSpace(raw)
		rest, ok := strings.CutPrefix(line, prefix)
		if !ok {
			continue
		}
		applyVrrpLine(&row, rest)
		found = true
	}
	if !found {
		return vrrpRow{}, false, nil
	}
	return row, true, nil
}

// applyVrrpLine populates one field from a body line stripped of the
// `vrrp <vrid> ` prefix. Dispatches to per-category helpers so the
// switch stays under funlen.
func applyVrrpLine(row *vrrpRow, rest string) {
	if applyVrrpAddrLine(row, rest) {
		return
	}
	if applyVrrpScalarLine(row, rest) {
		return
	}
	applyVrrpExtrasLine(row, rest)
}

// applyVrrpAddrLine handles ipv4 / ip / ipv6 lines.
func applyVrrpAddrLine(row *vrrpRow, rest string) bool {
	switch {
	case strings.HasPrefix(rest, "ipv4 "):
		applyVrrpIPv4(row, strings.TrimPrefix(rest, "ipv4 "))
	case strings.HasPrefix(rest, "ip "):
		// Legacy `ip` form retained for forward-compat parsing.
		applyVrrpIPv4(row, strings.TrimPrefix(rest, "ip "))
	case strings.HasPrefix(rest, "ipv6 "):
		row.Addresses = append(row.Addresses, strings.TrimPrefix(rest, "ipv6 "))
	default:
		return false
	}
	return true
}

// applyVrrpScalarLine handles priority / preempt / advertisement.
func applyVrrpScalarLine(row *vrrpRow, rest string) bool {
	switch {
	case strings.HasPrefix(rest, "priority-level "):
		if v, err := strconv.Atoi(strings.TrimPrefix(rest, "priority-level ")); err == nil {
			row.Priority = v
		}
	case strings.HasPrefix(rest, "priority "):
		// Legacy form retained for forward-compat parsing.
		if v, err := strconv.Atoi(strings.TrimPrefix(rest, "priority ")); err == nil {
			row.Priority = v
		}
	case rest == "preempt":
		row.Preempt = true
	case strings.HasPrefix(rest, "preempt delay minimum "):
		if v, err := strconv.Atoi(strings.TrimPrefix(rest, "preempt delay minimum ")); err == nil {
			row.PreemptDly = v
		}
	case strings.HasPrefix(rest, "advertisement interval "):
		if v, err := strconv.Atoi(strings.TrimPrefix(rest, "advertisement interval ")); err == nil {
			row.Advertise = v
		}
	case strings.HasPrefix(rest, "timers advertise "):
		// Legacy form retained for forward-compat parsing.
		if v, err := strconv.Atoi(strings.TrimPrefix(rest, "timers advertise ")); err == nil {
			row.Advertise = v
		}
	default:
		return false
	}
	return true
}

// applyVrrpExtrasLine handles description / tracks / bfd / disabled.
func applyVrrpExtrasLine(row *vrrpRow, rest string) {
	switch {
	case strings.HasPrefix(rest, "session description "):
		row.Description = strings.TrimPrefix(rest, "session description ")
	case strings.HasPrefix(rest, "description "):
		row.Description = strings.TrimPrefix(rest, "description ")
	case strings.HasPrefix(rest, "tracked-object "):
		applyVrrpTrack(row, strings.TrimPrefix(rest, "tracked-object "))
	case strings.HasPrefix(rest, "track "):
		applyVrrpTrack(row, strings.TrimPrefix(rest, "track "))
	case strings.HasPrefix(rest, "bfd ip "):
		row.BfdPeer = strings.TrimPrefix(rest, "bfd ip ")
	case strings.HasPrefix(rest, "bfd peer "):
		row.BfdPeer = strings.TrimPrefix(rest, "bfd peer ")
	case rest == "disabled" || rest == keywordShutdown:
		v := true
		row.Shutdown = &v
	case rest == "no disabled" || rest == "no "+keywordShutdown:
		v := false
		row.Shutdown = &v
	}
}

// applyVrrpIPv4 strips an optional `secondary` suffix and appends.
func applyVrrpIPv4(row *vrrpRow, rest string) {
	addr := strings.TrimSuffix(rest, " secondary")
	row.Addresses = append(row.Addresses, addr)
}

// applyVrrpTrack parses `<intf> decrement <N>` or `<intf> shutdown`.
func applyVrrpTrack(row *vrrpRow, rest string) {
	parts := strings.Fields(rest)
	if len(parts) < 2 {
		return
	}
	switch parts[1] {
	case "decrement":
		if len(parts) < 3 {
			return
		}
		v, err := strconv.Atoi(parts[2])
		if err != nil {
			return
		}
		row.Tracks = append(row.Tracks, VrrpTrackEntry{
			Interface: parts[0], Action: "decrement", Decrement: &v,
		})
	case keywordShutdown:
		row.Tracks = append(row.Tracks, VrrpTrackEntry{
			Interface: parts[0], Action: "shutdown",
		})
	}
}

// fillState writes the parsed row back to State.
func (r vrrpRow) fillState(s *VrrpState) {
	if r.Description != "" {
		v := r.Description
		s.Description = &v
	}
	if r.Priority > 0 {
		v := r.Priority
		s.Priority = &v
	}
	if r.Preempt {
		v := true
		s.Preempt = &v
	}
	if r.PreemptDly > 0 {
		v := r.PreemptDly
		s.PreemptDelayMinimum = &v
	}
	if r.Advertise > 0 {
		v := r.Advertise
		s.TimersAdvertise = &v
	}
	if len(r.Addresses) > 0 {
		s.VirtualAddresses = append([]string{}, r.Addresses...)
	}
	if len(r.Tracks) > 0 {
		s.Tracks = append([]VrrpTrackEntry{}, r.Tracks...)
	}
	if r.BfdPeer != "" {
		v := r.BfdPeer
		s.BfdPeer = &v
	}
	if r.Shutdown != nil {
		v := *r.Shutdown
		s.Shutdown = &v
	}
}
