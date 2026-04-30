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

// Sentinel errors specific to VlanRange.
var (
	ErrVlanRangeStartRange = errors.New("vlanRange start must be in 1..4094")
	ErrVlanRangeEndRange   = errors.New("vlanRange end must be in 1..4094")
	ErrVlanRangeStartGtEnd = errors.New("vlanRange start must be <= end")
)

// VlanRange models a contiguous block of VLANs created via the EOS bulk
// `vlan <start>-<end>` command. The optional Name applies to every VLAN
// in the block — useful for service-chain pools where every VLAN shares
// a descriptive role label.
//
// Source: EOS User Manual §11 (`vlan <range>` enters a multi-VLAN sub-
// mode in which subsequent `name <text>` and `state active|suspend`
// commands apply to every VLAN in the range).
type VlanRange struct{}

// VlanRangeArgs is the input set.
type VlanRangeArgs struct {
	// Start is the lowest VLAN id in the range (1..4094).
	Start int `pulumi:"start"`
	// End is the highest VLAN id in the range (1..4094, >= Start).
	End int `pulumi:"end"`
	// Name is the shared description applied to every VLAN in the
	// block. Optional.
	Name *string `pulumi:"name,optional"`

	// Host overrides the provider-level eosUrl host for this resource.
	Host *string `pulumi:"host,optional"`
	// Username overrides the provider-level eosUsername for this resource.
	Username *string `pulumi:"username,optional"`
	// Password overrides the provider-level eosPassword for this resource.
	Password *string `provider:"secret" pulumi:"password,optional"`
}

// VlanRangeState mirrors Args.
type VlanRangeState struct {
	VlanRangeArgs
}

// Annotate documents the resource.
func (r *VlanRange) Annotate(a infer.Annotator) {
	a.Describe(&r, "Bulk EOS VLAN allocation. Creates a contiguous `vlan <start>-<end>` block; the optional name applies to every VLAN in the range.")
}

// Annotate documents VlanRangeArgs fields.
func (a *VlanRangeArgs) Annotate(an infer.Annotator) {
	an.Describe(&a.Start, "Lowest VLAN id in the range (1..4094).")
	an.Describe(&a.End, "Highest VLAN id in the range (1..4094, >= start).")
	an.Describe(&a.Name, "Shared description applied to every VLAN in the block.")
	an.Describe(&a.Host, "Optional management hostname override.")
	an.Describe(&a.Username, "Optional AAA username override.")
	an.Describe(&a.Password, "Optional AAA password override.")
}

// Annotate is a no-op; State has no extra fields.
func (s *VlanRangeState) Annotate(_ infer.Annotator) {}

// Create configures the VLAN block.
func (*VlanRange) Create(ctx context.Context, req infer.CreateRequest[VlanRangeArgs]) (infer.CreateResponse[VlanRangeState], error) {
	if err := validateVlanRange(req.Inputs); err != nil {
		return infer.CreateResponse[VlanRangeState]{}, err
	}
	state := VlanRangeState{VlanRangeArgs: req.Inputs}
	if req.DryRun {
		return infer.CreateResponse[VlanRangeState]{ID: vlanRangeID(req.Inputs.Start, req.Inputs.End), Output: state}, nil
	}
	if err := applyVlanRange(ctx, req.Inputs, false); err != nil {
		return infer.CreateResponse[VlanRangeState]{}, fmt.Errorf("create vlan-range %d-%d: %w", req.Inputs.Start, req.Inputs.End, err)
	}
	return infer.CreateResponse[VlanRangeState]{ID: vlanRangeID(req.Inputs.Start, req.Inputs.End), Output: state}, nil
}

// Read verifies the VLAN block still exists by sampling the lowest and
// highest VLAN ids in the requested range.
//
// EOS does not expose a `show vlan range` view; we therefore probe the
// block boundaries via `show vlan` JSON. Both endpoints must be present
// for the resource to count as `found`.
func (*VlanRange) Read(ctx context.Context, req infer.ReadRequest[VlanRangeArgs, VlanRangeState]) (infer.ReadResponse[VlanRangeArgs, VlanRangeState], error) {
	cli, err := newClient(ctx, req.Inputs.Host, req.Inputs.Username, req.Inputs.Password)
	if err != nil {
		return infer.ReadResponse[VlanRangeArgs, VlanRangeState]{}, err
	}
	startName, foundStart, err := readVlanForRange(ctx, cli, req.Inputs.Start)
	if err != nil {
		return infer.ReadResponse[VlanRangeArgs, VlanRangeState]{}, err
	}
	_, foundEnd, err := readVlanForRange(ctx, cli, req.Inputs.End)
	if err != nil {
		return infer.ReadResponse[VlanRangeArgs, VlanRangeState]{}, err
	}
	if !foundStart || !foundEnd {
		return infer.ReadResponse[VlanRangeArgs, VlanRangeState]{}, nil
	}
	state := VlanRangeState{VlanRangeArgs: req.Inputs}
	if startName != "" && !strings.HasPrefix(startName, "VLAN") {
		// EOS auto-derives `VLAN<id>` when no `name` is configured; we
		// only echo back a user-set name.
		v := startName
		state.Name = &v
	}
	return infer.ReadResponse[VlanRangeArgs, VlanRangeState]{
		ID:     vlanRangeID(req.Inputs.Start, req.Inputs.End),
		Inputs: req.Inputs,
		State:  state,
	}, nil
}

