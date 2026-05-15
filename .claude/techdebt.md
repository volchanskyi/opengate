# Tech Debt Register

<!-- Last updated: 2026-05-15 -->
<!-- Update this file whenever tech debt is identified, reduced, or resolved. -->
<!-- Severity: 🔴 Critical | 🟠 High | 🟡 Medium | 🟢 Low -->

---

## 🔴 Critical

_None currently._

---

## 🟠 High

_None currently._

---

## 🟡 Medium

### QUIC Stream Ownership Workaround
- **Severity**: Medium (architectural, impacts scale)
- **Files**: `server/internal/agentapi/`, `crates/agent-core/src/connection/`
- **Issue**: Server opens the control stream instead of the agent, due to a quic-go `AcceptStream` bug with mTLS client certs. Inverts the expected ownership model.
- **Impact**: Works correctly up to ~20k concurrent agents; above that the workaround becomes a bottleneck.
- **Fix**: Revert ownership once quic-go fixes mTLS `AcceptStream`. Detailed revert steps in [plans/quic-stream-ownership-fix.md](plans/quic-stream-ownership-fix.md).
- **Identified**: Phase 4 (agent connections)

### Production-Side Clock Injection Deferred
- **Severity**: Medium (test ergonomics, not runtime risk)
- **Files**: `server/internal/` (81 `time.Now()` call sites across handlers, agentapi, signaling, relay, updater, etc.)
- **Issue**: Tests that exercise time-dependent behaviour (token expiry, idle timeouts, session deadlines, audit timestamps) cannot drive time forward deterministically — they either rely on real elapsed time (slow, flaky) or assert on inequalities (weakens the contract). Production code calls `time.Now()` directly instead of going through an injectable `Clock` interface.
- **Impact**: Time-dependent tests stay slow (`require.Eventually` poll loops dominate runtime) or under-asserted. Adding new time-dependent features means writing the same brittle pattern again.
- **Fix**: Introduce a `Clock` interface, plumb it through dependency-injection at server construction, and replace `time.Now()` call sites in the production graph. Out of scope for Phase C / C3 (81 call sites is too invasive for a test-coverage-focused PR); track here for a future dedicated refactor PR.
- **Identified**: 2026-05-15 (during Phase C / C3 plan review)

---

## 🟢 Low

### ESLint 10 Upgrade Blocked by eslint-plugin-react-hooks
- **Files**: `web/eslint.config.js`, `web/package.json`
- **Issue**: `eslint-plugin-react-hooks@7.x` peer-depends on ESLint ≤9. No stable release supports ESLint 10.
- **Impact**: Stuck on ESLint 9.x; missing new lint rules and performance improvements from ESLint 10.
- **Fix**: Upgrade once a stable `eslint-plugin-react-hooks` release adds ESLint 10 support. Comment in `eslint.config.js` documents the constraint.
- **Identified**: 2026-04-10

### TypeScript 6 Upgrade Blocked by openapi-typescript
- **Files**: `web/eslint.config.js`, `web/package.json`
- **Issue**: `openapi-typescript@7.x` peer-depends on `typescript@"^5.x"`. TypeScript 6 introduced stricter `Uint8Array` generics (already adapted in `codec.ts`).
- **Impact**: Stuck on TypeScript 5.x; missing TS 6 type-safety improvements.
- **Fix**: Upgrade once `openapi-typescript` releases a version supporting TypeScript 6. Comment in `eslint.config.js` documents the constraint.
- **Identified**: 2026-04-10

### SonarCloud Coverage Exclusions Need Integration Tests
- **Files**: `sonar-project.properties`, plus all files listed in `sonar.coverage.exclusions`
- **Issue**: Several production files are excluded from SonarCloud coverage analysis because they contain hardware-interaction, IO/transport, or bootstrap code that can't be unit-tested. Remaining exclusions include agent session/relay modules, MPS connection handling (`mps.go`), agentapi server, and UI components with complex side effects.
- **Impact**: Excluded code paths could regress without detection.
- **Progress**: Phase B / B3 (2026-05-13) retired the WSMAN carve-out: `client.go`, `operations.go`, `digest.go` are now exercised by `wsman/client_wire_test.go` and hit 85–100% per file. The pattern (extract a minimal interface for the IO dependency, fake it with `net.Pipe` for wire-level tests) is reusable for the remaining IO-only entries.
- **Fix**: Incrementally apply the same pattern to the remaining exclusions, prioritizing files with business logic over pure IO wrappers. Review exclusion list each quarter — remove entries when coverage improves.
- **Identified**: 2026-04-10

### Platform-Windows Stubs Are Cfg-Gated Only
- **Files**: `crates/platform-windows/`
- **Issue**: Windows platform code is entirely stubbed with `cfg(target_os = "windows")` — no real implementation. Builds but does nothing useful on Windows.
- **Impact**: Windows agent is non-functional for real deployments.
- **Fix**: Implement real Windows platform traits (ScreenCapture via DXGI, InputInjector via SendInput, ServiceLifecycle via SCM) as a future phase.
- **Identified**: Phase 5

### Rust ControlMessage Stub Variants With No Go Counterparts
- **Files**: `agent/crates/mesh-protocol/src/control.rs`
- **Issue**: Four `ControlMessage` variants are stubs with no corresponding Go `ControlMessageType` constants and no production callers on either side: `RequestUpdate`, `UpdateCheckResponse`, `RequestChatToken`, `ChatTokenResponse`. They were declared during earlier design iterations but never wired up end-to-end, so the Go decoder would reject them and the Rust encoder is unused.
- **Impact**: Dead variants in the protocol surface increase cognitive load and invite drift. Covered by Phase A golden-file audit — excluded from the new goldens precisely because they are non-goal. If a future feature resurrects any of them, the Go side must add a matching constant, decoder arm, and golden before the Rust encoder is turned on.
- **Fix**: Either (a) delete the unused variants when confident no imminent feature needs them, or (b) finish wiring each one to a Go decoder arm + golden test as part of the feature that actually needs it. Do **not** re-add to the Rust encoder without also closing the Go side.
- **Identified**: 2026-04-21 (Phase A test-coverage audit)
