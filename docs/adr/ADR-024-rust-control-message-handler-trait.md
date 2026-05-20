# ADR-024: Rust agent — `ControlMessageHandler` trait around inner `handle_control` fan-out

Date: 2026-05-19
Status: Accepted

## Context

[ADR-020](ADR-020-modular-monolith-full-hexagonal.md) adopted full hexagonal architecture across OpenGate. The Rust agent is already a 5-crate workspace (`mesh-agent-core`, `mesh-agent`, `mesh-protocol`, `platform-linux`, `platform-windows`) with mature trait-based platform abstraction. The only structural pinch-point identified is inside `mesh-agent-core`'s session-handling.

**Corrected location** (the original plan misidentified the file — verified 2026-05-19):

- [`agent/crates/mesh-agent-core/src/session/mod.rs::receive_loop`](../../agent/crates/mesh-agent-core/src/session/mod.rs) is the WebSocket message loop, **not** the dispatch.
- The actual frame-type dispatch lives at [`agent/crates/mesh-agent-core/src/session/handler.rs:17-46`](../../agent/crates/mesh-agent-core/src/session/handler.rs#L17-L46) — 30 lines, 4 outer branches (`Frame::Control`, `Frame::Terminal`, `Frame::Ping`, wildcard).
- The complexity lives in the **inner** `handle_control` fan-out: ~10 methods on `SessionHandler` — `handle_mouse_move`, `handle_mouse_click`, `handle_key_press`, `handle_file_list`, `handle_file_download`, `handle_ice_candidate`, `handle_switch_ack`, `handle_webrtc_offer`, etc.

No per-control-message trait exists today. The fan-out is implemented as a flat `match` on `ControlMessage` variants calling methods directly on `SessionHandler`.

Mutation score on `mesh-agent-core` is **89.5%** per [`.github/workflows/mutation.yml`](../../.github/workflows/mutation.yml). The 85% floor must be preserved through the carve-up.

## Decision

### `ControlMessageHandler` trait around the inner fan-out

Introduce a `ControlMessageHandler` trait that owns the ~10 control-message methods. Each `ControlMessage` variant routes through the trait. Implementations can be grouped by domain:

```rust
pub(crate) trait ControlMessageHandler: Send + Sync {
    async fn handle(
        &self,
        msg: ControlMessage,
        ctx: &mut HandlerContext<'_>,
    ) -> Result<(), SessionError>;
}
```

`HandlerContext` carries the per-frame dependencies (`InputInjector`, `FrameSender`, `FileOpsHandler`, `Option<&TerminalHandle>`, `Arc<Mutex<Option<Arc<AgentPeerConnection>>>>`). It is constructed once per `handle_frame` call and passed to whichever handler the dispatcher selects.

Initial grouped impls (subject to refinement during implementation):

| Impl | Covers |
|---|---|
| `MouseHandler` | `MouseMove`, `MouseClick`, `MouseWheel` |
| `KeyboardHandler` | `KeyPress`, `KeyRelease`, `KeyCombo` |
| `FileHandler` | `FileList`, `FileDownload`, `FileUpload` |
| `WebRTCHandler` | `WebRTCOffer`, `WebRTCAnswer`, `IceCandidate` |
| `SwitchHandler` | `SwitchAck` (and any future channel-switch messages) |
| `TerminalControlHandler` | terminal-control variants not handled by the `Frame::Terminal` branch |

The dispatcher in `handle_control` becomes a single `match` selecting which handler to invoke. Methods that today live as `SessionHandler::handle_*` move into the trait implementations.

### Outer frame dispatch stays a thin multiplexer

The 4-branch outer `handle_frame` ([`handler.rs:17-46`](../../agent/crates/mesh-agent-core/src/session/handler.rs#L17-L46)) stays as-is. Three of its four branches are 1-3 lines (Terminal forwarding, Ping/Pong, wildcard log). The fourth fans into `ControlMessageHandler`. **The outer dispatch does not become a trait** — earned-port rule from [ADR-020](ADR-020-modular-monolith-full-hexagonal.md) is not satisfied at the outer layer (insufficient implementations, no isolation need).

### Mutation-score preservation gate

The carve-up is staged through tests:

1. **Before any method moves**: add tests covering every `ControlMessage` variant if not already present (some are sparse today — `WebRTCAnswer`, `FileUpload`). Verify the current 89.5% baseline holds.
2. **Per-impl extraction**: move one impl group at a time. Re-run `cargo mutants --workspace --package mesh-agent-core` on each PR; the score may NOT drop below 89.5%. CI gate already enforces the 85% floor; the per-PR review enforces the no-regression rule against the current baseline.
3. **Final integration**: once all ~10 methods live behind the trait, `cargo mutants` runs on the full `mesh-agent-core` crate. Score must equal or exceed 89.5%.

If a mid-extraction score drops, the offending PR rebuilds tests before merging. The TDD gate ([`.claude/hooks/pretooluse-tdd-gate.sh`](../../.claude/hooks/pretooluse-tdd-gate.sh)) backs this up by requiring a test change before any source-file edit on the branch.

### Migration trigger

Per plan §9: the next session-protocol change is the first carve-up trigger. The user's intuition is that `MouseHandler` is the largest natural group and should land first to validate the pattern. Subsequent impls follow as protocol changes touch their methods.

`cargo-deny` ([ADR-020](ADR-020-modular-monolith-full-hexagonal.md)) gates that the new trait module does not introduce external HTTP / network crates — control-message handlers operate on local platform APIs only.

`cargo-modules` snapshot at `agent/crates/mesh-agent-core/tests/module-graph.snap` will record the new `session::control` submodule when introduced. CI fails on unreviewed snapshot diffs.

## Out of scope

- **No outer-frame `FrameHandler` trait.** Only the inner `handle_control` fan-out becomes a trait; the 4-branch outer dispatch stays as code.
- **No re-architecture of `SessionHandler` itself.** It remains the lifecycle owner — the trait extracts the handlers it dispatches to, not its lifecycle.
- **No async-trait removal.** The agent already uses `async_trait` (or native async-fn-in-trait depending on the MSRV); this ADR follows the existing convention rather than re-litigating it.
- **No platform-layer change.** `InputInjector`, `FileOpsHandler`, `TerminalHandle` are imported by the new handlers exactly as they are today.
- **No protocol change.** Wire format (MessagePack, `Frame` / `ControlMessage` enums) is untouched. The carve-up is purely internal.

## Consequences

**Positive.**

- The ~10-method fan-out becomes 5–6 grouped trait implementations. Each impl is small enough to read at a glance.
- Adding a new control-message variant is a self-contained PR — one new impl + the dispatcher entry, no growth of `SessionHandler`'s method surface.
- Tests can target individual handlers without instantiating the full `SessionHandler`. Isolated tests reduce mutation-test runtime per PR.
- Future platform-specific overrides (e.g. a Linux-only `XdotoolMouseHandler`) become trait swaps, not conditional compilation inside one big method.

**Accepted trade-offs.**

- `HandlerContext` is a new struct with five fields. Its lifetime threading needs care — owned `&mut` reference passed for the duration of the call.
- Initial PR pays for ~10 test additions plus a working baseline of `cargo mutants` against the new code. Heavier than a typical session-protocol change.
- Trait dispatch adds a vtable call per control message. Hot path is mouse-move at desktop frame rate — measured before/after; the PR rejects the change if the benchmark regresses > 5%.

## References

- Plan: [`.claude/plans/modular-monolith-evaluation.md`](../../.claude/plans/modular-monolith-evaluation.md) §4.2 (corrected hotspot location), §6 (mutation-score guard pitfall)
- Upstream: [ADR-020](ADR-020-modular-monolith-full-hexagonal.md) — earned-port rule, module-level CI gates
- Critical files: [`agent/crates/mesh-agent-core/src/session/handler.rs`](../../agent/crates/mesh-agent-core/src/session/handler.rs), [`agent/crates/mesh-agent-core/src/session/mod.rs`](../../agent/crates/mesh-agent-core/src/session/mod.rs)
- Mutation-score history: [`.github/workflows/mutation.yml`](../../.github/workflows/mutation.yml)
- TDD enforcement: [`.claude/hooks/pretooluse-tdd-gate.sh`](../../.claude/hooks/pretooluse-tdd-gate.sh)
