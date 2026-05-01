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

// Sentinel errors specific to Subinterface.
var (
	ErrSubinterfaceNameInvalid    = errors.New("subinterface name must be Ethernet<N>[/<sub>...].<id> or Port-Channel<N>.<id>")
	ErrSubinterfaceVlanOutOfRange = errors.New("encapsulationVlan must be in 1..4094")
	ErrSubinterfaceBadIPv4        = errors.New("subinterface ipAddress must be a valid IPv4 CIDR")
	ErrSubinterfaceBadIPv6        = errors.New("subinterface ipv6Address must be a valid IPv6 CIDR")
	ErrSubinterfaceBfdIncomplete  = errors.New("subinterface bfd requires interval, minRx, and multiplier together")
	ErrSubinterfaceBfdMultiplier  = errors.New("subinterface bfd multiplier must be in 3..50")
	ErrSubinterfaceBfdInterval    = errors.New("subinterface bfd interval and minRx must be > 0 ms")
)

// subinterfaceNameRe matches `Ethernet<N>[/<m>...].<sub>` and
// `Port-Channel<N>.<sub>` — the only forms EOS accepts for routed L3
// sub-interfaces (TOI 13633 + EOS User Manual §13.7).
var subinterfaceNameRe = regexp.MustCompile(`^(Ethernet|Port-Channel)\d+(\/\d+)*\.\d+$`)

// Subinterface models an EOS Layer-3 802.1Q sub-interface
// (`Ethernet<N>.<sub>` or `Port-Channel<N>.<sub>`). The parent interface
// must already be in routed mode (`no switchport`) and admin up — that
// state is owned by `eos:l2:Interface` / `eos:l2:PortChannel`.
//
// Source: EOS User Manual §13.7 — Subinterfaces; TOI 13633 (Subinterfaces);
// TOI 17032 (L3 sub-interfaces on access ports).
type Subinterface struct{}

// SubinterfaceBfd is the per-sub-interface BFD timer bundle.
//
// All three fields are required when set; partial bundles are rejected
// at validation time.
type SubinterfaceBfd struct {
	// Interval is the BFD transmit rate in milliseconds.
	Interval int `pulumi:"interval"`
	// MinRx is the minimum receive interval in milliseconds.
	MinRx int `pulumi:"minRx"`
	// Multiplier is the detection multiplier (3..50).
	Multiplier int `pulumi:"multiplier"`
}

// Annotate documents SubinterfaceBfd fields.
func (b *SubinterfaceBfd) Annotate(an infer.Annotator) {
	an.Describe(&b.Interval, "BFD transmit rate in milliseconds.")
	an.Describe(&b.MinRx, "Minimum receive interval in milliseconds.")
	an.Describe(&b.Multiplier, "BFD detection multiplier (3..50).")
}

// SubinterfaceArgs is the input set.
type SubinterfaceArgs struct {
	// Name is the EOS sub-interface identifier — e.g.
	// `Ethernet11.4011` or `Port-Channel10.4011`. The parent must
	// already be routed (`no switchport`).
	Name string `pulumi:"name"`
	// EncapsulationVlan is the 802.1Q tag carried inside the parent
	// interface (1..4094). Required: a sub-interface without
	// encapsulation does not come up.
	EncapsulationVlan int `pulumi:"encapsulationVlan"`
	// Description sets `description …`.
	Description *string `pulumi:"description,optional"`
	// IPAddress is the primary IPv4 in CIDR (e.g. `10.0.0.1/24`).
	IPAddress *string `pulumi:"ipAddress,optional"`
	// IPv6Address is the primary IPv6 in CIDR.
	IPv6Address *string `pulumi:"ipv6Address,optional"`
	// Vrf binds the sub-interface to a non-default VRF.
	Vrf *string `pulumi:"vrf,optional"`
	// Mtu sets the IP MTU.
	Mtu *int `pulumi:"mtu,optional"`
	// Shutdown administratively shuts the sub-interface down.
	Shutdown *bool `pulumi:"shutdown,optional"`
	// Bfd configures per-sub-interface BFD timers.
	Bfd *SubinterfaceBfd `pulumi:"bfd,optional"`

	// Host overrides the provider-level eosUrl host for this resource.
	Host *string `pulumi:"host,optional"`
	// Username overrides the provider-level eosUsername for this resource.
	Username *string `pulumi:"username,optional"`
	// Password overrides the provider-level eosPassword for this resource.
	Password *string `provider:"secret" pulumi:"password,optional"`
}

