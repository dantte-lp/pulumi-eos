# Go 1.26 patterns, antipatterns, standards

> Project standard for `pulumi-eos`. Pinned to **Go 1.26.2** (dev container
> `golang:1.26.2-trixie`, host `/usr/local/go`). Aligned with the official
> [Go 1.26 release notes](https://go.dev/doc/go1.26).

## 1. Language features adopted

| Feature | Pattern | Rationale |
|---|---|---|
| `new(value)` | `IpAddress: new("10.0.0.1/24")` for `*string` literals | Drops one-line `v := …; &v` ceremony; required by `modernize` linter (`newexpr`). |
| Self-referential generics (`type T[A T[A]]`) | Allowed for fluent / phantom-typed builders | Use only when standard generics are insufficient. |
| `for v := range strings.SplitSeq(s, "\n")` | Iterator-form line splitting | Lazy, no `[]string` allocation; `modernize` flags `strings.Split` in for-range as `stringsseq`. |
| `errors.AsType[T]()` | Type-safe error type-match | Replaces `var x *MyErr; errors.As(err, &x)` boilerplate. |
| `slog.NewMultiHandler` | Compose multiple sinks | When provider grows multiple log destinations (file + stderr + journald). |
| `reflect.Type.Fields()` / `.Methods()` / `.Ins()` / `.Outs()` | Iterators over reflection lists | Use only in codegen / schema tooling, never in CRUD hot paths. |
| `os/signal.NotifyContext` returning cancel cause + signal error | Provider main and long-running ops | Surfaces the actual signal in graceful-shutdown logs. |
| `t.ArtifactDir()` + `-artifacts` flag | Persisting test artefacts (cEOS configs, captures) | Replaces `t.TempDir()` for things we want kept on CI runs. |

## 2. Antipatterns banned in this codebase

| Antipattern | Replacement | Linter |
|---|---|---|
| `v := "foo"; p := &v` for optional-pointer literals | `p := new("foo")` | `modernize/newexpr` |
| `for _, line := range strings.Split(s, "\n")` | `for line := range strings.SplitSeq(s, "\n")` | `modernize/stringsseq` |
| `var e *MyErr; if errors.As(err, &e) { … }` | `if e, ok := errors.AsType[*MyErr](err); ok { … }` | manual review |
| Bare `panic(...)` outside `init` | Sentinel error + return | `gocritic`, `forcetypeassert` |
| Anonymous heap-allocated structs in hot paths (`return &struct{ … }{ … }`) | Named type + `new(value)` | `gocritic/typeDeclaration` |
| Hidden goroutine without ctx propagation | Pass `context.Context`, honour `Done()` | `containedctx`, `noctx`, `fatcontext` |
| Logging via package-level `log.*` | `log/slog` with structured attrs | `depguard` denies `^log$` |
| `math/rand` (v1) for any purpose | `math/rand/v2` for non-secret randomness, `crypto/rand` for keys / tokens | `depguard` denies `math/rand$` |
| Mutating shared state from multiple goroutines without sync primitives | `sync.Mutex` / `sync.RWMutex` / channels | `govet copylocks`, race detector |
| Holding a `sync.Mutex` across method boundaries | Channel-based 1-slot semaphore (see `internal/client/eapi.Client.sessionSlot`) | manual review (Semgrep MCP catches this) |
| `tls.Config{InsecureSkipVerify: true}` | Pin CA bundle via `RootCAs` | Semgrep MCP, `gosec` |
| Naked returns in non-trivial functions | Explicit return values | `nakedret max-func-lines: 0` |
| Named returns where not necessary | Anonymous returns | `nonamedreturns` |
| Predeclared identifiers as parameter names (`delete`, `len`, `new`, `cap`) | Project-specific names (`remove`, `count`, `make`, `capacity`) | `predeclared` |

## 3. Standards (mandatory)

| Area | Standard |
|---|---|
| Module path | `github.com/dantte-lp/pulumi-eos`. |
| Go directive | `go 1.26.2`. |
| Layout | `golang-standards/project-layout`. |
| Errors | Sentinel `var ErrXxx = errors.New("…")` at package level; wrap with `fmt.Errorf("…: %w", err)`; never bare strings (linter `err113`). |
| Logging | `log/slog`. Required key-value pairs (linter `loggercheck.require-string-key`, `no-printf-like`). |
| Concurrency | `context.Context` first arg; channels for ownership transfer; `sync.Mutex` only within a single function. |
| Pointer receivers | All methods on a type use the same form (linter `recvcheck`). |
| Resource resources (Pulumi) | Methods on `*T`; `Args` / `State` separate; `Annotate` on `*Args` / `*State`. |
| Field tags | `pulumi:"<name>[,optional]"` and `provider:"secret"` (linter `tagalign` enforces alignment). |
| Imports | `gofumpt` order; `goimports.local-prefixes: github.com/dantte-lp/pulumi-eos`. |
| Test names | `Test<Subject>_<Behaviour>` (e.g. `TestValidateVlanID`). |
| Test parallelism | `t.Parallel()` mandatory in unit tests; never in integration tests that share a single cEOS. |
| Build tags | `//go:build integration` and `//go:build acceptance` for slow / device-bound tests. |
| Race detector | `-race` always on in CI and `make test`. |
| Coverage threshold | ≥ 80 % per package by S9 exit (per `docs/06-testing.md`). |
| Vulnerability gate | `govulncheck` + `osv-scanner` with explicit allowlist; Reason field mandatory per allowed CVE. |

## 4. Runtime / build defaults adopted

| Setting | Value | Reason |
|---|---|---|
| GC | Green Tea (default in Go 1.26) | 10–40 % less GC overhead. |
| Heap address randomization | enabled (default) | Mitigates info-leak attack classes. |
| Goroutine leak profile | enabled in dev container only via `GOEXPERIMENT=goroutineleakprofile` | Diagnostics for the eAPI session semaphore and gRPC clients. |
| `GOFLAGS` | `-buildvcs=false` | Reproducible builds inside the dev container. |
| `GOTOOLCHAIN` | system default | We pin via `go 1.26.2` directive in `go.mod`. |
| Linker | external for cgo where required (we are CGO_ENABLED=0) | Static binary for `pulumi-resource-eos`. |

## 5. Cryptography & TLS

| Decision | Citation |
|---|---|
| `crypto/rand` only for any secret. | Go 1.26 ignores user-supplied `random` parameter in `crypto/{ecdsa,ed25519,rsa,ecdh,dsa}`. |
| Deterministic crypto tests via `testing/cryptotest.SetGlobalRandom()`. | Replaces ad-hoc `rand.Reader` mocks. |
| TLS post-quantum hybrids: leave defaults on. | `tls.SecP256r1MLKEM768` enabled by default; we do not override `CurvePreferences`. |
| Minimum TLS version: 1.3 (`internal/client/cvp` enforces `tls.VersionTLS13`). | Mandatory per `SECURITY.md`. |
| Certificate pinning: PEM bundle via `Config.CACertPEM`. | We do not use `InsecureSkipVerify`. |
| HKE: `crypto/hpke` available if the provider ever needs hybrid public-key encryption (CVP token escrow scenarios). | Reserved for future. |

## 6. Standard-library upgrade map (per-package)

Where the project already uses (or should use) a Go 1.26 idiom:

| Package | Use | Where |
|---|---|---|
| `errors` | `errors.AsType[T]()` for type-asserted errors | every CRUD path that asserts SDK error types |
| `errors` | `errors.Join(err1, err2)` for compound failures | `internal/resources/l2/vlan.go` (`apply` aborts) |
| `strings` | `strings.SplitSeq` over `strings.Split` in `for range` | `internal/resources/l2/vlan_interface.go` parser |
| `errors` | `errors.Join(err1, err2)` for compound failures | `internal/resources/l2/{vlan,vlan_interface,interface,port_channel}.go` |
| `strings` | `SplitSeq` over `Split` in `for-range` loops | `internal/resources/l2/{interface,port_channel,vlan_interface,switchport}.go` parsers |
| `os/signal` | `NotifyContext` returning cancel cause | provider entry-point graceful shutdown |
| `net/url` | strict-colon parsing default-on | `internal/config/clients.go`; `GODEBUG=urlstrictcolons=0` not set |

## 7. Tooling adopted

| Tool | Version | Use |
|---|---|---|
| `go` | 1.26.2 | language, module, build |
| `gofmt` / `gofumpt` / `goimports` | bundled / latest | format gate (`golangci-lint formatters`) |
| `golangci-lint` | `v2.11.4` | aggregate linter; allowlist mode |
| `go fix` | bundled (revamped in 1.26) | dry-run only — we drive modernization through golangci-lint's `modernize` linter, not `go fix --apply` |
| `gopls` | `v0.21.1` | LSP; project-wide references / rename / find-symbol |
| `govulncheck` | `v1.2.0` | CI gate |
| `osv-scanner` | `v2.3.5` | CI gate |
| `gosec` | latest in CI | SAST |
| `semgrep` | external | secondary SAST (catches non-Go-specific patterns) |
| `gotestsum` | latest | JUnit XML test reporting |
| `benchstat` | latest | benchmark statistical comparison |

## 8. Project conventions that defy upstream defaults

| Convention | Default | Rationale |
|---|---|---|
| `default: none` in `golangci-lint` | upstream enables a baseline | Allowlist gives auditable surface; ~70 linters listed explicitly. |
| `nakedret max-func-lines: 0` | 30 | Naked returns are banned outright. |
| `interfacebloat.max: 10` | 10 (matches default but enforced) | Forbids "kitchen-sink" interfaces. |
| `funlen: {lines: 60, statements: 40}` | 60 / 40 | Same as default; explicit. |
| `depguard.deny: math/rand$, ^log$, io/ioutil, unsafe` | none | Forbids legacy / unsafe APIs project-wide. |

## 9. Reading list

- [Go 1.26 release notes](https://go.dev/doc/go1.26) — authoritative.
- [Go 1.26 godebug history](https://pkg.go.dev/runtime#hdr-Environment_Variables) — runtime opt-outs.
- [`golangci-lint` `modernize` linter](https://golangci-lint.run/usage/linters/#modernize) — what auto-modernization triggers we accept.
- [Effective Go](https://go.dev/doc/effective_go) — still the baseline style guide; supersedes nothing in §1–§3.
- [Go Memory Model](https://go.go.dev/ref/mem) — required for any patch touching concurrency primitives.
