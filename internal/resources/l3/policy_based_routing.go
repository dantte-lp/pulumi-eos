package l3

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strconv"
	"strings"

	"github.com/pulumi/pulumi-go-provider/infer"

	"github.com/dantte-lp/pulumi-eos/internal/client/eapi"
	"github.com/dantte-lp/pulumi-eos/internal/config"
)

// Sentinel errors specific to PolicyBasedRouting.
var (
	ErrPbrNameRequired       = errors.New("policyBasedRouting name is required")
	ErrPbrEmptySequences     = errors.New("policyBasedRouting must have at least one sequence")
	ErrPbrSeqRange           = errors.New("policyBasedRouting sequence number must be in 1..65535")
	ErrPbrSeqClassMissing    = errors.New("policyBasedRouting sequence must reference a classMap (set 'class')")
	ErrPbrActionEmpty        = errors.New("policyBasedRouting sequence must have an action: setNexthop, setNexthopGroup, or drop=true")
	ErrPbrActionMutex        = errors.New("policyBasedRouting sequence actions are mutually exclusive: pick exactly one of setNexthop, setNexthopGroup, drop")
	ErrPbrNexthopBadIP       = errors.New("policyBasedRouting setNexthop must be a valid IPv4 or IPv6 literal")
	ErrPbrNexthopGroupEmpty  = errors.New("policyBasedRouting setNexthopGroup name is required")
	ErrPbrClassNameRequired  = errors.New("policyBasedRouting class entry name is required")
	ErrPbrInterfaceBadName   = errors.New("policyBasedRouting interface attachment must match an EOS interface name (e.g. Ethernet1, Port-Channel10, Vlan100)")
	ErrPbrInterfaceDirection = errors.New("policyBasedRouting interface direction must be 'input' (only input service-policy is accepted by EOS)")
)

// PolicyBasedRouting models a top-level
// `policy-map type pbr <name>` block plus an optional
// `service-policy type pbr input <name>` attachment on one or more
// interfaces.
//
// Architecture (per EOS User Manual §10.2 + TOI 14429):
//
//   - A PBR policy-map is composed of *sequences*. Each sequence
//     either references a `class-map type pbr` (typed match) or
//     specifies a raw inline `match` line.
//   - Each sequence has an *action*: `set nexthop <addr> [vrf X]`
//     (route override), `set nexthop-group <NAME>`
//     (TOI 13804 — multi-path / VxLAN nexthops), or `drop`
//     (silent discard).
//   - The policy-map itself is inactive until applied to one or
//     more interfaces via `service-policy type pbr input <name>`.
//     EOS only accepts the *input* direction.
//
// Dependencies (driving Tier-2 → Tier-3 sequencing):
//
//   - **`eos:security:IpAccessList`** — referenced by name in the
//     `class-map type pbr` match clause. v0 ships before
//     `IpAccessList` so the ACL name is taken as a string; the
//     user is expected to create the ACL via `eos:device:Configlet`
//     or `eos:device:RawCli` until S7 ships the typed resource.
//   - `eos:l3:Vrf` — optional `vrf` argument on `set nexthop`.
//   - `eos:l3:NexthopGroup` (Tier 6) — typed reference for
//     `set nexthop-group`; v0 takes the group name as a string.
//
// Render strategy: negate-then-rebuild inside one
// `configure session`. PBR is parser-strict — replacing a class
// reference or set-action mid-sequence is awkward via per-line
// edits, so we always emit `no policy-map type pbr <name>`
// followed by the full body. EOS' session diff applies only the
// minimum delta to running-config; the policy is not torn down on
// active interfaces.
//
// Source: EOS User Manual §10.2.7.43 (`service-policy type pbr`),
// §10.2.7.37 (`policy-map type pbr`); TOI 14429 (Policy Based
// Routing); TOI 14031 (PBR in any VRF); TOI 13804 (nexthop-group
// match in PBR policy); TOI 17517 (Arfa default — PBR shipped on
// cEOS-lab as of 4.30.1F).
type PolicyBasedRouting struct{}

// PbrAction is the action a sequence emits. Exactly one of the
// three fields must be set (validator enforces).
type PbrAction struct {
	// SetNexthop overrides the route to a single next-hop. IPv4 or
	// IPv6; IPv6 uses the same keyword (EOS infers from address).
	SetNexthop *string `pulumi:"setNexthop,optional"`
	// SetNexthopVrf is an optional VRF qualifier paired with
	// SetNexthop (`set nexthop <addr> vrf <X>`). Only meaningful
	// with SetNexthop.
	SetNexthopVrf *string `pulumi:"setNexthopVrf,optional"`
	// SetNexthopGroup references a `nexthop-group` by name (typed
	// `eos:l3:NexthopGroup` planned for Tier 6).
	SetNexthopGroup *string `pulumi:"setNexthopGroup,optional"`
	// Drop=true emits the bare `drop` keyword (no `set` prefix on
	// EOS 4.36 — verified live).
	Drop *bool `pulumi:"drop,optional"`
}

