package l3

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"strconv"
	"strings"

	"github.com/pulumi/pulumi-go-provider/infer"

	"github.com/dantte-lp/pulumi-eos/internal/client/eapi"
	"github.com/dantte-lp/pulumi-eos/internal/config"
)

// Sentinel errors specific to RouterBgp.
var (
	ErrBgpAsnInvalid                 = errors.New("routerBgp asn must be in 1..4294967295 (4-byte ASN)")
	ErrBgpRouterIdBadIPv4            = errors.New("routerBgp routerId must be an IPv4 address")
	ErrBgpMaximumPathsInvalid        = errors.New("routerBgp maximumPaths.paths must be > 0; ecmp (when set) must be >= paths")
	ErrBgpPeerGroupNameRequired      = errors.New("routerBgp peerGroup.name is required")
	ErrBgpPeerGroupSendCommunity     = errors.New("routerBgp peerGroup.sendCommunity must be one of: standard, extended, large, all")
	ErrBgpPeerGroupMaximumRoutes     = errors.New("routerBgp peerGroup.maximumRoutes must be >= 0")
	ErrBgpNeighborAddressBadIPv4     = errors.New("routerBgp neighbor.address must be a valid IPv4 address")
	ErrBgpNeighborPeerGroupRemoteAS  = errors.New("routerBgp neighbor must specify peerGroup OR remoteAs (not both)")
	ErrBgpAddressFamilyNameInvalid   = errors.New("routerBgp addressFamily.name must be one of: ipv4, ipv6, evpn")
	ErrBgpVrfNameRequired            = errors.New("routerBgp vrf.name is required")
	ErrBgpVrfNameReserved            = errors.New("routerBgp vrf.name 'default' is reserved (use the top-level block)")
	ErrBgpVrfRedistributeUnsupported = errors.New("routerBgp vrf.redistribute entries must be one of: connected, static, attached-host")
)

// validBgpSendCommunity enumerates the keywords EOS accepts for
// `neighbor X send-community`. Verified live on cEOS 4.36.0.1F.
var validBgpSendCommunity = map[string]struct{}{
	"standard": {},
	"extended": {},
	"large":    {},
	"all":      {},
}

// validBgpRedistribute is the v0 subset of `redistribute` sources we
// support inside a VRF block. Per EOS BGP/MPLS L3 VPN TOI 14091 these
// three are the canonical EVPN-VRF sources; OSPF / ISIS / BGP-leak
// will be added when a consumer requires them.
var validBgpRedistribute = map[string]struct{}{
	"connected":     {},
	"static":        {},
	"attached-host": {},
}

// validBgpAddressFamily enumerates the AF names we render. v0 covers
// the leaf-spine EVPN/VXLAN demo (S6 exit-criterion); other AFs
// (ipv4-unicast vrf X, l2vpn evpn flow-spec, path-selection) follow
// when consumers require them.
var validBgpAddressFamily = map[string]struct{}{
	"ipv4": {},
	"ipv6": {},
	"evpn": {},
}

// RouterBgp models the global EOS BGP routing instance. EOS allows at
// most one `router bgp <asn>` block per device, so this resource is
// effectively a singleton; ASN is the primary key so a Pulumi rename
// (ASN change) flows through replace, not update.
//
// v0 surface — sufficient for a leaf-spine EVPN/VXLAN fabric (S6
// exit): top-level globals, peer-groups + neighbor bindings, per-AF
// activate/deactivate, per-VRF RD/RT/redistribute. Higher-fidelity
// knobs (RCF, route-maps, dampening, graceful-restart, RPKI,
// per-VRF-AF redistribute filters, BGP password type-7) follow when
// the leaf-spine demo demands them.
//
// Source: EOS User Manual §16 (BGP); TOI 14091 (RFC 4364 BGP/MPLS L3
// VPN); validated live against cEOS 4.36.0.1F per the per-resource
// verification rule (`docs/05-development.md`).
type RouterBgp struct{}

