# NFS Network Storage Guide for YTMDL

> **Status: EXPERIMENTAL** (Architecture implemented; real host-mounted NFS release verification pending)

This guide describes how to reliably mount an NFS export (NFSv4.2 or NFSv3) on the host for YTMDL v0.11.0+.

---

## 1. Architecture Overview

```text
[ NAS / NFS Server ]
        │ (NFSv4.2 TCP)
        ▼
[ Linux Host Mount: /srv/music ]
        │ (Podman volume bind mount: /srv/music:/music:Z)
        ▼
[ YTMDL Container: /music ]  (UID: 10001, GID: 10001)
```

> [!IMPORTANT]
> YTMDL **never** mounts NFS inside the container. All NFS mounts must be performed on the host operating system. The host directory is passed to the container via volume mount (`-v /srv/music:/music:Z` or compose bind).

---

## 2. NFS Server Export Configuration (NAS)

On the NFS server (e.g. Synology, TrueNAS, Linux server), ensure the export is configured with UID/GID mapping for `10001`:

### Linux /etc/exports Example:
```text
/export/music  192.168.1.0/24(rw,sync,no_subtree_check,all_squash,anonuid=10001,anongid=10001)
```

- `rw`: Read/Write access.
- `sync`: Guarantees writes are committed before responding to I/O requests.
- `all_squash,anonuid=10001,anongid=10001`: Maps all incoming client requests directly to YTMDL's container UID/GID (`10001:10001`).

---

## 3. Host Prerequisites & Mount Options

Install the NFS client utilities on the host:

```bash
sudo apt-get update && sudo apt-get install -y nfs-common
```

### Critical Mount Flag Guidelines:
- **Use `hard`**: If the NFS server temporarily drops, processes wait indefinitely until storage returns.
- **NEVER use `soft`**: `soft` causes the kernel to return silent I/O errors (`EIO`) on timeout, leading to corrupted files and data loss.
- **DO NOT use `intr`**: `intr` is obsolete and completely ignored in Linux kernels 2.6.25+.
- **Use `_netdev,nofail`**: Ensures proper systemd network ordering and prevents host boot freezes if the NAS is temporarily offline.

---

## 4. Host Mount Configuration (`/etc/fstab`)

Add the mount entry to `/etc/fstab`:

```text
192.168.1.100:/export/music /srv/music nfs nfsvers=4.2,hard,timeo=600,retrans=2,rsize=1048576,wsize=1048576,_netdev,nofail 0 0
```

### Option Explanations:
- `nfsvers=4.2`: Modern NFS protocol with compound RPCs, server-side copy support, and better security.
- `hard`: Safe retry policy that prevents corrupted writes during NAS reboots.
- `timeo=600`: 60.0 second timeout before initiating TCP reconnect attempts.
- `retrans=2`: Number of retries before issuing a major timeout warning.
- `rsize=1048576,wsize=1048576`: 1 MiB transfer buffer size for maximum throughput.

Test the mount:

```bash
sudo mkdir -p /srv/music
sudo mount -a
mountpoint /srv/music
```

---

## 5. Storage Identity Guard Setup

Initialize the **Storage Identity Guard** on the mounted NFS share:

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

Or via environment variable:

```bash
MUSICDL_LIBRARY_STORAGE_GUARD_ID="<PASTE-YOUR-STORAGE_UUID-HERE>"
MUSICDL_LIBRARY_MIN_FREE_BYTES="1073741824"
```

---

## 6. Systemd Mount Unit (Alternative to fstab)

Create `/etc/systemd/system/srv-music.mount`:

```ini
[Unit]
Description=YTMDL NFS Music Library Mount
After=network-online.target
Wants=network-online.target

[Mount]
What=192.168.1.100:/export/music
Where=/srv/music
Type=nfs
Options=nfsvers=4.2,hard,timeo=600,retrans=2,rsize=1048576,wsize=1048576,_netdev
TimeoutSec=60

[Install]
WantedBy=multi-user.target
```

Enable and activate:

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now srv-music.mount
```
