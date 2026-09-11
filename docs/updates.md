# Update Detection & Releases

Starting with **v0.15**, YTMDL provides native update detection within the WebUI. Starting with **v0.16**, YTMDL introduces **`ytmdlctl`**, an official host-side lifecycle and update management CLI for transactional updates, verified database backups, and schema-neutral rollback. Starting with **v0.17**, `ytmdlctl` adds full schema-forward update orchestration (Schema 8 → 9), quiescent pre-migration backups, and explicit crash-boundary recovery (`ytmdlctl recover`).

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
> - **YTMDL Application Containers:** Native multi-platform images published for both **`linux/amd64`** and **`linux/arm64`** (Apple Silicon, ARM64 servers, Raspberry Pi 4/5).
> - **`ytmdlctl` Host CLI:** Provided as standalone native binaries for **`linux/amd64`**, **`linux/arm64`**, **`darwin/amd64`**, and **`darwin/arm64`**.
> - **Windows:** Windows is explicitly not supported.

On macOS, `ytmdlctl` runs natively on Darwin (`darwin/arm64` or `darwin/amd64`) and controls a rootless Podman Machine or Docker daemon through the standard CLI.

## How Update Detection Works

1. **GitHub Releases API:** The YTMDL backend queries the official GitHub API on a background schedule (default: once per hour) — `GET /repos/Der-Felix/ytmdl/releases/latest` on the stable channel, the recent release list on the development channel.
2. **SemVer Comparison:** The installed version is compared with the newest release of the chosen [update channel](#update-channels) using Semantic Versioning 2.0.0, prereleases included: `0.27.2-rc.1` < `0.27.2-rc.2` < `0.27.2-rc.10` < `0.27.2`. Drafts are never offered.
3. **WebUI Notifications:** **Settings → System & Updates** shows the channel, the installed version, the available target version (prereleases are marked as such), the release notes and the exact host commands. A failed check says so — a network or GitHub problem is never shown as "up to date".

The check is read-only. It never installs, restarts or downgrades anything; installing is always done with `ytmdlctl` on the host.

## Update Channels

| Channel | Offers | Source |
|---|---|---|
| **Stable** (default) | Regular, published releases only — never drafts or prereleases | `main`; images also tagged `latest` |
| **Development** | Explicitly published, qualified prereleases (`vX.Y.Z-rc.N`) | `dev`; never tagged `latest` |

Existing and new installations follow **Stable**. An administrator switches the channel deliberately in **Settings → System & Updates**; the choice is stored in the installation's settings and takes effect immediately, without an update or a container restart.

Every installable release has a unique version, its source commit, immutable multi-arch image digests, `SHA256SUMS` and a `release-manifest.json` that pins them. A moving image tag is never the basis of an update. Release candidates use manifest version 4, which adds the source commit and the channel; they can only be installed with the `ytmdlctl` binary of the same release.

### Which channel `ytmdlctl` uses

1. `--channel stable|development` on the command line — for that invocation only;
2. otherwise the channel chosen in the UI (read from the installation's database);
3. otherwise **Stable**.

`ytmdlctl` never changes the stored channel, and it prints a note when `--channel` differs from it.

```sh
ytmdlctl check                                     # newest release of the installation's channel
ytmdlctl check --channel development               # newest qualified prerelease
ytmdlctl update --channel development --target 0.27.2-rc.1 --dry-run
ytmdlctl update --channel development --target 0.27.2-rc.1
```

`--target` installs exactly that version. A prerelease target requires the development channel.

### Installing a release candidate

Use the `ytmdlctl` binary of the release candidate itself:

```sh
VERSION="0.27.2-rc.1"
ARCH="linux-amd64"   # or linux-arm64, darwin-amd64, darwin-arm64
curl -LO "https://github.com/Der-Felix/ytmdl/releases/download/v${VERSION}/ytmdlctl-${ARCH}"
curl -LO "https://github.com/Der-Felix/ytmdl/releases/download/v${VERSION}/SHA256SUMS"
sha256sum --ignore-missing -c SHA256SUMS      # macOS: shasum -a 256 --ignore-missing -c SHA256SUMS
chmod +x "ytmdlctl-${ARCH}"
./ytmdlctl-${ARCH} --project-dir /path/to/ytmdl update --channel development --target "${VERSION}" --dry-run
./ytmdlctl-${ARCH} --project-dir /path/to/ytmdl update --channel development --target "${VERSION}"
```

Backup, Storage Guard, manifest and digest verification, the update transaction and the local `compose.ghcr.override.yaml` apply exactly as for stable releases.

### Returning to Stable

Switching from Development back to Stable never downgrades anything. If the installed prerelease is newer than the latest stable release, the UI and `ytmdlctl check` report that and offer no update; the prerelease keeps running until a newer stable release exists (for example `0.27.2` after `0.27.2-rc.1`).

To return to an older release explicitly, use the verified paths only:

- `ytmdlctl rollback` — directly after an update, while the database schema is unchanged and the previous images are still present;
- `ytmdlctl recover status` and `ytmdlctl recover restore` — restore from the pre-update backup.

### A green CI run is not live throughput

A published release candidate has passed CI, the release qualification and the artifact verification. That shows the code and the artifacts are consistent — it does not show acquisition throughput against real providers. Throughput claims need a separately authorized operating run that counts new verified acquisitions, provider requests, rate limits, cooldown time and failure reasons per hour (see the `throughput summary` log line).

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
   VERSION="0.27.0"
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

## Schema-Forward Updates & Disaster Recovery (v0.17+)

Version 0.17 introduces **Schema 9**, which transitions YTMDL to true canonical artist identities (`artists.id` UUIDv4) with lossless multi-provider provenance tracking (`artist_sources`).

### The Quiescent Update Model

Upgrading across database schema boundaries requires strict lifecycle orchestration:
1. **Drain & Quiesce:** Background queue jobs finish or drain, and the old backend service is stopped. This guarantees zero in-flight database mutations while the pre-migration backup is taken.
2. **Quiescent Pre-Migration Backup:** A verified PostgreSQL custom archive (`pg_dump -Fc`) of Schema 8 is captured and structurally verified with `pg_restore --list`.
3. **Migration Boundary:** The new v0.17 container is started to execute migration `0009_artist_sources.sql`.
4. **Core Safety Invariant:** If an unrecoverable failure occurs *prior* to migration, `ytmdlctl` automatically rolls back to the previous Schema 8 application. If a failure occurs *after* migration has begun or completed, automatic rollback is strictly prohibited to prevent Schema 8 backends from running against Schema 9 tables. Instead, the system halts safely in **`RECOVERY_REQUIRED`**.

### The `ytmdlctl recover` Suite

When a deployment enters `RECOVERY_REQUIRED`, administrators have two deterministic, guided paths:

#### 1. Inspect Recovery Status
```sh
ytmdlctl recover status
```
Displays current transaction state, active database schema, failure cause, and the path to the verified pre-migration backup.

#### 2. Option A: Resume / Forward Recovery (`recover resume`)
If the failure was due to a transient issue (e.g. temporary network blip, container startup timeout, or host resource contention):
```sh
ytmdlctl recover resume
```
Re-evaluates health checks and completes cutover to the target version without repeating database migrations.

#### 3. Option B: Safe Restore & Rollback (`recover restore`)
If the new release cannot be run and you must roll back to Schema 8:
```sh
ytmdlctl recover restore
```
- Restores the verified pre-migration backup into an isolated temporary database (`<db>_restore_tmp`).
- Verifies structural integrity of the restored temporary database.
- Atomically swaps `<db>_restore_tmp` to the primary database name.
- Reverts `.env` and restarts the previous Schema 8 backend.
- Old Schema 8 containers are **NEVER** started until the database is verifiably restored to Schema 8.

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
  1. Inspect the state:
     ```sh
     ytmdlctl recover status
     ```
  2. To attempt completing the update to the new version:
     ```sh
     ytmdlctl recover resume
     ```
  3. To safely restore the pre-migration backup and revert to the previous version:
     ```sh
     ytmdlctl recover restore
     ```
