# Single-image deployment

YTMDL is also published as one image, `ghcr.io/der-felix/ytmdl`. It contains
the Go backend and the built web application; the backend serves both on one
port. PostgreSQL stays a separate container, so a deployment is two containers
instead of three:

```text
Browser
   │  :8080
   ▼
ytmdl        Go backend: web app, REST API, SSE, job queue, workers,
   │         yt-dlp, ffmpeg, ffprobe — one process, no nginx
   ▼
ytmdl-db     PostgreSQL 18
```

The image is available from the release after v1.9.0 on.

## Which variant to use

| | Single image (`compose.all-in-one.yaml`) | Two images (`compose.ghcr.yaml`) |
| --- | --- | --- |
| Images | `ghcr.io/der-felix/ytmdl` | `ytmdl-backend` and `ytmdl-frontend` |
| Meant for | NAS apps (TrueNAS, Unraid, Synology, Portainer) and hosts that want to reference one image | the documented installation with the host CLI |
| Updates | pull and recreate (below) | `ytmdlctl update`: verified backup, digest checks, rollback |
| Install helper | none; Compose file or app YAML | `scripts/install.py` from the release package |

Both run the same backend and the same web application, read the same `.env`,
and use the same database volume, network and paths. An installation can move
from one to the other (see [Moving between the variants](#moving-between-the-variants)).

`ytmdlctl update` manages the two-image layout only. It refuses to run against
any Compose file other than `compose.ghcr.yaml`, and the release manifest it
verifies pins the backend and frontend images, not this one. Choose the two-image
variant if you want managed, transactional updates with automatic rollback.

## Start

The Compose file is [`compose.all-in-one.yaml`](https://github.com/Der-Felix/ytmdl/blob/main/compose.all-in-one.yaml)
in the repository. It needs a `.env` next to it, created from
[`.env.example`](https://github.com/Der-Felix/ytmdl/blob/main/.env.example) with a real
database password in both `POSTGRES_PASSWORD` and `MUSICDL_DATABASE_URL`.

```sh
mkdir -p data music
# The application runs as UID 10001 and must be able to write both.
sudo chown 10001:10001 data music
docker compose -f compose.all-in-one.yaml up -d
docker compose -f compose.all-in-one.yaml ps
```

Wait until both containers are healthy, open `http://<host>:8080` and create the
administrator account. `YTMDL_HOST_PORT` changes the host port.

With rootless Podman, map the service user onto your own user the way
`compose.ghcr.podman.yaml` does for the backend, in an override file:

```yaml
# compose.all-in-one.podman.yaml
services:
  ytmdl:
    userns_mode: "keep-id:uid=10001,gid=10001"
```

```sh
podman compose -f compose.all-in-one.yaml -f compose.all-in-one.podman.yaml up -d
```

## Tags

| Tag | Moves to |
| --- | --- |
| `latest` | the newest stable release |
| `X` (e.g. `1`) | the newest stable release with major version X |
| `X.Y` (e.g. `1.10`) | the newest stable release of that minor line, i.e. its patch releases |
| `X.Y.Z` | never moves; one release |

Release candidates never move `latest`, `X.Y` or `X`. A patch for an older line
moves only that line's `X.Y`. The same tags exist for `ytmdl-backend` and
`ytmdl-frontend`.

`compose.all-in-one.yaml` uses `latest`. Set `YTMDL_IMAGE_TAG` in `.env` to a
line (`1.10`), a major version (`1`) or an exact version to control when a new
release arrives. `YTMDL_VERSION` belongs to the two-image layout and is ignored
here.

## Updating

```sh
docker compose -f compose.all-in-one.yaml pull
docker compose -f compose.all-in-one.yaml up -d
```

`pull_policy: always` makes `up -d` check the registry as well, so recreating
the stack is enough to pick up a new image behind the tag. A NAS app that
redeploys the Compose file does the same.

Before every update:

1. Read the release notes. They name database migrations and anything that
   needs attention.
2. Take a database backup and keep it until the new version runs well:

   ```sh
   docker compose -f compose.all-in-one.yaml exec -T db \
     pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc --no-owner > ytmdl-before-update.dump
   ```

   Back up `music`, `data` and `.env` separately; the dump holds the catalogue
   only.

The database is migrated forward when the new version starts. There is no
automatic way back: an older image does not check whether the schema is newer
than its own and was never tested against one, so starting it on a migrated
database is unsafe. To return to the previous version, restore the backup taken
before the update and start the previous `X.Y.Z` tag. Restore into an empty
database, so that nothing the migration added survives:

```sh
docker compose -f compose.all-in-one.yaml stop ytmdl
docker compose -f compose.all-in-one.yaml exec -T db dropdb -U "$POSTGRES_USER" --force "$POSTGRES_DB"
docker compose -f compose.all-in-one.yaml exec -T db createdb -U "$POSTGRES_USER" "$POSTGRES_DB"
docker compose -f compose.all-in-one.yaml exec -T db \
  pg_restore -U "$POSTGRES_USER" -d "$POSTGRES_DB" --no-owner < ytmdl-before-update.dump
# then set YTMDL_IMAGE_TAG to the previous X.Y.Z and start again
docker compose -f compose.all-in-one.yaml up -d
```

Pin `YTMDL_IMAGE_TAG` if you want to decide when a migration happens.

The update panel under Settings shows the `ytmdlctl` commands of the two-image
layout. With the single image, update as described here instead.

## Behind a reverse proxy

Browsers reach the container directly, so the backend only trusts
`X-Forwarded-For` and `X-Forwarded-Proto` from loopback by default. Behind your
own reverse proxy (Traefik, Caddy, nginx, a NAS proxy), name that proxy in
`MUSICDL_TRUSTED_PROXIES`, as an address or a narrow CIDR:

```sh
MUSICDL_TRUSTED_PROXIES=127.0.0.1/32,::1/128,192.168.1.20
MUSICDL_COOKIE_SECURE=true   # when the proxy serves HTTPS
```

Without it, sign-in rate limits and logs see the proxy's address for every
client. The header is read from the right, so a client cannot choose its address
by sending its own `X-Forwarded-For` through a proxy that appends to it.

Never trust the whole container network (`172.31.250.0/28`). The two-image
layout does that for its nginx container; here, the network's gateway is a
client address, and trusting it would let any client set its own address. If
your `.env` sets `MUSICDL_TRUSTED_PROXIES` to that subnet, remove it.

Each entry is an address or a CIDR, without a port: `192.168.1.20` or
`192.168.1.0/24`, not `192.168.1.20:443`. An entry the app cannot read stops it
at startup.

### Proxy on the same Docker host

When the reverse proxy runs on the same host as ytmdl, for example as another
container that reaches the published port through the host, the app does not
see the proxy's LAN address. It sees the gateway of its own Docker network:
`172.31.250.1`, and with IPv6 enabled on that network also its IPv6 gateway (an
address such as `fdd0:…::1`). Name exactly those gateway addresses, plus every
proxy further out if requests pass through more than one:

```sh
MUSICDL_TRUSTED_PROXIES=127.0.0.1/32,::1/128,172.31.250.1,fdd0:…::1
```

`docker network inspect ytmdl-net` (`<project>-net` with a different
`YTMDL_PROJECT_NAME`) shows the gateway addresses. Trust single addresses, never the subnet.

Every connection that arrives through the published port then comes from a
trusted address, so the published port must not be reachable by clients
directly: bind it to an address only the proxy uses or block it in the host's
firewall. Otherwise a client could send its own `X-Forwarded-For` and choose the
address it appears under. That only affects the sign-in rate limits and the
logged addresses, but it defeats the reason for trusting the proxy. Attaching the
proxy container to ytmdl's network and dropping the published port avoids the
question: the app then sees the proxy container's own address, which you name
instead.

To check the setting, sign in or load a page through the proxy and read the
container log: the `http request` lines show the client's address as
`remote_ip`, and the proxy or gateway as `peer_ip` when the two differ. A
`remote_ip` that is still the gateway or the proxy means an address in the chain
is missing from `MUSICDL_TRUSTED_PROXIES`.

The proxy itself must pass the event stream (`/api/v1/events`) unbuffered and
allow request bodies of 26 MiB for cookie uploads; the bundled nginx
configuration in `frontend/nginx.conf` shows both settings.

## Moving between the variants

Both Compose files use the same project name (`YTMDL_PROJECT_NAME`, default
`ytmdl`), the same `.env`, the database volume `<project>-postgres-data`, the
same network and the same `data` and `music` paths. Moving changes only which
containers run.

From two images to one:

1. Update the two-image installation to the release you will start with the
   single image, or to an older one — never start an older single image on a
   newer database.
2. Take a database backup (above, with `-f compose.ghcr.yaml`).
3. If `.env` sets `MUSICDL_TRUSTED_PROXIES` to the container subnet, remove that
   entry (see above).
4. Stop the old layout without removing volumes, then start the new one:

   ```sh
   docker compose -f compose.ghcr.yaml down          # never down -v
   docker compose -f compose.all-in-one.yaml up -d
   ```

Back to two images: the same steps in reverse. Set `YTMDL_VERSION` to the
version the single image ran (or newer); if `.env` sets
`MUSICDL_TRUSTED_PROXIES`, add the container subnet back, since the frontend's
nginx must be trusted there. Then
`docker compose -f compose.all-in-one.yaml down` and
`docker compose -f compose.ghcr.yaml up -d`. From there, `ytmdlctl update`
manages the installation again.

## TrueNAS custom app

TrueNAS SCALE installs a custom app from Compose YAML (**Apps → Discover Apps →
⋮ → Install via YAML**). Replace every value in angle brackets. Use a database
password of letters and digits only; it is part of a URL.

```yaml
services:
  db:
    image: docker.io/library/postgres:18-alpine
    environment:
      POSTGRES_DB: ytmdl
      POSTGRES_USER: ytmdl
      POSTGRES_PASSWORD: <database-password>
      POSTGRES_INITDB_ARGS: --encoding=UTF8 --locale=C
    volumes:
      - /mnt/<pool>/apps/ytmdl/postgres:/var/lib/postgresql
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U ytmdl -d ytmdl"]
      interval: 10s
      timeout: 5s
      retries: 5
      start_period: 10s
    restart: unless-stopped
    stop_grace_period: 30s

  ytmdl:
    image: ghcr.io/der-felix/ytmdl:latest
    pull_policy: always
    depends_on:
      db:
        condition: service_healthy
    environment:
      MUSICDL_DATABASE_URL: postgres://ytmdl:<database-password>@db:5432/ytmdl?sslmode=disable
      # Optional: the content of /music/.ytmdl-storage-id without the
      # "ytmdl-storage:" prefix. Stops downloads when the share is missing.
      # MUSICDL_STORAGE_GUARD_ID: <storage-id>
      # Behind a reverse proxy: its address or CIDR, without a port. A proxy
      # on this host that reaches the published port arrives from the app
      # network's gateway (IPv4 and, with IPv6 enabled, IPv6); name those
      # gateway addresses and every proxy further out, and keep the port
      # unreachable for clients. Check remote_ip in the access log.
      # MUSICDL_TRUSTED_PROXIES: 127.0.0.1/32,::1/128,<proxy-address>
    volumes:
      - /mnt/<pool>/apps/ytmdl/data:/data
      - /mnt/<pool>/<music-dataset>:/music
    ports:
      - "8080:8080"
    restart: unless-stopped
    stop_grace_period: 40s
```

The `data` and music datasets must be writable by UID/GID 10001 (dataset
permissions or ACL); the PostgreSQL directory is managed by the database
container. The image's own healthcheck reports the app as healthy once the API,
the database connection and the web application answer.

To update, redeploy the app; `pull_policy: always` pulls the image behind
`latest`. Take the database backup first, for example with
`docker exec <db-container> pg_dump -U ytmdl -d ytmdl -Fc --no-owner > ytmdl.dump`
in a TrueNAS shell.

## Differences from the two-image layout

The backend serves the web application with the rules of the frontend image's
nginx configuration: `/assets/*` cached for a year as `immutable` and answered
with 404 when missing; every other path the file it names or the application
shell, with `Cache-Control: no-cache, must-revalidate`; the same security headers
(`X-Frame-Options`, `Content-Security-Policy: frame-ancestors 'self'`,
`X-Content-Type-Options`) on every response, the API's included; gzip for the
same text types. What differs:

- A request body over the API's limits (1 MiB in general, 26 MiB for cookie
  uploads, 9 MiB for artwork) is answered by the API with `400`, where nginx
  answered `413`. A client still sending a large body may see the connection
  closed instead.
- Unknown paths below `/api` keep the API's JSON error. Dotfiles and paths that
  leave the web directory are `404`; nginx would have answered the application
  shell.
- A request body must arrive within the server's read timeout (30 s,
  `YTDM_SERVER_READ_TIMEOUT`). nginx buffered uploads before passing them on; a
  25 MiB cookie file over a slow link may need a longer timeout.
- Static files are compressed when the image is built, not per request.
