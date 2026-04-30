package l2

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/pulumi/pulumi-go-provider/infer"

	"github.com/dantte-lp/pulumi-eos/internal/client/eapi"
	"github.com/dantte-lp/pulumi-eos/internal/config"
)

// Spanning-tree modes accepted by `eos:l2:Stp`.
//
// Source: EOS User Manual §13.1.2 (`spanning-tree mode …`).
const (
	StpModeMstp      = "mstp"
	StpModeRstp      = "rstp"
	StpModeRapidPvst = "rapid-pvst"
	StpModeNone      = "none"
)

// MstInstanceMin / Max bound the MSTP instance identifier (1..4094 per
// IEEE 802.1Q § Multiple Spanning Tree). EOS rejects values outside that
// range.
const (
	MstInstanceMin = 1
	MstInstanceMax = 4094
)

// MstRevisionMin / Max bound the MST configuration revision number per
// IEEE 802.1Q-2014 §13.7 (16-bit unsigned).
const (
	MstRevisionMin = 0
	MstRevisionMax = 65535
)

// Sentinel errors specific to Stp.
var (
	ErrStpModeInvalid          = errors.New("stp mode must be mstp, rstp, rapid-pvst, or none")
	ErrStpMstInstanceRange     = errors.New("mst instance id must be 1..4094")
	ErrStpMstInstanceVlanEmpty = errors.New("mst instance vlanRange is required")
	ErrStpMstInstanceDup       = errors.New("mst configuration contains duplicate instance id")
	ErrStpMstRevisionRange     = errors.New("mst revision must be 0..65535")
)

// Stp models the singleton global spanning-tree configuration block. EOS
// allows exactly one STP configuration per device.
//
// Sources (verified via arista-mcp):
//   - EOS User Manual §13.1.2 — `spanning-tree mode mstp | rstp |
//     rapid-pvst | none`. Mode `none` disables STP entirely.
//   - EOS User Manual §13.1.3.4.3 / §13.1.4.32 — BPDU Guard semantics;
//     `spanning-tree edge-port bpduguard default` enables BPDU Guard on
//     all operational portfast ports.
//   - IEEE 802.1Q-2014 §13.7 — MST instance id 1..4094, revision
//     0..65535.
type Stp struct{}

// MstInstance binds a list of VLANs to a single MSTP instance.
type MstInstance struct {
	// Id is the MSTP instance identifier (1..4094).
	Id int `pulumi:"id"`
	// VlanRange is the EOS VLAN range syntax bound to this instance
	// (e.g. `100-199`, `200,300-400`).
	VlanRange string `pulumi:"vlanRange"`
}

// MstConfiguration is the `spanning-tree mst configuration` sub-block.
type MstConfiguration struct {
	// Name is the MSTP region name. Members of one region must share
	// Name + Revision + the full instance map.
	Name *string `pulumi:"name,optional"`
	// Revision is the MSTP configuration revision number (0..65535).
	Revision *int `pulumi:"revision,optional"`
	// Instances is the explicit instance → VLAN-range map. Order is
	// significant only for diff stability — EOS sorts by id on read.
	Instances []MstInstance `pulumi:"instances,optional"`
}

// StpArgs is the input set.
type StpArgs struct {
	// Mode is `mstp` (default), `rstp`, `rapid-pvst`, or `none`.
	Mode *string `pulumi:"mode,optional"`
	// EdgePortBpduGuardDefault toggles
	// `spanning-tree edge-port bpduguard default` — BPDU Guard on
	// every operational portfast port.
	EdgePortBpduGuardDefault *bool `pulumi:"edgePortBpduGuardDefault,optional"`
	// Mst is the optional MSTP region configuration. Required for
	// inter-vendor MSTP interop; safely omitted when running RSTP /
	// rapid-PVST.
	Mst *MstConfiguration `pulumi:"mst,optional"`

	// Host overrides the provider-level eosUrl host for this resource.
	Host *string `pulumi:"host,optional"`
	// Username overrides the provider-level eosUsername for this resource.
	Username *string `pulumi:"username,optional"`
	// Password overrides the provider-level eosPassword for this resource.
	Password *string `provider:"secret" pulumi:"password,optional"`
}

