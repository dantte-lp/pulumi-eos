package l2

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/pulumi/pulumi-go-provider/infer"

	"github.com/dantte-lp/pulumi-eos/internal/client/eapi"
	"github.com/dantte-lp/pulumi-eos/internal/config"
)

// Sentinel errors specific to Varp.
var (
	ErrVarpMacRequired  = errors.New("varp macAddress is required")
	ErrVarpMacBadFormat = errors.New("varp macAddress must be a valid MAC (e.g. 00:1c:73:00:00:01 or 001c.7300.0001)")
	ErrVarpMacMulticast = errors.New("varp macAddress must be unicast (lowest bit of first octet must be 0)")
)

// (regex no longer needed — net.ParseMAC handles all input forms.)

// Varp models the global VARP / anycast-gateway virtual-MAC binding.
// One per device; applies to every `ip address virtual` / `ipv6 address
// virtual` SVI on the switch.
//
// Sources (verified via arista-mcp):
//   - EOS User Manual §14.5.1.3 — VARP overview; the global virtual MAC
//     receives traffic for any virtual IP configured on any SVI; never
//     used as a source MAC.
//   - EOS TOI 14374 — `ip virtual-router mac-address <mac>` global
//     command and its semantics for `ip address virtual`.
//   - EOS TOI 15448 — IPv4 + IPv6 overlay anycast gateways share the
//     same virtual MAC.
type Varp struct{}

// VarpArgs is the input set.
type VarpArgs struct {
	// MacAddress is the global virtual MAC. Accepts EOS Cisco-style
	// (`001c.7300.0001`), colon-separated (`00:1c:73:00:00:01`), or
	// hyphen-separated (`00-1c-73-00-00-01`) input — normalised to
	// EOS Cisco-style on the wire.
	MacAddress string `pulumi:"macAddress"`

	// Host overrides the provider-level eosUrl host for this resource.
	Host *string `pulumi:"host,optional"`
	// Username overrides the provider-level eosUsername for this resource.
	Username *string `pulumi:"username,optional"`
	// Password overrides the provider-level eosPassword for this resource.
	Password *string `provider:"secret" pulumi:"password,optional"`
}

// VarpState mirrors Args; the device-side MAC is the canonical EOS form.
type VarpState struct {
	VarpArgs
}

// Annotate documents the resource.
func (r *Varp) Annotate(a infer.Annotator) {
	a.Describe(&r, "Global VARP (Virtual ARP) anycast-gateway MAC binding. Singleton per device; applies to every `ip address virtual` / `ipv6 address virtual` SVI on the switch.")
}

// Annotate documents VarpArgs fields.
func (a *VarpArgs) Annotate(an infer.Annotator) {
	an.Describe(&a.MacAddress, "Global virtual MAC. Accepts Cisco-style `HHHH.HHHH.HHHH`, colon-separated, or hyphen-separated MAC; normalised to Cisco-style on the wire. Must be unicast.")
	an.Describe(&a.Host, "Optional management hostname override.")
	an.Describe(&a.Username, "Optional AAA username override.")
	an.Describe(&a.Password, "Optional AAA password override.")
}

// Annotate is a no-op; State has no extra fields.
func (s *VarpState) Annotate(_ infer.Annotator) {}

// Create configures the global VARP MAC.
func (*Varp) Create(ctx context.Context, req infer.CreateRequest[VarpArgs]) (infer.CreateResponse[VarpState], error) {
	if err := validateVarp(req.Inputs); err != nil {
		return infer.CreateResponse[VarpState]{}, err
	}
	state := VarpState{VarpArgs: req.Inputs}
	if req.DryRun {
		return infer.CreateResponse[VarpState]{ID: varpID(), Output: state}, nil
	}
	if err := applyVarp(ctx, req.Inputs, false); err != nil {
		return infer.CreateResponse[VarpState]{}, fmt.Errorf("create varp: %w", err)
	}
	return infer.CreateResponse[VarpState]{ID: varpID(), Output: state}, nil
}

// Read refreshes the VARP MAC from the device.
func (*Varp) Read(ctx context.Context, req infer.ReadRequest[VarpArgs, VarpState]) (infer.ReadResponse[VarpArgs, VarpState], error) {
	cli, err := newClient(ctx, req.Inputs.Host, req.Inputs.Username, req.Inputs.Password)
	if err != nil {
		return infer.ReadResponse[VarpArgs, VarpState]{}, err
	}
	mac, found, err := readVarp(ctx, cli)
	if err != nil {
		return infer.ReadResponse[VarpArgs, VarpState]{}, err
	}
	if !found {
		return infer.ReadResponse[VarpArgs, VarpState]{}, nil
	}
	state := VarpState{VarpArgs: req.Inputs}
	state.MacAddress = mac
	return infer.ReadResponse[VarpArgs, VarpState]{
		ID:     varpID(),
		Inputs: req.Inputs,
		State:  state,
	}, nil
}

