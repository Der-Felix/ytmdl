# Configuration Reference

YTMDL is configured through environment variables loaded by the backend container. A template file with default values is provided in `.env.example`.

## Core Service Variables

| Variable | Default | Description |
| :--- | :--- | :--- |
| `MUSICDL_LISTEN_ADDR` | `0.0.0.0:8080` | Address and port for the Go HTTP API. |
| `MUSICDL_DATABASE_URL` | *Required* | PostgreSQL connection string (`postgres://user:pass@db:5432/ytmdl?sslmode=disable`). |
| `MUSICDL_LIBRARY` | `/music` | Target directory for the organized music library. |
| `MUSICDL_TRUSTED_PROXIES` | `127.0.0.1/32,::1/128,172.31.250.0/28` | CIDR blocks trusted for `X-Forwarded-For` client IP resolution. |

## Storage & Reliability

| Variable | Default | Description |
| :--- | :--- | :--- |
| `MUSICDL_STAGING_DIR` | `/data/staging` | Temporary scratch directory for in-progress downloads. |
| `MUSICDL_STORAGE_GUARD_ID` | *Empty* | Expected UUID or marker string in `.ytmdl-storage-id` at library root. |
| `MUSICDL_LIBRARY_MIN_FREE_BYTES` | `5368709120` (5 GiB) | Minimum free disk space required on `/music` before pausing downloads. |
| `MUSICDL_STAGING_MIN_FREE_BYTES` | `2147483648` (2 GiB) | Minimum free disk space required in `/data/staging`. |

## Download & Queue Controls

| Variable | Default | Description |
| :--- | :--- | :--- |
| `MUSICDL_CONCURRENT_DOWNLOADS` | `2` | Maximum simultaneous `yt-dlp` download processes. |
| `MUSICDL_MAX_ATTEMPTS` | `3` | Maximum retry attempts for failed download items before failing. |
| `MUSICDL_WRITE_COVER_FILE` | `true` | Save external `cover.jpg` alongside albums. |
| `MUSICDL_EMBED_COVER` | `true` | Embed album artwork directly into Vorbis comment blocks. |

## Metadata & Lyrics Providers

| Variable | Default | Description |
| :--- | :--- | :--- |
| `MUSICDL_PROVIDERS_SPOTIFY_CLIENT_ID` | *Optional* | Spotify Developer Client ID for metadata resolution. |
| `MUSICDL_PROVIDERS_SPOTIFY_CLIENT_SECRET` | *Optional* | Spotify Developer Client Secret. |
| `MUSICDL_PROVIDERS_GENIUS_ENABLED` | `false` | Enable the optional Genius plain-text lyrics fallback. |
| `MUSICDL_PROVIDERS_GENIUS_ACCESS_TOKEN` | *Optional* | Official Genius Client API Token (enables official search endpoint). |

## Update Checks (v0.15+)

| Variable | Default | Description |
| :--- | :--- | :--- |
| `MUSICDL_UPDATE_CHECKS_ENABLED` | `true` | Enable periodic background checks for new stable GitHub releases. |
| `MUSICDL_UPDATE_REPOSITORY` | `Der-Felix/ytmdl` | Canonical GitHub repository (`owner/repo`) queried for release metadata. |
| `MUSICDL_UPDATE_CHECK_INTERVAL` | `1h` | Cache TTL and minimum interval between remote update queries (min: `5m`). |

> [!NOTE]
> Setting `MUSICDL_UPDATE_CHECKS_ENABLED=false` completely disables all outbound HTTP requests to the GitHub Releases API.
