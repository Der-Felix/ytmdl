# Deployment mit Podman

## Zielarchitektur

```text
Browser
   │
   ▼
ytmdl-frontend        Nginx Alpine
   │  /       → statisches Webfrontend
   │  /api/*  → backend:8080
   ▼
ytmdl-backend         Go-Backend, REST API, SSE, Job Queue, Worker,
   │                  yt-dlp, ffmpeg, ffprobe
   ▼
ytmdl-db              PostgreSQL 18 (postgres:18-alpine)
```

Alle drei Container liegen im dedizierten Bridge-Netz `ytmdl-net` und sprechen
sich ausschließlich über ihre Compose-Service-Namen (`db`, `backend`, `frontend`)
an — nie über IP-Adressen. Nach außen ist ausschließlich Port 8080 am Frontend
veröffentlicht.

Der Worker ist Teil desselben Go-Prozesses wie die API; es gibt bewusst keinen
eigenen Worker-Container und keine zusätzliche Queue-Infrastruktur.

Das Backend-Image besteht aus einem Go-Builder auf `golang:alpine` und einem
separaten Alpine-Runtime-Image. Die Runtime enthält nur die statische
Backend-Binary sowie `yt-dlp`, `ffmpeg`, `ffprobe` und CA-Zertifikate — keinen
Go-Compiler. Der Backend-Prozess und alle gestarteten Werkzeuge laufen als
UID/GID `10001`, nicht als root.

`/app` und die Binary bleiben root-owned und für den Dienstbenutzer nur
les-/ausführbar. Flüchtige Tool-Caches liegen unter `/tmp/musicdl`; persistent
beschreibbar sind ausschließlich `/data` und `/music`. Der Katalog liegt
vollständig in PostgreSQL.

## Voraussetzungen

* Podman 5 oder neuer, rootless
* Ein Compose-Provider: `podman-compose` oder Docker Compose als externer
  Provider (kein Docker-Daemon nötig)
* Ausgehende Netzverbindung für die Provider-APIs und die Downloads

## Schnellstart

```sh
cp .env.example .env
```

In `.env` mindestens `POSTGRES_PASSWORD` setzen und denselben Wert in
`MUSICDL_DATABASE_URL` eintragen.

```sh
mkdir -p data music
podman compose config
podman compose up -d --build
```

Wer den Provider fest wählen möchte:

```sh
export PODMAN_COMPOSE_PROVIDER=podman-compose
```

Compose startet zuerst `ytmdl-db`, wartet auf dessen Healthcheck und startet
danach `ytmdl-backend`. Zusätzlich wartet das Backend beim Start selbst noch bis
zu `MUSICDL_DB_STARTUP_TIMEOUT` (Standard 90s) mit exponentiellem Backoff auf die
Datenbank. `depends_on` ist also nur eine Beschleunigung — die Anwendung
verlässt sich nicht darauf und startet auch dann korrekt, wenn die Datenbank
erst Sekunden später bereit ist.

## Netzwerk

```yaml
networks:
  ytmdl-net:
    name: ytmdl-net
    driver: bridge
    ipam:
      config:
        - subnet: 172.31.250.0/28
```

| Eigenschaft | Wert |
| --- | --- |
| Name | `ytmdl-net` |
| Treiber | `bridge` |
| Subnetz | `172.31.250.0/28` (16 Adressen, Gateway `172.31.250.1`) |
| `internal` | **nein** |

Ein `/28` reicht für diesen Stack und reserviert nicht unnötig ein ganzes `/24`.
Das Netz ist bewusst **nicht** `internal: true`: das Backend braucht ausgehenden
HTTPS-Zugriff für YouTube, YouTube Music, die Spotify-API, `yt-dlp` und die
Cover-Downloads.

Container erhalten ihre Adressen dynamisch aus diesem Subnetz. Es sind keine
statischen IPs konfiguriert, und keine Konfiguration verweist auf eine
IP-Adresse — die Auflösung läuft über die Service-Namen:

```sh
podman exec ytmdl-backend getent hosts db
# 172.31.250.2   db.dns.podman  db.dns.podman db
```

### Subnetz-Kollision

