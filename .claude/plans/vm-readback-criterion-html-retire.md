# Retire Criterion HTML reports (benchmark trends live in VM)

**Objective:** Drop Criterion's `html_reports` feature so benchmark artifacts are
**data-only** (`new/estimates.json`), now that benchmark trends live in
VictoriaMetrics ([ADR-038](../../docs/adr/ADR-038-victoriametrics-ci-trend-store.md)).
The summarizer only ever reads `target/criterion/*/new/estimates.json`, never the
generated HTML.

**Dependencies:** none. **Ordering:** land **before/with M2** (`vm-readback-m2-benchmark-nsop-gate.md`)
— both edit [`.github/workflows/benchmark.yml`](../../.github/workflows/benchmark.yml),
so landing this first leaves a clean workflow for M2 to build on. Independent of
M0/M1/M3.

## Context

`html_reports` is a **default** Criterion feature, so removing it from a `features`
list is not enough — the workspace dep must set `default-features = false` and
re-add only `cargo_bench_support`. Legacy gh-pages benchmark HTML is already removed
by `ci.yml` (`rm -rf gh-pages/dev/bench`), so nothing to do there. After the change,
`target/criterion` has no `report/` HTML, so the existing `cp -R target/criterion/.`
already produces an HTML-free artifact — narrow it and rename it to reflect
data-only intent.

## File inventory

- **Modify** [`agent/Cargo.toml`](../../agent/Cargo.toml) (line 56, workspace dep):
  `criterion = { version = "0.8", default-features = false, features = ["cargo_bench_support"] }`.
- **Modify** [`agent/crates/mesh-protocol/Cargo.toml`](../../agent/crates/mesh-protocol/Cargo.toml)
  (line 21): replace the local `features = ["html_reports"]` override with
  `criterion.workspace = true` (mesh-agent-core already inherits the workspace dep).
- **Regenerate** [`agent/Cargo.lock`](../../agent/Cargo.lock) — `plotters` /
  `plotters-backend` / `plotters-svg` / `cast` etc. drop out; verify with
  `cargo tree -p criterion`.
- **Modify** [`.github/workflows/benchmark.yml`](../../.github/workflows/benchmark.yml)
  — narrow the `cp -R target/criterion/.` (line 92) to the `new/estimates.json`
  files the summarizer needs; rename the `bench-rust-criterion` artifact to
  data-only intent (e.g. `bench-rust-estimates`), updating **both** the upload
  (run job, ~line 97) and download (publish job, ~line 121). Preserve the
  `<bench>/new/estimates.json` layout the summarizer globs.
- **Modify** [`scripts/tests/benchmark-summarize.test.sh`](../../scripts/tests/benchmark-summarize.test.sh)
  and [`scripts/tests/benchmark-vm-push.test.sh`](../../scripts/tests/benchmark-vm-push.test.sh)
  — update any assertion referencing the old `bench-rust-criterion` artifact name /
  HTML layout.

## Steps (TDD-first)

1. **Test first:** update the two shell tests to assert the new artifact name /
   data-only estimates layout → `make shell-test` red.
2. Change the two `Cargo.toml` files; regenerate `Cargo.lock`; confirm
   `cargo tree -p criterion` no longer pulls `plotters*`/`cast`.
3. `cargo bench -p mesh-protocol --bench codec_bench` still writes
   `new/estimates.json`; [`benchmark-summarize.sh`](../../scripts/benchmark-summarize.sh)
   parses it unchanged.
4. Narrow the `benchmark.yml` copy + rename the artifact at both sites; shell tests green.

## Gotchas / constraints

- `default-features = false` is **mandatory** — `html_reports` is a *default*
  Criterion feature; dropping it from a features list alone leaves it enabled.
- Do **not** break the `<bench>/new/estimates.json` glob layout the summarizer relies on.
- [`docs/CI-Pipeline.md`](../../docs/CI-Pipeline.md) Criterion line needs **no change**
  (it never claimed HTML is published).

## Reviewer checklist

- [ ] Workspace `criterion` has `default-features = false` + `cargo_bench_support` only;
      mesh-protocol uses `criterion.workspace = true`.
- [ ] `Cargo.lock` regenerated; `plotters*` / `cast` dropped (`cargo tree -p criterion`).
- [ ] `cargo bench -p mesh-protocol --bench codec_bench` still emits `new/estimates.json`;
      summarizer parses unchanged.
- [ ] Artifact renamed at **both** upload + download; copy narrowed; estimates layout preserved.
- [ ] Shell tests updated + green; `/precommit` green.

## Verification

`make shell-test`; `cargo bench -p mesh-protocol --bench codec_bench`; `/precommit`.
