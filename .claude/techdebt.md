# Tech Debt Register

<!-- Last updated: 2026-04-21 -->
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

### AgentConn.sendControl Lacks Write Mutex
- **Severity**: Medium (race-prone under concurrent API requests to one agent)
- **Files**: `server/internal/agentapi/conn.go` (`sendControl`)
- **Issue**: `protocol.Codec.WriteFrame` writes the 5-byte header then the payload as two separate `quic.Stream.Write` calls. `quic.Stream.Write` is internally mutex-protected, but two concurrent goroutines calling `AgentConn.sendControl` can interleave their (header, payload) writes on the same stream, corrupting the frame envelope on the agent side.
- **Impact**: Two simultaneous API requests targeting one agent (e.g. a Restart and a hardware-report fetch fired from the same UI session) can land corrupted bytes. The agent would log a decode error and exit its control loop, which the server then reconciles as a disconnect. Rare in practice today because the same-agent API request rate is low, but the failure mode is silent and racy.
- **Fix**: Add `sync.Mutex` to `AgentConn` and lock around the `WriteFrame` call in `sendControl`. Then add the deferred Phase B / B5 case (`server/tests/integration/control_stream_faults_test.go`) for concurrent server-initiated sends — currently skipped because the test would expose this bug without the production fix.
- **Identified**: 2026-05-14 (during Phase B / B5)

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
