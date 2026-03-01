# OpenGate

[![CI](https://github.com/volchanskyi/opengate/actions/workflows/ci.yml/badge.svg)](https://github.com/volchanskyi/opengate/actions/workflows/ci.yml)
[![Go Server Coverage](https://img.shields.io/endpoint?url=https://gist.githubusercontent.com/volchanskyi/cf505c74b56eab52c9497af517b53222/raw/opengate-coverage.json)](https://github.com/volchanskyi/opengate/actions/workflows/ci.yml)

Remote device management platform.

- **Agent** (Rust) — runs on managed devices (Windows/Linux)
- **Server** (Go) — central hub with QUIC + WebSocket + REST API
- **Web** (React/TypeScript) — browser-based management UI

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
  -data-dir ./data
```

| Flag | Default | Description |
|------|---------|-------------|
| `-listen` | `:8080` | HTTP address (REST API) |
| `-quic-listen` | `:9090` | QUIC address (agent connections, mTLS) |
| `-data-dir` | `./data` | Directory for SQLite database and CA certificates |
| `-jwt-secret` | — | JWT signing secret (or `JWT_SECRET` env); **required** |

On first startup the server generates a self-signed ECDSA P-256 CA under `data-dir` (`ca.crt`, `ca.key`) and creates the SQLite database with WAL mode enabled.

## Architecture

```
┌─────────┐  QUIC/mTLS   ┌─────────────────────────────┐  HTTP/JSON   ┌─────────┐
│  Agent   │◄────────────►│         Server              │◄────────────►│   Web   │
│  (Rust)  │  :9090       │         (Go)                │  :8080       │ (React) │
└─────────┘               │                             │              └─────────┘
                          │  ┌─────────┐  ┌───────────┐ │
                          │  │ AgentAPI │  │  REST API │ │
                          │  │  (QUIC)  │  │  (HTTP)   │ │
                          │  └────┬─────┘  └─────┬─────┘ │
                          │       │              │       │
                          │  ┌────▼──────────────▼─────┐ │
                          │  │   SQLite (WAL mode)     │ │
                          │  └─────────────────────────┘ │
                          └─────────────────────────────┘
```

**Agent → Server** connections use QUIC with mutual TLS (mTLS). The server
signs agent certificates from its CA and verifies them on every connection.
The control stream carries a binary handshake (ServerHello/AgentHello) followed
by MessagePack-encoded control messages (registration, heartbeat, session
requests).

**Web → Server** connections use standard HTTP with JWT bearer-token
authentication. Passwords are bcrypt-hashed.

## Project Structure

```
agent/                       Rust workspace
├── crates/
│   ├── mesh-protocol/       Shared wire protocol (MessagePack codec, frame format)
│   ├── mesh-agent-core/     Agent identity, QUIC connection, control loop
│   ├── platform-linux/      Linux platform layer
│   └── platform-windows/    Windows platform layer
server/                      Go module
├── cmd/meshserver/          Binary entry point
├── internal/
│   ├── agentapi/            QUIC server, handshake, agent connection lifecycle
│   ├── api/                 HTTP REST handlers (chi v5 router)
│   ├── auth/                JWT + bcrypt authentication
│   ├── cert/                CA management, mTLS certificate signing (ECDSA P-256)
│   ├── db/                  SQLite store, migrations (golang-migrate)
│   ├── protocol/            Go-side wire protocol codec + golden file verification
│   ├── relay/               Byte-transparent WebSocket relay for browser↔agent piping
│   └── testutil/            Shared test helpers (excluded from coverage metrics)
├── tests/integration/       Integration test suite (real QUIC + SQLite)
web/                         React + TypeScript (Vite, Tailwind, Zustand)
testdata/golden/             Cross-language wire format fixtures
```

### Wire Protocol

Control messages use MessagePack encoding inside a framed transport:

```
[1-byte frame type][4-byte BE payload length][payload]
```

Ping (`0x05`) and Pong (`0x06`) are single-byte frames with no payload.
The handshake (ServerHello/AgentHello) uses raw binary encoding, not MessagePack.
Golden file tests guarantee bit-identical encoding between the Rust and Go codecs.

### Database

SQLite with WAL mode, `MaxOpenConns(1)`, foreign keys enforced. Uses
`modernc.org/sqlite` (pure Go, no CGO). Migrations managed by `golang-migrate`
under `server/internal/db/migrations/`.

## Testing

The project follows a test-first approach. All logic is covered before shipping, the Go test
runner is always invoked with `-race` to surface data races, and every test gets its own
ephemeral database — there is no shared state between test cases.

### Test layers

| Layer | What it covers | Stack | Location |
|---|---|---|---|
| **Unit** | Individual packages: auth, DB, certificates, API handlers, protocol codec, agentapi, relay | Go `testing` + testify / Rust `#[test]` + proptest | `server/internal/*/` · `agent/crates/*/` |
| **Integration** | HTTP round-trips with real SQLite; QUIC agent lifecycle (connect, register, heartbeat, disconnect) with in-process server | Go `httptest` + real QUIC + live SQLite | `server/tests/integration/` |
| **Golden (cross-language)** | MessagePack wire format is bit-identical between the Rust encoder and Go decoder | Rust generates binary fixtures; Go verifies them | `testdata/golden/` |
| **Web** | React components and state hooks | Vitest + React Testing Library | `web/src/` |

### Running tests locally

```bash
make test               # All tests — Rust + Go + Web
make test-go            # Go server (unit + integration) with race detector
make test-integration   # Integration suite only
make test-rust          # Rust workspace
make test-web           # React / TypeScript
make test-coverage      # Go coverage report printed to stdout
make golden             # Regenerate golden fixtures and verify cross-language compat
```

### CI pipeline flow

Every push to `dev` or `main` and every pull request targeting `main` or `dev` runs the
CI pipeline. A daily schedule (`0 6 * * *`) also triggers all jobs for CodeQL and security
scanning. Benchmarks run in a separate workflow, triggered automatically when CI succeeds
on `dev`.

```
                        CI Workflow
push → dev|main  /  pull_request → main|dev  /  schedule
                │
     ┌──────────┼──────────────────────────────┐
     ▼          ▼          ▼          ▼         ▼
   Rust        Go         Web      Security   CodeQL         (parallel)
   ├─ lint      ├─ lint    ├─ test   ├─ govulncheck  ├─ Go
   ├─ test      ├─ unit    └─ build  ├─ cargo audit  ├─ TypeScript
   │            └─ integration       └─ npm audit    └─ Rust
   └─ generate golden files
         │
         ▼
   Golden verification   (needs Rust — consumes artifact)
         │
         └──────── all 11 jobs must pass ────────┐
                                                  ▼
                                     Auto-merge dev → main
                                     └─ Update coverage badge

                     Benchmarks Workflow
              (triggered by CI success on dev)
                          │
                          ▼
                    Go Benchmarks
                          │
                          ▼
                   Rust Benchmarks
                          │
                    stored in gh-pages
```

### CI pipeline jobs

The CI workflow contains **12 jobs** grouped by concern:

| Group | Jobs | Purpose |
|-------|------|---------|
| **Rust** | `rust-lint`, `rust-test` | fmt + clippy, nextest + golden file generation |
| **Go** | `go-lint`, `go-unit`, `go-integration` | go vet, unit tests with coverage, QUIC integration tests |
| **Web** | `web` | Vitest + Vite build |
| **Golden** | `golden` | Cross-language wire format verification (needs `rust-test` artifact) |
| **Security** | `security-audit` | govulncheck (Go), cargo audit (Rust), npm audit (Web) |
| **CodeQL** | `codeql-go`, `codeql-js`, `codeql-rust` | GitHub Code Scanning with `security-and-quality` queries |
| **Merge** | `merge-to-main` | Auto-merge `dev` → `main` after all 11 gate jobs pass |

The Benchmarks workflow contains **2 jobs** (`bench-go` → `bench-rust`) that run sequentially,
triggered by `workflow_run` when CI completes successfully on `dev`.

The **golden verification** job is sequenced after Rust so the Go verifier always works against
freshly generated fixtures — this prevents Rust ↔ Go wire-format drift from going undetected.
Pull requests execute every job except auto-merge. Benchmarks only run on `dev` pushes.

The Go unit test job enforces a **70% minimum coverage** threshold — the build fails if
coverage of production code drops below this level. Test utilities (`testutil/`) are excluded
from the coverage calculation.

Each CI job posts a native Markdown summary (pass/fail counts, failed test names) to the
GitHub Actions job summary tab for quick triage without digging into logs.

### Performance benchmarks

CI tracks performance of hot paths across commits to catch regressions. Benchmarks run on
every push to `dev` and results are stored in the `gh-pages` branch via
[github-action-benchmark](https://github.com/benchmark-action/github-action-benchmark).

| Language | What's benchmarked | Tool |
|----------|--------------------|------|
| Go | Protocol codec, cert signing, DB operations, handshake | `testing.B` + `-benchmem` |
| Rust | Frame encode/decode, handshake encode/decode | Criterion 0.5 |

Regression threshold: **110%** — the bench job fails if any benchmark is more than 10% slower
than the stored baseline. Historical charts are viewable on GitHub Pages.

### Branch protection

Both long-lived branches are protected:

| Branch | Rules |
|--------|-------|
| `main` | No force pushes, no deletion |
| `dev` | All 11 gate jobs must pass before PR merge; no force pushes, no deletion |

Direct pushes to `dev` are allowed (for daily development), but PRs (including Dependabot)
require all status checks to pass before merging.

### Dependency management

[Dependabot](.github/dependabot.yml) checks all four ecosystems (Go, Cargo, npm, GitHub
Actions) daily and opens PRs targeting `dev`. A companion
[auto-merge workflow](.github/workflows/dependabot-auto-merge.yml) approves and squash-merges
Dependabot PRs automatically after CI passes, keeping dependencies current without manual
review.

### Security scanning

Three layers of automated security analysis run on every CI trigger:

- **CodeQL** — static analysis for Go, TypeScript, and Rust with `security-and-quality` queries;
  also runs on a daily schedule to catch newly disclosed patterns
- **Vulnerability scanners** — `govulncheck` (Go), `cargo audit` (Rust), `npm audit` (Web)
  check dependencies against known vulnerability databases
- **Dependabot** — daily dependency updates to minimize exposure window

### Key dependencies

| Component | Dependency | Purpose |
|-----------|-----------|---------|
| Go | `chi/v5` | HTTP router |
| Go | `golang-jwt/v5` | JWT authentication |
| Go | `golang-migrate/v4` | Database migrations |
| Go | `quic-go` v0.49 | QUIC transport for agents |
| Go | `modernc.org/sqlite` | Pure-Go SQLite driver |
| Go | `vmihailenco/msgpack/v5` | MessagePack codec |
| Rust | `quinn` 0.11 | QUIC transport |
| Rust | `rustls` 0.23 | TLS implementation |
| Rust | `rcgen` 0.13 | Certificate generation |
| Rust | `rmp-serde` 1 | MessagePack codec |
| Rust | `tokio` 1 | Async runtime |
| Rust | `criterion` 0.5 | Benchmarking (dev) |
| Web | React 19 | UI framework |
| Web | Zustand | State management |
| Web | Tailwind CSS 4 | Styling |
