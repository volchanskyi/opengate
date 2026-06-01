# PMAT baselines

Snapshots used to detect quality regressions during the modular-monolith decomposition. Required by [ADR-019](../../docs/adr/ADR-019-pmat-quality-overlay.md) (PMAT adoption) and [ADR-020](../../docs/adr/ADR-020-modular-monolith-full-hexagonal.md) — a baseline must exist BEFORE the first opportunistic-trigger PR fires so post-decomposition drift is measurable.

## 2026-05-20 — pre-ADR-020 baseline

| Artifact | File | What it measures |
|---|---|---|
| TDG baseline | [`pmat-tdg-baseline-2026-05-20.json`](pmat-tdg-baseline-2026-05-20.json) | Per-file Technical Debt Grade (A+ → F) across 335 files + grade distribution + git context |
| Repository score | [`pmat-repo-score-2026-05-20.json`](pmat-repo-score-2026-05-20.json) | Aggregate 0–110 repository-health score across 6 categories |

**Captured at:** commit `ffa3c92` on `dev` (2026-05-20T15:17:31Z), tree state included 5 uncommitted files (the modular-monolith ADR batch was about to commit).

### TDG headline

- **Files analyzed:** 335 (139 Go, 143 TypeScript, 44 Rust, 9 JavaScript)
- **Average score:** 90.6 / 100
- **Grade distribution:**

  | Grade | Count |
  |---|---|
  | A− | 268 |
  | B+ | 21 |
  | B  | 17 |
  | B− | 18 |
  | C+ | 6 |
  | C  | 1 |
  | C− | 2 |
  | D  | 1 |
  | F  | 1 |

- **Lone F-grade:** [`agent/crates/mesh-agent/build.rs`](../../agent/crates/mesh-agent/build.rs) (score 0.0). Likely a tiny build script being treated as low-quality code; pre-existing, not introduced by ADR-020. **PMAT notes that F-grades cap the project grade at B.** Tracked but not blocking the baseline.

### Repository-score headline

- **Total score:** 64.5 / 110
- **Grade:** C
- **Category breakdown (6 categories):**

  | Category | Score | Status |
  |---|---|---|
  | Documentation | 10 / 15 (66.7%) | Fail |
  | (cat B — see JSON) | 0 / 20 (0%) | Fail |
  | (cat C — see JSON) | 15 / 15 (100%) | Pass |
  | (cat D — see JSON) | 17 / 25 (68%) | Fail |
  | (cat E — see JSON) | 20 / 20 (100%) | Pass |
  | (cat F — see JSON) | 2.5 / 5 (50%) | Fail |

  Full per-category subcategory detail is in the JSON file.

## Re-baseline policy

Per [PMAT plan §5.3](../plans/archive/pmat-adoption-evaluation.md), re-baseline at the end of each modular-monolith phase — i.e. each time one of the 12 modules listed in ADR-020 finishes its hexagonal extraction. File-naming: `pmat-tdg-baseline-YYYY-MM-DD.json` and `pmat-repo-score-YYYY-MM-DD.json`. Keep prior baselines for trend visibility; do not overwrite.

PMAT's nightly Loki/Grafana workflow (ADR-019, future opportunistic trigger) is the **trend store**; this directory is just the named-snapshot store referenced by ADRs.

## Reproducing a baseline

```bash
pmat tdg baseline create \
  --path . \
  --output .claude/baseline/pmat-tdg-baseline-YYYY-MM-DD.json \
  --name "<scope-tag>" \
  --with-git-context

pmat repo-score \
  --path . \
  --format json \
  --quiet > .claude/baseline/pmat-repo-score-YYYY-MM-DD.json
```

PMAT version: pinned to **`pmat@3.17.0`** per [ADR-019](../../docs/adr/ADR-019-pmat-quality-overlay.md). Reproducing on a fresh machine: `cargo install --locked --version 3.17.0 pmat`.

## Lesson learned — `--with-git-context` vs credential-bearing remote URLs

`pmat tdg baseline create --with-git-context` reads `git remote get-url origin` and serializes the result verbatim. If the git remote URL embeds credentials (e.g. `https://user:github_pat_xxx@github.com/...`), they land in the baseline JSON in plain text. **This was caught during the first baseline attempt on 2026-05-20** (handled before any commit).

**Rule:** Never invoke `--with-git-context` while the active git remote URL contains credentials. Either:

- Use a credentialed-helper form (no creds in URL): `git remote set-url origin https://github.com/<owner>/<repo>.git`, OR
- Use SSH: `git remote set-url origin git@github.com:<owner>/<repo>.git`

The repo's `gitleaks` step (`scripts/precommit-gauntlet.sh` step 9) would catch this on a commit attempt, but the right move is not to generate the leaked artifact in the first place.