`172.31.250.0/28` liegt in einem privaten RFC1918-Bereich und kann theoretisch
mit einem vorhandenen LAN-, VPN- oder anderen Container-Netz kollidieren.
Falls das passiert, kann das Subnetz in `compose.yaml` unter
`networks.ytmdl-net.ipam.config` frei geändert werden. Es ist keine Anpassung
an anderer Stelle nötig, weil die Container über DNS-Service-Namen und nicht
über fest codierte IP-Adressen kommunizieren. Eine automatische
Netzwerkerkennung gibt es bewusst nicht.

## Ports

| Container | Host-Port | Begründung |
| --- | --- | --- |
| `ytmdl-frontend` | `8080:8080` | Webfrontend & API-Reverse-Proxy |
| `ytmdl-backend` | — | nur über `ytmdl-net` erreichbar |
| `ytmdl-db` | — | nur über `ytmdl-net` erreichbar |

PostgreSQL und Backend veröffentlichen bewusst keinen direkten Host-Port. Der
Frontend-Nginx nimmt Anfragen entgegen, liefert statische Assets aus und leitet
`/api/*` über `ytmdl-net` an `backend:8080` weiter.

## Environment

Alle Werte kommen aus `.env`. Secrets gehören ausschließlich dorthin;
`.env.example` enthält nur Beispielwerte und `.env` ist von Git ausgeschlossen.

Die Backend-Variablen behalten das Präfix `MUSICDL_*`; das ist der bestehende,
getestete Vertrag des Backends und wurde nicht aus kosmetischen Gründen auf
`YTMDL_*` umbenannt. `YTMDL_*` benennt nur die reinen Compose-/Build-Werte.

| Variable | Bedeutung |
| --- | --- |
| `POSTGRES_DB`, `POSTGRES_USER`, `POSTGRES_PASSWORD` | Initialisierung von `ytmdl-db` |
| `MUSICDL_DATABASE_URL` | Verbindungs-URL des Backends, z. B. `postgres://ytmdl:pw@db:5432/ytmdl?sslmode=disable` |
| `MUSICDL_LISTEN_ADDR` | HTTP-Listener, Standard `0.0.0.0:8080` |
| `MUSICDL_LIBRARY` | Bibliothekspfad im Container, Standard `/music` |
| `MUSICDL_CONCURRENT_DOWNLOADS` | Parallel verarbeitete Tracks |
| `MUSICDL_YTDLP`, `MUSICDL_FFMPEG`, `MUSICDL_FFPROBE` | Pfade der Werkzeuge |
| `MUSICDL_DB_MAX_CONNS`, `MUSICDL_DB_MIN_CONNS` | Grenzen des Connection Pools (Standard 10/2) |
| `MUSICDL_DB_MAX_CONN_LIFETIME`, `MUSICDL_DB_MAX_CONN_IDLE_TIME` | Lebensdauer und Leerlauf einer Verbindung (Standard 30m/5m) |
| `MUSICDL_DB_CONNECT_TIMEOUT` | Timeout eines einzelnen Verbindungsversuchs (Standard 10s) |
| `MUSICDL_DB_STARTUP_TIMEOUT`, `MUSICDL_DB_STARTUP_BACKOFF` | Gesamtbudget und erster Backoff beim Warten auf PostgreSQL |
| `MUSICDL_DEEZER_REQUESTS_PER_SECOND`, `MUSICDL_DEEZER_BURST` | Deezer-Drosselung (Standard 8/s, Burst 5) |
| `MUSICDL_DEEZER_MAX_RETRIES`, `MUSICDL_DEEZER_RETRY_BACKOFF`, `MUSICDL_DEEZER_MAX_RETRY_BACKOFF` | Deezer-Retry-Grenzen (Standard 3, 500ms, 8s) |
| `MUSICDL_SUBSCRIPTIONS_ENABLED`, `MUSICDL_SUBSCRIPTION_SYNC_INTERVAL` | Künstler-Abonnements und Sync-Intervall (Standard true, 24h) |
| `MUSICDL_SUBSCRIPTION_CHECK_INTERVAL`, `MUSICDL_SUBSCRIPTION_RETRY_INTERVAL` | Scheduler-Prüfintervall und Retry-Intervall (Standard 15m, 1h) |
| `MUSICDL_SUBSCRIPTION_SYNC_TIMEOUT`, `MUSICDL_SUBSCRIPTION_BATCH_SIZE` | Sync-Timeout und Batch-Größe (Standard 30m, 25) |
| `YTDM_SPOTIFY_CLIENT_ID`, `YTDM_SPOTIFY_CLIENT_SECRET` | Optionaler Spotify-Metadatenprovider |
| `YTDM_COOKIEFILE` | Optionale `cookies.txt`, z. B. `/data/cookies.txt` |
| `YTMDL_VERSION`, `YTMDL_HOST_PORT`, `YTMDL_DATA_PATH`, `YTMDL_MUSIC_PATH` | nur von Compose gelesen |

