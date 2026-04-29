# Contributing

## Standards

| Item | Standard |
|---|---|
| Versioning | [SemVer 2.0.0](https://semver.org/spec/v2.0.0.html) |
| Commits | [Conventional Commits 1.0.0](https://www.conventionalcommits.org/en/v1.0.0/) |
| Changelog | [Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/) |
| Branching | Trunk-based; squash-merge |

## Workflow

1. Fork or branch: `feat/<area>/<name>`, `fix/<area>/<name>`, `chore/<area>/<name>`.
2. Develop inside the dev container: `make up && make shell`.
3. `make all` (build + test + lint) must pass locally before pushing.
4. Commit message follows Conventional Commits 1.0.0; scope = top-level area.
5. Update `CHANGELOG.md` `[Unreleased]` for any user-visible change.
6. Open a PR; PR title is itself a Conventional Commit subject.

## Commit types

| Type | Use |
|---|---|
| `feat` | New user-facing capability. |
| `fix` | Bug fix. |
| `docs` | Documentation only. |
| `chore` | Build / tooling / non-prod files. |
| `build` | Build system changes (`Containerfile`, `Makefile`, `go.mod`). |
| `ci` | CI / workflow changes. |
| `refactor` | Code change with no behaviour change. |
| `perf` | Performance change. |
| `test` | Tests only. |
| `style` | Formatting only. |
| `revert` | Revert of a prior commit. |

## Quality gates

| Gate | Command |
|---|---|
| Go static analysis | `make lint` |
| SAST | `make semgrep` |
| Vulnerabilities | `make vulncheck` |
| Tests + race | `make test` |
| Coverage | `make coverage` |
| Docs lint (md + mermaid + yaml + spell) | `make lint-docs` |

A PR may not merge while any `error`-tier finding is unresolved.

## Definition of done (per resource)

See [`docs/02-implementation-plan.md`](docs/02-implementation-plan.md) §7.

## Reporting security issues

See [`SECURITY.md`](SECURITY.md). Do not open public issues for vulnerabilities.
