FROM node:24-alpine AS web-build
WORKDIR /build/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --ignore-scripts
COPY web/ ./
RUN npm run build

FROM golang:1.27-alpine AS server-build
# The default strips symbols and debug info; the endurance run empties it to resolve core dumps.
ARG GO_LDFLAGS="-s -w"
WORKDIR /build/server
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="$GO_LDFLAGS" -o /meshserver ./cmd/meshserver

FROM alpine:3.20
# Unpinned packages track the latest CA bundle and security fixes.
# hadolint ignore=DL3018
RUN apk upgrade --no-cache \
    && apk add --no-cache ca-certificates tzdata \
    && addgroup -S opengate && adduser -S opengate -G opengate \
    && mkdir -p /data && chown opengate:opengate /data
COPY --from=server-build /meshserver /usr/local/bin/meshserver
COPY --from=web-build /build/web/dist /srv/web
USER opengate
EXPOSE 8080 8081 4433 9090/udp
# The image's own healthcheck lets `docker inspect` report liveness; busybox provides `wget`.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget --quiet --spider --tries=1 http://127.0.0.1:8080/healthz || exit 1
ENTRYPOINT ["meshserver"]
CMD ["-listen", ":8080", "-internal-listen", ":8081", "-quic-listen", ":9090", "-mps-listen", ":4433", "-data-dir", "/data", "-web-dir", "/srv/web"]
