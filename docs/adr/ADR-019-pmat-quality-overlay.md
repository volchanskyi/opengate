# ADR-019: PMAT adoption as augment-only quality overlay (MCP + precommit + daily Grafana)

Date: 2026-05-19
Status: Accepted

## Context

OpenGate already enforces quality densely: 23 checks in [`scripts/precommit-gauntlet.sh`](../../scripts/precommit-gauntlet.sh) per commit, 20+ CI jobs, SonarCloud as the merge-to-main blocker ([ADR-012](../Architecture-Decision-Records.md)), three-engine mutation testing regression-gated to 85% ([`.github/workflows/mutation.yml`](../../.github/workflows/mutation.yml)), CodeQL on Rust/Go/JS, and no-bypass TDD + commit-identity + refactor-marker hooks ([`.claude/hooks/`](../../.claude/hooks/)). Any new quality tool must (1) earn its slot without duplicating existing gates and (2) respect the no-suppression rule ([`.claude/rules/sonarcloud.md`](../../.claude/rules/sonarcloud.md)) and the no-bypass-hook rules ([`.claude/rules/git.md`](../../.claude/rules/git.md), [`.claude/rules/tdd.md`](../../.claude/rules/tdd.md)).

The Pragmatic Multi-language Agent Toolkit ([`paiml/paiml-mcp-agent-toolkit`](https://github.com/paiml/paiml-mcp-agent-toolkit)) is a Rust CLI + MCP server that produces static-analysis reports (TDG letter grade, 0–289 repo score, churn / entropy / duplication / fault overlays) across 25+ Tree-sitter languages including OpenGate's three. The evaluation in [`.claude/plans/pmat-adoption-evaluation.md`](../../.claude/plans/pmat-adoption-evaluation.md) verified each capability against the upstream repo and mapped them onto OpenGate's existing infrastructure. Four genuine gaps emerged:

1. **Code churn / entropy / hotspot overlay** — not tracked today.
2. **Single-letter TDG summary** — SonarCloud rates issues, not a file-level letter grade.
3. **Quality-trend timeseries** — mutation scores already flow to Loki; TDG/repo-score do not.
4. **MCP-exposed quality reports for Claude Code** — no MCP server currently serves quality data.

Everything else PMAT offers overlaps with an existing gate (lints, audits, secrets, IaC, coverage, mutation, taint, dead-code, smells, hotspots, dup detection).

The plan's [`§6 resolved decisions`](../../.claude/plans/pmat-adoption-evaluation.md) chose the strict end of every threshold tradeoff. This ADR codifies those values.

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
5. Push to Loki (existing observability).
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

Reviewed one quarter after this ADR's first opportunistic-trigger landing (see [plan §9](../../.claude/plans/pmat-adoption-evaluation.md)). PMAT is retired if all of the following hold:

- Grafana repo-score / TDG trend is flat or noise — no actionable signal.
- Hotspot follow-up PRs are < 50% within the quarter — the hotspot signal is not driving change.
- MCP tool calls reading PMAT data in routine Claude Code sessions are near zero — the surface is unused.

If any one of those signals is healthy, PMAT keeps its slot.

## References

- Plan: [`.claude/plans/pmat-adoption-evaluation.md`](../../.claude/plans/pmat-adoption-evaluation.md) (the plan's `§6` and `§7` carry the resolved-decision rationale and verification metrics)
- Paired evaluation: [`.claude/plans/modular-monolith-evaluation.md`](../../.claude/plans/modular-monolith-evaluation.md) (baseline-timing dependency)
- Upstream: [github.com/paiml/paiml-mcp-agent-toolkit](https://github.com/paiml/paiml-mcp-agent-toolkit), [paiml.github.io/pmat-book](https://paiml.github.io/pmat-book/)
- Template workflow: [`.github/workflows/mutation.yml`](../../.github/workflows/mutation.yml)
- Constraint sources: [`.claude/rules/sonarcloud.md`](../../.claude/rules/sonarcloud.md), [`.claude/rules/git.md`](../../.claude/rules/git.md), [`.claude/rules/tdd.md`](../../.claude/rules/tdd.md)

## Note on numbering

The originating plan refers to this ADR as "ADR-023" because it nominally slots after a five-ADR modular-monolith sequence (018–022) that has not been written. ADR-018 currently records the OCI Bastion decision and the modular-monolith ADRs remain proposed. Per [`.claude/rules/plans-and-adrs.md`](../../.claude/rules/plans-and-adrs.md) ("next sequential number"), this ADR lands as **ADR-019**. When the modular-monolith ADRs land, they will take subsequent numbers; no renumbering of accepted ADRs occurs.
