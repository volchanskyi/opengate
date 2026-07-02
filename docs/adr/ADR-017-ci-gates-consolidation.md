# ADR-017: CI gates consolidation — inline IaC gate, drop in-repo mutation history, remove load-test approval gate

Date: 2026-05-17
Status: Accepted; mutation trend-store clause superseded by ADR-038

## Context

Three CI/CD issues converged this week and required architectural decisions, not symptom patches:

1. **IaC plan preview did not gate direct-to-dev pushes.** The project's workflow is "push directly to `dev`; CI auto-merges to `main`." The standalone `.github/workflows/iac-plan-preview.yml` only triggered on `pull_request` and was not in `merge-to-main.needs:`. For our own pushes the destroy-blocklist did nothing — drift detection was the only line of defence, and it reports a day after the fact.

2. **Mutation testing's nightly publish job repeatedly failed** with `GH013: Repository rule violations found for refs/heads/dev` — the bot's `chore(mutation): nightly score snapshot` commit could never land because `dev` branch protection requires 19 status checks the new commit has not run. Three prior "fixes" (retarget main → dev, add rebase, add 3× retry) treated symptoms; the root cause is structural: a fresh bot commit cannot acquire required-check evidence without re-running CI on it, and `[skip ci]` would only make it worse.

3. **Weekly load-test was waiting for "Review deployments" approval.** [`.github/workflows/load-test.yml`](../../.github/workflows/load-test.yml) declared `environment: staging`, which inherits the `required_reviewers` policy used by the real CD deploys. Observability that runs unattended must not be gated on a click.

## Decision

### IaC gate — inline into ci.yml, hard-block destroys on direct push

Delete the standalone workflow file and add a new `iac-gate` job to [`ci.yml`](../../.github/workflows/ci.yml) with:

- Triggers on both `push` and `pull_request` (inherited from ci.yml's existing `on:` block).
- Path detection inline (`git diff` against `pull_request.base.sha` or `event.before`). When no `deploy/terraform/**` change is present, every subsequent step is `if:`-gated off and the job completes in ~10 s as `success`. This keeps the job safe to put in `merge-to-main.needs:`.
- PR comment posting kept (sticky, marker-based, idempotent across pushes).
- Plan summary written to `GITHUB_STEP_SUMMARY` on push events so the destroy report is visible without a PR.
- `terraform init` retried 3× with backoff, mirroring [`terraform-drift.yml`](../../.github/workflows/terraform-drift.yml).
- Bypass policy: PR + `iac:approve-destroy` label only. Direct push to `dev` has **no bypass** — destructive terraform changes must go through a labelled PR.
- Added to `merge-to-main.needs:`. The auto-merge to `main` cannot proceed until the gate passes (or correctly skips).

### Mutation testing — drop the in-repo history file; use an external trend store

Remove the `Commit JSONL row to dev` step from [`mutation.yml`](../../.github/workflows/mutation.yml). Remove `APPEND=1` from the summarize step so `docs/mutation-history.jsonl` is no longer written. Delete the existing file. Upload the per-run canonical row as a workflow artifact (`mutation-canonical-row`, 90-day retention) for one-off audits. Update the Telegram footer to point at the Grafana dashboard instead of the broken in-repo JSONL link.

Numeric trend data now lives in VictoriaMetrics and is visualised in the
provisioned mutation Grafana dashboard per
[ADR-038](ADR-038-victoriametrics-ci-trend-store.md). Audit access uses the
workflow artifact. The repo branch is no longer involved in nightly telemetry —
branch protection on `dev` correctly rejects unattended bot commits, and the
right answer is to stop trying to commit, not to widen the bypass surface.

### Load-test — drop the `environment: staging` declaration

Remove the line from [`load-test.yml`](../../.github/workflows/load-test.yml). All secrets the job reads (`OCI_*`, `DEPLOY_*`) are already repo-level, so removing the environment loses no inputs. Mirrors the same decision already documented in `mutation.yml` for its publish job.

## Consequences

### Positive

- Destructive terraform changes are now gated on **every** path into `main`, not just open PRs. The most dangerous path (direct push to `dev`) gets the strongest discipline.
- Mutation testing stops failing red every night for a non-failure reason. Branch protection on `dev` keeps its full surface — no bot bypass added.
- Weekly load-tests run unattended again.
- One fewer workflow file to maintain (iac-plan-preview.yml deleted).
- Plan summary is now visible on direct pushes via the GitHub Job Summary, not just on PRs.

### Negative / costs

- `iac-gate` runs ~10 s on every dev push even when no terraform files change (path detection + checkout). On terraform commits it adds ~2 min (init + plan).
- Loss of the 90-day, human-browsable `docs/mutation-history.jsonl` on `main`.
  Trend retention now follows the canonical store selected by ADR-038 rather
  than being restored to the repo.
- Branch protection rules on `dev` and `main` may still list `iac-plan-preview` as a required check if it was ever configured there. That status check no longer reports. **Operator action:** in GitHub repo settings, replace any required check named `iac-plan-preview / Plan preview + destroy-blocklist gate` with `CI / IaC plan + destroy-blocklist gate`.

### Risks

- A regression-detection bug in `mutation-summarize.sh` that previously surfaced
  via a JSONL diff is now visible through the VictoriaMetrics trend or the
  workflow artifact. Mitigated by Grafana alerting being the primary surface.
- The IaC gate's hard-block on direct-push destroys means an in-flight emergency tear-down requires a PR detour. Acceptable trade-off; drift detection covers the operational-mistake case.

## Alternatives considered

- **R1 — Reusable workflow** (`on: workflow_call`). One source of truth for the plan logic, called from a PR wrapper and from a ci.yml job. Rejected: more YAML and indirection for a single-module, single-environment project. Worth reconsidering if we add a second terraform module or a second deploy target.
- **R3 — Required status check via branch protection on the standalone workflow.** Would rely on operator-configured branch protection rather than `needs:` to enforce ordering. Rejected: the workflow is code-reviewable, branch protection is not — and the failure mode ("push rejected") is worse UX than "merge-to-main waited and is going red."
- **D2 — Commit trailer (`IaC-Approve-Destroy: yes`) bypass on direct push.** Convenient but auditable only via `git log`. Rejected: the labelled-PR path is one extra step and keeps human review in the loop for destructive changes.
- **Mutation Option A — `SYNC_TOKEN` with bypass privileges** to land the JSONL commit directly on `dev`. Rejected: widens the surface of what can bypass `dev` branch protection. The unattended-bot-commit-of-telemetry pattern is the wrong primitive; time-series belongs in a TSDB.
- **Mutation Option B — orphan branch (`mutation-history`)** for the JSONL trend
  file. Workable, similar pattern to `gh-pages`. Initially rejected in favour of
  Loki; that storage choice was later superseded when numeric CI trends
  consolidated in VictoriaMetrics under ADR-038.
