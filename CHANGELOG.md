# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning 2.0.0](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Python probe-audit harness under `tools/probe_audit/` (commit
  `8d7ea48`): stdlib-only Python (uv + ruff + ty) that drives every
  l3 resource's full Args-render surface against live cEOS via
  direct eAPI, commit-terminated per probe per docs/05-development.md
  rule 2b. Catches silent-no-op bugs that the Go integration body
  intentionally trims for run-time. Make targets: `make probe-audit`,
  `make probe-audit-only ONLY=<X>`, `make probe-audit-lint`,
  `make probe-audit-fmt`. Last run: 93/94 surface lines accepted
  across 13 l3 surfaces.
- Rule 2c in `docs/05-development.md` — cross-resource keyword
  audit on every l3 ship.

- `eos:l3:Vrrp` resource v0 (S6, Tier 2 #16, commit `9d28327`): VRRP
  virtual router on an EOS interface, inline-form `vrrp <vrid>
  <subcommand>` per User Manual §17. v0 surface covers identity
  (`interface` + `vrid` PK pair), `virtualAddresses` (IPv4 primary +
  `secondary` IPv4s + IPv6), `priority` (renders as
  `priority-level`), `preempt` + `preemptDelayMinimum`,
  `timersAdvertise` (renders as `advertisement interval`),
  `description` (renders as `session description`), `bfdPeer`
  (renders as `bfd ip`), `shutdown` (renders as `disabled`).
  EOS keyword corrections caught at integration time and noted in
  the resource's package comment + parser (legacy forms retained
  for forward-compat parsing). `tracks` is part of the input shape
  but only renders when an `eos:l3:Tracker` resource is shipped
  (S6 close-out follow-up — the EOS keyword is `tracked-object
  <NAME>` against the global `track` namespace, not an interface
  name).
- Probe-rule lessons (rule 2b reinforcement): all per-keyword
  corrections above were caught only after switching probes from
  `Abort`-terminated to the rule-2b helpers
  (`ProbeOnePerCmd` / `ProbeFullBody`). Documented inline in
  `internal/resources/l3/vrrp.go` so future audits can trace each
  keyword to the §-reference that justified it.