// Annotate documents the action.
func (a *PbrAction) Annotate(an infer.Annotator) {
	an.Describe(&a.SetNexthop, "Override the route to a single next-hop (IPv4 or IPv6 literal). EOS infers the family from the address.")
	an.Describe(&a.SetNexthopVrf, "Optional VRF for the next-hop lookup (`set nexthop <addr> vrf <X>`, TOI 14031).")
	an.Describe(&a.SetNexthopGroup, "Reference a `nexthop-group` by name (TOI 13804). v0 takes the group name as a string until `eos:l3:NexthopGroup` ships in Tier 6.")
	an.Describe(&a.Drop, "When true, drop matching traffic (bare `drop` keyword on EOS 4.36).")
}

// PbrSequence is one row inside a `policy-map type pbr`.
type PbrSequence struct {
	// Seq is the integer order key (1..65535).
	Seq int `pulumi:"seq"`
	// Class is the typed `class-map type pbr` name. Required for
	// v0 — raw inline match (TOI 14429 §"Raw Match Statements")
	// is not modelled in v0 because it requires a different
	// AST shape.
	Class string `pulumi:"class"`
	// Action carries the set / drop action.
	Action PbrAction `pulumi:"action"`
}

// Annotate documents the sequence.
func (s *PbrSequence) Annotate(an infer.Annotator) {
	an.Describe(&s.Seq, "Sequence number (1..65535) — orders the class clauses inside the policy-map.")
	an.Describe(&s.Class, "Name of a `class-map type pbr` defined elsewhere on the device (e.g. via `eos:device:Configlet`).")
	an.Describe(&s.Action, "Action emitted under this sequence — exactly one of setNexthop, setNexthopGroup, drop.")
}

// PbrInterfaceAttachment binds a policy-map to an interface in
// the input direction.
type PbrInterfaceAttachment struct {
	// Interface is the EOS interface name.
	Interface string `pulumi:"interface"`
	// Direction must be "input" (EOS only accepts input PBR).
	Direction *string `pulumi:"direction,optional"`
}

// Annotate documents the attachment.
func (a *PbrInterfaceAttachment) Annotate(an infer.Annotator) {
	an.Describe(&a.Interface, "Interface name (e.g. Ethernet1, Port-Channel10, Vlan100).")
	an.Describe(&a.Direction, "PBR direction. Only 'input' is accepted by EOS — defaults to 'input' when empty.")
}

// PolicyBasedRoutingArgs is the input set.
type PolicyBasedRoutingArgs struct {
	// Name is the policy-map identifier (PK).
	Name string `pulumi:"name"`
	// Sequences is the ordered list of class clauses.
	Sequences []PbrSequence `pulumi:"sequences"`
	// AttachInterfaces optionally binds the policy on a list of
	// interfaces. Empty = policy defined but not applied; users
	// can still apply it manually or via a separate
	// PbrAttachment resource (planned).
	AttachInterfaces []PbrInterfaceAttachment `pulumi:"attachInterfaces,optional"`

	// Host overrides the provider-level eosUrl host for this resource.
	Host *string `pulumi:"host,optional"`
	// Username overrides the provider-level eosUsername for this resource.
	Username *string `pulumi:"username,optional"`
	// Password overrides the provider-level eosPassword for this resource.
	Password *string `provider:"secret" pulumi:"password,optional"`
}

// PolicyBasedRoutingState mirrors Args.
type PolicyBasedRoutingState struct {
	PolicyBasedRoutingArgs
}

// Annotate documents the resource.
func (r *PolicyBasedRouting) Annotate(a infer.Annotator) {
	a.Describe(&r, "An EOS Policy Based Routing policy (`policy-map type pbr <name>`) with optional interface attachments. v0 takes class-map and ACL names as strings; typed cross-resource references land alongside `eos:security:IpAccessList` (Tier 3.1) and `eos:l3:NexthopGroup` (Tier 6).")
}

