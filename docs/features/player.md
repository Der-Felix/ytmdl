# Integrated Web Player

YTMDL features a persistent, in-browser HTML5 audio player designed for seamless music listening directly from your library without external desktop applications.

![YTMDL Web Player](/screenshots/player.webp)

## Playback & Streaming Architecture

- **Persistent Playback:** The player context remains persistent throughout in-app navigation and browser history traversal (Back/Forward). State updates are isolated so audio progress ticks do not trigger re-renders of the application view.
- **HTTP Range Streaming:** The backend audio streaming endpoint natively supports HTTP 206 Partial Content and byte-range requests (`Accept-Ranges: bytes`), enabling instant seeking across large audio files without waiting for the full file to buffer.
- **Native Audio Delivery:** Stored audio files are streamed directly in their native storage container and encoding format. No automatic server-side audio transcoding is performed or introduced in v0.22.0. Playback compatibility depends on browser-native container and codec support (such as Opus in Ogg/WebM, AAC in M4A/MP4, MP3, and FLAC).
- **Media Session Integration:** Integrates with the browser Media Session API for native operating system controls, lock screen metadata, and hardware media keys.

## Queue & Library Integration

- **Library & Multi-Disc Playback:** Plays tracks, albums, or disc releases directly from the library interface. Multi-disc albums respect canonical disc and track sequencing.
- **Playback Queue:** Maintains an independent in-memory playback queue that remains completely separate from the background downloader/job queue.
- **Shuffle & Repeat:** Supports shuffle mode and three-way repeat cycling (off, repeat all, repeat single track).
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
