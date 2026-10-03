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

## Install the Official Package

Download the versioned installation archive and checksums from the
[v1.2.0 release](https://github.com/Der-Felix/ytmdl/releases/tag/v1.2.0).
Use a fresh directory with Python 3 and Docker Compose v2 or newer, or rootless
Podman with Docker Compose v2 or newer as its provider:

```sh
curl -fLO https://github.com/Der-Felix/ytmdl/releases/download/v1.2.0/ytmdl-1.2.0.tar.gz
curl -fLO https://github.com/Der-Felix/ytmdl/releases/download/v1.2.0/SHA256SUMS
sha256sum --ignore-missing -c SHA256SUMS
# macOS: shasum -a 256 --ignore-missing -c SHA256SUMS
tar -xzf ytmdl-1.2.0.tar.gz
cd ytmdl-1.2.0
python3 scripts/install.py --engine docker
# Or: python3 scripts/install.py --engine podman
```

The installer creates random database credentials, a private `.env`, new writable
`data` and `music` directories, and the storage identity marker. It refuses
existing configuration or data. Use `--prepare-only` to inspect or adjust the
configuration before startup. The package includes `INSTALL.md` and the correct
Compose files for each engine. Existing installations use the [upgrade guide](/updates),
not the fresh installer.

## Create the Administrator

Wait for all three containers to become healthy, then open `http://localhost:8080`
and create the first administrator. Configure HTTPS at your reverse proxy before
exposing the installation publicly. Provider access requires outbound connectivity;
see [proxy configuration](/configuration) if your network uses a proxy.

## Next Steps

- Review [Installation & Deployment](/deployment) for production host storage setups.
- Learn about [Storage Identity Guard & Mounts](/storage/) for SMB and NFS.
- Learn about [Updates & Maintenance with ytmdlctl](/updates).
- Explore the [Core Features](/features/providers) of YTMDL.
