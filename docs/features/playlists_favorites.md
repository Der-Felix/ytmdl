# Playlists & Favorites

YTMDL provides user-managed playlists and track favorites, fully integrated with the persistent web player, library management, and multi-user authentication.

## Features

### User Playlists
- **Custom Playlists:** Create, rename, and manage custom playlists directly from the web interface.
- **Track Management:** Add tracks from anywhere in the library (track lists, album views, artist views) to any existing playlist or create a new playlist on the fly.
- **Deterministic Ordering:** Tracks within a playlist maintain an exact, 1-based contiguous sequence (`1..N`). Tracks can be reordered seamlessly.
- **Duplicate Prevention:** The database enforces uniqueness per playlist and track (`UNIQUE(playlist_id, track_id)`), preventing accidental duplicate entries.

### Track Favorites
- **One-Click Favoriting:** Quick-action heart toggle on every track row and player view to mark tracks as favorites.
- **Dedicated Favorites View:** Dedicated navigation item and library view displaying all favorited tracks with instant playback and management.
- **Dynamic Updates:** Adding or removing favorites immediately synchronizes across the user interface without page reloads.

## Architecture & Data Integrity

### Strict Per-User Privacy
Playlists and favorites are scoped strictly to the authenticated user (`user_id` foreign key referencing `users(id)`). 
- Users cannot view, modify, or list playlists or favorites belonging to other accounts.
- API endpoints strictly filter and enforce user ownership at the database query level.

### Ordering Invariants & Cascade Compaction
Playlists maintain a strict invariant: positions are contiguous 1-based integers `1, 2, ..., N` without gaps or duplicates.

- **Manual Removal:** Removing a track from a playlist compacts subsequent track positions.
- **Cascade Deletion:** When a track is deleted from the music catalog or library, database foreign keys (`ON DELETE CASCADE`) remove the corresponding entries from `playlist_tracks` and `favorite_tracks`.
- **Atomic Compaction Trigger:** An automated PostgreSQL statement-level trigger (`compact_playlist_tracks_after_delete`) immediately re-sequences affected playlist positions in a single atomic statement using `ROW_NUMBER()`, guaranteeing zero gaps even after bulk or cascade deletions.

### Snapshot Playback Semantics
Playing a playlist or your favorites collection enqueues the tracks into the persistent web player:
- **Queue Separation:** The playback queue is an independent in-memory snapshot. Reordering, adding, or clearing tracks in the active playback queue never modifies the underlying stored playlist.
- **Playback Controls:** Supports standard queue features including repeat, shuffle, and next/previous navigation across playlist tracks.

## Operator & Upgrade Notes

### Database Migration: Schema 11 → Schema 12
YTMDL v0.23.0 introduces database schema version 12 (`0012_playlists_favorites.sql`), adding:
- `playlists` table
- `playlist_tracks` table (with position ordering and unique constraints)
- `favorite_tracks` table
- Statement trigger `trg_compact_playlist_tracks_after_delete`

Migrations execute automatically on startup or via `ytmdlctl`:
```sh
ytmdlctl update
```

> [!IMPORTANT]
> **Pre-Migration Backup:** Always take a database backup prior to performing major schema migrations:
> ```sh
> ytmdlctl backup
> ```
> Direct schema-neutral rollback from Schema 12 to Schema 11 is unsupported; restoring from a pre-migration backup is required if rolling back to v0.22.0.
