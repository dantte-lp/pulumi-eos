package l2

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/pulumi/pulumi-go-provider/infer"

	"github.com/dantte-lp/pulumi-eos/internal/client/eapi"
	"github.com/dantte-lp/pulumi-eos/internal/config"
)

// DualPrimaryActionErrdisable is the canonical dual-primary detection
// action: `errdisable all-interfaces` (TOI 13947 / TOI 14406). It is the
// only action EOS accepts; the validator rejects anything else before it
// reaches the device.
const DualPrimaryActionErrdisable = "errdisable all-interfaces"

// PrimaryPriorityMin / Max bound the MLAG primary-priority value
// (0..32767, default 32767 — lowest wins) per EOS User Manual §11.3.
const (
	PrimaryPriorityMin = 0
	PrimaryPriorityMax = 32767
)

// Sentinel errors specific to Mlag.
var (
	ErrMlagDomainRequired             = errors.New("mlag domainId is required")
	ErrMlagLocalIfaceRequired         = errors.New("mlag localInterface is required")
	ErrMlagPeerLinkRequired           = errors.New("mlag peerLink is required")
	ErrMlagPeerAddressRequired        = errors.New("mlag peerAddress is required")
	ErrMlagPeerAddressNotIP           = errors.New("mlag peerAddress is not a valid IP")
	ErrMlagHeartbeatNotIP             = errors.New("mlag peerAddressHeartbeat is not a valid IP")
	ErrMlagPrimaryPriorityRange       = errors.New("mlag primaryPriority must be 0..32767")
	ErrMlagDualPrimaryDelayRange      = errors.New("mlag dualPrimaryDetectionDelay must be > 0")
	ErrMlagDualPrimaryActionInvalid   = errors.New("mlag dualPrimaryAction must be 'errdisable all-interfaces'")
	ErrMlagRecoveryDelayNegative      = errors.New("mlag dualPrimaryRecoveryDelay* must be >= 0")
	ErrMlagReloadDelayNegative        = errors.New("mlag reloadDelay* must be >= 0")
	ErrMlagDualPrimaryActionWithDelay = errors.New("mlag dualPrimaryAction requires dualPrimaryDetectionDelay")
)

// Mlag models the singleton `mlag configuration` block (one per EOS
// device). It carries everything needed for an MLAG peer pair to come up:
// domain-id, local SVI, peer-link Port-Channel, peer SVI IP, optional
// out-of-band heartbeat, dual-primary detection / recovery, and
// reload-delay timers.
//
// Sources (verified via arista-mcp):
//   - EOS User Manual §11.3.2 — domain-id, peer-link, peer-address,
//     local-interface, primary-priority semantics.
//   - EOS TOI 13947 — `peer-address heartbeat <ip> [vrf <name>]` and
//     `dual-primary detection delay <sec> [action errdisable
//     all-interfaces]`.
//   - EOS TOI 14406 — `dual-primary recovery delay mlag <sec> non-mlag
//     <sec>` and configured-vs-detected reporting.
type Mlag struct{}