// Annotate documents Args fields.
func (a *PolicyBasedRoutingArgs) Annotate(an infer.Annotator) {
	an.Describe(&a.Name, "Policy-map identifier (PK). EOS does not accept `description` inside `policy-map type pbr` blocks (verified live; unlike QoS policy-map). Use the resource's Pulumi-side description / labels for documentation.")
	an.Describe(&a.Sequences, "Ordered list of class clauses; each picks a class-map and an action.")
	an.Describe(&a.AttachInterfaces, "Interfaces to apply the policy on (`service-policy type pbr input <name>`).")
	an.Describe(&a.Host, "Optional management hostname override.")
	an.Describe(&a.Username, "Optional AAA username override.")
	an.Describe(&a.Password, "Optional AAA password override.")
}

// Annotate is a no-op; State has no extra fields.
func (s *PolicyBasedRoutingState) Annotate(_ infer.Annotator) {}

// Create stages the policy-map.
func (*PolicyBasedRouting) Create(ctx context.Context, req infer.CreateRequest[PolicyBasedRoutingArgs]) (infer.CreateResponse[PolicyBasedRoutingState], error) {
	if err := validatePbr(req.Inputs); err != nil {
		return infer.CreateResponse[PolicyBasedRoutingState]{}, err
	}
	state := PolicyBasedRoutingState{PolicyBasedRoutingArgs: req.Inputs}
	if req.DryRun {
		return infer.CreateResponse[PolicyBasedRoutingState]{ID: pbrID(req.Inputs.Name), Output: state}, nil
	}
	if err := applyPbr(ctx, req.Inputs, false); err != nil {
		return infer.CreateResponse[PolicyBasedRoutingState]{}, fmt.Errorf("create pbr %s: %w", req.Inputs.Name, err)
	}
	return infer.CreateResponse[PolicyBasedRoutingState]{ID: pbrID(req.Inputs.Name), Output: state}, nil
}

// Read refreshes policy-map state.
func (*PolicyBasedRouting) Read(ctx context.Context, req infer.ReadRequest[PolicyBasedRoutingArgs, PolicyBasedRoutingState]) (infer.ReadResponse[PolicyBasedRoutingArgs, PolicyBasedRoutingState], error) {
	cli, err := newClient(ctx, req.Inputs.Host, req.Inputs.Username, req.Inputs.Password)
	if err != nil {
		return infer.ReadResponse[PolicyBasedRoutingArgs, PolicyBasedRoutingState]{}, err
	}
	current, found, err := readPbr(ctx, cli, req.Inputs.Name)
	if err != nil {
		return infer.ReadResponse[PolicyBasedRoutingArgs, PolicyBasedRoutingState]{}, err
	}
	if !found {
		return infer.ReadResponse[PolicyBasedRoutingArgs, PolicyBasedRoutingState]{}, nil
	}
	state := PolicyBasedRoutingState{PolicyBasedRoutingArgs: req.Inputs}
	current.fillState(&state)
	return infer.ReadResponse[PolicyBasedRoutingArgs, PolicyBasedRoutingState]{
		ID:     pbrID(req.Inputs.Name),
		Inputs: req.Inputs,
		State:  state,
	}, nil
}

// Update re-applies negate-then-rebuild.
func (*PolicyBasedRouting) Update(ctx context.Context, req infer.UpdateRequest[PolicyBasedRoutingArgs, PolicyBasedRoutingState]) (infer.UpdateResponse[PolicyBasedRoutingState], error) {
	if err := validatePbr(req.Inputs); err != nil {
		return infer.UpdateResponse[PolicyBasedRoutingState]{}, err
	}
	state := PolicyBasedRoutingState{PolicyBasedRoutingArgs: req.Inputs}
	if req.DryRun {
		return infer.UpdateResponse[PolicyBasedRoutingState]{Output: state}, nil
	}
	if err := applyPbr(ctx, req.Inputs, false); err != nil {
		return infer.UpdateResponse[PolicyBasedRoutingState]{}, fmt.Errorf("update pbr %s: %w", req.Inputs.Name, err)
	}
	return infer.UpdateResponse[PolicyBasedRoutingState]{Output: state}, nil
}

// Delete removes the policy-map and any attachments.
func (*PolicyBasedRouting) Delete(ctx context.Context, req infer.DeleteRequest[PolicyBasedRoutingState]) (infer.DeleteResponse, error) {
	if err := applyPbr(ctx, req.State.PolicyBasedRoutingArgs, true); err != nil {
		return infer.DeleteResponse{}, fmt.Errorf("delete pbr %s: %w", req.State.Name, err)
	}
	return infer.DeleteResponse{}, nil
}

// --- helpers ---------------------------------------------------------------

