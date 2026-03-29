# Implementation Phases

<!-- Last updated: 2026-03-29 -->
<!-- Update this file after completing or starting any significant phase of work. -->

## ✅ Completed

| Phase | Summary | Key Paths | Plan |
|-------|---------|-----------|------|
| Phase 0: Scaffolding | Rust workspace, Go module, React/Vite app, CI, Makefile | `/`, `Makefile`, `.github/` | — |
| Phase 1: Shared Protocol | mesh-protocol crate (Rust) + internal/protocol (Go), golden file cross-lang tests | `crates/mesh-protocol/`, `server/internal/protocol/`, `testdata/golden/` | — |
| Phase 2: Server Infrastructure | SQLite WAL db (golang-migrate, 6 tables), ECDSA P-256 CA, mTLS, TLS 1.3 | `server/internal/db/`, `server/internal/cert/` | — |
| Phase 3: HTTP API + Auth | chi v5 router, JWT+bcrypt, REST handlers (health/auth/devices/groups/users), integration tests | `server/internal/api/` | — |
| Phase 4: Agent Connections | QUIC mTLS (quic-go v0.48), relay, agentapi handshake + conn lifecycle, Rust agent-core | `server/internal/agentapi/`, `server/internal/relay/`, `crates/agent-core/` | — |
| Phase 5: Platform Traits | ScreenCapture/InputInjector/ServiceLifecycle traits, platform-linux (systemd, X11), platform-windows stubs | `crates/platform-linux/`, `crates/platform-windows/` | [phase-4-platform-agents.md](plans/phase-4-platform-agents.md) |
| Phase 6: Session & WS Relay API | Session CRUD, WebSocket relay handler, side detection | `server/internal/api/session*.go`, `server/internal/relay/` | [phase-6-session-relay.md](plans/phase-6-session-relay.md) |
| Phase 7: Web Client UI | SessionView with tabs (desktop/terminal/files/chat), Zustand stores | `web/src/` | — |
| Phase 8: Web Client Perf & DRY | Performance and DRY improvements across web client | `web/src/` | [phase-8-refactoring.md](plans/phase-8-refactoring.md) |
| Phase 9: Agent Session + WebRTC | Terminal forwarding, file ops, signaling state machine, WebRTC transport | `crates/agent-core/src/session/`, `server/internal/signaling/` | [phase-9-session-webrtc.md](plans/phase-9-session-webrtc.md) |
| Phase 10: Admin + Notifications | VAPID web push, admin dashboard, service worker | `server/internal/api/admin*.go`, `web/src/admin/` | [phase-10-admin-notifications.md](plans/phase-10-admin-notifications.md) |
| Phase 11: Intel AMT MPS | CIRA/APF over TLS, RSA 2048 certs, amt_devices table | `server/internal/mps/`, `server/internal/db/migrations/` | [phase-11-mps-amt.md](plans/phase-11-mps-amt.md) |
| Phase 12: Integration & E2E Tests | Playwright E2E (22 tests), Go integration expansion, Docker Compose env, k6 load tests, CI gating | `tests/e2e/`, `server/tests/`, `docker-compose.test.yml` | [phase-12-e2e-tests.md](plans/phase-12-e2e-tests.md) |
| CD Phase A–C: Oracle Cloud Deployment | ARM64 Always Free VPS, Docker Compose + Caddy, GitHub Actions CD, staging/prod, auto-tag/changelog, aarch64-musl cross-compilation | `.github/workflows/`, `deploy/` | — |

---

## 🔄 In Progress

| Phase | Summary | Plan | Notes |
|-------|---------|------|-------|
| Linux Agent: No Tray | Linux agent = Terminal + FileManager only; remove tray, IPC, display detection | [linux-agent-no-tray.md](plans/linux-agent-no-tray.md) | |
| Broad Codebase Refactoring | 12 work items across 3 phases: DRY, module organisation, polish | [broad-refactoring-plan.md](plans/broad-refactoring-plan.md) | |
| CI Auto-Tag + Changelog | Auto-tag on merge to main, generate changelog, publish agent binaries | [agent-binary-release.md](plans/agent-binary-release.md) | |
| X11 Screen Capture + Chat Echo | JPEG capture via x11rb + chat message echo from agent | [screen-capture-chat.md](plans/screen-capture-chat.md) | Need to verify plan name |
| Test Coverage Gaps | 5 gaps: relay frames, agent handler, middleware+WS, WebRTC signaling, OTA E2E | [test-coverage-gaps.md](plans/test-coverage-gaps.md) | Need to verify plan name |

---

## ⏳ Planned

| Phase | Summary | Priority | Notes |
|-------|---------|----------|-------|
| Phase D: Monitoring & Observability | Grafana, Prometheus, alerting, dashboards | High | [phase-d-monitoring-observability.md](plans/phase-d-monitoring-observability.md) |
| Phase 13: Multiserver & Scaling | PostgreSQL backend (pgx/v5), cross-server routing, relay pool, Kubernetes | High | |
| Phase 14: Agent Auto-Update | Signed manifest, agent polls every 6h, binary over QUIC, Ed25519, atomic rename | Medium | [phase-14-agent-auto-update.md](plans/phase-14-agent-auto-update.md) |
| Phase 15: Advanced Features | MFA/TOTP, API keys, Prometheus metrics, session recordings, group permissions CRUD | Low | |
| SonarCloud Quality Gates | Integrate SonarCloud scanning and quality gate enforcement in CI | Low | [sonarcloud-quality-gates.md](plans/sonarcloud-quality-gates.md) |
| Performance Benchmarks | Go + Rust benchmarks with regression detection in CI | Low | [performance-benchmarks.md](plans/performance-benchmarks.md) |
