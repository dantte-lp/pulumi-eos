# EOS validation without hardware — emulator landscape

This document catalogues every Arista-supplied way to validate EOS
CLI / eAPI surface without a physical switch, and where each option
fits into the pulumi-eos validation pipeline.

## Surface coverage matrix

| Target | Boots in | Forwarding | Hardware-only features | Default integration target | Source |
|---|---|---|---|---|---|
| **cEOS-lab 4.36** (current) | ~10 s | software (Arfa, default since 4.30.1F) | rejects: subinterface MTU, RPKI `transport tls`, resilient ECMP, GRE `dont-fragment`, IPSec tunnel mode, MPLS-over-GRE | yes — fast dev loop | TOI 17517 (Arfa) |
| **vEOS-lab 4.36** (this doc) | ~3-4 min | software (Arfa, parity with cEOS-lab as of 4.35.2F) | same set as cEOS-lab — both share the same software fast-path | optional second target for surfaces cEOS-lab also rejects (mostly nothing extra in 4.36) | Arista vEOS-lab page; vrnetlab `arista/veos` |
| **CloudEOS Router** | depends on cloud | DPDK | hardware-emulating in cloud (AWS/Azure/GCP) — different feature set than vEOS-lab | no — out of scope for unit / integration | CloudEOS data sheet |
| **GNS3 + vEOS-lab.vmdk** | ~3-4 min | identical to vEOS-lab | identical to vEOS-lab | no — adds GUI layer w/o forwarding gain | Arista community: vEOS/cEOS GNS3 Labs |
| **EVE-NG + vEOS-lab.qcow2** | ~3-4 min | identical to vEOS-lab | identical to vEOS-lab | no — same reason | TOI: EVE-NG provisioning via `ardl --eve-ng` |
| **DCS-7280R / 7500R / 7800R** (production hardware) | hardware boot | ASIC | none — accepts the entire MPLS+IPsec+TI-LFA surface | reserved for S10 matrix-test stage | EOS Supported Features Matrix 4.35.0F |
| **Arista CloudVision 2026.1** + portal | minutes | n/a (CMS) | only Configlet validation against catalog | reserved for S8 | CVP 2026.1.0 release notes |

The two software fast-path targets (cEOS-lab and vEOS-lab) **share
the same forwarding agent (Arfa)** as of 4.30.1F — anything
hardware-conditional is rejected by _both_. The decision between
them is operational, not technical:

- **cEOS-lab** boots in seconds, costs ~200 MB RAM, runs on any
  podman/docker. Default for the daily loop.
- **vEOS-lab** boots in minutes, needs ~2 GB RAM and `/dev/kvm`,
  but is the same surface as cEOS-lab and provides
  bit-identical eAPI + serial console for boot-time diagnostics.

Hardware-conditional features (IPsec, MPLS-over-GRE, resilient ECMP,
subinterface MTU, RPKI TLS) only validate live on production
DCS-7280R3A/3 family or equivalent. Modelled as `input-shape`
resources with documenting Annotate strings; live integration
coverage lands in the **S10 matrix-test stage** with a
hardware-class fixture.

## Tooling — `eos-downloader` (`ardl`)

