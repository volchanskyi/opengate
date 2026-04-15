# OpenGate

[![CI](https://github.com/volchanskyi/opengate/actions/workflows/ci.yml/badge.svg)](https://github.com/volchanskyi/opengate/actions/workflows/ci.yml)
[![Go Server Coverage](https://img.shields.io/endpoint?url=https://gist.githubusercontent.com/volchanskyi/cf505c74b56eab52c9497af517b53222/raw/opengate-coverage.json)](https://github.com/volchanskyi/opengate/actions/workflows/ci.yml)
[![Rust Agent Coverage](https://img.shields.io/endpoint?url=https://gist.githubusercontent.com/volchanskyi/cf505c74b56eab52c9497af517b53222/raw/opengate-rust-coverage.json)](https://github.com/volchanskyi/opengate/actions/workflows/ci.yml)
[![Web Client Coverage](https://img.shields.io/endpoint?url=https://gist.githubusercontent.com/volchanskyi/cf505c74b56eab52c9497af517b53222/raw/opengate-web-coverage.json)](https://github.com/volchanskyi/opengate/actions/workflows/ci.yml)

Remote device management platform.

- **Agent** (Rust) — runs on managed devices (Windows/Linux)
- **Server** (Go) — central hub with QUIC + WebSocket + REST API
- **Web** (React/TypeScript) — browser-based management UI

> See [`docs/`](./docs/) for architecture, wire protocol, CI pipeline, and other detailed documentation. Start at [`docs/Home.md`](./docs/Home.md).
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
# OPENGATE_GITHUB_REPO enables auto-sync of agent manifests from GitHub Releases
# DATABASE_URL (or -database-url) is required — points at the PostgreSQL instance
DATABASE_URL=postgres://opengate:opengate@localhost:5432/opengate?sslmode=disable \
JWT_SECRET=changeme-must-be-at-least-32chars OPENGATE_GITHUB_REPO=volchanskyi/opengate ./meshserver \
  -listen :8080 \
  -quic-listen :9090 \
  -mps-listen :4433 \
  -data-dir ./data
```

Or run via Docker (multi-arch images published to GHCR on every push to `main`):

```bash
docker pull ghcr.io/volchanskyi/opengate-server:latest
docker run -e JWT_SECRET=changeme-must-be-at-least-32chars -p 8080:8080 -p 9090:9090/udp \
  -v opengate-data:/data ghcr.io/volchanskyi/opengate-server:latest
```

For production deployment with Caddy reverse proxy and auto-TLS:

```bash
cd deploy
cp .env.example .env          # fill in secrets (JWT_SECRET, AMT_PASS, DOMAIN)
docker compose up -d
```

## Project Structure

```
agent/                       Rust workspace
├── crates/
│   ├── mesh-agent/          Binary entry point (QUIC mTLS, handshake, control loop)
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
│   ├── db/                  PostgreSQL store (pgx/v5 stdlib), migrations (golang-migrate)
│   ├── mps/                 Intel AMT Management Presence Server (CIRA/APF over TLS)
│   ├── protocol/            Go-side wire protocol codec + golden file verification
│   ├── notifications/       Web Push notifications (VAPID, webpush-go), Notifier interface
│   ├── metrics/             Prometheus instrumentation (HTTP middleware, InstrumentedStore)
│   ├── relay/               Message-oriented WebSocket relay for browser↔agent piping
│   ├── signaling/           WebRTC signaling state machine, ICE config, session tracker
│   ├── updater/             Agent auto-update: Ed25519 signing, GitHub release sync, manifests
│   ├── clientapi/           Client-facing API helpers
│   ├── multiserver/         Cross-server routing types
│   └── testutil/            Shared test helpers (excluded from coverage metrics)
├── tests/integration/       Integration test suite (real QUIC + real PostgreSQL)
api/openapi.yaml             OpenAPI 3.0.3 spec (single source of truth)
docs/adr/                    Architecture Decision Records
docs/api/                    Scalar API reference viewer
web/                         React + TypeScript (Vite, Tailwind, Zustand)
deploy/                      Production deployment
├── terraform/               OCI infrastructure (VCN, subnet, compute)
├── caddy/                   Caddyfile (reverse proxy, SPA file serving, auto-TLS)
├── scripts/                 CD deploy, smoke-test, and rollback scripts
├── docker-compose.yml       Production stack (server + web-init + Caddy)
├── docker-compose.staging.yml  Persistent staging overrides
├── docker-compose.test.yml  E2E test environment (tmpfs, single server)
└── docker-compose.monitoring.yml  Observability stack (VictoriaMetrics, Grafana, Loki, Promtail, Node Exporter, Uptime Kuma)
load/k6/scenarios/           k6 load test scripts (API baseline, relay, concurrent agents)
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
make lint-deploy        # Validate deploy configs (Terraform, Compose, Caddy, YAML)
make e2e                # End-to-end Playwright tests via docker-compose.test.yml
make load-test          # k6 HTTP/WS load tests against localhost:8080
make load-test-quic     # Go QUIC load harness (100 concurrent agents)
```
