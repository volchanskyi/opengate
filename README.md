# OpenGate

[![CI](https://github.com/volchanskyi/opengate/actions/workflows/ci.yml/badge.svg)](https://github.com/volchanskyi/opengate/actions/workflows/ci.yml)
[![Go Server Coverage](https://img.shields.io/endpoint?url=https://gist.githubusercontent.com/volchanskyi/<GIST_ID>/raw/opengate-coverage.json)](https://github.com/volchanskyi/opengate/actions/workflows/ci.yml)

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

## Project Structure

```
agent/     Rust workspace — protocol, agent core, platform-specific code
server/    Go module — server, database, transport, API
web/       React + TypeScript — browser client
```

## Testing

The project follows a test-first approach. All logic is covered before shipping, the Go test
runner is always invoked with `-race` to surface data races, and every test gets its own
ephemeral database — there is no shared state between test cases.

### Test layers

| Layer | What it covers | Stack | Location |
|---|---|---|---|
| **Unit** | Individual packages: auth, DB, certificates, API handlers, protocol codec | Go `testing` + testify / Rust `#[test]` + proptest | `server/internal/*/` · `agent/crates/*/tests/` |
| **Integration** | Full HTTP request → handler → real SQLite round-trips; auth flows, RBAC, concurrent access | Go `httptest` + live in-process server | `server/tests/integration/` |
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

### CI pipeline

Every push to `dev` and every pull request targeting `main` runs the full pipeline:

```
push → dev  /  pull_request → main
                │
     ┌──────────┼──────────┐
     ▼          ▼          ▼
   Rust        Go         Web        (parallel)
   ├─ fmt       ├─ vet     ├─ vitest
   ├─ clippy    ├─ unit    └─ build
   ├─ test      └─ integration
   └─ generate golden files
         │
         ▼
   Golden verification   (needs Rust — consumes artifact)
   Go verifies golden fixtures match
         │
         ▼  (push events only, after all jobs pass)
   Auto-merge dev → main
```

The **golden verification** job is sequenced after Rust so the Go verifier always works against
freshly generated fixtures — this prevents Rust ↔ Go wire-format drift from going undetected.
Pull requests execute every job except the auto-merge step.

The Go job enforces a **70% minimum unit test coverage** threshold — the build fails if
coverage of production code drops below this level. Test utilities (`testutil/`) are excluded
from the coverage calculation.
