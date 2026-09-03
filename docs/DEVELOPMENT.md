# Development Guide

This document describes how to set up, build, test, and contribute to YTMDL locally.

## Development Model & Upstream Repository

Canonical day-to-day development, continuous integration, and release preparation for YTMDL take place in an upstream development repository. The public GitHub repository publishes stable releases, tags, and documentation, and welcomes community issues, feature requests, and pull requests.

## Prerequisites

- **Go:** 1.22 or newer (1.26 recommended)
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
  -p 55432:5432 docker.io/library/postgres:18-alpine

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
bun run typecheck

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
