# PR 9 redesign — mutation testing as observability, not as a gate

## Context

The current PR 9 plan in [enhance-audit-skills-with-structural-testing.md:89](/home/ivan/opengate/.claude/plans/enhance-audit-skills-with-structural-testing.md#L89) adds a `mutation-testing` matrix job and wires it into `merge-to-main.needs[]`. That would:

- Add ~20 min wall-clock to every dev→main merge (web Stryker alone took 18:28 in PR 8's final run on a clean tree).
- Roughly triple current CI time from 6-8 min to 25-30 min, blocking deploys behind a slow analysis tool.
- Wire a leading-indicator signal (test-suite quality) into a hard-fail gate.

The user's reframe is correct: mutation score is **observability data**, not a build gate. Industry practice for slow mutation tools (cargo-mutants, gremlins, Stryker) is scheduled async runs with regression alerts, not per-PR gating.

## Architecture (user-confirmed choices)

| Decision | Choice |
|---|---|
| Cadence | Nightly cron at **03:00 UTC** (slots between existing `load-test.yml` Sun 02:00 and `sync-from-main.yml` daily 04:00) |
| Regression trigger | **(drop > 2pp from previous successful run) OR (absolute score < 70%)** — per language, OR'd into a single alert |
| Trend storage | **Both**: canonical JSONL in repo + Grafana view via Loki push |
| Workflow status | **Fail red** on regression; Telegram alerts in parallel |

## Workflow design

### New workflow: `.github/workflows/mutation.yml`

```yaml
on:
  schedule:
    - cron: "0 3 * * *"          # nightly 03:00 UTC
  workflow_dispatch:             # manual reruns from GitHub UI

jobs:
  mutation:
    strategy:
      fail-fast: false           # one language failing must not cancel the others
      matrix:
        language: [rust, go, web]
    runs-on: ubuntu-latest       # 2-core, 7GB RAM — upgrade to ubuntu-latest-8-core
                                 # if web Stryker exceeds 25 min wall-clock
    timeout-minutes: 30
    # ... per-language setup steps ...

  publish:
    needs: mutation
    if: always()                 # publish even if one language failed
    runs-on: ubuntu-latest
    # downloads artifacts, summarizes, commits JSONL, pushes to Loki, alerts
```

The publish job has 5 steps:

1. **Download all matrix artifacts** (`actions/download-artifact@v4`).
2. **Summarize**: `scripts/mutation-summarize.sh` reads the three JSON outputs and emits a single canonical row.
3. **Append + commit JSONL**: `git commit docs/mutation-history.jsonl` with `[skip ci]`.
4. **Push to Loki**: SSH to VPS, push the JSON line to Loki via the docker monitoring network.
5. **Check regression + alert**: compare against previous row in JSONL; if regressed, POST to Telegram and `exit 1` to mark the workflow red.

### Parallelism

Matrix over `[rust, go, web]` so all three languages run concurrently. Wall-clock = max of the three ≈ **20 min** (web is the long pole), versus ~45 min sequentially.

`fail-fast: false` is important — if Rust mutation fails midway, the Go and Web runs must still complete so we don't lose visibility on the other languages.

### Output: machine-readable JSON per language

| Language | Output | How |
|---|---|---|
| Rust | `agent/mutants.out/outcomes.json` | Native, already emitted by cargo-mutants |
| Web  | `web/reports/mutation/mutation.json` | Add `"json"` to `reporters` in [web/stryker.config.json:20](/home/ivan/opengate/web/stryker.config.json#L20) |
| Go   | `server/mutation-report.json` | gremlins supports `--output` flag (verify exact flag name in PR 9 implementation; fallback: capture stdout via tee + parse with awk) |

`scripts/mutation-summarize.sh` reads all three and emits a canonical row:

```json
{
  "timestamp": "2026-05-09T03:00:00Z",
  "commit": "e03c95b",
  "scores": {
    "rust": { "killed": 271, "survived": 53,  "no_coverage": 0,   "total": 329,  "score_pct": 76.6 },
    "go":   { "killed": 602, "survived": 33,  "no_coverage": 140, "total": 782,  "score_pct": 77.9 },
    "web":  { "killed": 1244,"survived": 396, "no_coverage": 150, "total": 1880, "score_pct": 70.96 }
  }
}
```

### Trend storage: dual — JSONL canonical + Loki view

**Canonical: `docs/mutation-history.jsonl`** (one JSON object per line, rolling 90-day window).
- Committed by the workflow on each successful run via the existing GitHub Actions `permissions: contents: write` token. Commit message: `chore(mutation): nightly score snapshot [skip ci]`.
- Seed with three rows reconstructed from PR 6/7/8 commit messages (Rust 76.6, Go 77.9, Web 70.96) so the trend has anchor points from day one.
- This is the source of truth — Grafana is a presentation layer.
- **Rotation: 90 days.** Before each append, the summarize script drops rows older than 90 days. Pre-rotation row counts (~90 lines max at one row/night) keep the file under ~30 KB. Older history remains reachable via `git log -p docs/mutation-history.jsonl` if ever needed.

**Presentation: Loki + Grafana dashboard.**
- Loki ingest path: the workflow SSHes to the VPS using `DEPLOY_SSH_PRIVATE_KEY` (already in repo secrets, used by [.github/workflows/cd.yml:112](/home/ivan/opengate/.github/workflows/cd.yml#L112)) and pushes one log line per language per run via the existing monitoring docker network. No new ports, no new ingress route, no Caddy auth needed.
- Loki has no host port (verified — [deploy/docker-compose.monitoring.yml:60-69](/home/ivan/opengate/deploy/docker-compose.monitoring.yml#L60-L69)), so the push runs inside a temporary container on the `monitoring` network:
  ```bash
  ssh deploy-target "docker run --rm --network opengate-monitoring_monitoring \
    curlimages/curl -X POST http://loki:3100/loki/api/v1/push \
    -H 'Content-Type: application/json' -d @-" < payload.json
  ```
- New Grafana dashboard: [deploy/grafana/provisioning/dashboards/mutation-trend.json](/home/ivan/opengate/deploy/grafana/provisioning/dashboards/mutation-trend.json), provisioned alongside the existing three dashboards. Three time-series panels (Rust / Go / Web) using LogQL `unwrap` to extract `score_pct` from the log labels.

### Regression check + Telegram alert

The summarize script computes for each language:

```
prev_row = last successful row in docs/mutation-history.jsonl
regression =
    (curr.score_pct < 70.0)
 OR (prev_row.score_pct - curr.score_pct > 2.0)
```

If any language regresses:

1. POST to Telegram using the existing `DEPLOY_TELEGRAM_BOT_TOKEN` / `DEPLOY_TELEGRAM_CHAT_ID` (already in secrets per [cd.yml:341-342](/home/ivan/opengate/.github/workflows/cd.yml#L341-L342)). Payload:
    ```
    ⚠️ Mutation score regression on dev

    Rust: 76.6 → 73.8 (-2.8pp)
    Go:   77.9 → 77.9
    Web:  70.96 → 68.5 (drop > 2pp AND < 70% absolute floor)

    Run: github.com/volchanskyi/opengate/actions/runs/<id>
    Trend: github.com/volchanskyi/opengate/blob/main/docs/mutation-history.jsonl
    Reports: <artifact links>
    ```
2. **`exit 1`** to fail the workflow red — visible in the GitHub Actions history as a hard signal.

A regression alert is still committed to the JSONL — we record the dip, not just successes. The workflow's red status communicates the regression; the JSONL communicates the data.

### Artifact retention

Upload HTML reports (Stryker `mutation.html`, cargo-mutants `mutants.out/`, gremlins text report) as workflow artifacts with `retention-days: 90`. Matches the pattern used for Playwright reports in `ci.yml`.

### Explicit non-goals

- **Does not block merges or deploys.** No `needs:` wiring to `merge-to-main`.
- **Does not add a Pushgateway.** Loki ingest reuses the existing monitoring stack.
- **Does not modify Stryker / gremlins / cargo-mutants thresholds.** They stay null/advisory; alerting handles regression.
- **Does not modify ci.yml.** The original PR 9 plan also added gosec to `go-lint` (already done in PR 4) and ESLint security rules (already done in PR 5). PR 9 simplifies to just the new async workflow.

## Files to be added / modified

**New:**
- [.github/workflows/mutation.yml](/home/ivan/opengate/.github/workflows/mutation.yml) — the scheduled matrix workflow
- [scripts/mutation-summarize.sh](/home/ivan/opengate/scripts/mutation-summarize.sh) — reads three JSON outputs, emits canonical summary, runs the regression check, sets workflow exit code
- [scripts/mutation-loki-push.sh](/home/ivan/opengate/scripts/mutation-loki-push.sh) — wraps the SSH+docker-network curl to Loki
- [docs/mutation-history.jsonl](/home/ivan/opengate/docs/mutation-history.jsonl) — seeded with PR 6/7/8 baseline rows
- [deploy/grafana/provisioning/dashboards/mutation-trend.json](/home/ivan/opengate/deploy/grafana/provisioning/dashboards/mutation-trend.json) — Grafana dashboard, three time-series panels

**Modified:**
- [web/stryker.config.json](/home/ivan/opengate/web/stryker.config.json) — add `"json"` to `reporters` array; set `htmlReporter.fileName` and a new `jsonReporter.fileName` for canonical paths
- [server/.gremlins.yaml](/home/ivan/opengate/server/.gremlins.yaml) — wire up `--output` if supported by current gremlins version; otherwise document the stdout capture approach in the summarize script
- [docs/Testing.md](/home/ivan/opengate/docs/Testing.md) — add a "Mutation testing trend" section with links to the workflow, JSONL, and Grafana dashboard
- [docs/Monitoring.md](/home/ivan/opengate/docs/Monitoring.md) — document the new Loki ingest path and dashboard
- [.claude/plans/enhance-audit-skills-with-structural-testing.md](/home/ivan/opengate/.claude/plans/enhance-audit-skills-with-structural-testing.md) — supersede PR 9 entry with a pointer to this redesign
- [.claude/phases.md](/home/ivan/opengate/.claude/phases.md) — add PR 9 row referencing this plan
- [README.md](/home/ivan/opengate/README.md) — section on "Running mutation tests" if one exists, otherwise skip

## Verification

1. **Dry-run via `workflow_dispatch`** on a feature branch: push a deliberate `==` → `!=` mutation to a tested Rust file, trigger the workflow manually. Confirm (a) Rust score drops, (b) JSONL gets the new row, (c) Loki receives the push (verify via Grafana panel updating), (d) Telegram alert fires with the regression payload, (e) workflow goes red ❌ in GitHub Actions UI, (f) `merge-to-main` jobs are untouched (no `needs:` to mutation.yml).
2. **Cold-run on empty regression history**: rerun the workflow immediately after #1 with the dev branch reverted. The score should bounce back; no alert should fire; workflow goes green.
3. **Wall-clock budget**: scheduled run must complete under 30 min on ubuntu-latest. If web exceeds 25 min, document the `ubuntu-latest-8-core` upgrade path; do not pre-emptively upgrade.
4. **Grafana dashboard renders**: after the second successful nightly run, open Grafana and confirm three line-series panels populate. Time range default = last 30 days.
5. **`/precommit` still passes** after stryker.config.json + gremlins config tweaks (tests run unchanged; the new reporters are additive).
6. **Phase entry**: add a row to `.claude/phases.md` referencing this plan, with the final wall-clock observed in dry-run #1.

## Open risks

- **gremlins `--output` flag** may not exist in the current pinned version. Mitigation: the summarize script falls back to parsing stdout (the canonical text format is stable across recent gremlins versions). Verify in implementation.
- **Loki retention** is 14 days per [loki-config.yml:29-32](/home/ivan/opengate/deploy/loki/loki-config.yml) — Grafana shows the last 14 days from Loki, and the last 90 days from the JSONL (see retention below). Document the dual-window behaviour in `docs/Monitoring.md`.
- **JSONL retention: 90 days, rolling rotation.** The summarize script trims `docs/mutation-history.jsonl` to keep only the last 90 days of rows before committing. Rationale: keeps the diff-friendly file small and bounded; matches the existing 90-day artifact retention pattern used for Playwright reports in `ci.yml`; long-term archival of older rows (if ever wanted) can be reconstructed from prior commits on the `main` branch via `git log -p docs/mutation-history.jsonl`. The rotation logic is a single jq/awk one-liner: filter rows where `timestamp >= now - 90d`.
- **Workflow commits to dev** require `permissions: contents: write` on the job. Existing convention used by `auto-tag` and `sync-from-main` workflows — reuse the pattern.
- **First run with no previous successful row**: the regression check must handle `prev_row == None` gracefully (no alert, no `exit 1`). Unit-testable in the summarize script.
