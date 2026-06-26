# Index: Property-Based & Fuzz Testing Expansion (3 tracks)

**Register entry:** [techdebt.md](../../techdebt.md) — "Test-technique gaps — Go property
libs, Rust fuzz targets, web property/fuzz." **Master:** `techdebt-paydown-master.md`.
**Branch:** `dev`. **This file is now an index** — implement from the three per-language
micro-plans below (each is independently shippable; ship one track per PR).

## 1. Problem

Property/fuzz testing exists only on the wire protocol, split by language: Go fuzzing
([codec_fuzz_test.go](../../../server/internal/protocol/codec_fuzz_test.go)) and Rust
`proptest` ([property_test.rs](../../../agent/crates/mesh-protocol/tests/property_test.rs)).
Three independent gaps remain, each its own micro-plan.

## 2. Track breakdown

| Track | Micro-plan file | Adds | Primary targets |
|---|---|---|---|
| Go property | `td-property-fuzz-go-rapid.md` | `pgregory.net/rapid` | APF/AMT parsers, converters, pagination, relay framing |
| Rust fuzz | `td-property-fuzz-rust-cargofuzz.md` | `cargo-fuzz` (libFuzzer) | `mesh-protocol` `Frame::decode` + seed corpus |
| Web property | `td-property-fuzz-web-fastcheck.md` | `fast-check` | form validation, Zustand reducers, API-response handling |

## 3. Shared principle — determinism (binds all three)

Per [tests-determinism.md](../../rules/tests-determinism.md): every added test **runs
deterministically in the gauntlet** — bounded iterations + pinned seed; no
skip/build-tag/focus gating; the Rust libFuzzer target runs **bounded** (nightly) while
a **stable corpus-replay** test is the always-run guard. Counterexamples/crash inputs
are committed as regression fixtures. Prioritise parsing/boundary surfaces (highest
defect density). Fix defects the properties surface — never weaken a property to pass.

## 4. Sequencing

Independent — any order, one track per PR. Each keeps `/precommit` green per commit and
ends with its own acceptance/DoD (see the child plan).
