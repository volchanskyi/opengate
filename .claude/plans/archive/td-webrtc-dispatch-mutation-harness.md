# Micro-Plan: Close the 3 Residual WebRTC-Dispatch Mutants

**Register entry:** [techdebt.md](../../techdebt.md) — "ADR-024 WebRTC dispatch — 3
residual uncaught mutants in `handler.rs`." **Master:** `techdebt-paydown-master.md`.
**Branch:** `dev`. **Owner:** agent (Rust). **Status:** conditional — confirm §2 first.

## 1. Problem (exact)

`cargo mutants -p mesh-agent-core` leaves 3 uncaught match-arm-deletion mutants in
[`handler.rs::handle_control`](../../../agent/crates/mesh-agent-core/src/session/handler.rs).
The arms today dispatch directly:

- `FileUploadRequest { path, total_size }` → `FileHandler::handle_upload(&path, total_size)`
  — **equivalent mutant**: `handle_upload` only logs ("not yet implemented"), so deleting
  the arm is observationally identical to the `_ => debug!` fallthrough.
- `SwitchToWebRTC { sdp_offer }` → `WebRTCHandler::handle_offer(self.ice_servers.clone(),
  sdp_offer, frame_tx, webrtc_pc)` — **live-stack**: no observable effect without a real
  SDP/ICE peer.
- `IceCandidate { candidate, mid }` → `WebRTCHandler::handle_candidate(webrtc_pc,
  &candidate, &mid)` — **live-stack**: no observable effect without a remote description.

`exclude_globs` in [`mutants.toml`](../../../agent/.cargo/mutants.toml) cannot exclude
individual match arms inside an otherwise-covered function.

## 2. Decision required (confirm before implementing)

| Option | What | Recommend |
|---|---|---|
| **A. Testable dispatch seam** | Inject a `WebRtcDispatch` trait so a mock records the call; kills the two live-stack arm mutants without a live WebRTC stack. | **Yes** (live-stack arms) |
| **B. Headless WebRTC harness** | In-test `webrtc-rs` peer producing a real offer/ICE. | No (high cost/flake) |
| **C. Defer FileUpload** | The equivalent mutant closes when upload gains an observable effect (ack frame) — i.e. when the feature lands. | **Yes** (defer that arm) |

**Plan of record: A + C.** This is also a maintainability win (dispatch becomes unit
testable), not just a mutation-score chase.

## 3. Implementation (Option A — concrete seam)

The repo already uses `async_trait` for object-safe traits
([platform.rs:52](../../../agent/crates/mesh-agent-core/src/platform.rs#L52)) — mirror it.

| File | Change |
|---|---|
| `agent/crates/mesh-agent-core/src/session/handler.rs` | Add `#[async_trait::async_trait] pub trait WebRtcDispatch: Send + Sync { async fn offer(&self, ice_servers, sdp_offer, frame_tx, webrtc_pc); async fn candidate(&self, webrtc_pc, candidate, mid); }`. Add `struct RealWebRtcDispatch` delegating to [`WebRTCHandler::handle_offer`/`handle_candidate`](../../../agent/crates/mesh-agent-core/src/session/handlers/webrtc.rs#L31). Add field `webrtc: Arc<dyn WebRtcDispatch>` to `SessionHandler`, defaulted to `RealWebRtcDispatch` in `new()` (keep `new(token, permissions)` signature — so [connection.rs:152](../../../agent/crates/mesh-agent-core/src/connection.rs#L152) is **unchanged**). Add a `#[cfg(test)]` constructor (or `with_webrtc_dispatch` builder) to inject a mock. Change the two arms to call `self.webrtc.offer(...)` / `self.webrtc.candidate(...)`. |
| `agent/crates/mesh-agent-core/src/session/handler.rs` (tests) | A recording mock `WebRtcDispatch`; tests: `SwitchToWebRTC` ⇒ `offer` called once with the sdp; `IceCandidate` ⇒ `candidate` called once with `(candidate, mid)`. |
| `agent/.cargo/mutants.toml` | Comment documenting the `FileUploadRequest` arm as an equivalent mutant deferred to upload implementation (no per-arm exclusion exists). |

`webrtc.rs` is unchanged — `RealWebRtcDispatch` wraps it; no behavior change.

## 4. Approach (TDD)

1. **Red:** add the mock + a test asserting `SwitchToWebRTC` routes to `offer`. On
   `main` the call isn't interceptable — that's the seam you introduce.
2. Add the trait + `RealWebRtcDispatch` + the field (default in `new()`); route the two
   arms through `self.webrtc`. Keep behavior identical (strengthen the covering test
   first, per the TDD refactor path).
3. Add the `IceCandidate` routing test.
4. `cargo mutants -p mesh-agent-core` — confirm the two arms are now **caught**.
5. Leave `FileUploadRequest` documented as equivalent-until-upload.
6. `/precommit` → commit → `/refactor` → `/precommit` → commit → push.

## 5. Acceptance criteria / DoD

- [ ] `cargo mutants -p mesh-agent-core` no longer reports `SwitchToWebRTC` /
      `IceCandidate` arm-deletion as **missed** (attach before/after mutant output).
- [ ] `FileUploadRequest` documented as equivalent-until-upload (not silently excluded).
- [ ] `SessionHandler::new(token, permissions)` signature unchanged — `connection.rs:152`
      untouched; production wiring uses `RealWebRtcDispatch`.
- [ ] No behavior change (existing `session/handler.rs` tests green); `/precommit` green.

## 6. NFRs

- **Maintainability:** dispatch is unit-testable without a media stack (the durable win).
- **Performance/Security:** none (test seam + pure refactor; `Arc<dyn>` indirection is
  negligible on a control-message path).

## 7. Reviewer/QA checklist

- [ ] Refactor is behavior-preserving (diff is routing + DI only; `webrtc.rs` untouched).
- [ ] No lint/quality-gate suppression added to "fix" the mutant (forbidden without
      approval — [sonarcloud.md](../../rules/sonarcloud.md)).
- [ ] Mock asserts both *that* and *with what args* each arm dispatches.
- [ ] `new()` signature unchanged; production path uses the real dispatch.
- [ ] Mutation run output (before/after) attached.

## 8. Risks

- Over-refactoring for a low-severity score — keep the trait to exactly the two methods.
- If the team prefers B (headless harness), scope it as a separate, larger plan.
