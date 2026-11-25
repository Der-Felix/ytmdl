#!/usr/bin/env python3
"""Exercise release images with a disposable database and isolated backend.

Two layouts are covered: the backend and frontend images (--backend and
--frontend), and the single image that serves the web application itself
(--all-in-one). Both are held to the same web contract: the application shell
with its cache and security headers, immutable assets, the API's own JSON
errors, upload limits and cookie persistence across restart and recreation.
"""

import argparse
import gzip
import hashlib
import http.cookiejar
import json
import re
import secrets
import subprocess
import sys
import time
import urllib.error
import urllib.request


class SmokeFailure(Exception):
    pass


def check(condition, message):
    if not condition:
        raise SmokeFailure(message)


SECURITY_HEADERS = {
    "X-Frame-Options": "SAMEORIGIN",
    "Content-Security-Policy": "frame-ancestors 'self'",
    "X-Content-Type-Options": "nosniff",
}
SHELL_CACHE = "no-cache, must-revalidate"
ASSET_CACHE = "public, max-age=31536000, immutable"


def smoke(args):
    all_in_one = args.all_in_one is not None
    prefix = "ytmdl-smoke-" + secrets.token_hex(6)
    network = prefix + "-net"
    ingress = prefix + "-ingress"
    volumes = [prefix + suffix for suffix in ("-db", "-data", "-music")]
    containers = [prefix + suffix for suffix in ("-db", "-backend", "-frontend")]
    database, backend, frontend = containers
    if all_in_one:
        # One container is backend and web server at once.
        containers = containers[:2]
        frontend = backend

    def engine(*command, required=True):
        result = subprocess.run(
            [args.engine, *command], capture_output=True, text=True, timeout=180
        )
        if required:
            check(result.returncode == 0, "Container operation failed: " + command[0])
        return result.stdout.strip()

    def wait_for(probe, message):
        deadline = time.monotonic() + 180
        while time.monotonic() < deadline:
            if probe():
                return
            time.sleep(2)
        raise SmokeFailure(message)

    jar = http.cookiejar.CookieJar()
    client = urllib.request.build_opener(
        urllib.request.ProxyHandler({}), urllib.request.HTTPCookieProcessor(jar)
    )
    base = ""

    def request(path, method="GET", body=None, content_type="application/json"):
        headers = {}
        if body is not None:
            headers["Content-Type"] = content_type
        token = next((c.value for c in jar if c.name == "ytmdl_csrf"), None)
        if token:
            headers["X-CSRF-Token"] = token
        req = urllib.request.Request(base + path, body, headers, method=method)
        try:
            with client.open(req, timeout=60) as response:
                status, raw = response.status, response.read()
        except urllib.error.HTTPError as error:
            status, raw = error.code, error.read()
        try:
            payload = json.loads(raw)
        except ValueError:
            payload = {}
        return status, payload

    def json_request(path, body):
        return request(path, "POST", json.dumps(body).encode())

    def fetch(path, headers=None):
        req = urllib.request.Request(base + path, headers=headers or {})
        try:
            with client.open(req, timeout=60) as response:
                return response.status, response.headers, response.read()
        except urllib.error.HTTPError as error:
            return error.code, error.headers, error.read()

    def rejected(path, body, content_type, statuses):
        # nginx reads an oversized body before it answers. The backend
        # answers at once and may close the connection while the client is
        # still sending; a closed connection is a rejection as well.
        try:
            status, _ = request(path, "POST", body, content_type)
        except OSError:
            return all_in_one
        return status in statuses

    def check_security_headers(headers, what):
        for name, value in SECURITY_HEADERS.items():
            check(headers.get(name) == value, what + " lacks " + name + ": " + value)

    def check_web_contract():
        status, headers, shell = fetch("/")
        check(status == 200 and b'id="root"' in shell, "/ does not serve the application shell.")
        check(headers.get("Content-Type", "").startswith("text/html"), "The shell is not served as HTML.")
        check(headers.get("Cache-Control") == SHELL_CACHE, "The shell must be revalidated on every load.")
        check_security_headers(headers, "The shell")

        status, headers, body = fetch("/library/artists/smoke-route")
        check(status == 200 and body == shell, "A client-side route does not fall back to the shell.")
        check(headers.get("Cache-Control") == SHELL_CACHE, "The fallback shell must be revalidated.")

        match = re.search(rb'/assets/[A-Za-z0-9._-]+\.js', shell)
        check(match is not None, "The shell names no script bundle.")
        asset = match.group(0).decode()
        status, headers, plain = fetch(asset)
        check(status == 200 and plain, "The script bundle is not served.")
        check(headers.get("Cache-Control") == ASSET_CACHE, "Assets must be cached as immutable.")
        check("javascript" in headers.get("Content-Type", ""), "The script bundle has the wrong type.")
        check_security_headers(headers, "An asset")
        status, headers, packed = fetch(asset, {"Accept-Encoding": "gzip"})
        check(status == 200 and headers.get("Content-Encoding") == "gzip", "The script bundle is not sent gzip encoded.")
        check(gzip.decompress(packed) == plain, "The gzip encoded bundle differs from the plain one.")

        status, headers, body = fetch("/assets/smoke-missing-0000.js")
        check(status == 404 and b'id="root"' not in body, "A missing asset must be a 404, not the shell.")

        status, headers, body = fetch("/api/v1/health?scope=essential")
        check(status == 200 and json.loads(body)["data"]["version"] == args.version, "The API health is not served.")
        check_security_headers(headers, "The API")

        status, headers, body = fetch("/api/smoke-unknown")
        check(400 <= status < 500, "An unknown API path must be a client error.")
        check(headers.get("Content-Type", "").startswith("application/json") and "error" in json.loads(body),
              "An unknown API path must be answered by the API, not the shell.")
        check_security_headers(headers, "An API error")

    def healthy():
        try:
            status, payload = request("/api/v1/health?scope=essential")
            return status == 200 and payload.get("data", {}).get("version") == args.version
        except (OSError, urllib.error.URLError):
            return False

    def multipart(data):
        boundary = "ytmdl-smoke-boundary"
        body = (
            b"--" + boundary.encode() + b"\r\n"
            b'Content-Disposition: form-data; name="file"; filename="cookies.txt"\r\n'
            b"Content-Type: text/plain\r\n\r\n" + data
            + b"\r\n--" + boundary.encode() + b"--\r\n"
        )
        return body, "multipart/form-data; boundary=" + boundary

    try:
        engine("network", "create", "--internal", network)
        # Docker suppresses published ports on internal networks. Only the
        # frontend joins this second network; the backend and DB stay isolated.
        engine("network", "create", ingress)
        for volume in volumes:
            engine("volume", "create", volume)
        # Synthetic credentials exist only for the lifetime of this stack.
        password = secrets.token_hex(24)
        engine(
            "run", "-d", "--name", database, "--network", network,
            "--network-alias", "db", "-e", "POSTGRES_DB=ytmdl_smoke",
            "-e", "POSTGRES_USER=ytmdl", "-e", "POSTGRES_PASSWORD=" + password,
            "-v", volumes[0] + ":/var/lib/postgresql", "docker.io/library/postgres:18-alpine",
        )
        wait_for(
            lambda: subprocess.run(
                [args.engine, "exec", database, "pg_isready", "-U", "ytmdl", "-d", "ytmdl_smoke"],
                capture_output=True, timeout=15,
            ).returncode == 0,
            "Disposable PostgreSQL did not become ready.",
        )
        if all_in_one:
            # The single container is published itself: it starts on the
            # ingress network and joins the isolated one for the database.
            backend_command = (
                "run", "-d", "--name", backend, "--network", ingress,
                "-e", "MUSICDL_DATABASE_URL=postgres://ytmdl:" + password + "@db:5432/ytmdl_smoke?sslmode=disable",
                "-v", volumes[1] + ":/data", "-v", volumes[2] + ":/music",
                "-p", "127.0.0.1::8080", args.all_in_one,
            )
        else:
            backend_command = (
                "run", "-d", "--name", backend, "--network", network,
                "--network-alias", "backend",
                "-e", "MUSICDL_DATABASE_URL=postgres://ytmdl:" + password + "@db:5432/ytmdl_smoke?sslmode=disable",
                "-v", volumes[1] + ":/data", "-v", volumes[2] + ":/music", args.backend,
            )

        def start_backend():
            nonlocal base
            engine(*backend_command)
            if all_in_one:
                engine("network", "connect", network, backend)
                base = published_base()

        def published_base():
            port = engine("port", frontend, "8080/tcp").splitlines()[0].rsplit(":", 1)[1]
            check(port.isdigit(), "Published port could not be determined.")
            return "http://127.0.0.1:" + port

        start_backend()
        if not all_in_one:
            engine(
                "run", "-d", "--name", frontend, "--network", ingress,
                "-p", "127.0.0.1::8080", args.frontend,
            )
            engine("network", "connect", network, frontend)
            base = published_base()
        wait_for(healthy, "Stack did not serve the expected healthy release.")
        engine("exec", backend, "fpcalc", "-version")
        check(engine("exec", backend, "id", "-u") == "10001", "The backend does not run as UID 10001.")
        print("PASS: release images start with Chromaprint and serve the expected version", flush=True)

        check_web_contract()
        print("PASS: shell, assets, security headers and API errors match the web contract", flush=True)

        request("/api/v1/auth/status")
        status, _ = json_request("/api/v1/auth/setup", {
            "username": "release-smoke", "display_name": "Release Smoke",
            "password": secrets.token_urlsafe(32),
        })
        check(status == 201, "Administrator setup failed.")
        status, payload = request("/api/v1/settings")
        check(status == 200 and payload["data"]["combined_audio_fallback"] is False,
              "Combined-stream fallback must default to disabled.")

        size = 25 * 1024 * 1024
        header = b"# Netscape HTTP Cookie File\n.example.invalid\tTRUE\t/\tFALSE\t2147483647\tsmoke\tfixture\n"
        # Short comment lines fill the file to the exact accepted boundary.
        remaining = size - len(header)
        comment = b"#" + b"x" * 1022 + b"\n"
        tail = remaining % len(comment)
        cookies = header + comment * (remaining // len(comment))
        if tail:
            cookies += b"\n" if tail == 1 else b"#" + b"x" * (tail - 2) + b"\n"
        digest = hashlib.sha256(cookies).hexdigest()
        ids = []
        for route in ("/api/v1/media-sessions", "/api/v1/admin/media-sessions"):
            status, payload = json_request(route, {"name": "Release smoke", "enabled": False})
            check(status == 201, "Media-session creation failed.")
            session_id = payload["data"]["id"]
            check(bool(re.fullmatch(r"[a-zA-Z0-9_-]+", session_id)), "Invalid fixture session id.")
            ids.append(session_id)
            body, content_type = multipart(cookies)
            status, payload = request(route + "/" + session_id + "/cookies", "POST", body, content_type)
            check(status == 200 and payload["data"]["session"]["has_credentials"],
                  "Exact 25 MiB multipart cookie upload failed.")
            body, content_type = multipart(cookies + b"\n")
            status, payload = request(route + "/" + session_id + "/cookies", "POST", body, content_type)
            check(status == 400 and payload.get("error", {}).get("code") == "INVALID_REQUEST",
                  "A cookie file above 25 MiB was not rejected by the API.")
        print("PASS: both upload aliases accept 25 MiB and reject larger files", flush=True)

        check(rejected("/api/v1/media-sessions", b" " * (1024 * 1024 + 1), "application/json", (400,)),
              "Ordinary API requests must retain their 1 MiB body limit.")
        body, content_type = multipart(cookies + b"\n" * (1024 * 1024))
        # nginx answers 413 itself; without it, the API's own 26 MiB limit
        # answers 400.
        check(rejected("/api/v1/media-sessions/" + ids[0] + "/cookies", body, content_type,
                       (400,) if all_in_one else (413,)),
              "Request bodies above 26 MiB must be rejected.")

        def check_persistence():
            for session_id in ids:
                filename = "/data/cookies/" + session_id + ".cookies.txt"
                stored = engine("exec", backend, "sha256sum", filename).split()[0]
                permissions = engine("exec", backend, "stat", "-c", "%a", filename)
                check(stored == digest and permissions == "600", "Stored cookie content or permissions changed.")
                status, payload = request("/api/v1/media-sessions/" + session_id)
                check(status == 200 and payload["data"]["has_credentials"], "Cookie metadata was not preserved.")

        check_persistence()
        engine("restart", "--time", "40", backend)
        if all_in_one:
            # A restart may publish the container on another host port.
            base = published_base()
        wait_for(healthy, "Backend restart did not recover.")
        check_persistence()
        engine("stop", "--time", "40", backend)
        engine("rm", backend)
        start_backend()
        wait_for(healthy, "Backend recreation did not recover.")
        check_persistence()
        print("PASS: cookies and authentication survive restart and recreation", flush=True)
    finally:
        # Only this invocation's randomly named resources are removed.
        for container in reversed(containers):
            engine("rm", "-f", container, required=False)
        for volume in volumes:
            engine("volume", "rm", volume, required=False)
        engine("network", "rm", ingress, required=False)
        engine("network", "rm", network, required=False)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--engine", choices=("docker", "podman"), default="docker")
    parser.add_argument("--backend")
    parser.add_argument("--frontend")
    parser.add_argument("--all-in-one", help="the single image that serves the web application itself")
    parser.add_argument("--version", required=True)
    args = parser.parse_args()
    if (args.all_in_one is None) == (args.backend is None or args.frontend is None):
        parser.error("give either --backend and --frontend, or --all-in-one")
    if args.all_in_one is not None and (args.backend or args.frontend):
        parser.error("--all-in-one replaces --backend and --frontend")
    try:
        smoke(args)
    except SmokeFailure as error:
        print("FAIL: " + str(error), file=sys.stderr)
        return 1
    except (OSError, ValueError, KeyError, subprocess.TimeoutExpired):
        # No URLs, credentials, response bodies or container output in diagnostics.
        print("FAIL: isolated release container smoke test", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
