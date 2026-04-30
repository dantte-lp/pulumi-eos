# Status

> Live dashboard of progress against [`docs/02-implementation-plan.md`](02-implementation-plan.md).

## Phase progress

| Phase | Sprints | State | Evidence |
|---|---|---|---|
| Phase 1 — Requirements | S1 | done | `docs/02-implementation-plan.md`, `docs/08-research-references.md` |
| Phase 2 — Design | S2 — S3 | done (artefacts) | `docs/01-architecture.md`, `docs/03-resource-catalog.md`, `docs/04-provider-config.md` |
| Phase 3 — Implementation | S4 — S9 | S4 done; S5 implementation done (RC tag pending); S6 in progress (4/14); S7 — S9 pending | per-sprint table below |
| Phase 4 — Verification | S10 — S11 | pending | — |
| Phase 5 — Deployment | S12 | pending | — |
| Phase 6 — Maintenance | continuous | pending | — |

## Sprint progress

| Sprint | Scope | State | Evidence |
|---|---|---|---|
| S1 — Requirements | SRS, risk register, scope | done | `docs/02-implementation-plan.md` §6, §11; `docs/08-research-references.md` |
| S2 — System design | Component diagram, sequence diagrams, transport matrix | done | `docs/01-architecture.md` |
| S3 — Detailed design | Resource catalog + field shapes, ADRs | done (catalog), schema gen pending | `docs/03-resource-catalog.md`, `docs/04-provider-config.md` |
| S4 — Foundation | Provider runtime, eAPI/CVP clients, cEOS integration, CI green; `eos:device:Configlet` lacuna | done | commits `f6ae43f`, `0117e8a`, `6565ccd`, `9fa0b40`, `eb6cdc8`, `98daa9c`, `d661199`, Configlet (`be4d732`) |
| S5 — L2 family | `Vlan`, `VlanRange`, `VlanInterface`, `Interface`, `PortChannel`, `EvpnEthernetSegment`, `Mlag`, `VxlanInterface`, `MacAddressTable`, `Varp`, `Stp`; `RawCli` escape; minimum gNMI client | implementation done; sprint-exit (RC tag) pending | shipped: `Vlan` (`fa20b40`), `VlanInterface` (`2a77f2e`), `Interface` (`449ea8e`), `PortChannel` + shared `SwitchportFields` (`b516f28`), `VxlanInterface` (`193a4e6`), `EvpnEthernetSegment` (`09dd4f1`), `Mlag` (`b3e2c5d`), `Stp` (`2a64a58`), `Varp` (`726b26c`), `VlanRange` (`5fa01a1`), `RawCli` (`f4adb59`), `MacAddressTable` (`fb0be36`), minimum gNMI client (`363f31e`); 16/16 cEOS integration tests pass (incl. gNMI Capabilities). RC-readiness still open: `schema.json` generation, `pulumi package gen-sdk` × 5, `v0.1.0-rc.1` tag (see Open commitments). |
| S6 — L3 family | `Loopback`, `Vrf`, `Bfd`, `Subinterface` (was "Interface (routed)" in early plan; renamed to avoid the l2:Interface name clash), `StaticRoute`, `RouterBgp` (peer-groups + per-AF + per-VRF + EVPN AF + RD/RT), `RoutingPolicy`, `Rcf`, `Rpki`, `RouterOspf`, `GreTunnel`, `Vrrp`, `PolicyBasedRouting`, `ResilientEcmp` (14 resources, see Tier 2 ordering) | in progress (4/14) | shipped: `Loopback` (`5dcc4df`), `Vrf` (`c335d22`), `Bfd` (`eea0e74`), `Subinterface` (this commit); 20/20 cEOS integration tests pass |
| S7 — Security / Mgmt / Multicast / QoS | 46 resources across `eos:management` (11), `eos:security` (19), `eos:multicast` (6), `eos:qos` (6) + L2 access (`Dot1x`, `Mab`, `Pvlan`, `StormControl`); see Tier 3a — 3g ordering | pending | — |
| S8 — CloudVision | `Workspace`, `Studio`, `ChangeControl`, `Configlet`, `Tag`, `Device`, `Inventory`, `ServiceAccount`, `IdentityProvider`, `ImageBundle`, `Compliance`, `Alert` (12 resources); post-S8: `Dashboard`, `Audit` (Tier 4) | pending | — |
| S9 — Day-2 / gNOI / drift | `OsImage`, `Reboot`, `Certificate` (Tier 5); gNOI client (separate from gNMI); gNMI `Subscribe(last-configuration-timestamp)` drift; `pulumi refresh` accuracy report | pending | — |
| S10 — System test | Matrix (cEOS 4.30 — 4.36 × CVP 2024.3 — 2026.1) + soak | pending | — |
| S11 — UAT & docs | UAT; Pulumi Registry intake | pending | — |
| S12 — Release | `v1.0.0` | pending | — |

