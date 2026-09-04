# Update Detection & Releases

Starting with **v0.15**, YTMDL provides native update detection within the WebUI. Starting with **v0.16**, YTMDL introduces **`ytmdlctl`**, an official host-side lifecycle and update management CLI for transactional updates, verified database backups, and schema-neutral rollback.

![YTMDL System & Updates](/screenshots/updates.webp)

## Stable Release Distribution

YTMDL uses a two-tier repository architecture:
- **Canonical Development Repository:** Internal builds, feature branches, and full development history.
- **Public GitHub ([Der-Felix/ytmdl](https://github.com/Der-Felix/ytmdl)):** Public stable releases, issue tracking, official documentation, and container distribution.

Public container images are published to the GitHub Container Registry (GHCR):
- `ghcr.io/der-felix/ytmdl-backend:<version>`
- `ghcr.io/der-felix/ytmdl-frontend:<version>`

### Platform & Architecture Support

> [!IMPORTANT]
> **Container vs. CLI Architecture:**
> - **YTMDL Application Containers:** Built and tested for **`linux/amd64`**.
> - **`ytmdlctl` Host CLI:** Provided as standalone native binaries for **`linux/amd64`**, **`linux/arm64`**, **`darwin/amd64`**, and **`darwin/arm64`**.
> - **Windows:** Windows is explicitly not supported.

On macOS, `ytmdlctl` runs natively on Darwin (`darwin/arm64` or `darwin/amd64`) and controls a rootless Podman Machine or Docker daemon through the standard CLI.

## How Update Detection Works

1. **GitHub Releases API:** The YTMDL backend queries the official GitHub API (`GET /repos/Der-Felix/ytmdl/releases/latest`) on a background schedule (default: once per hour).
2. **SemVer Comparison:** The installed version (e.g. `0.15.0`) is compared against the latest stable release tag using strict Semantic Versioning. Pre-releases and drafts are filtered out.
3. **WebUI Notifications:** When a newer stable version is available, the WebUI displays an informational banner and details in **Settings → System & Updates**, including release notes, a direct command to execute on the host, and links to the release and documentation.

## Privacy & Network Transparency

- **No Telemetry:** YTMDL does not transmit library sizes, usernames, music data, or host metrics to GitHub. The update check is a plain anonymous HTTP GET request to the public GitHub Releases endpoint.
- **Can Be Disabled:** Administrators can completely disable outbound update checks by configuring:
  ```sh
  MUSICDL_UPDATE_CHECKS_ENABLED=false
  ```
  When disabled, no external network requests are made.

---

## Installing `ytmdlctl`

`ytmdlctl` is distributed as standalone, statically compiled binaries attached to every official GitHub Release. No package manager or external runtime dependencies (such as Python or Node.js) are required.

> [!TIP]
> **Security Best Practice:** Always verify release checksums against `SHA256SUMS` before running the binary. We do not recommend piping remote shell scripts directly into `bash`.

### Installation Steps

1. **Download the binary and checksums for your platform:**
   ```sh
   # Example for Linux (x86_64 / amd64):
   VERSION="0.16.0"
   curl -LO "https://github.com/Der-Felix/ytmdl/releases/download/v${VERSION}/ytmdlctl-linux-amd64"
   curl -LO "https://github.com/Der-Felix/ytmdl/releases/download/v${VERSION}/SHA256SUMS"
   ```

2. **Verify SHA256 checksum:**
   ```sh
   sha256sum --ignore-missing -c SHA256SUMS
   # Expected output: ytmdlctl-linux-amd64: OK
   ```

3. **Install to system PATH:**
   ```sh
   chmod +x ytmdlctl-linux-amd64
   sudo mv ytmdlctl-linux-amd64 /usr/local/bin/ytmdlctl
   ```

4. **Verify installation:**
   ```sh
   ytmdlctl version
   ```

---

## Managed Updates with `ytmdlctl`

`ytmdlctl` executes transactional, verified updates designed to prevent service disruption, corrupted states, and data loss.

### 1. Check for Releases
Inspect available official releases and your local deployment status:
```sh
ytmdlctl check
```

### 2. Run Preflight Dry-Run
Before applying changes, perform a full read-only safety check:
```sh
ytmdlctl update --dry-run
```
The dry-run verifies:
- Compose file and engine compatibility
- Container health and current application version
- Database schema compatibility and rollback classification
- Storage Guard filesystem token validity
- Active download queue state
- Release manifest schema and cryptographic image digest availability

### 3. Apply Update
When ready, execute the update:
```sh
ytmdlctl update
```

#### What Happens During Update:
1. **Preflight & Lock:** Acquires an exclusive host lock (`.ytmdl/update.lock`) and verifies ambient configuration.
2. **Runtime Snapshot:** Captures running container image IDs and digest sets for exact rollback reference.
3. **Target Image Staging:** Pre-pulls target images and strictly verifies immutable digests against `release-manifest.json`.
4. **Transactional Database Backup:** Dumps PostgreSQL using custom-format `pg_dump -Fc` and verifies it with `pg_restore --list`.
5. **Durable State Persistence:** Persists transaction state (`PREPARED` $\rightarrow$ `MUTATING`) to `.ytmdl/update-state.json` before touching configuration.
6. **Surgical Config Update:** Updates `YTMDL_VERSION` in `.env` atomically while preserving all comments and custom formatting.
7. **Service Cutover:** Recreates the backend, waits for health readiness, verifies database schema, verifies Storage Guard, then cutovers frontend.
8. **Final State:** Sets transaction status to `SUCCESS`. The pre-update database backup is retained in `backups/`.

---

## Database Backups

You can create a standalone, verified PostgreSQL database backup at any time without initiating an update:

```sh
ytmdlctl backup
```

- **Format:** PostgreSQL custom archive format (`pg_dump -Fc`).
- **Validation:** Every backup is automatically verified using `pg_restore --list` to guarantee structural integrity.
- **Location:** Saved under `backups/backup_v<version>_<timestamp>.dump`.
- **Independence:** Does not require access to GitHub or active storage mounts.

---

## Rollback & Safety Invariants

If an update is interrupted or an issue occurs with the new version, you can revert to the previous working state:

```sh
ytmdlctl rollback
```

### Schema-Neutral Rollback Policy
- **Automatic & Safe:** Rollback is supported when the target release is classified as `schema_neutral` and the database schema has not drifted (`schema == schemaBefore`).
- **Schema Drift Protection:** If the database schema was modified or cannot be safely determined, `ytmdlctl` transitions to the **`RECOVERY_REQUIRED`** state and **refuses automatic database restoration**. This prevents silent data loss or overwriting user data.
- **Preserved Backups:** The pre-update backup remains safely stored in `backups/` for manual administrative inspection.

---

## Troubleshooting

### Incompatible Podman Compose Provider
- **Symptom:** `preflight engine compatibility check failed: detected python 'podman-compose' CLI ...`
- **Cause:** Podman delegates `podman compose` calls to an external Compose provider. The legacy Python `podman-compose` implementation does not support Compose V2 volume flags or user namespace mapping (`keep-id`).
- **Resolution:** Configure Podman to use a Compose V2 compatible provider:
  ```sh
  # Install docker-compose (v2) or configure podman compose provider
  # Verify that `podman compose version` reports Docker Compose v2 compatibility
  ```

### Ambiguous Compose File or Engine
- **Symptom:** `ambiguous compose files found` or `ambiguous engine selection`
- **Resolution:** Explicitly specify the compose file and engine, or save your preferred configuration:
  ```sh
  ytmdlctl status --file compose.ghcr.yaml --engine podman --save
  ```

### Storage Guard Verification Failed
- **Symptom:** `Storage Guard check failed: .ytmdl-storage-id not found`
- **Cause:** The media volume is unmounted, offline, or corrupted.
- **Resolution:** Ensure the external filesystem or network share is mounted and that `.ytmdl-storage-id` matches `YTMDL_STORAGE_GUARD_ID` in `.env`. See [Storage Troubleshooting](/storage/troubleshooting).

### System in RECOVERY_REQUIRED State
- **Symptom:** `system is in RECOVERY_REQUIRED state`
- **Cause:** An update failed after database mutations occurred or schema could not be verified.
- **Resolution:**
  1. Check `.ytmdl/update-state.json` for details on the error and the backup path.
  2. Inspect the verified backup in `backups/`.
  3. If needed, manually restore the database using `pg_restore`:
     ```sh
     podman compose -f compose.ghcr.yaml exec -T db pg_restore -U ytmdl -d ytmdl --clean < backups/backup_v...dump
     ```
  4. Once resolved, clear the state file (`.ytmdl/update-state.json`) or reset state to resume normal operation.
