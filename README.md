# OpenGate

[![CI](https://github.com/volchanskyi/opengate/actions/workflows/ci.yml/badge.svg)](https://github.com/volchanskyi/opengate/actions/workflows/ci.yml)

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