// MlagArgs is the input set.
type MlagArgs struct {
	// DomainId is the text identifier shared by the two peers.
	DomainId string `pulumi:"domainId"`
	// LocalInterface is the SVI used for the MLAG control plane
	// (e.g. `Vlan4094`).
	LocalInterface string `pulumi:"localInterface"`
	// PeerLink is the inter-switch Port-Channel (e.g. `Port-Channel1000`).
	PeerLink string `pulumi:"peerLink"`
	// PeerAddress is the peer's MLAG SVI IP.
	PeerAddress string `pulumi:"peerAddress"`
	// PeerAddressHeartbeat is the peer's management IP used for the
	// out-of-band UDP heartbeat (TOI 13947). Optional.
	PeerAddressHeartbeat *string `pulumi:"peerAddressHeartbeat,optional"`
	// PeerAddressHeartbeatVrf is the VRF in which the heartbeat IP is
	// reachable. Optional; default VRF when unset.
	PeerAddressHeartbeatVrf *string `pulumi:"peerAddressHeartbeatVrf,optional"`
	// PrimaryPriority is the per-peer election priority (0..32767;
	// lowest wins; default 32767).
	PrimaryPriority *int `pulumi:"primaryPriority,optional"`
	// DualPrimaryDetectionDelay configures the dual-primary detection
	// delay in seconds. Required when DualPrimaryAction is set.
	DualPrimaryDetectionDelay *int `pulumi:"dualPrimaryDetectionDelay,optional"`
	// DualPrimaryAction is the action to take on dual-primary detection.
	// Currently `errdisable all-interfaces` is the only supported
	// action (TOI 13947).
	DualPrimaryAction *string `pulumi:"dualPrimaryAction,optional"`
	// DualPrimaryRecoveryDelayMlag is the MLAG-port recovery delay (s).
	DualPrimaryRecoveryDelayMlag *int `pulumi:"dualPrimaryRecoveryDelayMlag,optional"`
	// DualPrimaryRecoveryDelayNonMlag is the non-MLAG-port recovery
	// delay (s).
	DualPrimaryRecoveryDelayNonMlag *int `pulumi:"dualPrimaryRecoveryDelayNonMlag,optional"`
	// ReloadDelayMlag is the post-reload hold for MLAG ports (s).
	ReloadDelayMlag *int `pulumi:"reloadDelayMlag,optional"`
	// ReloadDelayNonMlag is the post-reload hold for non-MLAG ports (s).
	ReloadDelayNonMlag *int `pulumi:"reloadDelayNonMlag,optional"`
	// Shutdown brings the MLAG configuration administratively down.
	Shutdown *bool `pulumi:"shutdown,optional"`

	// Host overrides the provider-level eosUrl host for this resource.
	Host *string `pulumi:"host,optional"`
	// Username overrides the provider-level eosUsername for this resource.
	Username *string `pulumi:"username,optional"`
	// Password overrides the provider-level eosPassword for this resource.
	Password *string `provider:"secret" pulumi:"password,optional"`
}

// MlagState mirrors Args.
type MlagState struct {
	MlagArgs
}

// Annotate documents the resource.
func (r *Mlag) Annotate(a infer.Annotator) {
	a.Describe(&r, "EOS MLAG (Multi-Chassis Link Aggregation Group) configuration. Singleton per device — one `mlag configuration` block.")
}

// Annotate documents MlagArgs fields.
func (a *MlagArgs) Annotate(an infer.Annotator) {
	an.Describe(&a.DomainId, "Text identifier shared by the two MLAG peers (e.g. `dc1-rack1`).")
	an.Describe(&a.LocalInterface, "SVI used for the MLAG control plane (e.g. `Vlan4094`).")
	an.Describe(&a.PeerLink, "Inter-switch Port-Channel (e.g. `Port-Channel1000`).")
	an.Describe(&a.PeerAddress, "Peer's MLAG SVI IP.")
	an.Describe(&a.PeerAddressHeartbeat, "Peer's management IP for out-of-band UDP heartbeat (TOI 13947).")
	an.Describe(&a.PeerAddressHeartbeatVrf, "VRF in which the heartbeat IP is reachable. Default VRF when unset.")
	an.Describe(&a.PrimaryPriority, "Election priority (0..32767; lowest wins; EOS default 32767).")
	an.Describe(&a.DualPrimaryDetectionDelay, "Dual-primary detection delay in seconds. Required when dualPrimaryAction is set.")
	an.Describe(&a.DualPrimaryAction, "Action to take on dual-primary detection. Currently `errdisable all-interfaces` only.")
	an.Describe(&a.DualPrimaryRecoveryDelayMlag, "MLAG-port recovery delay in seconds.")
	an.Describe(&a.DualPrimaryRecoveryDelayNonMlag, "Non-MLAG-port recovery delay in seconds.")
	an.Describe(&a.ReloadDelayMlag, "Post-reload hold for MLAG ports in seconds.")
	an.Describe(&a.ReloadDelayNonMlag, "Post-reload hold for non-MLAG ports in seconds.")
	an.Describe(&a.Shutdown, "When true, shuts down the MLAG configuration administratively.")
	an.Describe(&a.Host, "Optional management hostname override.")
	an.Describe(&a.Username, "Optional AAA username override.")
	an.Describe(&a.Password, "Optional AAA password override.")
}

