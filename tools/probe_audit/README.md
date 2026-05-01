# probe-audit

Commit-terminated CLI surface auditor for pulumi-eos l3 resources.

Implements docs/05-development.md rule 2b: every probe terminates
with `commit`, not `abort`, so cEOSLab platform-validation fires
and silent-noop bugs surface (the same class of issue that produced
the 5 EOS-keyword corrections in `eos:l3:Vrrp` v0).

## Why Python

The harness is glue code — JSON-RPC, session orchestration, report
formatting — where Python's stdlib gives the shortest path. The
runtime-critical CLI rendering stays in Go (`internal/resources/l3/`).
The Go integration suite (`test/integration/`) is the source of
truth for per-resource correctness; this tool exists to _audit_ the
broader surface (every Args field) which the Go integration body
intentionally trims for run-time.

## Layout

```text
tools/probe_audit/
├── pyproject.toml         uv + ruff + ty config (Python 3.11+)
├── README.md              this file
└── probe_audit/
    ├── __init__.py        public API
    ├── __main__.py        CLI entry
    ├── client.py          stdlib http.client eAPI wrapper
    ├── harness.py         commit-terminated probe helpers
    └── surfaces.py        per-resource CLI surface specs
```

## Run

The audit runs from inside the project's dev container. uv +
ruff + ty are installed in the dev image (see `make probe-audit`).

```bash
make probe-audit                       # all surfaces
make probe-audit-only ONLY=router_ospf # one surface
make probe-audit-lint                  # ruff + ty
```

Manual invocation:

```bash
podman exec pulumi-eos-dev sh -c '
  cd /app/tools/probe_audit && \
  uv run --with-editable . probe-audit \
    --host host.containers.internal --port 18080
'
```

## Adding a resource

1. Open `probe_audit/surfaces.py`.
2. Append a `Surface(name=..., fixture=..., cleanup=..., probes=...)`
   to `SURFACES`. One probe line per Args field; reference the EOS
   User Manual section that justifies each line in a comment so the
   audit stays traceable.
3. `make probe-audit-only ONLY=<name>` to verify.

## Output

Per-resource:

```text
== loopback ==
  OK   description audit
  OK   ip address 192.0.2.1/32
  OK   ipv6 address 2001:db8::1/128
  OK   shutdown
  OK   no shutdown
  -- 5/5 OK
```

Failures distinguish parser-level rejects (`FAIL`) from cEOSLab
hardware-platform unsupported (`PLAT`):

```text
== gre_tunnel ==
  OK   tunnel mode gre
  OK   tunnel source 10.0.0.1
  PLAT tunnel dont-fragment
       -> Unavailable command (not supported on this hardware platform) ...
```

The audit reports only what cEOS accepts at _commit_. Production EOS
hardware may accept more (`PLAT` lines tagged `Unavailable command`
typically work on physical Arista switches).
