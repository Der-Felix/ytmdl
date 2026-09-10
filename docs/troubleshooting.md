# Troubleshooting

Operational problems and how to diagnose them. For storage-specific issues see
[Storage Troubleshooting](/storage/troubleshooting); for update/rollback issues
see [Updates → Troubleshooting](/updates#troubleshooting).

Each entry follows **Symptom → Cause → Diagnose → Fix → Verify**.

---

## Startup & Containers

### Backend exits immediately with a database error

- **Symptom:** `ytmdl-backend` restarts in a loop; logs mention
  `MUSICDL_DATABASE_URL` or `MUSICDL_DATABASE is no longer supported`.
- **Cause:** Missing/incorrect `MUSICDL_DATABASE_URL`, or a removed SQLite
  variable (`MUSICDL_DATABASE`, `YTDM_DATABASE_PATH`) is still set.
- **Diagnose:** `podman compose -f compose.ghcr.yaml logs backend | head`.
  Check that the password in `MUSICDL_DATABASE_URL` matches `POSTGRES_PASSWORD`.
- **Fix:** Set a valid `postgres://ytmdl:<pw>@db:5432/ytmdl?sslmode=disable` and
  remove any SQLite variables from `.env`.
- **Verify:** `curl -fsS 'http://127.0.0.1:8080/api/v1/health?scope=essential'`
  returns HTTP 200.

### Frontend returns `502 Bad Gateway` after recreating the backend

- **Symptom:** UI unreachable through the frontend after
  `up -d --force-recreate backend`.
- **Cause:** Historically, an nginx literal upstream cached the old backend IP.
- **Status:** **Fixed.** The frontend resolves `backend` per request via a
  templated `resolver`. If you still see this, you are on an old frontend image —
  update, or `podman compose -f compose.ghcr.yaml restart frontend`.
- **Verify:** `podman exec ytmdl-frontend nslookup backend` resolves, and the UI
  loads.

### `preflight engine compatibility check failed: detected python 'podman-compose'`

- **Cause:** Podman is delegating `podman compose` to the legacy Python
  `podman-compose` (1.3.x or older), which lacks Compose v2 features YTMDL needs.
- **Fix:** Install a Compose v2 provider (Docker Compose plugin). Confirm with
  `podman compose version` reporting Docker Compose v2.
- **Verify:** `ytmdlctl status` completes without the preflight error.

### `ambiguous compose files found` / `ambiguous engine selection`

- **Cause:** More than one `compose.*.yaml` is present and `ytmdlctl` will not
  guess for a mutating command.
- **Fix:** `ytmdlctl status --file compose.ghcr.yaml --engine podman --save`.
- **Verify:** Subsequent `ytmdlctl` commands no longer prompt for a file.

---

## Downloads & Jobs

### A job sits at "Wartet auf Provider"

- **Symptom:** Job/item badge reads *Wartet auf Provider*; nothing seems to
  progress.
- **Cause:** The media provider or an authenticated session is in a temporary
  cooldown (`SESSION_UNAVAILABLE`) — a rate limit or a verification challenge.
- **Diagnose:** **Settings → Media Sources** shows the session state and
  `cooldown_until`. `bot_challenge` cooldowns are 24 h (first) or 72 h
  (repeated).
- **Fix:** Usually none — it resumes automatically. If the state is
  *Anmeldung erforderlich* (`auth_failed`), upload a fresh `cookies.txt`. Add
  another managed session to spread load.
- **Verify:** After the cooldown, the item moves to `downloading` on its own; a
  successful download clears the session's protection immediately.

### Downloads never start, everything stays "In Warteschlange"

- **Cause:** The queue is globally paused (storage maintenance, or the Storage
  Guard tripped), or `MUSICDL_CONCURRENT_DOWNLOADS` workers are all blocked.
- **Diagnose:** **Settings → Storage** — is *Download-Warteschlange* showing
  *Pausiert*? Check `GET /api/v1/storage/status`.
- **Fix:** Resolve the storage condition, then *Warteschlange fortsetzen*.
- **Verify:** The paused banner clears and jobs advance.

### Job shows "Abgeschlossen" but some tracks are missing

- **Symptom:** Job status is *Abgeschlossen* with a non-zero failure count.
- **Cause:** Individual track failures do not fail the whole job by design.
- **Fix:** Open the job → *Fehlgeschlagene wiederholen*, or retry a single item.
  Check each failed item's error (not found, match score too low, provider
  cooldown).
- **Verify:** Re-run leaves 0 failed items, or the remaining ones have a clear
  terminal reason (e.g. track genuinely unavailable).

### Historical: jobs stuck forever in `downloading`, infinite retry loops

- **Status:** **Fixed** across v0.20.1–v0.26.0. Protected states
  (`BOT_CHALLENGE`, `RATE_LIMITED`, `SESSION_UNAVAILABLE`) now hold items in
  `retry_wait` without consuming retry budget; genuinely unusable configurations
  fail terminally instead of looping; crash recovery resets in-flight items to
  `pending`/`queued` on start. If you see a permanent `downloading` item, restart
  the backend once and report it.

### Tab badge counts look wrong

- **Status:** **Fixed** in v0.25.1 (badges reflect global DB totals, not the
  visible page) and refined in v0.27 (open-job badge counts active + queued and
  hides at zero).

---

## Subscriptions

### A subscription created a duplicate job

- **Status:** **Fixed** in v0.25.2 — deduplication now also considers paused
  non-terminal jobs (`HasNonTerminalJob`) and enqueue is serialised. Ensure both
  containers are current.

### Subscription sync times out on a huge discography

- **Cause:** The catalogue walk exceeded `MUSICDL_SUBSCRIPTION_SYNC_TIMEOUT`
  (default `30m`).
- **Fix:** Raise the timeout, or reduce `MUSICDL_SUBSCRIPTION_BATCH_SIZE`
  pressure. Admission is bounded to one concurrent walk on purpose.
- **Verify:** The next scheduled sync completes and the subscription's last sync
  report is green.

---

## Player

### No sound, or "format not supported"

- **Cause:** The browser cannot decode that container/codec (commonly Ogg/Opus in
  Safari).
- **Diagnose:** Open the same track in Chromium or Firefox.
- **Fix:** Use a supported browser for that file. YTMDL does not transcode on the
  fly. See [Player → Known Browser Limitations](/features/player).

### EQ / crossfade / visualizer do nothing on iPhone

- **Cause:** iOS Safari requires a user gesture to start audio and has Web Audio
  routing limitations.
- **Fix:** Tap play directly; expect reduced Web Audio behaviour on iOS. Basic
  playback still works.

### Playback stops when I navigate

- **Status:** **Fixed** in v0.25 / refined in v0.27 — the player context and
  audio state persist across in-app navigation and browser Back/Forward. If it
  still drops, you are on an old frontend image.

---

## Health & Diagnostics quick reference

```sh
# Container-level (never contacts external services)
curl -fsS 'http://127.0.0.1:8080/api/v1/health?scope=essential'

# Full local diagnostics (adds yt-dlp / ffmpeg / ffprobe checks)
curl -fsS 'http://127.0.0.1:8080/api/v1/health'

# Storage (admin session required)
curl -fsS 'http://127.0.0.1:8080/api/v1/storage/status'

# Logs
podman compose -f compose.ghcr.yaml logs -f backend
```

A missing tool makes `/api/v1/health` report `degraded` but still HTTP 200. Only
an unreachable database returns HTTP 503.
