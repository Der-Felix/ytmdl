# Library Management & Integrity Audits

YTMDL organizes music into a media-server-ready folder hierarchy with strict metadata standards.

![YTMDL Library](/screenshots/library.webp)

## Directory Layout

```text
/music/
├── Pink Floyd/
│   ├── 1973 - The Dark Side of the Moon/
│   │   ├── 01 - Speak to Me.opus
│   │   ├── 01 - Speak to Me.lrc
│   │   ├── ...
│   │   └── cover.jpg
```

- Multi-disc releases use hierarchical numbering (`101 - Track.opus`, `201 - Track.opus`).
- Standard Vorbis comments are embedded directly into each file (TITLE, ARTIST, ALBUM, ALBUMARTIST, DATE, TRACKNUMBER, DISCNUMBER).

## Non-Destructive Auditing Engine

- **Quick Audit:** Validates folder structures, missing cover art, and missing lyrics sidecars.
- **Deep Audit:** Verifies Vorbis tag consistency, embedded art dimensions, and audio stream integrity via `ffprobe`.
- **Repair Preview:** Generates an atomic diff preview before renaming files or rewriting tag blocks. Destructive actions require Administrator privileges.

## Canonical Artist Identity (Schema 9)

Starting with **v0.17**, YTMDL decouples internal artist identity from external provider IDs:
- **Canonical UUID Identity:** `artists.id` is a stable UUIDv4 serving as the single source of truth across your library.
- **Multi-Provider Sources (`artist_sources`):** External IDs from Spotify, YouTube Music, and Deezer are recorded in `artist_sources (artist_id, provider, provider_artist_id)` with timestamps and provenance metadata. A single artist can be linked to multiple providers simultaneously without data loss or collision.
- **Lossless Ingestion:** Downloads and subscription synchronizations preserve the upstream provider source and record provenance on each track and artist entity.

### Host Maintenance Tools

YTMDL provides administrative CLI commands in `ytmdlctl` for managing artist entities directly on the host:

#### Reconcile Artists
Audits and reconciles artist metadata across providers:
```sh
# Dry-run preview
ytmdlctl reconcile-artists --dry-run

# Execute reconciliation
ytmdlctl reconcile-artists
```

#### Merge Artists
Explicitly consolidates duplicate artist entries into a single canonical artist:
```sh
# Merge source artist into target artist
ytmdlctl merge-artists --target <TARGET_ARTIST_UUID> --source <SOURCE_ARTIST_UUID>
```
All albums, tracks, and provider sources from the source artist are re-linked to the target, and the source artist is safely pruned.

## Local Library Search & Multi-Facet Filters

Starting with **v0.24**, YTMDL provides fast, live search and multi-facet filtering directly within your indexed local library.

> [!IMPORTANT]
> **Local Library vs. Acquisition Discovery:**
> Local Library search operates exclusively on already-indexed tracks, releases, and artists in your local database. It does **not** query external providers (YouTube, YouTube Music, SoundCloud, Deezer), does not discover external metadata, and does not schedule download jobs. To search external services and download new media, use the **Downloads** or **Subscriptions** views.

### Search Capabilities & Semantics

- **Instant Flyout Search:** Typing in the global search field displays a debounced flyout grouping top matches into **Künstler** (Artists), **Alben & Releases** (Releases), and **Titel** (Tracks).
- **Playback from Search:** Selecting a track in the flyout queues the visible results and immediately begins playback.
- **Deterministic Ranking:** Free-text queries match artist names, release titles, and track titles using exact, prefix, and substring scoring with deterministic secondary tie-breakers (`created_at DESC, id DESC`).
- **Literal Matching & Escaping:** Free-text matching is case-insensitive, Unicode-safe, and literally escapes wildcards (`%`, `_`, `\`).

### Multi-Facet Track Filtering

When viewing the **Tracks** tab in the Library, you can combine multiple filters:
- **Free-Text (`q`):** Free-form search term.
- **Canonical Artist (`artistId`):** Filter by canonical artist entity.
- **Canonical Release (`releaseId`):** Filter by release entity.
- **Release Year (`year`):** Filter by specific four-digit release year.
- **Favorites (`favorite`):** Filter exclusively to the authenticated user's favorited tracks.

### URL & Navigation State

Search queries, filter chips, view tabs, and sort parameters are automatically reflected in browser URL query parameters (`/library?view=tracks&q=...&sort=title&order=asc`). Browser Back and Forward buttons accurately restore search state without interrupting persistent audio playback in the MiniPlayer.
