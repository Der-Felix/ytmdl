# Configuration Reference

YTMDL's backend is configured from **environment variables** (and, optionally, a
YAML file). In the standard container deployment every value comes from `.env`,
which Compose passes to the `ytmdl-backend` container. A template with safe
example values ships as [`.env.example`](https://github.com/Der-Felix/ytmdl/blob/main/.env.example).

## How configuration is resolved

The backend builds its configuration in this order, where each later step wins:

1. **Built-in defaults** (documented in the tables below).
2. **YAML file** — only if a `config.yaml` exists at the working directory, or a
   path is passed explicitly. The official container images do **not** mount one,
   so this is rarely used.
3. **Environment variables** — always override the YAML file and the defaults.

### Variable name prefixes

| Prefix | Meaning |
| :--- | :--- |
| `MUSICDL_*` | The canonical, supported container contract. Prefer these. |
| `YTDM_*` | Legacy aliases kept for backwards compatibility. |

When the same setting is provided under both prefixes, **`MUSICDL_*` wins**. For
the core settings (`MUSICDL_LISTEN_ADDR`, `MUSICDL_LIBRARY`,
`MUSICDL_CONCURRENT_DOWNLOADS`, `MUSICDL_YTDLP`, `MUSICDL_FFMPEG`,
`MUSICDL_FFPROBE`, `MUSICDL_DATABASE_URL`) a present canonical value also
suppresses parsing of the legacy alias, so a stale `YTDM_*` value cannot make a
valid `MUSICDL_*` configuration fail to start.

> [!WARNING]
> The removed SQLite variables `MUSICDL_DATABASE` and `YTDM_DATABASE_PATH` are
> rejected with a clear startup error. Use `MUSICDL_DATABASE_URL` (PostgreSQL).

### Dynamic settings (no restart)

A subset of settings — worker concurrency, lyrics toggles, minimum match score,
the Genius fallback — can also be changed at runtime by an administrator in
**Settings** (`PUT /api/v1/settings`). Runtime changes are stored in the database
and take precedence over the environment for those specific fields.

---

## Core Service

| Variable | Default | Description |
| :--- | :--- | :--- |
| `MUSICDL_LISTEN_ADDR` | `0.0.0.0:8080` | Address and port for the Go HTTP API. |
| `MUSICDL_DATABASE_URL` | *(required)* | PostgreSQL connection string, e.g. `postgres://ytmdl:pass@db:5432/ytmdl?sslmode=disable`. |
| `MUSICDL_LIBRARY` | `/music` *(container)* | Target directory for the organised music library. |
| `MUSICDL_TRUSTED_PROXIES` | `127.0.0.1/32,::1/128` | Comma-separated CIDRs/IPs trusted for `X-Forwarded-For`. The bundled compose files pass `127.0.0.1/32,::1/128,172.31.250.0/28` so the frontend proxy is trusted out of the box. |
| `MUSICDL_COOKIE_SECURE` | `false` | Force the `Secure` flag on session cookies (enable when serving over HTTPS). |

### Database pool

| Variable | Default | Description |
| :--- | :--- | :--- |
| `MUSICDL_DB_MAX_CONNS` | `10` | Maximum pooled server connections. Raise only together with worker count. |
| `MUSICDL_DB_MIN_CONNS` | `2` | Minimum idle connections kept open. |
| `MUSICDL_DB_MAX_CONN_LIFETIME` | `30m` | Maximum lifetime of a pooled connection. |
| `MUSICDL_DB_MAX_CONN_IDLE_TIME` | `5m` | Idle timeout before a connection is closed. |
| `MUSICDL_DB_CONNECT_TIMEOUT` | `10s` | Timeout for a single connection attempt. |
| `MUSICDL_DB_STARTUP_TIMEOUT` | `90s` | Total budget spent waiting for PostgreSQL during startup. |
| `MUSICDL_DB_STARTUP_BACKOFF` | `1s` | First retry delay while waiting for PostgreSQL (doubles, capped at 5s). |

---

## Storage & Reliability

| Variable | Default | Description |
| :--- | :--- | :--- |
| `MUSICDL_STAGING_DIR` | *(empty → internal default)* | Persistent scratch directory for in-progress downloads. In the container this lives under `/data`. |
| `MUSICDL_STORAGE_GUARD_ID` | *(empty)* | Expected content of `.ytmdl-storage-id` at the library root. Empty disables the Storage Identity Guard. See [Storage](/storage/). |
| `MUSICDL_LIBRARY_MIN_FREE_BYTES` | `0` *(disabled)* | Minimum free bytes on the library filesystem before downloads pause. `0` disables the check. |
| `MUSICDL_STAGING_MIN_FREE_BYTES` | `0` *(disabled)* | Minimum free bytes in the staging directory. `0` disables the check. |
| `MUSICDL_STAGING_MAX_BYTES` | `0` *(unbounded)* | Soft cap on total staging usage. |
| `MUSICDL_ALLOW_OFFLINE_STAGING` | `false` | Allow `yt-dlp` to keep staging locally while the library mount is offline. |

> [!NOTE]
> The `5 GiB` / `2 GiB` free-space reserves mentioned in earlier documentation
> were not backend defaults — they are only active if you set the variables
> above. Choosing an explicit reserve (for example `5368709120` = 5 GiB) is
> recommended for network storage.

---

## Downloads & Matching

| Variable | Default | Description |
| :--- | :--- | :--- |
| `MUSICDL_CONCURRENT_DOWNLOADS` | `2` | Maximum simultaneous `yt-dlp` download processes (valid range 1–16). Dynamic. |
| `MUSICDL_MAX_ATTEMPTS` | `5` | Maximum attempts for a job item across protected and unprotected retries. |
| `YTDM_MAX_RETRIES` | `2` | Ordinary retry budget for transient errors (range 0–10). |
| `YTDM_RETRY_BACKOFF` | `15s` | Base backoff between ordinary retries. |
| `YTDM_TRACK_TIMEOUT` | `30m` | Hard timeout for a single track's acquisition. |
| `YTDM_MATCH_MIN_SCORE` | `70` | Minimum match confidence (0–100) for a candidate to be accepted. Dynamic. |
| `YTDM_MATCH_CANDIDATE_LIMIT` | `10` | Maximum candidates evaluated per track (1–50). |
| `YTDM_MATCH_DURATION_TOLERANCE_MS` | `4000` | Allowed duration difference between metadata and audio candidate. |

### Combined-stream audio fallback (v0.27.3+)

Off by default, for every existing and every new installation. It is meant to
be switched on deliberately for a controlled comparison first — see
[Audio format classification](/diagnostics/audio-format-classification).

| Variable | Default | Description |
| :--- | :--- | :--- |
| `YTDM_COMBINED_AUDIO_FALLBACK` | `false` | Allow acquiring a track from a combined audio/video stream when the item offers no audio-only stream. The audio is copied out without re-encoding; the video never reaches the library. Requires a restart. |
| `YTDM_COMBINED_FALLBACK_MAX_BYTES` | `134217728` (128 MiB) | Hard limit on what one combined transfer may move. Enforced twice: `yt-dlp` refuses an announced size above it before the transfer, and the arrived file is measured afterwards. |
| `YTDM_COMBINED_FALLBACK_TIMEOUT` | `10m` | Hard limit on how long one combined transfer may run, because it occupies the media session for its whole duration. |

What the switch does **not** change: an audio-only stream always wins when one
exists; matching, verification and duration tolerance are untouched; nothing is
re-encoded; preview, DRM-protected and insufficiently described formats stay
rejected; and the absence of an audio-only stream never puts a provider family
on hold.

---

### Library output

| Variable | Default | Description |
| :--- | :--- | :--- |
| `MUSICDL_LIBRARY_WRITE_COVER_FILE` | `true` | Write an external `cover.jpg` next to each release. |
| `MUSICDL_LIBRARY_EMBED_COVER` | `true` | Embed album artwork into the audio file. |
| `MUSICDL_LIBRARY_LYRICS_ENABLED` | `true` | Resolve lyrics during download. Dynamic. |
| `MUSICDL_LIBRARY_LYRICS_WRITE_SIDECAR` | `true` | Write `.lrc` / `.txt` sidecar files. Dynamic. |

---

## Tools

| Variable | Default | Description |
| :--- | :--- | :--- |
| `MUSICDL_YTDLP` | `yt-dlp` | Path to the `yt-dlp` binary. |
| `MUSICDL_FFMPEG` | `ffmpeg` | Path to the `ffmpeg` binary. |
| `MUSICDL_FFPROBE` | `ffprobe` | Path to the `ffprobe` binary. |
| `MUSICDL_COOKIE_FILE` / `YTDM_COOKIEFILE` | *(empty)* | Optional Netscape `cookies.txt` for legacy external YouTube auth (e.g. `/data/cookies.txt`). Managed Media Sessions are the preferred mechanism — see [Providers](/features/providers). |
| `MUSICDL_YTDLP_PLAYER_CLIENTS` | *(yt-dlp default)* | Comma-separated `yt-dlp` player-client list. |
| `YTDM_TOOL_TIMEOUT` | `5m` | Timeout for a single tool invocation. |

---

## Metadata & Media Providers

| Variable | Default | Description |
| :--- | :--- | :--- |
| `YTDM_DEFAULT_METADATA_PROVIDER` | `deezer` | Preferred metadata provider. |
| `YTDM_DEFAULT_MEDIA_PROVIDER` | `ytmusic` | Preferred media acquisition provider. |
| `YTDM_SPOTIFY_CLIENT_ID` | *(empty)* | Spotify Developer Client ID. Spotify auto-disables itself if either credential is missing. |
| `YTDM_SPOTIFY_CLIENT_SECRET` | *(empty)* | Spotify Developer Client Secret. |
| `YTDM_SPOTIFY_MARKET` | `DE` | Spotify catalogue market. |
| `MUSICDL_DEEZER_REQUESTS_PER_SECOND` | `8` | Deezer sustained request ceiling (Deezer's own limit is ~10/s). |
| `MUSICDL_DEEZER_BURST` | `5` | Deezer token-bucket burst. |
| `MUSICDL_DEEZER_MAX_RETRIES` | `3` | Deezer retry attempts. |
| `MUSICDL_DEEZER_RETRY_BACKOFF` | `500ms` | Deezer initial retry backoff. |
| `MUSICDL_DEEZER_MAX_RETRY_BACKOFF` | `8s` | Deezer maximum retry backoff. |
| `MUSICDL_YTMUSIC_REQUESTS_PER_SECOND` | `1.0` | YouTube Music metadata request ceiling. |
| `MUSICDL_YTMUSIC_BURST` | `1` | YouTube Music burst. |
| `MUSICDL_YOUTUBE_REQUESTS_PER_SECOND` | *(provider default)* | YouTube family request ceiling. |
| `MUSICDL_YOUTUBE_BURST` | *(provider default)* | YouTube family burst. |
| `MUSICDL_SOUNDCLOUD_ENABLED` | `true` | Enable SoundCloud as an independent media provider. |
| `MUSICDL_SOUNDCLOUD_REQUESTS_PER_SECOND` | `1.0` | SoundCloud request ceiling. |
| `MUSICDL_SOUNDCLOUD_BURST` | `3` | SoundCloud burst. |

### Lyrics — Genius fallback

| Variable | Default | Description |
| :--- | :--- | :--- |
| `MUSICDL_GENIUS_ENABLED` | `false` | Enable the optional Genius plain-text lyrics fallback. Dynamic (Settings). |
| `MUSICDL_GENIUS_ACCESS_TOKEN` | *(empty)* | Genius API client token. If set, the authenticated Genius Search API is used; otherwise a best-effort public search is attempted. |

> [!NOTE]
> The canonical names are `MUSICDL_GENIUS_ENABLED` / `MUSICDL_GENIUS_ACCESS_TOKEN`
> (plus `YTDM_*` aliases and a bare `GENIUS_ACCESS_TOKEN`). Older notes that used
> `MUSICDL_PROVIDERS_GENIUS_*` were incorrect and are ignored by the parser.

---

## Managed Media Sessions

| Variable | Default | Description |
| :--- | :--- | :--- |
| `MUSICDL_COOKIE_DIR` / `YTDM_COOKIE_DIR` | `./data/cookies` | Directory where uploaded session cookie files are stored (writable by UID 10001). |
| `MUSICDL_SESSION_MAX_LEASES` | `1` | Concurrent `yt-dlp` processes allowed per managed session. |
| `MUSICDL_SESSION_REQUESTS_PER_SECOND` | `0.5` | Per-session process-start rate. |
| `MUSICDL_SESSION_BURST` | `1` | Per-session burst. |
| `MUSICDL_GLOBAL_REQUESTS_PER_SECOND` | `2.0` | Family-wide process-start ceiling. |
| `MUSICDL_GLOBAL_BURST` | `4` | Family-wide burst. |

---

## Subscriptions

| Variable | Default | Description |
| :--- | :--- | :--- |
| `MUSICDL_SUBSCRIPTIONS_ENABLED` | `true` | Master switch for periodic subscription sync (endpoints and manual "check now" stay available when `false`). |
| `MUSICDL_SUBSCRIPTION_SYNC_INTERVAL` | `24h` | Target interval between discography checks per artist. |
| `MUSICDL_SUBSCRIPTION_CHECK_INTERVAL` | `15m` | How finely the scheduler observes the sync interval. |
| `MUSICDL_SUBSCRIPTION_RETRY_INTERVAL` | `1h` | Delay before retrying a failed sync. |
| `MUSICDL_SUBSCRIPTION_SYNC_TIMEOUT` | `30m` | Execution budget for one artist's catalogue walk. |
| `MUSICDL_SUBSCRIPTION_BATCH_SIZE` | `25` | Releases evaluated per batch. |

---

## Update Checks (v0.15+)

| Variable | Default | Description |
| :--- | :--- | :--- |
| `MUSICDL_UPDATE_CHECKS_ENABLED` | `true` | Enable periodic background checks for new stable GitHub releases. |
| `MUSICDL_UPDATE_REPOSITORY` | `Der-Felix/ytmdl` | `owner/repo` queried for release metadata. |
| `MUSICDL_UPDATE_CHECK_INTERVAL` | `1h` | Cache TTL / minimum interval between checks (floored at `5m`). |

> [!NOTE]
> `MUSICDL_UPDATE_CHECKS_ENABLED=false` disables all outbound HTTP requests to
> the GitHub Releases API. No telemetry is sent in either case.

---

## Logging

| Variable | Default | Description |
| :--- | :--- | :--- |
| `YTDM_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, or `error`. |
| `YTDM_LOG_FORMAT` | `json` | `json` (structured, default) or `text` (human-readable). |

The backend never logs credentials or the database password; the connection URL
is always redacted before it reaches a log line.
