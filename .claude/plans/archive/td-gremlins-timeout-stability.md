# Micro-Plan (implementation-ready): Shard the Go Mutation Run by Package

**Register entry:** [techdebt.md](../../techdebt.md) — "Go mutation score is sensitive to
gremlins' runner-derived per-mutant timeout" (now elevated: the Go leg also crosses the
job cap and kills the nightly run). **Master:** `techdebt-paydown-master.md` +
[`benchmarks-grafana-trends.md`](benchmarks-grafana-trends.md) (trend pipeline).
**Branch:** `dev`. **Owner:** CI / Go. **Status:** investigated + spiked 2026-06-19,
ready to implement.

Self-contained: an engineer can implement from this file alone.

## 1. Problem (proven 2026-06-19)

`mutation.yml` runs the Go leg as one monolithic `gremlins unleash .` over the whole
`server` module under a fixed `timeout-minutes` cap bumped 35→90→100
([`mutation.yml:56`](../../../.github/workflows/mutation.yml#L56)). Empirical findings:

- Go runtime grew ~40m (coeff 5) → ~60m (coeff 10) → ~85m (coeff 15) while the **mutant
  count stayed flat (~870–913)** — runtime tracks the `timeout-coefficient`, not the
  workload. On **2026-06-18 and 06-19 the Go leg crossed the 100-min cap and was
  cancelled** → no `server/mutation-report.json` → `publish` exits 2 → **no trend data
  reached VictoriaMetrics**.
- The slow tail (~19 min in the last good run) is dominated by a few mutants hitting the
  `coefficient × dry-run` timeout leash on the contended 4-vCPU runner.
- **Scores are healthy and stable** (~86% Go for 3 weeks) — this is a *runtime/cap* and
  *observability* problem, not a test-quality problem.

Raising the cap or coefficient trades runtime against score stability; the wall returns
as the module grows. Fix = **shard horizontally**.

## 2. Spike evidence (gremlins v0.6.0, coeff 15, local — calibrate on CI)

Mechanics confirmed in v0.6.0: `gremlins unleash [path]` scopes to that package
(`osutil` → its exact 3 mutants); `--config`, `--timeout-coefficient`, `--workers`,
`--output`, `--dry-run`, `-D/--diff` all present. Per-mutant cost classes:

| pkg | mutants | wall (local 24c) | s/mutant | class |
|---|---|---|---|---|
| api | 231 | 310s | 1.34 | DB-handler (≈40% of whole run) |
| amt | 230 | 74s | 0.32 | pure (high volume, cheap) |
| agentapi | 52 | 71s | 1.37 | DB/handshake |
| notifications | 28 | 28s | 1.00 | crypto (VAPID) |
| protocol | 34 | 8s | 0.24 | pure |
| relay | 12 | 8s | 0.67 | (timed out in CI; fast locally — confirms runner-contention artifact) |

Per-package mutant counts (from the last-good CI log, total 877): amt 237, api 231,
updater 64, cert 57, device 55, agentapi 54, auth 42, protocol 34, notifications 29,
relay 13, db 13, session 10, metrics 10, signaling 9, testpg 8, audit 5, usecase 3,
osutil 3.

## 3. Finalized split (2 shards, balanced; each holds DB packages → generous budget)

A pure slow/fast split is badly imbalanced (~100s vs ~700s). Balance by weight while
keeping a DB-backed package in each shard so its coverage dry-run is robustly slow
(prevents the false-timeout collapse without a coefficient hike):

- **`go-1`** (~540 mutants, ~415s local): `api amt protocol relay metrics signaling osutil
  testpg usecase`
- **`go-2`** (~326 mutants, ~387s local): `agentapi notifications updater cert device auth
  db session audit`

Critical path ≈ max(415, 387) vs ~765s monolith ≈ **1.85× faster**; expect each CI shard
~45–50 min (calibrate). Add a 3rd shard (promote `api` to its own) only when a shard
approaches its cap.

## 4. File inventory + concrete changes

### 4.1 `.github/workflows/mutation.yml`

Replace the `language: [rust, go, web]` matrix with an `include:` list so the Go leg
fans out into shards while rust/web stay single:

```yaml
strategy:
  fail-fast: false
  matrix:
    include:
      - { language: rust }
      - { language: web }
      - { language: go, shard: go-1, packages: "./internal/api ./internal/amt ./internal/protocol ./internal/relay ./internal/metrics ./internal/signaling ./internal/osutil ./internal/testpg ./internal/usecase" }
      - { language: go, shard: go-2, packages: "./internal/agentapi ./internal/notifications ./internal/updater ./internal/cert ./internal/device ./internal/auth ./internal/db ./internal/session ./internal/audit" }
```

- Job name: `Mutation (${{ matrix.shard || matrix.language }})`.
- `timeout-minutes: 60` (down from 100 — each shard has ample headroom; rust ~46m / web
  ~25m also fit).
- Go run step (keyed on `matrix.language == 'go'`):
  `gremlins unleash --output "mutation-report-${{ matrix.shard }}.json" ${{ matrix.packages }} || EXIT=$?`
  (drop the `cd server` → run from `server` working-dir as today; coefficient stays in
  `.gremlins.yaml`, see 4.2).
- Postgres is still needed by `go-1` **and** `go-2` (both contain DB packages) — keep the
  uniform "Start Postgres" step.
- Artifact upload: `name: mutation-${{ matrix.shard || matrix.language }}`, path includes
  `server/mutation-report-${{ matrix.shard }}.json` for go.
- **publish job — replace "Place artifacts" Go handling** with a merge that sums the shard
  reports into the canonical single file `parse_go` already expects:

```bash
mkdir -p server
shards=(artifacts/mutation-go-*/server/mutation-report-*.json)
jq -s '{
  mutants_killed:      (map(.mutants_killed      // 0) | add),
  mutants_lived:       (map(.mutants_lived       // 0) | add),
  mutants_not_covered: (map(.mutants_not_covered // 0) | add),
  mutants_not_viable:  (map(.mutants_not_viable  // 0) | add)
}' "${shards[@]}" > server/mutation-report.json
```

(Per-shard reports remain individually downloadable for detail; the summarizer contract is
unchanged.)

### 4.2 `server/.gremlins.yaml`

Keep `timeout-coefficient: 15` and `exclude-files`. With slow packages no longer averaged
against a fast monolithic dry-run, the coefficient could later drop for a pure shard via a
per-shard `--config`/`--timeout-coefficient` override — leave that as a follow-up tune; the
default 15 is safe for both shards initially.

### 4.3 `scripts/mutation-summarize.sh` (cheap diagnosability fix)

In `build_row` ([:153-155](../../../scripts/mutation-summarize.sh#L153)) the parse
assignments run under a suspended `set -e` (caller is `row="$(build_row)" || exit 2`), so a
missing input prints the correct `missing: <file>` **then** a misleading `jq: invalid JSON
text passed to --argjson`. Add `|| return 2` to each:

```bash
rust="$(parse_rust "$RUST_OUTCOMES")" || return 2
go="$(parse_go   "$GO_REPORT")"      || return 2
web="$(parse_web "$WEB_REPORT")"     || return 2
```

No other change. Score/floor logic and the `<85%` floor stay; **do not** add previous-score
/ drop-detection (owner decision 2026-06-19: previous score is out of scope).

### 4.4 `Makefile`

Split `make mutate-go` to mirror the two shards (so local == CI). Keep a `mutate-go`
umbrella that runs both shard targets sequentially.

### 4.5 `scripts/lib/mutation-shards.sh` (new, single source of truth)

Define the shard→package mapping once (a bash array or a small file) consumed by the
workflow generation check **and** the Makefile, so the split lives in one place.

## 5. Approach (TDD where code; calibrate on CI)

1. **Failing tests first** (`scripts/tests/mutation-workflow.test.sh`, behavioral not grep):
   - **partition completeness:** every non-excluded `server/internal/*` package appears in
     **exactly one** shard (fails if a new package is unassigned or double-assigned).
   - **merge-sum:** given two stub shard reports, the merge produces one report whose four
     counts equal the element-wise sums.
   - **summarizer single-error:** `mutation-summarize.sh` with a missing input emits exactly
     one error line and exits 2 (no trailing `jq` noise).
2. Implement 4.5 mapping → 4.1 workflow → 4.3 summarizer guard → 4.4 Makefile → merge step.
3. **Validate locally:** `make mutate-go` per shard; merged counts ≈ last monolith
   (~754 killed / ~86.6%) within tolerance (no mutants dropped).
4. **CI calibration (one `workflow_dispatch` run):** read per-shard wall-clock from the
   matrix; if either shard exceeds ~50 min, rebalance the package lists (move a heavy
   package across) and/or promote `api` to a 3rd shard. Re-run.
5. **Validate the trend flow (first-ever for mutation):** confirm the completed run reaches
   `publish` → VM push **succeeds** and `mutation_score{language="go"}` appears in
   VictoriaMetrics / the Grafana "Mutation Testing Trend" dashboard. If the VM push fails,
   fix it here (it has never executed for mutation — 06-17 used Loki, the VM migration
   landed 06-19 in `cb03761`, and every run since cancelled).
6. `/precommit` → commit → `/refactor` → `/precommit` → commit → push.

## 6. Acceptance / DoD

- [ ] Both Go shards complete under the 60-min cap with headroom; no cancellations across 3
      consecutive nightly runs.
- [ ] Merged Go score within tolerance of the pre-shard baseline (~86.6%); no mutants
      dropped (denominator equal or larger).
- [ ] Partition guard fails on an unassigned/duplicated package (proven by test).
- [ ] Summarizer emits a single clear error on missing input (exit 2, no `jq` noise).
- [ ] A completed run publishes `mutation_score{language="go"}` to VictoriaMetrics (verified
      on the dashboard) — observability gap closed.
- [ ] Adding a server package requires only editing the shard map (caught by the guard), not
      touching the cap. `make mutate-go` + `/precommit` green.

## 7. Reviewer / QA checklist

- [ ] Shard lists partition the module exactly (guard test present and failing-before).
- [ ] Each shard contains ≥1 DB-backed package (generous coverage-derived budget); the fix
      removes the dependence on a single monolithic dry-run, not just lowers the cap.
- [ ] Merge sums counts correctly (test), per-shard reports still uploaded for detail.
- [ ] Summarizer change is the `|| return 2` guard only; **no** previous-score/drop logic
      added; `<85%` floor intact.
- [ ] First green run's VM push verified end-to-end.
- [ ] `make mutate-go` mirrors CI shards (local == CI); shard map has one source of truth.

## 8. NFRs

- **Scalability:** runtime scales with shard count, not a hand-tuned cap.
- **CI reliability:** removes cap-cancellation and the load-induced false-timeout collapse.
- **Observability:** trend data flows again; per-shard timing visible in the matrix.
- **Maintainability:** workflow + config only; single-source shard map; summarizer contract
  unchanged.

## 9. Risks / mitigations

- **Local≠CI timing** → ship the starting split, calibrate on one CI run (step 4) before
  declaring done; the 60-min cap has headroom for drift.
- **Package in two shards / unassigned** → partition guard test blocks it.
- **VM mutation push unproven** → budget time to debug on the first green run (step 5).
- **gremlins per-shard config** → if a pure shard later wants a lower coefficient, use
  `--config <file>` or `--timeout-coefficient` CLI override per matrix entry (verified to
  exist); not required for the initial landing.

## 10. Out of scope

Previous-score/drop-pp regression detection (owner: out); Rust/Web legs; the 3 residual
Rust WebRTC mutants (`td-webrtc-dispatch-mutation-harness.md`, orthogonal); the trend
pipeline migration itself (master plan). Option A (absolute per-mutant timeout) is not
pursued — sharding solves runtime and stability together.
