# Glossary

Terms used across the YTMDL interface, logs, and documentation.

## Architecture

**Backend (`ytmdl-backend`)**
: The Go service: REST API, SSE stream, job queue, workers, and the `yt-dlp` /
`ffmpeg` / `ffprobe` tooling. Runs as UID/GID `10001`.

**Frontend (`ytmdl-frontend`)**
: Nginx serving the compiled React single-page app and reverse-proxying `/api/*`
to the backend. The only container that publishes a host port (`8080`).

**`ytmdl-db`**
: PostgreSQL 18 container. The entire catalogue lives here; there is no local
database file.

**`ytmdl-net`**
: The dedicated bridge network (`172.31.250.0/28`). Containers address each other
by service name (`db`, `backend`, `frontend`), never by IP.

**`ytmdlctl`**
: The host-side lifecycle CLI: `status`, `check`, `update`, `backup`, `rollback`,
`recover`, `reconcile-artists`, `merge-artists`, `manifest-gen`.

## Storage

**Staging (`/data/staging`)**
: Local scratch directory where a download is assembled and verified before it is
moved into the library. Persistent across restarts so interrupted work resumes.

**Two-phase atomic promote**
: Phase 1 stages and verifies every track of a release locally; phase 2 atomically
moves the finished files into `/music/Artist/YYYY - Album/`.

**Storage Identity Guard**
: A check that the marker file `.ytmdl-storage-id` at the library root matches
`MUSICDL_STORAGE_GUARD_ID`. Prevents writes to the wrong (or unmounted) volume.

**Guard status** — `verified`, `missing`, `mismatch`, `disabled`.

**Storage health** — `healthy`, `degraded`, `read_only`, `low_space`,
`guard_missing`, `guard_mismatch`, `unavailable`, `unknown`.

**`PATH_CONFLICT`**
: A different, unregistered file already exists at a track's target path. YTMDL
refuses to overwrite it.

## Jobs & Queue

**Job**
: A unit of work created from a request — an artist discography, a release, or a
single track. Contains many **job items**.

**Job item**
: One track within a job, with its own status and attempt history.

**Priority** — `low` (subscriptions/background), `normal`, `high` (manual
requests), `very_high`.

**Job status** — `queued`, `resolving_*`, `matching`, `downloading`, `tagging`,
`finalizing`, `retry_wait`, `waiting_for_storage`, `waiting_for_space`,
`completed`, `failed`, `cancelled`.

**Paused**
: A per-job flag (separate from status) that holds a job in place without losing
its position; shown on the **Pausiert** tab.

**Global / storage queue pause**
: Admin control (**Settings → Storage**) that stops *all* processing, e.g. for
maintenance. Also triggered automatically by the Storage Guard.

**Wartet auf Provider**
: UI label for `retry_wait` + error code `SESSION_UNAVAILABLE` — a temporary
provider/session cooldown, not a failure. No retry budget is spent.

## Providers & Media

**Metadata provider** — Deezer (default, no key), Spotify (needs a client
ID/secret), YouTube Music. Supplies discographies, tracklists, ISRCs, artwork.

**Media provider / acquisition** — `ytmusic` → `youtube` → `soundcloud`. Supplies
the actual audio stream.

**Platform family**
: A group of providers that share health and cooldown state — `youtube`
(ytmusic + youtube) and `soundcloud`. Isolated from each other.

**Managed Media Session**
: A named, cookie-authenticated YouTube session configured under
**Settings → Media Sources**.

**Session health** — `healthy` (*Bereit*), `rate_limited` (*Rate-Limit*),
`bot_challenge` (*Bot-Prüfung erforderlich*), `auth_failed`
(*Anmeldung erforderlich*), `cooldown`, `unknown`.

**Bot challenge cooldown** — bounded: 24 h for a first challenge, 72 h for
genuinely repeated challenges.

**Match score**
: Confidence (0–100) that an audio candidate matches the metadata track. Minimum
is `YTDM_MATCH_MIN_SCORE` (default 70).

## Library

**Canonical artist identity (Schema 9, v0.17+)**
: `artists.id` is a stable UUIDv4. External provider IDs are recorded separately
in `artist_sources` so one artist can link to Spotify, Deezer, and YouTube Music
at once.

**Audit** — a non-destructive library scan. **Quick** checks structure, covers,
lyric sidecars; **Deep** also verifies tags, embedded art, and audio streams via
`ffprobe`.

**Finding**
: A single issue produced by an audit (missing file, orphan file, tag drift…).

**Repair preview / apply**
: Admin-only. Preview shows the exact changes; apply executes them (e.g.
quarantine orphans into `.ytmdl-trash`, reconcile paths).

**Reconciliation scan (`/library/scan`)**
: Compares physical files on disk with the `files` table and surfaces
legacy/orphan discrepancies. Legacy files are never auto-deleted.

## Player

**Playback queue**
: The temporary, in-memory list for the current listening session. Not a
playlist; not persisted; not synced between devices.

**Playlist**
: A permanent, database-backed, per-user collection with contiguous `1..N`
ordering.

**MiniPlayer**
: The compact persistent player bar shown while browsing.

**Graphic EQ**
: 10 fixed bands: 31, 62, 125, 250, 500, 1000, 2000, 4000, 8000, 16000 Hz.

**Parametric EQ**
: Up to 10 user-defined biquad filters (frequency, gain, Q).

**Crossfade**
: Overlapping fade between consecutive tracks (0–12 s), using a second audio deck.

## Versioning

**Stable release**
<<<<<<< HEAD
: A tagged version (`v0.26.0`) with published GHCR images. `ytmdlctl` tracks
these.

**`dev` branch**
: The unreleased development snapshot on public GitHub. No prebuilt images — build
from source.

**Schema version**
: The database schema revision. Stable v0.26.0 = schema **12**.
=======
: A tagged version (`v0.27.0`) with published GHCR images. `ytmdlctl` tracks
these.

**`dev` branch**
: The development snapshot on public GitHub. Stable releases have prebuilt images;
build from source for unreleased changes.

**Schema version**
: The database schema revision. Stable v0.27.0 = schema **12**.
>>>>>>> public/main

**`schema_neutral`**
: A release classification meaning an automatic rollback is safe because the
schema did not change.

**`RECOVERY_REQUIRED`**
: A safe halt state after an update failed post-migration. Resolve with
`ytmdlctl recover status` → `recover resume` or `recover restore`.
