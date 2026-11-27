# YTMDL REST API Reference

All API endpoints are served under `/api/v1`. Successful responses return JSON payloads wrapped in `{"data": ...}`. Error responses return JSON error envelopes formatted as `{"error": {"code": "...", "message": "...", "request_id": "..."}}`.

State-changing requests (`POST`, `PUT`, `PATCH`, `DELETE`) require a valid CSRF token passed via the `X-CSRF-Token` header and the `ytmdl_csrf` cookie.

A signed-out client gets its `ytmdl_csrf` cookie only from `GET /api/v1/auth/status`; fetch it before the first state-changing request (sign-in, setup, device start). No other endpoint issues a token to a signed-out client, so parallel first requests cannot hand a client competing tokens. Sign-in (`/auth/login`, `/auth/setup`, an authorized `/auth/device/poll`) sets the cookie to a token bound to the new session, and any later response for a session that has no cookie sets that same token. An existing cookie is otherwise never replaced.

---

## Authentication & Sessions

| Method | Endpoint | Access | Purpose |
| :--- | :--- | :--- | :--- |
| `GET` | `/api/v1/auth/status` | Public | Check if the system has completed first-run setup. |
| `POST` | `/api/v1/auth/setup` | Public | Create the initial administrator account on first run. |
| `POST` | `/api/v1/auth/login` | Public | Authenticate user credentials and issue session cookie. |
| `GET` | `/api/v1/auth/me` | User | Get current authenticated user details and role. |
| `POST` | `/api/v1/auth/logout` | User | Terminate current session and invalidate cookie. |
| `POST` | `/api/v1/auth/device` | Public | Request a short-lived device sign-in code. |
| `POST` | `/api/v1/auth/device/poll` | Public | Poll the opaque secret; exchange an approved code for a session cookie. |
| `POST` | `/api/v1/auth/device/preview` | User | Inspect the requesting device without approving it. |
| `POST` | `/api/v1/auth/device/confirm` | User | Explicitly approve a device using the short code. |

Device routes also require CSRF. See [device-code sign-in](features/device-sign-in.md)
for expiry, polling intervals, confirmation and revocation.

