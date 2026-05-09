# Implementation Phases

<!-- Last updated: 2026-05-06 -->
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
| Agent Restart + Hardware Inventory | Restart agent from web UI (exit code 42), on-demand hardware inventory (CPU/RAM/disk/NICs) | — | — |
| SonarCloud Quality Gates | SonarCloud scanning, 3-language coverage, SARIF export, merge gating | — | [sonarcloud-quality-gates.md](plans/archive/sonarcloud-quality-gates.md) |
| Dev Workflow Phase 3 | `/observe` skill — PromQL, LogQL, container health, WSL agent diagnostics, investigation playbooks | — | [developer-workflow-enhancement.md](plans/archive/developer-workflow-enhancement.md) |
| Device Logs | On-demand log retrieval via QUIC control path, DB caching, REST API, React UI with filtering/pagination | — | [device-logs-feature.md](plans/archive/device-logs-feature.md) |
| Coverage 80% + Quality Gates | Go/Web coverage to 80%, CI thresholds, precommit enforcement, 3-language coverage badges, SonarCloud hard-fail | — | [coverage-80-flip-gates.md](plans/archive/coverage-80-flip-gates.md) |
| Phase 13a: PostgreSQL Migration | Fresh-start cutover from SQLite to PostgreSQL 17 (pgx/v5 stdlib), native types (TIMESTAMPTZ/UUID/JSONB), colocated Docker service, pg_dump backups, postgres_exporter monitoring | v0.24.0 | [phase-13a-postgres-migration.md](plans/archive/phase-13a-postgres-migration.md) |
| Test Coverage Phase A: Targeted Gap-Closers | 10 cross-boundary ControlMessage goldens (session/file/chat/agent_update), 5 edge-case goldens (empty/UTF-8/>64KiB/forward-compat/LE-length negative), Device Logs and capability-tabs E2E specs | — | [tests-coverage-phase-a-targeted-gaps.md](plans/archive/tests-coverage-phase-a-targeted-gaps.md) |
| Structural Testing PR 1: Tooling install | Makefile targets (`mutate`, `taint-go/web`, `dead-code`), web devDeps (stryker, eslint-plugin-security/no-unsanitized, ts-prune), `web/stryker.config.json`, `web/eslint.security.config.js`, `server/.gosec.json`. Developer-facing only; no CI gates | — | [enhance-audit-skills-with-structural-testing.md](plans/enhance-audit-skills-with-structural-testing.md) |
| Structural Testing PR 2: Skill SKILL.md patches | `precommit` (mutation-diff gate, taint, dead-code as new lint steps); `tests-audit` (Section 0.5 mutation analysis); `backend-audit` (taint-paths section + renumbering); `frontend-audit` (DOM taint-paths section + renumbering); `infra-audit` (sensitive-value flow trace); `refactor` (slice-before-you-cut + dead-code sweep); `admin-infra-oci` (post-deployment control-flow validation) | — | [enhance-audit-skills-with-structural-testing.md](plans/enhance-audit-skills-with-structural-testing.md) |
| Structural Testing PR 3: Dead-code baseline cleanup | Rust 0 / Go 4 / TS 11 baseline → all clean. Removed unused `dbStore` interface (meshserver/main.go), `generateAgentCert` test helper (handshaker_test.go), `newTestServerWithAgents` (helpers_test.go), `result` shadow type (loadtest/main.go), and unused `src/test/test-utils.tsx`. Stripped `export` on module-private types (chat-store/connection-store/toast-store/webrtc-transport). Marked protocol-surface types with `// ts-prune-ignore-next-line`. Fixed `make dead-code` to scan via `tsconfig.app.json` and skip generated `src/types/api.d.ts` | — | [enhance-audit-skills-with-structural-testing.md](plans/enhance-audit-skills-with-structural-testing.md) |
| Structural Testing PR 4: gosec baseline cleanup (Go server) | Baseline 56 findings → 0. Fixes: G104 (32) explicit `_ = ` discard or `// #nosec` on best-effort phase transitions / cleanup paths; G115 (8) bounds checks + `clampNonNegativeUint32` / `clampInt64` helpers + `maxAPFPayload` cap; G304 (5) `filepath.Clean` + `// #nosec` justification on operator-supplied paths; G117 (2) `// #nosec` justification on persisted VAPID/signing key files; G301 (2) MkdirAll perms 0755 → 0750; G306 (2) WriteFile perms 0644 → 0600; G401/G501 (2) `//nolint:gosec` → `// #nosec G401/G501` for RFC-7616 Digest auth MD5. Fixed `.gosec.json`: removed `audit: enabled` (was reporting suppressed findings) and `nosec: false` (was being misinterpreted) — minimal config now honors annotations | — | [enhance-audit-skills-with-structural-testing.md](plans/enhance-audit-skills-with-structural-testing.md) |
| Structural Testing PR 5: ESLint security baseline cleanup (web) | Registered `eslint-plugin-security` (`detect-object-injection`, `detect-non-literal-fs-filename`, `detect-non-literal-regexp`, `detect-unsafe-regex`, etc.) and `eslint-plugin-no-unsanitized` in `eslint.config.js` at error severity. Cleared 8 baseline findings (all `security/detect-object-injection` false positives on bracket access): refactored typed Record lookups to switch statements (StatusBadge, SessionToolbar), `forEach` with `arr.at()` (Breadcrumbs), `Uint8Array.from` (NotificationCenter), `Object.fromEntries(filter)` instead of `delete obj[key]` (file-store), bounded array index with `Math.min` + `arr.at()` (DeviceDetail). Added `coverage/`, `reports/` to globalIgnores. `npm run lint` clean, 435 tests pass | — | [enhance-audit-skills-with-structural-testing.md](plans/enhance-audit-skills-with-structural-testing.md) |
| Structural Testing PR 6: Rust mutation-test gap closure (agent) | cargo-mutants baseline 46% (152 caught / 177 missed / 26 unviable / 355 total) → 76.6% (252/76/26/355) by closing test gaps in 11 files. New tests: exhaustive `key_to_bytes` table for terminal_handle.rs (kills 64 match-arm mutants); codec.rs boundary tests (MAX_FRAME_SIZE, decode-incomplete `needed`, single-byte ping/pong); handshake.rs AgentProof short/min payload; logs.rs level_severity table + parse_log_line timestamp validation + matches_filter time boundary inclusivity + discover_log_files truncation; identity.rs partial-state regenerate; file_ops.rs absolute-size CHUNK_SIZE pin; connection.rs AsyncControlStream r/w/x reaches underlying stream + reconnect_backoff no-trailing-sleep + receive_control bounds check; terminal.rs pty_reader/stdin_writer with stub Read/Write; session/handler.rs match-arm dispatch coverage (mouse click, key press, terminal resize, file download, ICE, switch ack, file upload silent ack); session/relay.rs capture_loop error counting + ws_writer_loop forwarding (function generalized over `Sink<Message>` for testability); webrtc.rs store_channel_by_label routing. Carve-outs in `agent/.cargo/mutants.toml`: platform shims, `mesh-agent/src/main.rs` entry point, `restore_selinux_context`. Golden tests support `OPENGATE_GOLDEN_DIR` so cargo-mutants' temp tree resolves fixtures. `make mutate-rust` sets the env automatically | — | [enhance-audit-skills-with-structural-testing.md](plans/enhance-audit-skills-with-structural-testing.md) |
| Structural Testing PR 7: Go mutation-test gap closure (server) | gremlins baseline (with Postgres test DB up + carve-outs for openapi_gen / cmd-meshserver / loadtest / testutil): 567 K / 68 L / 7 T / 140 NC / 782 total = 73.4% caught → **77.9% (602 K / 33 L / 7 T / 140 NC)**. 35 LIVED killed (efficacy 89.3% → 94.8%). New tests: cert validity-period pinning (kills all 10 ARITHMETIC_BASE on `5*time.Minute` / `365*24*time.Hour` / `10y` for CA/Agent/Server/MPS/AgentCSR); relay `msgs_copied` log assertion via captureHandler (kills `count++`); api server `MetricsRegistry`/`Metrics` wiring test (kills `!= nil` checks at api.go:131,139); agentapi `clampNonNegativeUint32` / `clampInt64` boundary tests; agentapi UpdateAck DB-state assertion (kills `msg.Success != nil` mutation that would silently mis-persist outcome); apf.go boundary battery (10 sub-tests for `strLen > maxAPFStringLen` at 6 read-points + ParseGlobalRequest off-boundary + ParseChannelOpen off+12 + ParseChannelData len-4 with error-message distinguishing + WriteChannelData maxAPFPayload + readChannelData 1MiB cap + writeStringMsg + ReadGlobalRequest/ChannelOpen name-dispatch); converters `Offset+Limit == total` no-more pin; install-script `len(prefix) > 0` empty-context test; protocol MaxFrameSize literal pin; vapid private-key 32-byte pin (kills `len(privBytes) < 32` padding loop). Carve-outs in `server/.gremlins.yaml`: openapi_gen, cmd-meshserver/main, tests/loadtest/main, internal/testutil. `make mutate-go` warns if `POSTGRES_TEST_URL` is unset (otherwise api/db tests skip and ~290 mutants drop to NOT COVERED) | — | [enhance-audit-skills-with-structural-testing.md](plans/enhance-audit-skills-with-structural-testing.md) |

---

## In Progress

_None currently._

---

## Planned

| Phase | Summary | Priority | Notes |
|-------|---------|----------|-------|
| Phase 13b: Multiserver & Scaling | Cross-server routing, relay pool, Kubernetes | High | Deferred from Phase 13 until after 13a |
| Phase 15: Advanced Features | MFA/TOTP, API keys, Prometheus metrics, session recordings, group permissions CRUD | Low | |
| CD Phase E (remaining) | Secrets management, network policies | Deprioritized | Cosign + Trivy already done |
| CD Phase G: Testing & Retention | Testing strategy, release notes, 20-day retention policy | Low | |
| Performance Benchmarks | Go + Rust benchmarks with regression detection in CI | Low | [performance-benchmarks.md](plans/performance-benchmarks.md) |
