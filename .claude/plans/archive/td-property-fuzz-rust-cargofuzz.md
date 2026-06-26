# Micro-Plan: Rust Fuzzing (`cargo-fuzz`) for `mesh-protocol` decode

**Parent:** `td-property-fuzz-testing-expansion.md` (track 2 of 3). **Register:**
[techdebt.md](../../techdebt.md) — "Test-technique gaps". **Branch:** `dev`. **Owner:** Rust.

## 1. Goal

A `cargo-fuzz` (libFuzzer) target over the wire decoder
[`Frame::decode`](../../../agent/crates/mesh-protocol/src/codec.rs#L56) + a committed seed
corpus, complementing the existing `proptest`
([property_test.rs](../../../agent/crates/mesh-protocol/tests/property_test.rs)). Decode
takes `&[u8]`, so the target feeds raw bytes — no `Arbitrary` impl needed.

## 2. Scope

**In:** a fuzz crate + `decode` target; a seed corpus; a **stable-toolchain
corpus-replay regression** that runs in the gauntlet.
**Out:** fuzzing higher-level session logic; an unbounded CI fuzz session.

## 3. File inventory

| File | Change |
|---|---|
| `agent/fuzz/Cargo.toml` | **New** cargo-fuzz crate (`cargo fuzz init` layout), depends on `mesh-protocol`. |
| `agent/fuzz/fuzz_targets/decode.rs` | **New.** `fuzz_target!(|data: &[u8]| { let _ = mesh_protocol::Frame::decode(data); })` — assert no panic/UB. |
| `agent/fuzz/corpus/decode/*` | **New** seed corpus (reuse the `proptest`/golden byte vectors as seeds). |
| `agent/crates/mesh-protocol/tests/decode_corpus_test.rs` | **New** stable regression: read every file in the corpus dir + any committed crash input, feed through `Frame::decode`, assert no panic. Runs in plain `cargo test`. |
| `Makefile` | a `fuzz` target (`cargo +nightly fuzz run decode -- -runs=<N>`), bounded. |
| `.github/workflows/` | gate the bounded fuzz run to a nightly-toolchain step (or nightly cron); the **corpus-replay regression** runs on stable in the normal test job. |

## 4. Determinism (mandatory — [tests-determinism.md](../../rules/tests-determinism.md))

- The gauntlet/stable job runs **only** the corpus-replay regression (`cargo test`) —
  always-run, deterministic, no nightly needed.
- The libFuzzer target runs **bounded** (`-runs=N` or `-max_total_time`), never
  open-ended; on a separate nightly-toolchain job (libFuzzer needs nightly).
- Any crash input is minimized and **committed** under the corpus as a regression
  fixture (so the stable replay re-runs it forever).

## 5. Approach (TDD)

1. `cargo fuzz init` under `agent/fuzz`; write the `decode` target.
2. Seed the corpus from existing golden/proptest byte vectors.
3. Add `decode_corpus_test.rs` (red if `Frame::decode` panics on any seed) — fix any
   panic in the decoder (don't weaken the assertion).
4. Wire the bounded fuzz `make` target + nightly job; keep the stable replay in the
   gauntlet.
5. `/precommit` → commit → `/refactor` → `/precommit` → commit → push.

## 6. Acceptance criteria / DoD

- [ ] `cargo +nightly fuzz run decode -- -runs=<N>` builds and runs bounded locally.
- [ ] `decode_corpus_test.rs` runs in the gauntlet on **stable**, replaying the corpus +
      any crash fixtures, asserting no panic.
- [ ] Any crash found is fixed and its input committed as a corpus fixture.
- [ ] No unbounded fuzz session in CI; nightly job (if added) is time/`runs`-bounded.
- [ ] `/precommit` green.

## 7. NFRs

- **Security/robustness:** decoder hardened against arbitrary/adversarial bytes (the
  agent's network attack surface).
- **Maintainability:** corpus + crash fixtures document edge cases.
- **Performance:** stable gauntlet stays fast (replay only); fuzzing is nightly.

## 8. Reviewer/QA checklist

- [ ] Stable gauntlet does **not** require nightly (replay test only).
- [ ] Fuzz target asserts no panic on `Frame::decode`; corpus seeded from real vectors.
- [ ] Crash inputs (if any) committed as fixtures and pass the replay.
- [ ] Nightly fuzz job (if added) is bounded.

## 9. Risks

- libFuzzer requires a nightly toolchain — keep it off the stable critical path (the
  replay regression is the always-run guard); the nightly job is best-effort.
