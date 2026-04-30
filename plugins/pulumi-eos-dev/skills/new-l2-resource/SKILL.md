---
description: Scaffold a new eos:l2:* resource using the existing Vlan / VlanInterface / Interface pattern. Pass the resource name as the argument (e.g. `Mlag`, `VxlanInterface`).
argument-hint: <ResourceName>
disable-model-invocation: true
---

# Scaffold a new eos:l2:* resource

Resource name: `$ARGUMENTS`

Create three files using the existing pattern from `internal/resources/l2/vlan.go`:

1. `internal/resources/l2/<lowercase_resource_name>.go` — Args / State / Annotate / Create / Read / Update / Delete / validate / build / parse / fillState.
2. `internal/resources/l2/<lowercase_resource_name>_test.go` — unit tests for validate, buildCmds, parseConfig.
3. `test/integration/<lowercase_resource_name>_test.go` — end-to-end test against cEOS using `mustApply` and `mustShowRunningInterface` helpers.

Mandatory shape:

- Sentinel errors at package scope (`var ErrXxx = errors.New(...)`).
- Per-resource override fields for `Host` / `Username` / `Password`.
- Use `internal/client/eapi.Session` (`OpenSession` → `Stage` → `Commit` / `Abort`).
- Use shared `SwitchportFields` helpers in `switchport.go` if the resource has switchport semantics.
- Use `strings.SplitSeq` (Go 1.26) for line iteration.
- Use `errors.Join` to combine staging error and abort error.
- Per-comment standardization rules in `docs/09-go-style.md`: declarative comments + source citation when non-obvious.

Then register the resource in `internal/provider/provider.go` `WithResources(...)`, run `golangci-lint run ./...` + `go test ./...` until both are clean, and commit with the Conventional Commits header `feat(l2): eos:l2:$ARGUMENTS resource`.
