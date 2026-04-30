# Status

> Live dashboard of progress against [`docs/02-implementation-plan.md`](02-implementation-plan.md).

## Phase progress

| Phase | Sprints | State | Evidence |
|---|---|---|---|
| Phase 1 — Requirements | S1 | done | `docs/02-implementation-plan.md`, `docs/08-research-references.md` |
| Phase 2 — Design | S2 — S3 | done (artefacts) | `docs/01-architecture.md`, `docs/03-resource-catalog.md`, `docs/04-provider-config.md` |
| Phase 3 — Implementation | S4 — S9 | S4 done; S5 — S9 pending | per-sprint table below |
| Phase 4 — Verification | S10 — S11 | pending | — |
| Phase 5 — Deployment | S12 | pending | — |
| Phase 6 — Maintenance | continuous | pending | — |

## Sprint progress

| Sprint | Scope | State | Evidence |
|---|---|---|---|
| S1 — Requirements | SRS, risk register, scope | done | `docs/02-implementation-plan.md` §6, §11; `docs/08-research-references.md` |
| S2 — System design | Component diagram, sequence diagrams, transport matrix | done | `docs/01-architecture.md` |
| S3 — Detailed design | Resource catalog + field shapes, ADRs | done (catalog), schema gen pending | `docs/03-resource-catalog.md`, `docs/04-provider-config.md` |
| S4 — Foundation | Provider runtime, eAPI/CVP clients, cEOS integration, CI green | done | commits `f6ae43f`, `0117e8a`, `6565ccd`, `9fa0b40`, `eb6cdc8`, `98daa9c`, `d661199` |
| S5 — L2 family | `Vlan`, `VlanRange`, `VlanInterface`, `Interface`, `PortChannel`, `EvpnEthernetSegment`, `Mlag`, `VxlanInterface`, `MacAddressTable`, `Varp`, `Stp`; `RawCli` escape; minimum gNMI client | in progress (10/11 core L2 resources shipped) | shipped: `Vlan` (`fa20b40`), `VlanInterface` (`2a77f2e`), `Interface` (`449ea8e`), `PortChannel` + shared `SwitchportFields` (`b516f28`), `VxlanInterface` (`193a4e6`), `EvpnEthernetSegment` (`09dd4f1`), `Mlag` (`b3e2c5d`), `Stp` (`2a64a58`), `Varp` (`726b26c`), `VlanRange` (this commit); 12/12 cEOS integration tests pass; pending: `MacAddressTable` (read-only data source, `infer.Function` form), `RawCli` (escape hatch — `eos:device:`), minimum gNMI client. |
| S6 — L3 family | `Vrf`, `RouterBgp` (peer-groups, EVPN AF), `Bfd`, `Rcf`, `Rpki`, `Vrrp`, `Pbr` | pending | — |
| S7 — Security / Mgmt / Multicast / QoS | ACLs, AAA, MACsec, DHCP, Igmp / Pim / Msdp, QoS | pending | — |
| S8 — CloudVision | `Workspace`, `Studio`, `ChangeControl`, `Configlet`, `Tag`, … | pending | — |
| S9 — Day-2 / gNOI / drift | `OsImage`, `Reboot`, `Certificate`; gNMI Subscribe drift | pending | — |
| S10 — System test | Matrix (cEOS 4.30 — 4.36 × CVP 2024.3 — 2026.1) + soak | pending | — |
| S11 — UAT & docs | UAT; Pulumi Registry intake | pending | — |
| S12 — Release | `v1.0.0` | pending | — |

## Quality gates (last run)

| Gate | Tool | State |
|---|---|---|
| Build | `go build ./...` (Go 1.26.2) | pass |
| Unit tests + race | `go test -race -count=1 ./...` | pass |
| Integration | `make test-integration` against cEOS 4.36.0.1F | pass |
| Go static analysis | `golangci-lint v2.11.4` (allowlist, ~70 linters) | 0 issues |
| SAST | `gosec` (audit) | 0 issues |
| Vulnerability | `govulncheck v1.2.0` + `osv-scanner v2.3.5` | no vulnerabilities |
| Markdown | `markdownlint-cli2` | 0 errors |
| Mermaid | `@mermaid-js/mermaid-cli` (`mmdc`) render | 6 diagrams parsed, 0 failures |
| YAML | `yamllint` | clean |
| Spelling | `cspell` | clean |
| LSP | `gopls v0.21.1` | references resolve across packages |

## Open commitments

| Topic | Where | Owner |
|---|---|---|
| Generate `schema.json` via `bin/pulumi-resource-eos -schema` | S3 carry-over → S5 entry | — |
| `pulumi package gen-sdk` for Go / Python / TypeScript / .NET / Java | S5 entry | — |
| `v0.1.0-rc.1` tag | S5 exit | — |

## Repository activity

| Commit | Subject | Date |
|---|---|---|
| `09dd4f1` | `feat(l2): eos:l2:EvpnEthernetSegment for EVPN multi-homing` | 2026-04-30 |
| `193a4e6` | `feat(l2): eos:l2:VxlanInterface — overlay VTEP with VLAN/VRF→VNI maps` | 2026-04-30 |
| `3581c37` | `fix(plugin): relocate pulumi-eos-dev to plugins/ and use ./ prefix in source` | 2026-04-30 |
| `fcf1418` | `docs(plugin): pulumi-eos-dev project plugin; scrub external-citations leakage` | 2026-04-30 |
| `b516f28` | `feat(l2): eos:l2:PortChannel + shared switchport helpers; clean stubs` | 2026-04-30 |
| `449ea8e` | `feat(l2): eos:l2:Interface (physical Ethernet) over eAPI config-session` | 2026-04-30 |
| `9a795dc` | `docs(go-style): Go 1.26 patterns, antipatterns, project standards` | 2026-04-30 |
| `2a77f2e` | `feat(l2): eos:l2:VlanInterface (SVI) over eAPI config-session` | 2026-04-30 |
| `fa20b40` | `feat(l2): eos:l2:Vlan resource with full CRUD over eAPI config-session` | 2026-04-30 |
| `f6ae43f` | `chore(build): bootstrap pulumi-eos repository` | 2026-04-30 |
| `0117e8a` | `ci(release): add release pipeline, goreleaser, codeowners, issue templates` | 2026-04-30 |
| `6565ccd` | `feat(provider): wire pulumi-go-provider runtime, eAPI/CVP clients, device canary` | 2026-04-30 |
| `9fa0b40` | `fix(ci): make all doc lint gates pass — mermaid, markdownlint, cspell, yamllint` | 2026-04-30 |
| `eb6cdc8` | `test(integration): cEOS 4.36.0.1F live test stack — show version + session lifecycle` | 2026-04-30 |
| `98daa9c` | `docs(catalog): expand resource catalog from arista-mcp` | 2026-04-30 |
