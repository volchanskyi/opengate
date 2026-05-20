# OpenGate — PMAT Adoption Evaluation

**Status:** Resolved — ready for ADR-023 drafting (decisions captured 2026-05-19)
**Date:** 2026-05-18 (decisions resolved 2026-05-19)
**Author:** Ivan Volchanskyi (with Claude)
**Tempo:** **Plan only.** Produces one proposed ADR (Section 8). No source, CI, or settings changes follow directly from this document.

**Predecessor plan:** [`modular-monolith-evaluation.md`](../../opengate/.claude/plans/modular-monolith-evaluation.md) in the project-local plans dir. The two plans are complementary — modular-monolith addresses **structure**; PMAT addresses **measurement and enforcement**. ADRs from this plan slot in after the modular-monolith ADR sequence (ADRs 018–022) as **ADR-023**.

---

## File-location note

The plan-mode harness pinned this file at `/home/ivan/.claude/plans/evaluate-the-whole-project-rustling-nova.md`. Per [`.claude/rules/plans-and-adrs.md`](../../opengate/.claude/rules/plans-and-adrs.md), project plans must live under `/home/ivan/opengate/.claude/plans/`. **After this plan-mode session closes, copy this file to `/home/ivan/opengate/.claude/plans/pmat-adoption-evaluation.md` before opening ADR-023.**

---

## 1. Context

The user requested an evaluation of the **Pragmatic Multi-language Agent Toolkit (PMAT)** — the `paiml/paiml-mcp-agent-toolkit` Rust crate — as a quality-enforcement overlay for OpenGate. The user supplied a research summary with capability claims; this plan **verifies each claim against the actual PMAT repository and documentation**, maps the verified capabilities onto OpenGate's existing infrastructure, and recommends a narrow adoption scope.

The evaluation is grounded in three constraints:

1. **OpenGate already enforces quality densely.** 23 checks in [`scripts/precommit-gauntlet.sh`](../../opengate/scripts/precommit-gauntlet.sh) per commit, 20+ CI jobs, SonarCloud as the merge-to-main blocker, mutation testing in all three languages, CodeQL on Go/Rust/JS, TDD + commit-identity + refactor-marker enforcement via no-bypass hooks. PMAT enters a saturated environment.
2. **The pending modular-monolith refactor (ADRs 018–022) will reshape package boundaries.** Quality measurements that bake in current shape — e.g. per-package TDG grades — will trend artificially during the refactor.
3. **The project's no-suppression rule** ([`.claude/rules/sonarcloud.md`](../../opengate/.claude/rules/sonarcloud.md)) and **no-bypass hook rules** ([`.claude/rules/git.md`](../../opengate/.claude/rules/git.md), [`.claude/rules/tdd.md`](../../opengate/.claude/rules/tdd.md)) constrain what auto-fix and gate-override behaviour can be enabled.

User's adoption decisions (asked and answered):

- **Primary use**: MCP integration **plus** precommit check **plus** daily Grafana analytics overlay.
- **Coexistence**: Augment only — no removals.
- **Auto-fix (Kaizen)**: Dry-run only; no auto-commit.
- **Tempo**: Plan only — produce one ADR, defer execution.

---

## 2. PMAT — verified capability assessment

