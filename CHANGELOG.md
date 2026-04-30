# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning 2.0.0](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added (S4 — foundation)

- `cmd/pulumi-resource-eos`: real entry point wiring the binary to the
  `pulumi-go-provider` infer framework via `provider.New().Run`.
- `internal/provider`: provider config (eAPI + CVP planes, secrets, retry
  policy), validation, and provider builder with namespace `eos`,
  Apache-2.0 metadata, language map for Go / Python / TypeScript.
- `internal/client/eapi`: goeapi-backed client with named configuration
  sessions, channel-based 1-slot semaphore, `commit timer` confirmed-commit,
  `Diff`, and `Abort`.
- `internal/client/cvp`: gRPC client with bearer-token credentials, TLS-1.3
  minimum, optional CA pinning via PEM bundle.
- `internal/resources/device`: `eos:device:Device` canary resource (read-only
  facts; `readFacts` stub lands real implementation in S5).
- `go.mod`: `github.com/aristanetworks/goeapi v1.0.0`,
  `github.com/pulumi/pulumi-go-provider v1.3.2`,
  `github.com/pulumi/pulumi/sdk/v3 v3.232.0`, `google.golang.org/grpc v1.80.0`,
  Go 1.26.2.

### Changed

- `deployments/containers/Containerfile.dev`: drop `mmdc` (Mermaid CLI) from
  the dev container — Chromium-deps bloat. `make lint-mmd` runs mmdc on the
  host or in CI's `lint-docs` job.

### Added

- Repository scaffold: project layout (`golang-standards/project-layout`), Makefile, dev container, Compose Specification dev stack.
- `cmd/pulumi-resource-eos` and `cmd/pulumi-eos-gen` entry-point skeletons with `internal/version` ldflags injection.
- `pkg/eos` public-types placeholder.
- `golangci-lint v2.11.4` configuration · allowlist mode · severity-tiered · ~70 linters enabled.
- `markdownlint-cli2`, Mermaid render-lint, `yamllint`, `cspell`, `commitlint` configurations and helpers.
- Podman + podman-compose + podman-py automation in `scripts/automation/build.py`.
- Vulnerability audit (`govulncheck` + `osv-scanner`) with allowlist support.
- CI workflows: `ci.yml` (build · test · lint-go · lint-docs · vulncheck · trivy · commitlint), `security.yml` (CodeQL · gosec · weekly schedule), `scorecard.yml`, `release.yml`.
- `goreleaser` configuration for cross-platform binaries, SBOM (syft), checksum, cosign signing.
- Dependabot configuration for `gomod`, `github-actions`, `docker`, `pip`.
- Documentation set: architecture, implementation plan (waterfall · 12 sprints · 6 phases), resource catalog skeleton, provider configuration schema preview, development workflow, testing matrix, release process, research references.
- `LICENSE` (Apache-2.0), `SECURITY.md`, `CONTRIBUTING.md`, `CODEOWNERS`, PR template, issue templates.

[Unreleased]: https://github.com/dantte-lp/pulumi-eos/compare/HEAD...HEAD