// SubinterfaceState mirrors Args.
type SubinterfaceState struct {
	SubinterfaceArgs
}

// Annotate documents the resource.
func (r *Subinterface) Annotate(a infer.Annotator) {
	a.Describe(&r, "An EOS Layer-3 802.1Q sub-interface (`Ethernet<N>.<sub>` or `Port-Channel<N>.<sub>`). Parent interface must be routed.")
}

// Annotate documents SubinterfaceArgs fields.
func (a *SubinterfaceArgs) Annotate(an infer.Annotator) {
	an.Describe(&a.Name, "Sub-interface identifier — `Ethernet<N>.<sub>` or `Port-Channel<N>.<sub>`. The parent must be in routed mode.")
	an.Describe(&a.EncapsulationVlan, "802.1Q tag carried on the parent (1..4094). Required for the sub-interface to come up.")
	an.Describe(&a.Description, "Interface description.")
	an.Describe(&a.IPAddress, "Primary IPv4 address (CIDR, e.g. 10.0.0.1/24).")
	an.Describe(&a.IPv6Address, "Primary IPv6 address (CIDR).")
	an.Describe(&a.Vrf, "Optional non-default VRF binding.")
	an.Describe(&a.Mtu, "IP MTU. Per-subinterface MTU is platform-conditional — cEOSLab 4.36 returns 'Unavailable command' at commit, while production EOS (DCS-7280R*, 7500R*, 7050X* etc.) accepts it.")
	an.Describe(&a.Shutdown, "When true, sub-interface is administratively shut down.")
	an.Describe(&a.Bfd, "Per-sub-interface BFD timers (interval / minRx / multiplier).")
	an.Describe(&a.Host, "Optional management hostname override.")
	an.Describe(&a.Username, "Optional AAA username override.")
	an.Describe(&a.Password, "Optional AAA password override.")
}

// Annotate is a no-op; State has no extra fields.
func (s *SubinterfaceState) Annotate(_ infer.Annotator) {}

// Create configures the sub-interface.
func (*Subinterface) Create(ctx context.Context, req infer.CreateRequest[SubinterfaceArgs]) (infer.CreateResponse[SubinterfaceState], error) {
	if err := validateSubinterface(req.Inputs); err != nil {
		return infer.CreateResponse[SubinterfaceState]{}, err
	}
	state := SubinterfaceState{SubinterfaceArgs: req.Inputs}
	if req.DryRun {
		return infer.CreateResponse[SubinterfaceState]{ID: subinterfaceID(req.Inputs.Name), Output: state}, nil
	}
	if err := applySubinterface(ctx, req.Inputs, false); err != nil {
		return infer.CreateResponse[SubinterfaceState]{}, fmt.Errorf("create subinterface %s: %w", req.Inputs.Name, err)
	}
	return infer.CreateResponse[SubinterfaceState]{ID: subinterfaceID(req.Inputs.Name), Output: state}, nil
}

// Read refreshes sub-interface state from the device.
func (*Subinterface) Read(ctx context.Context, req infer.ReadRequest[SubinterfaceArgs, SubinterfaceState]) (infer.ReadResponse[SubinterfaceArgs, SubinterfaceState], error) {
	cli, err := newClient(ctx, req.Inputs.Host, req.Inputs.Username, req.Inputs.Password)
	if err != nil {
		return infer.ReadResponse[SubinterfaceArgs, SubinterfaceState]{}, err
	}
	current, found, err := readSubinterface(ctx, cli, req.Inputs.Name)
	if err != nil {
		return infer.ReadResponse[SubinterfaceArgs, SubinterfaceState]{}, err
	}
	if !found {
		return infer.ReadResponse[SubinterfaceArgs, SubinterfaceState]{}, nil
	}
	state := SubinterfaceState{SubinterfaceArgs: req.Inputs}
	current.fillState(&state)
	return infer.ReadResponse[SubinterfaceArgs, SubinterfaceState]{
		ID:     subinterfaceID(req.Inputs.Name),
		Inputs: req.Inputs,
		State:  state,
	}, nil
}

