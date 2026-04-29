# Testing

## Layers

| Layer | Build tag | Runner | Target |
|---|---|---|---|
| Unit | none | `go test -race ./...` | All packages. |
| Integration | `integration` | `go test -tags integration ./test/integration/...` | cEOS in Containerlab; mocked CVP. |
| Acceptance | `acceptance` | `go test -tags acceptance ./test/acceptance/...` | End-to-end Pulumi programs against fixtures. |
| Matrix | external | Containerlab + GitHub matrix | EOS 4.30 / 4.32 / 4.34 / 4.36 × CVP 2024.3 / 2025.3 / 2026.1. |
| Soak | external | 24 h continuous apply/refresh | Single-fabric stability. |

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
| CVP | in-process gRPC server implementing the subset of Resource APIs we depend on. |

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
