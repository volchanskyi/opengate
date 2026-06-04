# ADR-032: TDG precommit gate excludes gofmt-only Go test files (amends ADR-019/ADR-028 scope)

Date: 2026-06-03
Status: Accepted

## Context

The precommit gauntlet gained a `go fmt` gate (`gofmt -l server` must be empty). Making it green required reformatting 36 gofmt-drifted Go files in one change. That reformatting then collided with the PMAT TDG gate: [ADR-028](ADR-028-pmat-3.17-cli-mapping.md) §"Integration point 2" set the TDG changed-set to **"changed code files including tests"**, and [`scripts/pmat-precommit.sh`](../../scripts/pmat-precommit.sh) resolves "changed" via plain `git diff` (no whitespace flag). So a formatting-only diff pulled **15 files** below the B+ floor into scope — **13 of them `*_test.go`** — and blocked the commit even though not one line of logic changed.

Empirical findings on the pinned `pmat@3.17.0` (TDG components sum to the total score; measured via `pmat tdg <file> --explain --format json`):

- The 13 flagged test files are **maxed on every TDG component except `duplication_ratio`** (structural/semantic/coupling/consistency at or near ceiling; `doc_coverage` has a ~2.5-point ceiling). The *only* lever to lift them is reducing duplication — i.e. aggressively de-duplicating test code, which conflicts with test-readability practice (DAMP over DRY).
- `pmat tdg --explain` **itself skips test files** (`Skipping test file: …`), and the gate's own remediation hint (`pmat tdg <file> --explain`) is therefore useless for those 13 files. Grading test files via `check-quality` while the tool's analysis mode excludes them is an internal inconsistency.
- `git diff -w --ignore-blank-lines` does **not** identify these as formatting-only (gofmt's alignment + blank-line normalisation survive git's whitespace flags); only a gofmt round-trip does. All 36 files were confirmed byte-identical to `gofmt(baseline)`.

Conclusion: enforcing B+ on a `*_test.go` file whose only change is gofmt formatting is enforcing a stricter scope than the tool analyses, for no quality gain, at the cost of harming test maintainability. Source files are different — production quality should be held on every touch.

## Decision

Narrow the TDG changed-set (ADR-028 §IP2): **drop any Go *test* file (`*_test.go`) whose only change versus the baseline is gofmt formatting.** Everything else in ADR-019/ADR-028 is unchanged.

- **Detector** ([`scripts/pmat-precommit.sh`](../../scripts/pmat-precommit.sh) `pmat_is_gofmt_only_test`): a file is excluded iff it is `*_test.go` **and** `gofmt(baseline_blob) == gofmt(current_worktree)`. The check is symmetric, so any real edit (which changes the formatted form) keeps the file graded.
- **Fail-safe / never silently skip:** a non-test file, a new file (no baseline blob), or an unavailable `gofmt` all return "not excluded" → the file is graded. `gofmt` is injectable via `GOFMT_BIN` for unit testing.
- **Source files are never exempt.** A fmt-only change to a `.go` *source* file is still graded — production quality is enforced on every touch. In this same change, `apf.go` (C+ 66.2) and `mps.go` (C+ 66.2) were lifted to B+ by genuine refactoring: deduplicating the APF read/write helpers, then splitting each cohesive-but-oversized file by concern (`apf.go`/`apf_messages.go`/`apf_write.go`; `mps.go`/`mps_handshake.go`/`mps_handlers.go`/`mps_conn.go`), since `structural_complexity` is file-size driven. Two pre-existing errcheck lint-suppression directives were replaced with explicit `defer func(){ _ = … }()` closures.
- Unit-tested in [`scripts/tests/pmat-precommit.test.sh`](../../scripts/tests/pmat-precommit.test.sh) (drops a gofmt-only test file; keeps a real-change test file and a fmt-only *source* file).

## Consequences

- Formatting-only commits (manual `gofmt`, and the new gauntlet `go fmt` gate) no longer trip the TDG gate on test files. The fmt-only test files in this change are excluded; the gate now grades 16 source files, all ≥ B+.
- Test quality is **not** left ungoverned: it remains covered by the TDD gate, the ≥80% coverage thresholds, and the test-determinism gate ([ADR-029](ADR-029-test-determinism-no-silent-skips.md)) — purpose-built for tests, unlike TDG's production-code metrics.
- Scope is deliberately narrow (Go + test + fmt-only). A real test edit, a non-Go test, or a source file is still graded, so the Clean-as-You-Code forcing function is preserved where it adds value.
- The exclusion adds a `gofmt` dependency to the gate's changed-set resolution; `GOFMT_BIN` keeps it injectable and the fail-safe path keeps it deterministic when gofmt is absent.

## References

- Amends: [ADR-019](ADR-019-pmat-quality-overlay.md) / [ADR-028](ADR-028-pmat-3.17-cli-mapping.md) (TDG changed-set scope; decisions otherwise unchanged)
- Related: [ADR-029](ADR-029-test-determinism-no-silent-skips.md) (test-determinism gate), [`.claude/rules/tests-determinism.md`](../../.claude/rules/tests-determinism.md)
- Implementation: [`scripts/pmat-precommit.sh`](../../scripts/pmat-precommit.sh), [`scripts/tests/pmat-precommit.test.sh`](../../scripts/tests/pmat-precommit.test.sh)
