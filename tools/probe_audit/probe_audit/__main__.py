"""CLI entry-point: ``probe-audit [--only NAME] [--host H] [--port P]``.

Drives every (or one) surface in `surfaces.SURFACES` against a live
cEOS via direct eAPI (commit-terminated) and prints a per-resource
report. Exit status is non-zero when any probe fails.
"""

from __future__ import annotations

import argparse
import sys

from probe_audit.client import Client, Config
from probe_audit.harness import audit_resource
from probe_audit.surfaces import SURFACES, get


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        prog="probe-audit",
        description="Commit-terminated CLI surface auditor for pulumi-eos l3 resources.",
    )
    parser.add_argument(
        "--only",
        action="append",
        default=None,
        help="Limit to one or more surface names (repeatable).",
    )
    parser.add_argument(
        "--host",
        default=None,
        help="cEOS host (default: host.containers.internal).",
    )
    parser.add_argument(
        "--port",
        type=int,
        default=None,
        help="eAPI port (default: 18080).",
    )
    parser.add_argument(
        "--username",
        default=None,
        help="eAPI username (default: admin).",
    )
    parser.add_argument(
        "--password",
        default=None,
        help="eAPI password (default: admin).",
    )
    parser.add_argument(
        "--quiet",
        action="store_true",
        help="Print only failures and a per-resource summary line.",
    )
    args = parser.parse_args(argv)

    defaults = Config()
    cfg = Config(
        host=args.host or defaults.host,
        port=args.port or defaults.port,
        username=args.username or defaults.username,
        password=args.password or defaults.password,
    )
    client = Client(cfg)

    surfaces = [get(name) for name in args.only] if args.only else SURFACES
    overall_failures = 0
    overall_total = 0
    print(f"probe-audit: running {len(surfaces)} surface(s)\n")

    for s in surfaces:
        print(f"== {s.name} ==")
        results = audit_resource(
            client,
            name=s.name,
            fixture=s.fixture,
            cleanup=s.cleanup,
            probes=s.probes,
        )
        local_fail = sum(1 for r in results if not r.ok)
        local_total = len(results)
        overall_failures += local_fail
        overall_total += local_total
        for r in results:
            if not r.ok:
                tag = "PLAT" if r.unsupported_platform else "FAIL"
                cause = r.root_cause or r.error
                print(f"  {tag} {r.cmd}\n         -> {cause[:160]}")
            elif not args.quiet:
                print(f"  OK   {r.cmd}")
        ok = local_total - local_fail
        print(f"  -- {ok}/{local_total} OK\n")

    print(
        f"probe-audit: {overall_total - overall_failures}/{overall_total} "
        f"probe lines accepted across {len(surfaces)} surface(s)"
    )
    return 0 if overall_failures == 0 else 1


if __name__ == "__main__":  # pragma: no cover
    sys.exit(main())