// StpState mirrors Args.
type StpState struct {
	StpArgs
}

// Annotate documents the resource.
func (r *Stp) Annotate(a infer.Annotator) {
	a.Describe(&r, "EOS global spanning-tree configuration: mode, edge-port BPDU Guard default, and optional MSTP region. Singleton per device.")
}

// Annotate documents StpArgs fields.
func (a *StpArgs) Annotate(an infer.Annotator) {
	an.Describe(&a.Mode, "`mstp` (default), `rstp`, `rapid-pvst`, or `none`. `none` disables STP entirely.")
	an.Describe(&a.EdgePortBpduGuardDefault, "When true, BPDU Guard is enabled on every operational portfast port (`spanning-tree edge-port bpduguard default`).")
	an.Describe(&a.Mst, "Optional MSTP region configuration (required for inter-vendor MSTP interop).")
	an.Describe(&a.Host, "Optional management hostname override.")
	an.Describe(&a.Username, "Optional AAA username override.")
	an.Describe(&a.Password, "Optional AAA password override.")
}

// Annotate is a no-op; State has no extra fields.
func (s *StpState) Annotate(_ infer.Annotator) {}

// Annotate documents MstConfiguration fields.
func (m *MstConfiguration) Annotate(an infer.Annotator) {
	an.Describe(&m.Name, "MSTP region name.")
	an.Describe(&m.Revision, "MSTP configuration revision number (0..65535).")
	an.Describe(&m.Instances, "Explicit MSTP instance → VLAN-range map.")
}

// Annotate documents MstInstance fields.
func (m *MstInstance) Annotate(an infer.Annotator) {
	an.Describe(&m.Id, "MSTP instance identifier (1..4094).")
	an.Describe(&m.VlanRange, "VLAN range bound to this instance (e.g. `100-199`).")
}

// Create configures the global STP block.
func (*Stp) Create(ctx context.Context, req infer.CreateRequest[StpArgs]) (infer.CreateResponse[StpState], error) {
	if err := validateStp(req.Inputs); err != nil {
		return infer.CreateResponse[StpState]{}, err
	}
	state := StpState{StpArgs: req.Inputs}
	if req.DryRun {
		return infer.CreateResponse[StpState]{ID: stpID(), Output: state}, nil
	}
	if err := applyStp(ctx, req.Inputs, false); err != nil {
		return infer.CreateResponse[StpState]{}, fmt.Errorf("create stp: %w", err)
	}
	return infer.CreateResponse[StpState]{ID: stpID(), Output: state}, nil
}

// Read refreshes the STP block from the device.
func (*Stp) Read(ctx context.Context, req infer.ReadRequest[StpArgs, StpState]) (infer.ReadResponse[StpArgs, StpState], error) {
	cli, err := newClient(ctx, req.Inputs.Host, req.Inputs.Username, req.Inputs.Password)
	if err != nil {
		return infer.ReadResponse[StpArgs, StpState]{}, err
	}
	row, found, err := readStp(ctx, cli)
	if err != nil {
		return infer.ReadResponse[StpArgs, StpState]{}, err
	}
	if !found {
		return infer.ReadResponse[StpArgs, StpState]{}, nil
	}
	state := StpState{StpArgs: req.Inputs}
	row.fillState(&state)
	return infer.ReadResponse[StpArgs, StpState]{
		ID:     stpID(),
		Inputs: req.Inputs,
		State:  state,
	}, nil
}

// Update re-applies the STP block. The MST sub-block is fully cleared
// (`no spanning-tree mst configuration`) before re-rendering so stale
// `instance N vlan …` rows from a prior apply are guaranteed gone.
func (*Stp) Update(ctx context.Context, req infer.UpdateRequest[StpArgs, StpState]) (infer.UpdateResponse[StpState], error) {
	if err := validateStp(req.Inputs); err != nil {
		return infer.UpdateResponse[StpState]{}, err
	}
	state := StpState{StpArgs: req.Inputs}
	if req.DryRun {
		return infer.UpdateResponse[StpState]{Output: state}, nil
	}
	if err := applyStp(ctx, req.Inputs, false); err != nil {
		return infer.UpdateResponse[StpState]{}, fmt.Errorf("update stp: %w", err)
	}
	return infer.UpdateResponse[StpState]{Output: state}, nil
}

