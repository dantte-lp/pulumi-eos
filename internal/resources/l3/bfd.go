package l3

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

// Sentinel errors specific to Bfd.
var (
	ErrBfdTimerBundleIncomplete = errors.New("bfd interval / minRx / multiplier must be set together (all or none)")
	ErrBfdMultiplierOutOfRange  = errors.New("bfd multiplier must be in 3..50")
	ErrBfdIntervalNonPositive   = errors.New("bfd interval / minRx must be > 0 ms")
	ErrBfdSlowTimerNonPositive  = errors.New("bfd slowTimer must be > 0 ms")
)

// Bfd models the global EOS Bidirectional Forwarding Detection settings
// configured under `router bfd` (introduced as a modal CLI in EOS 4.22.0F,
// TOI 14641). It is a singleton per device — one `router bfd` block is
// active at a time.
//
// Per-interface BFD timers (`bfd interval ... min-rx ... multiplier ...`)
// live on `eos:l2:Interface` / `eos:l3:Interface`; per-peer `fall-over
// bfd` lives on `eos:l3:RouterBgp`. This resource owns the global timer
// profile and the admin-shutdown switch only.
//
// Source: EOS User Manual §16.7 (Bidirectional Forwarding Detection); TOI
// 14641 (Modal BFD CLI).
type Bfd struct{}

// BfdArgs is the input set.
type BfdArgs struct {
	// Interval is the BFD transmit rate in milliseconds. Bound together
	// with `minRx` and `multiplier`: set all three or none.
	Interval *int `pulumi:"interval,optional"`
	// MinRx is the expected minimum receive interval in milliseconds.
	// Bound together with `interval` and `multiplier`.
	MinRx *int `pulumi:"minRx,optional"`
	// Multiplier is the detection multiplier (3..50). Bound together
	// with `interval` and `minRx`.
	Multiplier *int `pulumi:"multiplier,optional"`
	// SlowTimer sets the BFD slow-timer (echo-mode min Rx) in
	// milliseconds. Default on EOS is 2000 ms.
	SlowTimer *int `pulumi:"slowTimer,optional"`
	// Shutdown takes BFD admin-down globally when true.
	Shutdown *bool `pulumi:"shutdown,optional"`

	// Host overrides the provider-level eosUrl host for this resource.
	Host *string `pulumi:"host,optional"`
	// Username overrides the provider-level eosUsername for this resource.
	Username *string `pulumi:"username,optional"`
	// Password overrides the provider-level eosPassword for this resource.
	Password *string `provider:"secret" pulumi:"password,optional"`
}

// BfdState mirrors Args.
type BfdState struct {
	BfdArgs
}

// Annotate documents the resource.
func (r *Bfd) Annotate(a infer.Annotator) {
	a.Describe(&r, "Global EOS BFD settings configured under `router bfd`. Singleton per device. Per-interface and per-peer BFD knobs live on Interface / RouterBgp.")
}

// Annotate documents BfdArgs fields.
func (a *BfdArgs) Annotate(an infer.Annotator) {
	an.Describe(&a.Interval, "BFD transmit rate in milliseconds. Bound with minRx and multiplier — set all three or none.")
	an.Describe(&a.MinRx, "Minimum expected receive interval in milliseconds. Bound with interval and multiplier.")
	an.Describe(&a.Multiplier, "BFD detection multiplier (3..50). Bound with interval and minRx.")
	an.Describe(&a.SlowTimer, "BFD slow-timer (echo-mode min Rx) in milliseconds. EOS default is 2000.")
	an.Describe(&a.Shutdown, "Globally admin-down all BFD sessions when true.")
	an.Describe(&a.Host, "Optional management hostname override.")
	an.Describe(&a.Username, "Optional AAA username override.")
	an.Describe(&a.Password, "Optional AAA password override.")
}

// Annotate is a no-op; State has no extra fields.
func (s *BfdState) Annotate(_ infer.Annotator) {}

