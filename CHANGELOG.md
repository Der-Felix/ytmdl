# Changelog

Das Format folgt lose [Keep a Changelog](https://keepachangelog.com/de/1.1.0/).

## 0.19.3 — 2026-09-06

### Improvements

- Simplified the desktop application shell by removing the unused global status/header bar.
- Kept a compact navigation header for mobile layouts.

### Bug Fixes

- Browser-native reload shortcuts such as Cmd+R, Cmd+Shift+R and Ctrl+R are no longer intercepted by player keyboard controls.

### Changes

- Removed the global Live connection badge from the AppShell.
- **Database Schema:** Schema remains at Schema 10; no database migration is required.

## 0.19.2 — 2026-09-06

### Improvements

- Media source resolution now evaluates ranked fallback candidates when the best metadata match cannot be streamed.
- Temporary YouTube throttling uses retry/backoff behavior instead of immediately failing a track where applicable.

### Bug Fixes

- Fixed tracks being permanently marked failed when only the highest-ranked YouTube candidate was unavailable while valid alternatives existed.
- Improved distinction between unavailable media and temporary provider throttling/authentication failures.

### Changes

- Candidate resolution is bounded and deterministic.
- Systemic provider errors stop candidate fan-out to avoid increasing request pressure.
- **Database Schema:** Schema remains at Schema 10; no database migration is required.

## 0.19.1 — 2026-09-06

### Features

- Added a fourth priority level: **Very High**
- Added Very High priority support for subscriptions
- Added Very High priority filtering and controls in the WebUI

### Improvements

- Queue scheduling is now deterministic and easier to understand
- Priority ordering now follows:
  `Very High → High → Normal → Low`
- Jobs with equal priority continue to use FIFO ordering

### Bug Fixes

- Fixed manually selected High priority losing its practical effect against aged background jobs
- Fixed tagged release qualification incorrectly treating pre-existing release tags as publication mutations

### Changes

- Removed automatic starvation-based priority promotion
- Normal and Low jobs no longer increase their priority based on age
- Very High jobs are selected as soon as a worker becomes available
- Already running downloads are never interrupted

### Updates

- Database schema updated from **9 to 10**
- Priority constraints now support values `0–3`
- Release manifest updated for Schema 10 upgrade paths
- Release consistency qualification hardened for tagged releases
- **Database Schema:** This release includes a database migration from **Schema 9 to Schema 10**. A pre-migration backup is required and handled by the managed updater.

## 0.18.1 — 2026-09-05

### Highlights

- **Safe Release Notes Markdown Rendering in WebUI:**
  - Implemented safe, styled Markdown rendering for GitHub release notes under **Settings → System & Updates**.
  - Supports Markdown headings, paragraphs, bullet lists, bold text, inline code, fenced code blocks, and external links styled to match the YTMDL theme.
  - Safe link handling with strict `rel="noopener noreferrer"` and `target="_blank"`, restricting URLs to safe `http://` / `https://` protocols.
  - Zero raw HTML execution: all markdown parsed safely into React elements without `dangerouslySetInnerHTML`.
  - Expandable/collapsible view ("Mehr anzeigen" / "Weniger anzeigen") for longer release notes to preserve clean mobile and desktop layouts.

### Changes

- **Canonical Release Note Generation & Workflow Validation:**
  - Sourced future release notes automatically from `CHANGELOG.md`, including Highlights, Changes, `ytmdlctl update` instructions, and automated compare links.
  - Automated release qualification gate ensuring future release notes contain full content and full changelog compare link, failing fast if notes collapse to compare-only links.
- **Database Schema:** Schema remains at Schema 9; no database migration required.

## 0.18.0 — 2026-09-05

### Added

- **Live Download Queue ETA & Throughput Metrics:**
  - Track-based real-time ETA calculation derived strictly from measured wall-clock item completions (1-hour window with 6-hour fallback).
  - Four confidence tiers (`none` / "Berechnung läuft …", `low`, `medium`, `high`) ensuring no false precision during startup or idle periods.
  - Dedicated isolation of paused jobs (`paused = true`) as a separate status count excluded from active ETA countdowns.
  - Transparent handling of exponential backoff retry states (`"Wartet auf erneuten Versuch"`).
- **Active Processing & Next-Up Live Previews:**
  - Real-time active worker cards displaying current artist, release, track title, track number, processing phase, and per-track progress.
  - Next-Up candidate preview reflecting exact dispatcher eligibility and priority ordering with starvation protection (`EffectivePriority`).
  - EventSource (SSE) background updates keeping queue statistics and active worker states synchronized in real time.
- **Removed Synthetic Global Queue Progress:**
  - Removed misleading global queue progress percentage to prevent erratic regressions in open-ended subscription import environments.
- **Database Schema:** Schema remains at Schema 9; no database migration required.

## 0.17.4 — 2026-09-05

### Changed

- **App-Shell Consolidation:** Unified application administration pages (`/users` and `/settings/server`) into the standard YTMDL `AppShell`.
  - Persistent standard left sidebar and atmospheric glow background across all administrative views.
  - Standard max content width and layout alignment matching the Dashboard and Library.
  - Floating `MiniPlayer` remains active and visible during user and server management.
  - Server settings (`/settings/server`) feature compact local page tab navigation (`Allgemein`, `Downloads`, `Speicher`, `Provider`) with full URL hash synchronization (`#health`, `#updates`, `#startup`, `#downloads`, `#storage`, `#providers`) and browser back/forward history support.
  - Removed obsolete standalone `AdminLayout` and related styling.
- **Database Schema:** Schema remains at Schema 9; no database migration required.

## 0.17.3 — 2026-09-05

### Added

- **Native Multi-Arch Application Containers:** Built and published native OCI multi-platform container images for both `linux/amd64` (x86_64) and `linux/arm64` (aarch64 / Apple Silicon / Raspberry Pi / ARM servers) for both `backend` and `frontend`.
- **Release Manifest Specification v3 (`upgrade_paths` & `platforms`):**
  - Introduced `manifest_version: 3` with an explicit matrix `upgrade_paths` specification defining per-source-schema update classifications:
    - `SourceSchema: 8` → `TargetSchema: 9`: `schema_forward`, `backup_restore_required` (triggers quiescent pre-migration backup)
    - `SourceSchema: 9` → `TargetSchema: 9`: `schema_neutral`, `schema_neutral` (non-quiescent patch update without disabling automatic rollback)
  - Added platform-specific image digest mappings (`images.backend.platforms` and `images.frontend.platforms`) alongside top-level multi-arch index digests.
- **Enhanced Engine & Staging Multi-Arch Verification:**
  - `ytmdlctl` automatically detects target host platform architecture via engine runtime query (`podman info` / `docker info`).
  - Staging verification validates pulled images against either the multi-arch index digest or the platform-specific manifest digest, fail-closing if the host platform is unsupported.
  - Image inspect verification ensures the pulled container architecture matches the target host architecture.
  - Snapshot engine preserves previous working image identity even across cross-architecture upgrades (e.g. historical v0.15.0 emulated AMD64 on ARM64 hosts).
- **Multi-Arch CI & GitHub Actions Release Pipeline:**
  - Integrated `docker/setup-qemu-action` for multi-platform buildx emulation.
  - Cross-compilation builder pattern (`FROM --platform=$BUILDPLATFORM golang:alpine`) in `Containerfile` with `CGO_ENABLED=0` targeting `$TARGETOS/$TARGETARCH` at native host build speed.
  - Non-publishing `workflow_dispatch` verify-only mode qualifies multi-arch image compilation on `ubuntu-latest`.
- **Database Schema:** Schema remains at Schema 9; no database migration required from v0.17.0 / v0.17.1 / v0.17.2.

## 0.17.2 — 2026-09-05

### Fixed

- **GitHub Release Runner Toolchain Hardening:** Hardened PostgreSQL 18 client tool resolution in GitHub Actions runner (`ubuntu-latest`) by prepending `/usr/lib/postgresql/18/bin` to PATH, ensuring `pg_dump`, `pg_restore`, `psql`, and related tools resolve to PostgreSQL 18 rather than pre-installed runner wrappers.
- **Pre-Publication Verify-Only Gate:** Added non-publishing `workflow_dispatch` qualification mode to the release workflow to verify the complete runner environment, toolchain versions, build pipeline, and real PostgreSQL 18 lifecycle tests before tags are published.
- **Binary Resolver Precedence:** Enhanced `resolvePostgresBinary` in test harnesses to honor explicit `MUSICDL_TEST_PG_BIN_DIR` overrides and prioritize PostgreSQL 18 paths.
- **Schema Compatibility:** Database schema remains at Schema 9; no database migration required from v0.17.0.

## 0.17.1 — 2026-09-05

### Fixed

- **Public Release CI Pipeline:** Configured a dedicated PostgreSQL 18 test service container, PostgreSQL 18 client toolchain, and fail-closed readiness validation in the GitHub Actions release workflow. Ensures all disaster recovery and schema migration lifecycle tests (`TestE2E_H`, `TestE2E_I`, `TestE2E_J`) execute against real PostgreSQL 18 before publication.
- **Test Harness Fail-Closed Semantics:** Standardized `ytmdlctl` integration test helpers to skip only when `MUSICDL_TEST_DATABASE_URL` is intentionally unset, and fail hard on any connection or infrastructure failure when configured.
- **Schema Compatibility:** No database schema changes beyond the v0.17.0 canonical artist architecture; database schema remains at Schema 9.

## 0.17.0 — 2026-09-04

### Added

- **Canonical Artist Identity (Schema 9):** Decoupled internal artist identity from external provider IDs using stable UUIDv4 identifiers in `artists.id`.
- **Multi-Provider Source Preservation:** Introduced `artist_sources` table mapping canonical artists to multiple external providers (Spotify, YouTube Music, Deezer) losslessly with upstream provenance and verification tracking.
- **Artist Management CLI Commands:**
  - `ytmdlctl reconcile-artists`: Reconciles artist metadata across multiple providers with dry-run support.
  - `ytmdlctl merge-artists`: Safely consolidates duplicate artist entries into a canonical entity with complete relationship re-linking.
- **Release Manifest v2:** Extended `release-manifest.json` specification supporting `manifest_version: 2`, `update_classification: schema_forward`, `rollback_classification: backup_restore_required`, and `supported_source_schemas: [8]`.
- **Quiescent Pre-Migration Backups:** Enhanced `ytmdlctl update` to drain active jobs, stop old backend services, and capture a verified PostgreSQL custom archive (`pg_dump -Fc`) before executing database migrations.
- **Disaster Recovery Suite (`ytmdlctl recover`):**
  - `ytmdlctl recover status`: Inspects current state, failure classification, active schema, and pre-migration backup location.
  - `ytmdlctl recover resume`: Re-attempts cutover and health validation without re-executing completed migrations.
  - `ytmdlctl recover restore`: Restores the verified pre-migration backup via isolated temporary database swap (`<db>_restore_tmp`) and safely rolls back to the previous Schema 8 release.

### Security / Safety

- **Core Safety Law Enforcement:** Automatic rollback is strictly prohibited once database schema mutations commence. If a post-migration failure occurs, the system transitions into `RECOVERY_REQUIRED` to guarantee old Schema 8 backends are never started against Schema 9 databases.
- **Isolated Database Restore Swaps:** Database restoration in `ytmdlctl recover restore` performs `pg_restore` into a temporary staging database before atomically swapping to the live database name, preventing partial or corrupted restore states.
- **Deterministic State Tracking v2:** Durable state transitions (`PREPARED` → `MUTATING` → `SUCCESS` / `RECOVERY_REQUIRED`) recorded in `.ytmdl/update-state.json` under exclusive host locks.
- **PostgreSQL 18 Compatibility:** Continuous integration and deployment verified against PostgreSQL 18.

## 0.16.0 — 2026-09-04

### Added

- Official host-side lifecycle and update management CLI: `ytmdlctl`.
- Transactional managed updates (`ytmdlctl update`) with multi-stage verification and automatic rollback.
- Read-only preflight analysis and dry-run mode (`ytmdlctl update --dry-run`) to check schema, storage, and image readiness without mutation.
- Verified database backups (`ytmdlctl backup`) producing PostgreSQL custom-format (`pg_dump -Fc`) archives verified via `pg_restore --list`.
- Schema-neutral rollback orchestrator (`ytmdlctl rollback`) protecting deployments with automatic schema drift safeguards.
- Strict cryptographic image digest verification using immutable SHA256 hashes published in `release-manifest.json`.
- Storage Identity Guard verification and active download queue drainage checks before updating.
- Web UI update instructions with copy-to-clipboard button and direct documentation links in System & Updates.
- Native binary distribution of `ytmdlctl` for `linux/amd64`, `linux/arm64`, `darwin/amd64`, and `darwin/arm64` with `SHA256SUMS`.
- Podman Compose provider compatibility check to detect and safely block incompatible Python `podman-compose` implementations.

### Security / Safety

- Rollback invariant: automatic rollback is only permitted when the database schema is confirmed identical (`schema == schemaBefore`).
- Schema drift protection: any detected schema divergence forces `RECOVERY_REQUIRED` state and preserves the pre-update backup without destructive automated modifications.
- Durable transaction state tracking in `.ytmdl/update-state.json` with exclusive host locking (`.ytmdl/update.lock`).
- Surgical atomic `.env` modifications preserving user comments, spacing, and custom variables.
- Zero container socket exposure to application workloads; `ytmdlctl` operates strictly on the host.
- No database migration required (Schema remains 8).

## 0.15.0 — 2026-09-03

### Added

- Public VitePress documentation foundation with local search, dark mode, and comprehensive feature guides.
- GitHub Pages deployment workflow (`.github/workflows/pages.yml`).
- Stable GHCR container publishing workflow (`.github/workflows/ghcr.yml`) for `linux/amd64`.
- Public deployment configuration (`compose.ghcr.yaml`) with configurable `YTMDL_VERSION`.
- GitHub Releases based update detection via official REST API with strict SemVer 2.0 comparison.
- System & Updates status panel in Settings WebUI with safe plain text release notes rendering.
- Manual update check endpoint (`POST /api/v1/system/update/check`) with CSRF protection and singleflight deduplication.

### Security / Privacy

- Hardcoded `https://api.github.com` host guaranteeing protection against SSRF attempts.
- Update checks are completely optional and can be disabled via `MUSICDL_UPDATE_CHECKS_ENABLED=false`.
- Zero telemetry: no user identifiers, music library metrics, or host statistics are transmitted.
- Release notes are rendered strictly as plain text (pre-wrap) with zero HTML execution.

### Distribution

- Support for prebuilt public OCI images via GitHub Container Registry (`ghcr.io/der-felix/ytmdl-*`).
- Source builds remain fully supported via `compose.yaml`.
- Clean separation between internal development repository and public GitHub release repository.

### Important

- Update detection is informational only; v0.15.0 does not perform automatic updates or container restarts.
- No database migration required (Schema remains 8).

## 0.14.1 — 2026-09-03

### Added

- Optional Genius lyrics fallback provider as third tier in lyrics resolver (`LRCLIB -> YouTube Music -> Genius`).
- Conservative song and variant matching with strict false-positive rejection for studio vs live/remix variants.
- Plain-text lyrics support (`.txt` sidecar files; never generates `.lrc` files).
- Runtime enable/disable toggle in Settings with automatic default-off behavior.
- Optional `GENIUS_ACCESS_TOKEN` configuration with token-configured status badge in Settings UI.
- Best-effort unauthenticated web search fallback when no API token is provided.
- Manual track lyrics refresh endpoint (`POST /api/v1/library/tracks/{id}/lyrics/refresh`).
- Read-only missing-lyrics backfill preview endpoint (`GET /api/v1/library/lyrics/backfill/preview`).
- Subtle lyrics source attribution (`Quelle: Genius`) in Web Player for plain-text lyrics.
- Existing synchronized (`.lrc`) and plain (`.txt`) lyrics sidecars are strictly protected and never overwritten.
- Non-fatal error handling with automated 5-minute circuit breaker on consecutive provider failures.
- No database migration required (Schema remains 8).

## 0.14.0 — 2026-09-03

### Added

- Integrated Web Player with persistent mini-player and immersive full-screen Now Playing view.
- Direct authenticated audio streaming endpoint (`GET /api/v1/library/tracks/{id}/stream`) with full HTTP 206 Range seeking support.
- Synchronized (`.lrc`) and plain (`.txt`) lyrics display with smooth autoscrolling and gradient fade mask.
- 10-Band Graphic Equalizer with studio console pill handles and customizable factory presets.
- Precision Parametric Equalizer with configurable filter types, frequency, gain, and Q-factor.
- Client-side DSP pipeline with Preamp, Auto-Headroom protection, Audio Safety Limiter (-0.5 dBFS), Stereo Balance with center notch, and Mono summing.
- Crossfade playback with smart consecutive album transition bypass and deck preloading.
- Real-time Web Audio Visualizer supporting Spectrum Analyzer and Waveform modes.
- Media Session API integration for OS/hardware media keys and background playback control.
- Scoped browser state persistence with safe non-autoplay state restoration.
- Responsive mobile web player viewports.
- No database migration required (Schema remains 8).

## 0.13.6 — 2026-09-02

### Added

- Validated Direct-ID matching fast path in YouTube media provider for exact source video IDs.
- Plausibility and duration validation guards for direct candidates.
- Safe fallback to generic query search on unavailable, geoblocked, or score-rejected direct candidates.
- Comprehensive unit and regression test suite covering exact matches, variant mismatches, and edge cases.
- No database migration required (Schema remains 8).

## 0.13.5 — 2026-09-02

### Fixed

- Batch and artist jobs now show live progress based on persisted job items instead of stale parent-job counters.
- Job list and detail endpoints now report consistent progress.
- Paginated job progress aggregation avoids N+1 queries.
- No database migration required (Schema remains 8).

## 0.13.4 — 2026-09-02

### Added

- Portable versioned JSON export for artist subscriptions (`ytmdl-subscriptions` format v1).
- Two-step subscription import with non-mutating preview, change diffing, duplicate detection, and transactional apply.
- Idempotent subscription imports without automatic job generation or background download triggers.
- Compact subscriptions table with live search, filters (status, active/paused, auto-download, provider), and multi-field sorting.
- Multi-select row selection with bulk actions (enable, pause, sync, export selected, delete with confirmation modal).
- Dedicated full-width administration workspace (`AdminLayout`) with horizontal tab navigation for users, server settings, storage, and diagnostics.
- Responsive layout enhancements across desktop, tablet, and mobile viewports.
- No database migration required (Schema remains 8).

## 0.13.3 — 2026-09-01

### Fixed

- Extract true release cover artwork from YouTube Music release headers, explicitly ignoring artist avatars from `straplineThumbnail`.
- Tolerate capability-related `EPERM` / `EOPNOTSUPP` errors on POSIX `chmod` when writing atomic files to CIFS/SMB storage where permissions are fixed at mount time (`nounix`).
- Ensure strict permission error checking is maintained on local filesystems (`ext4`, `xfs`, `apfs`, `btrfs`).
- Retain NFS filesystem support as experimental.
- Losslessly rewrite Vorbis comment artwork blocks in Ogg Opus files without audio re-encoding, preserving 100% byte-identical audio packets and identical decoded PCM audio waveforms.
- Preserve existing custom/foreign `cover.jpg` sidecar files against unprompted overwrite via atomic write safety.
- Provide admin-authenticated artwork preview and refresh endpoints (`/api/v1/library/releases/{id}/artwork/preview` and `/refresh`) with full transaction safety and idempotency.
- No database migration required (Schema remains 8).

## 0.13.2 — 2026-08-31

### Fixed

- Deduplicate tracks with identical stable `Track.ID` / provider `SourceID` across differently named releases (e.g. standard vs extended/alt single releases).
- Prevent duplicate `tracks.id` persistence collisions (`SQLSTATE 23505`) in PostgreSQL catalog.
- Make existing-track persistence idempotent and race-safe under concurrent workers (`release_id = COALESCE(release_id, ...)`).
- Avoid orphaned/untracked physical audio files on target storage caused by post-placement database transaction rollbacks.
- Preserve representative release ranking deterministically across input orders.
- No database migration required (Schema remains 8).

## 0.13.1 — 2026-08-28

### Fixed

- Fixed false-positive `PATH_MISMATCH` findings for Singles and EPs.
- Integrity audits now use the actual `releases.release_type` from PostgreSQL.
- Canonical path calculation is consistent between audit, repair, reorganize and downloader.
- Compilation / Live / Remix release types now use the same canonical release-type-aware path calculation.
- Existing audit history remains unchanged.
- No database migration required.

## 0.13.0 — 2026-08-28

### Library Integrity

- persistent Quick and Deep audits
- local DB/filesystem reconciliation
- audio, tags, cover and lyrics validation
- legacy and duplicate detection
- zero provider calls during audits
- server-side finding pagination and filters

### Safe Repair

- repair preview before every mutation
- SHA-256 stale-preview protection
- canonical path repair
- safe file adoption
- tag restoration without transcoding
- quarantine instead of direct deletion

### Safety

- Storage Identity Guard before repair
- shared path locking with downloads and maintenance
- atomic no-replace semantics
- crash-safe repair recovery
- idempotent apply
- no automatic repair
- no automatic delete
- no Repair All

### Quarantine

Reserved internal directory:

`/music/.ytmdl-trash`

YTMDL selbst ignoriert dieses Verzeichnis bei Integrity Scans. Media-Server-Administratoren sollen es vom Scan ausschließen (Plex, Jellyfin, Emby, Navidrome ignorieren es nicht zwingend automatisch).

## 0.12.0 — 2026-08-28

Download-Management, Automatisierung, Prioritäten, Zeitsteuerung und erweiterte Abonnement-Steuerung.

### Queue Management

- Low / Normal / High Prioritäten für Download-Aufträge
- Faires Scheduling (Fair Interleaving) zwischen parallelen Jobs
- Starvation Protection & dynamisches Aging für lange wartende Aufträge
- Persistentes, individuelles Job-Pause und Resume
- Konfigurierbarer Worker-Pool (1 bis 4 parallele Download-Worker)
- Live Worker-Limit Hot-Reloading ohne Service-Neustart

### Automation

- Download-Zeitfenster mit nativer Server-Zeitzonen-Unterstützung und DST-Kompensation
- Konfigurierbare Bandbreitenbegrenzung (Rate Limit) pro Download via yt-dlp
- Flexible Release-Type-Filter für Künstler-Abonnements (Alben, EPs, Singles, Live, Compilations, Remixes)
- Konfigurierbare Standard-Download-Priorität für Abonnements

### Retry & History

- Gezieltes Wiederholen einzelner fehlgeschlagener Tracks direkt aus der UI
- Sammel-Wiederholung fehlgeschlagener Tracks pro Job ("Fehlgeschlagene wiederholen")
- Permanente Sicherheit bei Pfadkonflikten (`PATH_CONFLICT` bleibt geschützt und wird beim Bulk-Retry übersprungen)
- Serverseitige Pagination für die Download-Historie mit stabilen Tie-Breakern
- Filterung nach Status und Priorität
- Sicherer, administrativer Verlauf-Cleanup (Musikbibliothek, Audiodateien, Metadaten und Cover bleiben zu 100% unberührt)

### Compatibility & Storage

- **Bestehende Jobs**: Automatische Zuordnung zu Normal-Priorität und unpausiertem Status
- **Bestehende v0.11 Abonnements**: Beibehaltung der Normal-Priorität und bisherigen Release-Typen (Alben, EPs, Singles)
- **Neue Abonnements**: Standard-Priorität konfigurierbar (Initial: Niedrig)
- **Storage Support**: Local (Supported), SMB/CIFS Host Mount (Supported), NFS (Experimental)

## 0.11.0 — 2026-08-28


Zuverlässige Downloads, Crash-Recovery, transaktionale Persistenz und Vorbereitung für Netzwerkspeicher (NFS & SMB).

### Hinzugefügt

* **Zuverlässige Downloads & Crash-Recovery (`internal/storage/staging.go`, `internal/jobs/worker.go`)**:
  * **Persistentes lokales Staging (`/data/staging`)**: Deterministische Verzeichnisstruktur `/data/staging/<item-id>/` für Downloads und Audio-Verarbeitung, die Server-Neustarts und Container-Crashes unbeschadet übersteht.
  * **yt-dlp Download-Resume**: Automatische Fortsetzung unvollständiger Audio-Downloads via HTTP Range Requests (`.part` / `.ytdl`).
  * **Crash-Recovery (SIGTERM / SIGKILL)**: Unterbrochene Items verbleiben im persistenten Staging und setzen nach dem Neustart an der vorhandenen Byte-Grenze an.
  * **Persistente Retry-Steuerung**: Retry-Zähler (`attempts`) wird in PostgreSQL transaktional persistiert und bei Neustarts nicht zurückgesetzt; exponentieller Backoff mit Jitter.
  * **Wartezustände ohne Retry-Verbrauch**: Neue Statuswerte `waiting_for_storage` und `waiting_for_space` pausieren Jobs ohne Verlust von Retry-Versuchen.
  * **Globaler Provider-Cooldown**: Zentraler `MediaCooldownManager` fängt HTTP 429 / Rate-Limits providerweit ab und verhindert Anfragestürme paralleler Worker.

* **Sichere Finalisierung & Dateisystem-Schutz (`internal/storage/library.go`, `internal/storage/rename_*.go`)**:
  * **SHA-256 Validierung**: Prüfung von Größe und SHA-256 Prüfsumme vor der finalen Bibliotheksübernahme.
  * **Race-sicherer atomarer Commit**: Einsatz von `renameat2(RENAME_NOREPLACE)` unter Linux und `link()` + `unlink()` unter macOS/POSIX verhindert TOCTOU Race Conditions und stilles Überschreiben fremder Dateien.
  * **Idempotente Crash-Recovery**: Bereits platzierte Zieldateien werden anhand des SHA-256 Hashes idempotent erkannt, ohne fälschlicherweise `PATH_CONFLICT` auszulösen.
  * **Strikte `PATH_CONFLICT` Sicherheit**: Fremde Dateien am Zielpfad werden unter allen Umständen unverändert erhalten.
  * **Entkoppelte Finalisierung**: Separate, begrenzte Ausführungslane (`finalizerSem = 1`) schützt Netzwerk-Dateisysteme vor Überlastung, während Download-Worker lokal weiterarbeiten.

* **Speicher-Diagnose & Schutz (`internal/storage/guard.go`, `internal/api/handlers/storage.go`)**:
  * **Storage Identity Guard**: Validierung der Identitätsmarkerdatei `.ytmdl-storage-id` vor jeglichen Schreibvorgängen; 0 Schreibzugriffe bei fehlendem oder falschem Marker.
  * **Sicherheit vor Informationslecks**: `GET /api/v1/storage/status` liefert ausschließlich `guard_status` (`disabled | verified | missing | mismatch | invalid`) ohne Exponierung geheimer Guard-IDs oder UUIDs.
  * **Space Guards**: Überwachung konfigurierbarer Mindest-Freispeicher-Grenzwerte für Staging und Zielbibliothek.
  * **Unabhängige Health-Prüfung**: `/api/v1/health?scope=essential` sichert Container-Gesundheit auch bei temporären Speicher-Ausfällen (keine Restart-Loops).
  * **Persistente Queue-Steuerung**: Queue Pause/Resume übersteht Neustarts via Einstellungsdatenbank (`queue.paused`).

* **Frontend Speicher-Diagnose (`frontend/src/components/storage/StoragePanel.tsx`)**:
  * **Speicher-Panel in Einstellungen**: Übersicht über Belegung, freie Kapazität, Dateisystem-Typ und Storage Guard Status.
  * **Manuelle Integritätsprüfung**: "Speicher prüfen"-Aktion mit sofortiger Live-Diagnose.
  * **Queue-Pause-Schalter**: Pausieren und Fortsetzen des Download-Dispatchers direkt aus den Einstellungen.
  * **Erweiterte Download-UX**: Neue Lokalisierungen und Badges für `waiting_for_storage`, `waiting_for_space` und `retry_wait`.

* **Netzwerkspeicher-Architektur & Dokumentation (`docs/storage/`)**:
  * **Host-Mount Architektur**: Bereitstellung von Richtlinien für NFSv4.2 (`docs/storage/nfs.md`) und SMB 3.1.1 (`docs/storage/smb.md`).
  * **Support-Status**:
    * **Local Storage**: Supported
    * **Host-mounted SMB/CIFS**: Supported (Post-Release auf Linux Host-Mount mit SMB 3.1.1, UID/GID 10001 und Storage Identity Guard erfolgreich validiert)
    * **Host-mounted NFS**: Experimental – real NFS release verification pending

* **Datenbankmigration `0006_reliable_downloads_storage.sql`**:
  * Ergänzung der Statuswerte `retry_wait`, `waiting_for_storage`, `waiting_for_space` und `finalizing` im `job_items_status_check` Constraint.
  * Neue Spalten `max_attempts`, `next_retry_at`, `staging_relpath`, `staged_size` und `staged_sha256` auf `job_items`.
  * Partielle B-Tree-Indizes zur Beschleunigung des Dispatchers.

## 0.10.0 — 2026-08-27

Umfassendes lokales Bibliotheks-Erlebnis ("Library Experience & Discoverability"), serverseitige Suche, Filterung, Sortierung, datenbankgestützte Paginierung, dedizierte lokale Künstler- und Release-Detailansichten, technischer Track-Inspektor, formatierte synchronisierte Songtexte und optimiertes Dashboard.

### Hinzugefügt

* **Lokale Bibliotheks-Erfahrung & Navigation (`frontend/src/pages/Library*`)**:
  * **100 % lokale Trennung**: `/library`, `/library/artists/:id` und `/library/releases/:id` arbeiten ausschließlich mit lokalen YTMDL-Datenbankeinträgen und Dateien ohne externe Provider-Netzwerkaufrufe.
  * **Dedizierte lokale Künstleransicht (`/library/artists/:id`)**: Übersicht über alle lokal vorhandenen Alben, EPs, Singles und Tracks eines Künstlers mit Subscription-Status und aggregierter Speichergröße.
  * **Dedizierte lokale Releaseansicht (`/library/releases/:id`)**: Lokale Trackliste mit Multi-Disc-Gruppierung (`Disc 1`, `Disc 2`), Gesamtspieldauer, Dateigröße und Lyrics-Badges.
  * **Track-Detail-Inspektor (`components/music/TrackDetailDialog`)**: Slide-over Dialog mit Anzeige aller Metadaten, technischer Audioparameter (Codec, Bitrate, Sample-Rate, Kanäle), Dateipfad, ISRC und integriertem Lyrics-Viewer.
  * **URL-Synchronisation & Deep-Linking**: Vollständige Synchronisation aller Filter, Suchen, Tabs und Paginierungszustände mit dem Browserverlauf (`/library?view=...&q=...&sort=...&page=...` und `/library?track=<id>`).
  * **Tab-Navigation**: Schneller Wechsel zwischen Releases, Titeln, Künstlern und Wartung.
* **Serverseitige Suche, Filterung & Sortierung (`internal/database/repository/catalog_library.go`)**:
  * **Neue REST-Endpunkte**: `GET /api/v1/library/artists`, `GET /api/v1/library/artists/{id}`, `GET /api/v1/library/releases`, `GET /api/v1/library/releases/{id}`, `GET /api/v1/library/tracks`, `GET /api/v1/library/tracks/{id}` und `GET /api/v1/library/search`.
  * **Parameterisierte Suche**: Sichere ILIKE-Suche über Titel, Künstler und Alben sowie exakte (case-insensitive) ISRC-Suche.
  * **Strikte Sortier-Allowlist**: Absicherung aller Sortier- und Order-Parameter gegen SQL-Injection mit HTTP 400 Bad Request bei ungültigen Eingaben.
  * **Skalierbare Paginierung**: Einheitlicher `meta`-Block `{ count, total, limit, offset }` mit serverseitig begrenzten Limits (Tracks: 50/100, Releases/Artists: 24/120).
* **Verbesserte Songtext-Darstellung (`src/lib/utils/lrc.ts`, `components/music/LyricsPanel`)**:
  * **Robuster LRC-Parser**: Saubere Formatierung synchronisierter Songtexte mit 2- und 3-stelligen Millisekunden, 1–3-stelligen Minuten, Mehrfach-Timestamps, Metadaten-Tags (`[ar:]`, `[ti:]`, `[offset:]`), CRLF- und Unicode/Emoji-Sicherheit.
  * **Flexibler Lyrics-Viewer**: Umschaltbare Zeitanzeige und Unterstützung für Plain-Text, synchrone Texte, instrumentale Stücke und nicht gefundene Songtexte.
* **Dashboard-Optimierung (`frontend/src/pages/Dashboard.tsx`)**:
  * **Echtzeit-SQL-Aggregate**: Schnelle Kennzahlen für Künstler, Releases, Tracks, Dateien, Speicherplatz und Codecs ohne In-Memory Schleifen.
  * **Lyrics-Abdeckung**: Kompakte visualisierte Abdeckung für synchrone, plain und instrumentale Songtexte.
  * **Kürzlich hinzugefügt**: Cover-Vorschau der zuletzt in die Bibliothek importierten Releases.
* **Datenbankmigration `0005_library_indexes.sql`**:
  * Rein additive B-Tree-Indizes für `tracks(created_at DESC)`, `releases(created_at DESC)`, `releases(year DESC, title)` und `tracks(lyrics_state)` zur Beschleunigung von Sortierung und Filterung.

## 0.9.1 — 2026-08-27

### Behoben

* **Lyrics-Backfill Lifecycle-Entkopplung (`internal/library`, `internal/api/handlers`)**:
  * Lyrics backfill now runs independently from the HTTP request lifetime.
  * POST `/api/v1/library/lyrics/backfill` returns immediately with HTTP 202.
  * Client disconnects and HTTP request timeouts no longer cancel active backfills.
  * Backfill remains tied to the application lifecycle and stops cleanly during server shutdown.
  * Backfill status remains queryable independently through GET `/api/v1/library/lyrics/backfill`.
  * Existing single-flight, pacing and transient-error semantics remain unchanged.

## 0.9.0 — 2026-08-27

Universelle Medienserver-Kompatibilität (Plex, Jellyfin, Emby), synchronisierte & Plain-Text Songtexte (LRCLIB & YouTube Music), Sidecar-Dateien, nicht-destruktive Bibliotheks-Reorganisation und UI-Lyrics-Verwaltung.

### Hinzugefügt

* **Universelle Medienserver-Kompatibilität (`internal/storage`, `internal/metadata`, `internal/jobs`)**:
  * Einheitliches Standard-Ablageschema für Plex, Jellyfin und Emby ohne serverspezifische Modi: `<Album Artist>/<YYYY - Title [Type]>/<NN - Title.ext>` (bzw. `<DNN - Title.ext>` bei Multi-Disc-Releases).
  * **Multi-Disc Struktur**: Flache Ordnerstruktur ohne Disc-Unterordner mit Standardpräfixen (`101`, `201` etc.) und nahtlos korrespondierenden Sidecar-Dateinamen.
  * **Cover-Bildverwaltung**: Speicherung von Cover-Bildern als `cover.jpg` (bzw. `cover.png` bei echtem PNG-MIME) im Albumordner ohne Überschreibung bestehender Bilddateien.
  * **Erweiterte Vorbis-Comments**: Standardkonforme Tags `ALBUMARTIST`, `TRACKTOTAL`, `DISCTOTAL`, `COMPILATION` (1/0) und vollständiger Erhalt bestehender Metadaten beim Retagging.
  * **Präzise Künstlerkredit- & Kompilationsklassifikation**: Strukturierte Metadaten provider-übergreifend bevorzugt; zuverlässiger Erhalt legitimer Bandnamen (`Simon & Garfunkel`, `Earth, Wind & Fire`, `Hall & Oates`, `AC/DC`).
  * **Schutz vor stillen Dateiüberschreibungen**: Rückgabe von `PATH_CONFLICT` bei fremden Zieldateien ohne Anlegen von Zähler-Duplikaten (`Song (1).opus`).
* **Songtext- & Lyrics-Integration (`internal/lyrics`, `internal/jobs`)**:
  * **LRCLIB als primärer Provider**: Automatische Suche nach synchronisierten (`.lrc`) und plain (`.txt`) Lyrics mit sequenzieller Token-Bucket-Drosselung (400ms) und strikter `Retry-After`-Auswertung bei HTTP 429.
  * **YouTube Music Fallback**: Automatischer Rückfall auf YouTube Music Plain-Text Lyrics (`next` + `browse` API) bei fehlenden LRCLIB-Treffern.
  * **Sidecar-Dateien als Single Source of Truth**: `.lrc`- (bei synchronen Texten) und `.txt`-Dateien (bei Plain-Text) mit atomarem Schreibvorgang (Temp-Datei, Sync, Rename).
  * **Robuste Zustandsmaschine**: Zustände `unknown`, `available_synced`, `available_plain`, `instrumental` und `not_found` mit 14-Tage-Cooldown bei `not_found`. Transiente Fehler (Netzwerk, Timeouts, 5xx) setzen keinen Cooldown.
  * **Lyrics-Verwaltung per API**: `GET /api/v1/library/tracks/{id}/lyrics`, `POST /api/v1/library/tracks/{id}/lyrics/refresh` und `DELETE /api/v1/library/tracks/{id}/lyrics`.
  * **Bulk Lyrics Backfill (`internal/library/backfill.go`)**: Hintergrund-Job (`POST`/`GET` `/api/v1/library/lyrics/backfill`) zur sequenziellen Single-Flight Nachindizierung bestehender Tracks mit Live-Fortschrittsaktualisierung.
* **Nicht-destruktive Bibliotheks-Reorganisation (`internal/library/compat.go`)**:
  * **Schreibgeschützter Kompatibilitätsreport (`GET /api/v1/library/compatibility`)**: Identifiziert Legacy-Pfade (`artist_folder`, `multidisc_name`, `missing_totals`, `missing_lyrics`).
  * **Bestätigte Reorganisation (`POST /api/v1/library/reorganize`)**: Verschiebt ausgewählte Audio- und Sidecar-Dateien sicher, aktualisiert Dateirekords atomar und bereinigt leere Quellverzeichnisse.
* **Webfrontend-Erweiterungen**:
  * **LyricsBadge & LyricsPanel (`components/music`)**: Status-Badges auf Trackzeilen und interaktives Modal zum Lesen, Aktualisieren und Löschen von Songtexten.
  * **Bibliotheksverwaltung (`/library`)**: Werkzeuge für Server-Kompatibilitätsprüfung, geführte Reorganisation und Lyrics-Backfill.
  * **Erweiterte Einstellungen (`/settings`)**: Toggles für automatische Lyrics-Suche (`lyrics_enabled`) und Sidecar-Dateien (`lyrics_write_sidecar`).
* **Datenbankmigration `0004_lyrics_media_compat.sql`**:
  * Rückwärtskompatible Erweiterung der Tabelle `tracks` um `lyrics_state`, `lyrics_provider`, `lyrics_checked_at` und `compilation`.

## 0.8.0 — 2026-08-27

Lokale Benutzer- und Rechteverwaltung, First-Run Administrator Setup, Argon2id-Passwortsicherheit, serverseitige Sessions und konfigurierbare Trusted Proxies.

### Hinzugefügt

* **First-Run Administrator Setup (`POST /api/v1/auth/setup`)**: Initiales Einrichten des ersten Administrators beim ersten Start ohne Benutzer mit PostgreSQL Advisory Transaction Lock (`7311402659002`) gegen Race Conditions.
* **Passwortsicherheit mit Argon2id (`internal/auth`)**: OWASP-konformes Password-Hashing mit 64 MiB Speicher, $t=3$, $p=2$, 16 Bytes kryptografischem Zufallssalt und 32 Bytes Schlüssel im PHC-Format. Dummy-Verifikation bei unbekannten Benutzern zur vollständigen Eliminierung von Timing-Angriffen.
* **Serverseitiges Session-Management (`internal/auth`, `internal/database/repository/sessions.go`)**: 256-Bit Zufallstoken als `HttpOnly`, `SameSite=Lax` Cookie (`ytmdl_session`), Speicherung in PostgreSQL ausschließlich als SHA-256 Hash. Konfigurierbare Session-Gültigkeit (7 Tage Inaktivität / 30 Tage absolute Lebensdauer), gedrosselte Touch-Aktualisierung (15 Min.) und automatischer stündlicher Hintergrund-Cleanup.
* **CSRF-Schutz (`internal/api/middleware`)**: Double-Submit-Cookie-Verfahren (`ytmdl_csrf`) mit zeitkonstantem Header-Vergleich (`X-CSRF-Token`) für alle mutierenden API-Endpunkte.
* **Login Rate Limiting (`internal/auth/limiter.go`)**: Sliding-Window Limiter gegen Brute-Force-Angriffe (max. 5 Fehlversuche pro 5 Minuten pro Client-IP und pro IP+Benutzername) mit automatischem Cleanup.
* **Rollenmodell (Admin / User)**:
  * **Admin**: Voller Zugriff auf Benutzerverwaltung (`/api/v1/users`), Systemeinstellungen (`PUT /api/v1/settings`) und physische Bibliothekslöschungen.
  * **User**: Zugriff auf Dashboard, Suche, Downloads, Abonnements, Metadaten-Reparaturen, Profil und eigene Sitzungen.
  * *Hinweis:* Die Musikbibliothek bleibt gemeinsam verwaltet (shared library).
* **Benutzerverwaltung (`/users`)**: Administratives Anlegen, Aktivieren/Deaktivieren, Rollenwechsel, Passwort-Reset und Löschen von Benutzern.
* **Last-Admin Invariante**: PostgreSQL Transaction Advisory Lock (`7311402659003`) verhindert, dass der letzte verbleibende Administrator gelöscht, deaktiviert oder herabgestuft wird.
* **Profil- & Sitzungsverwaltung (`/profile`)**: Eigenständiges Ändern des Anzeigenamens, Passwortänderung mit automatischer Abmeldung anderer Sitzungen sowie Übersicht und gezieltes Beenden aktiver Sitzungen.
* **Session Revocation**: Sofortiges Invalidieren und Löschen von Sitzungen in der Datenbank bei Deaktivierung, Passwortänderung oder administrativem Passwort-Reset.
* **Konfigurierbare Trusted Proxies (`MUSICDL_TRUSTED_PROXIES`)**: Sichere Standard-Einstellung (nur Loopback `127.0.0.1/32`, `::1/128`). Sichere Auswertung von `X-Forwarded-For` und `X-Forwarded-Proto` ausschließlich bei Anfragen von explizit vertrauenswürdigen Proxies / Subnetzen; Schutz vor IP- und HTTPS-Spoofing.
* **Authentifizierung für SSE und API-Endpunkte**: `GET /api/v1/events` und alle Anwendungsrouten erfordern eine gültige Session. Öffentliche Endpunkte beschränken sich auf `/health`, `/auth/status`, `/auth/setup` und `/auth/login`.
* **Datenbankmigration `0003_users_sessions.sql`**: Rückwärtskompatibles Anlegen der Tabellen `users` und `sessions` mit Case-Insensitive Unique Index auf Benutzernamen; Bestandsdaten bleiben vollständig erhalten.

### Bekannte Einschränkungen

* **Gemeinsame Bibliothek (Shared Library)**: Die Musikbibliothek wird zwischen allen registrierten Benutzern geteilt; es gibt keine benutzerspezifischen Download-Verzeichnisse oder Multi-Tenant-Isolierung.

## 0.7.0 — 2026-08-27

Bibliotheksverwaltung, Reconciliation, Health-Scanning, sichere Reparaturaktionen und Wartungs-UI.

### Hinzugefügt

* **Library Health Scanning & Reconciliation (`internal/library`)**: Vollständiger Abgleich zwischen physischem Dateisystem und PostgreSQL-Datenbank über `POST`/`GET` `/api/v1/library/scan` und `GET` `/api/v1/library/stats`.
* **Erkennung von 6 Health-Zuständen**: Erkennung von `healthy` (intakt), `missing_file` (DB vorhanden, Datei fehlt), `orphan_file` (Datei vorhanden, DB fehlt), `invalid_file` (korrupte/unlesbare Audiodatei), `metadata_mismatch` (Tags weichen vom Katalog ab) und `duplicate_file` (mehrere Datensätze verweisen auf denselben Track/Pfad).
* **Sicherer Track-Redownload (`POST /api/v1/library/tracks/{id}/redownload`)**: Gezieltes erneutes Herunterladen fehlender Tracks über die bestehende Job-Queue.
* **Verlustfreies Retagging (`POST /api/v1/library/tracks/{id}/retag`)**: Vorbis-Comment-Header-Rewrite für Opus/Ogg-Dateien ohne FFmpeg und ohne Audio-Dekodierung; Stream-Copy (`-c:a copy`) für sonstige Container unter Erhalt eingebetteter und lokaler Cover-Bilder.
* **Sicheres Track-Löschen (`DELETE /api/v1/library/tracks/{id}`)**: Atomares Entfernen physischer Dateien, Dateirekords und Katalogeinträge.
* **Chirurgisches Release-Löschen (`DELETE /api/v1/library/releases/{id}`)**: Löscht ausschließlich registrierte Track-Dateien und `cover.jpg` des Releases; unbekannte oder verwaiste Dateien im Ordner bleiben erhalten (kein blindes `RemoveAll`).
* **Sicheres Orphan-Löschen (`DELETE /api/v1/library/scan/issues/{id}`)**: Löschen verwaister Dateien ausschließlich über serverseitig generierte Scan-Issue-IDs mit Verifikation vor I/O.
* **Dateisystem-Confinement & Symlink-Schutz**: Striktes Confinement gegen Path Traversal (`../`) und Symlink-Escape vor jeglichen Dateioperationen.
* **Parallele Job-Konfliktvermeidung**: Automatische Prüfung auf aktive Downloads vor Retag, Redownload und Delete (Rückgabe von HTTP 409 Conflict bei Konflikten).
* **SSE Scan-Fortschritt**: Echtzeitübertragung des Scan-Fortschritts über `/api/v1/events` (`library_scan_started`, `library_scan_progress`, `library_scan_completed`).
* **Bibliotheks- und Wartungs-UI**: Interaktive `/library`-Seite mit Speicher- und Health-Metriken, On-Demand-Scan, Filterchips und Aktionsschaltflächen für Reparatur und Löschung.

### Bekannte Einschränkungen

* **Große / langsame Speicher**: Tiefenscans auf sehr großen Bibliotheken oder langsamen SMB/NFS-Netzwerk-Mounts können längere Ausführungszeiten beanspruchen.

## 0.6.0 — 2026-08-27


Künstler-Abonnements mit automatischer Diskografie-Synchronisation, periodischer Scheduler, Deezer Request-Pacing und UI-Verwaltung.

### Hinzugefügt

* **Künstler-Abonnements (`internal/subscriptions`)**: Dauerhafte Beobachtung von Künstlern (`GET`/`POST` `/api/v1/subscriptions`, `GET`/`PATCH`/`DELETE` `/api/v1/subscriptions/{id}`). Ein Sync vergleicht die Diskografie beim jeweiligen Metadatenprovider mit der lokalen Bibliothek und erkennt neu erschienene Releases und Aufnahmen.
* **Manuelle und geplante Synchronisation**: Manueller Sync-Trigger (`POST /api/v1/subscriptions/{id}/sync`) und konfigurierbarer Hintergrund-Scheduler (`MUSICDL_SUBSCRIPTIONS_ENABLED`, `MUSICDL_SUBSCRIPTION_SYNC_INTERVAL`, `MUSICDL_SUBSCRIPTION_CHECK_INTERVAL`, `MUSICDL_SUBSCRIPTION_RETRY_INTERVAL`, `MUSICDL_SUBSCRIPTION_SYNC_TIMEOUT`, `MUSICDL_SUBSCRIPTION_BATCH_SIZE`).
* **Automatischer Download neu entdeckter Tracks**: Optionales automatisches Einreihen fehlender Releases in die Download-Warteschlange (`auto_download`) unter Berücksichtigung von Release-Filtern (Alben, Singles, EPs).
* **Persistenter Abonnement-Zustand**: PostgreSQL-Schema `0002_artist_subscriptions.sql` mit Tabellen `artist_subscriptions`, `subscription_sync_runs` und `subscription_sync_run_items` zur dauerhaften Speicherung von Metadaten, Synchronisationsergebnissen und Fehlern.
* **Subscription SSE-Events**: Echtzeit-Fortschrittsaktualisierung über `/api/v1/events` (`subscription.sync.started`, `subscription.sync.progress`, `subscription.sync.completed`, `subscription.sync.failed`).
* **Abonnement-Verwaltungs-UI**: Eigene Seite `/subscriptions` mit Statusübersicht, manuellem Sync, Auto-Download-Toggle, Pausieren/Fortsetzen und Löschen; Schnellzugriff über `SubscribeControl` auf Künstlerseiten.
* **Deezer Request-Pacing & Bounded Retry (`internal/provider/deezer`)**: Token-Bucket-Drosselung (`MUSICDL_DEEZER_REQUESTS_PER_SECOND`, `MUSICDL_DEEZER_BURST`) und begrenzter exponentieller Retry (`MUSICDL_DEEZER_MAX_RETRIES`, `MUSICDL_DEEZER_RETRY_BACKOFF`, `MUSICDL_DEEZER_MAX_RETRY_BACKOFF`) für zuverlässige Synchronisation großer Diskografien ohne Deezer-Quota-Verluste. Liveness-Probes werden bewusst ungedrosselt ausgeführt.
* **Partielles Sync-Retry-Scheduling**: Differenzierte Folgezeitpunkte (`apperr.Retryable`): Vorübergehende Fehler (z. B. Rate Limits) werden nach dem kürzeren Retry-Intervall erneut versucht; permanente Fehler behalten das normale Sync-Intervall.
* **Schutz vor doppelten Queue-Einträgen**: Verhinderung redundanter Downloads bei noch unfertigen Jobs für dasselbe Release.

### Bekannte Einschränkungen

* **Batch-Deduplizierung**: Der In-Memory-Dedup-Check läuft bei großen Release-Mengen in O(n²), liegt bei 185 Releases jedoch im Bereich von ~0,2s gegenüber ~47s Gesamtsync-Dauer.
* **Geteilte Releases bei parallelem Sync**: Wenn zwei unterschiedliche Künstler dasselbe Release teilen, kann ein gleichzeitiger Sync beider Abonnements kurzzeitig parallele Queue-Einträge erzeugen; keine Datenkorruption.
* **Deezer-Liveness-Probe**: Die Verfügbarkeitsprüfung wird absichtlich nicht gedrosselt, um stets den aktuellen Provider-Status zu liefern.

## 0.5.0 — 2026-08-27

Deezer als standardmäßiger credential-freier Metadatenprovider, Spotify als optionaler Metadatenprovider, Provider-Fallback-Kette und automatisierte CI/CD Release-Pipeline.

### Hinzugefügt

* **Deezer Metadatenprovider (`internal/provider/deezer`)**: Verwendet Deezer's öffentlich zugängliche Katalog-Endpunkte (`/search/artist`, `/artist/:id`, `/artist/:id/albums`, `/album/:id`, `/album/:id/tracks`, `/track/:id`) für vollständige Künstlersuche, Diskografien und Tracklisten inklusive ISRC, Track-/Disc-Nummerierung und hochauflösenden Coverbildern. Erfordert keine Zugangsdaten, API-Keys oder ARL-Cookies und wird ausschließlich für Metadaten verwendet (kein Deezer-Audio/Previews).
* **Provider-Hierarchie & Metadata Fallback**: Deezer ist der neue Standard-Metadatenprovider (`default_metadata: "deezer"`). Fällt Deezer durch Netzwerkfehler, Timeouts oder Rate-Limits aus (`PROVIDER_UNAVAILABLE`), greift das Backend transparent auf YouTube Music zurück. YouTube Music bleibt der primäre Media-/Audio-Provider mit YouTube als Audio-Fallback.
* **Spotify Metadatenprovider (`internal/provider/spotify`)**: Vollständig integriert als optionaler Metadatenprovider über die Spotify Web API (Client Credentials Flow). Wird automatisch aktiviert, wenn `YTDM_SPOTIFY_CLIENT_ID` und `YTDM_SPOTIFY_CLIENT_SECRET` gesetzt sind.
* **Deezer URL- und URI-Erkennung**: Der Resolver (`GET /api/v1/resolve?url=`) und das Frontend erkennen `deezer.com`- und `www.deezer.com`-Adressen (inklusive Sprachpräfixen wie `/de/album/...`) sowie URIs im Format `deezer:artist:...`, `deezer:album:...` und `deezer:track:...`.
* **Gitea CI/CD Release-Pipeline (`.gitea/workflows/release.yml`)**: Automatischer Multi-Job Release-Workflow bei `v*`-Tags für Linting, Tests, PostgreSQL-Integration, Race-Tests, Versionsabgleich und Bereitstellung der Container-Images (`linux/amd64`) in der Gitea Package Registry.
* **Frontend-Aktualisierung**: Servereinstellungen listen Deezer als aktiven Standard-Metadatenprovider; Entdecken-Seite und Eingabevalidierung unterstützen Deezer-Links und -URIs.

### Geändert

* **Standardkonfiguration**: `default_metadata` wechselt von `spotify`/`ytmusic` auf `deezer`. `default_media` bleibt `ytmusic`.

## 0.4.0 — 2026-08-26

Webfrontend für YTMDL, dazu die Backend-Korrekturen, die dafür nötig waren.

### Hinzugefügt

* **Webfrontend V1** auf Bun, React 19, TypeScript, Vite, Tailwind CSS 4,
  shadcn/ui und Base UI: Dashboard, Entdecken, Künstler, Release, Downloads,
  Bibliothek, Servereinstellungen und 404. Navigation über die History API ohne
  Router-Bibliothek, Datenzugriff über `fetch`.
* **Live-Fortschritt über SSE.** Die Downloadansicht folgt `GET /api/v1/events`
  mit der nativen `EventSource` — Status, Zähler und aktueller Track ohne
  Neuladen. Die Ansicht bleibt bedienbar, wenn der Strom abreißt, und lädt beim
  Wiederverbinden nach.
* **Dritter Container `ytmdl-frontend`:** Bun baut, `nginx:alpine` liefert aus.
  Er übernimmt den Host-Port und reicht `/api/*` an `backend:8080` weiter.
  Backend und PostgreSQL veröffentlichen keinen Host-Port mehr.
* **`GET /api/v1/resolve?url=`** löst eine eingefügte Adresse zu Provider, Typ
  und ID auf. Damit versteht das Backend Kanal-, Album- und Spotify-Adressen —
  und über `yt-dlp` auch Handle-Adressen wie `youtube.com/@name`, die selbst
  keine Kanal-ID enthalten. Die Logik liegt im neuen Paket `internal/resolve`;
  das Frontend entscheidet nur noch, ob eine Eingabe gesucht oder aufgelöst wird.

### Behoben

* **Diskografien brachen bei zehn Alben und zehn Singles ab.** Auf den
  „Alle anzeigen"-Seiten trägt jeder Eintrag ein Überlaufmenü mit einem
  „Zum Künstler"-Ziel. `node.browseID()` durchsuchte den Teilbaum, und weil der
  in sortierter Schlüsselreihenfolge durchlaufen wird, kam `menu` vor
  `navigationEndpoint`: jedes Release dieser Seiten wurde als der Künstler
  gelesen und verworfen. `browseID()` bevorzugt jetzt das eigene Navigationsziel
  — dasselbe Muster, das `videoID()` schon verwendet. Linkin Park: 20 → 69
  Releases, Daft Punk: 20 → 38, Lacazette: 12 → 49.
* **Der SSE-Endpunkt sendete seine Header nicht.** `GET /api/v1/events` rief
  `Flush()` vor dem Setzen der Header auf; der Flush committet die Antwort, also
  gingen `Content-Type: text/event-stream` und die übrigen Header verloren.
  Browser lehnen einen Strom ohne `text/event-stream` ab, womit `EventSource`
  grundsätzlich nicht funktionierte.
* **Der API-Proxy fiel nach einem Backend-Neustart auf 502.** nginx löst einen
  literalen Host in `proxy_pass` einmal beim Start auf. Der Upstream läuft jetzt
  über eine Variable, sodass die Auflösung pro Anfrage passiert; der `resolver`
  wird beim Start aus `/etc/resolv.conf` gelesen und ist damit unabhängig vom
  konfigurierten Subnetz.
* **`index.html` wurde vom Browser gecacht** und zeigte nach einem Update
  weiterhin die Bundles der Vorversion. Sie wird jetzt mit `no-cache,
  must-revalidate` ausgeliefert, die gehashten Assets unverändert mit
  `immutable`.
* **Die Transcoding-Einstellung war nicht speicherbar.** Das Frontend bot einen
  Schalter für `allow_transcode` an, das `PUT /settings` nicht annimmt — der
  Speicherversuch endete in HTTP 400. Der Wert wird beim Serverstart festgelegt
  und steht jetzt schreibgeschützt unter „Beim Start festgelegt".
* **Die Sidebar verschwand bei normalen Desktop-Breiten**, weil sie am
  `lg`-Breakpoint (1024 px) hing. Sie erscheint jetzt ab `md` (768 px).

### Geändert

* **Providerdarstellung** in den Servereinstellungen ist nach Provider gruppiert
  statt nach Rolle: `ytmusic` erscheint einmal mit beiden Rollen statt zweimal.
  Weicht der konfigurierte Standard-Metadatenprovider von der Registry ab — etwa
  wenn Spotify ohne Zugangsdaten konfiguriert ist — wird das benannt.
* **Einstellungstexte** benennen keine Dateiformate mehr, die der Workflow nicht
  zusichert; zur Laufzeit änderbare Werte sind von den beim Start festgelegten
  getrennt.
* **Design-Finish:** kräftigere, weich geblurrte Akzent-Glows im Hintergrund,
  Panels mit Blur, Schatten und heller Oberkante, ruhigerer Sidebar-Zustand,
  gleich hohe Release-Karten und ein Dashboard mit weniger Leerraum.

## 0.3.1

Deployment-Struktur für die spätere Webanwendung vorbereitet. Am Backend-Code
wurde nichts geändert.

### Geändert

* **Getrennte, benannte Container.** Aus den Compose-Services `postgres` und
  `musicdl` werden `db` und `backend`, die Container heißen `ytmdl-db` und
  `ytmdl-backend`, der Compose-Projektname ist `ytmdl`. PostgreSQL läuft
  ausschließlich in `ytmdl-db`; Go-Backend, Worker, `yt-dlp`, `ffmpeg` und
  `ffprobe` bleiben zusammen in `ytmdl-backend`.
* **Eigenes Bridge-Netz `ytmdl-net`** mit dem Subnetz `172.31.250.0/28`. Es ist
  bewusst nicht `internal: true`, weil das Backend ausgehenden HTTPS-Zugriff für
  die Provider und `yt-dlp` braucht. Es sind keine statischen Container-IPs
  konfiguriert — die Auflösung läuft über die Service-Namen `db` und `backend`.
* **Volume umbenannt** auf `ytmdl-postgres-data`. Der Mount bleibt auf
  `/var/lib/postgresql`, weil `postgres:18-alpine` mit einem Volume auf dem
  älteren `/var/lib/postgresql/data` den Start verweigert.
* **Standard-Datenbankname und -benutzer** sind jetzt `ytmdl`.
* **Compose-eigene Variablen** heißen `YTMDL_VERSION`, `YTMDL_HOST_PORT`,
  `YTMDL_DATA_PATH`, `YTMDL_MUSIC_PATH` und `YTMDL_ENV_FILE`. Die
  Backend-Variablen behalten das Präfix `MUSICDL_*`; sie wurden bewusst nicht
  aus kosmetischen Gründen umbenannt.

### Dokumentation

* `docs/DEPLOYMENT.md` beschreibt die Drei-Container-Zielarchitektur, das Netz
  samt Subnetz, den Kollisionshinweis und den Weg zum späteren
  `ytmdl-frontend`, das den Host-Port übernimmt und `/api/*` an `backend:8080`
  weiterreicht.

### Unverändert

Backend-Port 8080 bleibt veröffentlicht, PostgreSQL weiterhin ohne Host-Port.
Rootless Podman, UID 10001, `/app` nicht beschreibbar, `/music` beschreibbar,
Healthchecks, Startup-Retry, Graceful Shutdown und die Job-Recovery sind
unverändert und erneut verifiziert. Es wurde kein Frontend-Container angelegt.

## 0.3.0

Umstellung des Katalogs von SQLite auf PostgreSQL.

### Geändert

* **PostgreSQL 18 statt SQLite.** Der Katalog liegt vollständig in PostgreSQL,
  angebunden über `pgx/v5` mit `pgxpool` und dem `stdlib`-Adapter, sodass der
  bestehende Repository-Layer auf `database/sql` unverändert bleibt.
* **Konfiguration über eine Verbindungs-URL.** `MUSICDL_DATABASE_URL` ersetzt
  `MUSICDL_DATABASE` und `YTDM_DATABASE_PATH`. Beide alten Variablen führen
  jetzt zu einem klaren Startfehler statt zu einem stillen Rückfall auf einen
  Standardwert. Die URL wird nur ohne Passwort geloggt.
* **Konfigurierbarer Connection Pool** über `MUSICDL_DB_MAX_CONNS`,
  `MUSICDL_DB_MIN_CONNS`, `MUSICDL_DB_MAX_CONN_LIFETIME`,
  `MUSICDL_DB_MAX_CONN_IDLE_TIME` und `MUSICDL_DB_CONNECT_TIMEOUT`.
* **Schema auf native PostgreSQL-Typen** portiert: `timestamptz` statt
  Textzeitstempeln, `jsonb` für die JSON-Spalten, `double precision` für
  Bewertungen und Bitraten sowie CHECK-Constraints für Job-, Item-, Release- und
  Quellentypen.
* **Compose** startet zusätzlich `postgres:18-alpine` mit eigenem Healthcheck
  und dem Named Volume `postgres_data`. Die Datenbank wird nicht auf dem Host
  veröffentlicht; nach außen bleibt nur Port 8080.
* **Job-Recovery ist deterministisch.** Nach Shutdown, Absturz oder Neustart ist
  jeder nicht abgeschlossene Job `queued` und jedes nicht abgeschlossene Item
  `pending`. Kein Job bleibt in `downloading` stehen.
* **Job-Zähler** werden in einem einzigen `UPDATE ... RETURNING` neu berechnet
  und zurückgelesen, statt in zwei getrennten Anweisungen.

### Hinzugefügt

* **Atomares Speichern eines Downloads.** Künstler, Release, Aufnahme, Quellen
  und Bibliotheksdatei werden in einer Transaktion geschrieben. Ein Abbruch
  hinterlässt keine Aufnahme ohne Datei und keine Datei ohne Aufnahme.
* **Startup-Retry mit Backoff.** Ist PostgreSQL beim Compose-Start noch nicht
  bereit, wartet das Backend begrenzt (`MUSICDL_DB_STARTUP_TIMEOUT`) statt sofort
  abzustürzen — ohne Endlosschleife.
* **Migrationen laufen unter einem Advisory Lock**, sodass zwei gleichzeitig
  startende Prozesse dieselbe Migration nicht doppelt anwenden.
* **PostgreSQL-Integrationstests** mit einem eigenen Schema pro Test, inklusive
  Tests für gleichzeitige Track-Upserts, gleichzeitige Statuswechsel,
  Zählerkonsistenz und Recovery.
* **Strukturtests des Ogg/Opus-Tag-Writers**: Seitenprüfsummen, Lacing,
  unveränderter `OpusHead`, mehrseitige große Cover, Unicode-Metadaten und
  Lesbarkeit mit `ffprobe`/`ffmpeg` nach dem Schreiben.
* `README.md` und dieses Changelog.

### Behoben

* **yt-dlp wurde mit einer ungültigen Option aufgerufen.** Jede
  Metadatenabfrage übergab `--no-playlist-metafiles`, das es in yt-dlp nicht
  gibt; der Prozess brach mit „no such option“ ab, sodass Suche, Auflösung und
  Download scheiterten. Die Option ist entfernt — `--dump-json` impliziert
  ohnehin `--simulate`. Ein Test prüft die tatsächlich verwendeten
  Argumentvektoren gegen ein installiertes yt-dlp.
* **Gleichzeitige Statuswechsel konnten sich überschreiben.** Job- und
  Item-Updates lesen den aktuellen Zustand jetzt mit `SELECT ... FOR UPDATE`,
  sodass eine Abbruchanforderung nicht mehr von einem parallelen
  Fortschrittsupdate verworfen wird.
* **Gleichzeitiges Speichern derselben Aufnahme** konnte zwei Zeilen erzeugen.
  Der Upsert serialisiert sich jetzt über transaktionsgebundene Advisory Locks
  auf ISRC und Identitätsschlüssel, ergänzt um einen eindeutigen Index auf
  nicht leere ISRCs.
* **Der aufgelöste Job-Name wurde nicht gespeichert.** Ein Job zeigte dauerhaft
  seine Ziel-ID statt des Künstler- oder Releasenamens.
* **Ein wiederaufgenommener Job blieb als `queued` sichtbar**, während seine
  Worker bereits liefen.
* **Eine bereits abgelegte Bibliotheksdatei wurde beim Herunterfahren nicht mehr
  im Katalog vermerkt.** Der abschließende Katalogschreibvorgang läuft jetzt mit
  eigenem, begrenztem Kontext.
* **Ein unlesbares YouTube-Music-Album lieferte still eine leere Trackliste.**
  Jetzt gibt es einen sauberen Providerfehler.
* **Beim Herunterfahren wurden abgebrochene Datenbankabfragen als `ERROR`
  geloggt**, obwohl sie zum normalen Shutdown gehören.
* **Der Sortierschlüssel eines Künstlers** blieb leer, wenn kein Name geliefert
  wurde.

### Entfernt

* SQLite-Treiber (`modernc.org/sqlite`), DSN, PRAGMA-Anweisungen, die auf einen
  einzelnen Writer beschränkte Pool-Konfiguration, der Datenbankdateipfad
  `/data/musicdl.db` und alle SQLite-spezifischen Tests und Dokumentation.

## 0.2.2

Containerisierung mit Alpine und rootless Podman, natives Ogg/Opus-Tagging,
Prozessgruppen-Abbruch für yt-dlp und ffmpeg, SSE-Event-Stream.