// Delete reverts STP to EOS defaults: mode mstp, no edge-port bpduguard
// default, no mst configuration. EOS does not support a literal
// `no spanning-tree` global toggle — instead we negate each individual
// knob the resource owned.
func (*Stp) Delete(ctx context.Context, req infer.DeleteRequest[StpState]) (infer.DeleteResponse, error) {
	if err := applyStp(ctx, req.State.StpArgs, true); err != nil {
		return infer.DeleteResponse{}, fmt.Errorf("delete stp: %w", err)
	}
	return infer.DeleteResponse{}, nil
}

// validateStp enforces enum and range constraints.
func validateStp(args StpArgs) error {
	if args.Mode != nil {
		switch *args.Mode {
		case "", StpModeMstp, StpModeRstp, StpModeRapidPvst, StpModeNone:
		default:
			return fmt.Errorf("%w (got %q)", ErrStpModeInvalid, *args.Mode)
		}
	}
	if args.Mst != nil {
		if err := validateMstConfiguration(*args.Mst); err != nil {
			return err
		}
	}
	return nil
}

func validateMstConfiguration(m MstConfiguration) error {
	if m.Revision != nil && (*m.Revision < MstRevisionMin || *m.Revision > MstRevisionMax) {
		return fmt.Errorf("%w (got %d)", ErrStpMstRevisionRange, *m.Revision)
	}
	seen := map[int]struct{}{}
	for _, e := range m.Instances {
		if e.Id < MstInstanceMin || e.Id > MstInstanceMax {
			return fmt.Errorf("%w (got %d)", ErrStpMstInstanceRange, e.Id)
		}
		if strings.TrimSpace(e.VlanRange) == "" {
			return fmt.Errorf("%w (instance %d)", ErrStpMstInstanceVlanEmpty, e.Id)
		}
		if _, dup := seen[e.Id]; dup {
			return fmt.Errorf("%w (id=%d)", ErrStpMstInstanceDup, e.Id)
		}
		seen[e.Id] = struct{}{}
	}
	return nil
}

func stpID() string { return "stp" }

func applyStp(ctx context.Context, args StpArgs, reset bool) error {
	cli, err := newClient(ctx, args.Host, args.Username, args.Password)
	if err != nil {
		return err
	}
	cfg := config.FromContext(ctx)
	sessName := cfg.SessionPrefix() + "stp"

	sess, err := cli.OpenSession(ctx, sessName)
	if err != nil {
		return err
	}
	cmds := buildStpCmds(args, reset)
	if stageErr := sess.Stage(ctx, cmds); stageErr != nil {
		if abortErr := sess.Abort(ctx); abortErr != nil {
			return errors.Join(stageErr, abortErr)
		}
		return stageErr
	}
	return sess.Commit(ctx)
}

// buildStpCmds renders the ordered command list.
//
// On reset (Delete) we revert each knob the resource owned:
//   - `default spanning-tree mode` (returns to EOS default mstp).
//   - `no spanning-tree edge-port bpduguard default`.
//   - `no spanning-tree mst configuration`.
//
// On apply (Create / Update) we always negate the MST sub-block first so
// stale `instance N vlan …` rows are guaranteed gone — same idempotency
// pattern used by `eos:l2:Mlag` and `eos:l2:EvpnEthernetSegment`.
func buildStpCmds(args StpArgs, reset bool) []string {
	if reset {
		return []string{
			"default spanning-tree mode",
			"no spanning-tree edge-port bpduguard default",
			"no spanning-tree mst configuration",
		}
	}

	var cmds []string
	if args.Mode != nil && *args.Mode != "" {
		cmds = append(cmds, "spanning-tree mode "+*args.Mode)
	}
	if args.EdgePortBpduGuardDefault != nil {
		if *args.EdgePortBpduGuardDefault {
			cmds = append(cmds, "spanning-tree edge-port bpduguard default")
		} else {
			cmds = append(cmds, "no spanning-tree edge-port bpduguard default")
		}
	}
	if args.Mst != nil {
		cmds = append(cmds, "no spanning-tree mst configuration", "spanning-tree mst configuration")
		if args.Mst.Name != nil && *args.Mst.Name != "" {
			cmds = append(cmds, "name "+*args.Mst.Name)
		}
		if args.Mst.Revision != nil {
			cmds = append(cmds, "revision "+strconv.Itoa(*args.Mst.Revision))
		}
		instances := append([]MstInstance(nil), args.Mst.Instances...)
		sort.Slice(instances, func(i, j int) bool { return instances[i].Id < instances[j].Id })
		for _, e := range instances {
			cmds = append(cmds, "instance "+strconv.Itoa(e.Id)+" vlan "+e.VlanRange)
		}
		cmds = append(cmds, "exit")
	}
	return cmds
}

