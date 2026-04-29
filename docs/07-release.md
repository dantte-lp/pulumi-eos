# Release

## Cadence

| Type | Trigger | Examples |
|---|---|---|
| Patch (`vX.Y.Z+1`) | Bug fix on `main`. | `v1.0.1`, `v1.0.2`. |
| Minor (`vX.Y+1.0`) | New backward-compatible features. | `v1.1.0`. |
| Major (`vX+1.0.0`) | Schema-breaking change. | `v2.0.0`. |

## Process

1. Update `CHANGELOG.md` `[Unreleased]` with all user-visible changes.
2. Move `[Unreleased]` content under a dated `[X.Y.Z] - YYYY-MM-DD` heading.
3. Tag: `git tag -s vX.Y.Z -m "release: vX.Y.Z"` (annotated, GPG-signed).
4. Push tag → release workflow fires.
5. Workflow extracts release notes (awk between `## [` headers), runs `goreleaser`, builds:
   - `pulumi-resource-eos` for `linux/{amd64,arm64}`, `darwin/{amd64,arm64}`, `windows/{amd64,arm64}`.
   - SBOM (syft).
   - Checksums.
   - Cosign signatures.
   - Multi-arch container image to `ghcr.io/dantte-lp/pulumi-eos:vX.Y.Z`.
6. SDKs published to npm / PyPI / NuGet / Maven Central via `pulumi package gen-sdk`.

## Required artefacts per release

| Artefact | Source |
|---|---|
| GitHub Release | release workflow |
| Release notes | `CHANGELOG.md` extract |
| `pulumi-resource-eos-vX.Y.Z-os-arch.tar.gz` × 6 | goreleaser |
| `pulumi-resource-eos-vX.Y.Z.SHA256SUMS` | goreleaser |
| Cosign signatures | release workflow |
| SBOM SPDX/JSON | syft |
| Container image | `ghcr.io/dantte-lp/pulumi-eos:vX.Y.Z` (and `vX`, `vX.Y`, `latest`) |
| SDK · Go | `github.com/dantte-lp/pulumi-eos/sdk/go` (tagged) |
| SDK · Python | `pulumi-eos` on PyPI |
| SDK · TypeScript | `@dantte-lp/pulumi-eos` on npm |
| SDK · .NET | `DantteLp.Pulumi.Eos` on NuGet |
| SDK · Java | `com.dantte_lp:pulumi-eos` on Maven Central |

## Pulumi Registry

Submission per <https://www.pulumi.com/registry/>; intake checklist:

- [ ] Apache-2.0 license.
- [ ] Top-level `schema.json` valid.
- [ ] At least one stable `vX.Y.Z` release.
- [ ] Reference docs auto-rendered from schema.
- [ ] At least three example programs in different languages.
