// Package l2 contains Pulumi resources whose token starts with `eos:l2:`.
package l2

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/pulumi/pulumi-go-provider/infer"

	"github.com/dantte-lp/pulumi-eos/internal/client/eapi"
	"github.com/dantte-lp/pulumi-eos/internal/config"
)

// Sentinel errors returned by package l2.
var (
	ErrVlanIDOutOfRange = errors.New("vlan id must be in 1..4094")
)

// Vlan models an EOS VLAN. CRUD goes through eAPI config sessions with
// confirmed-commit semantics inherited from the eapi.Session helper.
type Vlan struct{}

// VlanArgs is the input set.
type VlanArgs struct {
	// Id is the VLAN identifier (1–4094).
	Id int `pulumi:"id"`
	// Name is the human-readable VLAN name. Optional.
	Name *string `pulumi:"name,optional"`
	// Host overrides the provider-level eosUrl host for this resource.
	Host *string `pulumi:"host,optional"`
	// Username overrides the provider-level eosUsername for this resource.
	Username *string `pulumi:"username,optional"`
	// Password overrides the provider-level eosPassword for this resource.
	Password *string `provider:"secret" pulumi:"password,optional"`
}

// VlanState mirrors Args; VLAN has no derived outputs at v0.1.0.
type VlanState struct {
	VlanArgs
}

// Annotate registers the resource description and per-field documentation.
func (r *Vlan) Annotate(a infer.Annotator) {
	a.Describe(&r, "An EOS VLAN. Created and updated through atomic configuration sessions over eAPI.")
}

// Annotate documents VlanArgs fields.
func (a *VlanArgs) Annotate(an infer.Annotator) {
	an.Describe(&a.Id, "VLAN identifier (1..4094).")
	an.Describe(&a.Name, "VLAN name. Defaults to EOS-derived (`VLAN<id>`) when unset.")
	an.Describe(&a.Host, "Optional management hostname override. Falls back to provider eosUrl host.")
	an.Describe(&a.Username, "Optional AAA username override.")
	an.Describe(&a.Password, "Optional AAA password override.")
}

// Annotate documents VlanState (no extra fields beyond the embedded Args).
func (s *VlanState) Annotate(_ infer.Annotator) {}

// Create configures the VLAN inside an eAPI configuration session.
func (*Vlan) Create(ctx context.Context, req infer.CreateRequest[VlanArgs]) (infer.CreateResponse[VlanState], error) {
	if err := validateVlanID(req.Inputs.Id); err != nil {
		return infer.CreateResponse[VlanState]{}, err
	}
	state := VlanState{VlanArgs: req.Inputs}
	if req.DryRun {
		return infer.CreateResponse[VlanState]{ID: vlanID(req.Inputs.Id), Output: state}, nil
	}
	if err := apply(ctx, req.Inputs, false); err != nil {
		return infer.CreateResponse[VlanState]{}, fmt.Errorf("create vlan %d: %w", req.Inputs.Id, err)
	}
	return infer.CreateResponse[VlanState]{ID: vlanID(req.Inputs.Id), Output: state}, nil
}

// Read refreshes the VLAN state from the device.
//
// If the VLAN is missing on the device, Read returns an empty ID, signalling
// to the Pulumi engine that the resource has been deleted out-of-band.
func (*Vlan) Read(ctx context.Context, req infer.ReadRequest[VlanArgs, VlanState]) (infer.ReadResponse[VlanArgs, VlanState], error) {
	cli, err := newClient(ctx, req.Inputs.Host, req.Inputs.Username, req.Inputs.Password)
	if err != nil {
		return infer.ReadResponse[VlanArgs, VlanState]{}, err
	}
	current, found, err := readVlan(ctx, cli, req.Inputs.Id)
	if err != nil {
		return infer.ReadResponse[VlanArgs, VlanState]{}, err
	}
	if !found {
		return infer.ReadResponse[VlanArgs, VlanState]{}, nil
	}
	state := VlanState{VlanArgs: req.Inputs}
	if current.Name != "" {
		nm := current.Name
		state.Name = &nm
	}
	return infer.ReadResponse[VlanArgs, VlanState]{
		ID:     vlanID(req.Inputs.Id),
		Inputs: req.Inputs,
		State:  state,
	}, nil
}