Native Apple clients reuse cookie authentication and CSRF. Device pairing adds
`POST /api/v1/auth/device` (start), `POST /api/v1/auth/device/poll` (exchange),
and authenticated `POST /api/v1/auth/device/preview` and
`POST /api/v1/auth/device/confirm`. Every POST requires the normal CSRF cookie
and header, including pre-auth start/poll. Codes expire after five minutes;
poll no faster than the returned interval and back off on `slow_down`.
See the [Apple client security design](https://github.com/Der-Felix/ytmdl/blob/dev/apple/SECURITY-DESIGN.md)
for the one-time exchange, limits and account permissions.


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
| `PATCH` | `/api/v1/subscriptions/{id}` | User | Update subscription filters, auto-download, priority, or `include_appearances` (`true`/`false`, or `null` to follow the server-wide setting). |
| `DELETE` | `/api/v1/subscriptions/{id}` | User | Remove artist subscription. |
| `POST` | `/api/v1/subscriptions/{id}/sync` | User | Trigger immediate discography check (returns `202 Accepted`). |
| `GET` | `/api/v1/subscriptions/export` | User | Export subscriptions as JSON. |
| `POST` | `/api/v1/subscriptions/import/preview` | User | Preview subscription import file. |
| `POST` | `/api/v1/subscriptions/import/apply` | User | Apply subscription import. |

---

## Queue & Job Execution

| Method | Endpoint | Access | Purpose |
| :--- | :--- | :--- | :--- |
| `GET` | `/api/v1/jobs` | User | List jobs with status, priority, and progress. Supports `view=all\|active\|queued\|paused\|done\|failed` together with `status`, `type`, `priority`, `limit` and `offset`. Group filters apply before pagination; `meta.total` counts matching jobs. Paused overlaps queued/active; done includes completed/cancelled. Unknown groups return `INVALID_REQUEST`. |
| `GET` | `/api/v1/jobs/summary` | User | Global queue counters (active, queued, paused, done, failed) used by the Downloads tab badges, the estimate, and `provider_pauses` (provider, `reason_code`, `phase` `paused`/`probe_ready`/`probing`, German `message`, `resumes_at`, `level`) plus `blocked_failed_items`. During a provider pause `eta_confidence` is `provider_paused` and `eta_text` names it ("YouTube pausiert bis 14:32"). |
| `POST` | `/api/v1/jobs/provider-pause/probe` | Admin | End a running provider pause now and let a single probe item through. Returns `pause_ended` and the new summary. Session cooldowns are left to the session pool. |
| `POST` | `/api/v1/jobs/retry-blocked` | Admin | Requeue, across all jobs, every failed item whose error is a provider block (`SESSION_BOT_CHALLENGE`, `SESSION_AUTH_FAILED`, `SESSION_RATE_LIMITED`, `SESSION_UNAVAILABLE`, `PROVIDER_RATE_LIMITED`) with fresh attempts. Optional body `{"include_zero_score_matches": true}` also requeues `MATCH_FAILED` items whose best score was 0.0. Returns `items` and `jobs`. |
| `GET` | `/api/v1/jobs/{id}` | User | Inspect job items, attempts, and error details. |
| `PATCH` | `/api/v1/jobs/{id}` | User | Update job priority and/or paused state (`{"priority": "...", "paused": true}`). |
| `POST` | `/api/v1/jobs/{id}/pause` | User | Pause a specific job. |
| `POST` | `/api/v1/jobs/{id}/resume` | User | Resume a paused job. |
| `POST` | `/api/v1/jobs/{id}/retry-failed` | User | Retry all failed items in a job. |
| `POST` | `/api/v1/jobs/{job_id}/items/{item_id}/retry` | User | Retry a specific failed job item. |
| `DELETE` | `/api/v1/jobs/{id}` | User | Cancel an active or pending job. |
| `DELETE` | `/api/v1/jobs/history` | Admin | Clear completed and cancelled job history. |
| `GET` | `/api/v1/events` | User | Server-Sent Events (SSE) stream for real-time progress. |

---

## Playlists & Favorites

Playlists and favorites are strictly scoped to the authenticated user.

| Method | Endpoint | Access | Purpose |
| :--- | :--- | :--- | :--- |
| `GET` | `/api/v1/playlists` | User | List the current user's playlists. |
| `GET` | `/api/v1/playlists/{id}` | User | Get one playlist with its ordered tracks. |
| `POST` | `/api/v1/playlists` | User | Create a playlist. |
| `PATCH` | `/api/v1/playlists/{id}` | User | Rename a playlist. |
| `DELETE` | `/api/v1/playlists/{id}` | User | Delete a playlist. |
| `POST` | `/api/v1/playlists/{id}/tracks` | User | Add a track to a playlist. |
| `DELETE` | `/api/v1/playlists/{id}/tracks/{track_id}` | User | Remove a track (positions re-compact to `1..N`). |
| `PUT` | `/api/v1/playlists/{id}/tracks/reorder` | User | Reorder playlist tracks. |
| `GET` | `/api/v1/favorites` | User | List favorited tracks. |
| `GET` | `/api/v1/favorites/ids` | User | List favorited track IDs only. |
| `GET` | `/api/v1/favorites/{track_id}` | User | Check whether a track is favorited. |
| `PUT` | `/api/v1/favorites/{track_id}` | User | Mark a track as favorite. |
| `DELETE` | `/api/v1/favorites/{track_id}` | User | Remove a favorite. |
| `GET` | `/api/v1/history` | User | The current user's listening history (`sort=recent` or `frequent`). |
| `POST` | `/api/v1/history` | User | Record a play: `{track_id, event_id}`; idempotent per `event_id` (16–100 characters). |
| `POST` | `/api/v1/history/skips` | User | Record a skip (moved on before the track counted as a play): `{track_id, event_id}`; idempotent per `event_id`. |
| `DELETE` | `/api/v1/history` | User | Clear the current user's plays and skips. |
| `GET` | `/api/v1/history/stats` | User | Listening statistics for `period=week`, `month`, `year` or `all`: totals, top tracks, artists, releases and genres, activity per day or month. See [Listening API](features/listening.md). |
| `GET` | `/api/v1/library/mixes` | User | The current user's personal mixes (`id`, `kind`, `title`, `description`, `group`, `genre`, `artist_id`, `decade`, `daypart`, `track_count`, `cover_track_ids`, `cover_release_ids`); stable for a calendar day; optional `tz`. Kinds and rules: [Listening API](features/listening.md). |
| `GET` | `/api/v1/library/mixes/{id}` | User | One mix with up to 50 `tracks`; `404 PLAYLIST_NOT_FOUND` when the mix is not offered today. |
| `GET` | `/api/v1/library/radio` | User | Local radio as a track list: `seed` (track), `artist`, `release` or `genre`, `nonce`, `limit` (1–500), `exclude`. |
| `GET` | `/api/v1/library/radio/station` | User | A titled radio station in batches (`limit` 1–100) with a no-repeat `cursor`. |
| `GET` | `/api/v1/listening-preferences` | User | The user's exclusions for mixes and radio (artist, genre, track). |
| `PUT` | `/api/v1/listening-preferences` | User | Add an exclusion: `{kind, target}`; idempotent. |
| `DELETE` | `/api/v1/listening-preferences/{id}` | User | Remove an exclusion; `404 PREFERENCE_NOT_FOUND` for an unknown or foreign id. |
| `DELETE` | `/api/v1/listening-preferences?kind=&target=` | User | Remove the exclusion of that target, if any. |

---

## Library & Streaming

| Method | Endpoint | Access | Purpose |
| :--- | :--- | :--- | :--- |
| `GET` | `/api/v1/library/stats` | User | Library storage, codec, and lyrics statistics. |
| `GET` | `/api/v1/library/search` | User | Search local catalog across artists, releases, and tracks, plus a `lyrics` group of tracks found by their stored lyrics with a snippet (`lyrics=false` skips it). |
| `GET` | `/api/v1/library/artists` | User | Paginated local artists list. |
| `GET` | `/api/v1/library/artists/{id}` | User | Local artist detail with size and the releases that have at least one track in the library. |
| `GET` | `/api/v1/library/releases` | User | Paginated local releases list. Filters: `q`, `artist_id`, `release_type`, `year`, `genre`, `genre_missing=true`, and `in_library=true` for only releases with at least one track in the library (`track_count_in_library > 0`); without it, releases known only from a download attempt or a subscription are listed too. |
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
| `GET` | `/api/v1/library/scan` | User | Read-only reconciliation scan: physical files vs. database (`files` table), including legacy/orphan issues. |
| `POST` | `/api/v1/library/scan` | User | Start a background reconciliation scan. |
| `DELETE` | `/api/v1/library/scan/issues/{id}` | **Admin** | Resolve a specific orphan/legacy scan issue (may quarantine the unreferenced file). |

---

## Library Maintenance & Audits

Audit *reads* are available to any authenticated user; starting audits, cancelling
them, and every repair or delete action require an Administrator.

| Method | Endpoint | Access | Purpose |
| :--- | :--- | :--- | :--- |
| `GET` | `/api/v1/library/audits` | User | List historical library audit runs. |
| `GET` | `/api/v1/library/audits/current` | User | Get currently running audit status. |
| `GET` | `/api/v1/library/audits/{id}` | User | Get audit summary and finding counts. |
| `GET` | `/api/v1/library/audits/{id}/findings` | User | Paginated list of audit findings (missing files, orphan files, etc.). |
| `GET` | `/api/v1/library/naming-migration` | User | State of the automatic move to the current [file naming scheme](/features/library#moving-an-existing-library-to-the-current-names): counts, and every file left in place with the reason. |
| `GET` | `/api/v1/library/identity-migration` | User | State of the background carry-over of stored track identity keys to the current identity scheme: counts, and every track held at its older key with the reason (`collision`: another track already carries the new key; `not_derivable`: the stored key no longer follows from the stored title and artists). A held track is still found as before and is looked at again on every start. |
| `GET` | `/api/v1/library/complete-releases` | User | State of the backfill of incomplete releases: `state` (`idle`, `scanning`, `scanned`, `failed`, `cancelled`), progress, `setting_enabled` (the `complete_releases` setting) and the last scan result: releases a backfill would add copies to, how many copies and how many bytes they need, recordings the library does not hold (`unknown_tracks`), and releases whose lookup failed by error code. Kept in memory only; after a restart the state is `idle`. |
| `POST` | `/api/v1/library/complete-releases/scan` | Admin | Start the read-only background scan (one metadata lookup per stored release with a provider identity). `409 ALREADY_EXISTS` while one runs. |
| `DELETE` | `/api/v1/library/complete-releases/scan` | Admin | Cancel the running scan. |
| `POST` | `/api/v1/library/complete-releases/apply` | Admin | Queue one copies-only release job per release the last finished scan found incomplete. Copies-only jobs never download; they copy recordings the library already holds, whatever `complete_releases` says. Refused without a finished scan, while one runs, after the scan was applied, or when it is older than 24 hours. |
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
| `GET` | `/api/v1/health` | Public | System health check (application, database, ffmpeg, yt-dlp); `features` lists optional capabilities such as `radio.station` or `history.stats` for clients to check. |
| `GET` | `/api/v1/settings` | User | Get effective server settings and provider statuses. |
| `PUT` | `/api/v1/settings` | Admin | Update dynamic server settings (workers, lyrics toggles, match score, subscription defaults, `subscription_include_appearances`, `complete_releases`: place copies of recordings the library already holds on further releases of new downloads). |
| `GET` | `/api/v1/storage/status` | Admin | Inspect storage volume capacity, mount health, and identity guard. |
| `POST` | `/api/v1/storage/probe` | Admin | Re-probe storage accessibility and guard token. |
| `POST` | `/api/v1/storage/queue/pause` | Admin | Manually pause queue processing for storage maintenance. |
| `POST` | `/api/v1/storage/queue/resume` | Admin | Resume queue processing after storage maintenance. |
| `GET` | `/api/v1/system/update` | Admin | Get current or cached GitHub release update status. |
| `POST` | `/api/v1/system/update/check` | Admin | Force a fresh update check against the official release repository (CSRF required). |

---

## Managed Media Sessions (Admin Only)

Manage authenticated YouTube sessions used for media acquisition. The same
handlers are mounted under both `/api/v1/media-sessions` and
`/api/v1/admin/media-sessions`. See [Providers → Managed Media Sessions](/features/providers).

| Method | Endpoint | Access | Purpose |
| :--- | :--- | :--- | :--- |
| `GET` | `/api/v1/media-sessions` | Admin | List managed sessions and their health status. |
| `GET` | `/api/v1/media-sessions/{id}` | Admin | Get one session's detail and cooldown state. |
| `POST` | `/api/v1/media-sessions` | Admin | Create a named session entry. |
| `POST` | `/api/v1/media-sessions/{id}/cookies` | Admin | Upload / replace the session's Netscape `cookies.txt` (validated before the previous file is overwritten). |
| `POST` | `/api/v1/media-sessions/{id}/probe` | Admin | Run an on-demand health probe for the session. |
| `PATCH` | `/api/v1/media-sessions/{id}` | Admin | Rename or reconfigure a session. |
| `DELETE` | `/api/v1/media-sessions/{id}` | Admin | Remove a session. |

### Update Status Schema

Endpoints under `/api/v1/system/update` require Administrator privileges. `POST /api/v1/system/update/check` requires a valid `X-CSRF-Token` header.

**Response Structure (`{"data": ...}`):**
```json
{
  "current_version": "0.25.2",
  "latest_version": "0.26.0",
  "state": "update_available",
  "release_name": "YTMDL v0.26.0",
  "published_at": "2026-09-08T12:00:00Z",
  "release_url": "https://github.com/Der-Felix/ytmdl/releases/tag/v0.26.0",
  "release_notes": "...",
  "checked_at": "2026-09-08T14:30:00Z",
  "cached": true
}
```

**Possible `state` Values:**
- `up_to_date`: The installed version is identical to or newer than the latest stable release.
- `update_available`: A newer stable release is available on GitHub.
- `no_public_release`: The repository has no published releases yet (HTTP 404 handled gracefully).
- `disabled`: Update checks are disabled (`MUSICDL_UPDATE_CHECKS_ENABLED=false`).
- `unavailable`: GitHub is unreachable, timed out, or rate-limited (403/429).
- `invalid_release`: The remote release tag cannot be parsed as valid SemVer, or is a draft/prerelease.
- `development_version`: The installed version is a local dev build (`"dev"`).