// Annotate is a no-op; State has no extra fields.
func (s *MlagState) Annotate(_ infer.Annotator) {}

// Create configures the MLAG block.
func (*Mlag) Create(ctx context.Context, req infer.CreateRequest[MlagArgs]) (infer.CreateResponse[MlagState], error) {
	if err := validateMlag(req.Inputs); err != nil {
		return infer.CreateResponse[MlagState]{}, err
	}
	state := MlagState{MlagArgs: req.Inputs}
	if req.DryRun {
		return infer.CreateResponse[MlagState]{ID: mlagID(), Output: state}, nil
	}
	if err := applyMlag(ctx, req.Inputs, false); err != nil {
		return infer.CreateResponse[MlagState]{}, fmt.Errorf("create mlag: %w", err)
	}
	return infer.CreateResponse[MlagState]{ID: mlagID(), Output: state}, nil
}

// Read refreshes the MLAG block from the device.
func (*Mlag) Read(ctx context.Context, req infer.ReadRequest[MlagArgs, MlagState]) (infer.ReadResponse[MlagArgs, MlagState], error) {
	cli, err := newClient(ctx, req.Inputs.Host, req.Inputs.Username, req.Inputs.Password)
	if err != nil {
		return infer.ReadResponse[MlagArgs, MlagState]{}, err
	}
	row, found, err := readMlag(ctx, cli)
	if err != nil {
		return infer.ReadResponse[MlagArgs, MlagState]{}, err
	}
	if !found {
		return infer.ReadResponse[MlagArgs, MlagState]{}, nil
	}
	state := MlagState{MlagArgs: req.Inputs}
	row.fillState(&state)
	return infer.ReadResponse[MlagArgs, MlagState]{
		ID:     mlagID(),
		Inputs: req.Inputs,
		State:  state,
	}, nil
}

// Update re-applies the MLAG block.
//
// EOS keeps each knob inside `mlag configuration` until it is explicitly
// negated. To make idempotent re-apply deterministic we negate the entire
// block first (`no mlag configuration`) then re-render the desired set —
// matching the pattern used by `eos:l2:EvpnEthernetSegment`.
func (*Mlag) Update(ctx context.Context, req infer.UpdateRequest[MlagArgs, MlagState]) (infer.UpdateResponse[MlagState], error) {
	if err := validateMlag(req.Inputs); err != nil {
		return infer.UpdateResponse[MlagState]{}, err
	}
	state := MlagState{MlagArgs: req.Inputs}
	if req.DryRun {
		return infer.UpdateResponse[MlagState]{Output: state}, nil
	}
	if err := applyMlag(ctx, req.Inputs, false); err != nil {
		return infer.UpdateResponse[MlagState]{}, fmt.Errorf("update mlag: %w", err)
	}
	return infer.UpdateResponse[MlagState]{Output: state}, nil
}

// Delete removes the MLAG block via `no mlag configuration`.
func (*Mlag) Delete(ctx context.Context, req infer.DeleteRequest[MlagState]) (infer.DeleteResponse, error) {
	if err := applyMlag(ctx, req.State.MlagArgs, true); err != nil {
		return infer.DeleteResponse{}, fmt.Errorf("delete mlag: %w", err)
	}
	return infer.DeleteResponse{}, nil
}