// BgpMaximumPaths bundles `maximum-paths <paths> [ecmp <ecmp>]`.
type BgpMaximumPaths struct {
	// Paths is the count of paths a neighbor can install (0 disables).
	Paths int `pulumi:"paths"`
	// Ecmp is the count of ECMP paths (when nil, the `ecmp` clause is
	// omitted and EOS uses Paths as both).
	Ecmp *int `pulumi:"ecmp,optional"`
}

// Annotate documents BgpMaximumPaths fields.
func (m *BgpMaximumPaths) Annotate(an infer.Annotator) {
	an.Describe(&m.Paths, "Paths a neighbor can install (typically 4 in a 2-spine fabric).")
	an.Describe(&m.Ecmp, "ECMP path count. When unset, EOS uses `paths` as both.")
}

// BgpPeerGroup models `neighbor <name> peer group` plus the most-used
// per-group knobs. Order of fields matches the canonical EOS render
// order so the Read parser and the buildCmds renderer agree.
type BgpPeerGroup struct {
	Name          string  `pulumi:"name"`
	RemoteAs      *int    `pulumi:"remoteAs,optional"`
	UpdateSource  *string `pulumi:"updateSource,optional"`
	EbgpMultihop  *int    `pulumi:"ebgpMultihop,optional"`
	SendCommunity *string `pulumi:"sendCommunity,optional"`
	MaximumRoutes *int    `pulumi:"maximumRoutes,optional"`
	Bfd           *bool   `pulumi:"bfd,optional"`
	Description   *string `pulumi:"description,optional"`
}

// Annotate documents BgpPeerGroup fields.
func (p *BgpPeerGroup) Annotate(an infer.Annotator) {
	an.Describe(&p.Name, "Peer-group name (becomes `neighbor <name>` on the device).")
	an.Describe(&p.RemoteAs, "Remote ASN (1..4294967295).")
	an.Describe(&p.UpdateSource, "Source interface for outbound updates (e.g. Loopback0).")
	an.Describe(&p.EbgpMultihop, "Maximum eBGP TTL (1..255).")
	an.Describe(&p.SendCommunity, "One of: standard, extended, large, all.")
	an.Describe(&p.MaximumRoutes, "Per-neighbor route limit (0 disables).")
	an.Describe(&p.Bfd, "When true, emit `neighbor <name> bfd` (per-peer BFD detection).")
	an.Describe(&p.Description, "Free-text description.")
}

// BgpNeighbor binds a per-IP neighbor to a peer-group OR sets a
// per-neighbor remote-as directly (rare; peer-groups are preferred).
type BgpNeighbor struct {
	Address     string  `pulumi:"address"`
	PeerGroup   *string `pulumi:"peerGroup,optional"`
	RemoteAs    *int    `pulumi:"remoteAs,optional"`
	Description *string `pulumi:"description,optional"`
}

// Annotate documents BgpNeighbor fields.
func (n *BgpNeighbor) Annotate(an infer.Annotator) {
	an.Describe(&n.Address, "Neighbor IPv4 address.")
	an.Describe(&n.PeerGroup, "Peer-group name to inherit settings from. Mutually exclusive with remoteAs.")
	an.Describe(&n.RemoteAs, "Per-neighbor remote ASN. Mutually exclusive with peerGroup.")
	an.Describe(&n.Description, "Free-text description.")
}

// BgpAddressFamily models `address-family <name>` activate / deactivate
// per peer-group. EOS' `no neighbor X activate` is required under
// `ipv4` when the EVPN AF is the only active one for that peer-group.
type BgpAddressFamily struct {
	// Name is one of: ipv4 | ipv6 | evpn.
	Name string `pulumi:"name"`
	// Activate lists peer-group names to `neighbor X activate`.
	Activate []string `pulumi:"activate,optional"`
	// Deactivate lists peer-group names to `no neighbor X activate`.
	Deactivate []string `pulumi:"deactivate,optional"`
}

