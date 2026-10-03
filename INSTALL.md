# Install YTMDL 1.2.0

Official images are published in GHCR for Linux amd64 and arm64. Docker Engine
with Compose v2 or newer, or rootless Podman with Docker Compose v2 or newer as its provider, and Python 3 are
required. PostgreSQL 18 runs inside the stack; do not expose its port.

Download `ytmdl-1.2.0.tar.gz`, your `ytmdlctl-<os>-<arch>` binary, and
`SHA256SUMS` from the [release](https://github.com/Der-Felix/ytmdl/releases/tag/v1.2.0).
Verify the downloads before running them:

```sh
sha256sum --ignore-missing -c SHA256SUMS
# macOS: shasum -a 256 --ignore-missing -c SHA256SUMS
tar -xzf ytmdl-1.2.0.tar.gz
cd ytmdl-1.2.0
python3 scripts/install.py --engine docker
# Or: python3 scripts/install.py --engine podman
```

The installer requires a fresh directory. It creates a private `.env` with a
random database password, prepares new `data` and `music` directories for backend
UID 10001, verifies write access, and configures a storage identity marker.
It never recursively changes permissions or overwrites existing configuration.
Use `--prepare-only` to configure before starting, or `--port 8081` to change
the default port. Keep `.env` private and include it in protected backups.

Wait for all three containers to become healthy, then open
`http://localhost:8080` and create the administrator account. Before exposing
the service publicly, configure HTTPS at your trusted reverse proxy.

Manual Compose commands must select the engine overlay:

```sh
docker compose -f compose.ghcr.yaml ps
podman compose -f compose.ghcr.yaml -f compose.ghcr.podman.yaml ps
```

The host CLI selects the Podman overlay automatically. Optional host settings go
in `compose.ghcr.override.yaml`, which is always applied last. A pinned image in
that override must be removed or updated before a managed update can proceed.

## Existing installations

Do **not** run the fresh installer or replace `.env`. Preserve your music/data
mounts, cookies, database volume and local Compose overrides. Verify the new
CLI checksum and make it executable. Download the two public Compose files
into the existing project, retaining the database volume name. Then:

```sh
./ytmdlctl-linux-amd64 --project-dir /path/to/ytmdl --engine docker update --channel stable --target 1.2.0 --dry-run
./ytmdlctl-linux-amd64 --project-dir /path/to/ytmdl --engine docker update --channel stable --target 1.2.0
```

Substitute the host platform and `--engine podman` as appropriate. This release
uses database schema 18 and adds migrations for older supported schemas. Verify a
backup first; a full schema rollback requires restoring it. A v0.28.0 CLI with disabled Storage Identity Guard cannot
perform this upgrade; use the verified v1 CLI. A missing or mismatched configured
guard remains a blocker. Never disable guard verification to hide a bad mount.

The release gate tests upgrades from v0.28.1, v1.0.0 and v1.1.1 with both engines and both guard
modes. Older installations should first follow their version's upgrade notes;
they are not covered by that v1 qualification.

For an existing Linux Docker bind mount, ensure the service user 10001 can write
the selected directories. For rootless Podman, the overlay maps that service
user to the container owner. NAS permissions must be configured on the actual
mounted share; see the public storage guide before changing ownership.

Before updating, take and verify a backup with `ytmdlctl backup --help`. Database
backups do not replace a backup of music files, `.env`, cookies and Compose
overrides. Recovery restore uses `ytmdlctl recover restore --help`; retain the previous version
and a verified backup until the upgraded installation is healthy.

## Support boundary

Local Linux storage and host-mounted SMB/CIFS are supported. NFS, native mobile
clients and full Safari/iOS playback are not qualified for v1. Combined-stream
audio fallback remains off by default. Search/download availability depends on
provider access; configure the container proxy if your network requires it.
Provider challenges and restrictions are surfaced rather than bypassed.
