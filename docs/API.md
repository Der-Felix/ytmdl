# YTMDL REST API Reference

All API endpoints are served under `/api/v1`. Successful responses return JSON payloads wrapped in `{"data": ...}`. Error responses return JSON error envelopes formatted as `{"error": {"code": "...", "message": "...", "request_id": "..."}}`.

State-changing requests (`POST`, `PUT`, `PATCH`, `DELETE`) require a valid CSRF token passed via the `X-CSRF-Token` header and the `ytmdl_csrf` cookie.

---

## Authentication & Sessions

| Method | Endpoint | Access | Purpose |
| :--- | :--- | :--- | :--- |
| `GET` | `/api/v1/auth/status` | Public | Check if the system has completed first-run setup. |
| `POST` | `/api/v1/auth/setup` | Public | Create the initial administrator account on first run. |
| `POST` | `/api/v1/auth/login` | Public | Authenticate user credentials and issue session cookie. |
| `GET` | `/api/v1/auth/me` | User | Get current authenticated user details and role. |
| `POST` | `/api/v1/auth/logout` | User | Terminate current session and invalidate cookie. |

---

## User & Profile Management

| Method | Endpoint | Access | Purpose |
| :--- | :--- | :--- | :--- |
| `GET` | `/api/v1/profile` | User | Retrieve current user profile. |
| `PATCH` | `/api/v1/profile` | User | Update username or display preferences. |
| `POST` | `/api/v1/profile/password` | User | Change current user password. |
| `GET` | `/api/v1/profile/sessions` | User | List all active sessions for current user. |
| `DELETE` | `/api/v1/profile/sessions/{id}` | User | Revoke a specific active session. |
| `POST` | `/api/v1/profile/sessions/revoke-others` | User | Revoke all sessions except the current one. |
| `GET` | `/api/v1/users` | Admin | List all registered user accounts. |
| `POST` | `/api/v1/users` | Admin | Create a new user account. |
| `GET` | `/api/v1/users/{id}` | Admin | Get specific user account details. |
| `PATCH` | `/api/v1/users/{id}` | Admin | Update user details or role. |
| `POST` | `/api/v1/users/{id}/reset-password` | Admin | Reset a user's password. |
| `DELETE` | `/api/v1/users/{id}` | Admin | Delete a user account (last admin protected). |

---

## Discovery & Downloads

| Method | Endpoint | Access | Purpose |
| :--- | :--- | :--- | :--- |
| `GET` | `/api/v1/providers` | User | List configured metadata and media providers and health. |
| `GET` | `/api/v1/search/artists?q=` | User | Search artists across configured providers. |
| `GET` | `/api/v1/resolve?url=` | User | Resolve external URL (YouTube/Spotify/Deezer) to entity. |
| `GET` | `/api/v1/artists/{id}` | User | Retrieve artist metadata. |
| `GET` | `/api/v1/artists/{id}/discography` | User | Fetch artist discography, filterable by release type. |
| `GET` | `/api/v1/releases/{id}` | User | Retrieve release details and track list. |
| `POST` | `/api/v1/downloads/artist` | User | Enqueue download job for an artist discography. |
| `POST` | `/api/v1/downloads/release` | User | Enqueue download job for a release. |
| `POST` | `/api/v1/downloads/track` | User | Enqueue download job for a single track. |

---

## Subscriptions

| Method | Endpoint | Access | Purpose |
| :--- | :--- | :--- | :--- |
| `GET` | `/api/v1/subscriptions` | User | List all artist subscriptions and latest sync status. |
| `POST` | `/api/v1/subscriptions` | User | Subscribe to an artist. |
| `GET` | `/api/v1/subscriptions/{id}` | User | Get subscription details and sync report. |
| `PATCH` | `/api/v1/subscriptions/{id}` | User | Update subscription filters, auto-download, or priority. |
| `DELETE` | `/api/v1/subscriptions/{id}` | User | Remove artist subscription. |
| `POST` | `/api/v1/subscriptions/{id}/sync` | User | Trigger immediate discography check (returns `202 Accepted`). |
| `GET` | `/api/v1/subscriptions/export` | User | Export subscriptions as JSON. |
| `POST` | `/api/v1/subscriptions/import/preview` | User | Preview subscription import file. |
| `POST` | `/api/v1/subscriptions/import/apply` | User | Apply subscription import. |

---

## Queue & Job Execution

| Method | Endpoint | Access | Purpose |
| :--- | :--- | :--- | :--- |
| `GET` | `/api/v1/jobs` | User | List jobs with status, priority, and progress. |
| `GET` | `/api/v1/jobs/{id}` | User | Inspect job items, attempts, and error details. |
| `PATCH` | `/api/v1/jobs/{id}` | User | Update job priority. |
| `POST` | `/api/v1/jobs/{id}/pause` | User | Pause a specific job. |
| `POST` | `/api/v1/jobs/{id}/resume` | User | Resume a paused job. |
| `POST` | `/api/v1/jobs/{id}/retry-failed` | User | Retry all failed items in a job. |
| `POST` | `/api/v1/jobs/{job_id}/items/{item_id}/retry` | User | Retry a specific failed job item. |
| `DELETE` | `/api/v1/jobs/{id}` | User | Cancel an active or pending job. |
| `DELETE` | `/api/v1/jobs/history` | Admin | Clear completed and cancelled job history. |
| `GET` | `/api/v1/events` | User | Server-Sent Events (SSE) stream for real-time progress. |