## Quality gates (last run)

| Gate | Tool | State |
|---|---|---|
| Build | `go build ./...` (Go 1.26.2) | pass |
| Unit tests + race | `go test -race -count=1 ./...` | pass |
| Integration | `make test-integration` against cEOS 4.36.0.1F | pass |
| Go static analysis | `golangci-lint v2.11.4` (allowlist, **83 linters** incl. `gosec` audit-mode + `forbidigo` + `gomodguard` + `protogetter` + `embeddedstructfieldcheck` + `funcorder` + `goconst` + `godox` + `iotamixing` + `nestif` + `paralleltest`) | 0 issues |
| SAST | `gosec` embedded in golangci-lint (`severity: low`, `confidence: low`, `audit: true`); `semgrep p/golang` available host-side via `make semgrep` | clean |
| Vulnerability | `govulncheck v1.2.0` + `osv-scanner v2.3.5` | no vulnerabilities |
| Markdown | `markdownlint-cli2` | 0 errors |
| Mermaid | `@mermaid-js/mermaid-cli` (`mmdc`) render | 5 diagrams parsed, 0 failures |
| YAML | `yamllint` | clean |
| Spelling | `cspell` | clean |
| LSP | `gopls v0.21.1` | references resolve across packages |

## Group readiness

Per-group inventory against `docs/03-resource-catalog.md`:

| Group | Total | Shipped | Pending | % | Sprint span |
|---|---:|---:|---:|---:|---|
| `eos:device` | 6 | 3 | 3 | 50% | S4 — S9 |
| `eos:l2` | 16 | 11 | 5 | 69% | S5 — post-S9 |
| `eos:l3` | 16 | 4 | 12 | 25% | S6 — post-S9 |
| `eos:multicast` | 6 | 0 | 6 | 0% | S7 |
| `eos:security` | 19 (`RouteMap` modeled in `RoutingPolicy`) | 0 | 19 | 0% | S7 |
| `eos:qos` | 6 | 0 | 6 | 0% | S7 |
| `eos:management` | 11 | 0 | 11 | 0% | S7 |
| `eos:cvp` | 14 | 0 | 14 | 0% | S8 — post-S8 |
| **Total** | **94** | **18** | **76** | **19%** | — |

## Priority ordering (2026-04-30)

Closeout tiers — sequenced for end-to-end deployability of a leaf-spine
EVPN/VXLAN fabric.

