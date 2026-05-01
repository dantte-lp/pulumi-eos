"""Commit-terminated CLI probe-audit harness for pulumi-eos l3 resources.

Drives every resource's CLI surface against a live cEOS via direct eAPI
and reports keyword corrections needed. Implements rule 2b
(docs/05-development.md) — every probe terminates with `commit`, not
`abort`, so cEOSLab platform-validation fires and silent-noop bugs
surface.

Public API:

- `Client.run_cmds(cmds)` — JSON-RPC runCmds with optional eAPI errors
  surfaced as Python exceptions.
- `probe_one_per_cmd(client, fixture, cleanup, probes)` — one
  commit-terminated session per probe.
- `audit_resource(client, resource)` — runs the surface-spec for one
  resource and returns a list of accepted / rejected lines.

Why Python: the audit-loop is glue code (network I/O, JSON parsing,
report formatting) where Python's stdlib gives the shortest path. The
runtime-critical CLI rendering stays in Go (`internal/resources/l3/`).
"""

__all__ = [
    "Client",
    "EapiError",
    "ProbeResult",
    "audit_resource",
    "probe_one_per_cmd",
]

from probe_audit.client import Client, EapiError
from probe_audit.harness import ProbeResult, audit_resource, probe_one_per_cmd
