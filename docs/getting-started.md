# Getting Started with YTMDL

**YTMDL** is a self-hosted music management platform that combines automated music downloading, discography tracking, library organization, and an in-browser web player.

## High-Level Architecture

YTMDL operates as a three-tier service stack orchestrated via Docker or Podman:

```text
┌─────────────────┐       ┌────────────────────────┐       ┌─────────────────┐
│ ytmdl-frontend  │ ────> │     ytmdl-backend      │ ────> │    ytmdl-db     │
│  (Nginx, SPA)   │ :8080 │ (Go API, Workers, Job) │       │ (PostgreSQL 18) │
└─────────────────┘       └────────────────────────┘       └─────────────────┘
                                       │
                    ┌──────────────────┴──────────────────┐
                    ▼                                     ▼
         /data/staging (scratch)               /music (library store)
```

1. **Frontend (SPA):** Modern React application served by Nginx, providing full responsive control for desktop and mobile browsers.
2. **Backend (Go API & Queue):** Concurrency-controlled workers managing metadata searches, `yt-dlp` extraction processes, tag embedding, lyrics resolution, and storage operations.
3. **Database (PostgreSQL 18):** Relational schema tracking users, artist discographies, albums, tracks, job queue tasks, and audit logs.

## Quick Start in 3 Steps

### 1. Configure Your Environment

Download the standard Compose definition and template environment:

```sh
curl -fsSL -O https://raw.githubusercontent.com/Der-Felix/ytmdl/main/compose.ghcr.yaml
curl -fsSL -O https://raw.githubusercontent.com/Der-Felix/ytmdl/main/.env.example
cp .env.example .env
```

Generate secure secrets for PostgreSQL and session tokens:

```sh
# Set strong random secrets and configure your pinned version in .env
openssl rand -hex 24
# Set YTMDL_VERSION=0.17.0 in .env for deterministic deployment and managed updates
```

### 2. Launch the Stack

Start the containers using Podman Compose or Docker Compose:

```sh
# For Podman (using native Compose V2 provider)
podman compose -f compose.ghcr.yaml up -d

# Or for Docker Compose
docker compose -f compose.ghcr.yaml up -d
```

### 3. Complete First-Run Setup

Open your browser at `http://localhost:8080`. The first-run setup wizard will prompt you to create the initial Administrator account. Once created, you are ready to search for artists, subscribe to releases, or import your existing library.

## Next Steps

- Review [Installation & Deployment](/deployment) for production host storage setups.
- Learn about [Storage Identity Guard & Mounts](/storage/) for SMB and NFS.
- Learn about [Updates & Maintenance with ytmdlctl](/updates).
- Explore the [Core Features](/features/providers) of YTMDL.