[titom73/eos-downloader](https://github.com/titom73/eos-downloader)
pulls cEOS / vEOS / CVP images from the Arista support portal. The
CLI is uv-installed in the dev container; auth via `ARISTA_TOKEN`
env-var (rotate per session; never commit).

```bash
podman exec -e ARISTA_TOKEN="…" pulumi-eos-dev \
  sh -c 'export PATH=$HOME/.local/bin:$PATH && \
    cd /app/.tmp/arista && \
    ardl get eos --version 4.36.0.1F --format vEOS64-lab-qcow2 -o .'
```

Supported image types (per `eos_downloader/models/data.py`):

| Format | Filename | Use |
|---|---|---|
| `cEOS` (32-bit) | `cEOS-lab-X.Y.Z.tar.xz` | docker import |
| `cEOS64` | `cEOS64-lab-X.Y.Z.tar.xz` | docker import (default cEOS-lab target) |
| `vEOS-lab` | `vEOS-lab-X.Y.Z.vmdk` | VMware / vrnetlab |
| `vEOS64-lab-qcow2` | `vEOS64-lab-X.Y.Z.qcow2` | qemu/KVM (default vEOS-lab target) |
| `vEOS64-lab-swi` | `vEOS64-lab-X.Y.Z.swi` | onie / BoxBoot |
| `64` | `EOS64-X.Y.Z.swi` | physical hardware |

Released images are gitignored under `.tmp/arista/`. The qcow2
overlay (`disk.qcow2`) is created at boot time from the read-only
mount.

## Reference docs in arista-mcp

Already indexed (`mcp__arista-mcp__list_documents` count = 2425
docs / 117 940 chunks):

- `EOS-4.36.0F-CommandApiGuide` (`document_id: 8a6304f8a31aa03f`) —
  the eAPI ground-truth reference. Use this when authoring or
  changing anything in `internal/client/eapi/`.
- `EOS USER GUIDE` (`document_id: 597d6e05e564a54a`) — full CLI
  reference (≥ 4 000 sections); §-numbers cited throughout
  pulumi-eos resources resolve against this document.
- 2 400+ TOIs (one per feature) — e.g. TOI 17517 (Arfa), TOI 13938
  (Resilient ECMP), TOI 14470 (RPKI), TOI 14271 (GRE Tunnel),
  TOI 13916 (RouteMap match route-type).
- `EOS Supported Features Matrix 4.35.0F`
  (`document_id: 79deff6fdc1eeca5`) — per-platform availability.

## Containerlab integration

Both targets run as podman-compose stacks under
`deployments/compose/`:

- `compose.integration.yml` — cEOS-lab, the default integration
  target (port 18080).
- `compose.veos.yml` — vEOS-lab, opt-in (port 18180), brought up
  with `podman-compose -f deployments/compose/compose.veos.yml up
  -d`.

`make test-integration` continues to run only the cEOS-lab body.
`make test-integration-veos` (planned) will run the same suite
against the vEOS-lab port. The integration test files address the
target via the `EOS_HOST` + `EOS_PORT` env-vars so the same Go
code drives both.

### vEOS bring-up

`compose.veos.yml` references the Arista 4.36.0.1F release set via
the in-repo `.tmp/arista/` staging path (gitignored). Artefact roles:

| Artefact | Size | Role in pulumi-eos |
|---|---:|---|
| `Aboot-veos-serial-8.0.2.iso` | 6 MB | Aboot bootloader + serial-console support; mounted as primary CDROM in `compose.veos.yml`. |
| `vEOS64-lab-4.36.0.1F.qcow2` | 613 MB | EOS root file system; mounted as disk 1. Also fetchable via `ardl get eos --format vEOS64-lab-qcow2`. |
| `EOS-4.36.0.1F.swi` / `EOS64-4.36.0.1F.swi` | 1.6 / 1.7 GB | Hardware swi; reserved for the S10 matrix-test stage. |
| `EOSuni-4.36.0.1F.swi` | 4.1 GB | Cross-platform swi; reserved for S10. |
| `EOS64-4.36.0.1F-docker.swix` | 111 MB | cEOS docker package. |
| `EOS-4.36.0.1F-CommandApiGuide.pdf` | 4.3 MB | Indexed in `arista-mcp` (`document_id: 8a6304f8a31aa03f`); prefer MCP query over local PDF. |
| `EOS-4.36.0.1F-SysMsgGuide.csv` | 808 KB | 2 747 syslog rows (Facility · Mnemonic · Format · Severity · Explanation · Recommended Action); reserved for `internal/client/eapi/` typed-sentinel table. |
| `EOS-4.36.0.1F-SysMsgGuide.pdf` | 793 KB | Same content, PDF render. |
| `Aboot-norcal16-…` | 15 MB | Aboot for `norcal16` hardware; not used by vEOS-lab. |
| `EOS-4.23.9M-glibc64.swix` | 4 MB | glibc64 compatibility shim. |

Disk layout: Aboot ISO as primary CDROM (`-drive index=0,media=cdrom`,
`-boot d`), qcow2 as disk 1. Boot reaches "EOS init stage 2" via
the Aboot serial console; per TOI 17517 (Arfa fast-path) the
forwarding plane converges to the same state as cEOS-lab — vEOS-lab
adds a real serial console for boot-time diagnostics, no surface
gain over cEOS-lab in 4.36.