// Update re-applies the desired configuration. The eAPI session lifecycle
// handles diff and rollback; idempotent re-apply is safe.
func (*Vlan) Update(ctx context.Context, req infer.UpdateRequest[VlanArgs, VlanState]) (infer.UpdateResponse[VlanState], error) {
	if err := validateVlanID(req.Inputs.Id); err != nil {
		return infer.UpdateResponse[VlanState]{}, err
	}
	state := VlanState{VlanArgs: req.Inputs}
	if req.DryRun {
		return infer.UpdateResponse[VlanState]{Output: state}, nil
	}
	if err := apply(ctx, req.Inputs, false); err != nil {
		return infer.UpdateResponse[VlanState]{}, fmt.Errorf("update vlan %d: %w", req.Inputs.Id, err)
	}
	return infer.UpdateResponse[VlanState]{Output: state}, nil
}

// Delete removes the VLAN.
func (*Vlan) Delete(ctx context.Context, req infer.DeleteRequest[VlanState]) (infer.DeleteResponse, error) {
	if err := apply(ctx, req.State.VlanArgs, true); err != nil {
		return infer.DeleteResponse{}, fmt.Errorf("delete vlan %d: %w", req.State.Id, err)
	}
	return infer.DeleteResponse{}, nil
}

// --- helpers ---------------------------------------------------------------

func validateVlanID(id int) error {
	if id < 1 || id > 4094 {
		return fmt.Errorf("%w (got %d)", ErrVlanIDOutOfRange, id)
	}
	return nil
}

func vlanID(id int) string { return "vlan/" + strconv.Itoa(id) }

func newClient(ctx context.Context, host, user, pass *string) (*eapi.Client, error) {
	cfg := config.FromContext(ctx)
	return cfg.EAPIClient(ctx, host, user, pass)
}

// apply opens a config session, stages the desired commands, and commits.
// When remove is true the VLAN is removed; otherwise it is created/updated.
func apply(ctx context.Context, args VlanArgs, remove bool) error {
	cli, err := newClient(ctx, args.Host, args.Username, args.Password)
	if err != nil {
		return err
	}
	cfg := config.FromContext(ctx)
	sessName := cfg.SessionPrefix() + "vlan-" + strconv.Itoa(args.Id)

	sess, err := cli.OpenSession(ctx, sessName)
	if err != nil {
		return err
	}
	cmds := buildVlanCmds(args, remove)
	if stageErr := sess.Stage(ctx, cmds); stageErr != nil {
		if abortErr := sess.Abort(ctx); abortErr != nil {
			return errors.Join(stageErr, abortErr)
		}
		return stageErr
	}
	return sess.Commit(ctx)
}

func buildVlanCmds(args VlanArgs, remove bool) []string {
	if remove {
		return []string{"no vlan " + strconv.Itoa(args.Id)}
	}
	cmds := []string{"vlan " + strconv.Itoa(args.Id)}
	if args.Name != nil && *args.Name != "" {
		cmds = append(cmds, "name "+*args.Name)
	}
	return cmds
}

// vlanRow is the structured output of `show vlan <id>` (per goeapi JSON).
type vlanRow struct {
	Name string
}

// readVlan returns the live state of the VLAN (or found=false when absent).
//
// Uses `show vlan` (no id filter) since cEOS rejects `show vlan id <n>` for
// VLANs that exist only in running-config without an active state record.
// Filtering happens in the parsed JSON.
func readVlan(ctx context.Context, cli *eapi.Client, id int) (vlanRow, bool, error) {
	resp, err := cli.RunCmds(ctx, []string{"show vlan"}, "json")
	if err != nil {
		return vlanRow{}, false, err
	}
	if len(resp) == 0 {
		return vlanRow{}, false, nil
	}
	vlans, ok := resp[0]["vlans"].(map[string]any)
	if !ok || len(vlans) == 0 {
		return vlanRow{}, false, nil
	}
	entry, ok := vlans[strconv.Itoa(id)].(map[string]any)
	if !ok {
		return vlanRow{}, false, nil
	}
	row := vlanRow{}
	if v, ok := entry["name"].(string); ok {
		row.Name = v
	}
	return row, true, nil
}
