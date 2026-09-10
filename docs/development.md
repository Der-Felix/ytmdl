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
- **PostgreSQL:** 16+ or 18 (for running integration tests)
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