// Annotate documents BgpAddressFamily fields.
func (af *BgpAddressFamily) Annotate(an infer.Annotator) {
	an.Describe(&af.Name, "Address-family name: ipv4, ipv6, or evpn.")
	an.Describe(&af.Activate, "Peer-group names to activate within this AF.")
	an.Describe(&af.Deactivate, "Peer-group names to explicitly deactivate within this AF.")
}

// BgpVrf models a per-VRF block under router bgp. RD/RT EVPN
// import/export plus a small redistribute list cover the v0 EVPN
// L3-gateway use case.
type BgpVrf struct {
	Name                  string   `pulumi:"name"`
	Rd                    *string  `pulumi:"rd,optional"`
	RouteTargetImportEvpn *string  `pulumi:"routeTargetImportEvpn,optional"`
	RouteTargetExportEvpn *string  `pulumi:"routeTargetExportEvpn,optional"`
	RouterId              *string  `pulumi:"routerId,optional"`
	Redistribute          []string `pulumi:"redistribute,optional"`
}

// Annotate documents BgpVrf fields.
func (v *BgpVrf) Annotate(an infer.Annotator) {
	an.Describe(&v.Name, "VRF name. Cannot be `default` — top-level block applies to default.")
	an.Describe(&v.Rd, "Route distinguisher (e.g. 10.255.1.1:10).")
	an.Describe(&v.RouteTargetImportEvpn, "EVPN route-target import (e.g. 64500:10).")
	an.Describe(&v.RouteTargetExportEvpn, "EVPN route-target export (e.g. 64500:10).")
	an.Describe(&v.RouterId, "Per-VRF router-id (typically loopback IP).")
	an.Describe(&v.Redistribute, "List of `redistribute <source>` entries: connected, static, attached-host.")
}

// RouterBgpArgs is the input set.
type RouterBgpArgs struct {
	// Asn is the local ASN (1..4294967295). Primary key.
	Asn int `pulumi:"asn"`
	// RouterId is the BGP router-id (typically Loopback0 IPv4).
	RouterId *string `pulumi:"routerId,optional"`
	// NoDefaultIpv4Unicast emits `no bgp default ipv4-unicast` when
	// true. Required for EVPN-only fabrics where the IPv4 AF is
	// explicitly deactivated for the EVPN peer-group.
	NoDefaultIpv4Unicast *bool `pulumi:"noDefaultIpv4Unicast,optional"`
	// MaximumPaths sets `maximum-paths <paths> [ecmp <ecmp>]`.
	MaximumPaths *BgpMaximumPaths `pulumi:"maximumPaths,optional"`
	// Bfd toggles the global `bfd` knob inside `router bgp`.
	Bfd *bool `pulumi:"bfd,optional"`
	// PeerGroups declared at the top level (sorted by Name on render
	// for stable diffs).
	PeerGroups []BgpPeerGroup `pulumi:"peerGroups,optional"`
	// Neighbors maps IPv4 addresses to peer-groups.
	Neighbors []BgpNeighbor `pulumi:"neighbors,optional"`
	// AddressFamilies declares per-AF activations.
	AddressFamilies []BgpAddressFamily `pulumi:"addressFamilies,optional"`
	// Vrfs declares per-VRF blocks (RD / RT EVPN / redistribute).
	Vrfs []BgpVrf `pulumi:"vrfs,optional"`

	// Host overrides the provider-level eosUrl host for this resource.
	Host *string `pulumi:"host,optional"`
	// Username overrides the provider-level eosUsername for this resource.
	Username *string `pulumi:"username,optional"`
	// Password overrides the provider-level eosPassword for this resource.
	Password *string `provider:"secret" pulumi:"password,optional"`
}

// RouterBgpState mirrors Args.
type RouterBgpState struct {
	RouterBgpArgs
}

