# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning 2.0.0](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Repository scaffold: project layout, Makefile, dev container, compose, CI workflows.
- `golangci-lint v2.11.4` configuration (allowlist mode, severity-tiered, ~70 linters).
- `markdownlint-cli2`, Mermaid render-lint, `yamllint`, `cspell`, `commitlint` configurations.
- Podman + podman-compose + podman-py automation in `scripts/automation/`.
- Vulnerability audit (`govulncheck` + `osv-scanner`) with allowlist support.
- Documentation: architecture, implementation plan (waterfall, 12 sprints), resource catalog skeleton, research references.
- `LICENSE` (Apache-2.0), `SECURITY.md`, `CONTRIBUTING.md`.

[Unreleased]: https://github.com/dantte-lp/pulumi-eos/compare/HEAD...HEAD
