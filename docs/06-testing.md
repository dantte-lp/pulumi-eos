# Testing

## Layers

| Layer | Build tag | Runner | Target |
|---|---|---|---|
| Unit | none | `go test -race ./...` | All packages. |
| Integration | `integration` | `go test -tags integration ./test/integration/...` | cEOS in Containerlab; mocked CVP. |
| Probe | `integration probe` | `go test -tags="integration probe" -run TestProbe_<X> -v ./test/integration/...` | On-demand surface discovery scripts (`probe_<resource>_test.go`) sharing the integration `newTestClient`. Used to enumerate accepted CLI fragments and verify session idempotency before authoring a resource. Not part of CI; not part of `make verify`. |
| Probe-audit | Python (uv + ruff + ty) | `make probe-audit` | Cross-resource keyword auditor under `tools/probe_audit/`. Drives every l3 resource's full Args-render surface against live cEOS via direct eAPI, commit-terminated per probe (rule 2b), and reports per-line accept / reject / platform-quirk. Catches silent-no-op bugs that the Go integration body trims for run-time. Not part of CI by default; run on doc-sync / surface-audit cycles. |
| Acceptance | `acceptance` | `go test -tags acceptance ./test/acceptance/...` | End-to-end Pulumi programs against fixtures. |
| Matrix | external | Containerlab + GitHub matrix | EOS 4.30 / 4.32 / 4.34 / 4.36 × CVP 2024.3 / 2025.3 / 2026.1. |
| Soak | external | 24 h continuous apply/refresh | Single-fabric stability. |

## Pre-commit gate

`make verify` chains the Unit + Integration layers plus all Go and doc
linters, against a kept-running cEOS 4.36.0.1F container. It is the
mandatory pre-commit gate documented in
[`docs/05-development.md`](05-development.md#mandatory-per-resource-verification-rules):

| Step | Command | Source of truth |
|---|---|---|
| Build + race tests | `make test` | Go test runner inside `pulumi-eos-dev`. |
| Static + style | `make lint` (83 linters) + `make lint-docs` | golangci-lint v2.11.4 + markdownlint + cspell + yamllint + mermaid render + `lint-probes` (rule 2b enforcer). |
| Live cEOS round-trip | `make test-integration-keep` | `pulumi-eos-it-ceos` container; bring-up via `scripts/integration-bootstrap.sh`. |

Recommended on every l3 resource ship (not part of `verify`, run
explicitly): `make probe-audit` — cross-checks every Args-render
keyword against live cEOS and surfaces silent-no-op bugs that the
Go integration body intentionally trims away.

## Coverage

| Quality gate | Threshold |
|---|---|
| Total Go coverage | ≥ 80 % per package by S9 exit. |
| Critical path coverage | 100 % for `internal/client/*` and `internal/resources/*` CRUD. |
| Race detector | Always on (`-race`). |

## Idempotence proof per resource

```text
1. pulumi up         → resource created
2. pulumi up         → no diff
3. modify on device  → drift
4. pulumi refresh    → state matches device, drift reported
5. pulumi up         → drift remediated, no further diff
```

## Mock harnesses

| Target | Harness |
|---|---|
| eAPI | net/http test server replaying recorded JSON-RPC responses. |
| gNMI | embedded `openconfig/gnmi`-test server. |
| CVP | in-process gRPC server implementing the subset of Resource APIs the provider consumes. |

## CI matrix

| Job | Image | Trigger |
|---|---|---|
| `build-and-test` | `golang:1.26.2-trixie` | push, PR |
| `lint-go` | `golang:1.26.2-trixie` | push, PR |
| `lint-docs` | `ubuntu-latest` + Node 22 | push, PR |
| `vulncheck` | `golang:1.26.2-trixie` | push, PR |
| `trivy` | `ubuntu-latest` | push, PR |
| `commitlint` | `ubuntu-latest` + Node 22 | PR only |
| `codeql`, `gosec` | `ubuntu-latest` | push, PR, weekly |
| `scorecard` | `ubuntu-latest` | weekly |
