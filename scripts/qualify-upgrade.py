#!/usr/bin/env python3
"""Real managed update against disposable official-image installations."""
import argparse
import hashlib
import http.cookiejar
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
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
import threading
import time
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parent.parent
spec = importlib.util.spec_from_file_location("installation", ROOT / "scripts/install.py")
install = importlib.util.module_from_spec(spec)
spec.loader.exec_module(install)


def qualify(args, guard_enabled):
    prefix = "ytmdl-upgrade-" + secrets.token_hex(4)
    directory = Path(tempfile.mkdtemp(prefix=prefix + "-"))
    for name in ("compose.ghcr.yaml", "compose.ghcr.podman.yaml", ".env.example"):
        shutil.copyfile(ROOT / name, directory / name)
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        port = sock.getsockname()[1]
    compose = install.prepare(directory, args.engine, args.source, port, prefix)
    env_file = directory / ".env"
    text = env_file.read_text()
    if not guard_enabled:
        text = "\n".join(line for line in text.splitlines() if not line.startswith("MUSICDL_STORAGE_GUARD_ID=")) + "\n"
    text += "MUSICDL_UPDATE_CHECKS_ENABLED=false\nMUSICDL_LIBRARY_LYRICS_ENABLED=false\n"
    text += "HTTP_PROXY=http://192.0.2.10:3128\nNO_PROXY=localhost,127.0.0.1,db,backend\n"
    env_file.write_text(text)
    override = directory / "compose.ghcr.override.yaml"
    base_override = """services:
  frontend:
    networks:
      - ingress
networks:
  ytmdl-net:
    internal: true
  ingress: {}
"""
    override.write_text(base_override)
    compose += ["-f", "compose.ghcr.override.yaml"]

    def describe(command, result):
        # Everything here is a disposable installation with generated
        # credentials, so the failing command and the end of its output
        # can be shown. Without them a failed release run names no cause.
        shown = " ".join(str(part) for part in command[:1] + command[-4:])
        output = (result.stdout + result.stderr).decode(errors="replace").strip()
        return "(exit " + str(result.returncode) + "): " + shown + "\n" + output[-3000:]

    def run(command, input=None, required=True, timeout=300, env=None):
        result = subprocess.run(command, cwd=directory, input=input, capture_output=True, timeout=timeout, env=env)
        if required and result.returncode:
            raise RuntimeError("Managed qualification operation failed " + describe(command, result))
        return result

    # compose.ghcr.yaml pins ytmdl-net to one subnet (172.31.250.0/28, which
    # MUSICDL_TRUSTED_PROXIES trusts by default), so every scenario on this
    # host uses the same subnet. A network this scenario leaves behind makes
    # the next scenario's "up" fail, so the teardown has to prove it is gone.
    network = prefix + "-net"
    project_filter = "label=com.docker.compose.project=" + prefix

    def leftovers():
        names = run([args.engine, "network", "ls", "--format", "{{.Name}}"]).stdout.decode().split()
        containers = run([args.engine, "ps", "-a", "--filter", project_filter,
                          "--format", "{{.ID}} {{.Names}} {{.Status}}"]).stdout.decode().strip()
        return [name for name in names if name.startswith(prefix)], containers

    def teardown():
        down = compose + ["down", "--volumes", "--remove-orphans"]
        result = run(down, required=False)
        if result.returncode:
            print("WARN: " + args.engine + " teardown: compose down failed " + describe(down, result), file=sys.stderr, flush=True)
        networks, containers = leftovers()
        if not networks and not containers:
            print("PASS: " + args.engine + " teardown removed " + network + " and the project's containers", flush=True)
            return
        print("WARN: " + args.engine + " teardown left networks " + str(networks) + " and containers ["
              + containers + "]; retrying with the engine's own commands", file=sys.stderr, flush=True)
        # A second down after the first one's stops have settled, then the
        # engine directly for anything still there.
        result = run(down, required=False)
        if result.returncode:
            print("WARN: second compose down failed " + describe(down, result), file=sys.stderr, flush=True)
        for line in leftovers()[1].splitlines():
            run([args.engine, "rm", "--force", "--volumes", line.split()[0]], required=False)
        for name in leftovers()[0]:
            run([args.engine, "network", "rm", name], required=False)
        run([args.engine, "volume", "rm", "--force", prefix + "-postgres-data"], required=False)
        networks, containers = leftovers()
        if networks or containers:
            raise RuntimeError("Teardown could not remove " + str(networks) + " [" + containers
                               + "]; the next scenario would collide on " + network + "'s subnet.")
        print("WARN: " + args.engine + " teardown recovered; " + network + " is gone", file=sys.stderr, flush=True)

    jar = http.cookiejar.CookieJar()
    client = urllib.request.build_opener(urllib.request.ProxyHandler({}), urllib.request.HTTPCookieProcessor(jar))
    base = "http://127.0.0.1:" + str(port)

    def request(path, method="GET", body=None, content_type="application/json"):
        token = next((c.value for c in jar if c.name == "ytmdl_csrf"), None)
        headers = {"Content-Type": content_type}
        if token:
            headers["X-CSRF-Token"] = token
        req = urllib.request.Request(base + "/api/v1" + path, body, headers, method=method)
        try:
            response = client.open(req, timeout=20)
        except urllib.error.HTTPError as error:
            response = error
        with response:
            return response.status, json.loads(response.read())

    def wait_version(version):
        deadline = time.monotonic() + 180
        last = "no answer"
        while time.monotonic() < deadline:
            try:
                status, body = request("/health?scope=essential")
                reported = body.get("data", {}).get("version")
                if reported == version:
                    return
                last = "HTTP " + str(status) + ", version " + str(reported)
            except (OSError, ValueError) as error:
                last = type(error).__name__ + ": " + str(error)
            time.sleep(2)
        raise RuntimeError("Expected version " + version + " did not become healthy within 180 s (last: " + last + ").")

    server = None
    environment = os.environ.copy()
    environment.pop("YTMDL_VERSION", None)
    expected_schema = max(int(f.name.split("_", 1)[0]) for f in (ROOT / "backend/internal/database/migrations").glob("*.sql"))
    if args.manifest:
        manifest_bytes = Path(args.manifest).read_bytes()
        manifest = json.loads(manifest_bytes)
        assert manifest["release_version"] == args.target
        assert manifest["target_schema"] == expected_schema, "Manifest/schema contract mismatch."

        class ReleaseFixture(BaseHTTPRequestHandler):
            def log_message(self, *values):
                pass

            def do_GET(self):
                origin = "http://127.0.0.1:" + str(self.server.server_port)
                if self.path == "/release-manifest.json":
                    payload = manifest_bytes
                else:
                    payload = json.dumps({
                        "tag_name": "v" + args.target, "name": "Qualification release",
                        "draft": False, "prerelease": "-rc." in args.target,
                        "assets": [{"name": name, "browser_download_url": origin + "/" + name} for name in (
                            "release-manifest.json", "SHA256SUMS", "ytmdlctl-linux-amd64",
                            "ytmdlctl-linux-arm64", "ytmdlctl-darwin-amd64", "ytmdlctl-darwin-arm64",
                        ) if (Path(args.manifest).parent / name).is_file()],
                    }).encode()
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(payload)))
                self.end_headers()
                self.wfile.write(payload)

        server = ThreadingHTTPServer(("127.0.0.1", 0), ReleaseFixture)
        threading.Thread(target=server.serve_forever, daemon=True).start()
        environment["YTMDL_TEST_GITHUB_URL"] = "http://127.0.0.1:" + str(server.server_port)
    channel = "development" if "-rc." in args.target else "stable"
    cli = [str(Path(args.cli).resolve()), "--project-dir", str(directory), "--engine", args.engine,
           "--file", "compose.ghcr.yaml", "--base-url", base]
    completed = False
    try:
        run(compose + ["up", "-d"])
        wait_version(args.source)
        print("PASS: " + args.engine + " upgrade source is healthy", flush=True)
        request("/auth/status")
        status, _ = request("/auth/setup", "POST", json.dumps({"username": "upgrade-admin", "password": secrets.token_urlsafe(32)}).encode())
        assert status == 201, "Upgrade fixture setup failed."
        status, session = request("/media-sessions", "POST", b'{"name":"Upgrade fixture","enabled":false}')
        assert status == 201
        session_id = session["data"]["id"]
        cookies = b"# Netscape HTTP Cookie File\n.example.invalid\tTRUE\t/\tFALSE\t2147483647\tfixture\tvalue\n"
        boundary = b"upgrade-fixture"
        upload = b'--' + boundary + b'\r\nContent-Disposition: form-data; name="file"; filename="cookies.txt"\r\nContent-Type: text/plain\r\n\r\n' + cookies + b'\r\n--' + boundary + b'--\r\n'
        assert request("/media-sessions/" + session_id + "/cookies", "POST", upload, "multipart/form-data; boundary=upgrade-fixture")[0] == 200
        snapshot = env_file.read_bytes()
        # Local override rejection must happen before any managed mutation.
        override.write_text(base_override.replace("services:\n", "services:\n  backend:\n    image: localhost/qualification-hotfix:old\n"))
        blocked = run(cli + ["update", "--channel", channel, "--target", args.target, "--dry-run"], required=False, env=environment)
        assert blocked.returncode != 0 and env_file.read_bytes() == snapshot, "Pinned image override was not blocked read-only."
        override.write_text(base_override)
        if guard_enabled:
            marker = "/music/.ytmdl-storage-id"
            run(compose + ["exec", "-T", "backend", "sh", "-c", "mv " + marker + " " + marker + ".qualification-save"])
            blocked = run(cli + ["update", "--channel", channel, "--target", args.target, "--dry-run"], required=False, env=environment)
            assert blocked.returncode != 0 and env_file.read_bytes() == snapshot, "Missing storage identity was not blocked."
            run(compose + ["exec", "-T", "backend", "sh", "-c", "mv " + marker + ".qualification-save " + marker])
        elif args.old_cli:
            blocked = run([str(Path(args.old_cli).resolve()), *cli[1:], "update", "--channel", channel, "--target", args.target, "--yes"], required=False, env=environment)
            assert blocked.returncode != 0 and env_file.read_bytes() == snapshot, "Old CLI disabled-guard case was not contained."
            if channel == "stable":
                failure = (blocked.stdout + blocked.stderr).lower()
                assert b"storage guard" in failure and b"disabled" in failure, "Old CLI failed for an unrelated reason."
        print("PASS: negative update preflight preserves the isolated installation", flush=True)
        run(cli + ["update", "--channel", channel, "--target", args.target, "--dry-run"], env=environment)
        print("PASS: managed update dry run permits the verified target", flush=True)
        result = run(cli + ["update", "--channel", channel, "--target", args.target, "--yes"], env=environment, timeout=600)
        wait_version(args.target)
        expected_env = snapshot.replace(("YTMDL_VERSION=" + args.source + "\n").encode(), ("YTMDL_VERSION=" + args.target + "\n").encode(), 1)
        assert env_file.read_bytes() == expected_env, "Managed update changed unrelated configuration or proxy settings."
        assert override.read_text() == base_override, "Managed update changed host overrides."
        assert request("/auth/me")[0] == 200, "User/session did not survive managed upgrade."
        assert request("/media-sessions/" + session_id)[1]["data"]["has_credentials"], "Managed cookies did not survive upgrade."
        stored = run(compose + ["exec", "-T", "backend", "sha256sum", "/data/cookies/" + session_id + ".cookies.txt"]).stdout.decode().split()[0]
        assert stored == hashlib.sha256(cookies).hexdigest(), "Managed cookie checksum changed."
        schema = run(compose + ["exec", "-T", "db", "psql", "-U", "ytmdl", "-d", "ytmdl", "-Atc", "SELECT max(version) FROM schema_migrations"]).stdout.strip()
        assert schema == str(expected_schema).encode(), "Upgrade did not reach the target schema."
        backups = list((directory / "backups").glob("**/*.dump"))
        assert backups, "Managed update did not create its backup."
        print("PASS:", args.engine, args.source + " -> " + args.target,
              "verified guard" if guard_enabled else "disabled guard",
              "real update, backup, users/cookies/schema and negative preflight cases", flush=True)
        completed = True
    finally:
        try:
            teardown()
        except (OSError, RuntimeError, subprocess.SubprocessError) as error:
            # A failed teardown stops the run, but it must not replace the
            # failure that brought the scenario here; that one is raised.
            print("FAIL: " + args.engine + " teardown: " + str(error), file=sys.stderr, flush=True)
            if completed:
                raise
        finally:
            if server:
                server.shutdown()
                server.server_close()
            shutil.rmtree(directory, ignore_errors=True)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--engine", choices=("docker", "podman"), default="docker")
    parser.add_argument("--cli", required=True)
    parser.add_argument("--source", default="0.28.1")
    parser.add_argument("--target", default=(ROOT / ".release-version").read_text().strip())
    parser.add_argument("--manifest")
    parser.add_argument("--old-cli")
    args = parser.parse_args()
    try:
        for guard_enabled in (True, False):
            qualify(args, guard_enabled)
    except (OSError, RuntimeError, AssertionError, subprocess.SubprocessError):
        import traceback
        print("FAIL: managed update qualification stopped; no production resources were used.", file=sys.stderr)
        # The step that failed and why; see run() for what may be shown.
        traceback.print_exc()
        sys.exit(1)