| Tier | Scope | Resources | Why first |
|---|---|---|---|
| ~~**Tier 1 — Close S4 + S5**~~ | ~~`eos:device:Configlet`~~, ~~`eos:device:RawCli`~~, ~~`eos:l2:MacAddressTable`~~, ~~minimum gNMI client~~ — all shipped. | 0 items | Tier 1 closed; v0.1.0-rc.1 unblocked. |
| **Tier 2 — Open S6 (L3 critical path)** | ~~`Loopback`~~ → ~~`Vrf`~~ → ~~`Bfd`~~ → ~~`Subinterface`~~ (was `Interface (routed)`) → `StaticRoute` → `RouterBgp` (peer-groups + per-AF + per-VRF + EVPN AF + RD/RT) → `RoutingPolicy` → `Rcf` → `Rpki` → `RouterOspf` → `GreTunnel` → `Vrrp` → `PolicyBasedRouting` → `ResilientEcmp`. | 10 items remaining | `RouterBgp` is the single largest resource in the project; everything from underlay reachability to overlay EVPN flows through it. Sequencing `Loopback`/`Vrf`/`Bfd`/`Subinterface` first makes BGP reusable across both planes. |
| **Tier 3a — S7 management bootstrap** | `Hostname`, `ManagementInterface`, `NtpServer`, `DnsServer`, `Logging`, `EApi`. | 6 items | Required day-zero on every device; trivial shape; enables all subsequent S7 work to drive a real device through Pulumi. |
| **Tier 3b — S7 security core** | `IpAccessList`, `Ipv6AccessList`, `MacAccessList`, `RoleBasedAccessList`, `UserAccount`, `Role`, `AaaServer`, `AaaAuthentication`, `SslProfile`, `ControlPlanePolicing`, `Urpf`, `ServiceAcl`. | 12 items | Control plane and data-plane policy. ACLs are referenced by routing-policy, PBR, and CoPP, so they unlock cross-resource composition. |
| **Tier 3c — S7 access-edge (campus)** | `Dot1x`, `Mab`, `Pvlan`, `StormControl`, `DhcpRelay`, `DhcpSnooping`, `DynamicArpInspection`, `IpSourceGuard`, `ArpRateLimit`. | 9 items | Campus / access-layer. Independent of the spine/leaf path. |
| **Tier 3d — S7 MACsec** | `MacSecProfile`, `MacSecBinding`. | 2 items | DCI / dark-fiber encryption; depends on `Interface` only. |
| **Tier 3e — S7 multicast** | `Igmp`, `IgmpSnooping`, `Pim`, `AnycastRp`, `Msdp`, `MulticastRoutingTable`. | 6 items | Specialized; ship after security. |
| **Tier 3f — S7 QoS** | `ClassMap`, `PolicyMap`, `ServicePolicy`, `QosMap`, `PriorityFlowControl`, `BufferProfile`. | 6 items | Specialized; ship last in S7. |
| **Tier 3g — S7 management extras** | `Snmp`, `Sflow`, `Telemetry`, `EventMonitor`, `PortMirror`. | 5 items | Observability; lower priority than core mgmt. |
| **Tier 4 — S8 CVP/CVaaS** | `Workspace`, `Studio`, `Configlet`, `ChangeControl`, `Tag`, `Device`, `Inventory`, `ServiceAccount`, `IdentityProvider`, `ImageBundle`, `Compliance`, `Alert`; post-S8: `Dashboard`, `Audit`. | 14 items | Out-of-band orchestration plane; depends on a CVP test instance. |
| **Tier 5 — S9 Day-2 / gNOI** | `OsImage`, `Reboot`, `Certificate`. | 3 items | Operational lifecycle; gNOI client (separate from gNMI) needed first. |
| **Tier 6 — post-S9 stretch** | `eos:l3:RouterIsis`, `eos:l3:Nat`, `eos:l2:Cfm`. | 3 items | Specialized / stretch goals; not on the v1.0 critical path. |

## Open commitments

| Topic | Where | Owner |
|---|---|---|
| Generate `schema.json` via `bin/pulumi-resource-eos -schema` | S3 carry-over → S5 exit | — |
| `pulumi package gen-sdk` for Go / Python / TypeScript / .NET / Java | S5 exit | — |
| `v0.1.0-rc.1` tag | S5 exit (after Tier 1 closes) | — |
| ~~Tier 1 — `eos:device:Configlet`~~ shipped | S4 lacuna | — |
| ~~Tier 1 — `eos:device:RawCli`~~ shipped | S5 closeout | — |
| ~~Tier 1 — `eos:l2:MacAddressTable` (`infer.Function`)~~ shipped | S5 closeout | — |
| ~~Tier 1 — minimum gNMI client (`internal/client/gnmi/`)~~ shipped | S5 closeout | — |

## Repository activity

