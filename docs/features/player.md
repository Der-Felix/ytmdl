# Integrated Web Player

YTMDL features a persistent, in-browser HTML5 audio player designed for seamless music listening directly from your library without external desktop applications.

![YTMDL Web Player](/screenshots/player.webp)

## Playback & Streaming Architecture

- **Persistent Playback:** The player context remains persistent throughout in-app navigation and browser history traversal (Back/Forward). State updates are isolated so audio progress ticks do not trigger re-renders of the application view.
- **HTTP Range Streaming:** The backend audio streaming endpoint natively supports HTTP 206 Partial Content and byte-range requests (`Accept-Ranges: bytes`), enabling instant seeking across large audio files without waiting for the full file to buffer.
- **Native Audio Delivery:** Stored audio files are streamed directly in their native storage container and encoding format. No automatic server-side audio transcoding is performed or introduced in v0.22.0. Playback compatibility depends on browser-native container and codec support (such as Opus in Ogg/WebM, AAC in M4A/MP4, MP3, and FLAC).
- **Media Session Integration:** Integrates with the browser Media Session API for native operating system controls, lock screen metadata, and hardware media keys.

## Playback Queue Management

Starting with **v0.25**, the web player provides a first-class interactive playback queue drawer and control surface.

> [!IMPORTANT]
> **Playback Queue vs. Playlists vs. Download Queue:**
> - **Playback Queue:** A temporary, in-memory queue that governs your current listening session. It does not persist across browser reloads or sync between devices.
> - **Playlists:** Permanent, database-backed user collections created in the Playlists section.
> - **Download Queue:** Asynchronous background job queue managing download and metadata acquisition tasks.

### Queue Features & Operations

- **Queue Drawer Panel:** Click the queue icon in the MiniPlayer or open the Queue tab in Now Playing (`/player?tab=queue`) to inspect the current queue.
- **Direct Track Selection:** Clicking any item in the queue immediately begins playback of that track while maintaining proper queue progression.
- **Reordering:** Tracks can be rearranged using HTML5 drag-and-drop or via accessible Move Up and Move Down buttons on desktop, tablet, and mobile.
- **"Als Nächstes abspielen" (Play Next):** Inserts a track immediately after the currently playing item. If the track is already present in the queue, it is repositioned to play next without creating duplicates.
- **"Zur Queue hinzufügen" (Add to Queue):** Appends a track to the end of the temporary queue. Duplicate occurrences are fully supported and safely tracked.
- **"Nächste leeren" (Clear Upcoming):** Prunes upcoming tracks after the current item while preserving already-played tracks and the currently playing item. This ensures that navigation backwards ("Previous") remains fully functional.
- **"Leeren" (Clear Queue):** Empties the entire queue and stops playback cleanly.

## Queue & Library Integration

- **Library & Multi-Disc Playback:** Plays tracks, albums, or disc releases directly from the library interface. Multi-disc albums respect canonical disc and track sequencing.
- **Consistent Queue Actions:** "Als Nächstes abspielen" and "Zur Queue hinzufügen" actions are available across Library tables, Search results, Album pages, Playlists, and Favorites.
- **Shuffle & Repeat:** Supports shuffle mode and three-way repeat cycling (off, repeat all, repeat single track). Queue mutations while shuffled maintain deterministic mapping to original queue ordering.
- **Responsive Controls:** Desktop control bar with timeline scrubbing, volume slider, and album artwork, transitioning to a compact responsive mini-player on mobile and tablet viewport widths.

## Keyboard Shortcuts

Global player shortcuts are available during active browsing and automatically isolated whenever focus is within an input field, search bar, or modal dialog:

| Shortcut | Action |
| --- | --- |
| `Space` | Play / Pause toggle |
| `ArrowLeft` / `ArrowRight` | Seek backward / forward 5 seconds |
| `ArrowUp` / `ArrowDown` | Adjust volume up / down |
| `M` | Mute / Unmute toggle |
| `N` | Next track |
| `P` | Previous track |
| `S` | Toggle shuffle |
| `R` | Cycle repeat mode |
