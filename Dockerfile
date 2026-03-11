# ---- Stage 1: Build web assets ----
FROM node:24-alpine AS web-build
WORKDIR /build/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --ignore-scripts --legacy-peer-deps
COPY web/ ./
RUN npm run build

# ---- Stage 2: Build Go server ----
FROM golang:1.24-alpine AS server-build
RUN apk add --no-cache git
WORKDIR /build/server
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /meshserver ./cmd/meshserver

# ---- Stage 3: Final image ----
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S opengate && adduser -S opengate -G opengate
COPY --from=server-build /meshserver /usr/local/bin/meshserver
COPY --from=web-build /build/web/dist /srv/web
USER opengate
EXPOSE 8080 9090/udp
ENTRYPOINT ["meshserver"]
CMD ["-listen", ":8080", "-quic-listen", ":9090", "-data-dir", "/data"]