// Annotate documents the resource.
func (r *RouterBgp) Annotate(a infer.Annotator) {
	a.Describe(&r, "EOS BGP routing instance (singleton). v0 surface covers underlay + EVPN/VXLAN overlay control plane: globals, peer-groups, neighbors, AF activate/deactivate, per-VRF RD/RT and redistribute.")
}

// Annotate documents RouterBgpArgs fields.
func (a *RouterBgpArgs) Annotate(an infer.Annotator) {
	an.Describe(&a.Asn, "Local ASN (1..4294967295). The `router bgp <asn>` instance is unique per device.")
	an.Describe(&a.RouterId, "BGP router-id (IPv4, typically the Loopback0 address).")
	an.Describe(&a.NoDefaultIpv4Unicast, "When true emit `no bgp default ipv4-unicast` (mandatory for EVPN-only fabrics).")
	an.Describe(&a.MaximumPaths, "`maximum-paths <paths> [ecmp <ecmp>]` global multipath config.")
	an.Describe(&a.Bfd, "When true emit the global `bfd` knob inside the router bgp block.")
	an.Describe(&a.PeerGroups, "Peer-group declarations (preferred for inheritance).")
	an.Describe(&a.Neighbors, "Per-IP neighbor → peer-group bindings.")
	an.Describe(&a.AddressFamilies, "Per-AF activate / deactivate per peer-group.")
	an.Describe(&a.Vrfs, "Per-VRF blocks (RD, RT EVPN, redistribute).")
	an.Describe(&a.Host, "Optional management hostname override.")
	an.Describe(&a.Username, "Optional AAA username override.")
	an.Describe(&a.Password, "Optional AAA password override.")
}

// Annotate is a no-op; State has no extra fields.
func (s *RouterBgpState) Annotate(_ infer.Annotator) {}

// Create configures the BGP instance.
func (*RouterBgp) Create(ctx context.Context, req infer.CreateRequest[RouterBgpArgs]) (infer.CreateResponse[RouterBgpState], error) {
	if err := validateRouterBgp(req.Inputs); err != nil {
		return infer.CreateResponse[RouterBgpState]{}, err
	}
	state := RouterBgpState{RouterBgpArgs: req.Inputs}
	if req.DryRun {
		return infer.CreateResponse[RouterBgpState]{ID: routerBgpID(req.Inputs.Asn), Output: state}, nil
	}
	if err := applyRouterBgp(ctx, req.Inputs, false); err != nil {
		return infer.CreateResponse[RouterBgpState]{}, fmt.Errorf("create router bgp %d: %w", req.Inputs.Asn, err)
	}
	return infer.CreateResponse[RouterBgpState]{ID: routerBgpID(req.Inputs.Asn), Output: state}, nil
}

// Read refreshes BGP state from the device.
func (*RouterBgp) Read(ctx context.Context, req infer.ReadRequest[RouterBgpArgs, RouterBgpState]) (infer.ReadResponse[RouterBgpArgs, RouterBgpState], error) {
	cli, err := newClient(ctx, req.Inputs.Host, req.Inputs.Username, req.Inputs.Password)
	if err != nil {
		return infer.ReadResponse[RouterBgpArgs, RouterBgpState]{}, err
	}
	current, found, err := readRouterBgp(ctx, cli)
	if err != nil {
		return infer.ReadResponse[RouterBgpArgs, RouterBgpState]{}, err
	}
	if !found || current.Asn != req.Inputs.Asn {
		return infer.ReadResponse[RouterBgpArgs, RouterBgpState]{}, nil
	}
	state := RouterBgpState{RouterBgpArgs: req.Inputs}
	current.fillState(&state)
	return infer.ReadResponse[RouterBgpArgs, RouterBgpState]{
		ID:     routerBgpID(req.Inputs.Asn),
		Inputs: req.Inputs,
		State:  state,
	}, nil
}

