# Strip the Windows agent implementation — keep the extension points

**Status:** specified, not started. Standalone plan (no master plan).
**Decided by:** the project owner, 2026-08-06, in two rounds of scoping questions.
**Sequencing:** this lands **first**; the edge-first micro-plan revisions are step 8 of this plan.

## Intent, in one line

Delete the agent code that *pretends* to implement Windows, and keep every place a real
Windows or macOS implementation would plug in later.

## Why

There is no engineering capacity to test an agent on Windows, and no Windows-centric agent
exists. Untested code that looks implemented is worse than absent code: it invites a reader
to believe a capability is present, it carries maintenance cost on every refactor, and it has
**never been compiled by CI** — verified below. The extension points cost nothing and Windows
or macOS remain named future capabilities, so they stay.

## Empirical baseline — measured in the tree, 2026-08-06

| Finding | Value | Method |
|---|---|---|
| Windows in CI | **absent entirely** — no runner, no target, no cross-compile | grep over `.github/workflows/`, `scripts/`, `Makefile` |
| `platform-windows` crate | 4 files, **229 lines** | `wc -l crates/platform-windows/src/*.rs` |
| Anything wiring that crate | **nothing** — `main.rs:459` calls `platform_linux::create_service_lifecycle()` unconditionally | grep for `platform_windows` outside the crate |
| Already excluded from mutation testing | yes, `.cargo/mutants.toml:29` | file read |
| Windows branches outside the crate | **20**, across 7 files | per-file counts below |
| Windows-only dependency | `windows = "0.62"`, target-gated | `platform-windows/Cargo.toml:16-17` |
| Windows Event Log reader | ~70 of `host_logs.rs`'s 684 lines | grep count |
| Docs referencing the crate path | **none** — the doc-links gate is not at risk | grep over `docs/`, `.claude/` |
| Dangling workspace exclusion | `exclude = ["crates/mesh-agent-tray"]` names a directory that **does not exist** | `agent/Cargo.toml:5`, `ls` |

Per-file branch counts outside the crate:

| File | Branches |
|---|---|
| `mesh-agent-core/src/discovery/services.rs` | 6 |
| `mesh-agent-core/src/discovery/packages.rs` | 5 |
| `mesh-agent-core/src/discovery/ports.rs` | 4 |
| `mesh-agent/src/host_logs.rs` | 2 (plus the reader body) |
| `mesh-agent-core/src/amt_detect.rs` | 1 |
| `mesh-agent-core/src/hardware.rs` | 1 |
| `mesh-agent-core/src/terminal.rs` | 1 |

## Scope — settled, do not re-litigate

**Deleted:**

- The `platform-windows` crate in full.
- All 20 Windows branches in `mesh-agent-core` and `mesh-agent`.
- The Windows-only dependency.
- The Windows Event Log reader inside `host_logs.rs`.
- `darwin` / `macos` recognition in the server's OS normaliser.
- The dangling `mesh-agent-tray` workspace exclusion (unrelated dead reference, cleaned in passing).

**Kept, untouched:**

- The device `os` field and its display.
- Per-OS agent-update manifest targeting.
- The wire vocabulary for the host-log source, **including the `windows` value**.
- The platform abstraction layer — Linux plus the do-nothing implementations, which headless,
  container and CI runs depend on.
- `windows` recognition in the server's OS normaliser.
- All eight architecture records, each gaining a short note (no new record, by decision).

**Note on one asymmetry, recorded so it is not read as an oversight:** `windows` stays in the
server's OS normaliser while `darwin` is removed. That is the owner's decision as taken; the
extension point that matters is per-OS targeting, which survives either way.

## File inventory

**Delete**

- `agent/crates/platform-windows/` — the whole crate.

**Modify — Rust**

- `agent/Cargo.toml` — drop the dangling `mesh-agent-tray` exclusion. Membership is `crates/*`,
  so removing the crate directory removes the member; **verify no explicit member list appears
  at implementation time**.
- `agent/.cargo/mutants.toml` — remove the `crates/platform-windows/**` exclusion line.
- `agent/crates/mesh-agent-core/src/platform.rs` — the module doc names `platform-windows`;
  restate it for the implementations that exist.
- `agent/crates/mesh-agent-core/src/amt_detect.rs`, `hardware.rs`, `terminal.rs`,
  `discovery/{services,packages,ports}.rs` — remove the branches, keep the existing fallbacks.
- `agent/crates/mesh-agent/src/host_logs.rs` — remove the Event Log reader and its command
  construction; keep the source dispatch (see trap 4).

**Modify — Go**

- `server/internal/osutil/normalize.go` — remove the `darwin` / `macos` cases only.
- `server/internal/osutil/normalize_test.go` — the covering test; edit **first** (TDD gate).

**Modify — docs**

- `docs/Platform-Abstraction.md`, `docs/Architecture.md`, `docs/Testing.md`,
  `docs/Agent-Updates.md`, `docs/Multiscale-Readiness.md`, `docs/Wire-Protocol.md`.
- Eight records under `docs/adr/`: 005, 020, 024, 048, 050, 051, 052, 058 — a short note each.

## Steps (TDD-first)