| # | Claim from user's research | Verdict | Evidence |
|---|---|---|---|
| 1 | PMAT exists at `paiml/paiml-mcp-agent-toolkit` | **CONFIRMED** | Active repo, MIT, Rust 97.4 %, v3.17.0 (2026-05-06), 153 ★, full docs at `paiml.github.io/pmat-book/` |
| 2 | `pmat-gates.toml` config for quality gates | **CONFIRMED** | Real config format; thresholds for complexity, SATD, coverage, memory, CPU; install via `pmat tdg hooks install --backup` |
| 3 | Technical Debt Grading A+ → F over "six orthogonal metrics" | **PARTIAL** | A+/F grading confirmed; "six orthogonal metrics" referenced in marketing but the exact metric list was not exposed in the docs we fetched |
| 4 | Repository Scoring 0–289 over 11 categories | **CONFIRMED** | Real ranges; `pmat repo-score` covers TDG, Big-O, churn, clones, pattern diversity, faults |
| 5 | "30+ compliance regulations" | **UNVERIFIED** | `pmat comply check --strict` exists; the "30+" count is plausible but not corroborated |
| 6 | Mutation testing, 85 % kill-rate target | **CONFIRMED** | PMAT runs its own AST-based mutation engine (not a wrapper); 85 % documented as the "sweet spot" |
| 7 | Design by Contract with "rescue protocols" | **PARTIAL / VAGUE** | Marketing language; no concrete DbC primitives located in docs |
| 8 | CLI flags `--churn`, `--entropy`, `--duplicates`, `--faults` | **CONFIRMED** | Documented under `pmat query <search>` |
| 9 | MCP integration with RRF Git-history queries | **PARTIAL** | 19 MCP tools exposed; "Git History RAG" feature exists; specific RRF endpoints not separately confirmed but the pattern matches the docs |
| 10 | `SEED=42` for embeddings, `SEED=12345` for clustering, `docs/ml/REPRODUCIBILITY.md` | **HALLUCINATED** | No such PMAT feature. PMAT is a code-analysis tool, not an ML reproducibility tool. **Discard this claim entirely.** |
| 11 | Autonomous Kaizen with auto-fix + auto-commit | **CONFIRMED (but dangerous here)** | `pmat kaizen --commit --push` is real; **conflicts with OpenGate's no-bypass commit-guard hook** — see §5 |

**What PMAT actually is:** a Rust CLI plus MCP server that produces static-analysis reports (TDG grade, repo score, churn/entropy/duplication/fault overlays), runs its own mutation engine, exposes 19 read-only tools over MCP for use by Claude Code/Cline, and offers an opt-in `kaizen` auto-fix routine. Languages: 25+ via Tree-sitter, including Rust, Go, TypeScript — OpenGate's three. Maturity: modest-but-real (153 ★, active releases, 78-chapter handbook).

**One claim to retire:** the ML-seed claim is wrong for both PMAT and OpenGate. No ML code exists in this repo; no PMAT feature manages seeds. Treat the user's source-of-claims as partially hallucinated.

