# ADR-024: Rust agent — `ControlMessageHandler` trait around inner `handle_control` fan-out

Date: 2026-05-19
Status: Accepted

## Context

[ADR-020](ADR-020-modular-monolith-full-hexagonal.md) adopted full hexagonal architecture across OpenGate. The Rust agent is already a multi-crate workspace (`mesh-agent-core`, `mesh-agent`, `mesh-protocol`, `edge-tsdb`, `platform-linux`) with mature trait-based platform abstraction — a further platform plugs in as one more `platform-*` crate implementing the same three traits. The only structural pinch-point identified is inside `mesh-agent-core`'s session-handling.

Two locations were in play (the original plan misidentified the file):

- [`session/mod.rs::receive_loop`](../../agent/crates/mesh-agent-core/src/session/mod.rs) is the WebSocket message loop, **not** the dispatch.
- The frame-type dispatch is `handle_frame` in [`session/handler.rs`](../../agent/crates/mesh-agent-core/src/session/handler.rs) — 4 outer branches (`Frame::Control`, `Frame::Terminal`, `Frame::Ping`, wildcard).
- The complexity was in the **inner** `handle_control` fan-out: ~10 methods on `SessionHandler` (`handle_mouse_move`, `handle_mouse_click`, `handle_key_press`, `handle_file_list`, `handle_file_download`, `handle_ice_candidate`, `handle_switch_ack`, `handle_webrtc_offer`, …), implemented as a flat `match` calling methods directly on `SessionHandler`, with no per-control-message trait.

The carve-up had to preserve the crate's mutation score. The regression floor and allowed drop are the `REGRESSION_FLOOR_PCT` / `REGRESSION_DROP_PP` constants in [`mutation-summarize.sh`](../../scripts/mutation-summarize.sh); the nightly run is [`mutation.yml`](../../.github/workflows/mutation.yml).

## Decision

### `ControlMessageHandler` trait around the inner fan-out

Group the ~10 control-message methods into per-domain handlers, each a documented participant in a `ControlMessageHandler` trait, living in [`session/handlers/`](../../agent/crates/mesh-agent-core/src/session/handlers/).

The trait is a **marker** — it carries no methods:

```rust
pub trait ControlMessageHandler {}
```

Dispatch stays a `match` in `handle_control` calling each handler's associated functions directly, rather than routing through a trait method over a shared `HandlerContext`. A method-carrying trait would have bought dynamic dispatch nobody needs — every variant's owner is known statically — at the cost of a context struct threading five per-frame dependencies through an object-safe signature. The marker keeps the value that was actually wanted: each group is a separate module, separately testable, and discoverable via `cargo doc`.

Grouped impls:

| Impl | Covers |
|---|---|
| `MouseHandler` | `MouseMove`, `MouseClick`, `MouseWheel` |
| `KeyboardHandler` | `KeyPress`, `KeyRelease`, `KeyCombo` |
| `FileHandler` | `FileList`, `FileDownload`, `FileUpload` |
| `WebRTCHandler` | `WebRTCOffer`, `WebRTCAnswer`, `IceCandidate` |
| `SwitchHandler` | `SwitchAck` (and any future channel-switch messages) |
| `TerminalControlHandler` | terminal-control variants not handled by the `Frame::Terminal` branch |

The `SessionHandler::handle_*` methods live in the handler modules; `handle_control` selects the owning handler per variant.

### Outer frame dispatch stays a thin multiplexer

The 4-branch outer `handle_frame` in [`handler.rs`](../../agent/crates/mesh-agent-core/src/session/handler.rs) is unchanged. Three of its four branches are 1-3 lines (Terminal forwarding, Ping/Pong, wildcard log). The fourth fans into the grouped handlers. **The outer dispatch is not a trait** — the earned-port rule from [ADR-020](ADR-020-modular-monolith-full-hexagonal.md) is not satisfied at the outer layer (insufficient implementations, no isolation need).

### Mutation-score preservation gate

Turning a match arm into a one-liner delegate moves logic out from under the tests that covered it, so the carve-up was staged: cover every `ControlMessage` variant first, extract one impl group at a time, and re-check `cargo mutants --workspace --package mesh-agent-core` on each step against the previous score, not just the floor. Each handler carries direct unit tests in its own file plus an integration test under `tests/`.

The TDD gate ([`.claude/hooks/pretooluse-tdd-gate.sh`](../../.claude/hooks/pretooluse-tdd-gate.sh)) backs this up by requiring a test change before any source-file edit on the branch.

`cargo-deny` ([ADR-020](ADR-020-modular-monolith-full-hexagonal.md)) gates that the handler modules do not introduce external HTTP / network crates — control-message handlers operate on local platform APIs only.

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

- Each per-frame dependency (`InputInjector`, frame sender, `FileOpsHandler`, `Option<&TerminalHandle>`, the WebRTC peer-connection handle) is threaded through the handler call signatures rather than bundled in one context struct — more parameters at each call site, in exchange for no lifetime-threading of a shared `&mut` context.
- The carve-up paid for ~10 test additions plus a fresh `cargo mutants` baseline against the new code. Heavier than a typical session-protocol change.
- Grouping by domain means a variant that straddles two groups (a file transfer that also drives terminal output, say) has no obvious owner and needs a judgement call at review time.

## References

- Plan: [`modular-monolith-evaluation.md`](../../.claude/plans/archive/modular-monolith-evaluation.md) §4.2 (corrected hotspot location), §6 (mutation-score guard pitfall)
- Upstream: [ADR-020](ADR-020-modular-monolith-full-hexagonal.md) — earned-port rule, module-level CI gates
- Critical files: [`session/handler.rs`](../../agent/crates/mesh-agent-core/src/session/handler.rs), [`session/handlers/`](../../agent/crates/mesh-agent-core/src/session/handlers/), [`session/mod.rs`](../../agent/crates/mesh-agent-core/src/session/mod.rs)
- Mutation-score history: VictoriaMetrics + Grafana per [ADR-038](ADR-038-victoriametrics-ci-trend-store.md); the run itself is [`mutation.yml`](../../.github/workflows/mutation.yml)
- TDD enforcement: [`.claude/hooks/pretooluse-tdd-gate.sh`](../../.claude/hooks/pretooluse-tdd-gate.sh)
