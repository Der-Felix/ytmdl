#!/usr/bin/env python3
"""Exercise the official package with real Compose, isolated fixtures and DB."""
import argparse
import http.cookiejar
import importlib.util
import json
import os
from pathlib import Path
import secrets
import shutil
import socket
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parent.parent
spec = importlib.util.spec_from_file_location("installation", ROOT / "scripts/install.py")
install = importlib.util.module_from_spec(spec)
spec.loader.exec_module(install)


def qualify(args):
    prefix = "ytmdl-qualification-" + secrets.token_hex(4)
    directory = Path(tempfile.mkdtemp(prefix=prefix + "-"))
    project = directory / "project"
    subprocess.run([sys.executable, str(ROOT / "scripts/package-installation.py"), "--output", str(directory)], check=True)
    import tarfile
    version = (ROOT / ".release-version").read_text().strip()
    with tarfile.open(directory / ("ytmdl-" + version + ".tar.gz")) as archive:
        archive.extractall(directory)
    (directory / ("ytmdl-" + version)).rename(project)
    fixtures = project / "fixtures"
    shutil.copytree(ROOT / "scripts/fixtures", fixtures)
    (fixtures / "ytdlp.py").chmod(0o755)
    subprocess.run([args.engine, "run", "--rm", "--network", "none", "--user", "0:0",
                    "--entrypoint", "ffmpeg", "-v", str(fixtures) + ":/fixtures:Z", args.backend,
                    "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=4",
                    "-c:a", "flac", "/fixtures/tone.flac"], check=True, capture_output=True)
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        port = sock.getsockname()[1]
    compose = install.prepare(project, args.engine, version, port, prefix, args.backend)
    env = project / ".env"
    with env.open("a") as handle:
        handle.write("\nYTMDL_NETWORK_SUBNET=203.0.113.0/24\n"
                     "YTDM_DEFAULT_METADATA_PROVIDER=deezer\nYTDM_DEFAULT_MEDIA_PROVIDER=youtube\n"
                     "YTDM_DEEZER_API_BASE_URL=http://203.0.113.10:8080\n"
                     "YTDM_YTMUSIC_ENABLED=false\nYTDM_SPOTIFY_ENABLED=false\nYTDM_SOUNDCLOUD_ENABLED=false\n"
                     "MUSICDL_LIBRARY_LYRICS_ENABLED=false\nMUSICDL_UPDATE_CHECK_ENABLED=false\n"
                     "MUSICDL_YTDLP=/fixtures/ytdlp.py\n")
    override = project / "compose.qualification.yaml"
    override.write_text("""services:
  backend:
    image: """ + args.backend + """
    volumes:
      - ./fixtures:/fixtures:ro,Z
  frontend:
    image: """ + args.frontend + """
    networks:
      - ingress
  fixture:
    image: """ + args.backend + """
    entrypoint: ["python3", "/fixtures/catalog.py"]
    volumes:
      - ./fixtures:/fixtures:ro,Z
    networks:
      ytmdl-net:
        ipv4_address: 203.0.113.10
networks:
  ytmdl-net:
    internal: true
  ingress: {}
""")
    compose += ["-f", str(override)]

    def run(*commands, check=True):
        result = subprocess.run(compose + list(commands), cwd=project, capture_output=True, text=True, timeout=300)
        if check and result.returncode:
            raise RuntimeError("Compose operation failed: " + commands[0])
        return result.stdout

    jar = http.cookiejar.CookieJar()
    client = urllib.request.build_opener(urllib.request.ProxyHandler({}), urllib.request.HTTPCookieProcessor(jar))
    base = "http://127.0.0.1:" + str(port)

    def request(path, method="GET", data=None):
        headers = {"Content-Type": "application/json"}
        token = next((c.value for c in jar if c.name == "ytmdl_csrf"), None)
        if token:
            headers["X-CSRF-Token"] = token
        req = urllib.request.Request(base + "/api/v1" + path, None if data is None else json.dumps(data).encode(), headers, method=method)
        try:
            response = client.open(req, timeout=20)
        except urllib.error.HTTPError as error:
            response = error
        with response:
            return response.status, json.loads(response.read())

    def wait(predicate):
        deadline = time.monotonic() + 180
        while time.monotonic() < deadline:
            try:
                if predicate():
                    return
            except (OSError, urllib.error.URLError, ValueError):
                pass
            time.sleep(2)
        raise RuntimeError("Qualification timed out.")

    try:
        run("up", "-d")
        wait(lambda: request("/health?scope=essential")[1].get("data", {}).get("version") == version)
        status, storage = request("/auth/status")
        assert status == 200
        if args.browser:
            state = {"base": base, "project": str(project), "compose": compose, "engine": args.engine,
                     "prefix": prefix, "version": version}
            target = Path(args.browser)
            target.write_text(json.dumps(state))
            target.chmod(0o600)
            print("PASS: official package starts with " + args.engine + "; browser fixture ready", flush=True)
            return
        status, _ = request("/auth/setup", "POST", {"username": "qualification-admin", "password": "qualification-password-123"})
        assert status == 201, "Administrator setup failed"
        status, storage = request("/storage/status")
        assert status == 200
        status, result = request("/search/artists?q=Qualification&provider=deezer")
        assert status == 200 and result["data"][0]["name"] == "Qualification Artist", "Catalogue fixture search failed"
        request("/artists/1001/discography?provider=deezer")
        status, release = request("/releases/2001?provider=deezer")
        assert status == 200 and release["data"]["tracks"], "Release fixture lookup failed"
        status, job = request("/downloads/track", "POST", {"provider": "deezer", "track_id": "3001", "release_id": "2001", "media_provider": "youtube"})
        assert status == 202, "Download fixture enqueue failed"
        job_id = job["data"]["id"]
        wait(lambda: request("/jobs/" + job_id)[1]["data"]["job"]["status"] in ("completed", "failed"))
        assert request("/jobs/" + job_id)[1]["data"]["job"]["status"] == "completed", "Synthetic acquisition failed"
        # A real PostgreSQL custom archive is restored into a separate disposable DB.
        dump = subprocess.run(compose + ["exec", "-T", "db", "pg_dump", "-U", "ytmdl", "-d", "ytmdl", "-Fc"], cwd=project, capture_output=True, check=True, timeout=60).stdout
        run("exec", "-T", "db", "createdb", "-U", "ytmdl", "qualification_restore")
        subprocess.run(compose + ["exec", "-T", "db", "pg_restore", "-U", "ytmdl", "-d", "qualification_restore", "--no-owner"], cwd=project, input=dump, capture_output=True, check=True, timeout=60)
        count = run("exec", "-T", "db", "psql", "-U", "ytmdl", "-d", "qualification_restore", "-Atc", "SELECT count(*) FROM users")
        assert count.strip() == "1", "Restored user count mismatch"
        before = run("exec", "-T", "db", "psql", "-U", "ytmdl", "-d", "ytmdl", "-Atc", "SELECT count(*) FROM files")
        after = run("exec", "-T", "db", "psql", "-U", "ytmdl", "-d", "qualification_restore", "-Atc", "SELECT count(*) FROM files")
        assert before.strip() == after.strip() and int(before.strip()) > 0, "Restored media inventory mismatch"
        run("up", "-d", "--force-recreate", "backend", "frontend")
        wait(lambda: request("/health?scope=essential")[0] == 200)
        assert request("/auth/me")[0] == 200, "Session did not persist"
        print("PASS: " + args.engine + " official Compose, fixture search/acquisition, database restore and recreation", flush=True)
    finally:
        if not args.browser:
            run("down", "--volumes", "--remove-orphans", check=False)
            # Docker-created ownership may prevent host removal; scoped resources
            # are cleaned through the service user without touching other data.
            shutil.rmtree(directory, ignore_errors=True)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--engine", default="docker", choices=("docker", "podman"))
    parser.add_argument("--backend", required=True)
    parser.add_argument("--frontend", required=True)
    parser.add_argument("--browser", help="Keep isolated stack and write its browser connection file")
    args = parser.parse_args()
    try:
        qualify(args)
    except (RuntimeError, AssertionError, subprocess.SubprocessError) as error:
        print("FAIL:", str(error).split("\n")[0], file=sys.stderr)
        sys.exit(1)
