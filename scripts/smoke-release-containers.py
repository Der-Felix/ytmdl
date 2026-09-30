#!/usr/bin/env python3
"""Exercise built release images in a disposable, internet-isolated stack."""

import argparse
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


def smoke(args):
    prefix = "ytmdl-smoke-" + secrets.token_hex(6)
    network = prefix + "-net"
    volumes = [prefix + suffix for suffix in ("-db", "-data", "-music")]
    containers = [prefix + suffix for suffix in ("-db", "-backend", "-frontend")]
    database, backend, frontend = containers

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
        backend_command = (
            "run", "-d", "--name", backend, "--network", network,
            "--network-alias", "backend",
            "-e", "MUSICDL_DATABASE_URL=postgres://ytmdl:" + password + "@db:5432/ytmdl_smoke?sslmode=disable",
            "-v", volumes[1] + ":/data", "-v", volumes[2] + ":/music", args.backend,
        )
        engine(*backend_command)
        engine(
            "run", "-d", "--name", frontend, "--network", network,
            "-p", "127.0.0.1::8080", args.frontend,
        )
        port = engine("port", frontend, "8080/tcp").splitlines()[0].rsplit(":", 1)[1]
        check(port.isdigit(), "Frontend port could not be determined.")
        base = "http://127.0.0.1:" + port
        wait_for(healthy, "Stack did not serve the expected healthy release.")
        print("PASS: release images start and serve the expected version", flush=True)

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

        status, _ = request("/api/v1/media-sessions", "POST", b" " * (1024 * 1024 + 1))
        check(status == 400, "Ordinary API requests must retain their 1 MiB body limit.")
        body, content_type = multipart(cookies + b"\n" * (1024 * 1024))
        status, _ = request("/api/v1/media-sessions/" + ids[0] + "/cookies", "POST", body, content_type)
        check(status == 413, "Nginx must reject request bodies above 26 MiB.")

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
        wait_for(healthy, "Backend restart did not recover.")
        check_persistence()
        engine("stop", "--time", "40", backend)
        engine("rm", backend)
        engine(*backend_command)
        wait_for(healthy, "Backend recreation did not recover.")
        check_persistence()
        print("PASS: cookies and authentication survive restart and recreation", flush=True)
    finally:
        # Only this invocation's randomly named resources are removed.
        for container in reversed(containers):
            engine("rm", "-f", container, required=False)
        for volume in volumes:
            engine("volume", "rm", volume, required=False)
        engine("network", "rm", network, required=False)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--engine", choices=("docker", "podman"), default="docker")
    parser.add_argument("--backend", required=True)
    parser.add_argument("--frontend", required=True)
    parser.add_argument("--version", required=True)
    args = parser.parse_args()
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
