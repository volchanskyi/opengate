# Future Plan: Full Agent E2E Testing

## Overview

True end-to-end tests with a **real Rust agent connecting to the server** via QUIC, verified through Playwright browser tests. Blocked until a runnable agent binary exists.

## Prerequisites (from future phases)

1. **Agent binary crate** — Create `agent/crates/mesh-agent/` with `main.rs` that wires together `mesh-agent-core` + `platform-linux` into a runnable binary
2. **Agent Dockerfile** — Multi-stage Rust build (builder stage compiles, final stage = minimal Alpine/scratch image)
3. **Agent CLI flags** — `--server-url`, `--data-dir`, `--hostname` (for deterministic test identity)

## Test Environment: `docker-compose.e2e-full.yml`

```yaml
services:
  server:
    build: { context: .., dockerfile: Dockerfile }
    # same as docker-compose.test.yml
    tmpfs: [/data]
    ports: ["8080:8080", "9090:9090/udp"]

  agent:
    build: { context: ../agent, dockerfile: Dockerfile }
    command: ["--server-url", "server:9090", "--data-dir", "/data", "--hostname", "e2e-agent-1"]
    depends_on:
      server: { condition: service_healthy }
    tmpfs: [/data]
    # Agent needs UDP access to server:9090 for QUIC
    # Docker Compose default network handles this
```

## E2E Test Scenarios (Playwright + real agent)

| Spec | Tests |
|------|-------|
| `e2e/agent-device.spec.ts` | Agent connects → device appears in device list → status = online |
| `e2e/agent-session.spec.ts` | Create session → WebSocket relay established → data flows browser ↔ agent |
| `e2e/agent-terminal.spec.ts` | Open terminal tab → type command → see output (PTY via agent) |
| `e2e/agent-files.spec.ts` | Open file manager → browse agent filesystem → download a file |
| `e2e/agent-reconnect.spec.ts` | `docker compose restart agent` → device goes offline → reconnects → back online |
| `e2e/agent-disconnect.spec.ts` | `docker compose stop agent` → device goes offline → session cleaned up |

## Key Technical Challenges

1. **Cert bootstrapping** — Agent needs the server CA cert to establish mTLS. Options:
   - Agent discovers CA via an unauthenticated enrollment endpoint (like ACME)
   - Shared volume between server and agent containers (`/certs/ca.crt`)
   - Server generates agent cert on first QUIC connection (current model — agent presents unsigned cert, server signs it)

2. **QUIC in Docker** — UDP between containers works on Docker's default bridge network. No special config needed.

3. **Test timing** — Agent registration is async. Playwright tests need `page.waitForSelector` with timeouts to wait for the device to appear after agent starts.

4. **Agent shutdown** — Tests that verify disconnect behavior need `docker compose stop agent` from the test script (via Playwright's `test.beforeAll` running a shell command).

## Implementation Estimate

- Agent binary crate: ~1 day (wire existing libraries)
- Agent Dockerfile: ~0.5 day
- docker-compose.e2e-full.yml: ~0.5 day
- 6 Playwright specs: ~2-3 days
- CI job for full E2E: ~0.5 day
- **Total: ~5 days** (after agent binary exists)

## When to Execute

After Phase 14 (Agent Auto-Update) when the agent binary is fully functional. The agent binary is a prerequisite — without it, this plan is blocked.

## Builds On

- Phase 12 `docker-compose.test.yml` infrastructure
- Phase 12 Playwright setup (config, fixtures, helpers)
- Phase 12 CI `e2e` job pattern
