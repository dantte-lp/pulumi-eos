# Research references

> Compact snapshot of source-of-truth findings consulted during S1.
> Versions are as observed in Q2 2026.

## Arista EOS programmatic surfaces

| Surface | Transport | Latest tested EOS | Notes |
|---|---|---|---|
| eAPI | JSON-RPC over HTTPS / Unix socket | 4.36.0F | `runCmds` method; per-cmd `revision`; VRF-bound. |
| gNMI | gRPC/TLS · port 6030 | 4.36.0F | `Set` ops: `delete | replace | update | union_replace` (4.35.0F+). |
| gNOI | gRPC/TLS | 4.36.0F | `OS.Install` (4.24.2F+), `System.Reboot` (4.27.0F+), `Cert.Rotate` (4.20.6F+), `Containerz` (4.34.2F+). |
| OpenConfig | gNMI / RESTCONF / NETCONF | 4.36.0F | Per-path read/write disable since 4.28.0F. |
| Config sessions | eAPI | 4.36.0F | `commit timer` confirmed-commit; ACL-fail auto-rollback. |
| Streaming telemetry | TerminAttr | 4.36.0F | `last-configuration-timestamp` for cheap drift signal. |
| Authn (eAPI) | AAA / TACACS+ / RADIUS / mTLS | — | SSL profile via `management security ssl profile`. |
| Authn (gNMI) | TLS / mTLS / gNSI | 4.31.0F+ | Hitless cert rotation. |

## CloudVision Portal / CVaaS

| Item | Detail |
|---|---|
| Latest tested CVP | 2026.1.0 |
| API style | Resource APIs · gRPC + REST gateway |
| Service catalog | 28 packages: `workspace.v1`, `studio.v1`, `configlet.v1`, `changecontrol.v1`, `tag.v2`, `inventory.v1`, `dashboard.v1`, `event.v1`, `alert.v1`, `imagestatus.v1`, `softwaremanagement.v1`, `lifecycle.v1`, `serviceaccount.v1`, `identityprovider.v1`, `license.v1`, `endpointlocation.v1`, `connectivitymonitor.v1`, `bugexposure.v1`, `auditlog.v1`, `redirector.v1`, `arista_portal.v1`, `asset_manager.v1`, `studio_topology.v1`, `subscriptions`, `syslog.v1`, `time` (+ `fmp/`). |
| Authn | Service-account bearer tokens; 1-year max expiry; regional endpoints. |
| Atomic apply | Workspace → build → submit → Change Control → approve → execute. |

## `aristanetworks/*` repositories (selected)

| Repo | Module / scope | License | Last push (snapshot) | Use |
|---|---|---|---|---|
| `goeapi` | `github.com/aristanetworks/goeapi` | BSD-3 | 2025-04-11 | eAPI client. |
| `cloudvision-go` | `github.com/aristanetworks/cloudvision-go` | Apache-2.0 | 2026-04-29 | CVP gRPC SDK. |
| `cloudvision-apis` | proto IDL | Apache-2.0 | 2026-04-29 | CV resource API schemas. |
| `eossdkrpc` | proto IDL · per-EOS-train tags | Apache-2.0 | 2026-04-29 | On-box EOS SDK gRPC. |
| `goarista` | `github.com/aristanetworks/goarista` | Apache-2.0 | 2025-12-01 | `key.Path`, gNMI helpers. |
| `terraform-provider-cloudeos` | Terraform provider | MPL-2.0 | 2026-03-26 | Reference design (CloudEOS multi-cloud). |
| `avd` | Ansible / Python · v6.1.0 | Apache-2.0 | 2026-04-29 | `eos_cli_config_gen` schema (211 root keys, 222 fragments). |
| `openmgmt` | Examples | Apache-2.0 | 2026-02-17 | gNMI / gNOI / RESTCONF examples. |
| `yang` | YANG models per EOS train | Apache-2.0 | 2026-04-12 | OpenConfig + Arista native. |

## OpenConfig upstream

| Module | Module path | Use |
|---|---|---|
| `gnmi` | `github.com/openconfig/gnmi` | gNMI client. |
| `gnoi` | `github.com/openconfig/gnoi` | gNOI client. |
| `ygot` | `github.com/openconfig/ygot` | YANG → Go bindings. |
| `goyang` | `github.com/openconfig/goyang` | YANG parser. |

## AVD as design reference

- **Schema:** `python-avd/pyavd/_eos_cli_config_gen/schema/eos_cli_config_gen.schema.yml` (26,613 lines, 211 root keys).
- **Fragments:** `python-avd/pyavd/_eos_cli_config_gen/schema/schema_fragments/` (222 files).
- **Metaschema:** `python-avd/pyavd/_schema/avd_meta_schema.json` (non-standard JSON-Schema dialect; used for guidance only).
- **Public Python API:** `pyavd.{validate_inputs, get_avd_facts, get_device_structured_config, validate_structured_config, get_device_config, get_device_doc}`. No applier — push happens via Ansible eAPI / CVP gRPC.

## Library decisions

| Capability | Library | Reason |
|---|---|---|
| Pulumi provider runtime | `github.com/pulumi/pulumi-go-provider` (infer) | Native Go; full diff/preview/rollback. |
| Pulumi SDK | `github.com/pulumi/pulumi/sdk/v3` | Pulumi-blessed. |
| eAPI client | `github.com/aristanetworks/goeapi` | First-party; stable. |
| gNMI client | `github.com/openconfig/gnmi` | Upstream; Arista forks are stale. |
| gNOI client | `github.com/openconfig/gnoi` | Upstream. |
| YANG bindings | `github.com/openconfig/ygot` + `goyang` | Upstream. |
| CVP / CVaaS gRPC | `github.com/aristanetworks/cloudvision-go` | First-party; auto-generated from `cloudvision-apis`. |