// Update re-applies the BGP block. EOS' configure-session diff
// computes the minimum delta; users see only the affected lines in
// `show session-config diffs`.
func (*RouterBgp) Update(ctx context.Context, req infer.UpdateRequest[RouterBgpArgs, RouterBgpState]) (infer.UpdateResponse[RouterBgpState], error) {
	if err := validateRouterBgp(req.Inputs); err != nil {
		return infer.UpdateResponse[RouterBgpState]{}, err
	}
	state := RouterBgpState{RouterBgpArgs: req.Inputs}
	if req.DryRun {
		return infer.UpdateResponse[RouterBgpState]{Output: state}, nil
	}
	if err := applyRouterBgp(ctx, req.Inputs, false); err != nil {
		return infer.UpdateResponse[RouterBgpState]{}, fmt.Errorf("update router bgp %d: %w", req.Inputs.Asn, err)
	}
	return infer.UpdateResponse[RouterBgpState]{Output: state}, nil
}

// Delete clears the entire BGP block via `no router bgp <asn>`.
func (*RouterBgp) Delete(ctx context.Context, req infer.DeleteRequest[RouterBgpState]) (infer.DeleteResponse, error) {
	if err := applyRouterBgp(ctx, req.State.RouterBgpArgs, true); err != nil {
		return infer.DeleteResponse{}, fmt.Errorf("delete router bgp %d: %w", req.State.Asn, err)
	}
	return infer.DeleteResponse{}, nil
}

// --- helpers ---------------------------------------------------------------

func validateRouterBgp(args RouterBgpArgs) error {
	if args.Asn < 1 || args.Asn > 4294967295 {
		return fmt.Errorf("%w: got %d", ErrBgpAsnInvalid, args.Asn)
	}
	if args.RouterId != nil && *args.RouterId != "" {
		if addr, err := netip.ParseAddr(*args.RouterId); err != nil || !addr.Is4() {
			return fmt.Errorf("%w: %q", ErrBgpRouterIdBadIPv4, *args.RouterId)
		}
	}
	if args.MaximumPaths != nil {
		if args.MaximumPaths.Paths <= 0 {
			return fmt.Errorf("%w: paths=%d", ErrBgpMaximumPathsInvalid, args.MaximumPaths.Paths)
		}
		if args.MaximumPaths.Ecmp != nil && *args.MaximumPaths.Ecmp < args.MaximumPaths.Paths {
			return fmt.Errorf("%w: ecmp=%d < paths=%d", ErrBgpMaximumPathsInvalid, *args.MaximumPaths.Ecmp, args.MaximumPaths.Paths)
		}
	}
	for i := range args.PeerGroups {
		if err := validateBgpPeerGroup(&args.PeerGroups[i]); err != nil {
			return err
		}
	}
	for i := range args.Neighbors {
		if err := validateBgpNeighbor(&args.Neighbors[i]); err != nil {
			return err
		}
	}
	for i := range args.AddressFamilies {
		if _, ok := validBgpAddressFamily[args.AddressFamilies[i].Name]; !ok {
			return fmt.Errorf("%w: got %q", ErrBgpAddressFamilyNameInvalid, args.AddressFamilies[i].Name)
		}
	}
	for i := range args.Vrfs {
		if err := validateBgpVrf(&args.Vrfs[i]); err != nil {
			return err
		}
	}
	return nil
}

func validateBgpPeerGroup(p *BgpPeerGroup) error {
	if strings.TrimSpace(p.Name) == "" {
		return ErrBgpPeerGroupNameRequired
	}
	if p.SendCommunity != nil && *p.SendCommunity != "" {
		if _, ok := validBgpSendCommunity[*p.SendCommunity]; !ok {
			return fmt.Errorf("%w: got %q", ErrBgpPeerGroupSendCommunity, *p.SendCommunity)
		}
	}
	if p.MaximumRoutes != nil && *p.MaximumRoutes < 0 {
		return fmt.Errorf("%w: got %d", ErrBgpPeerGroupMaximumRoutes, *p.MaximumRoutes)
	}
	return nil
}

