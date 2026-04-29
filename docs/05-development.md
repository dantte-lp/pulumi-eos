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
| Run all | `make all` |
| Tear down | `make down` |

## Layout

See [02-implementation-plan.md §8](02-implementation-plan.md#8-repository-layout).

## Container references

| Spec | URL |
|---|---|
| Containerfile.5 | <https://github.com/containers/common/blob/main/docs/Containerfile.5.md> |
| containerignore.5 | <https://github.com/containers/common/blob/main/docs/containerignore.5.md> |
| containers.conf.5 | <https://github.com/containers/common/blob/main/docs/containers.conf.5.md> |
| Compose Specification | <https://github.com/compose-spec/compose-spec/blob/main/spec.md> |
