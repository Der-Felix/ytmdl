# syntax=docker/dockerfile:1

FROM golang:alpine AS builder

WORKDIR /src/backend

COPY backend/go.mod backend/go.sum ./
RUN go mod download

COPY backend/ ./
COPY .release-version /src/.release-version

ARG VERSION=""
RUN build_version="${VERSION:-$(cat /src/.release-version)}" && \
    CGO_ENABLED=0 go build \
      -trimpath \
      -ldflags="-s -w -X main.version=${build_version}" \
      -o /out/musicdl \
      ./cmd/server

FROM alpine:latest AS runtime

RUN apk add --no-cache \
      ca-certificates \
      ffmpeg \
      yt-dlp && \
    addgroup -S -g 10001 musicdl && \
    adduser -S -D -H -u 10001 -G musicdl musicdl && \
    install -d -o root -g root -m 0755 /app && \
    install -d -o musicdl -g musicdl -m 0755 /data /music /tmp/musicdl

COPY --from=builder /out/musicdl /app/musicdl

# MUSICDL_DATABASE_URL has no default on purpose: the backend must be pointed
# at a PostgreSQL server explicitly and refuses to start without one.
ENV MUSICDL_LISTEN_ADDR=0.0.0.0:8080 \
    MUSICDL_LIBRARY=/music \
    MUSICDL_CONCURRENT_DOWNLOADS=2 \
    MUSICDL_YTDLP=/usr/bin/yt-dlp \
    MUSICDL_FFMPEG=/usr/bin/ffmpeg \
    MUSICDL_FFPROBE=/usr/bin/ffprobe \
    HOME=/tmp/musicdl \
    XDG_CACHE_HOME=/tmp/musicdl/.cache \
    DENO_DIR=/tmp/musicdl/deno

WORKDIR /app
VOLUME ["/data", "/music"]
EXPOSE 8080
STOPSIGNAL SIGTERM

USER musicdl:musicdl

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget -q -O /dev/null 'http://127.0.0.1:8080/api/v1/health?scope=essential' || exit 1

ENTRYPOINT ["/app/musicdl"]
