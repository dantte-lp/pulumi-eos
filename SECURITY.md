# Security policy

## Supported versions

| Version | Supported |
|---|---|
| `v1.x` (latest minor) | yes |
| `v1.x` (previous minor) | security-only |
| Older | no |

## Reporting a vulnerability

- **Do not** open a public GitHub issue.
- Use GitHub Security Advisories: <https://github.com/dantte-lp/pulumi-eos/security/advisories/new>.
- Provide reproduction steps, affected versions, and environment.
- Initial response within 7 days; CRITICAL CVE patch released within 7 days of confirmation.

## Hardening

| Concern | Control |
|---|---|
| Token storage | Pulumi `Secret` types. Never persist plaintext. |
| TLS | mTLS preferred for gNMI/gNOI; CA pinning supported. |
| Authentication | Service-account bearer tokens for CVP/CVaaS; AAA / TACACS+ / RADIUS for eAPI. |
| Supply chain | Pinned `golangci-lint`, `govulncheck`, `osv-scanner`; SBOM (syft) per release; cosign signatures. |
| Dependencies | Dependabot weekly; `make vulncheck` in CI; explicit allowlist. |
| Container | Minimal base (`debian:trixie-slim` + non-root user); Trivy scan in CI. |
