# OpenGate

[![CI](https://github.com/volchanskyi/opengate/actions/workflows/ci.yml/badge.svg)](https://github.com/volchanskyi/opengate/actions/workflows/ci.yml)
[![Go Server Coverage](https://img.shields.io/endpoint?url=https://gist.githubusercontent.com/volchanskyi/cf505c74b56eab52c9497af517b53222/raw/opengate-coverage.json)](https://github.com/volchanskyi/opengate/actions/workflows/ci.yml)

Remote device management platform.

- **Agent** (Rust) — runs on managed devices (Windows/Linux)
- **Server** (Go) — central hub with QUIC + WebSocket + REST API
- **Web** (React/TypeScript) — browser-based management UI

> See the [Wiki](https://github.com/volchanskyi/opengate/wiki) for architecture, wire protocol, CI pipeline, and other detailed documentation.
>
> [API Reference](https://volchanskyi.github.io/opengate/docs/api/) — interactive OpenAPI documentation (Scalar)

## Quick Start

```bash
make build   # Build all components
make test    # Run all tests
make lint    # Run all linters
```

### Running the server

```bash
cd server
go build -o meshserver ./cmd/meshserver

# JWT_SECRET is required — pass via flag or env var
JWT_SECRET=changeme ./meshserver \
  -listen :8080 \
  -quic-listen :9090 \
  -mps-listen :4433 \
  -data-dir ./data
```

Or run via Docker (multi-arch images published to GHCR on every push to `main`):

```bash
docker pull ghcr.io/volchanskyi/opengate-server:latest
docker run -e JWT_SECRET=changeme -p 8080:8080 -p 9090:9090/udp \
  -v opengate-data:/data ghcr.io/volchanskyi/opengate-server:latest
```

For production deployment with Caddy reverse proxy and auto-TLS:

```bash
cd deploy
cp .env.example .env          # edit JWT_SECRET and DOMAIN
docker compose up -d
```

| Flag | Default | Description |
|------|---------|-------------|
| `-listen` | `:8080` | HTTP address (REST API) |
| `-quic-listen` | `:9090` | QUIC address (agent connections, mTLS) |
| `-mps-listen` | `:4433` | MPS TLS address (Intel AMT CIRA connections) |
| `-data-dir` | `./data` | Directory for SQLite database and CA certificates |
| `-jwt-secret` | — | JWT signing secret (or `JWT_SECRET` env); **required** |
| `-vapid-contact` | — | VAPID contact email for Web Push notifications (optional) |

On first startup the server generates a self-signed ECDSA P-256 CA under `data-dir` (`ca.crt`, `ca.key`), VAPID keys (`vapid.json`), and creates the SQLite database with WAL mode enabled.

## Project Structure

```
agent/                       Rust workspace
├── crates/
│   ├── mesh-protocol/       Shared wire protocol (MessagePack codec, frame format)
│   ├── mesh-agent-core/     Agent identity, QUIC connection, platform traits,
│   │                        session handler, WebRTC peer connection
│   ├── platform-linux/      Linux: runtime detection, systemd, X11 capture (feature-gated)
│   └── platform-windows/    Windows: DXGI capture, Win32 input (cfg-gated)
server/                      Go module
├── cmd/meshserver/          Binary entry point
├── internal/
│   ├── agentapi/            QUIC server, handshake, agent connection lifecycle
│   ├── api/                 HTTP REST handlers (oapi-codegen strict server, chi v5)
│   ├── auth/                JWT + bcrypt authentication
│   ├── cert/                CA management, mTLS certificate signing (ECDSA P-256, RSA 2048 for MPS)
│   ├── db/                  SQLite store, migrations (golang-migrate)
│   ├── mps/                 Intel AMT Management Presence Server (CIRA/APF over TLS)
│   ├── protocol/            Go-side wire protocol codec + golden file verification
│   ├── notifications/       Web Push notifications (VAPID, webpush-go), Notifier interface
│   ├── relay/               Byte-transparent WebSocket relay for browser↔agent piping
│   ├── signaling/           WebRTC signaling state machine, ICE config, session tracker
│   └── testutil/            Shared test helpers (excluded from coverage metrics)
├── tests/integration/       Integration test suite (real QUIC + SQLite)
api/openapi.yaml             OpenAPI 3.0.3 spec (single source of truth)
docs/api/                    Scalar API reference viewer
web/                         React + TypeScript (Vite, Tailwind, Zustand)
deploy/                      Production deployment
├── terraform/               OCI infrastructure (VCN, subnet, compute)
├── caddy/                   Caddyfile (reverse proxy, auto-TLS)
├── docker-compose.yml       Production stack (server + Caddy)
└── docker-compose.staging.yml  Ephemeral staging overrides
testdata/golden/             Cross-language wire format fixtures
```

## Running tests locally

```bash
make test               # All tests — Rust + Go + Web
make test-go            # Go server (unit + integration) with race detector
make test-integration   # Integration suite only
make test-rust          # Rust workspace
make test-web           # React / TypeScript
make test-coverage      # Go coverage report printed to stdout
make golden             # Regenerate golden fixtures and verify cross-language compat
```
