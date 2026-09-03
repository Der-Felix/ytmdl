# SMB/CIFS Network Storage Guide for YTMDL

> **Status: SUPPORTED** (Validated on Linux Host Mount with SMB 3.1.1, UID/GID 10001, and Storage Identity Guard)

This guide describes how to reliably mount an SMB/CIFS network share (e.g. from Synology, QNAP, TrueNAS, Unraid, or Windows Server) on the Linux host for YTMDL v0.11.0+.

---

## 1. Architecture & Support Scope

```text
[ NAS / SMB Server ]
        │ (SMB 3.1.1 Network Protocol)
        ▼
[ Linux Host Mount: /srv/music ]
        │ (Podman volume bind mount: /srv/music:/music:rslave)
        ▼
[ YTMDL Container: /music ]  (UID: 10001, GID: 10001)
```

### Supported Deployment Scope:
SMB/CIFS is **supported** when mounted directly on the Linux host operating system and provided to the YTMDL container as `/music`.

**Validated Environment:**
- **Host OS**: Linux (Debian, Ubuntu, Fedora CoreOS, RHEL)
- **Client**: Linux Kernel CIFS client (`cifs-utils`, `mount.cifs`)
- **Protocol**: SMB 3.1.1
- **Mount Point**: Host-mounted filesystem mapped to UID `10001` and GID `10001`
- **Safety**: Storage Identity Guard configured (`.ytmdl-storage-id`)
- **Staging**: Persistent local `/data/staging` inside the persistent container data volume
- **Bind Mount**: Podman volume mount (`-v /srv/music:/music:rslave` or compose bind)

> [!IMPORTANT]
> - YTMDL **never** mounts network filesystems inside the container.
> - The container requires **no** `CAP_SYS_ADMIN` privileges.
> - YTMDL contains **no** SMB credentials; all authentication is isolated to the host OS.

---

## 2. Prerequisites on Host (Debian/Ubuntu/RHEL/Fedora)

Install the CIFS utilities on the host system:

```bash
# Debian / Ubuntu
sudo apt-get update && sudo apt-get install -y cifs-utils

# RHEL / Fedora / CentOS
sudo dnf install -y cifs-utils
```

Ensure the container user UID/GID (`10001:10001`) is permitted to write to the share.

---

## 3. Host Credentials File

Store the SMB credentials securely on the host under `/etc/samba/credentials-ytmdl`:

```bash
sudo mkdir -p /etc/samba
sudo tee /etc/samba/credentials-ytmdl > /dev/null << 'EOF'
username=ytmdl_user
password=your_secure_password
domain=WORKGROUP
EOF

# Set strict permissions (readable only by root)
sudo chmod 0600 /etc/samba/credentials-ytmdl
sudo chown root:root /etc/samba/credentials-ytmdl
```

---

## 4. Host Mount Configuration (`/etc/fstab`)

Add the mount entry to `/etc/fstab` with network dependencies and correct UID mapping:

```text
//192.168.1.100/music /srv/music cifs credentials=/etc/samba/credentials-ytmdl,uid=10001,gid=10001,file_mode=0664,dir_mode=0775,vers=3.1.1,soft,_netdev,nofail,noserverino,mfsymlinks,actimeo=1 0 0
```

### Option Explanations:
- `uid=10001,gid=10001`: Maps file ownership directly to the YTMDL container runtime user (`musicdl`).
- `file_mode=0664,dir_mode=0775`: Ensures YTMDL can write, and media servers (Plex, Jellyfin, Emby) can read.
- `vers=3.1.1`: Modern, secure, and performant SMB protocol version.
- `soft`: On Linux CIFS, allows I/O requests to return an error to YTMDL upon network timeout rather than hanging the kernel thread indefinitely, allowing YTMDL's resilient state machine to transition items to `waiting_for_storage`. *(Note: This applies specifically to CIFS; NFS should use `hard`).*
- `_netdev`: Ensures systemd mounts this only after the network stack is fully online.
- `nofail`: Prevents boot freezes if the NAS is temporarily offline.
- `noserverino`: Avoids inode number collisions across heterogeneous SMB implementations.
- `actimeo=1`: Ensures fast attribute cache invalidation for near-instant detection of modified files.

Create mount point and test:

```bash
sudo mkdir -p /srv/music
sudo mount -a
mountpoint /srv/music
```

---

## 5. Storage Identity Guard Setup

To protect against accidentally writing files onto the local host disk when the NAS is unmounted or disconnected, initialize the **Storage Identity Guard**:

```bash
# Generate a unique UUID for this storage volume
STORAGE_UUID=$(cat /proc/sys/kernel/random/uuid 2>/dev/null || uuidgen)

# Write the marker file to the root of the mounted share
echo "ytmdl-storage:${STORAGE_UUID}" | sudo tee /srv/music/.ytmdl-storage-id > /dev/null
sudo chmod 0644 /srv/music/.ytmdl-storage-id
sudo chown 10001:10001 /srv/music/.ytmdl-storage-id

echo "Configured Storage UUID: ${STORAGE_UUID}"
```