func validatePbr(args PolicyBasedRoutingArgs) error {
	if strings.TrimSpace(args.Name) == "" {
		return ErrPbrNameRequired
	}
	if len(args.Sequences) == 0 {
		return ErrPbrEmptySequences
	}
	for i := range args.Sequences {
		if err := validatePbrSequence(&args.Sequences[i]); err != nil {
			return err
		}
	}
	for i := range args.AttachInterfaces {
		if err := validatePbrAttachment(&args.AttachInterfaces[i]); err != nil {
			return err
		}
	}
	return nil
}

func validatePbrSequence(s *PbrSequence) error {
	if s.Seq < 1 || s.Seq > 65535 {
		return fmt.Errorf("%w: got %d", ErrPbrSeqRange, s.Seq)
	}
	if strings.TrimSpace(s.Class) == "" {
		return ErrPbrSeqClassMissing
	}
	return validatePbrAction(&s.Action)
}

func validatePbrAction(a *PbrAction) error {
	count := 0
	if a.SetNexthop != nil && *a.SetNexthop != "" {
		count++
		if _, err := netip.ParseAddr(*a.SetNexthop); err != nil {
			return fmt.Errorf("%w: %q", ErrPbrNexthopBadIP, *a.SetNexthop)
		}
	}
	if a.SetNexthopGroup != nil && *a.SetNexthopGroup != "" {
		count++
	}
	if a.Drop != nil && *a.Drop {
		count++
	}
	switch count {
	case 0:
		return ErrPbrActionEmpty
	case 1:
		return nil
	default:
		return ErrPbrActionMutex
	}
}

func validatePbrAttachment(a *PbrInterfaceAttachment) error {
	if !vrrpInterfaceRe.MatchString(a.Interface) {
		return fmt.Errorf("%w: %q", ErrPbrInterfaceBadName, a.Interface)
	}
	if a.Direction != nil && *a.Direction != "" && *a.Direction != "input" {
		return fmt.Errorf("%w: got %q", ErrPbrInterfaceDirection, *a.Direction)
	}
	return nil
}

func pbrID(name string) string {
	return "policy-based-routing/" + name
}

func applyPbr(ctx context.Context, args PolicyBasedRoutingArgs, remove bool) error {
	cli, err := newClient(ctx, args.Host, args.Username, args.Password)
	if err != nil {
		return err
	}
	cfg := config.FromContext(ctx)
	sessName := cfg.SessionPrefix() + "pbr-" + sanitizePrefixListName(args.Name)
	sess, err := cli.OpenSession(ctx, sessName)
	if err != nil {
		return err
	}
	cmds := buildPbrCmds(args, remove)
	if stageErr := sess.Stage(ctx, cmds); stageErr != nil {
		if abortErr := sess.Abort(ctx); abortErr != nil {
			return errors.Join(stageErr, abortErr)
		}
		return stageErr
	}
	return sess.Commit(ctx)
}

// buildPbrCmds renders the staged CLI block.
//
//	# detach first (if any)
//	interface <X>
//	  no service-policy type pbr input <name>
//	  exit
//	# negate-then-rebuild the policy-map
//	no policy-map type pbr <name>
//	policy-map type pbr <name>
//	  description <text>
//	  N class <CMAP>
//	    set nexthop <addr> [vrf <V>] | set nexthop-group <NAME> | drop
//	  …
//	# re-attach
//	interface <X>
//	  service-policy type pbr input <name>
func buildPbrCmds(args PolicyBasedRoutingArgs, remove bool) []string {
	var cmds []string
	cmds = appendPbrDetach(cmds, args)
	cmds = append(cmds, "no policy-map type pbr "+args.Name)
	if remove {
		return cmds
	}
	cmds = append(cmds, "policy-map type pbr "+args.Name)
	// NB: EOS rejects `description` inside `policy-map type pbr`
	// (Invalid input; verified live against cEOS 4.36). The QoS
	// policy-map family accepts it, but PBR does not.
	for _, seq := range args.Sequences {
		cmds = appendPbrSequence(cmds, seq)
	}
	cmds = append(cmds, "exit")
	cmds = appendPbrAttach(cmds, args)
	return cmds
}

// appendPbrDetach removes any existing service-policy attachment
// from each requested interface so the policy-map can be replaced.
func appendPbrDetach(cmds []string, args PolicyBasedRoutingArgs) []string {
	for _, ai := range args.AttachInterfaces {
		cmds = append(cmds,
			"interface "+ai.Interface,
			"no service-policy type pbr input "+args.Name,
			"exit",
		)
	}
	return cmds
}

