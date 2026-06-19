# ADR-019: PMAT adoption as augment-only quality overlay (MCP + precommit + daily Grafana)

Date: 2026-05-19
Status: Accepted; trend-store mechanics superseded by ADR-038

## Context

OpenGate already enforces quality densely: 23 checks in [`scripts/precommit-gauntlet.sh`](../../scripts/precommit-gauntlet.sh) per commit, 20+ CI jobs, SonarCloud as the merge-to-main blocker ([ADR-012](../Architecture-Decision-Records.md)), three-engine mutation testing regression-gated to 85% ([`.github/workflows/mutation.yml`](../../.github/workflows/mutation.yml)), CodeQL on Rust/Go/JS, and no-bypass TDD + commit-identity + refactor-marker hooks ([`.claude/hooks/`](../../.claude/hooks/)). Any new quality tool must (1) earn its slot without duplicating existing gates and (2) respect the no-suppression rule ([`.claude/rules/sonarcloud.md`](../../.claude/rules/sonarcloud.md)) and the no-bypass-hook rules ([`.claude/rules/git.md`](../../.claude/rules/git.md), [`.claude/rules/tdd.md`](../../.claude/rules/tdd.md)).

The Pragmatic Multi-language Agent Toolkit ([`paiml/paiml-mcp-agent-toolkit`](https://github.com/paiml/paiml-mcp-agent-toolkit)) is a Rust CLI + MCP server that produces static-analysis reports (TDG letter grade, 0–289 repo score, churn / entropy / duplication / fault overlays) across 25+ Tree-sitter languages including OpenGate's three. The evaluation in [`.claude/plans/pmat-adoption-evaluation.md`](../../.claude/plans/archive/pmat-adoption-evaluation.md) verified each capability against the upstream repo and mapped them onto OpenGate's existing infrastructure. Four genuine gaps emerged:

1. **Code churn / entropy / hotspot overlay** — not tracked today.
2. **Single-letter TDG summary** — SonarCloud rates issues, not a file-level letter grade.
3. **Quality-trend timeseries** — mutation scores already flow to VictoriaMetrics; TDG/repo-score do not.
4. **MCP-exposed quality reports for Claude Code** — no MCP server currently serves quality data.

Everything else PMAT offers overlaps with an existing gate (lints, audits, secrets, IaC, coverage, mutation, taint, dead-code, smells, hotspots, dup detection).

The plan's [`§6 resolved decisions`](../../.claude/plans/archive/pmat-adoption-evaluation.md) chose the strict end of every threshold tradeoff. This ADR codifies those values.

## Decision

Adopt PMAT as an **augment-only** quality overlay at three separately-togglable integration points. No existing gate is replaced. No Kaizen auto-write mode is enabled. Pinned to **exact version `pmat@3.17.0`** — patch updates go through `dev` like any other dependency.

### Integration point 1 — MCP server for Claude Code

Register `pmat mcp serve` in [`.claude/settings.json`](../../.claude/settings.json) with a curated 7-tool read-only allow-list:

1. TDG grade lookup
2. Repo score
3. Churn query
4. Entropy query
5. Duplicates query
6. Faults query
7. Git-history RAG

Tools that write outside an ephemeral report directory — notably any `kaizen --commit` variant — are not exposed.

### Integration point 2 — precommit-gauntlet step

Append a single fast check to [`scripts/precommit-gauntlet.sh`](../../scripts/precommit-gauntlet.sh):

```
pmat tdg --since-commit HEAD~1 --threshold B+
```

Runs on changed files only. Fails the commit if any changed file's TDG grade drops below **B+** from day one. Appended last so failures don't mask faster checks. Same no-suppression policy as SonarCloud — exceptions require an ADR reference, not an inline suppression directive.

Not added: `pmat repo-score` or `pmat mutate` at commit time — too slow per commit and overlapping with existing gates.

### Integration point 3 — daily analytics workflow → Grafana

New CI workflow `.github/workflows/pmat-trend.yml`, modelled on [`.github/workflows/mutation.yml`](../../.github/workflows/mutation.yml). **Nightly full-repo every night** (off-peak cron). Steps:

1. `pmat tdg` → file-level grades.
2. `pmat repo-score` → 0–289 number plus per-category breakdown.
3. `pmat query --churn --entropy --duplicates --faults` → hotspot ranks.
4. Reshape into the existing mutation-workflow line-protocol shape.
5. Push numeric series to VictoriaMetrics through the shared CI-trend transport.
6. Grafana panels: TDG distribution over time, repo-score trend, top-N hotspots by churn × entropy.

Telegram alert on regression — either condition fires (same alert scoping as terraform-drift, commit `678dda3`):

- Repo-score drop **≥ 3 points** day-over-day.
- **Any single file** whose TDG grade drops below B+ since the previous run.

### Resolved thresholds (from plan §6)

| # | Concern | Value |
|---|---|---|
| 1 | TDG floor (precommit) | **B+** from day one |
| 2 | Repo-score alert | **≥ 3 points** drop day-over-day |
| 3 | Version pin | **`pmat@3.17.0`** exact (no patch auto-update) |
| 4 | MCP tool allow-list | 7 tools (above) |
| 5 | Workflow cadence | **Nightly full-repo every night** |
| 6 | Baseline timing | Establish before the first modular-monolith ADR lands; re-baseline at the end of each modular-monolith phase |
| 7 | Single-file TDG-slip alert | Below B+ since previous run |

## Out of scope (explicit non-goals)

- **Not replacing existing tools.** cargo-mutants / gremlins / stryker remain the mutation engines. SonarCloud remains the merge-to-main quality gate. Lints, security audits, secrets, IaC, coverage, taint, dead-code, dup detection — all stay as configured.
- **Not enabling Kaizen auto-write.** `pmat kaizen --commit --push` conflicts with the no-bypass commit-guard hooks: it would commit as the CI user (commit-identity guard), write source-file edits ahead of any test (TDD gate), and push ahead of the refactor marker (push guard). Disabling those hooks is forbidden. Kaizen runs in `--dry-run` mode only; fixes ride the normal TDD → `/precommit` → commit → `/refactor` → push flow.
- **Not adopting `pmat comply check --strict`** until its rule set is reviewed against the project's no-suppression policy.
- **Not adopting PMAT's Design-by-Contract feature** — semantics not concretely documented enough to commit to.
- **Not creating per-module PMAT gates** until the modular-monolith ADRs land and package boundaries stabilize.
- **No ML reproducibility scaffolding.** There is no ML in this project; the source research's `SEED=42` claim is discarded.

## Consequences

**Positive.**
- Fills four real measurement gaps without removing any existing gate.
- Single-letter TDG grade gives reviewers a fast at-a-glance quality signal SonarCloud doesn't produce.
- Hotspot data (churn × entropy × faults) gives Claude Code and humans an actionable pre-screen for risky areas before edits land.
- Grafana trend timeseries make quality regression visible alongside the existing mutation-score panel.

**Accepted trade-offs (resolved at the strict end deliberately).**
- First-week alert flurry expected. B+ floor + ≥3-pt repo-score sensitivity + single-file TDG-slip alert are all strict from day one rather than ramped.
- CI minutes higher than a delta-only nightly. Full-repo every night was chosen for consistency with the single-file alert (a delta scan would miss slips on untouched files).
- Commit friction higher than starting at a `C` floor and raising. Strict-from-day-one was chosen to catch regressions immediately.
- Manual version bumps required for PMAT patch releases (exact-pin policy).
- Overlap noise risk: PMAT and SonarCloud may report duplication / complexity with different counts. Mitigated by gating precommit on TDG letter grade only (not individual issue counts); daily Grafana workflow is observability, not enforcement; SonarCloud remains the sole merge-to-main blocker.
- Modular-monolith refactor will spike churn and may shift TDG grades as package boundaries move. Baseline before the first modular-monolith ADR lands; re-baseline at each phase end. Regressions traceable to that refactor are excused by ADR cross-reference in the PR description, not by a PMAT inline suppression.

## Retirement criterion

Reviewed one quarter after this ADR's first opportunistic-trigger landing (see [plan §9](../../.claude/plans/archive/pmat-adoption-evaluation.md)). PMAT is retired if all of the following hold:

- Grafana repo-score / TDG trend is flat or noise — no actionable signal.
- Hotspot follow-up PRs are < 50% within the quarter — the hotspot signal is not driving change.
- MCP tool calls reading PMAT data in routine Claude Code sessions are near zero — the surface is unused.

If any one of those signals is healthy, PMAT keeps its slot.

## Amendments

The decisions above are unchanged; these amendments record the verified
implementation mechanics and one scope narrowing. (Formerly standalone ADR-028
and ADR-032, consolidated here when per-file ADRs became mutable —
[ADR-036](ADR-036-mutable-adrs-current-state-doctrine.md).)

### Amendment 1 — PMAT 3.17.0 CLI / MCP surface mapping (2026-05-31)

The integration points above were written against an *anticipated* CLI/MCP
surface. Several literal prescriptions do not exist in the pinned `pmat@3.17.0`,
so the realised mechanics differ while the decisions hold. The exact-version pin
makes this mapping load-bearing: a future `pmat` bump must re-verify it.

**Integration point 2 — precommit gate.** `pmat tdg --since-commit HEAD~1
--threshold B+` does not run (`--since-commit` is absent; `--threshold` is a
*complexity* knob for `--explain`, not a grade floor). The implemented form is
`pmat tdg check-quality -p <file> --min-grade B+ --fail-on-violation`, one
invocation per changed code file (`check-quality` exits 3 on a violation).
[`scripts/pmat-precommit.sh`](../../scripts/pmat-precommit.sh) resolves the
changed set in git (union of `origin/dev..HEAD` + staged + unstaged + untracked)
and scopes it to **changed code files including tests, excluding generated**
([`scripts/tdd-check.sh`](../../scripts/tdd-check.sh) `is-code`). `build.rs` is
included (hand-written `.rs`); pre-existing below-B+ files block only once
touched (Clean-as-You-Code). No inline suppressions.

**Integration point 1 — MCP read-only allow-list.** `pmat serve --mode mcp`
exposes its full ~21-tool surface with no server-side filter, so the read-only
allow-list is enforced in [`.claude/settings.json`](../../.claude/settings.json)
(`enabledMcpjsonServers: ["pmat"]`; 7 read-only tools in `permissions.allow`;
file-writers in `permissions.deny`). The realised 7 tools: `analyze_complexity`
(TDG is CLI-only; complexity is the closest read-only proxy), `analyze_code_churn`,
`analyze_satd` (faults), `analyze_duplicates_vectorized`, `analyze_dead_code`,
`analyze_deep_context` (git-history RAG), `get_server_info`. Repo-score and
entropy have no MCP tool in 3.17.0 (CLI-only) and are surfaced via the nightly
workflow. Denied file-writers: `scaffold_project`, `generate_template`,
`generate_enhanced_report`.

**Integration point 3 — nightly workflow.** `repo-score` reports on a **0–100**
scale (with a letter grade) in 3.17.0, not 0–289. The previous repo-score and
below-B+ count are read from VictoriaMetrics before the current push through
[`scripts/pmat-vm-query.sh`](../../scripts/pmat-vm-query.sh), following the
canonical trend-store decision in
[ADR-038](ADR-038-victoriametrics-ci-trend-store.md). The single-file-TDG-slip
alert (threshold #7) is implemented as the below-B+ **count rising**
day-over-day; exact per-file enforcement is the precommit gate.

**Pre-decomposition baseline** (threshold #6), `dev` HEAD `d3f0373`: repo score
**64.5 / 100 (grade C)** — the low categories (Pre-commit Hooks 0/20, PMAT
Compliance 2.5/5) are artifacts of pmat's own checklist looking for a
`.pre-commit-config.yaml`/`.pmat-gates.toml` that OpenGate deliberately does not
use, so the absolute number is a weak signal and the day-over-day trend is the
actionable part. Files below B+: 8 production + ~36 server test files; `web/src`
clean.

### Amendment 2 — TDG gate excludes gofmt-only Go test files (2026-06-03)

Narrows the Integration-point-2 changed-set: **drop any Go test file
(`*_test.go`) whose only change versus the baseline is gofmt formatting.**
Everything else is unchanged.

Trigger: the gauntlet's `go fmt` gate forced reformatting 36 gofmt-drifted Go
files, which pulled 13 fmt-only `*_test.go` below the B+ floor into scope and
blocked the commit with no logic change. Empirically those test files are maxed
on every TDG component except `duplication_ratio` (the only lever being harmful
test de-duplication), and `pmat tdg --explain` itself skips test files — so
grading them on a fmt-only diff enforces a stricter scope than the tool
analyses, for no quality gain.

Detector `pmat_is_gofmt_only_test` in
[`scripts/pmat-precommit.sh`](../../scripts/pmat-precommit.sh): exclude a file
iff it is `*_test.go` **and** `gofmt(baseline_blob) == gofmt(current_worktree)`.
Symmetric, so any real edit keeps the file graded. Fail-safe: a non-test file, a
new file (no baseline), or an unavailable `gofmt` all grade the file; `gofmt` is
injectable via `GOFMT_BIN`. Unit-tested in
[`scripts/tests/pmat-precommit.test.sh`](../../scripts/tests/pmat-precommit.test.sh).

**Source files are never exempt** — a fmt-only change to a `.go` *source* file is
still graded. In the same change, `apf.go` and `mps.go` (C+ 66.2) were lifted to
B+ by genuine refactoring (deduplicating APF read/write helpers, then splitting
each oversized file by concern, since `structural_complexity` is file-size
driven), and two pre-existing errcheck suppressions were replaced with explicit
`_ =` closures. Test quality stays governed by the TDD gate, the ≥80% coverage
thresholds, and the test-determinism gate
([ADR-029](ADR-029-test-determinism-no-silent-skips.md)).

## References

- Plan: [`.claude/plans/pmat-adoption-evaluation.md`](../../.claude/plans/archive/pmat-adoption-evaluation.md) (the plan's `§6` and `§7` carry the resolved-decision rationale and verification metrics)
- Paired evaluation: [`.claude/plans/modular-monolith-evaluation.md`](../../.claude/plans/archive/modular-monolith-evaluation.md) (baseline-timing dependency)
- Upstream: [github.com/paiml/paiml-mcp-agent-toolkit](https://github.com/paiml/paiml-mcp-agent-toolkit), [paiml.github.io/pmat-book](https://paiml.github.io/pmat-book/)
- Template workflow: [`.github/workflows/mutation.yml`](../../.github/workflows/mutation.yml)
- Constraint sources: [`.claude/rules/sonarcloud.md`](../../.claude/rules/sonarcloud.md), [`.claude/rules/git.md`](../../.claude/rules/git.md), [`.claude/rules/tdd.md`](../../.claude/rules/tdd.md)

## Note on numbering

The originating plan refers to this ADR as "ADR-023" because it nominally slots after a five-ADR modular-monolith sequence (018–022) that has not been written. ADR-018 currently records the OCI Bastion decision and the modular-monolith ADRs remain proposed. Per [`.claude/rules/plans-and-adrs.md`](../../.claude/rules/plans-and-adrs.md) ("next sequential number"), this ADR lands as **ADR-019**. When the modular-monolith ADRs land, they will take subsequent numbers; no renumbering of accepted ADRs occurs.