- `eos:l3:GreTunnel` resource v0 (S6, Tier 2 #15, commit `d2ee58a`):
  EOS GRE tunnel interface (`interface Tunnel<id>`). v0 surface
  covers identity (id 0..65535), encapsulation mode (gre |
  mpls-gre | mpls-over-gre | ipsec), underlay (source / destination
  IPv4, underlayVrf, tos, key, mssCeiling, pathMtuDiscovery,
  dontFragment), and overlay (ipAddress CIDR, mtu, vrf, description,
  shutdown). Render uses simple re-emit (no `default interface`
  reset) — EOS' session diff applies the minimum delta and the
  interface does not flap on Update. Optional fields without a value
  are silently skipped (set to empty string / 0 to emit `no <field>`
  via Args; the v0 input shape relies on Pulumi's nil-vs-set
  semantics).
  Deferred to v1: `tunnel ttl <N>`, `tunnel source <interface>`,
  `tunnel ipsec profile <name>` (rejected on cEOSLab platform
  validation; production EOS supports them — Manual §29).
  Lab quirk: `tunnel dont-fragment` is in the input shape but the
  integration test does not exercise it because cEOSLab can mark it
  "Unavailable on this hardware platform" when staged with stale
  Tunnel<id> session state. Production EOS hardware does not exhibit
  the issue.
  Source: EOS User Manual §29 (GRE Tunneling); cEOS 4.36.0.1F live
  probe (commit `3c13006`); double validation per
  docs/05-development.md rule 2 + new rule 2b (probes terminate
  with `commit`, not `abort`).
- `keywordShutdown` / `keywordNoShutdown` constants in
  `internal/resources/l3/types.go` to share the `shutdown` /
  `no shutdown` admin-state CLI keywords across the growing l3
  resource set (RouterOspf, GreTunnel, Subinterface) per goconst
  policy.
- `eos:l3:RouterOspf` resource v0 (S6, Tier 2 #14, commit `a059ec1`):
  EOS OSPFv2 process under `router ospf <instance> [vrf <vrf>]`. v0
  surface covers the day-zero leaf-spine fabric set: process / vrf
  identity, routerId, shutdown, maxLsa, maximumPaths,
  autoCostReferenceBandwidth, networks [{prefix, area}], areas
  [{id, type:stub|stub-no-summary|nssa|nssa-default-information-originate,
  defaultCost, ranges, nssaMetric, nssaMetricType, nssaOnly}],
  redistribute [{source:connected|static|bgp|isis, routeMap}],
  defaultInformationOriginate, summaryAddresses, distance, timers
  (spf delay initial / out-delay / pacing flood), gracefulRestartHelper,
  logAdjacencyChanges, passiveInterfaceDefault + (no)passiveInterfaces.
  Render uses negate-then-rebuild inside one config-session — stale
  area / network / redistribute lines are guaranteed evicted before
  re-emit, while EOS' session diff applies the minimum delta (no
  process restart). Area ids canonicalised to dotted-quad on render
  to match cEOS' running-config; distance values rendered as three
  separate lines (single-line form rejected on 4.36 — probe-verified).
  Deferred to v1 (probe-rejected on cEOS 4.36): `area X nssa no-
  redistribution`, mixed `nssa no-summary default-information-
  originate`, `bfd all-interfaces` (different render form),
  `graceful-restart restart-period N`, `area virtual-link`, `area
  filter prefix-list`, OSPFv3 (separate `eos:l3:RouterOspfv3`).
  Source: EOS User Manual §16.2 (OSPFv2 Commands); cEOS 4.36.0.1F
  live probe (commit `3c13006`); double validation per
  docs/05-development.md rule 2.
- `eos:device:Device` unit + integration tests (audit-gap close):
  `internal/resources/device/device_test.go` pins the Delete-no-op
  contract; `test/integration/device_test.go` pins the `show
  version` JSON schema (modelName / serialNumber / version /
  systemMacAddress / hardwareRevision must remain non-empty
  strings) so a silent eAPI schema change does not zero device
  facts during refresh.
- `eos:l3:Rpki` resource (S6, Tier 2 #13, commit `1e93356`): EOS BGP
  RPKI cache. Composes with `router bgp <asn>` independently of
  `eos:l3:RouterBgp` — multiple caches per ASN supported (canonical
  redundant 2-cache pattern from the EOS BGP RPKI Origin Validation
  Design Guide). Args: `name` + `bgpAsn` (composite PK), `cacheHost`
  (IPv4-only validator), `vrf`, `port` (1..65535), `preference`
  (1..10), `refreshInterval`/`retryInterval`/`expireInterval`,
  `localInterface`, `transport` (`tcp`|`ssh`). Render path emits
  `rpki cache <name>` block under `router bgp <asn>`. Parser keys
  rows by ASN+name to avoid cross-matching across `router bgp`
  blocks. Source: EOS User Manual §32.4 (BGP Origin Validation);
  TOI 12048.
- `eos:l3:Rcf` resource (S6, Tier 2 #12, commit `8e02a46` — supersedes
  the v0 `fbdfbb5`): EOS Routing Control Function code unit, the
  programmable alternative to route-maps. Three mutually-exclusive
  delivery modes:
  - `Code` — inline RCF source pushed via the eAPI rich-command form
    (`{cmd: "code unit X", input: "<source>\nEOF\n"}`) per EOS Command
    API Guide §1.2.3 + TOI 19238. Pulumi-native primary path.
  - `SourceFile` — pre-staged path on flash, emits
    `code [unit X] source pulled-from <storage:path>`.
  - `SourceUrl` — `pull [unit X] replace <url>` over http / https /
    ftp / tftp / scp.
  Foundational eapi additions: `eapi.Command{Cmd, Input}` rich-command
  type, `eapi.Client.RunCmdsRich(...)`, `eapi.Session.StageRich(...)`.
  Bypasses goeapi v1.0.0's `[]string`-only `RunCommands` (no `input`
  field) by crafting JSON-RPC directly. Unblocks any future resource
  needing multi-line stdin (banner, comment, on-box config-replace).
  Source: EOS User Manual §16.8; TOI 15102; TOI 19238; TOI 15859.
- `eos:l3:AsPathAccessList` resource (S6, Tier 2 #11, commit
  `888a110`): EOS BGP AS-path access-list. Closes the RoutingPolicy
  5/5 decomposition. Args: `name` (PK), `entries [{action, regex}]`.
  Discovery: cEOS auto-appends an `any` AS-set type token to every running-
  config entry; the parser strips it so `state.Entries[].Regex`
  matches user input verbatim. Source: EOS User Manual §15.6.
- `eos:l3:ExtCommunityList` resource (S6, Tier 2 #10, commit
  `51416dc`): EOS BGP extended-community-list. Args: `name` (PK),
  `listType` (standard|regexp), `entries [{action, type rt|soo for
  standard, value}]`. Mirrors CommunityList's eAPI quirk — cEOS uses
  `regexp` (not `expanded` per User Manual). Standard entries carry
  a `type` prefix (`rt`/`soo`) on the wire; regexp entries do not.
  Source: EOS User Manual §15.5; TOI 13855.
- `eos:l3:CommunityList` resource (S6, Tier 2 #9, commit `1566544`):
  EOS BGP community-list. Args: `name` (PK), `type` (standard
  default | regexp), `entries [{action, value}]`. Standard values
  validate against the well-known keyword set
  (`internet`/`local-as`/`no-advertise`/`no-export`/`GSHUT`),
  `aa:nn` pairs, or numeric 0..4294967040. Discovery: cEOS 4.36 uses
  `regexp` keyword (not `expanded` as the EOS User Manual §15.5
  documents); the resource always renders the form the device
  accepts. Source: EOS User Manual §15.5; TOI 14078.
- `eos:l3:RouteMap` resource (S6, Tier 2 #8, commits `d50fc2a` +
  `72c7db5`): EOS named route-map with sequenced match/set clauses.
  Composes with PrefixList / CommunityList / ExtCommunityList /
  AsPathAccessList and BGP peer-group inbound/outbound filters.
  v0 surface — match (ipAddressPrefixList, community, extcommunity,
  asPath, interface, tag, metric, localPreference, origin igp/egp/
  incomplete, sourceProtocol connected/static/isis/ospf/bgp/rip) +
  set (community + additive/none, extcommunity rt + additive,
  localPreference, metric N|+N|-N delta, asPathPrepend, ipNextHop
  ip|unchanged|self, origin, tag) + continue. The
  `extcommunityRtAdditive` flag (`72c7db5`) closed the audit gap
  documented in STATUS Open commitments: catalog § Routing-policy
  match/set listed `set extcommunity rt <rt> [additive]` but v0
  rendered the bare form only. Source: EOS User Manual §15.6;
  TOI 14078; TOI 13855; TOI 14045.
- `eos:l3:PrefixList` resource (S6, Tier 2 #7, commit `11a4cd6`): IPv4
  prefix-list with named sequenced entries. Args: `name` (PK), `remark`,
  `entries [{seq, action permit|deny, prefix CIDR, eq | ge[le] | le}]`.
  Validation: seq 0..65535 (unique within list), v4-only CIDR, mask range
  1..32, eq XOR ge/le, ge ≤ le, ge ≥ prefix length. Update uses
  negate-then-rebuild so stale `seq N` lines cannot leak across versions.
  Read parses `show running-config | grep prefix-list` (single-word EOS
  pipe-grep) + filter `ip prefix-list <name>` lines client-side.
  Catalog change: `eos:l3:RoutingPolicy` row decomposed into 5 atomic
  resources (`PrefixList`, `RouteMap`, `CommunityList`, `ExtCommunityList`,
  `AsPathAccessList`); l3 catalog 16 → 20, total 94 → 98. Source: EOS
  User Manual §15.4.1.61.
- `eos:l3:RouterBgp` v0 resource (S6, Tier 2 #6, commit `1574102`):
  global EOS BGP routing instance. v0 surface scoped to leaf-spine
  EVPN/VXLAN demo (S6 exit-criterion). Args: `asn` (PK), `routerId`,
  `noDefaultIpv4Unicast`, `maximumPaths {paths, ecmp}`, `bfd`, plus 4
  nested arrays — `peerGroups` (name, remoteAs, updateSource,
  ebgpMultihop, sendCommunity, maximumRoutes, bfd, description),
  `neighbors` (address IPv4 PK, peerGroup XOR remoteAs, description),
  `addressFamilies` (name ipv4|ipv6|evpn, activate / deactivate per-PG
  lists), `vrfs` (name, rd, routeTargetImport/ExportEvpn, routerId,
  redistribute connected|static|attached-host). Render order matches
  `show running-config` canonical emit order; sub-arrays sorted on
  render for stable diffs. Update re-emits the full block; EOS' session
  diff computes the minimum delta — destructive negate-then-rebuild
  deliberately avoided to prevent dynamic-peer flap during commit.
  Higher-fidelity knobs (RCF, route-maps in PG, dampening,
  graceful-restart, RPKI, per-VRF-AF redistribute filters) follow when
  consumers require them. Source: EOS User Manual §16; TOI 14091.
- `eos:l3:StaticRoute` resource (S6, Tier 2 #5, commit `908af76`):
  IPv4 static routes. Composite identity (vrf + prefix + nextHop +
  distance) lets ECMP and floating-route sets coexist as independent
  Pulumi rows. Args: `prefix` (PK), `nextHop` (PK, IPv4 or interface
  form), `vrf` (PK when set), `distance` (PK, 1..255 default 1), `tag`
  (0..2^32-1), `name` (no spaces), `metric` (>0), `track` (no spaces).
  Next-hop accepts `Null0`, `Ethernet<N>[/<m>...][.<sub>]`,
  `Loopback<N>`, `Management<N>`, `Port-Channel<N>[.<sub>]`, `Vlan<N>`,
  `Vxlan<N>`. Discovery: EOS pipe-grep accepts only single-word
  substring — `grep ^ip route` and `grep "ip route"` both return
  empty; read uses `grep ip` + client-side filter. Source: EOS User
  Manual §14.1.14.65.
- `eos:l3:Subinterface` resource (S6, Tier 2 #4, commit `8b2dd6e`):
  802.1Q L3 sub-interface (`Ethernet<N>.<sub>`,
  `Port-Channel<N>.<sub>`). Args: `name` (PK, regex matched),
  `encapsulationVlan` (1..4094 required), `description`, `ipAddress`
  (IPv4 CIDR), `ipv6Address` (IPv6 CIDR), `vrf`, `mtu`, `shutdown`,
  `bfd {interval, minRx, multiplier}`. Catalog rename:
  `eos:l3:Interface (routed)` → `Subinterface` (avoids name clash with
  `eos:l2:Interface`; parent's `no switchport` is owned by L2). Source:
  EOS User Manual §13.7; TOI 13633; TOI 17032.
- `eos:l3:Bfd` singleton (S6, Tier 2 #3, commit `eea0e74`): global
  EOS BFD settings configured under `router bfd` (modal CLI introduced
  in EOS 4.22.0F, TOI 14641). Args: `interval` (ms), `minRx` (ms),
  `multiplier` (3..50), `slowTimer` (ms), `shutdown`. Validation:
  interval/minRx/multiplier are bound — set all three or none. cEOS
  4.36 quirk: `interval N min-rx N multiplier N` requires the trailing
  `default` profile selector — bare form rejected with "Incomplete
  command"; render emits the selector accordingly. Per-interface and
  per-peer BFD knobs live on `eos:l2:Interface` /
  `eos:l3:Subinterface` / `eos:l3:RouterBgp`.
- `eos:l3:Vrf` resource (S6, Tier 2 #2, commit `c335d22`): EOS VRF
  instance plus the global `(ip|ipv6) routing vrf <name>` toggles.
  Args: `name` (PK, "default" reserved), `description`, `ipRouting`
  (default true), `ipv6Routing` (default false). RD/RT belong to
  `eos:l3:RouterBgp` per EOS BGP/MPLS L3 VPN TOI 14091, not to Vrf.
- `eos:l3:Loopback` resource (S6, Tier 2 #1, commit `5dcc4df`): EOS
  Loopback interface. Args: `number` (PK, 0..1000), `ipAddress` (IPv4
  CIDR), `ipv6Address` (IPv6 CIDR), `vrf`, `description`, `shutdown`.
  Validation: requires at least one of v4/v6; rejects 4-in-6;
  `netip.ParsePrefix` for both families. Anchor for BGP router-id,
  EVPN VTEP source, BFD multihop sessions.
- Tier 1 closeout — minimum gNMI client (commit `363f31e`):
  `internal/client/gnmi/` exposes `Dial`, `Capabilities`, `Get`, and
  the `name[k=v]` path parser (10 unit tests + 1 cEOS Capabilities
  integration test, gNMI 0.7.0 / 288 supported models confirmed).
  `config.GNMIClient(ctx, host?, user?, pass?)` factory plumbed.
  `scripts/integration-bootstrap.sh` enables `management api gnmi
  transport grpc default port 6030` so cEOS serves gNMI on the port
  already mapped to host 18830. Set/Subscribe deferred until first
  consumer (S6 RouterBgp drift, S9 gNOI).
- Tier 1 — `eos:l2:MacAddressTable` (commit `fb0be36`): read-only
  data source via `infer.Function` (token `eos:l2:macAddressTable`).
  Filters by VLAN id and entry type (dynamic / static / all).
  Per-call host / username / password overrides on top of provider
  config. Source: EOS Command API Guide §6.
- Tier 1 — `eos:device:RawCli` (commit `f4adb59`): diff-driven
  idempotent escape hatch. Inspects
  `show session-config named <name> diffs` and aborts the session
  whenever the diff is empty — re-applies against an already
  converged device never touch running-config. Optional inverse
  `deleteBody` applied on Delete.
- Tier 1 — `eos:device:Configlet` (commit `be4d732`): atomic raw
  CLI block via configuration session. SHA-256 digest of the
  canonicalised body exposed as state for drift detection. Closes
  the S4 lacuna documented in `docs/STATUS.md`.
- `eos:l2:Vlan` resource (S5): full CRUD lifecycle (`Create` / `Read` /
  `Update` / `Delete` / `Diff`) over eAPI configuration sessions, using
  the `internal/client/eapi.Session` semaphore-protected commit/abort
  primitives. Idempotent re-apply verified.
- `eos:l2:PortChannel` resource (S5): logical Port-Channel CRUD. Args:
  `id` (1..2000), `description`, `mtu`, `shutdown`, `switchport*`,
  `lacpFallback` (`static` / `individual`), `lacpFallbackTimeout`. Shares
  switchport semantics with `eos:l2:Interface` via `SwitchportFields`.
  Sources: EOS User Manual §11.2.5, §11.2.5.18.
- `internal/resources/l2/switchport.go`: shared switchport helpers
  (`SwitchportFields`, `validateSwitchport`, `buildSwitchportCmds`,
  `parseSwitchportLine`, `fillSwitchport`) consumed by both `Interface`
  and `PortChannel`. `interface.go` refactored to use the helpers; tests
  updated to point at the shared sentinel errors.
- `internal/resources/device/device.go`: `readFacts` now performs a real
  `show version` and maps `modelName` / `serialNumber` / `hardwareRevision`
  / `version` / `systemMacAddress` into the resource state.
- `eos:l2:Interface` resource (S5): physical (Ethernet) and Management
  interface CRUD. Args: `name` (PK), `description`, `mtu`, `shutdown`,
  `switchportMode` (`access` / `trunk` / `routed` → `no switchport`),
  `accessVlan`, `trunkAllowedVlans`, `trunkNativeVlan`, nested
  `channelGroup` (id + LACP `mode`). Mutually-exclusive validation
  enforced (access / trunk fields cross-checked against mode). Delete
  resets to defaults via `default interface <name>` since physical
  interfaces persist in hardware.
- `eos:l2:VlanInterface` resource (S5): SVI / `interface VlanN`. Args:
  `vlanId`, `vrf`, `ipAddress` (regular), `ipAddressVirtual` (anycast,
  mutually exclusive with `ipAddress`), `mtu`, `description`,
  `noAutostate`, `shutdown`. Read parses `show running-config interfaces
  Vlan<id>` because the structured JSON omits SVI-specific fields like
  `ip address virtual` and `vrf`.
- `internal/resources/l2/vlan_test.go` unit tests: vlan-id range guard
  (1..4094), `buildVlanCmds` table tests, id-formatter sanity.
- `internal/resources/l2/vlan_interface_test.go` unit tests:
  `validateVlanInterface` (range, address conflict), `buildVlanInterfaceCmds`
  table, `parseVlanInterfaceConfig` round-trip.
- `test/integration/vlan_test.go` build-tagged integration test:
  end-to-end Create → Read → Update → idempotent re-apply → Delete →
  Read-after-delete against cEOS 4.36.0.1F.
- `test/integration/vlan_interface_test.go` build-tagged integration
  test: vlan + SVI Create → Read → Update → Delete cycle on cEOS.
- `internal/config` package: extracted `Config`, `Annotate`, `Configure`,
  and the `EAPIClient` factory out of `internal/provider` so resource
  packages can derive clients without an import cycle.
- Repository scaffold: project layout (`golang-standards/project-layout`),
  Makefile, dev container, Compose Specification dev stack.
- `cmd/pulumi-resource-eos`: entry point wiring the binary to the
  `pulumi-go-provider` infer framework via `provider.New().Run`. `-version`
  flag plus engine-driven gRPC server.
- `internal/provider`: provider config (eAPI + CVP planes, secrets,
  retry policy), validation, provider builder with namespace `eos`,
  Apache-2.0 metadata, language map for Go / Python / TypeScript.
- `internal/client/eapi`: goeapi-backed client with named configuration
  sessions, channel-based 1-slot semaphore, `commit timer`
  confirmed-commit, `Diff`, and `Abort`.
- `internal/client/cvp`: gRPC client with bearer-token credentials, TLS 1.3
  minimum, optional CA pinning via PEM bundle.
- `internal/resources/device`: `eos:device:Device` canary resource
  (read-only facts; real `readFacts` lands in S5).
- `internal/version` package, ldflags-injected.
- `pkg/eos` public-types placeholder.
- `go.mod`: `github.com/aristanetworks/goeapi v1.0.0`,
  `github.com/pulumi/pulumi-go-provider v1.3.2`,
  `github.com/pulumi/pulumi/sdk/v3 v3.232.0`,
  `google.golang.org/grpc v1.80.0`. Go 1.26.2.
- `deployments/compose/compose.integration.yml`: Compose Specification
  stack bringing up Arista cEOS Lab 4.36.0.1F (privileged +
  SYS_ADMIN/NET_ADMIN caps + systemd PID-1); eAPI ports 18443/18080/18830.
- `scripts/integration-bootstrap.sh`: applies the minimal eAPI bootstrap
  config (admin user, `management api http-commands`) and waits until eAPI
  is reachable.
- `test/integration/eapi_test.go`: build-tag-gated `integration` tests
  (`show version`, configuration-session abort and release-slot canary).
- `Makefile`: `test-integration`, `test-integration-up`,
  `test-integration-down`, `test-integration-logs`.
- `golangci-lint v2.11.4` configuration: allowlist mode, severity-tiered,
  ~70 linters enabled.
- `markdownlint-cli2`, Mermaid render-lint, `yamllint`, `cspell`,
  `commitlint` configurations and helpers.
- Podman + podman-compose + podman-py automation in
  `scripts/automation/build.py`.
- Vulnerability audit (`govulncheck` + `osv-scanner`) with allowlist
  support.
- CI workflows: `ci.yml` (build · test · lint-go · lint-docs · vulncheck
  · trivy · commitlint), `security.yml` (CodeQL · gosec · weekly
  schedule), `scorecard.yml`, `release.yml`.
- `goreleaser` configuration for cross-platform binaries, SBOM (syft),
  checksum, cosign signing.
- Dependabot configuration for `gomod`, `github-actions`, `docker`, `pip`.
- Documentation set: architecture, implementation plan (waterfall ·
  12 sprints · 6 phases), resource catalog, provider configuration
  schema preview, development workflow, testing matrix, release process,
  research references, status dashboard.
- `docs/03-resource-catalog.md`: resource families per the EOS Supported
  Features Matrix 4.35.0F:
  - `eos:device` — `Device`, `Configlet`, `RawCli`, `OsImage`, `Reboot`,
    `Certificate`.
  - `eos:l2` — `Vlan`, `VlanRange`, `VlanInterface`, `Interface`,
    `PortChannel`, `EvpnEthernetSegment`, `Mlag`, `VxlanInterface`,
    `MacAddressTable`, `Varp`, `Stp`, `Dot1x`, `Mab`, `Pvlan`, `Cfm`,
    `StormControl`.
  - `eos:l3` — `Loopback`, `Vrf`, `Subinterface`, `StaticRoute`,
    `RouterBgp` (peer-groups, per-AF, per-VRF, RD/RT, RCF), `RouterOspf`,
    `RouterIsis`, `Bfd`, `PrefixList`, `RouteMap`, `CommunityList`,
    `ExtCommunityList`, `AsPathAccessList`, `Rcf`, `Rpki`, `GreTunnel`,
    `Vrrp`, `PolicyBasedRouting`, `Nat`, `ResilientEcmp`.
  - `eos:multicast` — `Igmp`, `IgmpSnooping`, `Pim`, `AnycastRp`, `Msdp`,
    `MulticastRoutingTable`.
  - `eos:security` — `IpAccessList`, `Ipv6AccessList`, `MacAccessList`,
    `RoleBasedAccessList`, `UserAccount`, `Role`, `AaaServer`,
    `AaaAuthentication`, `SslProfile`, `MacSecProfile`, `MacSecBinding`,
    `ControlPlanePolicing`, `Urpf`, `DhcpRelay`, `DhcpSnooping`,
    `DynamicArpInspection`, `IpSourceGuard`, `ServiceAcl`, `ArpRateLimit`.
  - `eos:qos` — `ClassMap`, `PolicyMap`, `ServicePolicy`, `QosMap`,
    `PriorityFlowControl`, `BufferProfile`.
  - `eos:management` — `ManagementInterface`, `Hostname`, `NtpServer`,
    `DnsServer`, `Logging`, `Snmp`, `Sflow`, `Telemetry`, `EApi`,
    `EventMonitor`, `PortMirror`.
  - `eos:cvp` — `Workspace`, `Studio`, `Configlet`, `ChangeControl`,
    `Tag`, `Device`, `Inventory`, `ServiceAccount`, `IdentityProvider`,
    `ImageBundle`, `Compliance`, `Alert`.
- `docs/02-implementation-plan.md` §3.2 / §5: sprint scope (S5 — S9)
  expanded to match the resource catalog; S4 marked done.
- `docs/09-go-style.md`: Go 1.26 patterns / antipatterns / project
  standards. Adopts `new(value)`, `strings.SplitSeq`, `errors.AsType[T]`,
  `slog.NewMultiHandler`, `os/signal.NotifyContext` cancel-cause,
  `t.ArtifactDir()`, `crypto/sha3` zero values, TLS post-quantum hybrids.
  Bans pointer-literal `&v` ceremony, `strings.Split` in `for-range`,
  `math/rand` v1, log/log, plain `tls.Config{InsecureSkipVerify: true}`,
  predeclared-name parameters, holding `sync.Mutex` across method
  boundaries. References Green Tea GC, heap address randomization, and
  the `goroutineleak` profile (dev-only).
- `docs/STATUS.md`: live progress dashboard mapping commits to phases and
  sprints; per-gate quality status; open commitments.
- `LICENSE` (Apache-2.0), `SECURITY.md`, `CONTRIBUTING.md`, `CODEOWNERS`,
  PR template, issue templates.

### Removed

- `eos:l3:RouteMap` `Match.Origin` field (commit `8d7ea48`). EOS
  does not accept `match origin igp|egp|incomplete` — the keyword
  is rejected with "Incomplete token" by every cEOS variant.
  The Cisco IOS-style `match origin` clause has no EOS equivalent;
  use `match route-type internal|external|local|confederation-
  external` (TOI 13916) for routing-source filtering. The
  `set origin` path is unaffected.
- `cmd/pulumi-eos-gen` placeholder binary. Will be reintroduced as a
  fully-implemented tool when SDK / schema generation enters the active
  sprint.
- All references to private / external production deployments scrubbed
  from `docs/03-resource-catalog.md`, `docs/02-implementation-plan.md`,
  `docs/STATUS.md`. Citations now point exclusively to the EOS User
  Manual, per-feature TOIs, the EOS Supported Features Matrix 4.35.0F,
  and IETF RFCs.

### Changed

- `eos:l3:GreTunnel` mode set narrowed from
  `{gre, mpls-gre, mpls-over-gre, ipsec}` to `{gre, ipsec}` (commit
  `8d7ea48`). cEOSLab rejects `mpls-gre` and `mpls-over-gre` at
  commit (TOI 18464 — pseudowire modes require MPLS fabric
  context). A future `eos:l3:GreTunnelMpls` resource will model the
  pseudowire surface alongside MPLS forwarding pieces.
- 7-standards audit (commit `cabe86c`): close the
  Keep-a-Changelog 1.1.0 + Conventional Commits 1.0.0 +
  containers.conf.5 audit. CHANGELOG re-aligned to actual repo
  state. `.commitlintrc.yaml` scope-enum extended with
  `client / status / audit / verify / plugin / multicast / qos`
  (used in 7 recent commits). New
  `deployments/containers/containers.conf` pins userns=keep-id,
  default capabilities, init=true, network_backend=netavark,
  pull_policy=missing, compose_providers=[podman-compose],
  stop_timeout=10. Apply via
  `export CONTAINERS_CONF=$(pwd)/deployments/containers/containers.conf`.
- `docs/05-development.md` and `docs/06-testing.md` (commits
  `fabf547`, `7ee0fd7`): mandatory per-resource verification rules
  documented; `make verify` introduced as the canonical pre-commit
  gate (= `all` + `test-integration-keep` — keeps cEOS running across
  iterations to amortise the ~60 s bring-up). `make
  test-integration-keep` is the new no-tear-down variant; the
  original `make test-integration` retains the bring-up/tear-down
  shape for CI. `lint-spell` routed through the dev container.
  Plan §4 quality-gate row + STATUS quality-gates updated.
- `.golangci.yml` (commit `0c746ab`): expanded from 74 to **83
  linters**. Added: `embeddedstructfieldcheck`, `forbidigo`,
  `funcorder`, `goconst`, `godox`, `gomodguard`, `iotamixing`,
  `nestif`, `paralleltest`. `gosec` settings hardened to the
  official reference (severity: low, confidence: low, audit-mode,
  G301/G302/G306 file-mode policy). Severity `error` tier extended
  to forbidigo / gomodguard / iotamixing / paralleltest.
  `internal/client/eapi/client.go` and `internal/config/config.go`
  reordered for `funcorder` compliance (unexported methods after
  exported).
- Catalog `eos:l3` row `RoutingPolicy` (commit `11a4cd6`):
  decomposed into 5 atomic resources (`PrefixList`, `RouteMap`,
  `CommunityList`, `ExtCommunityList`, `AsPathAccessList`) matching
  EOS CLI structure; cross-references between them work without
  circular Pulumi resource dependencies. l3 catalog 16 → 20, total
  94 → 98.
- `.commitlintrc.yaml` scope-enum extended with
  `client / status / audit / verify / plugin / multicast / qos`
  to cover scopes used in recent commits.
- `deployments/containers/Containerfile.dev`: drop `mmdc` (Mermaid CLI)
  from the dev container — Chromium-deps bloat. `make lint-mmd` runs mmdc
  on the host or in CI's `lint-docs` job.
- `.cspell.json`: ~80 networking-domain words added to the dictionary.
- `deployments/compose/compose.integration.yml`: `pull_policy: never` on
  the cEOS service to keep podman-compose from treating `localhost/` as a
  registry hostname.
- `internal/provider`: now contains only the inferred-provider
  entry-point; `Config` and client factories moved to `internal/config` to
  break the resource → provider import cycle.

[Unreleased]: https://github.com/dantte-lp/pulumi-eos/compare/HEAD...HEAD