---

## Library & Streaming

| Method | Endpoint | Access | Purpose |
| :--- | :--- | :--- | :--- |
| `GET` | `/api/v1/library/stats` | User | Library storage, codec, and lyrics statistics. |
| `GET` | `/api/v1/library/search` | User | Search local catalog across artists, releases, and tracks. |
| `GET` | `/api/v1/library/artists` | User | Paginated local artists list. |
| `GET` | `/api/v1/library/artists/{id}` | User | Local artist detail with releases and size. |
| `GET` | `/api/v1/library/releases` | User | Paginated local releases list. |
| `GET` | `/api/v1/library/releases/{id}` | User | Local release detail with track listing and multi-disc grouping. |
| `GET` | `/api/v1/library/tracks` | User | Paginated local tracks list. |
| `GET` | `/api/v1/library/tracks/{id}` | User | Technical audio inspector (bitrate, sample rate, channels, codec). |
| `GET`/`HEAD` | `/api/v1/library/tracks/{id}/stream` | User | Stream track audio (supports HTTP 206 Partial Content range requests). |
| `GET`/`HEAD` | `/api/v1/library/files/{id}/stream` | User | Stream audio file by file ID. |
| `GET` | `/api/v1/library/tracks/{id}/lyrics` | User | Retrieve synchronized or plain lyrics for a track. |
| `POST` | `/api/v1/library/tracks/{id}/lyrics/refresh` | User | Re-query lyrics provider chain (LRCLIB -> YTM -> Genius). |
| `DELETE` | `/api/v1/library/tracks/{id}/lyrics` | User | Delete lyrics and remove sidecar file. |
| `GET` | `/api/v1/library/lyrics/backfill/preview` | User | Read-only scan of missing vs. eligible lyrics tracks. |
| `GET` | `/api/v1/library/lyrics/backfill` | User | Get status of running lyrics backfill job. |
| `POST` | `/api/v1/library/lyrics/backfill` | User | Start background lyrics backfill for missing tracks. |
| `POST` | `/api/v1/library/tracks/{id}/redownload` | User | Re-enqueue download for a missing track file. |
| `POST` | `/api/v1/library/tracks/{id}/retag` | User | Losslessly rewrite Vorbis comment tags from database. |
| `GET` | `/api/v1/library/compatibility` | User | Media server compatibility report. |
| `POST` | `/api/v1/library/reorganize` | User | Reorganize files into canonical directory layout. |

---

## Library Maintenance & Audits (Admin Only)

| Method | Endpoint | Access | Purpose |
| :--- | :--- | :--- | :--- |
| `GET` | `/api/v1/library/audits` | User | List historical library audit runs. |
| `GET` | `/api/v1/library/audits/current` | User | Get currently running audit status. |
| `GET` | `/api/v1/library/audits/{id}` | User | Get audit summary and finding counts. |
| `GET` | `/api/v1/library/audits/{id}/findings` | User | Paginated list of audit findings (missing files, orphan files, etc.). |
| `POST` | `/api/v1/library/audits` | Admin | Start new Quick or Deep library audit. |
| `POST` | `/api/v1/library/audits/{id}/cancel` | Admin | Cancel in-progress library audit. |
| `POST` | `/api/v1/library/repairs/preview` | Admin | Preview non-destructive audit repair actions. |
| `POST` | `/api/v1/library/repairs/apply` | Admin | Apply confirmed repair actions (quarantine orphans, reconcile paths). |
| `POST` | `/api/v1/library/releases/{id}/artwork/preview` | Admin | Preview artwork refresh for a release. |
| `POST` | `/api/v1/library/releases/{id}/artwork/refresh` | Admin | Refresh cover artwork for a release. |
| `POST` | `/api/v1/library/artwork/preview` | Admin | Preview bulk artwork repair for missing covers. |
| `POST` | `/api/v1/library/artwork/refresh` | Admin | Execute bulk artwork repair. |
| `DELETE` | `/api/v1/library/releases/{id}` | Admin | Delete release and tracks (preserves untracked files). |
| `DELETE` | `/api/v1/library/tracks/{id}` | Admin | Delete single track from library and database. |

---

## System & Settings

| Method | Endpoint | Access | Purpose |
| :--- | :--- | :--- | :--- |
| `GET` | `/api/v1/health` | Public | System health check (application, database, ffmpeg, yt-dlp). |
| `GET` | `/api/v1/settings` | User | Get effective server settings and provider statuses. |
| `PUT` | `/api/v1/settings` | Admin | Update dynamic server settings (workers, lyrics toggles, match score). |
| `GET` | `/api/v1/storage/status` | Admin | Inspect storage volume capacity, mount health, and identity guard. |
| `POST` | `/api/v1/storage/probe` | Admin | Re-probe storage accessibility and guard token. |
| `POST` | `/api/v1/storage/queue/pause` | Admin | Manually pause queue processing for storage maintenance. |
| `POST` | `/api/v1/storage/queue/resume` | Admin | Resume queue processing after storage maintenance. |
