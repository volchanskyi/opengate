# ---- Stage 1: Build web assets ----
FROM node:24-alpine AS web-build
WORKDIR /build/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --ignore-scripts
COPY web/ ./
RUN npm run build

# ---- Stage 2: Build Go server ----
FROM golang:1.26-alpine AS server-build
# What the linker keeps. The default drops the symbol table and the debugging
# information, which is what every image this repository ships wants.
#
# The endurance run is the exception and builds with it empty. A core dump is
# how the run follows what actually holds a leaked object, and reading one means
# resolving addresses back to types and frames — which needs the very tables the
# default removes. It changes no generated code, only what travels beside it.
ARG GO_LDFLAGS="-s -w"
WORKDIR /build/server
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="$GO_LDFLAGS" -o /meshserver ./cmd/meshserver

# ---- Stage 3: Final image ----
FROM alpine:3.20
# Pull latest security fixes for base-image packages before installing extras,
# so Trivy doesn't flag HIGH CVEs in libcrypto3/libssl3 etc. that are already
# patched in the 3.20 repo. Pinning ca-certificates/tzdata to a specific
# version (DL3018) would freeze the image on a stale CA bundle and defeat
# `apk upgrade` above; suppress for those packages only.
# hadolint ignore=DL3018
RUN apk upgrade --no-cache \
    && apk add --no-cache ca-certificates tzdata \
    && addgroup -S opengate && adduser -S opengate -G opengate \
    && mkdir -p /data && chown opengate:opengate /data
COPY --from=server-build /meshserver /usr/local/bin/meshserver
COPY --from=web-build /build/web/dist /srv/web
USER opengate
EXPOSE 8080 8081 4433 9090/udp
# busybox `wget` ships in alpine:3.20 by default — no extra apk add needed.
# Production health is still gated by docker-compose's per-service healthcheck,
# but this satisfies Checkov CKV_DOCKER_2 and lets `docker inspect` report
# liveness for any operator running the image directly.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget --quiet --spider --tries=1 http://127.0.0.1:8080/healthz || exit 1
ENTRYPOINT ["meshserver"]
CMD ["-listen", ":8080", "-internal-listen", ":8081", "-quic-listen", ":9090", "-mps-listen", ":4433", "-data-dir", "/data", "-web-dir", "/srv/web"]
