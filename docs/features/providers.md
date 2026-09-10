# Providers, Matching & Media Sessions

YTMDL resolves artist discographies, album tracklists, and canonical ISRC/catalog data through structured metadata providers, then acquires audio through separate media providers.

## Supported Metadata Providers

- **Spotify:** Fast artist searches, high-resolution cover artwork, and rich discography listings. Requires a free Spotify Developer Client ID & Secret.
- **Deezer:** Comprehensive international catalog with ISRC codes, barcode identifiers, and album artwork. Does not require API keys.
- **YouTube Music:** Track metadata and audio candidate search.

## Media Acquisition Providers

YTMDL retrieves audio streams through media providers coordinated by the `ProviderOrchestrator`:

- **YouTube Music (`ytmusic`)**: Primary media acquisition provider searching official track releases.
- **YouTube (`youtube`)**: Secondary video catalog fallback within the YouTube platform family.
- **SoundCloud (`soundcloud`)**: Optional independent media acquisition provider. No SoundCloud user account or login credentials are required for supported public media.

### Provider Ordering & Fallback

The default acquisition chain evaluates providers in registration order:
`ytmusic` → `youtube` → `soundcloud`

1. **Candidate/Content Fallback**: If a track cannot be located or resolved on the YouTube family (`TrackNotFound` / candidate exhaustion), acquisition falls back cleanly to SoundCloud.
2. **Protection Failure Isolation**: Systemic or platform-level protection failures (such as `SESSION_BOT_CHALLENGE` or rate limits) immediately halt same-attempt fanout. The item transitions to `retry_wait` rather than hopping providers to bypass platform controls.
3. **Platform Family Isolation**: SoundCloud operates under its own platform family (`soundcloud`), isolated from the YouTube family (`youtube`). Quota limits, family cooldowns, and session health on one family never affect or poison the other. YouTube credentials and session cookies are never sent to SoundCloud.

## Conservative Track Matching

When matching metadata against audio streams:
- Track durations must match within tight bounds (default tolerance `4000 ms`, configurable via `YTDM_MATCH_DURATION_TOLERANCE_MS`).
- A minimum match score (`YTDM_MATCH_MIN_SCORE`, default `70`) must be reached; up to `YTDM_MATCH_CANDIDATE_LIMIT` candidates (default `10`) are evaluated.
- Official artist channels and official audio releases are strictly prioritized over user uploads.
- Explicit checks prevent live performances, acoustic covers, or remixes from substituting standard studio album cuts.

## Native Audio Formats

`yt-dlp` prefers a native Opus stream and remuxes it stream-copy into an `.opus`
(Ogg) container — no re-encoding, no generational loss. When only another format
is available (for example AAC in an M4A container), the file is stored in that
native format; the backend does **not** silently transcode
(`YTDM_ALLOW_TRANSCODE` is `false` by default). `ffprobe` verifies every file's
audio stream before it is promoted to the library. Cover art is embedded into
the file (`MUSICDL_LIBRARY_EMBED_COVER`) and, by default, also written as an
external `cover.jpg` (`MUSICDL_LIBRARY_WRITE_COVER_FILE`).

## Managed Media Sessions

Media acquisition from the YouTube family can use **authenticated sessions**.
Configure them as an administrator under **Server Settings → Media Sources**.

### Uploading cookies

1. Create a named session entry.
2. Export a Netscape-format `cookies.txt` for `youtube.com` **from a browser you
   are logged into** and upload it to the session. YTMDL validates the candidate
   file in isolation and only promotes it if a health probe succeeds, so a bad
   file never overwrites a working one.
3. Uploaded cookie files are stored under the cookie directory
   (`MUSICDL_COOKIE_DIR`, default `./data/cookies`) and must be writable by the
   container user (UID/GID `10001`). Include `./data` in your backups.

Legacy single-file configuration (`YTDM_COOKIEFILE` / `MUSICDL_COOKIE_FILE`)
continues to work and coexists with managed sessions in the runtime pool.

> [!IMPORTANT]
> YTMDL does not bypass provider protection. It only uses credentials you supply,
> paces its own requests, and backs off when a provider signals a limit.

### Health states & cooldowns

On-demand probing and real download outcomes classify a session:

| State | UI label | Meaning |
| :--- | :--- | :--- |
| `healthy` | Bereit | Usable now. |
| `rate_limited` | Rate-Limit | Temporary provider rate limit; progressive backoff cooldown. |
| `bot_challenge` | Bot-Prüfung erforderlich | Provider requested verification. Bounded cooldown: **24 h** for a first challenge, **72 h** for genuinely repeated challenges. |
| `auth_failed` | Anmeldung erforderlich | Cookies are expired/invalid — upload a fresh `cookies.txt`. |
| `cooldown` | — | Generic protective cooldown in effect (`cooldown_until`). |
| `unknown` | — | Not yet probed since creation or replacement. |

### Recovery behaviour

- A **temporary** cooldown parks affected job items in *Wartet auf Provider*
  (`retry_wait` + `SESSION_UNAVAILABLE`) — no retry budget is consumed and the
  scheduler does not busy-loop.
- A **successful real media download** clears active protection for that session
  and immediately wakes eligible waiting jobs.
- Replacing a session's cookies **preserves** its health and cooldown history, so
  a recovering provider backlog is not suddenly flushed.
- A metadata-only probe never clears a media cooldown and never certifies full
  download health.
- If no usable session exists at all, non-retriable items fail terminally rather
  than looping forever.