Das Backend loggt die Verbindungs-URL nur ohne Passwort und schreibt Credentials
in keine Log- oder Fehlermeldung.

Die alten Variablen `MUSICDL_DATABASE` und `YTDM_DATABASE_PATH` zeigten auf eine
SQLite-Datei. Sie werden bewusst nicht mehr ignoriert, sondern führen zu einem
klaren Startfehler mit dem Hinweis auf `MUSICDL_DATABASE_URL`.

## Volumes

| Ziel | Inhalt | Typ |
| --- | --- | --- |
| `ytmdl-postgres-data` → `/var/lib/postgresql` | PostgreSQL-Cluster | Named Volume |
| `./music` → `/music` | Musikbibliothek | Bind-Mount |
| `./data` → `/data` | optionale Laufzeitdateien wie `cookies.txt` | Bind-Mount |

Es liegt kein PostgreSQL-Datenverzeichnis im Projektrepository; der Cluster lebt
ausschließlich im Named Volume.

Das Image `postgres:18-alpine` legt seinen Cluster unter
`$PGDATA=/var/lib/postgresql/18/docker` an und deklariert sein Volume eine Ebene
darüber. Ein Mount auf das ältere `/var/lib/postgresql/data` lässt das Image mit
einer ausführlichen Fehlermeldung abbrechen; der Mount muss deshalb auf
`/var/lib/postgresql` liegen.

## Rootless Podman und Bind-Mounts

`compose.yaml` nutzt Podmans `keep-id`-Mapping für den Backend-Container und
bindet `./data` sowie `./music` mit SELinux-Relabeling (`:Z`) ein. Dadurch
entspricht der Benutzer des aufrufenden Rootless-Podman-Prozesses im Container
der UID/GID `10001` und beide Verzeichnisse bleiben ohne Host-UID-0 beschreibbar.

`ytmdl-db` braucht kein `keep-id`: sein Cluster liegt in einem Named Volume,
dessen Eigentümer Podman im Benutzer-Namespace verwaltet.

Falls ein älterer Compose-Provider die erweiterte `keep-id`-Syntax nicht
unterstützt, die Zeile `userns_mode` entfernen und die Verzeichnisse einmal im
Rootless-Namespace vorbereiten:

```sh
mkdir -p data music
podman unshare chown -R 10001:10001 data music
```

Auf Systemen ohne SELinux kann `:Z` entfallen.

## Healthchecks

```sh
podman compose ps
curl -fsS 'http://127.0.0.1:8080/api/v1/health?scope=essential'
```

`ytmdl-db` prüft sich mit `pg_isready`. Der Containercheck des Backends mit
`scope=essential` prüft nur, ob der Prozess antwortet und ob die
PostgreSQL-Verbindung funktioniert. Externe Dienste wie Spotify oder
YouTube Music werden dabei nie angefragt.

`GET /api/v1/health` ohne `scope` ergänzt lokale Diagnosen für `yt-dlp`,
`ffmpeg` und `ffprobe`. Ein fehlendes Werkzeug ergibt den Status `degraded`,
aber weiterhin HTTP 200. Nur eine nicht erreichbare Datenbank macht den Check
mit HTTP 503 ungesund.

## Frontend-Proxy und DNS

`ytmdl-frontend` liefert `/` statisch aus und reicht `/api/*` an `backend:8080`
weiter. Der Upstream steht dabei bewusst in einer nginx-Variablen:

```nginx
set $ytmdl_backend http://backend:8080;
proxy_pass $ytmdl_backend;
```

Mit einem literalen Host löst nginx den Namen einmalig beim Start auf und behält
die Adresse für die gesamte Laufzeit. Nach `podman compose up -d
--force-recreate backend` hat der Backend-Container eine andere IP, und der
Proxy antwortet dann dauerhaft mit `502`. Über eine Variable findet die
Auflösung pro Anfrage statt.