// Create configures the singleton.
func (*Bfd) Create(ctx context.Context, req infer.CreateRequest[BfdArgs]) (infer.CreateResponse[BfdState], error) {
	if err := validateBfd(req.Inputs); err != nil {
		return infer.CreateResponse[BfdState]{}, err
	}
	state := BfdState{BfdArgs: req.Inputs}
	if req.DryRun {
		return infer.CreateResponse[BfdState]{ID: bfdID(), Output: state}, nil
	}
	if err := applyBfd(ctx, req.Inputs, false); err != nil {
		return infer.CreateResponse[BfdState]{}, fmt.Errorf("create bfd: %w", err)
	}
	return infer.CreateResponse[BfdState]{ID: bfdID(), Output: state}, nil
}

// Read refreshes singleton state from the device.
func (*Bfd) Read(ctx context.Context, req infer.ReadRequest[BfdArgs, BfdState]) (infer.ReadResponse[BfdArgs, BfdState], error) {
	cli, err := newClient(ctx, req.Inputs.Host, req.Inputs.Username, req.Inputs.Password)
	if err != nil {
		return infer.ReadResponse[BfdArgs, BfdState]{}, err
	}
	current, err := readBfd(ctx, cli)
	if err != nil {
		return infer.ReadResponse[BfdArgs, BfdState]{}, err
	}
	state := BfdState{BfdArgs: req.Inputs}
	current.fillState(&state)
	return infer.ReadResponse[BfdArgs, BfdState]{
		ID:     bfdID(),
		Inputs: req.Inputs,
		State:  state,
	}, nil
}

// Update re-applies the singleton.
func (*Bfd) Update(ctx context.Context, req infer.UpdateRequest[BfdArgs, BfdState]) (infer.UpdateResponse[BfdState], error) {
	if err := validateBfd(req.Inputs); err != nil {
		return infer.UpdateResponse[BfdState]{}, err
	}
	state := BfdState{BfdArgs: req.Inputs}
	if req.DryRun {
		return infer.UpdateResponse[BfdState]{Output: state}, nil
	}
	if err := applyBfd(ctx, req.Inputs, false); err != nil {
		return infer.UpdateResponse[BfdState]{}, fmt.Errorf("update bfd: %w", err)
	}
	return infer.UpdateResponse[BfdState]{Output: state}, nil
}

// Delete reverts the singleton via `no router bfd`.
func (*Bfd) Delete(ctx context.Context, req infer.DeleteRequest[BfdState]) (infer.DeleteResponse, error) {
	if err := applyBfd(ctx, req.State.BfdArgs, true); err != nil {
		return infer.DeleteResponse{}, fmt.Errorf("delete bfd: %w", err)
	}
	return infer.DeleteResponse{}, nil
}

// --- helpers ---------------------------------------------------------------

func validateBfd(args BfdArgs) error {
	have := func(p *int) bool { return p != nil }
	bundle := []bool{have(args.Interval), have(args.MinRx), have(args.Multiplier)}
	someSet := bundle[0] || bundle[1] || bundle[2]
	all := bundle[0] && bundle[1] && bundle[2]
	if someSet && !all {
		return ErrBfdTimerBundleIncomplete
	}
	if all {
		if *args.Interval <= 0 || *args.MinRx <= 0 {
			return fmt.Errorf("%w: got interval=%d minRx=%d", ErrBfdIntervalNonPositive, *args.Interval, *args.MinRx)
		}
		if *args.Multiplier < 3 || *args.Multiplier > 50 {
			return fmt.Errorf("%w: got %d", ErrBfdMultiplierOutOfRange, *args.Multiplier)
		}
	}
	if args.SlowTimer != nil && *args.SlowTimer <= 0 {
		return fmt.Errorf("%w: got %d", ErrBfdSlowTimerNonPositive, *args.SlowTimer)
	}
	return nil
}

func bfdID() string { return "bfd/global" }

func applyBfd(ctx context.Context, args BfdArgs, reset bool) error {
	cli, err := newClient(ctx, args.Host, args.Username, args.Password)
	if err != nil {
		return err
	}
	cfg := config.FromContext(ctx)
	sessName := cfg.SessionPrefix() + "bfd-global"

	sess, err := cli.OpenSession(ctx, sessName)
	if err != nil {
		return err
	}
	cmds := buildBfdCmds(args, reset)
	if stageErr := sess.Stage(ctx, cmds); stageErr != nil {
		if abortErr := sess.Abort(ctx); abortErr != nil {
			return errors.Join(stageErr, abortErr)
		}
		return stageErr
	}
	return sess.Commit(ctx)
}