In your YTMDL configuration (`config.yaml` or `.env`):

```yaml
library:
  path: /music
  storage_guard_id: "<PASTE-YOUR-STORAGE_UUID-HERE>"
  min_free_bytes: 1073741824 # 1 GiB safety reserve
```

Or via environment variables:

```bash
MUSICDL_LIBRARY_STORAGE_GUARD_ID="<PASTE-YOUR-STORAGE_UUID-HERE>"
MUSICDL_LIBRARY_MIN_FREE_BYTES="1073741824"
```

---

## 6. Live Mount Propagation (`:rslave`)

When mounting `/srv/music` into the YTMDL container, use `rslave` propagation so that unmounting and remounting the share on the host is dynamically reflected inside the running container without requiring a container restart:

```yaml
# compose.yaml
services:
  backend:
    image: ytmdl-backend:local
    volumes:
      - ./data:/data:Z
      - /srv/music:/music:rslave
```

> [!NOTE]
> `:rslave` enables container propagation only if the host source mount has `shared` propagation.
> To verify host mount propagation:
> ```bash
> findmnt -o TARGET,SOURCE,FSTYPE,PROPAGATION /srv/music
> ```
> If propagation is `private`, enable sharing on the host mount:
> ```bash
> sudo mount --make-shared /srv/music
> ```

---

## 7. Validated Failure Scenarios & Test Results

The SMB/CIFS support in YTMDL v0.11.0 was validated in real-world failure tests on a Linux host with a live SMB 3.1.1 server. All tests passed with **0 bytes data loss** and **0 silent overwrites**:

| Scenario | Tested Real-World Behavior | Result |
| :--- | :--- | :--- |
| **Filesystem Detection** | API reports `fs_type: "CIFS/SMB"` and `is_network_fs: true`. | PASS |
| **Permissions** | UID/GID `10001:10001` creates directories and audio files with `0664`/`0775`. | PASS |
| **Storage Identity Guard** | Status reports `verified`; UUID is checked server-side and never exposed in API. | PASS |
| **Zero-Write Guard** | When share is unmounted, API reports `guard_missing` and writes **0 bytes** to local fallback disk. | PASS |
| **Live Remount Visibility** | Remounting share on host is immediately visible in container via `:rslave` without container restart. | PASS |
| **Normal Download** | yt-dlp downloads to local persistent `/data/staging`, remuxes, tags, and commits to SMB. Staging is cleaned up. | PASS |
| **Foreign Target (`PATH_CONFLICT`)** | Unregistered pre-existing file at target path is detected; download fails with `PATH_CONFLICT` and original file is preserved 100% byte-for-byte. | PASS |
| **TOCTOU No-Replace Race** | Kernel `renameat2(..., RENAME_NOREPLACE)` and `os.O_EXCL` prevent concurrent overwrite. | PASS |
| **Case Collision Safety** | Case-insensitive SMB collisions are caught via `O_EXCL`, preventing silent replacements. | PASS |
| **Storage Drop During Download** | When SMB drops mid-download, yt-dlp finishes staging locally and item safely transitions to `waiting_for_storage`. No retries spent. | PASS |
| **Storage Recovery** | Remounting SMB wakes queue; items transition `waiting_for_storage` → `finalizing` → `completed` without redownloading from internet. | PASS |
| **Storage Drop During Finalize** | Interrupted copy triggers safe rollback; staged file is preserved and finalized upon recovery. | PASS |
| **Target ENOSPC (`waiting_for_space`)** | Low space triggers `waiting_for_space` without retry loss; finalization resumes when space is freed. | PASS |
| **Read-Only Share** | Read-only mount transitions storage to `read_only` and protects files from partial writes. | PASS |
| **Container Health Independence** | During complete share outage, `/api/v1/health?scope=essential` remains healthy, preventing restart loops. | PASS |
| **Sidecars & Cover** | Sidecar lyrics (`.lrc`, `.txt`) and `cover.jpg` are written cleanly alongside audio files. | PASS |

---

## 8. Systemd Mount Unit (Alternative to fstab)

If managing mounts with systemd units, create `/etc/systemd/system/srv-music.mount`:

```ini
[Unit]
Description=YTMDL SMB Music Library Mount
After=network-online.target
Wants=network-online.target

[Mount]
What=//192.168.1.100/music
Where=/srv/music
Type=cifs
Options=credentials=/etc/samba/credentials-ytmdl,uid=10001,gid=10001,file_mode=0664,dir_mode=0775,vers=3.1.1,soft,_netdev,noserverino,mfsymlinks,actimeo=1
TimeoutSec=30

[Install]
WantedBy=multi-user.target
```

Enable and start:

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now srv-music.mount
```
