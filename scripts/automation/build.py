#!/usr/bin/env python3
"""Build automation for pulumi-eos via podman-py.

Drives image builds, dev container lifecycle, and one-shot lint runs through
the Podman REST socket. Used both locally and in CI.

Spec references:
  - Containerfile.5  https://github.com/containers/common/blob/main/docs/Containerfile.5.md
  - Compose Spec     https://github.com/compose-spec/compose-spec/blob/main/spec.md
  - containers.conf  https://github.com/containers/common/blob/main/docs/containers.conf.5.md

Requires:
  - python >= 3.11
  - podman-py >= 5.5

Usage:
  scripts/automation/build.py dev-image       Build the development image.
  scripts/automation/build.py release-image   Build the release image.
  scripts/automation/build.py up              Start the dev container.
  scripts/automation/build.py down            Stop the dev container.
  scripts/automation/build.py exec -- <cmd>   Run <cmd> inside the dev container.
  scripts/automation/build.py lint            Run all lint targets in one shot.
"""
from __future__ import annotations

import argparse
import os
import sys
from dataclasses import dataclass
from pathlib import Path

try:
    from podman import PodmanClient
    from podman.errors import APIError, NotFound
except ImportError as exc:  # pragma: no cover
    print(f"podman-py not installed: {exc}", file=sys.stderr)
    sys.exit(2)


REPO_ROOT = Path(__file__).resolve().parents[2]
DEV_IMAGE = "localhost/pulumi-eos-dev:local"
RELEASE_IMAGE = "localhost/pulumi-eos:local"
DEV_CONTAINER = "pulumi-eos-dev"

DEV_CONTAINERFILE = REPO_ROOT / "deployments" / "containers" / "Containerfile.dev"
RELEASE_CONTAINERFILE = REPO_ROOT / "deployments" / "containers" / "Containerfile.release"


@dataclass(frozen=True, slots=True)
class Settings:
    socket_uri: str

    @classmethod
    def from_env(cls) -> "Settings":
        sock = os.environ.get("CONTAINER_HOST") or os.environ.get("PODMAN_SOCKET")
        if not sock:
            uid = os.geteuid()
            sock = f"unix:///run/user/{uid}/podman/podman.sock" if uid else "unix:///run/podman/podman.sock"
        return cls(socket_uri=sock)


def client(settings: Settings) -> PodmanClient:
    return PodmanClient(base_url=settings.socket_uri)


# --- commands ---------------------------------------------------------------


def cmd_dev_image(settings: Settings) -> int:
    return _build(
        settings,
        containerfile=DEV_CONTAINERFILE,
        tag=DEV_IMAGE,
    )


def cmd_release_image(settings: Settings) -> int:
    return _build(
        settings,
        containerfile=RELEASE_CONTAINERFILE,
        tag=RELEASE_IMAGE,
    )


def cmd_up(settings: Settings) -> int:
    with client(settings) as podman:
        try:
            existing = podman.containers.get(DEV_CONTAINER)
            if existing.status == "running":
                print(f"{DEV_CONTAINER} already running")
                return 0
            existing.start()
            print(f"{DEV_CONTAINER} started")
            return 0
        except NotFound:
            pass

        if not podman.images.exists(DEV_IMAGE):
            rc = cmd_dev_image(settings)
            if rc:
                return rc

        container = podman.containers.create(
            image=DEV_IMAGE,
            name=DEV_CONTAINER,
            command=["sleep", "infinity"],
            working_dir="/app",
            mounts=[
                {
                    "type": "bind",
                    "source": str(REPO_ROOT),
                    "target": "/app",
                    "read_only": False,
                },
            ],
            environment={
                "GOFLAGS": "-buildvcs=false",
                "GOCACHE": "/tmp/go-cache",
                "GOMODCACHE": "/go/pkg/mod",
                "PULUMI_SKIP_UPDATE_CHECK": "true",
            },
            cap_drop=["ALL"],
            security_opt=["no-new-privileges:true"],
            restart_policy={"Name": "unless-stopped"},
        )
        container.start()
        print(f"{DEV_CONTAINER} started ({container.id[:12]})")
        return 0


def cmd_down(settings: Settings) -> int:
    with client(settings) as podman:
        try:
            container = podman.containers.get(DEV_CONTAINER)
        except NotFound:
            print(f"{DEV_CONTAINER}: not present")
            return 0
        container.stop()
        container.remove(force=True)
        print(f"{DEV_CONTAINER}: stopped and removed")
        return 0


def cmd_exec(settings: Settings, command: list[str]) -> int:
    if not command:
        print("usage: build.py exec -- <command...>", file=sys.stderr)
        return 2
    with client(settings) as podman:
        try:
            container = podman.containers.get(DEV_CONTAINER)
        except NotFound:
            print(f"{DEV_CONTAINER} not running; run `build.py up` first", file=sys.stderr)
            return 1
        rc, output = container.exec_run(cmd=command, demux=False, tty=True)
        if isinstance(output, (bytes, bytearray)):
            sys.stdout.buffer.write(output)
        elif isinstance(output, str):
            sys.stdout.write(output)
        return int(rc or 0)


def cmd_lint(settings: Settings) -> int:
    targets = [
        ["golangci-lint", "run", "./..."],
        ["markdownlint-cli2", "**/*.md"],
        ["yamllint", "-c", ".yamllint.yaml", "."],
        ["cspell", "--no-progress", "--no-summary", "--config", ".cspell.json", "**/*.md", "**/*.go"],
        ["go", "run", "./scripts/vuln-audit.go"],
    ]
    rc = 0
    for cmd in targets:
        print(f">>> {' '.join(cmd)}")
        rc = max(rc, cmd_exec(settings, cmd))
    return rc


# --- helpers ----------------------------------------------------------------


def _build(settings: Settings, containerfile: Path, tag: str) -> int:
    with client(settings) as podman:
        print(f"build: {containerfile} -> {tag}")
        try:
            stream = podman.images.build(
                path=str(REPO_ROOT),
                dockerfile=str(containerfile.relative_to(REPO_ROOT)),
                tag=tag,
                rm=True,
                forcerm=True,
                pull=True,
                decode=True,
            )
        except APIError as exc:
            print(f"podman build failed: {exc}", file=sys.stderr)
            return 1

        for chunk in stream:
            if isinstance(chunk, dict):
                msg = chunk.get("stream") or chunk.get("error") or ""
                if msg:
                    sys.stdout.write(msg)
            else:
                sys.stdout.write(str(chunk))
        return 0


# --- entry point ------------------------------------------------------------


def main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser(prog="build.py", add_help=True)
    sub = parser.add_subparsers(dest="cmd", required=True)
    sub.add_parser("dev-image", help="Build the development image.")
    sub.add_parser("release-image", help="Build the release image.")
    sub.add_parser("up", help="Start the development container.")
    sub.add_parser("down", help="Stop and remove the development container.")
    p_exec = sub.add_parser("exec", help="Run a command inside the dev container.")
    p_exec.add_argument("command", nargs=argparse.REMAINDER)
    sub.add_parser("lint", help="Run all linters one after another.")

    args = parser.parse_args(argv)
    settings = Settings.from_env()

    match args.cmd:
        case "dev-image":
            return cmd_dev_image(settings)
        case "release-image":
            return cmd_release_image(settings)
        case "up":
            return cmd_up(settings)
        case "down":
            return cmd_down(settings)
        case "exec":
            return cmd_exec(settings, [arg for arg in args.command if arg != "--"])
        case "lint":
            return cmd_lint(settings)
        case _:
            parser.error(f"unknown command: {args.cmd}")
            return 2


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