// stpRow holds the parsed live state of `show running-config | section
// spanning-tree`.
type stpRow struct {
	Mode                     string
	EdgePortBpduGuardDefault *bool
	Mst                      *MstConfiguration
	hasMst                   bool
}

// readStp returns the live STP block, or (false, nil) when running-config
// has no spanning-tree directives at all (EOS default has at minimum a
// `spanning-tree mode mstp` line, so the false case is rare).
func readStp(ctx context.Context, cli *eapi.Client) (stpRow, bool, error) {
	resp, err := cli.RunCmds(ctx,
		[]string{"show running-config | section spanning-tree"},
		"text")
	if err != nil {
		return stpRow{}, false, err
	}
	if len(resp) == 0 {
		return stpRow{}, false, nil
	}
	out, _ := resp[0]["output"].(string)
	if out == "" {
		return stpRow{}, false, nil
	}
	return parseStpConfig(out)
}

// parseStpConfig is exposed for unit tests.
func parseStpConfig(out string) (stpRow, bool, error) {
	if !strings.Contains(out, "spanning-tree") {
		return stpRow{}, false, nil
	}
	row := stpRow{}
	inMst := false
	mst := MstConfiguration{}
	for raw := range strings.SplitSeq(out, "\n") {
		line := strings.TrimSpace(raw)
		switch line {
		case "spanning-tree mst configuration":
			inMst = true
			row.hasMst = true
			continue
		case "exit":
			inMst = false
			continue
		}
		if inMst {
			parseStpMstLine(line, &mst)
			continue
		}
		if mode, ok := strings.CutPrefix(line, "spanning-tree mode "); ok {
			row.Mode = mode
			continue
		}
		switch line {
		case "spanning-tree edge-port bpduguard default":
			t := true
			row.EdgePortBpduGuardDefault = &t
		case "no spanning-tree edge-port bpduguard default":
			f := false
			row.EdgePortBpduGuardDefault = &f
		}
	}
	if row.hasMst {
		row.Mst = &mst
	}
	return row, true, nil
}

// parseStpMstLine consumes one trimmed line from inside the
// `spanning-tree mst configuration` block.
func parseStpMstLine(line string, mst *MstConfiguration) {
	switch {
	case strings.HasPrefix(line, "name "):
		v := strings.TrimPrefix(line, "name ")
		mst.Name = &v
	case strings.HasPrefix(line, "revision "):
		if v, err := strconv.Atoi(strings.TrimPrefix(line, "revision ")); err == nil {
			mst.Revision = &v
		}
	case strings.HasPrefix(line, "instance "):
		// `instance <id> vlan <range>`
		parts := strings.Fields(line)
		if len(parts) >= 4 && parts[2] == "vlan" {
			if id, err := strconv.Atoi(parts[1]); err == nil {
				mst.Instances = append(mst.Instances, MstInstance{
					Id:        id,
					VlanRange: strings.Join(parts[3:], " "),
				})
			}
		}
	}
}

func (r stpRow) fillState(s *StpState) {
	if r.Mode != "" {
		v := r.Mode
		s.Mode = &v
	}
	if r.EdgePortBpduGuardDefault != nil {
		b := *r.EdgePortBpduGuardDefault
		s.EdgePortBpduGuardDefault = &b
	}
	if r.Mst != nil {
		s.Mst = r.Mst
	}
}