// validateMlag enforces required fields, IP parsing, ranges, and
// cross-field constraints.
func validateMlag(args MlagArgs) error {
	if strings.TrimSpace(args.DomainId) == "" {
		return ErrMlagDomainRequired
	}
	if strings.TrimSpace(args.LocalInterface) == "" {
		return ErrMlagLocalIfaceRequired
	}
	if strings.TrimSpace(args.PeerLink) == "" {
		return ErrMlagPeerLinkRequired
	}
	if strings.TrimSpace(args.PeerAddress) == "" {
		return ErrMlagPeerAddressRequired
	}
	if net.ParseIP(args.PeerAddress) == nil {
		return fmt.Errorf("%w (got %q)", ErrMlagPeerAddressNotIP, args.PeerAddress)
	}
	if args.PeerAddressHeartbeat != nil && *args.PeerAddressHeartbeat != "" {
		if net.ParseIP(*args.PeerAddressHeartbeat) == nil {
			return fmt.Errorf("%w (got %q)", ErrMlagHeartbeatNotIP, *args.PeerAddressHeartbeat)
		}
	}
	if args.PrimaryPriority != nil {
		if *args.PrimaryPriority < PrimaryPriorityMin || *args.PrimaryPriority > PrimaryPriorityMax {
			return fmt.Errorf("%w (got %d)", ErrMlagPrimaryPriorityRange, *args.PrimaryPriority)
		}
	}
	if args.DualPrimaryDetectionDelay != nil && *args.DualPrimaryDetectionDelay <= 0 {
		return fmt.Errorf("%w (got %d)", ErrMlagDualPrimaryDelayRange, *args.DualPrimaryDetectionDelay)
	}
	if args.DualPrimaryAction != nil && *args.DualPrimaryAction != "" {
		if *args.DualPrimaryAction != DualPrimaryActionErrdisable {
			return fmt.Errorf("%w (got %q)", ErrMlagDualPrimaryActionInvalid, *args.DualPrimaryAction)
		}
		if args.DualPrimaryDetectionDelay == nil {
			return ErrMlagDualPrimaryActionWithDelay
		}
	}
	if args.DualPrimaryRecoveryDelayMlag != nil && *args.DualPrimaryRecoveryDelayMlag < 0 {
		return fmt.Errorf("%w (mlag, got %d)", ErrMlagRecoveryDelayNegative, *args.DualPrimaryRecoveryDelayMlag)
	}
	if args.DualPrimaryRecoveryDelayNonMlag != nil && *args.DualPrimaryRecoveryDelayNonMlag < 0 {
		return fmt.Errorf("%w (non-mlag, got %d)", ErrMlagRecoveryDelayNegative, *args.DualPrimaryRecoveryDelayNonMlag)
	}
	if args.ReloadDelayMlag != nil && *args.ReloadDelayMlag < 0 {
		return fmt.Errorf("%w (mlag, got %d)", ErrMlagReloadDelayNegative, *args.ReloadDelayMlag)
	}
	if args.ReloadDelayNonMlag != nil && *args.ReloadDelayNonMlag < 0 {
		return fmt.Errorf("%w (non-mlag, got %d)", ErrMlagReloadDelayNegative, *args.ReloadDelayNonMlag)
	}
	return nil
}

func mlagID() string { return "mlag" }

func applyMlag(ctx context.Context, args MlagArgs, remove bool) error {
	cli, err := newClient(ctx, args.Host, args.Username, args.Password)
	if err != nil {
		return err
	}
	cfg := config.FromContext(ctx)
	sessName := cfg.SessionPrefix() + "mlag"

	sess, err := cli.OpenSession(ctx, sessName)
	if err != nil {
		return err
	}
	cmds := buildMlagCmds(args, remove)
	if stageErr := sess.Stage(ctx, cmds); stageErr != nil {
		if abortErr := sess.Abort(ctx); abortErr != nil {
			return errors.Join(stageErr, abortErr)
		}
		return stageErr
	}
	return sess.Commit(ctx)
}