Dafür braucht nginx einen expliziten `resolver`. Dessen Adresse hängt vom
Subnetz von `ytmdl-net` ab, weshalb sie nicht fest in der Konfiguration steht:
`docker-entrypoint.d/15-resolver.envsh` liest beim Start den ersten
`nameserver` aus `/etc/resolv.conf` des Containers und setzt ihn in die Vorlage
ein. Ein geändertes Subnetz erfordert deshalb keine Anpassung am Frontend.

`index.html` wird mit `Cache-Control: no-cache, must-revalidate` ausgeliefert,
die Dateien unter `/assets/` mit `immutable`. Nur die Assets tragen einen
Inhalts-Hash im Namen; eine gecachte `index.html` würde nach einem Update
weiterhin auf die Bundles der vorherigen Version zeigen.

Der SSE-Strom `/api/v1/events` läuft durch denselben `location`-Block mit
`proxy_buffering off`, damit Ereignisse den Browser sofort erreichen.

## CI und Release über Gitea Actions

Zwei Workflows unter `.gitea/workflows/`:

| Workflow | Auslöser | Tut |
| --- | --- | --- |
| `ci.yml` | Push auf `main`, Pull Request gegen `main` | Tests und Builds, veröffentlicht nichts |
| `release.yml` | Tag `v*` | dieselben Tests, danach Images in die Registry |

Beide führen die PostgreSQL-Integrationstests wirklich aus: Der Job startet
`postgres:18-alpine` als Service und setzt `MUSICDL_TEST_DATABASE_URL`. Ohne
diese Variable überspringen sich die Repository-Tests selbst — in CI tun sie
das nicht. Die Zugangsdaten dieses Dienstes gelten für die Dauer eines Jobs und
haben mit dem Deployment nichts zu tun.

### Versionierung

Der Git-Tag ist die Quelle der Wahrheit. `refs/tags/v0.4.0` ergibt die Images:

```text
<registry-url>/<owner>/ytmdl-backend:<version>    und :latest
<registry-url>/<owner>/ytmdl-frontend:<version>   und :latest
```

Vor dem Bauen vergleicht der Workflow den Tag mit `.release-version` und bricht
bei einer Abweichung ab. Ein Release beginnt deshalb damit, `.release-version`
zu setzen, zu committen und dann erst zu taggen. `latest` entsteht
ausschließlich aus einem `v*`-Tag, nie aus einem Branch-Build.

### Registry-Anmeldung

Der Workflow meldet sich mit dem konfigurierten Secret `REGISTRY_TOKEN`
(bzw. dem Fallback `GITHUB_TOKEN`) an.
Für einen Host, der Images zieht, reicht eine einmalige Anmeldung:

```sh
podman login <registry-url>
```

### Deployment aus der Registry

`compose.yaml` baut lokal und bleibt das Werkzeug für die Entwicklung.
`compose.registry.yaml` zieht stattdessen die veröffentlichten Images und hat
bewusst keinen Build-Kontext:

```sh
YTMDL_VERSION=0.14.1 podman compose -f compose.registry.yaml up -d
```

`YTMDL_VERSION` hat dort keinen Standardwert — ein Tippfehler bricht ab,
statt unbemerkt eine andere Version zu starten. Netz, Volume, `.env` und die
Healthchecks sind mit `compose.yaml` identisch.

## Stop, Restart und Recovery

```sh
podman compose stop
podman compose start
```

Podman sendet SIGTERM direkt an `/app/musicdl`. Das Backend sperrt daraufhin
neue Jobs, trennt Event-Streams, fährt HTTP herunter, beendet Worker und die
gesamte `yt-dlp`/`ffmpeg`-Prozessgruppe, setzt unterbrochene Arbeit zurück,
schließt den Datenbankpool und beendet sich mit Exit-Code 0. Die
`stop_grace_period` von 40s ist dafür großzügig bemessen; im Test dauert der
Shutdown ein bis zwei Sekunden.

Das Recovery-Verhalten ist deterministisch und gilt gleichermaßen für einen
sauberen SIGTERM, einen Absturz und einen Host-Neustart. Beim Herunterfahren und
noch einmal beim Start gilt:

```text
Job nicht terminal        → queued
JobItem in matching /
downloading / tagging     → pending (startet erneut bei matching)
Bereits fertige JobItems  → bleiben completed / failed / skipped
```

Damit bleibt kein Job dauerhaft in `downloading` stehen, kein Track wird doppelt
heruntergeladen und keine Datei doppelt zugeordnet.

## Backup und Restore

Sichern lassen sich Katalog und Bibliothek getrennt.