Sources: [github.com/paiml/paiml-mcp-agent-toolkit](https://github.com/paiml/paiml-mcp-agent-toolkit), [paiml.github.io/pmat-book](https://paiml.github.io/pmat-book/), [lib.rs/crates/pmat](https://lib.rs/crates/pmat).

---

## 3. PMAT vs. OpenGate — overlap matrix

| PMAT capability | OpenGate has it? | Tool / config | Overlap verdict |
|---|---|---|---|
| Linting (Rust/Go/TS) | Yes | clippy + go vet + ESLint + actionlint (gauntlet steps 1–5) | **Full overlap** — skip in PMAT |
| Security audit (deps) | Yes | govulncheck + cargo audit + npm audit (gauntlet steps 17–19) | **Full overlap** — skip in PMAT |
| Secrets scanning | Yes | gitleaks (gauntlet step 9) + [`.gitleaks.toml`](../../opengate/.gitleaks.toml) | **Full overlap** — skip in PMAT |
| IaC linting | Yes | Checkov + Hadolint + Conftest (gauntlet step 10) | **Full overlap** — skip in PMAT |
| Coverage floor | Yes | 80 %/lang enforced in gauntlet (steps 12, 14, 15) | **Full overlap** — skip in PMAT |
| Mutation testing | Yes | cargo-mutants + gremlins + stryker, 85 % regression-gated ([`.github/workflows/mutation.yml`](../../opengate/.github/workflows/mutation.yml)) | **Full overlap** — skip in PMAT |
| Code smells / bugs / vulns / hotspots | Yes | SonarCloud quality gate, merge-to-main blocker ([`sonar-project.properties`](../../opengate/sonar-project.properties)) | **Full overlap** — skip in PMAT |
| Duplication detection | Yes | SonarCloud `duplicated_blocks` metric | **Partial overlap** — SonarCloud reports; PMAT `--duplicates` gives cross-language uniform metric |
| Dead code | Yes | `make dead-code` (clippy + staticcheck + ts-prune) | **Full overlap** — skip in PMAT |
| Taint / static security | Yes | gosec + ESLint security plugins ([`web/eslint.security.config.js`](../../opengate/web/eslint.security.config.js)) | **Full overlap** — skip in PMAT |
| Dependency-direction enforcement | **No (planned)** | Planned in modular-monolith ADR-018 as go-arch-lint / eslint-plugin-boundaries | **No PMAT fit** — PMAT does not enforce architectural rules |
| SBOM / license compliance | **No** | Not present | **No PMAT fit** — PMAT does not generate SBOMs |
| Performance-regression detection | **No** | benchmarks run but no comparison gate | **No PMAT fit** — PMAT does not benchmark |
| **Code churn / entropy / hotspot overlay** | **No** | Not tracked | **Genuine gap → PMAT fit** |
| **Letter-grade TDG summary per PR / repo-wide** | **No** | SonarCloud rates issues, not a single letter grade | **Genuine gap → PMAT fit** |
| **Quality-trend timeseries (Loki/Grafana)** | **Partial** | Mutation scores already pushed to Loki ([`.github/workflows/mutation.yml`](../../opengate/.github/workflows/mutation.yml)) | **Genuine gap → PMAT fit** for grade/score trending |
| **MCP-exposed quality reports for Claude Code** | **No** | No MCP server presently serves quality data | **Genuine gap → PMAT fit** |
| Auto-fix (Kaizen) | **N/A** | Not present; **incompatible with no-bypass commit guards** | **Do not enable beyond `--dry-run`** |

**Conclusion**: PMAT fills exactly four real gaps for OpenGate — churn/entropy/hotspot overlay, single-letter TDG summary, quality-trend timeseries, and MCP-exposed quality data for Claude Code. Everything else is duplication. The adoption scope must reflect this.

---

## 4. The three integration points

Per the user's answers, PMAT is wired into the project at three distinct points. Each is read-only or report-only; none replaces an existing gate.

### 4.1 PMAT as an MCP server for Claude Code

- Register `pmat mcp serve` (or the equivalent invocation per PMAT docs) as an MCP server in [`.claude/settings.json`](../../opengate/.claude/settings.json).
- Expose exactly seven read-only tools (curated allow-list — adjust via ADR amendment):
  1. TDG grade lookup
  2. Repo score
  3. Churn query
  4. Entropy query
  5. Duplicates query
  6. Faults query
  7. Git-history RAG
- Tools used by Claude Code during coding sessions to find hotspots, locate prior fixes in Git history, and pre-screen risky areas before edits.
- **Not** exposed: `kaizen --commit` and any tool that writes files outside an ephemeral report directory.

### 4.2 PMAT as an additive precommit-gauntlet step

- Add a **single fast check** to [`scripts/precommit-gauntlet.sh`](../../opengate/scripts/precommit-gauntlet.sh): `pmat tdg --since-commit HEAD~1 --threshold <floor>` on changed files only.
- Fails the commit if the TDG grade of any changed file drops below **B+** (resolved 2026-05-19). The plan originally proposed starting at `C` and raising; the strict posture was chosen to catch regressions from day one. First-week commit friction is expected; revisit at the ADR-023 30-day review if friction outweighs signal.
- **Not** a full `pmat repo-score` or `pmat mutate` run — those are too slow for per-commit and overlap with existing gates.
- **Not** added before existing checks; appended last so failures don't mask faster checks.
- Same exception policy as SonarCloud: no inline suppression without an ADR reference.

### 4.3 PMAT as a daily analytics workflow → Grafana

- New CI workflow `.github/workflows/pmat-trend.yml`, modelled on the existing [`.github/workflows/mutation.yml`](../../opengate/.github/workflows/mutation.yml).
- Schedule: nightly cron (off-peak).
- Steps:
  1. `pmat tdg` → file-level grades.
  2. `pmat repo-score` → single 0–289 number plus per-category breakdown.
  3. `pmat query <empty> --churn --entropy --duplicates --faults` → hotspot ranks.
  4. Format as the same line-protocol shape as the mutation workflow.
  5. Push to Loki (existing observability env).
  6. Grafana dashboard panels: TDG distribution over time, repo-score trend, top-N hotspots by churn × entropy.
- Telegram alert on regression — TWO conditions, either fires (resolved 2026-05-19):
  - Repo-score day-over-day drop **≥ 3 points**.
  - **Any single file** whose TDG grade drops below B+ since the previous run.

  Same alert scoping as terraform-drift (see commit `678dda3`).

Workflow scope: **nightly full-repo every night** (resolved 2026-05-19). The plan originally proposed nightly delta + weekly full to bound CI minutes; full-repo every night was chosen for consistency with the single-file TDG-slip alert (delta scans would miss slips on untouched files). CI-minute budget revisited at the §7 30-day review.

These three points are **separately togglable**. Either of them can be disabled without affecting the others.

---

## 5. Risks, constraints, and what we will NOT do

### 5.1 Kaizen auto-fix is incompatible with OpenGate's commit guards

`pmat kaizen --commit --push` writes fixes, commits them, and pushes. The commit-guard hook ([`.claude/hooks/pretooluse-git-commit-guard.sh`](../../opengate/.claude/hooks/pretooluse-git-commit-guard.sh)) requires:

- Identity is Ivan Volchanskyi (Kaizen would commit as the CI/system user → blocked).
- TDD check has passed (Kaizen's fix-first writes are source-file edits before any test → blocked by [`pretooluse-tdd-gate.sh`](../../opengate/.claude/hooks/pretooluse-tdd-gate.sh)).
- Refactor marker matches HEAD (Kaizen would produce a commit ahead of the marker → push blocked by [`pretooluse-git-push-guard.sh`](../../opengate/.claude/hooks/pretooluse-git-push-guard.sh)).

Disabling these hooks is forbidden by project rules. **Therefore Kaizen runs in `--dry-run` only**, producing a diff suggestion in PR comments or local terminal output. The user applies fixes through the normal TDD → `/precommit` → commit → `/refactor` → push flow.

### 5.2 Overlap noise

If both SonarCloud and PMAT report duplication, complexity, or smells with different counts and thresholds, code review becomes noisier. Mitigations:

- The PMAT precommit threshold (§4.2) gates **only TDG letter grade**, not individual issue counts.
- The daily Grafana workflow is **observability**, not enforcement — it surfaces trends but does not block PRs.
- SonarCloud remains the merge-to-main quality blocker; PMAT does not gate merge.

### 5.3 Modular-monolith refactor interaction

The modular-monolith refactor (ADRs 018–022) will move files between packages and change import graphs. PMAT's churn metric will spike on touched files (correctly); TDG grades may shift as package boundaries change.

- **Establish PMAT baseline before any modular-monolith refactor PR lands.**
- During refactor PRs, **suppress regressions traceable to the refactor** by an ADR-018 cross-reference in the PR description (not by a PMAT inline suppression).
- Re-baseline at the end of each modular-monolith phase.

**Resolved 2026-05-19:** Baseline before ADR-018. Re-baseline at the end of each modular-monolith phase.

### 5.4 CI minute cost

Daily workflow + per-commit `pmat tdg` add CI minutes. Resolved 2026-05-19: the daily workflow runs **full-repo every night** (the delta-cap recommendation was not adopted). Per-commit `pmat tdg` runs on changed files only and stays cheap. Reassess at the §7 30-day review if CI minutes exceed the budget target.

### 5.5 PMAT version-pinning

PMAT releases frequently (v3.x cadence). Pin **exact version `pmat@3.17.0`** (resolved 2026-05-19) in CI workflow and precommit-gauntlet. Patch updates are manual and dev-staged. The plan originally proposed `3.17.x` (patch auto-update); exact pinning was chosen for maximum reproducibility. Treat PMAT upgrades like any other dependency — staged through `dev`.

### 5.6 What we will NOT do

- Not replace cargo-mutants, gremlins, or stryker with `pmat mutate`. The existing three-engine setup is regression-gated, integrated with Loki, and team-trusted.
- Not replace any SonarCloud function with PMAT.
- Not enable `pmat kaizen` in any mode that writes to the repo automatically.
- Not add PMAT's compliance check (`pmat comply check --strict`) before reviewing its rule set against the existing no-suppression policy — the "30+ regulations" is unverified, and a wave of new violations could flood the gauntlet.
- Not create per-module PMAT gates until modular-monolith ADRs 018–022 have landed and package boundaries are stable.
- Not introduce PMAT's Design-by-Contract feature — its semantics are not concretely documented enough to commit to.
- Not generate ML reproducibility documents. There is no ML in this project. The `SEED=42` claim from the source research is discarded.

---

## 6. Resolved decisions (was: open questions for ADR review)

Resolved 2026-05-19. ADR-023 cites these values verbatim.

| # | Reference | Resolved value | Plan's original recommendation |
|---|---|---|---|
| 1 | §4.2 — TDG floor (precommit) | **B+ from day one** | Start at `C` for two weeks, then raise — **overridden, strict from day one** |
| 2 | §4.3 — Repo-score alert threshold | **≥ 3 points drop day-over-day** | ≥ 5 points — **overridden, more sensitive** |
| 3 | §5.5 — PMAT version pin | **`pmat@3.17.0` exact** (no patch auto-update) | `pmat@3.17.x` — **overridden, exact for max reproducibility** |
| 4 | §4.1 — MCP tool allow-list | **7 tools**: TDG, repo-score, churn, entropy, duplicates, faults, Git-history RAG | Curated 6 — **augmented with Git-history RAG** |
| 5 | §4.3 — Workflow scope and cadence | **Nightly full-repo every night** | Nightly delta + weekly full — **overridden, simpler + consistent with single-file TDG alert** |
| 6 | §5.3 — Baseline timing | **Before ADR-018 lands** | Same — **confirmed** |
| 7 | §4.3 step 6 — TDG-slip alert condition | **Any single file slipping below B+** | Unquantified in original plan — **quantified now** |

**Cross-cutting consequences accepted:**

- First-week alert flurry expected (B+ floor + ≥3-pt repo-score + single-file TDG-slip).
- CI minutes higher than the §5.4 cap recommendation (full-repo nightly).
- Commit friction higher than C-then-raise (B+ from day one will reject more new code initially).
- Manual version bumps required for PMAT patch releases.
- All revisited at the §7 30-day review.

---

## 7. Verification

How we will know PMAT adoption is paying off, not just adding noise:

| Signal | Source | Target |
|---|---|---|
| Precommit-gauntlet wall-clock | Local timing | No regression > 10 s on the median commit |
| Daily workflow CI minutes | GHA usage | < 10 min/day |
| Repo-score trend (Grafana) | Loki | Non-decreasing month over month |
| TDG grade distribution | Grafana panel | Share of A/B grades non-decreasing |
| Number of `--commit reason=ADR-023` exceptions in commit messages | `git log` grep | Zero or near-zero — exceptions should be rare |
| PMAT-flagged hotspots that produce a follow-up fix PR | manual review | ≥ 50 % within one quarter — proves the hotspot signal is actionable |
| Claude Code MCP tool calls that read PMAT data | session transcripts | Non-zero in routine sessions — proves the MCP surface is used |
| Conflicts between PMAT alerts and SonarCloud findings | manual triage | Documented and resolved in the ADR's living "known overlaps" appendix |
| Alerts fired per night (Telegram) | Telegram log | < 3 alerts/night after 4 weeks (calibration target for the strict alert posture) |

If after one quarter the Grafana trend, hotspot follow-ups, and MCP usage are all near zero, **retire PMAT** — it has not earned its slot.

---

## 8. ADR sequence

A single ADR captures the adoption decision. It sits **after** the modular-monolith ADR sequence so PMAT thresholds can be set against the post-decomposition shape (proposed but not required — ADR-023 can land independently).

| # | Title | Key questions |
|---|---|---|
| **ADR-023** | PMAT adoption: augment-only quality overlay with MCP, precommit, and daily Grafana integration | Three integration points, the TDG / repo-score thresholds (§6), the MCP tool allow-list, Kaizen disabled beyond dry-run, no replacement of existing tools, version-pin policy, retirement criterion (§7). |

ADR-023 cites the resolved values from §6: B+ floor, ≥3-pt alert, exact-version pin `pmat@3.17.0`, 7-tool MCP allow-list, full-repo nightly, baseline-before-ADR-018, single-file TDG-slip alert.

ADR-023 references — but does not block on — ADRs 018–022.

---

## 9. Opportunistic implementation triggers

Per the project's no-greenfield-refactor norm, PMAT adoption rides on existing triggers:

| Trigger | Step | Artifact |
|---|---|---|
| ADR-023 acceptance | Add `pmat mcp serve` to [`.claude/settings.json`](../../opengate/.claude/settings.json) | MCP server registered |
| Next gauntlet update PR | Add `pmat tdg --since-commit HEAD~1 --threshold <floor>` to [`scripts/precommit-gauntlet.sh`](../../opengate/scripts/precommit-gauntlet.sh) | New gauntlet step |
| Next observability PR | Add [`.github/workflows/pmat-trend.yml`](../../opengate/.github/workflows/) modelled on `mutation.yml` | New workflow + Grafana panels |
| First Claude Code session post-MCP registration | Use the MCP tools to read a hotspot | Smoke-test of the MCP wiring |
| One month post-adoption | Review §7 metrics; tune thresholds | ADR-023 living "thresholds" appendix updated |

If no trigger fires within 60 days of ADR-023 acceptance, archive this plan as `not-pursued`. **Review date: 2026-07-17.**

---

## 10. Explicit non-goals

- No PMAT mutation testing — the existing three engines stay.
- No PMAT replacement of SonarCloud, lints, audits, taint scans, dead-code sweeps, or coverage gates.
- No Kaizen auto-commit or auto-push.
- No PMAT compliance check until its rule set is reviewed against the no-suppression policy.
- No PMAT Design-by-Contract adoption.
- No ML-reproducibility scaffolding. There is no ML.
- No microservice-level quality grading. PMAT is a monorepo overlay here.
- No timelines, no person-hour estimates.

---

## 11. Critical files referenced

- [`.claude/settings.json`](../../opengate/.claude/settings.json) — MCP server registration target.
- [`scripts/precommit-gauntlet.sh`](../../opengate/scripts/precommit-gauntlet.sh) — host of the new TDG step.
- [`.github/workflows/mutation.yml`](../../opengate/.github/workflows/mutation.yml) — template for the new daily PMAT workflow.
- [`sonar-project.properties`](../../opengate/sonar-project.properties) — existing quality gate; reference for what PMAT must NOT duplicate.
- [`.claude/hooks/pretooluse-git-commit-guard.sh`](../../opengate/.claude/hooks/pretooluse-git-commit-guard.sh), [`.claude/hooks/pretooluse-tdd-gate.sh`](../../opengate/.claude/hooks/pretooluse-tdd-gate.sh), [`.claude/hooks/pretooluse-git-push-guard.sh`](../../opengate/.claude/hooks/pretooluse-git-push-guard.sh) — the no-bypass hooks that constrain Kaizen.
- [`.claude/rules/sonarcloud.md`](../../opengate/.claude/rules/sonarcloud.md) — no-suppression rule that applies to PMAT findings too.
- [`.claude/plans/modular-monolith-evaluation.md`](../../opengate/.claude/plans/modular-monolith-evaluation.md) — paired plan; coordinate baselines.