// Update re-applies the sub-interface configuration.
func (*Subinterface) Update(ctx context.Context, req infer.UpdateRequest[SubinterfaceArgs, SubinterfaceState]) (infer.UpdateResponse[SubinterfaceState], error) {
	if err := validateSubinterface(req.Inputs); err != nil {
		return infer.UpdateResponse[SubinterfaceState]{}, err
	}
	state := SubinterfaceState{SubinterfaceArgs: req.Inputs}
	if req.DryRun {
		return infer.UpdateResponse[SubinterfaceState]{Output: state}, nil
	}
	if err := applySubinterface(ctx, req.Inputs, false); err != nil {
		return infer.UpdateResponse[SubinterfaceState]{}, fmt.Errorf("update subinterface %s: %w", req.Inputs.Name, err)
	}
	return infer.UpdateResponse[SubinterfaceState]{Output: state}, nil
}

// Delete removes the sub-interface.
func (*Subinterface) Delete(ctx context.Context, req infer.DeleteRequest[SubinterfaceState]) (infer.DeleteResponse, error) {
	if err := applySubinterface(ctx, req.State.SubinterfaceArgs, true); err != nil {
		return infer.DeleteResponse{}, fmt.Errorf("delete subinterface %s: %w", req.State.Name, err)
	}
	return infer.DeleteResponse{}, nil
}

// --- helpers ---------------------------------------------------------------

func validateSubinterface(args SubinterfaceArgs) error {
	if !subinterfaceNameRe.MatchString(args.Name) {
		return fmt.Errorf("%w: got %q", ErrSubinterfaceNameInvalid, args.Name)
	}
	if args.EncapsulationVlan < 1 || args.EncapsulationVlan > 4094 {
		return fmt.Errorf("%w: got %d", ErrSubinterfaceVlanOutOfRange, args.EncapsulationVlan)
	}
	if args.IPAddress != nil && *args.IPAddress != "" {
		if pfx, err := netip.ParsePrefix(*args.IPAddress); err != nil || !pfx.Addr().Is4() {
			return fmt.Errorf("%w: %q", ErrSubinterfaceBadIPv4, *args.IPAddress)
		}
	}
	if args.IPv6Address != nil && *args.IPv6Address != "" {
		if pfx, err := netip.ParsePrefix(*args.IPv6Address); err != nil || !pfx.Addr().Is6() || pfx.Addr().Is4In6() {
			return fmt.Errorf("%w: %q", ErrSubinterfaceBadIPv6, *args.IPv6Address)
		}
	}
	if args.Bfd != nil {
		if args.Bfd.Interval <= 0 || args.Bfd.MinRx <= 0 {
			return fmt.Errorf("%w: got interval=%d minRx=%d", ErrSubinterfaceBfdInterval, args.Bfd.Interval, args.Bfd.MinRx)
		}
		if args.Bfd.Multiplier < 3 || args.Bfd.Multiplier > 50 {
			return fmt.Errorf("%w: got %d", ErrSubinterfaceBfdMultiplier, args.Bfd.Multiplier)
		}
	}
	return nil
}

func subinterfaceID(name string) string { return "subinterface/" + name }

func applySubinterface(ctx context.Context, args SubinterfaceArgs, remove bool) error {
	cli, err := newClient(ctx, args.Host, args.Username, args.Password)
	if err != nil {
		return err
	}
	cfg := config.FromContext(ctx)
	sessName := cfg.SessionPrefix() + "subif-" + sanitizeSessionName(args.Name)

	sess, err := cli.OpenSession(ctx, sessName)
	if err != nil {
		return err
	}
	cmds := buildSubinterfaceCmds(args, remove)
	if stageErr := sess.Stage(ctx, cmds); stageErr != nil {
		if abortErr := sess.Abort(ctx); abortErr != nil {
			return errors.Join(stageErr, abortErr)
		}
		return stageErr
	}
	return sess.Commit(ctx)
}

// sanitizeSessionName turns `.` into `-` so session names stay legal.
func sanitizeSessionName(name string) string {
	return strings.ReplaceAll(name, ".", "-")
}

func buildSubinterfaceCmds(args SubinterfaceArgs, remove bool) []string {
	if remove {
		return []string{"no interface " + args.Name}
	}
	cmds := []string{
		"interface " + args.Name,
		"encapsulation dot1q vlan " + strconv.Itoa(args.EncapsulationVlan),
	}
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
	if args.Mtu != nil && *args.Mtu > 0 {
		cmds = append(cmds, "mtu "+strconv.Itoa(*args.Mtu))
	}
	if args.Bfd != nil {
		cmds = append(cmds, fmt.Sprintf("bfd interval %d min-rx %d multiplier %d",
			args.Bfd.Interval, args.Bfd.MinRx, args.Bfd.Multiplier))
	}
	if args.Shutdown != nil && *args.Shutdown {
		cmds = append(cmds, keywordShutdown)
	} else {
		cmds = append(cmds, keywordNoShutdown)
	}
	return cmds
}

