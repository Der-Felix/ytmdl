# Frequently Asked Questions

## General

### What is YTMDL, and how is it different from a yt-dlp web wrapper?

A typical yt-dlp web UI takes a URL and hands you a file. YTMDL is a **library
manager**: it resolves a complete artist discography from structured metadata
providers (Deezer, Spotify, YouTube Music), matches each track to an audio
candidate with conservative scoring, downloads native Opus without re-encoding,
writes standardised tags and cover art, resolves synchronised lyrics, files
everything into a Jellyfin/Plex/Navidrome/Emby-compatible folder tree, tracks
everything in PostgreSQL, and keeps it up to date through artist subscriptions.
It also includes a multi-user web player. The download step is one part of a
pipeline, not the product.

### Is YTMDL affiliated with YouTube, Spotify, or Deezer?

No. It is an independent open-source project (Apache-2.0). Provider names and
trademarks belong to their owners. See [Legal](/legal).

### Does YTMDL send telemetry?

No. The only outbound "phone home" is the optional GitHub release check, which is
an anonymous `GET` to the public Releases API and can be disabled entirely with
`MUSICDL_UPDATE_CHECKS_ENABLED=false`.

## Installation & Platforms

### Which platforms are supported?

Application containers (`ghcr.io/der-felix/ytmdl-backend` / `-frontend`) are
published for **`linux/amd64`** and **`linux/arm64`**. The `ytmdlctl` host CLI is
additionally built for `darwin/amd64` and `darwin/arm64`. **Windows is not
supported.** On macOS, `ytmdlctl` drives a Podman Machine or Docker daemon.

### Do I need to build from source?

No — for stable releases use the official images with `compose.ghcr.yaml`. You
only build from source to run the unreleased `dev` branch: `git clone -b dev`,
then `docker compose -f compose.yaml up -d --build`. See
[Development](/development).

### Podman or Docker?

Both work. With Podman, use a **Compose v2** provider (Docker Compose plugin or
`podman-compose-subcommand`). The legacy Python `podman-compose` (1.3.x and
older) does not support the Compose v2 volume flags and `keep-id` user-namespace
mapping YTMDL relies on; `ytmdlctl` detects it in preflight and refuses to
proceed.

## Configuration & Storage

### Where do settings live?

In environment variables, supplied through `.env`. See the
[Configuration Reference](/configuration). A handful of settings (worker
concurrency, lyrics toggles, minimum match score, Genius) can also be changed at
runtime in **Settings** without a restart.

### `MUSICDL_*` or `YTDM_*`?

Prefer `MUSICDL_*` — it is the supported contract. `YTDM_*` names still work as
aliases; if both are set, `MUSICDL_*` wins.

### Can I store my library on a NAS?

Yes, by mounting the SMB/CIFS or NFS share **on the host** and bind-mounting the
host path into the container as `/music`. YTMDL never mounts network filesystems
itself. Protect against a disconnected share with the Storage Identity Guard.
See [Storage](/storage/), [SMB](/storage/smb), [NFS](/storage/nfs).

### What is the Storage Identity Guard?

A marker file (`.ytmdl-storage-id`) at the library root whose expected value you
put in `MUSICDL_STORAGE_GUARD_ID`. If the marker is missing or wrong (share
unmounted, wrong volume), YTMDL pauses the queue and rejects writes instead of
silently filling the host disk.

## Downloads & Library

### Why is a download "Wartet auf Provider"?

The media provider or an authenticated session is in a temporary cooldown
(rate limit or verification challenge). This is **not** a failure: the item waits
without spending its retry budget and resumes automatically when the provider
recovers. See [Providers](/features/providers).

### Why did a track fail to match?

Conservative matching rejects candidates outside the duration tolerance
(`4000 ms` default) or below the minimum score (`70`), and filters out live
versions, covers, and remixes. Lower `YTDM_MATCH_MIN_SCORE` or raise
`YTDM_MATCH_DURATION_TOLERANCE_MS` cautiously if legitimate tracks are missed.

### Manual downloads run before my subscription sync — why?

Manual requests default to **High** priority; subscription sync jobs are **Low**.
You can change any job's priority on the Downloads page. See
[Download Automation](/features/downloads).

### Does YTMDL re-encode audio?

No. It prefers a native Opus stream and remuxes stream-copy. Other native
formats (AAC/M4A, etc.) are stored as-is. `YTDM_ALLOW_TRANSCODE` is `false` by
default.

### How are files named?

`Artist/YYYY - Album/NN - Title.opus`, with `101 -`, `201 -` numbering for
multi-disc releases, embedded Vorbis comments, `cover.jpg`, and `.lrc`/`.txt`
lyric sidecars. See [Library](/features/library).

## Updates, Backup & Rollback

### How do I update?

Raise `YTMDL_VERSION` in `.env` and run `ytmdlctl update` (preview first with
`ytmdlctl update --dry-run`). It takes a verified database backup, checks image
digests against the release manifest, and can roll back. See [Updates](/updates).

### What does a backup contain — and not contain?

`ytmdlctl backup` (or `pg_dump -Fc`) captures the **PostgreSQL catalogue only**:
users, artists, albums, tracks, jobs, playlists, favorites, audit history. It
does **not** contain your audio files (`./music`) or `./data` (cookie files).
Back those up separately.

### Can I always roll back?

Only when the target release is `schema_neutral` and the schema has not drifted.
Across a schema migration, automatic rollback is intentionally blocked; you
restore the verified pre-migration backup with `ytmdlctl recover restore`. The
pre-update backup always remains in `backups/`.

### Is there a database migration in the current stable release?

<<<<<<< HEAD
Stable **v0.26.0** keeps schema **12**; no migration. The last migration was
=======
Stable **v0.27.0** keeps schema **12**; no migration. The last migration was
>>>>>>> public/main
schema 11 → 12 in v0.23.0 (playlists/favorites).

## Player

### The player won't play a file in Safari.

Codec support is the browser's. Opus-in-Ogg is not universally supported in
Safari. Try the same track in Chromium/Firefox to confirm. YTMDL does not
transcode on the fly. See [Player → Known Browser Limitations](/features/player).

### Is there a mobile app or a TV app?

No. There is no installable PWA, service worker, or native app of any kind. The
browser UI is the only client.

## Security

### How are passwords stored?

Argon2id. Sessions are random server-side tokens in PostgreSQL; mutating
endpoints require a CSRF token (double-submit cookie). Two roles exist:
Administrator and User. See [Security](/security).

### How do I report a vulnerability?

Privately, via GitHub Private Vulnerability Reporting — not a public issue. See
[SECURITY.md](https://github.com/Der-Felix/ytmdl/blob/main/SECURITY.md).
