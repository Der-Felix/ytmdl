# Development Guide

This document describes how to set up, build, test, and contribute to YTMDL locally.

## GitHub-only Development Model

Development and integration take place exclusively at
[Der-Felix/ytmdl on GitHub](https://github.com/Der-Felix/ytmdl). Create a feature
branch from the latest `dev` and submit a PR to `dev`. Wait for the final commit's
GitHub CI and follow applicable branch protection before merging. `main` is the
reviewed stable branch; promotion, release tags and production deployment need
separate scope and approval. Do not push private history, internal configuration,
credentials or local overrides. See [CONTRIBUTING](https://github.com/Der-Felix/ytmdl/blob/dev/CONTRIBUTING.md).

The non-publishing `.github/workflows/ci.yml` checks frontend tests/lint/build
and backend formatting/vet/build/tests/race tests with isolated PostgreSQL 18.
A development PR does not invoke the release or documentation deployment jobs.

## Prerequisites

- **Go:** the version required by `backend/go.mod` (currently 1.26.6)
- **Node.js / Bun:** Bun 1.1+ (or Node 20+ with npm)
- **Container Runtime:** Podman or Docker with Compose
- **PostgreSQL:** 18 server and client tools (for running integration tests)
- **CLI Tools:** `ffmpeg`, `ffprobe`, `yt-dlp`

## Local Development Setup

### 1. Backend

The Go backend code is located in the `backend/` directory.

```sh
cd backend

# Format and analyze code
gofmt -w .
go vet ./...

# Build server binary
go build -o server ./cmd/server
```

#### Running Backend Tests

Backend tests include both unit tests and PostgreSQL integration tests. If `MUSICDL_TEST_DATABASE_URL` is set, integration tests run automatically with an isolated schema per test package:

```sh
# Optional: Spin up a local PostgreSQL test container
podman run -d --rm --name ytmdl-pgtest \
  -e POSTGRES_USER=ytmdl -e POSTGRES_PASSWORD=testpw -e POSTGRES_DB=ytmdl_test \
  -p 127.0.0.1:55432:5432 docker.io/library/postgres:18-alpine

# Run tests
export MUSICDL_TEST_DATABASE_URL='postgres://ytmdl:testpw@127.0.0.1:55432/ytmdl_test?sslmode=disable'
go test -count=1 ./...

# Race condition detection
go test -race ./...
```

### 2. Frontend

The frontend is a single-page application built with React, TypeScript, Vite, Tailwind CSS, and Base UI located in `frontend/`.

```sh
cd frontend

# Install dependencies
bun install

# Run development server with live reload (proxies /api to backend:8080)
bun run dev

# Run unit and component tests
bun test

# Type check
bunx tsc -b

# Lint with Oxlint
bun run lint

# Production build
bun run build
```

## Running Full Stack Locally via Compose

To test the entire stack as a cohesive deployment:

```sh
# In repository root
cp .env.example .env
podman compose up -d --build
# or: docker compose up -d --build
```

Access the web interface at `http://localhost:8080`.
## Release container smoke test

After building the backend and frontend images, run the release smoke test with
Python 3 and Docker (or add `--engine podman`):

```sh
python3 scripts/smoke-release-containers.py \
  --backend ytmdl-backend:release-smoke \
  --frontend ytmdl-frontend:release-smoke \
  --version 1.1.0-rc.1
```

The test creates uniquely named networks, disposable PostgreSQL 18,
volumes and containers. Backend and database use an internal network without
internet access; the frontend also joins an ingress network to publish its test
port on host loopback under both Docker and Podman. It checks first-run setup, the default-disabled combined
fallback, exact 25 MiB multipart uploads through both route aliases, oversized
file/request rejection, and cookie content, permissions and authentication after
backend restart and recreation. It uses synthetic cookies, sends no provider
requests, and removes its own containers, volumes and networks when finished.

The release workflow runs this test before publishing. Its manual verify-only
mode also builds both container architectures, compiles the host CLI binaries,
and validates the manifest without publishing artifacts:

```sh
gh workflow run release.yml --repo Der-Felix/ytmdl --ref dev -f verify_only=true
```

## v1 Release Qualification

`scripts/test-installation.py` checks private configuration, refusal of existing
files and the reproducible package whitelist. `scripts/qualify-compose.py` uses
the packaged Compose stack, an isolated synthetic catalogue and a real FLAC tone
to exercise setup, search, acquisition, PostgreSQL backup/restore and recreation.
It runs with Docker and rootless Podman. The browser qualification covers Chromium
setup/search/acquisition/playback and ordinary-user role denial, plus Firefox
login and playback. It does not measure real provider throughput.

Before publication, the release workflow scans both immutable image architectures,
records SBOMs and runtime tool versions, exercises ARM64 runtime under QEMU, and
performs real v0.28.1 upgrades for both engines and both storage guard modes.
Only then does it publish the checked draft and promote immutable images to
`latest`. Release assets include `qualification.json`, full scan reports and
SPDX inventories; every asset is covered by `SHA256SUMS`. Manual verify-only runs
publish no images or releases and report the tag-only gates as unqualified.
