# Development

## Prerequisites

| Tool | Min version |
|---|---|
| `podman` | 5.0 |
| `podman-compose` | 1.5 |
| Python | 3.11 |
| `podman-py` | 5.5 |

## Bootstrap

```bash
# Clone
git clone https://github.com/dantte-lp/pulumi-eos
cd pulumi-eos

# Build dev image and start container
make up

# Open a shell inside the dev container
make shell
```

The dev container provides Go 1.26.2, Pulumi CLI, golangci-lint, govulncheck, osv-scanner, gotestsum, benchstat, markdownlint-cli2, mermaid-cli, cspell, yamllint, junit2html, and podman-py. The host Go toolchain is not required.

## Equivalent automation paths

| Surface | Tool |
|---|---|
| Make targets | `make up · make build · make test · make lint · make sdks` |
| Compose | `podman-compose -f deployments/compose/compose.dev.yml up -d` |
| Python | `scripts/automation/build.py up · build.py exec -- go test ./...` |

## Daily loop

| Step | Command |
|---|---|
| Start dev container | `make up` |
| Build provider | `make build` |
| Run tests | `make test` |
| Lint Go | `make lint` |
| Lint docs | `make lint-docs` |
| Run lint + tests | `make all` |
| Run lint + tests + live cEOS (pre-commit gate) | `make verify` |
| Tear down | `make down` |

## Mandatory per-resource verification rules

Apply on every new resource or CLI-touching change in
`internal/resources/**`. No exceptions; failure of any step blocks the
commit.

| # | Rule |
|---|---|
| 1 | **Live cEOS integration test in the same commit.** Run via `make test-integration` (or directly: `env EOS_HOST=host.containers.internal go test -tags integration ./test/integration/...`). The result line `ok ... github.com/dantte-lp/pulumi-eos/test/integration` must appear in the build log AND in the commit message body as `N/N cEOS integration tests pass`. |
| 2 | **Double validation of every CLI fragment.** Two independent sources MUST agree before the fragment is rendered: (a) `arista-mcp` (User Manual + TOI search), and (b) a direct `eAPI` round-trip against the running cEOS container (`POST http://127.0.0.1:18080/command-api` with `runCmds`). When the two disagree, the live device wins; the deviation MUST be cited in the resource file's package comment or in the commit body. |
| 2a | **Single CLI-validation channel.** All staging/inspection during research, debugging, or test-authoring goes through `internal/client/eapi` against `127.0.0.1:18080` — the same path the runtime uses. `Cli` / `FastCli` via `podman exec`, screen-scraping, and direct SSH are forbidden: cEOS Cli-mode and eAPI parsers diverge in subtle ways (BFD `default` keyword, CommunityList `regexp` vs `expanded`, RPKI default elision). Discovery scripts live as `//go:build integration && probe`-tagged files under `test/integration/` so they share `newTestClient` and the same eAPI client. Run with `go test -tags="integration probe" -run TestProbe_<X> -v ./test/integration/...`. |
| 2b | **Probes terminate with `commit`, not `abort`.** EOS validates per-line CLI grammar at `Stage` time but only triggers full hardware-platform validation at `commit` (or sometimes `end`). Probes that terminate with `Abort` will mark hardware-unsupported commands as OK and ship them into resources that then fail at runtime (cEOSLab `tunnel dont-fragment` was caught this way — commit `<this>` for `eos:l3:GreTunnel` v0). Per-command isolation probes that need to be cheap MAY use `abort`, but the full-body integration test for the resource MUST `commit` and then explicitly clean up; the probe-tagged file SHOULD have at least one `commit`-terminated combo per command it claims to verify. |
| 3 | **Read-back verification.** Both `show running-config \| section <X>` AND `show running-config all \| section <X>` are exercised at least once per new resource: EOS elides default-equal lines from the plain output, and tests that only inspect the trimmed view will silently pass against incorrectly-rendered configurations. |
| 4 | **Standard quality gates after each change.** `go build`, `go test -race`, `golangci-lint v2.11.4` (83 linters incl. gosec audit-mode), `govulncheck`, `cspell`, `markdownlint`, `yamllint` — all clean before commit. |

`make verify` chains build + tests + lint + lint-docs + live cEOS
integration so the four rules are satisfied with a single invocation.

```makefile
all:    build test lint lint-docs
verify: all test-integration-keep
```

Where `test-integration` brings up `pulumi-eos-it-ceos`, applies the
bootstrap config (`scripts/integration-bootstrap.sh`), and runs the
`integration`-tagged suite against it.

`make verify` MUST exit 0 before any commit that touches
`internal/resources/**`, `internal/client/**`, or
`scripts/integration-bootstrap.sh`. The closing line of the integration
run (`ok ... github.com/dantte-lp/pulumi-eos/test/integration`) is the
literal evidence cited in commit message bodies as
`N/N cEOS integration tests pass`.

## Layout

See [02-implementation-plan.md §8](02-implementation-plan.md#8-repository-layout).

## Container references

| Spec | URL |
|---|---|
| Containerfile.5 | <https://github.com/containers/common/blob/main/docs/Containerfile.5.md> |
| containerignore.5 | <https://github.com/containers/common/blob/main/docs/containerignore.5.md> |
| containers.conf.5 | <https://github.com/containers/common/blob/main/docs/containers.conf.5.md> |
| Compose Specification | <https://github.com/compose-spec/compose-spec/blob/main/spec.md> |
