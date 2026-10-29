# =============================================================================
# Sklad — container image
#
#   Stage 1  node:22-alpine    build the SPA  (web/dist)
#   Stage 2  golang:1.26.9-alpine build the server statically (CGO_ENABLED=0)
#   Stage 3  alpine:3.21       run it as a non-root user with a healthcheck
#
# The container is meant to sit behind the Caddy edge (see docker-compose.yml):
# it listens on plain HTTP on :8080, keeps its state in the /data volume, and
# terminates nothing itself.
#
# Build metadata is injected with build arguments so /api/v1/version and the
# About page can show a real version:
#
#   docker build \
#     --build-arg VERSION=1.0.0 \
#     --build-arg COMMIT=$(git rev-parse --short HEAD) \
#     --build-arg BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ) \
#     -t sklad:local .
#
# No BuildKit-only features are used, so the classic builder shipped with older
# QNAP Container Station releases can build this file too.
# =============================================================================

# -----------------------------------------------------------------------------
# Stage 1 — single-page application
# -----------------------------------------------------------------------------
FROM node:22-alpine AS web

WORKDIR /src/web

# Copy the whole frontend so the image also builds from a checkout where the
# SPA does not exist yet (the server then serves its placeholder page).
COPY web/ ./

RUN set -eux; \
    if [ -f package.json ]; then \
        if [ -f package-lock.json ]; then \
            npm ci --no-audit --no-fund; \
        else \
            echo "note: web/package-lock.json is absent - falling back to npm install"; \
            npm install --no-audit --no-fund; \
        fi; \
        npm run build; \
        test -f dist/index.html; \
    else \
        echo "note: no frontend yet - building the placeholder only"; \
        mkdir -p dist; \
        touch dist/.gitkeep; \
    fi

# -----------------------------------------------------------------------------
# Stage 2 — server
# -----------------------------------------------------------------------------
FROM golang:1.26.9-alpine AS build

ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_TIME=unknown

WORKDIR /src

# Dependencies first so source edits do not invalidate the module download.
COPY go.mod go.sum ./
RUN go mod download

COPY server/ ./server/
COPY web/embed.go ./web/
COPY --from=web /src/web/dist ./web/dist

# Static binary: no CGO, which is what lets the runtime stage stay minimal and
# the same build work on any NAS architecture.
RUN CGO_ENABLED=0 go build -trimpath \
        -ldflags "-s -w \
          -X sklad/server/internal/buildinfo.Version=${VERSION} \
          -X sklad/server/internal/buildinfo.Commit=${COMMIT} \
          -X sklad/server/internal/buildinfo.BuildTime=${BUILD_TIME}" \
        -o /out/sklad ./server/cmd/sklad

RUN CGO_ENABLED=0 go build -trimpath -o /out/import-json ./server/cmd/import-json

# Inspect the binary without executing it, so the check also works when the
# image is cross-built for another architecture.
RUN go version -m /out/sklad | sed -n '1,6p'

# -----------------------------------------------------------------------------
# Stage 3 — runtime
# -----------------------------------------------------------------------------
FROM alpine:3.21 AS runtime

LABEL org.opencontainers.image.title="Sklad" \
      org.opencontainers.image.description="Equipment inventory & lending (server + embedded SPA)" \
      org.opencontainers.image.authors="David Žitník" \
      org.opencontainers.image.licenses="MIT"

# ca-certificates: outbound HTTPS (Discord webhooks). tzdata: human-readable
# timestamps for the tools run inside the container; the server itself embeds
# the Go time zone database.
RUN apk add --no-cache ca-certificates tzdata

# Non-root by default. Change APP_UID/APP_GID when the data directory is a bind
# mount owned by another account (typical on a QNAP: the NAS user's uid).
ARG APP_UID=10001
ARG APP_GID=10001
RUN addgroup -g "${APP_GID}" -S sklad \
 && adduser -u "${APP_UID}" -S -G sklad -h /app -s /sbin/nologin sklad

COPY --from=build /out/sklad /usr/local/bin/sklad
COPY --from=build /out/import-json /usr/local/bin/import-json

WORKDIR /app
RUN mkdir -p /data \
 && chown -R sklad:sklad /app /data

# Defaults that suit the compose deployment; every one of them is overridable.
ENV SKLAD_HTTP_ADDR=:8080 \
    SKLAD_DATA_DIR=/data \
    SKLAD_DB_PATH=/data/sklad.db \
    SKLAD_LOG_FORMAT=json

# Keep the data volume even when the container is started by hand without -v.
VOLUME ["/data"]

USER sklad
EXPOSE 8080

# Liveness: the API answers 200 with {"status":"ok"} while every dependency is
# reachable, and 503 with "degraded" when SQLite is not.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- http://127.0.0.1:8080/api/v1/health | grep -q '"status":"ok"' || exit 1

STOPSIGNAL SIGTERM
ENTRYPOINT ["/usr/local/bin/sklad"]