| Commit | Subject | Date |
|---|---|---|
| pending | `feat(l3): eos:l3:Subinterface — 802.1Q L3 sub-interface (was "Interface (routed)")` | 2026-04-30 |
| `eea0e74` | `feat(l3): eos:l3:Bfd singleton (router bfd timers + slow-timer + admin)` | 2026-04-30 |
| `c335d22` | `feat(l3): eos:l3:Vrf instance + per-VRF routing toggles` | 2026-04-30 |
| `79b4945` | `docs(status): record 5dcc4df hash for Loopback Tier-2 open` | 2026-04-30 |
| `5dcc4df` | `feat(l3): eos:l3:Loopback — first S6 / Tier-2 resource` | 2026-04-30 |
| `8b84e8a` | `docs(plan): bump linter count to 83 + reflect lint hardening` | 2026-04-30 |
| `0c746ab` | `build(lint): expand golangci-lint to 83 linters; harden gosec settings` | 2026-04-30 |
| `32f3b1f` | `docs(audit): sync STATUS + plan to actual repo state` | 2026-04-30 |
| `5a742d9` | `docs(status): record 363f31e hash for gNMI Tier-1 close` | 2026-04-30 |
| `363f31e` | `feat(client): minimum gNMI client (Capabilities + Get) — Tier-1 close` | 2026-04-30 |
| `fb0be36` | `feat(l2): eos:l2:MacAddressTable read-only data source via infer.Function` | 2026-04-30 |
| `f4adb59` | `feat(device): eos:device:RawCli — diff-driven idempotent escape hatch` | 2026-04-30 |
| `be4d732` | `feat(device): eos:device:Configlet — atomic raw CLI block via config-session` | 2026-04-30 |
| `5fa01a1` | `feat(l2): eos:l2:VlanRange bulk allocation helper` | 2026-04-30 |
| `726b26c` | `feat(l2): eos:l2:Varp global anycast-gateway MAC` | 2026-04-30 |
| `2a64a58` | `feat(l2): eos:l2:Stp global spanning-tree configuration` | 2026-04-30 |
| `b3e2c5d` | `feat(l2): eos:l2:Mlag singleton + sync STATUS / plan to S5 progress` | 2026-04-30 |
| `09dd4f1` | `feat(l2): eos:l2:EvpnEthernetSegment for EVPN multi-homing` | 2026-04-30 |
| `193a4e6` | `feat(l2): eos:l2:VxlanInterface — overlay VTEP with VLAN/VRF→VNI maps` | 2026-04-30 |
| `3581c37` | `fix(plugin): relocate pulumi-eos-dev to plugins/ and use ./ prefix in source` | 2026-04-30 |
| `fcf1418` | `docs(plugin): pulumi-eos-dev project plugin; scrub external-citations leakage` | 2026-04-30 |
| `b516f28` | `feat(l2): eos:l2:PortChannel + shared switchport helpers; clean stubs` | 2026-04-30 |
| `449ea8e` | `feat(l2): eos:l2:Interface (physical Ethernet) over eAPI config-session` | 2026-04-30 |
| `9a795dc` | `docs(go-style): Go 1.26 patterns, antipatterns, project standards` | 2026-04-30 |
| `2a77f2e` | `feat(l2): eos:l2:VlanInterface (SVI) over eAPI config-session` | 2026-04-30 |
| `fa20b40` | `feat(l2): eos:l2:Vlan resource with full CRUD over eAPI config-session` | 2026-04-30 |
| `d661199` | `docs(plan): expand catalog per Supported Features Matrix; add STATUS dashboard` | 2026-04-30 |
| `f6ae43f` | `chore(build): bootstrap pulumi-eos repository` | 2026-04-30 |
| `0117e8a` | `ci(release): add release pipeline, goreleaser, codeowners, issue templates` | 2026-04-30 |
| `6565ccd` | `feat(provider): wire pulumi-go-provider runtime, eAPI/CVP clients, device canary` | 2026-04-30 |
| `9fa0b40` | `fix(ci): make all doc lint gates pass — mermaid, markdownlint, cspell, yamllint` | 2026-04-30 |
| `eb6cdc8` | `test(integration): cEOS 4.36.0.1F live test stack — show version + session lifecycle` | 2026-04-30 |
| `98daa9c` | `docs(catalog): expand resource catalog from arista-mcp` | 2026-04-30 |
