package l3

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/pulumi/pulumi-go-provider/infer"

	"github.com/dantte-lp/pulumi-eos/internal/client/eapi"
	"github.com/dantte-lp/pulumi-eos/internal/config"
)

// Sentinel errors specific to Vrf.
var (
	ErrVrfNameRequired = errors.New("vrf name is required")
	ErrVrfNameReserved = errors.New("vrf name 'default' is reserved")
)

// Vrf models an EOS VRF instance — `vrf instance <name>` plus the global
// per-VRF routing toggles (`ip routing vrf <name>`,
// `ipv6 unicast-routing vrf <name>`).
//
// RD / RT / route-map import-export configuration is intentionally NOT
// modelled here; per the EOS BGP/MPLS L3 VPN TOI those fields live under
// `router bgp <asn> / vrf <name>` and belong to `eos:l3:RouterBgp`.
//
// Source: EOS User Manual §17 — Multi-VRF; TOI 14091 (RFC 4364) for the
// RD/RT split that justifies the resource boundary.
type Vrf struct{}

// VrfArgs is the input set.
type VrfArgs struct {
	// Name is the VRF identifier. Cannot be the reserved word "default".
	Name string `pulumi:"name"`
	// Description sets the VRF description. Optional.
	Description *string `pulumi:"description,optional"`
	// IPRouting toggles `ip routing vrf <name>`. Defaults to true; set
	// false to keep the VRF declared but without an IPv4 RIB.
	IPRouting *bool `pulumi:"ipRouting,optional"`
	// IPv6Routing toggles `ipv6 unicast-routing vrf <name>`. Defaults
	// to false (most fabrics deploy v6 explicitly).
	IPv6Routing *bool `pulumi:"ipv6Routing,optional"`

	// Host overrides the provider-level eosUrl host for this resource.
	Host *string `pulumi:"host,optional"`
	// Username overrides the provider-level eosUsername for this resource.
	Username *string `pulumi:"username,optional"`
	// Password overrides the provider-level eosPassword for this resource.
	Password *string `provider:"secret" pulumi:"password,optional"`
}

// VrfState mirrors Args.
type VrfState struct {
	VrfArgs
}

// Annotate documents the resource.
func (r *Vrf) Annotate(a infer.Annotator) {
	a.Describe(&r, "An EOS VRF instance plus the per-VRF IPv4/IPv6 routing toggles. RD/RT belong to eos:l3:RouterBgp.")
}

// Annotate documents VrfArgs fields.
func (a *VrfArgs) Annotate(an infer.Annotator) {
	an.Describe(&a.Name, "VRF name. The reserved name `default` is rejected.")
	an.Describe(&a.Description, "VRF description.")
	an.Describe(&a.IPRouting, "When true (default), enables `ip routing vrf <name>`.")
	an.Describe(&a.IPv6Routing, "When true, enables `ipv6 unicast-routing vrf <name>`.")
	an.Describe(&a.Host, "Optional management hostname override.")
	an.Describe(&a.Username, "Optional AAA username override.")
	an.Describe(&a.Password, "Optional AAA password override.")
}

// Annotate is a no-op; State has no extra fields.
func (s *VrfState) Annotate(_ infer.Annotator) {}

// Create configures the VRF.
func (*Vrf) Create(ctx context.Context, req infer.CreateRequest[VrfArgs]) (infer.CreateResponse[VrfState], error) {
	if err := validateVrf(req.Inputs); err != nil {
		return infer.CreateResponse[VrfState]{}, err
	}
	state := VrfState{VrfArgs: req.Inputs}
	if req.DryRun {
		return infer.CreateResponse[VrfState]{ID: vrfID(req.Inputs.Name), Output: state}, nil
	}
	if err := applyVrf(ctx, req.Inputs, false); err != nil {
		return infer.CreateResponse[VrfState]{}, fmt.Errorf("create vrf %s: %w", req.Inputs.Name, err)
	}
	return infer.CreateResponse[VrfState]{ID: vrfID(req.Inputs.Name), Output: state}, nil
}

