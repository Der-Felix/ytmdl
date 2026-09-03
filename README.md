# YTMDL

> **Self-hosted music downloader, library manager, and integrated web player.**

YTMDL is an independent open-source project by Felix Möschen. It resolves artist discographies through structured metadata providers, matches tracks against audio streams, downloads native Opus audio using `yt-dlp`, verifies streams with `ffprobe`, embeds Vorbis comments and cover art, and organizes files into a standardized music library compatible with Plex, Jellyfin, Navidrome, and Emby.

📖 **Official Documentation:** [https://der-felix.github.io/ytmdl/](https://der-felix.github.io/ytmdl/)

> [!NOTE]
> **Project Status:** Pre-1.0 / Active Development. APIs, database schemas, and configuration options may evolve between minor versions.

---

## Features

- **High-Fidelity Audio:** Prefers native Opus streams where available. Remuxes stream-copy into Ogg/Opus containers without unnecessary lossy re-encoding; verifies bitrate, channels, and sample rate with `ffprobe`.
- **Integrated Web Player:** Modern in-browser audio player with persistent mini-player, full-screen Now Playing view, queue management, 10-band graphic EQ, parametric filters, crossfade, visualizer, and Media Session API.
- **Multi-Tier Lyrics:** Automatic lyrics retrieval through LRCLIB (synced `.lrc` and plain `.txt`), YouTube Music, and an optional Genius fallback.
- **Automated Subscriptions:** Track artist discographies, automatically queue new releases, and sync catalogs with starvation-protected fair scheduling.
- **Media Server Ready:** Strict directory naming (`Artist/YYYY - Album/NN - Title.opus`), multi-disc numbering (`101 - Title.opus`), embedded cover art, and external `cover.jpg` / `.lrc` sidecars.
- **User Management & Security:** First-run setup wizard, role-based access control (Admin / User), Argon2id password hashing, server-side session management, and CSRF protection.
- **Library Auditing & Repair:** Non-destructive Quick/Deep audit engine, repair preview, metadata retagging, and safe quarantine isolation (`.ytmdl-trash`).
- **Reliable Storage:** Two-phase atomic staging (`/data/staging` -> `/music`) with Storage Identity Guard for local disks and host-mounted SMB/CIFS shares.

---

## Screenshots

| Dashboard & Search | Library & Discography |
| :---: | :---: |
| *(Screenshot Placeholder)* | *(Screenshot Placeholder)* |

| Web Player & Lyrics | Equalizer & DSP |
| :---: | :---: |
| *(Screenshot Placeholder)* | *(Screenshot Placeholder)* |

---

## Architecture

```text
┌─────────────────┐       ┌────────────────────────┐       ┌─────────────────┐
│ ytmdl-frontend  │ ────> │     ytmdl-backend      │ ────> │    ytmdl-db     │
│  (Nginx, SPA)   │ :8080 │ (Go API, Workers, Job) │       │ (PostgreSQL 18) │
└─────────────────┘       └────────────────────────┘       └─────────────────┘
                                       │
                    ┌──────────────────┴──────────────────┐
                    ▼                                     ▼
             /data (Staging)                       /music (Library)
          (Local scratch volume)               (Local disk or SMB share)
```

- **Frontend:** Single-page application built with React 19, TypeScript, Tailwind CSS, and Base UI, served via Nginx.
- **Backend:** Go 1.26 monolith combining HTTP API routing, queue management, worker pools, and metadata parsers.
- **Database:** PostgreSQL 18 storing catalogs, subscriptions, sessions, audit findings, and job histories.

---

## Requirements

- **Operating System:** Linux or macOS.
- **Container Runtime:** Podman or Docker with Compose support.
- **Hardware:** Minimum 1 GHz x86-64 or ARM64 CPU, 1 GB RAM, plus adequate storage for your music library.

---

## Quick Start

### 1. Clone the Repository

```sh
git clone https://github.com/Der-Felix/ytmdl.git
cd ytmdl
```

### 2. Configure Environment

Copy the example environment file and set a secure database password:

```sh
cp .env.example .env
```

Edit `.env` and configure `POSTGRES_PASSWORD` and `MUSICDL_DATABASE_URL`:

```env
POSTGRES_PASSWORD=your_secure_password_here
MUSICDL_DATABASE_URL=postgres://ytmdl:your_secure_password_here@db:5432/ytmdl?sslmode=disable
```

### 3. Start the Stack

Build and start the application containers using Podman Compose or Docker Compose:

```sh
# Using Podman Compose:
podman compose up -d --build

# Or using Docker Compose:
docker compose up -d --build
```

### 4. Access the Web Interface

Open your browser and navigate to:

```text
http://localhost:8080
```

1. **First-Run Setup:** The setup wizard will prompt you to create the initial administrator account.
2. **Library Configuration:** Verify that your music folder is mapped to `/music` and accessible.

For detailed production deployment guides, reverse proxy configurations, and prebuilt GHCR images, see [docs/deployment.md](docs/deployment.md).

---

## Storage & Support Matrix

YTMDL enforces a two-phase atomic storage model:
1. **Local Staging (`/data/staging`):** Downloads, remuxing, tagging, and validation occur locally on the fast staging volume.
2. **Atomic Commit (`/music`):** Completed tracks are validated (SHA-256) and moved atomically to the target directory.

| Storage Type | Support Status | Requirements & Details |
| :--- | :--- | :--- |
| **Local Filesystem** | **SUPPORTED** | Direct SSD/HDD paths, bind mounts, or container volumes. |
| **Host-Mounted SMB/CIFS** | **SUPPORTED** | Host-level mount with CIFS vers=3.1.1, UID/GID 10001, and Storage Identity Guard. See [docs/storage/smb.md](docs/storage/smb.md). |
| **Host-Mounted NFS** | **EXPERIMENTAL** | Host-level NFSv4 mount. Verification in progress. See [docs/storage/nfs.md](docs/storage/nfs.md). |

---

## Metadata Providers

- **YouTube Music (Default):** Resolves artists, albums, tracks, and native audio stream identifiers.
- **Deezer:** Comprehensive metadata catalog with configurable client-side rate limiting (`MUSICDL_DEEZER_REQUESTS_PER_SECOND`).
- **Spotify (Optional):** Requires `YTDM_SPOTIFY_CLIENT_ID` and `YTDM_SPOTIFY_CLIENT_SECRET`.

---

## Lyrics

YTMDL resolves lyrics sequentially through a tiered provider chain:

1. **LRCLIB (Primary):** Retrieves synchronized (`.lrc`) or plain text (`.txt`) lyrics with timestamps.
2. **YouTube Music (Fallback):** Provides plain text lyrics for tracks missing on LRCLIB.
3. **Genius (Optional Fallback):** Best-effort fallback for plain text lyrics (`.txt`). Disabled by default; can be enabled in Server Settings or via `.env`. Supports optional `GENIUS_ACCESS_TOKEN` for official Search API access.
4. **Missing:** Marked as `not_found` without writing empty files.

Lyrics can be embedded into audio tags or written alongside tracks as `.lrc` / `.txt` sidecar files.

---

## Web Player

The built-in Web Player allows instant playback of your music library directly in the browser:

- **Persistent Mini-Player:** Remains active across navigation with progress seek, volume, and playback controls.
- **Now Playing Screen (`/player`):** Full-screen view with album artwork, synchronized/plain lyrics auto-scroller, and track queue.
- **Queue Management:** Reorder tracks, add albums to queue, play next, or clear queue.
- **Audio DSP & Equalizer:** Client-side 10-band graphic equalizer, parametric peaking filters, preamp gain, and crossfade powered by the Web Audio API.
- **Audio Visualizer:** Real-time canvas frequency spectrum analyzer.
- **Media Session API:** Supports hardware media keys, Bluetooth controls, and operating system lock-screen widgets.

*Note: All audio digital signal processing (DSP) runs client-side within your web browser; original audio files on disk are never altered.*

---

## Subscriptions & Queue Management

- **Artist Subscriptions:** Automatically monitors subscribed artists for new releases.
- **Fair Scheduling:** Interleaves jobs with low, normal, and high priorities so smaller downloads are not starved by massive discographies.
- **Time Windows:** Restrict automated downloads to specific off-peak hours (e.g., `22:00 - 06:00`).
- **Pacing & Limits:** Global bandwidth throttles and per-provider rate limiters protect upstream endpoints.

---

## Security & Deployment Guidelines

- **First-Run Initialization:** Initial administrator setup is locked once the first user is created.
- **Password Security:** Credentials hashed using Argon2id ($m=64\,\text{MiB}, t=3, p=2$).
- **Session Protection:** Random 256-bit session tokens stored in secure, SameSite cookies with SHA-256 hashed DB persistence.
- **CSRF Protection:** Double-submit cookie pattern with `X-CSRF-Token` validation for state-changing requests.
- **Reverse Proxy Recommended:** When exposing YTMDL outside a private local network, place it behind a TLS-terminating reverse proxy (Nginx, Caddy, or Traefik) and configure `MUSICDL_TRUSTED_PROXIES` and `MUSICDL_COOKIE_SECURE=true`.

---

## Updating

To update an existing installation to the latest version:

```sh
git pull
podman compose up -d --build
# or: docker compose up -d --build
```

Database migrations run automatically at backend startup. Always create a backup before upgrading:

```sh
podman exec ytmdl-db pg_dump -U ytmdl -d ytmdl -Fc > backups/backup_$(date +%Y%m%d).dump
```

---

## Development & Testing

### Backend (Go)

```sh
cd backend
gofmt -w .
go vet ./...
go test -count=1 ./...
go build ./cmd/server
```

### Frontend (TypeScript / React)

```sh
cd frontend
bun install
bun test
bun run build
```

---

## Contributing

Contributions, bug reports, and suggestions are welcome! Please read [CONTRIBUTING.md](CONTRIBUTING.md) for details on code standards, local testing, and pull request workflows.

---

## License

YTMDL is licensed under the **Apache License, Version 2.0**. You may use, modify, distribute, and sell the software according to the terms of that license.

See the [LICENSE](LICENSE) file for the full license text and [NOTICE](NOTICE) for project attribution. Third-party dependency notices are listed in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).

---

## Legal Notice

YTMDL is a general-purpose metadata organizer and media utility. **YTMDL is intended for lawful use only.**

Users are solely responsible for ensuring that their use complies with local copyright laws, applicable exceptions (such as private copying regulations where applicable), and third-party terms of service. YTMDL does not host, provide, or distribute media content.

For complete legal information, statutory frameworks, and disclaimers, please read [LEGAL.md](LEGAL.md).
