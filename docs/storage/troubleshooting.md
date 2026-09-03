# Storage & Download Reliability Troubleshooting Guide

This guide describes how to diagnose storage issues, understand YTMDL's resilient storage state machine, and resolve operational problems.

---

## 1. Storage Status Codes & Diagnostics

YTMDL v0.11.0 monitors both the destination music library and the local persistent staging directory. You can inspect live diagnostics via `GET /api/v1/storage/status` (Admin only) or the Settings/Storage UI in the frontend.

| Status Code | Meaning | Root Cause | Resolution |
| :--- | :--- | :--- | :--- |
| `healthy` | Storage is verified and writable | Mount is online, identity marker matches, free space is above limit. | No action required. |
| `guard_missing` | Marker file `.ytmdl-storage-id` is absent | The network share is unmounted (pointing to an empty host directory) or marker was deleted. | Verify host mount with `mountpoint /srv/music`. Ensure `.ytmdl-storage-id` is present on the NAS root. |
| `guard_mismatch` | Marker file UUID does not match config | A different volume is mounted, or `storage_guard_id` was misconfigured. | Update `storage_guard_id` in config to match the UUID in `.ytmdl-storage-id`. |
| `read_only` | Share cannot be written to | NFS export is read-only, CIFS credentials lack write permissions, or host folder ownership is wrong. | Check permissions on NAS/host. Ensure UID/GID `10001` has write access. |
| `low_space` | Available space is below `min_free_bytes` | Storage volume is nearly full. | Free up disk space or adjust `min_free_bytes` safety threshold. |
| `unavailable` | Path does not exist or statfs fails | Host mount point is missing or NAS is offline. | Check NAS network connectivity and remount on host. |

---

## 2. Storage Outage Behavior & Safe States

When a network storage outage occurs during unattended 24/7 operation:

1. **Zero Data Loss & No False Fails**:
   - YTMDL does **not** fail the job or spend retry attempts when storage drops.
   - Active download items waiting for finalization transition to `waiting_for_storage` or `waiting_for_space`.
   - Completed audio tracks remain fully preserved in `/data/staging/<item-id>/`.
2. **Automatic Background Recovery**:
   - The background storage monitor probes the storage guard every 30 seconds.
   - When the NAS comes back online and passes guard verification, YTMDL wakes the queue and automatically commits all staged tracks.
3. **Container Health Independence**:
   - The Podman container health check remains **healthy** during storage outages. This prevents Podman restart loops from killing active staging downloads.

---

## 3. Persistent Staging & Resume

- Local staging is stored under `/data/staging/<item-id>/`.
- yt-dlp partial files (`source.*.part`) are preserved across backend restarts.
- Corrupted audio files (detected by duration or ffprobe verification) are automatically cleaned and re-downloaded up to 2 times.
- Staged artifacts are cleaned up **only after** the database transaction commits.

---

## 4. Idempotent Finalization (Crash Recovery)

If the server crashes or loses power after writing a track to `/music/Artist/Album/01 - Song.opus` but before the database records it:

- On restart, YTMDL re-verifies the target file's SHA-256 checksum and file size against `staged_sha256`.
- If the checksum matches, YTMDL recognizes the file was already successfully placed, skips copying, and updates the database without triggering a `PATH_CONFLICT`.
- If a foreign, different file exists at that path, YTMDL strictly refuses to overwrite it and reports `PATH_CONFLICT`.

---

## 5. Storage Identity Guard Verification Command

To manually test guard verification on the host:

```bash
# Check marker content
cat /srv/music/.ytmdl-storage-id

# Expected format:
# ytmdl-storage:12345678-1234-1234-1234-123456789abc

# Test write probe as UID 10001
sudo -u '#10001' touch /srv/music/.ytmdl-health-test && sudo rm -f /srv/music/.ytmdl-health-test
```

---

## 6. Library Reconciliation & Legacy File Audit Note

When comparing the total count of physical audio files on disk with the database `files` table:
- Historical development versions prior to v0.9 used unstandardized folder naming (such as `[Single]` folder tags or single-artist folder names instead of collaborative album artists).
- In modernized versions (v0.10.0+), tracks are catalogued and placed strictly according to the Plex/Jellyfin/Emby compatible path standard (e.g. `Album Artist/Year - Title/Track - Title.opus`).
- Legacy files remaining in older directory locations are treated as unmanaged files. They represent **no data corruption** and are **never automatically deleted or overwritten** by YTMDL.
- Operators can identify and manage unreferenced files via the Library Scan API (`GET /api/v1/library/scan`) and safely remove specific orphan issues via `DELETE /api/v1/library/scan/issues/{id}` if desired.