// Read refreshes VRF state from the device.
func (*Vrf) Read(ctx context.Context, req infer.ReadRequest[VrfArgs, VrfState]) (infer.ReadResponse[VrfArgs, VrfState], error) {
	cli, err := newClient(ctx, req.Inputs.Host, req.Inputs.Username, req.Inputs.Password)
	if err != nil {
		return infer.ReadResponse[VrfArgs, VrfState]{}, err
	}
	current, found, err := readVrf(ctx, cli, req.Inputs.Name)
	if err != nil {
		return infer.ReadResponse[VrfArgs, VrfState]{}, err
	}
	if !found {
		return infer.ReadResponse[VrfArgs, VrfState]{}, nil
	}
	state := VrfState{VrfArgs: req.Inputs}
	current.fillState(&state)
	return infer.ReadResponse[VrfArgs, VrfState]{
		ID:     vrfID(req.Inputs.Name),
		Inputs: req.Inputs,
		State:  state,
	}, nil
}

// Update re-applies the VRF configuration.
func (*Vrf) Update(ctx context.Context, req infer.UpdateRequest[VrfArgs, VrfState]) (infer.UpdateResponse[VrfState], error) {
	if err := validateVrf(req.Inputs); err != nil {
		return infer.UpdateResponse[VrfState]{}, err
	}
	state := VrfState{VrfArgs: req.Inputs}
	if req.DryRun {
		return infer.UpdateResponse[VrfState]{Output: state}, nil
	}
	if err := applyVrf(ctx, req.Inputs, false); err != nil {
		return infer.UpdateResponse[VrfState]{}, fmt.Errorf("update vrf %s: %w", req.Inputs.Name, err)
	}
	return infer.UpdateResponse[VrfState]{Output: state}, nil
}

// Delete removes the VRF and its global routing toggles.
func (*Vrf) Delete(ctx context.Context, req infer.DeleteRequest[VrfState]) (infer.DeleteResponse, error) {
	if err := applyVrf(ctx, req.State.VrfArgs, true); err != nil {
		return infer.DeleteResponse{}, fmt.Errorf("delete vrf %s: %w", req.State.Name, err)
	}
	return infer.DeleteResponse{}, nil
}

// --- helpers ---------------------------------------------------------------

func validateVrf(args VrfArgs) error {
	name := strings.TrimSpace(args.Name)
	if name == "" {
		return ErrVrfNameRequired
	}
	if strings.EqualFold(name, "default") {
		return ErrVrfNameReserved
	}
	return nil
}

func vrfID(name string) string { return "vrf/" + name }

func applyVrf(ctx context.Context, args VrfArgs, remove bool) error {
	cli, err := newClient(ctx, args.Host, args.Username, args.Password)
	if err != nil {
		return err
	}
	cfg := config.FromContext(ctx)
	sessName := cfg.SessionPrefix() + "vrf-" + args.Name

	sess, err := cli.OpenSession(ctx, sessName)
	if err != nil {
		return err
	}
	cmds := buildVrfCmds(args, remove)
	if stageErr := sess.Stage(ctx, cmds); stageErr != nil {
		if abortErr := sess.Abort(ctx); abortErr != nil {
			return errors.Join(stageErr, abortErr)
		}
		return stageErr
	}
	return sess.Commit(ctx)
}

