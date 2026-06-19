# Micro-Plan: Agent Reconnect Flap-Guard + Jitter

**Register entry:** [techdebt.md](../../techdebt.md) — "Agent reconnect lacks backoff
after a post-register server drop (storm-readiness)."
**Master:** `techdebt-paydown-master.md`. **Branch:** `dev`. **Owner:** agent (Rust).

## 1. Problem (proven)

- [reconnect_with_backoff](../../../agent/crates/mesh-agent-core/src/connection.rs)
  escalates 1→2→4→8→16→30s **only within one invocation** and has **no jitter**.
- The main loop ([main.rs](../../../agent/crates/mesh-agent/src/main.rs)) calls
  it fresh on every cycle. A connect that **succeeds then drops** (e.g. server accepts
  the mTLS handshake then closes — observed live during an admin device-deletion)
  returns `Ok` on attempt 1 with no sleep, then `continue`s and connects again
  immediately → ~8 reconnects/sec. Backoff never accrues because each *connect*
  succeeds.
- [Multiscale-Readiness §4](../../../docs/Multiscale-Readiness.md) explicitly requires
  "connection-attempt backoff **and jitter**"; jitter is absent today.

## 2. Scope

**In:**
1. **Jitter** on the backoff schedule (de-synchronise a reconnecting herd).
2. **Flap-guard:** when a *registered* connection drops within a short stability
   window, apply backoff at the session level instead of reconnecting immediately;
   reset only after a connection stays up past the window.

**Out:** changing the "exit after 10 failed attempts → systemd restart" model (that is
the supervised-restart contract); server-side changes; the `0x14`/full handshake logic.

## 3. Design (as built)

One **shared pure seam** `full_jitter(base, cap, exp, &mut rng)` is the single
tested RNG dependency; both the connect-failure backoff and the flap-guard call
it.

- **`full_jitter`** — AWS "full jitter": uniform-random delay in
  `[0, min(cap, base·2^exp)]`. Full jitter (not equal/decorrelated) de-synchronises
  a reconnecting herd most aggressively — the §4 concern. RNG injected
  (`&mut R: Rng`): tests seed `StdRng`; production passes `rand::rng()`. `exp` is
  clamped (`MAX_BACKOFF_SHIFT`) so `1u32 << exp` cannot overflow.
- **`ReconnectGovernor`** — holds `flap_count` + schedule constants (`DEFAULT_BASE`
  1s, `DEFAULT_CAP` 30s, `DEFAULT_STABILITY_WINDOW` 5s). `record_disconnect(elapsed,
  &mut rng)` returns `Some(jittered_backoff)` for a sub-window session (escalates
  `flap_count`) or `None` for a stable session (`>= window`, resets `flap_count`).
- **`reconnect_with_backoff`** now jitters its connect-retry delay via `full_jitter`
  (was a fixed-doubling `delay`).

**Placement in `main.rs`:** the observed incident reached the control loop (server
sent `AgentDeregistered`, handled there) — i.e. registration *succeeded* then the
session dropped. So `connected_at = Instant::now()` is stamped at register success
and the flap-guard sits at the **bottom of the outer loop**, reached only when the
inner control loop breaks (`break 'outer` shutdown paths skip it). The fast-path
handshake/register-send `continue` paths keep their existing single immediate
retry — out of scope per §2 (handshake logic) and not the path the incident took.

## File inventory (as built)

| File | Change |
|---|---|
| `Cargo.toml` (workspace) + both agent-crate `Cargo.toml` | `rand = "0.9"` (`rand.workspace = true`). |
| `agent/crates/mesh-agent-core/src/connection.rs` | `full_jitter` + `ReconnectGovernor`; jittered `reconnect_with_backoff`; unit tests (seeded `StdRng`). |
| `agent/crates/mesh-agent-core/src/lib.rs` | Export `full_jitter`, `ReconnectGovernor`. |
| `agent/crates/mesh-agent/src/main.rs` | Governor + rng before the loop; `connected_at` at register success; flap-guard at loop bottom. |
| `agent/crates/mesh-agent/tests/connection_test.rs` | Integration: accept-then-drop loop always backs off; resets on a stable session. |
| `docs/Multiscale-Readiness.md` §4 + `.claude/techdebt.md` | Record landed; clear the debt entry. |

## 4. Approach (TDD) — done

1. **Tests first** (test files before source):
   - integration `reconnect_governor_rate_limits_accept_then_drop_loop`.
   - unit `full_jitter_stays_within_bounds` / `full_jitter_respects_cap`.
   - unit `governor_backs_off_short_sessions_and_resets_on_stable` +
     `governor_rate_limits_accept_then_drop_storm`.
   - **Kept** `reconnect_backoff_does_not_sleep_after_last_attempt` green (last
     attempt never sleeps, jitter or not).
2. Implemented `full_jitter` + `ReconnectGovernor`; rewired `reconnect_with_backoff`.
3. Wired the governor into `main.rs`.
4. Updated doc + techdebt; staged **only** these files (parallel-engineer WIP present).
5. `/precommit` → commit → `/refactor` → `/precommit` → commit → push.

## 5. Quality metrics / acceptance

- [ ] Accept-then-drop loop is **rate-limited** to ≤ a small bound (e.g. the backoff
      schedule), proven by the integration test (no ~8/s spin).
- [ ] Jitter randomises delays within documented bounds; RNG seam keeps tests
      deterministic (no `t.Skip`-style nondeterminism — [tests-determinism](../../rules/tests-determinism.md)).
- [ ] A stable session resets backoff (no permanent penalty after recovery).
- [ ] Existing backoff/disconnect tests still pass.
- [ ] Full `/precommit` gauntlet green.

## 6. NFRs

- **Availability / storm control:** bounds self-inflicted reconnect rate under
  accept-then-drop and de-synchronises a herd after a node restart (the §4 concern).
- **Performance:** fewer wasted dials/handshakes during pathological churn.
- **Maintainability:** jitter via an injected RNG seam keeps the function pure and
  deterministically testable; flap state is local to the outer loop.

## 7. Reviewer checklist

- [ ] Jitter RNG is injectable; no wall-clock-sensitive assertions (deterministic test).
- [ ] Stability window + caps are named constants with a one-line rationale.
- [ ] Flap counter resets on a genuinely stable session (verify the reset path).
- [ ] No change to the 10-attempt exit contract or handshake selection.
- [ ] Tests fail pre-impl, pass post-impl.

## 8. Risks

- Too-long a stability window could delay legitimate fast reconnects after a brief blip;
  5s is a starting point — tune against a load-test if one exists.
- Coordinate ordering with `td-agent-session-resumption-cache.md` (same files: main.rs
  connect path); land one, rebase the other.