// Update re-applies the block configuration.
func (*VlanRange) Update(ctx context.Context, req infer.UpdateRequest[VlanRangeArgs, VlanRangeState]) (infer.UpdateResponse[VlanRangeState], error) {
	if err := validateVlanRange(req.Inputs); err != nil {
		return infer.UpdateResponse[VlanRangeState]{}, err
	}
	state := VlanRangeState{VlanRangeArgs: req.Inputs}
	if req.DryRun {
		return infer.UpdateResponse[VlanRangeState]{Output: state}, nil
	}
	if err := applyVlanRange(ctx, req.Inputs, false); err != nil {
		return infer.UpdateResponse[VlanRangeState]{}, fmt.Errorf("update vlan-range %d-%d: %w", req.Inputs.Start, req.Inputs.End, err)
	}
	return infer.UpdateResponse[VlanRangeState]{Output: state}, nil
}

// Delete removes the VLAN block.
func (*VlanRange) Delete(ctx context.Context, req infer.DeleteRequest[VlanRangeState]) (infer.DeleteResponse, error) {
	if err := applyVlanRange(ctx, req.State.VlanRangeArgs, true); err != nil {
		return infer.DeleteResponse{}, fmt.Errorf("delete vlan-range %d-%d: %w", req.State.Start, req.State.End, err)
	}
	return infer.DeleteResponse{}, nil
}

// validateVlanRange enforces id ranges and start ≤ end.
func validateVlanRange(args VlanRangeArgs) error {
	if args.Start < 1 || args.Start > 4094 {
		return fmt.Errorf("%w (got %d)", ErrVlanRangeStartRange, args.Start)
	}
	if args.End < 1 || args.End > 4094 {
		return fmt.Errorf("%w (got %d)", ErrVlanRangeEndRange, args.End)
	}
	if args.Start > args.End {
		return fmt.Errorf("%w (got %d > %d)", ErrVlanRangeStartGtEnd, args.Start, args.End)
	}
	return nil
}

func vlanRangeID(start, end int) string {
	return "vlan-range/" + strconv.Itoa(start) + "-" + strconv.Itoa(end)
}

func applyVlanRange(ctx context.Context, args VlanRangeArgs, remove bool) error {
	cli, err := newClient(ctx, args.Host, args.Username, args.Password)
	if err != nil {
		return err
	}
	cfg := config.FromContext(ctx)
	sessName := cfg.SessionPrefix() + "vlanrange-" + strconv.Itoa(args.Start) + "-" + strconv.Itoa(args.End)

	sess, err := cli.OpenSession(ctx, sessName)
	if err != nil {
		return err
	}
	cmds := buildVlanRangeCmds(args, remove)
	if stageErr := sess.Stage(ctx, cmds); stageErr != nil {
		if abortErr := sess.Abort(ctx); abortErr != nil {
			return errors.Join(stageErr, abortErr)
		}
		return stageErr
	}
	return sess.Commit(ctx)
}

// buildVlanRangeCmds renders the bulk-allocation command list.
func buildVlanRangeCmds(args VlanRangeArgs, remove bool) []string {
	rng := strconv.Itoa(args.Start) + "-" + strconv.Itoa(args.End)
	if remove {
		return []string{"no vlan " + rng}
	}
	cmds := []string{"vlan " + rng}
	if args.Name != nil && *args.Name != "" {
		cmds = append(cmds, "name "+*args.Name)
	}
	return cmds
}

// readVlanForRange returns the configured name for a single VLAN id and
// whether the VLAN is present, by querying `show vlan` in JSON form.
//
// EOS exposes the result as `{"vlans": {"<id>": {"name": "..."}}}`.
// `show vlan id <n>` returns an empty `vlans` map (and exits non-zero on
// some EOS trains) when the id is absent — we therefore probe via the
// unfiltered `show vlan` instead.
func readVlanForRange(ctx context.Context, cli *eapi.Client, id int) (string, bool, error) {
	resp, err := cli.RunCmds(ctx, []string{"show vlan"}, "json")
	if err != nil {
		return "", false, err
	}
	if len(resp) == 0 {
		return "", false, nil
	}
	vlans, ok := resp[0]["vlans"].(map[string]any)
	if !ok {
		return "", false, nil
	}
	entry, ok := vlans[strconv.Itoa(id)].(map[string]any)
	if !ok {
		return "", false, nil
	}
	if name, ok := entry["name"].(string); ok {
		return name, true, nil
	}
	return "", true, nil
}
