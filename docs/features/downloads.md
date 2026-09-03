# Download Automation & Queue

The YTMDL download engine coordinates background worker pools to process individual tracks, complete albums, and full discographies without starving shorter jobs.

## Fair Queue Scheduling

- **Fair Division:** Jobs with hundreds of tracks are processed in balanced round-robin chunks, ensuring a single massive discography cannot monopolize the queue.
- **Opus Stream Extraction:** `yt-dlp` extracts native Opus streams (`audio/opus` inside WebM containers) and remuxes them stream-copy into `.opus` (Ogg) containers without generational audio transcoding.
- **Verification:** Every downloaded file is probed via `ffprobe` to verify audio stream validity, channels, and sample rates before promotion to the library.
- **Retry & Circuit Breaker:** Transient network or rate-limiting errors trigger exponential backoff.