// buildBfdCmds renders the staged CLI block.
func buildBfdCmds(args BfdArgs, reset bool) []string {
	if reset {
		// Clears every sub-command under `router bfd`.
		return []string{"no router bfd"}
	}
	cmds := []string{"router bfd"}
	if args.Interval != nil && args.MinRx != nil && args.Multiplier != nil {
		// cEOS 4.36+ requires the trailing `default` profile selector;
		// the bare interval/min-rx/multiplier triple is rejected with
		// "Incomplete command".
		cmds = append(cmds, fmt.Sprintf("interval %d min-rx %d multiplier %d default",
			*args.Interval, *args.MinRx, *args.Multiplier))
	}
	if args.SlowTimer != nil {
		cmds = append(cmds, "slow-timer "+strconv.Itoa(*args.SlowTimer))
	}
	if args.Shutdown != nil {
		if *args.Shutdown {
			cmds = append(cmds, "shutdown")
		} else {
			cmds = append(cmds, "no shutdown")
		}
	}
	cmds = append(cmds, "exit")
	return cmds
}

// bfdRow is the parsed live state we care about.
type bfdRow struct {
	Interval   int
	MinRx      int
	Multiplier int
	SlowTimer  int
	Shutdown   bool
	HasTimers  bool
}

// readBfd returns the live router bfd state. The block is always
// present after first apply — there is no "deleted" form because
// `no router bfd` reverts the block to factory defaults rather than
// removing the section header. Callers that care about absence should
// compare against the Args zero values.
func readBfd(ctx context.Context, cli *eapi.Client) (bfdRow, error) {
	resp, err := cli.RunCmds(ctx,
		[]string{"show running-config | section router bfd"},
		"text")
	if err != nil {
		return bfdRow{}, err
	}
	if len(resp) == 0 {
		return bfdRow{}, nil
	}
	out, _ := resp[0]["output"].(string)
	return parseBfdSection(out), nil
}

// parseBfdSection extracts the BFD knobs from a `router bfd` running-config
// section. Exposed for unit tests.
func parseBfdSection(out string) bfdRow {
	row := bfdRow{}
	for raw := range strings.SplitSeq(out, "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case line == "shutdown":
			row.Shutdown = true
		case strings.HasPrefix(line, "slow-timer "):
			if v, err := strconv.Atoi(strings.TrimPrefix(line, "slow-timer ")); err == nil {
				row.SlowTimer = v
			}
		case strings.HasPrefix(line, "interval "):
			parseBfdTimerLine(line, &row)
		}
	}
	return row
}

// parseBfdTimerLine reads `interval N min-rx N multiplier N` into row.
func parseBfdTimerLine(line string, row *bfdRow) {
	tokens := strings.Fields(line)
	for i := range len(tokens) - 1 {
		switch tokens[i] {
		case "interval":
			if v, err := strconv.Atoi(tokens[i+1]); err == nil {
				row.Interval = v
			}
		case "min-rx":
			if v, err := strconv.Atoi(tokens[i+1]); err == nil {
				row.MinRx = v
			}
		case "multiplier":
			if v, err := strconv.Atoi(tokens[i+1]); err == nil {
				row.Multiplier = v
			}
		}
	}
	if row.Interval > 0 && row.MinRx > 0 && row.Multiplier > 0 {
		row.HasTimers = true
	}
}

func (r bfdRow) fillState(s *BfdState) {
	if r.HasTimers {
		i := r.Interval
		s.Interval = &i
		m := r.MinRx
		s.MinRx = &m
		mu := r.Multiplier
		s.Multiplier = &mu
	}
	if r.SlowTimer > 0 {
		v := r.SlowTimer
		s.SlowTimer = &v
	}
	if r.Shutdown {
		v := true
		s.Shutdown = &v
	}
}