func validateBgpNeighbor(n *BgpNeighbor) error {
	if addr, err := netip.ParseAddr(n.Address); err != nil || !addr.Is4() {
		return fmt.Errorf("%w: %q", ErrBgpNeighborAddressBadIPv4, n.Address)
	}
	hasPG := n.PeerGroup != nil && *n.PeerGroup != ""
	hasRA := n.RemoteAs != nil
	if hasPG == hasRA {
		return fmt.Errorf("%w: address=%s", ErrBgpNeighborPeerGroupRemoteAS, n.Address)
	}
	return nil
}

func validateBgpVrf(v *BgpVrf) error {
	if strings.TrimSpace(v.Name) == "" {
		return ErrBgpVrfNameRequired
	}
	if strings.EqualFold(v.Name, "default") {
		return ErrBgpVrfNameReserved
	}
	for _, r := range v.Redistribute {
		if _, ok := validBgpRedistribute[r]; !ok {
			return fmt.Errorf("%w: got %q", ErrBgpVrfRedistributeUnsupported, r)
		}
	}
	if v.RouterId != nil && *v.RouterId != "" {
		if addr, err := netip.ParseAddr(*v.RouterId); err != nil || !addr.Is4() {
			return fmt.Errorf("%w: vrf=%s routerId=%q", ErrBgpRouterIdBadIPv4, v.Name, *v.RouterId)
		}
	}
	return nil
}

func routerBgpID(asn int) string { return "router-bgp/" + strconv.Itoa(asn) }

func applyRouterBgp(ctx context.Context, args RouterBgpArgs, remove bool) error {
	cli, err := newClient(ctx, args.Host, args.Username, args.Password)
	if err != nil {
		return err
	}
	cfg := config.FromContext(ctx)
	sessName := cfg.SessionPrefix() + "rbgp-" + strconv.Itoa(args.Asn)

	sess, err := cli.OpenSession(ctx, sessName)
	if err != nil {
		return err
	}
	cmds := buildRouterBgpCmds(args, remove)
	if stageErr := sess.Stage(ctx, cmds); stageErr != nil {
		if abortErr := sess.Abort(ctx); abortErr != nil {
			return errors.Join(stageErr, abortErr)
		}
		return stageErr
	}
	return sess.Commit(ctx)
}

// buildRouterBgpCmds renders the staged CLI block. Order matches what
// EOS emits in `show running-config`:
//
//	router bgp <asn>
//	   router-id ...
//	   no bgp default ipv4-unicast
//	   maximum-paths ...
//	   bfd
//	   neighbor <pg> peer group
//	   neighbor <pg> ...
//	   neighbor <ip> peer group <pg>
//	   address-family <af>
//	      neighbor <pg> activate / no neighbor <pg> activate
//	   vrf <name>
//	      rd / rt / router-id / redistribute
//
// On Update the staged block is fully re-emitted; EOS' configure
// session diff computes the minimum delta. Negate-then-rebuild is
// NOT used at the top level because `no router bgp` would drop all
// dynamic peer state inside the session and may surface as a momentary
// session flap when committed.
func buildRouterBgpCmds(args RouterBgpArgs, remove bool) []string {
	if remove {
		return []string{"no router bgp " + strconv.Itoa(args.Asn)}
	}
	cmds := []string{"router bgp " + strconv.Itoa(args.Asn)}
	if args.RouterId != nil && *args.RouterId != "" {
		cmds = append(cmds, "router-id "+*args.RouterId)
	}
	if args.NoDefaultIpv4Unicast != nil && *args.NoDefaultIpv4Unicast {
		cmds = append(cmds, "no bgp default ipv4-unicast")
	}
	if args.MaximumPaths != nil {
		line := "maximum-paths " + strconv.Itoa(args.MaximumPaths.Paths)
		if args.MaximumPaths.Ecmp != nil {
			line += " ecmp " + strconv.Itoa(*args.MaximumPaths.Ecmp)
		}
		cmds = append(cmds, line)
	}
	if args.Bfd != nil && *args.Bfd {
		cmds = append(cmds, "bfd")
	}

	pgs := append([]BgpPeerGroup(nil), args.PeerGroups...)
	sort.Slice(pgs, func(i, j int) bool { return pgs[i].Name < pgs[j].Name })
	for _, pg := range pgs {
		cmds = append(cmds, peerGroupCmds(pg)...)
	}

	nbs := append([]BgpNeighbor(nil), args.Neighbors...)
	sort.Slice(nbs, func(i, j int) bool { return nbs[i].Address < nbs[j].Address })
	for _, n := range nbs {
		cmds = append(cmds, neighborCmds(n)...)
	}

	afs := append([]BgpAddressFamily(nil), args.AddressFamilies...)
	sort.Slice(afs, func(i, j int) bool { return afs[i].Name < afs[j].Name })
	for _, af := range afs {
		cmds = append(cmds, addressFamilyCmds(af)...)
	}

	vrfs := append([]BgpVrf(nil), args.Vrfs...)
	sort.Slice(vrfs, func(i, j int) bool { return vrfs[i].Name < vrfs[j].Name })
	for _, v := range vrfs {
		cmds = append(cmds, bgpVrfCmds(v)...)
	}

	cmds = append(cmds, "exit")
	return cmds
}