```sh
# Datenbank sichern
podman compose exec -T db \
  pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc --no-owner > ytmdl.dump

# Bibliothek sichern
tar -czf music.tar.gz music
```

Zurückspielen:

```sh
podman compose up -d db
podman compose exec -T db \
  pg_restore -U "$POSTGRES_USER" -d "$POSTGRES_DB" --clean --if-exists --no-owner < ytmdl.dump
podman compose up -d
```

Ein Klartext-Dump funktioniert genauso:

```sh
podman compose exec -T db pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" > ytmdl.sql
podman compose exec -T db psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" < ytmdl.sql
```

Ein normales `podman compose down` entfernt Container und Netz, aber keine
Daten: das Volume `ytmdl-postgres-data` und `./music` bleiben erhalten, und
`ytmdl-net` wird beim nächsten `up` mit demselben Subnetz neu angelegt. Nur
`podman compose down -v` löscht das Volume und damit den Katalog.

## Update

```sh
podman compose down
podman compose build --no-cache
podman compose up -d
```

Migrationen laufen automatisch beim Start und sind gegen parallele Starts durch
einen Advisory Lock abgesichert. Updates von Alpine, `yt-dlp`, `ffmpeg` oder
PostgreSQL erfolgen reproduzierbar über einen neuen Build, nicht durch
Änderungen im laufenden Container.

## Logs

```sh
podman compose logs -f backend
podman compose logs -f db
```

Das Backend schreibt strukturiertes JSON auf stdout (`YTDM_LOG_FORMAT=text` für
lesbare Zeilen). Query-Strings, Credentials und Datenbankpasswörter erscheinen
nicht im Log.

## Direkter Podman-Betrieb ohne Compose

```sh
podman network create --subnet 172.31.250.0/28 ytmdl-net
podman volume create ytmdl-postgres-data

podman run -d --name ytmdl-db \
  --network ytmdl-net --network-alias db \
  -e POSTGRES_DB=ytmdl -e POSTGRES_USER=ytmdl -e POSTGRES_PASSWORD=... \
  -v ytmdl-postgres-data:/var/lib/postgresql \
  docker.io/library/postgres:18-alpine

podman build -t ytmdl-backend -f Containerfile .
mkdir -p data music
podman run -d --name ytmdl-backend \
  --network ytmdl-net --network-alias backend \
  --userns=keep-id:uid=10001,gid=10001 \
  --env-file .env \
  -p 8080:8080 \
  -v "$(pwd)/data:/data:Z" \
  -v "$(pwd)/music:/music:Z" \
  ytmdl-backend
```

Die in `.release-version` abgelegte Version wird beim Build eingebettet. Ein
expliziter Buildwert ist ebenfalls möglich:

```sh
podman build --build-arg VERSION=0.4.0 -t ytmdl-backend:0.4.0 .
```

Podmans standardmäßiges OCI-Imageformat ignoriert Healthcheck-Metadaten aus
einem Containerfile. `compose.yaml` setzt den identischen Check deshalb
zusätzlich beim Erzeugen des Containers. Wer das Image direkt mit eingebettetem
Healthcheck verwenden möchte, baut es im Docker-v2-Imageformat:

```sh
podman build --format=docker -t ytmdl-backend .
```

## Frontend-Container: ytmdl-frontend

Der Frontend-Container wird als Multi-Stage-Build gebaut:

```text
Bun (Stage 1)  →  Frontend-Build  →  dist/  →  nginx:alpine (Stage 2)
```

Bun bleibt dabei reines Build-Werkzeug und läuft nicht als Produktions-Webserver.
Nginx liefert die kompilierten statischen Assets aus, fängt SPA-Routen ab und
leitet `/api/*` unbuffered (SSE-fähig) an das Backend weiter:

```yaml
  frontend:
    build:
      context: ./frontend
      dockerfile: Containerfile
    image: ytmdl-frontend:${YTMDL_VERSION:-local}
    container_name: ytmdl-frontend
    depends_on:
      backend:
        condition: service_healthy
    ports:
      - "${YTMDL_HOST_PORT:-8080}:8080"
    networks:
      - ytmdl-net
    healthcheck:
      test:
        - CMD
        - wget
        - -q
        - -O
        - /dev/null
        - http://127.0.0.1:8080/
      interval: 15s
      timeout: 5s
      start_period: 5s
      retries: 3
    restart: unless-stopped
```
