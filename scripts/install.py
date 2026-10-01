#!/usr/bin/env python3
"""Prepare a fresh official-image installation; never modify existing data."""
import argparse
import os
from pathlib import Path
import re
import secrets
import subprocess
import sys
import uuid


def run(args, cwd):
    result = subprocess.run(args, cwd=cwd, capture_output=True, text=True, timeout=600)
    if result.returncode:
        # Engine output may contain environment values. Do not echo it.
        raise RuntimeError("Container engine operation failed; check engine availability and image access.")
    return result.stdout


def prepare(project, engine, version, port, project_name="ytmdl", backend_image=None):
    project = Path(project).resolve()
    for name in (".env", "data", "music"):
        if (project / name).exists() or (project / name).is_symlink():
            raise RuntimeError("Fresh installation requires absent .env, data and music; use the update guide for existing installations.")
    if not re.fullmatch(r"[a-z0-9][a-z0-9-]{0,47}", project_name):
        raise RuntimeError("Project name must contain lowercase letters, digits or hyphens.")
    if not re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+(?:-rc\.[1-9][0-9]*)?", version):
        raise RuntimeError("Invalid release version.")
    provider_version = run([engine, "compose", "version"], project)
    if engine == "podman" and "podman-compose" in provider_version.lower():
        raise RuntimeError("Podman requires Docker Compose v2 or newer as its provider.")
    password = secrets.token_hex(32)
    guard = str(uuid.uuid4())
    text = (project / ".env.example").read_text()
    text = re.sub(r"(?m)^YTMDL_VERSION=.*$", "YTMDL_VERSION=" + version, text)
    text = re.sub(r"(?m)^YTMDL_HOST_PORT=.*$", "YTMDL_HOST_PORT=" + str(port), text)
    text = text.replace("change-me", password)
    text += "\nYTMDL_PROJECT_NAME=" + project_name + "\nMUSICDL_STORAGE_GUARD_ID=" + guard + "\n"
    # Create the environment exclusively and privately. A failed preparation keeps
    # the file and directories for inspection rather than deleting operator data.
    descriptor = os.open(project / ".env", os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(descriptor, "w") as handle:
        handle.write(text)
    for name in ("data", "music"):
        (project / name).mkdir(mode=0o755)
    marker = project / "music" / ".ytmdl-storage-id"
    marker.write_text("ytmdl-storage:" + guard + "\n")
    marker.chmod(0o644)
    args = [engine, "run", "--rm", "--network", "none"]
    if engine == "podman":
        args += ["--userns", "keep-id:uid=10001,gid=10001"]
    args += ["--user", "0:0", "--entrypoint", "/bin/sh",
             "-v", str(project / "data") + ":/data:Z",
             "-v", str(project / "music") + ":/music:Z",
             backend_image or "ghcr.io/der-felix/ytmdl-backend:" + version,
             "-c", "chown 10001:10001 /data /music /music/.ytmdl-storage-id"]
    run(args, project)
    # Verify as the actual backend user before starting any database or services.
    args[args.index("0:0")] = "10001:10001"
    args[-1] = "touch /data/.install-check /music/.install-check && rm /data/.install-check /music/.install-check"
    run(args, project)
    return compose_args(engine)


def compose_args(engine):
    args = [engine, "compose", "-f", "compose.ghcr.yaml"]
    if engine == "podman":
        args += ["-f", "compose.ghcr.podman.yaml"]
    return args


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--engine", choices=("docker", "podman"), required=True)
    parser.add_argument("--project-dir", type=Path, default=Path(__file__).resolve().parent.parent)
    parser.add_argument("--port", type=int, default=8080)
    parser.add_argument("--prepare-only", action="store_true")
    args = parser.parse_args()
    if not 1 <= args.port <= 65535:
        parser.error("Port must be between 1 and 65535.")
    try:
        version = (args.project_dir / ".release-version").read_text().strip()
        compose = prepare(args.project_dir, args.engine, version, args.port)
        if not args.prepare_only:
            run(compose + ["up", "-d"], args.project_dir)
        print("Installation prepared" if args.prepare_only else "Stack started; wait for container health, then open http://localhost:" + str(args.port))
    except (OSError, RuntimeError, subprocess.TimeoutExpired):
        print("Installation stopped. Verify engine access, images and a fresh installation directory; existing data was preserved.", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