// buildVrfCmds renders the staged CLI block.
//
// Default for IPRouting is true: the resource exists to make a routable
// VRF, so the absence of the optional flag implies enable. IPv6 routing
// has the opposite default — most fabrics opt into v6 explicitly.
func buildVrfCmds(args VrfArgs, remove bool) []string {
	if remove {
		// `no vrf instance` cascades; clear the global toggles
		// explicitly to keep `show running-config` clean across EOS
		// minor versions.
		return []string{
			"no ip routing vrf " + args.Name,
			"no ipv6 unicast-routing vrf " + args.Name,
			"no vrf instance " + args.Name,
		}
	}
	cmds := []string{"vrf instance " + args.Name}
	if args.Description != nil && *args.Description != "" {
		cmds = append(cmds, "description "+*args.Description)
	} else {
		cmds = append(cmds, "no description")
	}
	cmds = append(cmds, "exit")

	if args.IPRouting == nil || *args.IPRouting {
		cmds = append(cmds, "ip routing vrf "+args.Name)
	} else {
		cmds = append(cmds, "no ip routing vrf "+args.Name)
	}
	if args.IPv6Routing != nil && *args.IPv6Routing {
		cmds = append(cmds, "ipv6 unicast-routing vrf "+args.Name)
	} else {
		cmds = append(cmds, "no ipv6 unicast-routing vrf "+args.Name)
	}
	return cmds
}

// vrfRow is the parsed live state we care about.
type vrfRow struct {
	Description string
	IPRouting   bool
	IPv6Routing bool
}

// readVrf returns the live VRF state, or (false, nil) when absent.
//
// Source of truth: `show running-config | section vrf instance <name>`
// for the description and `show running-config | include ... vrf <name>`
// for the global routing toggles. JSON forms do not surface description
// reliably across EOS releases.
func readVrf(ctx context.Context, cli *eapi.Client, name string) (vrfRow, bool, error) {
	resp, err := cli.RunCmds(ctx,
		[]string{"show running-config | section vrf instance " + name},
		"text")
	if err != nil {
		return vrfRow{}, false, err
	}
	row := vrfRow{}
	found := false
	if len(resp) > 0 {
		out, _ := resp[0]["output"].(string)
		row, found = parseVrfSection(out, name)
	}
	if !found {
		return vrfRow{}, false, nil
	}
	// Probe global routing toggles separately because the section view
	// only contains the `vrf instance` block.
	if v4, err := vrfRoutingEnabled(ctx, cli, name, false); err == nil {
		row.IPRouting = v4
	}
	if v6, err := vrfRoutingEnabled(ctx, cli, name, true); err == nil {
		row.IPv6Routing = v6
	}
	return row, true, nil
}

// parseVrfSection extracts the description line from a `vrf instance <name>`
// section. Exposed for unit tests.
func parseVrfSection(out, name string) (vrfRow, bool) {
	header := "vrf instance " + name
	if !strings.Contains(out, header) {
		return vrfRow{}, false
	}
	row := vrfRow{}
	for raw := range strings.SplitSeq(out, "\n") {
		line := strings.TrimSpace(raw)
		if v, ok := strings.CutPrefix(line, "description "); ok {
			row.Description = v
		}
	}
	return row, true
}

// vrfRoutingEnabled probes whether the global `(ip|ipv6) routing vrf` knob
// is set. Implemented via `show running-config | include` because the
// running-config section view does not include these globals.
func vrfRoutingEnabled(ctx context.Context, cli *eapi.Client, name string, ipv6 bool) (bool, error) {
	needle := "ip routing vrf " + name
	if ipv6 {
		needle = "ipv6 unicast-routing vrf " + name
	}
	resp, err := cli.RunCmds(ctx,
		[]string{"show running-config | include " + needle},
		"text")
	if err != nil {
		return false, err
	}
	if len(resp) == 0 {
		return false, nil
	}
	out, _ := resp[0]["output"].(string)
	for raw := range strings.SplitSeq(out, "\n") {
		line := strings.TrimSpace(raw)
		// Match the exact line; an EOS response may also include
		// commented-out or `no` forms which we explicitly reject.
		if line == needle {
			return true, nil
		}
	}
	return false, nil
}

func (r vrfRow) fillState(s *VrfState) {
	if r.Description != "" {
		v := r.Description
		s.Description = &v
	}
	v4 := r.IPRouting
	s.IPRouting = &v4
	v6 := r.IPv6Routing
	s.IPv6Routing = &v6
}