// Update re-applies the VARP MAC. EOS replaces the previous binding
// when a new `ip virtual-router mac-address <mac>` is committed.
func (*Varp) Update(ctx context.Context, req infer.UpdateRequest[VarpArgs, VarpState]) (infer.UpdateResponse[VarpState], error) {
	if err := validateVarp(req.Inputs); err != nil {
		return infer.UpdateResponse[VarpState]{}, err
	}
	state := VarpState{VarpArgs: req.Inputs}
	if req.DryRun {
		return infer.UpdateResponse[VarpState]{Output: state}, nil
	}
	if err := applyVarp(ctx, req.Inputs, false); err != nil {
		return infer.UpdateResponse[VarpState]{}, fmt.Errorf("update varp: %w", err)
	}
	return infer.UpdateResponse[VarpState]{Output: state}, nil
}

// Delete removes the global virtual MAC.
func (*Varp) Delete(ctx context.Context, req infer.DeleteRequest[VarpState]) (infer.DeleteResponse, error) {
	if err := applyVarp(ctx, req.State.VarpArgs, true); err != nil {
		return infer.DeleteResponse{}, fmt.Errorf("delete varp: %w", err)
	}
	return infer.DeleteResponse{}, nil
}

// validateVarp parses + format-checks the MAC.
func validateVarp(args VarpArgs) error {
	if strings.TrimSpace(args.MacAddress) == "" {
		return ErrVarpMacRequired
	}
	mac, err := net.ParseMAC(strings.TrimSpace(args.MacAddress))
	if err != nil {
		return fmt.Errorf("%w (got %q: %w)", ErrVarpMacBadFormat, args.MacAddress, err)
	}
	// IEEE 802 unicast: lowest bit of the first octet is the I/G bit;
	// 0 = unicast, 1 = multicast. VARP requires unicast.
	if mac[0]&0x01 != 0 {
		return fmt.Errorf("%w (got %q)", ErrVarpMacMulticast, mac)
	}
	return nil
}

// normalizeMac returns the canonical EOS wire form of a validated MAC.
//
// EOS 4.36 reports `ip virtual-router mac-address` under
// `show running-config` as colon-separated lowercase
// (`00:1c:73:00:00:01`); we therefore emit the same form for diff
// stability so a re-applied state is a no-op diff.
func normalizeMac(s string) (string, error) {
	mac, err := net.ParseMAC(s)
	if err != nil {
		return "", err
	}
	return mac.String(), nil
}

func varpID() string { return "varp" }

func applyVarp(ctx context.Context, args VarpArgs, remove bool) error {
	cli, err := newClient(ctx, args.Host, args.Username, args.Password)
	if err != nil {
		return err
	}
	cfg := config.FromContext(ctx)
	sessName := cfg.SessionPrefix() + "varp"

	sess, err := cli.OpenSession(ctx, sessName)
	if err != nil {
		return err
	}
	cmds, err := buildVarpCmds(args, remove)
	if err != nil {
		if abortErr := sess.Abort(ctx); abortErr != nil {
			return errors.Join(err, abortErr)
		}
		return err
	}
	if stageErr := sess.Stage(ctx, cmds); stageErr != nil {
		if abortErr := sess.Abort(ctx); abortErr != nil {
			return errors.Join(stageErr, abortErr)
		}
		return stageErr
	}
	return sess.Commit(ctx)
}

// buildVarpCmds renders the VARP directive list. EOS requires the MAC on
// both sides — `no ip virtual-router mac-address <mac>` is the canonical
// removal form; the bare `no ip virtual-router mac-address` raises
// `invalid command`. We therefore always render the canonical EOS MAC
// (colon-separated lowercase) on both apply and reset for diff stability
// and a deterministic delete path.
func buildVarpCmds(args VarpArgs, remove bool) ([]string, error) {
	mac, err := normalizeMac(strings.TrimSpace(args.MacAddress))
	if err != nil {
		return nil, fmt.Errorf("%w (got %q: %w)", ErrVarpMacBadFormat, args.MacAddress, err)
	}
	if remove {
		return []string{"no ip virtual-router mac-address " + mac}, nil
	}
	return []string{"ip virtual-router mac-address " + mac}, nil
}

// readVarp returns the configured global virtual MAC, or (false, nil)
// when no `ip virtual-router mac-address` directive is present.
func readVarp(ctx context.Context, cli *eapi.Client) (string, bool, error) {
	resp, err := cli.RunCmds(ctx,
		[]string{"show running-config | include ^ip virtual-router mac-address"},
		"text")
	if err != nil {
		return "", false, err
	}
	if len(resp) == 0 {
		return "", false, nil
	}
	out, _ := resp[0]["output"].(string)
	return parseVarpConfig(out)
}

// parseVarpConfig is exposed for unit tests.
func parseVarpConfig(out string) (string, bool, error) {
	for raw := range strings.SplitSeq(out, "\n") {
		line := strings.TrimSpace(raw)
		if mac, ok := strings.CutPrefix(line, "ip virtual-router mac-address "); ok {
			return strings.ToLower(mac), true, nil
		}
	}
	return "", false, nil
}