func peerGroupCmds(p BgpPeerGroup) []string {
	cmds := []string{"neighbor " + p.Name + " peer group"}
	if p.RemoteAs != nil {
		cmds = append(cmds, "neighbor "+p.Name+" remote-as "+strconv.Itoa(*p.RemoteAs))
	}
	if p.UpdateSource != nil && *p.UpdateSource != "" {
		cmds = append(cmds, "neighbor "+p.Name+" update-source "+*p.UpdateSource)
	}
	if p.EbgpMultihop != nil {
		cmds = append(cmds, "neighbor "+p.Name+" ebgp-multihop "+strconv.Itoa(*p.EbgpMultihop))
	}
	if p.SendCommunity != nil && *p.SendCommunity != "" {
		cmds = append(cmds, "neighbor "+p.Name+" send-community "+*p.SendCommunity)
	}
	if p.MaximumRoutes != nil {
		cmds = append(cmds, "neighbor "+p.Name+" maximum-routes "+strconv.Itoa(*p.MaximumRoutes))
	}
	if p.Bfd != nil && *p.Bfd {
		cmds = append(cmds, "neighbor "+p.Name+" bfd")
	}
	if p.Description != nil && *p.Description != "" {
		cmds = append(cmds, "neighbor "+p.Name+" description "+*p.Description)
	}
	return cmds
}

func neighborCmds(n BgpNeighbor) []string {
	cmds := []string{}
	if n.PeerGroup != nil && *n.PeerGroup != "" {
		cmds = append(cmds, "neighbor "+n.Address+" peer group "+*n.PeerGroup)
	}
	if n.RemoteAs != nil {
		cmds = append(cmds, "neighbor "+n.Address+" remote-as "+strconv.Itoa(*n.RemoteAs))
	}
	if n.Description != nil && *n.Description != "" {
		cmds = append(cmds, "neighbor "+n.Address+" description "+*n.Description)
	}
	return cmds
}

func addressFamilyCmds(af BgpAddressFamily) []string {
	cmds := make([]string, 0, 1+len(af.Activate)+len(af.Deactivate)+1)
	cmds = append(cmds, "address-family "+af.Name)
	for _, pg := range af.Activate {
		cmds = append(cmds, "neighbor "+pg+" activate")
	}
	for _, pg := range af.Deactivate {
		cmds = append(cmds, "no neighbor "+pg+" activate")
	}
	cmds = append(cmds, "exit")
	return cmds
}

