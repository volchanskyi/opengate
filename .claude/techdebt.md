# Technical Debt Register

<!-- Ordered by severity. Update when debt is introduced or paid down. -->

## Severity: High

_None currently._

## Severity: Medium

### Phase 13b k8s — server CA + VAPID keys are per-replica (blocks multi-replica)

The Helm chart (`deploy/helm/opengate`, ADR-030) mounts the server's `-data-dir`
`/data` — which holds the self-signed enrollment CA and the VAPID push keypair —
on a per-replica `ReadWriteOnce` PVC. This is correct for the **single-replica**
PR-B (the keys survive restarts), but a second replica would generate its **own**
CA + VAPID keys, so agents enrolled against replica A would fail mTLS against
replica B, and push subscriptions would split.

**Pay-down trigger:** before scaling the server past one replica (PR-C cross-server
proxy / PR-E HPA). Promote the CA + VAPID material to a shared `Secret` mounted
read-only into every replica (generated once at install, e.g. by a pre-install
Job or out-of-band like `existingSecret`), and drop the `/data` PVC to an
`emptyDir`. Until then `server.replicas` must stay `1` and a `PodDisruptionBudget`
/ HPA must not raise it.

### ADR-024 WebRTC dispatch — 3 residual uncaught mutants in `handler.rs`

`cargo mutants -p mesh-agent-core` leaves three uncaught mutants in
`session/handler.rs::handle_control`, all match-arm deletions:

- `ControlMessage::FileUploadRequest` arm — **equivalent mutant**. `FileHandler::handle_upload` only logs ("not yet implemented"); deleting the arm falls through to the `_ => debug!` branch, which is observationally identical (no frame, no state change). Killing it would require giving upload an observable side effect (e.g. an ack frame) — a business-logic change, deferred until upload is actually implemented.
- `ControlMessage::SwitchToWebRTC` arm — **live-WebRTC-stack**. Distinguishing the arm from the `_` fallthrough needs `handle_offer` to produce an answer frame, which requires a valid browser SDP offer + ICE gathering (network).
- `ControlMessage::IceCandidate` arm — **live-WebRTC-stack**. The peer-present path needs a remote description set (only `handle_offer` does that); without it, both the arm and `_` produce no observable effect.

The two cheaply-killable mutants from the original 7-mutant gap were closed with tests (`switch.rs::handle_ack` body + the `SwitchAck` dispatch arm), and the `session/handlers/webrtc.rs` bodies were added to `agent/.cargo/mutants.toml` `exclude_globs` (same live-stack rationale as the long-standing `webrtc.rs` exclusion — ADR-024 §9 merely relocated that code). These three remain because the `mutants.toml` glob mechanism cannot exclude individual match arms within an otherwise well-covered function. **Pay-down trigger:** revisit when file upload is implemented (closes the equivalent mutant) or when a headless WebRTC offer/answer harness exists (closes the two live-stack arms).

## Severity: Low

_None currently._