// subinterfaceRow is the parsed live state we care about.
type subinterfaceRow struct {
	EncapsulationVlan int
	Description       string
	Vrf               string
	IPAddress         string
	IPv6Address       string
	Mtu               int
	Shutdown          bool
	Bfd               *SubinterfaceBfd
}

// readSubinterface returns the live sub-interface state, or (false, nil)
// when absent.
//
// Source: `show running-config interfaces <name>` (text). The structured
// `show interfaces <name>` JSON elides VRF binding and BFD timers, so we
// parse the running-config block directly.
func readSubinterface(ctx context.Context, cli *eapi.Client, name string) (subinterfaceRow, bool, error) {
	resp, err := cli.RunCmds(ctx,
		[]string{"show running-config interfaces " + name},
		"text")
	if err != nil {
		return subinterfaceRow{}, false, err
	}
	if len(resp) == 0 {
		return subinterfaceRow{}, false, nil
	}
	out, _ := resp[0]["output"].(string)
	if out == "" {
		return subinterfaceRow{}, false, nil
	}
	return parseSubinterfaceConfig(out, name), true, nil
}

// parseSubinterfaceConfig extracts the sub-interface fields from the
// running-config block. Exposed for unit tests.
func parseSubinterfaceConfig(out, name string) subinterfaceRow {
	header := "interface " + name
	if !strings.Contains(out, header) {
		return subinterfaceRow{}
	}
	row := subinterfaceRow{}
	for raw := range strings.SplitSeq(out, "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(line, "encapsulation dot1q vlan "):
			if v, err := strconv.Atoi(strings.TrimPrefix(line, "encapsulation dot1q vlan ")); err == nil {
				row.EncapsulationVlan = v
			}
		case strings.HasPrefix(line, "description "):
			row.Description = strings.TrimPrefix(line, "description ")
		case strings.HasPrefix(line, "vrf "):
			row.Vrf = strings.TrimPrefix(line, "vrf ")
		case strings.HasPrefix(line, "ipv6 address "):
			row.IPv6Address = strings.TrimPrefix(line, "ipv6 address ")
		case strings.HasPrefix(line, "ip address "):
			row.IPAddress = strings.TrimPrefix(line, "ip address ")
		case strings.HasPrefix(line, "mtu "):
			if v, err := strconv.Atoi(strings.TrimPrefix(line, "mtu ")); err == nil {
				row.Mtu = v
			}
		case line == keywordShutdown:
			row.Shutdown = true
		case strings.HasPrefix(line, "bfd interval "):
			row.Bfd = parseSubBfdLine(line)
		}
	}
	return row
}

// parseSubBfdLine extracts the per-interface BFD triple.
func parseSubBfdLine(line string) *SubinterfaceBfd {
	tokens := strings.Fields(line)
	bfd := &SubinterfaceBfd{}
	for i := range len(tokens) - 1 {
		switch tokens[i] {
		case "interval":
			if v, err := strconv.Atoi(tokens[i+1]); err == nil {
				bfd.Interval = v
			}
		case "min-rx":
			if v, err := strconv.Atoi(tokens[i+1]); err == nil {
				bfd.MinRx = v
			}
		case "multiplier":
			if v, err := strconv.Atoi(tokens[i+1]); err == nil {
				bfd.Multiplier = v
			}
		}
	}
	if bfd.Interval == 0 || bfd.MinRx == 0 || bfd.Multiplier == 0 {
		return nil
	}
	return bfd
}

func (r subinterfaceRow) fillState(s *SubinterfaceState) {
	if r.EncapsulationVlan > 0 {
		s.EncapsulationVlan = r.EncapsulationVlan
	}
	if r.Description != "" {
		v := r.Description
		s.Description = &v
	}
	if r.Vrf != "" {
		v := r.Vrf
		s.Vrf = &v
	}
	if r.IPAddress != "" {
		v := r.IPAddress
		s.IPAddress = &v
	}
	if r.IPv6Address != "" {
		v := r.IPv6Address
		s.IPv6Address = &v
	}
	if r.Mtu > 0 {
		v := r.Mtu
		s.Mtu = &v
	}
	if r.Shutdown {
		v := true
		s.Shutdown = &v
	}
	if r.Bfd != nil {
		bfd := *r.Bfd
		s.Bfd = &bfd
	}
}
