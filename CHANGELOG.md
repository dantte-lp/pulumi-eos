# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning 2.0.0](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `eos:l2:Vlan` resource (S5): full CRUD lifecycle (`Create` / `Read` /
  `Update` / `Delete` / `Diff`) over eAPI configuration sessions, using
  the `internal/client/eapi.Session` semaphore-protected commit/abort
  primitives. Idempotent re-apply verified.
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
- `cmd/pulumi-eos-gen`: SDK / docs / schema generator skeleton.
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
  - `eos:l3` — `Loopback`, `Vrf`, `Interface`, `StaticRoute`,
    `RouterBgp` (peer-groups, per-AF, per-VRF, RD/RT, RCF), `RouterOspf`,
    `RouterIsis`, `Bfd`, `RoutingPolicy`, `Rcf`, `Rpki`, `GreTunnel`,
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

### Changed

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