// buildMlagCmds renders the ordered command list.
//
// The existing block is always negated first (when not removing) so stale
// peer-address-heartbeat / dual-primary / reload-delay knobs are
// guaranteed gone — same idempotency pattern as
// `eos:l2:EvpnEthernetSegment`.
func buildMlagCmds(args MlagArgs, remove bool) []string {
	cmds := []string{"no mlag configuration"}
	if remove {
		return cmds
	}
	cmds = append(cmds,
		"mlag configuration",
		"domain-id "+args.DomainId,
		"local-interface "+args.LocalInterface,
		"peer-link "+args.PeerLink,
		"peer-address "+args.PeerAddress,
	)
	if args.PeerAddressHeartbeat != nil && *args.PeerAddressHeartbeat != "" {
		line := "peer-address heartbeat " + *args.PeerAddressHeartbeat
		if args.PeerAddressHeartbeatVrf != nil && *args.PeerAddressHeartbeatVrf != "" {
			line += " vrf " + *args.PeerAddressHeartbeatVrf
		}
		cmds = append(cmds, line)
	}
	if args.PrimaryPriority != nil {
		cmds = append(cmds, "primary-priority "+strconv.Itoa(*args.PrimaryPriority))
	}
	if args.DualPrimaryDetectionDelay != nil {
		line := "dual-primary detection delay " + strconv.Itoa(*args.DualPrimaryDetectionDelay)
		if args.DualPrimaryAction != nil && *args.DualPrimaryAction != "" {
			line += " action " + *args.DualPrimaryAction
		}
		cmds = append(cmds, line)
	}
	if args.DualPrimaryRecoveryDelayMlag != nil || args.DualPrimaryRecoveryDelayNonMlag != nil {
		mlagSec := 0
		nonMlagSec := 0
		if args.DualPrimaryRecoveryDelayMlag != nil {
			mlagSec = *args.DualPrimaryRecoveryDelayMlag
		}
		if args.DualPrimaryRecoveryDelayNonMlag != nil {
			nonMlagSec = *args.DualPrimaryRecoveryDelayNonMlag
		}
		cmds = append(cmds,
			fmt.Sprintf("dual-primary recovery delay mlag %d non-mlag %d", mlagSec, nonMlagSec))
	}
	if args.ReloadDelayMlag != nil {
		cmds = append(cmds, "reload-delay mlag "+strconv.Itoa(*args.ReloadDelayMlag))
	}
	if args.ReloadDelayNonMlag != nil {
		cmds = append(cmds, "reload-delay non-mlag "+strconv.Itoa(*args.ReloadDelayNonMlag))
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

// mlagRow holds the parsed live state of `show running-config | section
// mlag configuration`.
type mlagRow struct {
	DomainId                        string
	LocalInterface                  string
	PeerLink                        string
	PeerAddress                     string
	PeerAddressHeartbeat            string
	PeerAddressHeartbeatVrf         string
	PrimaryPriority                 int
	DualPrimaryDetectionDelay       int
	DualPrimaryAction               string
	DualPrimaryRecoveryDelayMlag    int
	DualPrimaryRecoveryDelayNonMlag int
	ReloadDelayMlag                 int
	ReloadDelayNonMlag              int
	Shutdown                        bool
	hasRecoveryDelay                bool
}

// readMlag returns the live MLAG block, or (false, nil) when the device
// has no `mlag configuration` block.
func readMlag(ctx context.Context, cli *eapi.Client) (mlagRow, bool, error) {
	resp, err := cli.RunCmds(ctx,
		[]string{"show running-config | section mlag configuration"},
		"text")
	if err != nil {
		return mlagRow{}, false, err
	}
	if len(resp) == 0 {
		return mlagRow{}, false, nil
	}
	out, _ := resp[0]["output"].(string)
	if out == "" {
		return mlagRow{}, false, nil
	}
	return parseMlagConfig(out)
}

// parseMlagConfig is exposed for unit tests.
func parseMlagConfig(out string) (mlagRow, bool, error) {
	if !strings.Contains(out, "mlag configuration") {
		return mlagRow{}, false, nil
	}
	row := mlagRow{}
	for raw := range strings.SplitSeq(out, "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(line, "domain-id "):
			row.DomainId = strings.TrimPrefix(line, "domain-id ")
		case strings.HasPrefix(line, "local-interface "):
			row.LocalInterface = strings.TrimPrefix(line, "local-interface ")
		case strings.HasPrefix(line, "peer-link "):
			row.PeerLink = strings.TrimPrefix(line, "peer-link ")
		case strings.HasPrefix(line, "peer-address heartbeat "):
			parts := strings.Fields(strings.TrimPrefix(line, "peer-address heartbeat "))
			if len(parts) > 0 {
				row.PeerAddressHeartbeat = parts[0]
			}
			for i := 1; i < len(parts)-1; i++ {
				if parts[i] == "vrf" {
					row.PeerAddressHeartbeatVrf = parts[i+1]
				}
			}
		case strings.HasPrefix(line, "peer-address "):
			row.PeerAddress = strings.TrimPrefix(line, "peer-address ")
		case strings.HasPrefix(line, "primary-priority "):
			if v, err := strconv.Atoi(strings.TrimPrefix(line, "primary-priority ")); err == nil {
				row.PrimaryPriority = v
			}
		case strings.HasPrefix(line, "dual-primary detection delay "):
			parseMlagDualPrimaryDelay(line, &row)
		case strings.HasPrefix(line, "dual-primary recovery delay "):
			parseMlagRecoveryDelay(line, &row)
		case strings.HasPrefix(line, "reload-delay mlag "):
			if v, err := strconv.Atoi(strings.TrimPrefix(line, "reload-delay mlag ")); err == nil {
				row.ReloadDelayMlag = v
			}
		case strings.HasPrefix(line, "reload-delay non-mlag "):
			if v, err := strconv.Atoi(strings.TrimPrefix(line, "reload-delay non-mlag ")); err == nil {
				row.ReloadDelayNonMlag = v
			}
		case line == "shutdown":
			row.Shutdown = true
		}
	}
	return row, true, nil
}

func parseMlagDualPrimaryDelay(line string, row *mlagRow) {
	rest := strings.TrimPrefix(line, "dual-primary detection delay ")
	parts := strings.Fields(rest)
	if len(parts) == 0 {
		return
	}
	if v, err := strconv.Atoi(parts[0]); err == nil {
		row.DualPrimaryDetectionDelay = v
	}
	if i := indexOf(parts, "action"); i >= 0 && i+1 < len(parts) {
		row.DualPrimaryAction = strings.Join(parts[i+1:], " ")
	}
}

func parseMlagRecoveryDelay(line string, row *mlagRow) {
	rest := strings.TrimPrefix(line, "dual-primary recovery delay ")
	parts := strings.Fields(rest)
	for i := 0; i+1 < len(parts); i++ {
		switch parts[i] {
		case "mlag":
			if v, err := strconv.Atoi(parts[i+1]); err == nil {
				row.DualPrimaryRecoveryDelayMlag = v
				row.hasRecoveryDelay = true
			}
		case "non-mlag":
			if v, err := strconv.Atoi(parts[i+1]); err == nil {
				row.DualPrimaryRecoveryDelayNonMlag = v
				row.hasRecoveryDelay = true
			}
		}
	}
}

func indexOf(items []string, target string) int {
	for i, item := range items {
		if item == target {
			return i
		}
	}
	return -1
}

func (r mlagRow) fillState(s *MlagState) {
	if r.DomainId != "" {
		s.DomainId = r.DomainId
	}
	if r.LocalInterface != "" {
		s.LocalInterface = r.LocalInterface
	}
	if r.PeerLink != "" {
		s.PeerLink = r.PeerLink
	}
	if r.PeerAddress != "" {
		s.PeerAddress = r.PeerAddress
	}
	if r.PeerAddressHeartbeat != "" {
		v := r.PeerAddressHeartbeat
		s.PeerAddressHeartbeat = &v
	}
	if r.PeerAddressHeartbeatVrf != "" {
		v := r.PeerAddressHeartbeatVrf
		s.PeerAddressHeartbeatVrf = &v
	}
	if r.PrimaryPriority > 0 {
		v := r.PrimaryPriority
		s.PrimaryPriority = &v
	}
	if r.DualPrimaryDetectionDelay > 0 {
		v := r.DualPrimaryDetectionDelay
		s.DualPrimaryDetectionDelay = &v
	}
	if r.DualPrimaryAction != "" {
		v := r.DualPrimaryAction
		s.DualPrimaryAction = &v
	}
	if r.hasRecoveryDelay {
		mlagSec := r.DualPrimaryRecoveryDelayMlag
		nonMlagSec := r.DualPrimaryRecoveryDelayNonMlag
		s.DualPrimaryRecoveryDelayMlag = &mlagSec
		s.DualPrimaryRecoveryDelayNonMlag = &nonMlagSec
	}
	if r.ReloadDelayMlag > 0 {
		v := r.ReloadDelayMlag
		s.ReloadDelayMlag = &v
	}
	if r.ReloadDelayNonMlag > 0 {
		v := r.ReloadDelayNonMlag
		s.ReloadDelayNonMlag = &v
	}
	if r.Shutdown {
		v := true
		s.Shutdown = &v
	}
}