// appendPbrSequence emits one `<seq> class <CMAP>` block.
func appendPbrSequence(cmds []string, s PbrSequence) []string {
	cmds = append(cmds, strconv.Itoa(s.Seq)+" class "+s.Class)
	switch {
	case s.Action.SetNexthop != nil && *s.Action.SetNexthop != "":
		line := "set nexthop " + *s.Action.SetNexthop
		if s.Action.SetNexthopVrf != nil && *s.Action.SetNexthopVrf != "" {
			line += " vrf " + *s.Action.SetNexthopVrf
		}
		cmds = append(cmds, line)
	case s.Action.SetNexthopGroup != nil && *s.Action.SetNexthopGroup != "":
		cmds = append(cmds, "set nexthop-group "+*s.Action.SetNexthopGroup)
	case s.Action.Drop != nil && *s.Action.Drop:
		cmds = append(cmds, "drop")
	}
	cmds = append(cmds, "exit")
	return cmds
}

// appendPbrAttach binds the policy on every interface.
func appendPbrAttach(cmds []string, args PolicyBasedRoutingArgs) []string {
	for _, ai := range args.AttachInterfaces {
		cmds = append(cmds,
			"interface "+ai.Interface,
			"service-policy type pbr input "+args.Name,
			"exit",
		)
	}
	return cmds
}

// pbrRow is the parsed live state.
type pbrRow struct {
	Description string
	Sequences   []PbrSequence
}

// readPbr returns the live policy-map or (false, nil) when absent.
func readPbr(ctx context.Context, cli *eapi.Client, name string) (pbrRow, bool, error) {
	resp, err := cli.RunCmds(ctx,
		[]string{"show running-config section policy-map"},
		"text")
	if err != nil {
		return pbrRow{}, false, err
	}
	if len(resp) == 0 {
		return pbrRow{}, false, nil
	}
	out, _ := resp[0]["output"].(string)
	row, found := parsePbrSection(out, name)
	return row, found, nil
}

// parsePbrSection extracts one `policy-map type pbr <name>` block.
func parsePbrSection(out, name string) (pbrRow, bool) {
	header := "policy-map type pbr " + name
	row := pbrRow{}
	inOurs := false
	var current *PbrSequence
	found := false
	for raw := range strings.SplitSeq(out, "\n") {
		line := strings.TrimSpace(raw)
		if line == header {
			inOurs, found = true, true
			continue
		}
		if strings.HasPrefix(line, "policy-map ") {
			inOurs = false
			continue
		}
		if !inOurs {
			continue
		}
		current = applyPbrSectionLine(&row, current, line)
	}
	return row, found
}

// applyPbrSectionLine processes one body line of the policy-map.
// It maintains the "current sequence" pointer so set / drop lines
// land on the right row.
func applyPbrSectionLine(row *pbrRow, current *PbrSequence, line string) *PbrSequence {
	if rest, ok := strings.CutPrefix(line, "description "); ok {
		row.Description = rest
		return current
	}
	if seq, class, ok := parsePbrSeqHeader(line); ok {
		row.Sequences = append(row.Sequences, PbrSequence{Seq: seq, Class: class})
		return &row.Sequences[len(row.Sequences)-1]
	}
	if current == nil {
		return current
	}
	switch {
	case strings.HasPrefix(line, "set nexthop-group "):
		v := strings.TrimPrefix(line, "set nexthop-group ")
		current.Action.SetNexthopGroup = &v
	case strings.HasPrefix(line, "set nexthop "):
		applyPbrNexthop(current, strings.TrimPrefix(line, "set nexthop "))
	case line == "drop":
		v := true
		current.Action.Drop = &v
	}
	return current
}

// parsePbrSeqHeader matches `<N> class <CMAP>`.
func parsePbrSeqHeader(line string) (int, string, bool) {
	parts := strings.Fields(line)
	if len(parts) != 3 || parts[1] != "class" {
		return 0, "", false
	}
	seq, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, "", false
	}
	return seq, parts[2], true
}

// applyPbrNexthop parses `<addr> [vrf <V>]`.
func applyPbrNexthop(s *PbrSequence, rest string) {
	tokens := strings.Fields(rest)
	if len(tokens) == 0 {
		return
	}
	addr := tokens[0]
	s.Action.SetNexthop = &addr
	for i := 1; i+1 < len(tokens); i++ {
		if tokens[i] == "vrf" {
			vrf := tokens[i+1]
			s.Action.SetNexthopVrf = &vrf
		}
	}
}

// fillState writes the parsed row back to State.
func (r pbrRow) fillState(s *PolicyBasedRoutingState) {
	if len(r.Sequences) > 0 {
		s.Sequences = append([]PbrSequence{}, r.Sequences...)
	}
}
