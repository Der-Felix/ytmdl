# Tips & Best Practices

Practical advice distilled from real deployments. Nothing here is required, but
each item prevents a class of problem.

## Deployment

- **Pin `YTMDL_VERSION`.** Use an explicit version (e.g. `0.26.0`), not `latest`.
  Deterministic images make `ytmdlctl update` and rollback meaningful.
- **Keep `.env` out of version control** and back it up somewhere safe — it holds
  your database password and, with it, your only path to the catalogue.
- **Put host-specific tweaks in `compose.ghcr.override.yaml`**, not in the
  tracked `compose.ghcr.yaml`. It is git-ignored and survives updates. See
  [Deployment → Local customisations](/deployment#lokale-anpassungen-mit-compose-ghcr-override-yaml).
- **Serve over HTTPS** behind your own reverse proxy and set
  `MUSICDL_COOKIE_SECURE=true`. Add your proxy's IP/CIDR to
  `MUSICDL_TRUSTED_PROXIES` so client IPs in logs and rate limiting are correct.
- **Don't publish the database or backend port.** Only the frontend needs a host
  port. The bundled compose files already follow this.

## Storage

- **Always configure the Storage Identity Guard** for network storage. Without
  `MUSICDL_STORAGE_GUARD_ID`, an unmounted share means writes land on the host
  root disk.
- **Set an explicit free-space reserve** on network volumes, e.g.
  `MUSICDL_LIBRARY_MIN_FREE_BYTES=5368709120` (5 GiB). The backend default is `0`
  (disabled).
- **SMB uses `soft`, NFS uses `hard`.** For CIFS, `soft` lets YTMDL's state
  machine react to an outage; for NFS, `soft` risks silent `EIO` corruption — use
  `hard`.
- **Use `:rslave` propagation** (SMB guide) so a host remount is visible in the
  running container without a restart.
- **Mount the Postgres volume at `/var/lib/postgresql`**, not the older
  `/var/lib/postgresql/data` — the `postgres:18-alpine` image expects the parent.

## Backups

- **Take a backup before every update** — `ytmdlctl update` does this
  automatically, but run `ytmdlctl backup` before manual schema changes too.
- **Back up three things separately:** the database dump (catalogue), `./music`
  (audio), and `./data` (cookie files) plus `.env`. A database dump alone cannot
  rebuild your library.
- **Test a restore occasionally** into a throwaway database name — a backup you
  have never restored is a hypothesis.
- `podman compose down` keeps your data; only `down -v` deletes the Postgres
  volume.

## Providers & Sessions

- **Add more than one Managed Media Session.** Load and cooldowns spread across
  sessions; a single session in a 72 h `bot_challenge` cooldown otherwise stalls
  YouTube-family downloads.
- **Refresh cookies when a session shows *Anmeldung erforderlich*.** Export a
  fresh `cookies.txt` from a logged-in browser; the upload is validated before it
  replaces the working file.
- **Configure Deezer, keep Spotify optional.** Deezer needs no API key and is the
  default metadata provider; Spotify auto-disables itself if its credentials are
  missing.
- **Leave provider rate limits at their defaults** unless you have a specific
  reason. They are set below each provider's real ceiling on purpose.

## Library & Matching

- **Let subscriptions do the routine work** — they run at Low priority so your
  manual downloads always jump ahead.
- **Run a Quick audit after a large import**, then a Deep audit occasionally to
  catch tag drift and truncated files.
- **Preview every repair.** Repair preview shows the exact file operations before
  anything moves; orphans are quarantined into `.ytmdl-trash`, never deleted.
- **Tune matching conservatively.** If real tracks are being missed, nudge
  `YTDM_MATCH_MIN_SCORE` down or `YTDM_MATCH_DURATION_TOLERANCE_MS` up in small
  steps and re-check for wrong matches.

## Player

- **Use a Chromium-based or Firefox browser** for the widest codec coverage and
  full Web Audio (EQ, crossfade, visualizer).
- **On iOS, expect basic playback only.** Start audio with a tap; Web Audio
  effects may not behave as on desktop.
- **Playback queue ≠ playlist.** The queue is a throwaway session list. To keep a
  collection, save a Playlist.

## Media server integration

- The default layout (`Artist/YYYY - Album/NN - Title.opus` + `cover.jpg` +
  `.lrc`) is designed for Jellyfin, Plex, Navidrome, and Emby — point them at the
  same `/music` path (read-only is fine).
- Legacy files from very old YTMDL versions are left untouched; they are not
  corruption. Use the reconciliation scan to review them.