func bgpVrfCmds(v BgpVrf) []string {
	cmds := []string{"vrf " + v.Name}
	if v.Rd != nil && *v.Rd != "" {
		cmds = append(cmds, "rd "+*v.Rd)
	}
	if v.RouteTargetImportEvpn != nil && *v.RouteTargetImportEvpn != "" {
		cmds = append(cmds, "route-target import evpn "+*v.RouteTargetImportEvpn)
	}
	if v.RouteTargetExportEvpn != nil && *v.RouteTargetExportEvpn != "" {
		cmds = append(cmds, "route-target export evpn "+*v.RouteTargetExportEvpn)
	}
	if v.RouterId != nil && *v.RouterId != "" {
		cmds = append(cmds, "router-id "+*v.RouterId)
	}
	for _, r := range v.Redistribute {
		cmds = append(cmds, "redistribute "+r)
	}
	cmds = append(cmds, "exit")
	return cmds
}

// routerBgpRow is the parsed live state we care about. v0 captures
// the same surface as Args.
type routerBgpRow struct {
	Asn                  int
	RouterId             string
	NoDefaultIpv4Unicast bool
	MaximumPaths         *BgpMaximumPaths
	Bfd                  bool
}

// readRouterBgp returns the parsed router-bgp block, or (false, nil)
// when no `router bgp` is configured. v0 read parses only the
// top-level scalars; declaration arrays (peer-groups / neighbors / AFs
// / VRFs) are echoed back from the resource Args because the Pulumi
// engine reconciles drift on the structured fields, not on the parsed
// running-config view.
func readRouterBgp(ctx context.Context, cli *eapi.Client) (routerBgpRow, bool, error) {
	resp, err := cli.RunCmds(ctx,
		[]string{"show running-config | section router bgp"},
		"text")
	if err != nil {
		return routerBgpRow{}, false, err
	}
	if len(resp) == 0 {
		return routerBgpRow{}, false, nil
	}
	out, _ := resp[0]["output"].(string)
	if !strings.Contains(out, "router bgp ") {
		return routerBgpRow{}, false, nil
	}
	return parseRouterBgpSection(out), true, nil
}

// parseRouterBgpSection extracts top-level scalars. Exposed for tests.
func parseRouterBgpSection(out string) routerBgpRow {
	row := routerBgpRow{}
	for raw := range strings.SplitSeq(out, "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(line, "router bgp "):
			if v, err := strconv.Atoi(strings.TrimPrefix(line, "router bgp ")); err == nil {
				row.Asn = v
			}
		case strings.HasPrefix(line, "router-id "):
			row.RouterId = strings.TrimPrefix(line, "router-id ")
		case line == "no bgp default ipv4-unicast":
			row.NoDefaultIpv4Unicast = true
		case strings.HasPrefix(line, "maximum-paths "):
			row.MaximumPaths = parseBgpMaximumPaths(strings.TrimPrefix(line, "maximum-paths "))
		case line == "bfd":
			row.Bfd = true
		}
	}
	return row
}

func parseBgpMaximumPaths(s string) *BgpMaximumPaths {
	tokens := strings.Fields(s)
	if len(tokens) == 0 {
		return nil
	}
	paths, err := strconv.Atoi(tokens[0])
	if err != nil {
		return nil
	}
	out := &BgpMaximumPaths{Paths: paths}
	for i := range len(tokens) - 1 {
		if tokens[i] == "ecmp" {
			if v, err := strconv.Atoi(tokens[i+1]); err == nil {
				out.Ecmp = &v
			}
		}
	}
	return out
}

func (r routerBgpRow) fillState(s *RouterBgpState) {
	if r.Asn > 0 {
		s.Asn = r.Asn
	}
	if r.RouterId != "" {
		v := r.RouterId
		s.RouterId = &v
	}
	if r.NoDefaultIpv4Unicast {
		v := true
		s.NoDefaultIpv4Unicast = &v
	}
	if r.MaximumPaths != nil {
		mp := *r.MaximumPaths
		s.MaximumPaths = &mp
	}
	if r.Bfd {
		v := true
		s.Bfd = &v
	}
}
