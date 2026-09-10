# Download Automation & Queue

The YTMDL download engine coordinates background worker pools to process individual tracks, complete albums, and full discographies without starving shorter jobs.

## Fair Queue Scheduling

- **Fair Division:** Jobs with hundreds of tracks are processed in balanced round-robin chunks, ensuring a single massive discography cannot monopolize the queue.
- **Opus Stream Extraction:** `yt-dlp` extracts native Opus streams (`audio/opus` inside WebM containers) and remuxes them stream-copy into `.opus` (Ogg) containers without generational audio transcoding.
- **Verification:** Every downloaded file is probed via `ffprobe` to verify audio stream validity, channels, and sample rates before promotion to the library.
- **Retry & Circuit Breaker:** Transient network or rate-limiting errors trigger exponential backoff.

## Job Priorities

Every job carries a priority that decides its order relative to other jobs. Manual
requests are placed ahead of background automation by default.

| Priority | Label (UI) | Assigned to |
| :--- | :--- | :--- |
| `low` | Niedrig | Artist subscription synchronization and background discovery jobs |
| `normal` | Normal | General default |
| `high` | Hoch | Manually requested downloads (artist, release, or track) |
| `very_high` | Sehr hoch | Reserved for expedited items |

- **Change a priority:** open a job on the **Downloads** page and pick a new value
  from the *Priorität* selector, or `PATCH /api/v1/jobs/{id}` with `{"priority": "..."}`.
- Raising a subscription job to `high` lets it compete with manual downloads;
  lowering a manual job to `low` parks it behind everything else.

## Pausing the Queue

### Global pause (storage maintenance)

Administrators can stop **all** queue processing from **Settings → Storage**
(*Warteschlange anhalten* / *fortsetzen*), or via
`POST /api/v1/storage/queue/pause` and `.../resume`. While paused the panel shows
*Download-Warteschlange: Pausiert*. In-flight items finish their current step and
no new work starts. YTMDL also pauses the queue on its own when the Storage
Identity Guard fails or the library runs low on space.

### Per-job pause

Individual jobs can be paused and resumed without affecting the rest of the queue
(`POST /api/v1/jobs/{id}/pause` and `.../resume`). Paused jobs are listed under
the **Pausiert** tab on the Downloads page. A paused job keeps its place and
priority; deduplication still treats its items as pending so a subscription run
will not create a duplicate job for the same release.

## Understanding Status Labels

Job and item badges use plain-language German labels. The most common ones:

| Internal status | Job label | Meaning |
| :--- | :--- | :--- |
| `queued` | In Warteschlange | Accepted, waiting for a worker |
| `resolving_artist` / `resolving_releases` / `resolving_tracks` | Künstler/Releases/Tracks werden aufgelöst | Building the track list from metadata providers |
| `matching` | Quellen werden gesucht | Selecting an audio candidate for each track |
| `downloading` | Wird heruntergeladen | `yt-dlp` is fetching audio |
| `tagging` | Tags werden geschrieben | Writing Vorbis comments and cover art |
| `finalizing` | Wird finalisiert | Atomic promote from staging into `/music` |
| `retry_wait` | Wartet auf Wiederholung | Transient error; will retry after backoff |
| `retry_wait` + `SESSION_UNAVAILABLE` | **Wartet auf Provider** | Deferred by a temporary provider or session cooldown — **not** a failure and no retry budget is spent |
| `waiting_for_storage` | Wartet auf Speicher | Library mount is offline; staged audio is safe |
| `waiting_for_space` | Wartet auf Speicherplatz | Free space below the configured reserve |
| `completed` | Abgeschlossen | Finished — individual track failures do not fail the whole job |
| `failed` | Fehlgeschlagen | The job itself could not proceed |
| `cancelled` | Abgebrochen | Cancelled by a user |

> [!NOTE]
> A job that finishes with some failed tracks still reports **Abgeschlossen**.
> Use *Fehlgeschlagene wiederholen* on the job, or retry a single item, to
> re-attempt just those tracks.

The Downloads page tab badges (`Aktiv`, `In Warteschlange`, `Pausiert`,
`Abgeschlossen`, `Fehlgeschlagen`) reflect global database totals, not just the
jobs visible on the current page.
