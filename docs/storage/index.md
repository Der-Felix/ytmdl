# Storage & Filesystems

YTMDL organizes audio into a permanent music library using an atomic, two-phase staging architecture.

## Two-Phase Atomic Staging

To prevent partially downloaded files, corrupt headers, or broken album directories from polluting your media server:

1. **Phase 1: Scratch Staging (`/data/staging`):** `yt-dlp` extracts audio streams, verifies bitrates, and downloads sidecars strictly within a local scratch directory.
2. **Phase 2: Atomic Promote (`/music`):** Once all tracks in a release pass integrity checks, the files are atomically moved into their permanent destination (`/music/Artist/YYYY - Album/`).

## Storage Identity Guard

When mounting network shares (SMB/CIFS or NFS) into a container, network drops or unmounts can cause the container to write files into the underlying host root filesystem.

To prevent silent local fills when remote shares disconnect:
- A marker file named `.ytmdl-storage-id` is placed at the root of the music share.
- The backend checks for the presence and content of this marker file before executing any write operations.
- If the marker disappears, the queue immediately pauses, writes are rejected, and an alert is displayed in the WebUI.

## Storage Guides

- [SMB / CIFS Storage Setup](/storage/smb) — Recommended configuration for Synology, TrueNAS, and Linux Samba servers.
- [NFS Storage Setup](/storage/nfs) — Configuration for NFSv4.2 exports with Kerberos or UID matching.
- [Storage Troubleshooting](/storage/troubleshooting) — Diagnosing permission denied, locking issues, and CIFS latency.
