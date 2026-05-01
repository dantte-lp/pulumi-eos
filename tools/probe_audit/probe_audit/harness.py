"""Probe harness — commit-terminated per-command + full-body runs.

Mirrors the Go helpers in `test/integration/probe_helpers.go` so the
two languages share semantics. Every probe terminates with `commit`
to surface cEOSLab platform-validation that abort-only probes hide.
"""

from __future__ import annotations

import contextlib
import itertools
from collections.abc import Iterable
from dataclasses import dataclass

from probe_audit.client import Client, EapiError


@dataclass(frozen=True)
class ProbeResult:
    """Outcome of a single per-command probe."""

    cmd: str
    ok: bool
    error: str = ""
    root_cause: str = ""
    unsupported_platform: bool = False


_session_counter = itertools.count(1)


def _next_session(prefix: str) -> str:
    """Unique cleanup-session name (avoids cEOS' 1-slot completed-
    session retention queue collisions)."""
    return f"{prefix}-{next(_session_counter)}"


def run_cleanup(client: Client, cleanup: Iterable[str]) -> None:
    """Best-effort: open a fresh session, stage the cleanup body,
    commit. Failures are swallowed."""
    body = list(cleanup)
    if not body:
        return
    name = _next_session("probe-audit-cleanup")
    try:
        client.run_cmds([f"configure session {name}", *body, "commit"])
    except EapiError:
        with contextlib.suppress(EapiError):
            client.run_cmds([f"configure session {name}", "abort"])


def probe_one_per_cmd(
    client: Client,
    *,
    fixture: list[str],
    cleanup: list[str],
    probes: list[str],
    session_prefix: str = "probe-audit",
) -> list[ProbeResult]:
    """Stage `fixture + [one probe]` per session, COMMIT each.

    The device is reset to the cleanup baseline between probes so
    each one starts from a known state. Uses unique session names to
    avoid completed-session retention collisions.
    """
    results: list[ProbeResult] = []
    for cmd in probes:
        run_cleanup(client, cleanup)
        sname = _next_session(session_prefix)
        body = [f"configure session {sname}", *fixture, cmd, "commit"]
        try:
            client.run_cmds(body)
            results.append(ProbeResult(cmd=cmd, ok=True))
        except EapiError as exc:
            results.append(
                ProbeResult(
                    cmd=cmd,
                    ok=False,
                    error=exc.message,
                    root_cause=exc.root_cause,
                    unsupported_platform=exc.is_unsupported_platform,
                )
            )
            with contextlib.suppress(EapiError):
                client.run_cmds([f"configure session {sname}", "abort"])
    run_cleanup(client, cleanup)
    return results


def probe_full_body(
    client: Client,
    *,
    fixture: list[str],
    cleanup: list[str],
    body: list[str],
    session_prefix: str = "probe-audit-full",
    views: list[str] | None = None,
) -> tuple[bool, str, dict[str, str]]:
    """Stage `fixture + body` in one session, COMMIT, capture views.

    Returns:
        (ok, error_message, captured_views) — ok=False with an error
        message when commit fails.
    """
    run_cleanup(client, cleanup)
    sname = _next_session(session_prefix)
    try:
        client.run_cmds([f"configure session {sname}", *fixture, *body, "commit"])
    except EapiError as exc:
        with contextlib.suppress(EapiError):
            client.run_cmds([f"configure session {sname}", "abort"])
        run_cleanup(client, cleanup)
        return False, f"{exc.message}\n  -> {exc.root_cause}", {}

    captured: dict[str, str] = {}
    for view in views or []:
        try:
            res = client.run_cmds([view], fmt="text")
            captured[view] = res[0].get("output", "").strip() if res else ""
        except EapiError as exc:
            captured[view] = f"<view error: {exc.message}>"

    run_cleanup(client, cleanup)
    return True, "", captured


def audit_resource(
    client: Client,
    *,
    name: str,
    fixture: list[str],
    cleanup: list[str],
    probes: list[str],
) -> list[ProbeResult]:
    """High-level wrapper: probe one resource's full surface.

    Drains stale sessions before starting so the probe begins from a
    clean slot pool.
    """
    client.cleanup_sessions(prefix="probe-audit")
    return probe_one_per_cmd(
        client,
        fixture=fixture,
        cleanup=cleanup,
        probes=probes,
        session_prefix=f"probe-audit-{name}",
    )
