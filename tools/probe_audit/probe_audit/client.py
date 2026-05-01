"""Direct eAPI JSON-RPC client (HTTP-only, lab-scoped).

Mirrors the wire path used by `internal/client/eapi` so probe-audit
results match the production runtime exactly. Stdlib-only,
HTTP-only — the lab cEOS exposes plain HTTP on
``host.containers.internal:18080``. Production deployments of the
provider use the Go `internal/client/eapi` package which speaks both
HTTP and HTTPS with proper TLS handling; this Python harness exists
solely to drive *lab* surface audits and intentionally has no
HTTPS code path.
"""

from __future__ import annotations

import base64
import http.client
import json
from dataclasses import dataclass
from typing import Any


class EapiError(Exception):
    """Raised when an eAPI runCmds call returns a JSON-RPC error.

    Attributes:
        code: numeric error code from the EOS envelope (e.g. 1002).
        message: top-level message, typically of the form
            ``CLI command N of M '<cmd>' failed: <reason>``.
        data: per-command details list — the failing command's slot
            usually carries `{"errors": ["<message>"]}`.
        cmds: the cmds slice that was sent.
    """

    def __init__(
        self,
        code: int,
        message: str,
        data: list[dict[str, Any]],
        cmds: list[str],
    ) -> None:
        super().__init__(message)
        self.code = code
        self.message = message
        self.data = data
        self.cmds = cmds

    @property
    def root_cause(self) -> str:
        """Return the first per-command error string, or ''."""
        for slot in self.data:
            errors = slot.get("errors")
            if errors:
                return str(errors[0])
        return ""

    @property
    def is_unsupported_platform(self) -> bool:
        """Heuristic: cEOSLab platform-validation rejection."""
        return "Unavailable command" in self.root_cause


@dataclass(frozen=True)
class Config:
    """eAPI connection parameters (HTTP-only, lab cEOS)."""

    host: str = "host.containers.internal"
    port: int = 18080
    username: str = "admin"
    password: str = "admin"  # noqa: S105 — lab cEOS default; non-prod
    timeout: float = 15.0


class Client:
    """Stateless wrapper around eAPI's runCmds endpoint (HTTP-only).

    Uses `http.client.HTTPConnection` directly — the URL scheme is
    never interpreted at request-build time, eliminating the
    dynamic-URL risk class that affects `urllib.request` (which
    silently honours `file://`). Probes always POST to a fixed
    `host:port/command-api` over plain HTTP.

    The corresponding Go runtime client (`internal/client/eapi`)
    handles HTTPS / certificate-pinning for production deployments;
    this lab harness is HTTP-only by design.
    """

    _PATH = "/command-api"

    def __init__(self, cfg: Config | None = None) -> None:
        self._cfg = cfg or Config()
        token = base64.b64encode(f"{self._cfg.username}:{self._cfg.password}".encode()).decode()
        self._headers = {
            "Authorization": f"Basic {token}",
            "Content-Type": "application/json",
        }

    def run_cmds(
        self,
        cmds: list[str],
        *,
        fmt: str = "json",
        rpc_id: str = "probe-audit",
    ) -> list[dict[str, Any]]:
        """Execute a list of CLI commands.

        Mirrors goeapi behaviour: prepends 'enable' and drops the
        result slot for it. Raises EapiError when EOS returns a
        JSON-RPC error envelope.

        Use a session-wrapped form like
        ``["configure session probe-X", *body, "commit"]`` to drive
        the rule-2b probe pattern.
        """
        full = ["enable", *cmds]
        payload = {
            "jsonrpc": "2.0",
            "method": "runCmds",
            "params": {"version": 1, "cmds": full, "format": fmt},
            "id": rpc_id,
        }
        data = json.dumps(payload).encode()

        conn = http.client.HTTPConnection(
            self._cfg.host,
            self._cfg.port,
            timeout=self._cfg.timeout,
        )
        try:
            conn.request("POST", self._PATH, body=data, headers=self._headers)
            resp = conn.getresponse()
            raw = resp.read().decode()
        finally:
            conn.close()

        if resp.status >= 400:
            raise RuntimeError(f"eapi: HTTP status={resp.status} body={raw[:300]}")
        body = json.loads(raw)
        if "error" in body:
            err = body["error"]
            raise EapiError(
                code=int(err.get("code", -1)),
                message=str(err.get("message", "")),
                data=list(err.get("data", [])),
                cmds=full,
            )
        return list(body.get("result", []))[1:]  # drop the `enable` slot

    def list_pending_sessions(self) -> list[str]:
        """Return the names of all pending configure-sessions."""
        try:
            res = self.run_cmds(["show configuration sessions detail"], fmt="json")
        except EapiError:
            return []
        sessions_obj = res[0].get("sessions", {}) if res else {}
        return [
            name
            for name, meta in sessions_obj.items()
            if isinstance(meta, dict) and meta.get("state") == "pending"
        ]

    def cleanup_sessions(self, *, prefix: str = "") -> int:
        """Abort every pending session whose name starts with `prefix`.

        Returns the count aborted. Pass an empty prefix to drain all
        pending sessions.
        """
        names = self.list_pending_sessions()
        aborted = 0
        for name in names:
            if prefix and not name.startswith(prefix):
                continue
            try:
                self.run_cmds([f"configure session {name}", "abort"])
                aborted += 1
            except EapiError:
                continue
        return aborted
