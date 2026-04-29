# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning 2.0.0](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
