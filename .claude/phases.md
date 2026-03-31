# Implementation Phases

<!-- Last updated: 2026-03-30 -->
<!-- Update this file after completing or starting any significant phase of work. -->

## Completed

| Phase | Summary | Version | Plan |
|-------|---------|---------|------|
| Phase 0: Scaffolding | Rust workspace, Go module, React/Vite app, CI, Makefile | — | — |
| Phase 1: Shared Protocol | mesh-protocol crate (Rust) + internal/protocol (Go), golden file cross-lang tests | — | — |
| Phase 2: Server Infrastructure | SQLite WAL db (golang-migrate, 8 tables), ECDSA P-256 CA, mTLS, TLS 1.3 | — | — |
| Phase 3: HTTP API + Auth | chi v5 router, JWT+bcrypt, REST handlers, integration tests | — | — |
| Phase 4: Agent Connections | QUIC mTLS (quic-go v0.48), relay, agentapi handshake + conn lifecycle, Rust agent-core | — | — |
| Phase 5: Platform Traits | ScreenCapture/InputInjector/ServiceLifecycle traits, platform-linux, platform-windows stubs | — | [phase-4-platform-agents.md](plans/archive/phase-4-platform-agents.md) |
| Phase 6: Session & WS Relay | Session CRUD, WebSocket relay handler, side detection | — | [phase-6-session-relay.md](plans/archive/phase-6-session-relay.md) |
| Phase 7: Web Client UI | SessionView with tabs (desktop/terminal/files/chat), Zustand stores | — | — |
| Phase 8: Web Client Perf & DRY | Performance and DRY improvements across web client | — | [phase-8-refactoring.md](plans/archive/phase-8-refactoring.md) |
| Phase 9: Agent Session + WebRTC | Terminal forwarding, file ops, signaling state machine, WebRTC transport | — | [phase-9-session-webrtc.md](plans/archive/phase-9-session-webrtc.md) |
| Phase 10: Admin + Notifications | VAPID web push, admin dashboard, service worker | — | [phase-10-admin-notifications.md](plans/archive/phase-10-admin-notifications.md) |
| Phase 11: Intel AMT MPS | CIRA/APF over TLS, RSA 2048 certs, amt_devices table | — | [phase-11-mps-amt.md](plans/archive/phase-11-mps-amt.md) |
| Phase 12: Integration & E2E Tests | Playwright E2E (22 tests/5 files), Go integration expansion, k6 load tests, CI gating | — | [phase-12-e2e-tests.md](plans/archive/phase-12-e2e-tests.md) |
| Phase 14: Agent Auto-Update | Ed25519 signed manifests, QUIC push, rollback watchdog, SELinux restorecon | v0.11.0 | [phase-14-agent-auto-update.md](plans/archive/phase-14-agent-auto-update.md) |
| Broad Codebase Refactoring | 12 work items: DRY helpers, module splits, polish | v0.7.4 | [broad-refactoring-plan.md](plans/archive/broad-refactoring-plan.md) |
| Test Coverage Gaps | 17 cross-component integration tests covering 5 gaps | v0.14.4 | — |
| File Manager + Capability Tabs | File view/download, capability-based session tabs | v0.15.0 | [file-manager-capability-detection.md](plans/archive/file-manager-capability-detection.md) |
| Display Detection Fixes | Socket probing for systemd services, probe_socket helper extraction | v0.15.2–v0.15.6 | [linux-display-detection.md](plans/archive/linux-display-detection.md) |
| X11 Screen Capture + Chat Echo | JPEG capture via x11rb + chat message echo from agent | v0.16.0 | — |
| System Tray with IPC | Desktop agent tray, dedicated desktop CI job | v0.17.0 | — |
| CD Phase A–C: Oracle Cloud Deployment | ARM64 Always Free VPS, Docker Compose + Caddy, GitHub Actions CD, staging/prod | — | — |
| CD Phase D: Monitoring & Observability | VictoriaMetrics, Grafana, Loki, Promtail, Node Exporter, Uptime Kuma, Telegram alerting | v0.13.0 | [phase-d-monitoring-observability.md](plans/archive/phase-d-monitoring-observability.md) |
| CD Phase E (partial): Cosign + Trivy | Cosign keyless signing, SBOM attestation, Trivy image scanning | v0.14.0 | — |
| CD Phase F: Agent Release Pipeline | Auto-tag, changelog, agent binary builds, release pipeline | — | [agent-binary-release.md](plans/archive/agent-binary-release.md) |
| CI Auto-Tag + Changelog | Conventional commit bumping, Keep a Changelog, SYNC_TOKEN trigger | — | [agent-binary-release.md](plans/archive/agent-binary-release.md) |
| Linux Agent: No Tray | Remove tray/IPC crates, Linux = Terminal + FileManager only, delete display detection | — | [linux-agent-no-tray.md](plans/archive/linux-agent-no-tray.md) |
| Dev Workflow Phase 1 | `.claude/` dedup — compact ADR index with wiki cross-refs, archive 47 completed plan files | — | — |
| Linux Agent: Remove Display Detection | Strip X11 capture, input injection, and related deps from platform-linux; Linux = Terminal + FileManager only | — | — |
| Agent Restart + Hardware Inventory | Restart agent from web UI (exit code 42), on-demand hardware inventory (CPU/RAM/disk/NICs) | — | [quirky-snacking-knuth.md](plans/quirky-snacking-knuth.md) |

---

## Planned

| Phase | Summary | Priority | Notes |
|-------|---------|----------|-------|
| Phase 13: Multiserver & Scaling | PostgreSQL backend (pgx/v5), cross-server routing, relay pool, Kubernetes | High | Next major milestone |
| Phase 15: Advanced Features | MFA/TOTP, API keys, Prometheus metrics, session recordings, group permissions CRUD | Low | |
| CD Phase E (remaining) | Secrets management, network policies | Deprioritized | Cosign + Trivy already done |
| CD Phase G: Testing & Retention | Testing strategy, release notes, 20-day retention policy | Low | |
| SonarCloud Quality Gates | Integrate SonarCloud scanning and quality gate enforcement in CI | Low | [sonarcloud-quality-gates.md](plans/sonarcloud-quality-gates.md) |
| Performance Benchmarks | Go + Rust benchmarks with regression detection in CI | Low | [performance-benchmarks.md](plans/performance-benchmarks.md) |