1. **Test first:** edit `normalize_test.go` — a `darwin` / `macOS` input now falls through to the
   default and is returned unchanged; a `windows` input still normalises to `windows`; every
   Linux distribution case still passes. Then remove the two cases from `normalize.go`.
2. **Test first:** for each of the six `mesh-agent-core` files, assert the behaviour that remains
   on Linux **before** deleting the other branch, so the deletion is proven not to change the
   Linux path. Where a file has no covering test, add one — a deletion with no assertion behind
   it is how a fallback silently changes meaning.
3. **Test first (`host_logs.rs`):** a request naming the `windows` log source returns a clean,
   counted "not available on this host" outcome — **not** a panic, **not** an empty success, and
   **not** a silent fall back to the agent's own files. The wire keeps the value, so the agent
   must answer it honestly. This is the single most important test in the plan.
4. Delete the Windows reader and the six files' branches.
5. Delete the `platform-windows` crate; remove its mutation exclusion; clean the dangling
   `mesh-agent-tray` exclusion; fix the `platform.rs` module doc.
6. Run `make dead-code` and resolve what the deletions orphan — helpers such as the Windows
   product-UUID reader become unreachable and must go with them, not linger.
7. **Docs.** Per [`docs-live-state.md`](../../rules/docs-live-state.md), describe what the system
   **is**: "the agent implements Linux; the do-nothing implementations cover headless, container
   and CI environments." Do **not** write "Windows support was removed" anywhere. The eight
   record notes follow the same rule — state that the agent implements Linux, not that something
   was taken away.
8. **Revise the edge-first plans** (this step is why the sequencing was chosen):
   - `edge-first-telemetry-and-investigations.md` — the 16/24 platform split collapses to a
     single 24; D23 (no invented Windows stall measurement) is recorded as settled but not
     currently applicable; the Windows coverage table in §6.3 goes; §6.4's rule pack drops to
     four Linux rules.
   - `edge-first-b2-vitals-contract-and-cadence.md` — one measurement count, no split.
   - `edge-first-b4-stall-vitals-psi.md` — drop the Windows-analogue reasoning and the
     non-Linux build assertion.
   - `edge-first-b5-disk-performance-vitals.md` — drop acceptance of the exact 16/24 split.
   - `edge-first-b6-event-rule-pack.md` — four rules, not seven; drop the PowerShell
     command-injection trap along with the code path it guarded.
   - Each gains one line naming what a future Windows agent would need to add.

## Traps

1. **These are runtime `cfg!` macros, not compile-time `#[cfg]` attributes** — both arms compile
   on Linux today. Deleting them is safe, but it means the *existing* Linux tests already
   exercised only one arm, so coverage will not move the way a normal deletion moves it.
2. **`make dead-code` is the real gate here**, not the compiler. Removing a branch leaves its
   helper reachable-but-unused, which compiles cleanly and rots.
3. **Mutation floors** (Rust ≥ 85 %). Deleting code changes the denominator for every remaining
   crate. Run `make mutate-rust` and confirm the floor still holds rather than assuming a
   deletion can only help.
4. **The wire vocabulary keeps `windows` deliberately.** The temptation is to also delete the
   value "since nothing produces it". Do not — it is the named extension point, and removing it
   is a protocol change with golden-file consequences that this plan explicitly avoids. The
   agent's job is to answer the value honestly, which is step 3.
5. **No golden regeneration should be needed.** If `make golden` demands one, something in the
   wire contract moved that this plan did not intend — stop and find it.
6. **Do not add a new architecture record.** Decided: notes only.
7. Coverage ≥ 80 % and PMAT grade on touched files still apply to what remains.

## Out of scope

Removing the device OS field, per-OS update targeting, the wire log-source vocabulary, the
platform abstraction layer, or `windows` from the server's OS normaliser. Any new architecture
record. Any change to the edge-first *implementation* — step 8 revises those plans as documents
only, and none of them has been built yet.

## Reviewer checklist

- [ ] `platform-windows` gone; no reference to it survives in any manifest, config or doc.
- [ ] All 20 branches removed; each file's remaining Linux path has an assertion behind it.
- [ ] A `windows` log-source request answers honestly — counted, not a panic, not a silent
      fall back to the agent's own files.
- [ ] `darwin` / `macos` cases gone from the normaliser; `windows` recognition intact.
- [ ] Dangling `mesh-agent-tray` exclusion cleaned.
- [ ] `make dead-code` clean; no orphaned helper left behind.
- [ ] `make mutate-rust` floor still met after the deletions.
- [ ] `make golden` green with **no** fixture regeneration.
- [ ] No doc or record narrates a removal; all describe the current system.
- [ ] The five edge-first plans revised, each naming what a future Windows agent would add.

## Verification

`cd agent && cargo test --workspace`, `cargo build --workspace`,
`cd server && go test ./internal/osutil/... ./internal/api/... ./tests/integration/...`,
`make dead-code`, `make mutate-rust`, `make golden`,
`GO111MODULE=off go run ./scripts/check-doc-links`, then `/precommit`.

## Close-out (mandatory)

In the commit that lands the implementation: `git mv` this plan to `archive/`, bump its internal
relative links one `../` deeper, and add the `phases.md` **Completed** row linking the archived
path.
