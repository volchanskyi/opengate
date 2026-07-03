# CI Pipeline

## Triggers

Every push to `dev` and every pull request targeting `main` or `dev` runs the CI pipeline. CodeQL and security scanning run on every push and PR (no separate schedule). Load testing has its own scheduled workflow.

## Branching Flow

```
dependabot/* PR ──► dev ──► main
human commits  ──► dev ──► main
                   (CI-gated, auto-merged)
```

- **`dev`** — primary development branch. Human commits land directly; Dependabot opens PRs against it. After all CI checks pass, the `merge-to-main` job forwards `dev` → `main`.
- **`main`** — stable branch. Receives code from `dev` only, via the automated `merge-to-main` job. Protected: requires 1 PR review for non-admin pushes; force-push and deletion disabled.
- **Dependabot** — PRs open against `dev` with the same gate any human commit clears. [`dependabot-auto-merge.yml`](../.github/workflows/dependabot-auto-merge.yml) squash-merges patch + minor updates once CI is green; major-version bumps stay open for review.

## Job Graph

```
                          CI Workflow
   push → dev   /  pull_request → main|dev
                   │
        ┌──────────┼──────────────────────────────────────┐
        ▼          ▼            ▼            ▼             ▼
      Rust         Go          Web            Security       CodeQL           parallel
      ├─ lint      ├─ lint     ├─ unit+build  ├─ govulncheck ├─ Go        YAML
      ├─ test      ├─ unit     ├─ integration ├─ cargo audit ├─ TypeScript ├─ actionlint
      │            └─ integration             └─ npm audit   └─ Rust
      └─ generate golden files
               │                ├─ bundle-size (size-limit, gated)
               ▼
        Golden verification     (needs Rust — consumes artifact)
               │
        Deploy API Docs         (needs web-unit, dev push only → gh-pages)
        SonarCloud Analysis     (needs Go unit + Rust test + Web unit)
        └─ downloads coverage artifacts from all 3 test jobs
               │
        E2E (Playwright)       (needs all prior checks + bundle-size)
        └─ docker-compose.test.yml → Playwright → Lighthouse CI → tear down
               │
        Load Test (k6)         (dispatch/schedule only — not gated)
               │
               └─────────────── all gate jobs must pass ──────────────────┐
                                                                        ▼
                                                    Auto-merge dev → main
                                                    └─ Update coverage badge (only on push to dev)
                                                                        │
                                                                        ▼
                                                    Auto-tag release (needs merge-to-main)
                                                    └─ Conventional commits → semver bump → CHANGELOG.md + git tag

               └─────────────── any job fails (non-PR) ─────────────────┐
                                                                        ▼
                                                    Notify failure (needs all upstream jobs)
                                                    └─ Creates/updates GitHub Issue per failed job

        Benchmark Trends Workflow (nightly / workflow_dispatch)
        └─ Go + Rust benchmarks → VictoriaMetrics → Grafana Benchmark Trends

        Load-Test Workflow (scheduled / workflow_dispatch)
        └─ k6 + QUIC summary artifact → VM read-back gate → VictoriaMetrics → Telegram/fail-red

        Build & Push Container Image Workflow
        (main push / CI success on dev)
                    │
                    ▼
              Build multi-arch → Push GHCR → Cosign sign → SBOM attest → Trivy scan
                    │
                    ▼
              CD Workflow (cd.yml)
              ├─ resolve-tag + cosign verify
              ├─ deploy-staging-k8s → Helm upgrade → smoke-test + Playwright via port-forward
              └─ deploy-production-k8s (environment approval) → Helm upgrade → smoke-test via port-forward
```

## Jobs

The CI workflow jobs are grouped by concern:

| Group | Jobs | Purpose |
|-------|------|---------|
| **Rust** | `rust-lint`, `rust-test` | `cargo fmt` + clippy, nextest + golden file generation + llvm-cov coverage |
| **Go** | `go-lint`, `go-unit`, `go-integration` | `go vet` + OpenAPI codegen sync check, unit tests with coverage, QUIC integration tests |
| **Web** | `web-lint`, `web-unit`, `web-integration` | ESLint; unit/component tests (with v8 coverage) + Vite build; integration tests |
| **Bundle Size** | `web-bundle-size` | `size-limit` gzip size check (JS ≤250KB, CSS ≤10KB, Total ≤260KB). Runs in parallel with other web jobs. |
| **API Docs** | `deploy-api-docs` | Deploys OpenAPI spec + Scalar viewer to gh-pages (dev push only) |
| **Config** | `config-lint` | actionlint, yamllint, `terraform fmt/validate`, tflint, `terraform test` (module invariants), output-sensitivity grep, gitleaks (L2), Hadolint Dockerfile policy (L4), Checkov (L4: terraform + dockerfile + github_actions, baseline at `.checkov.baseline`), Conftest+Rego custom policies (L5: compose images, action SHA-pinning), `docker compose config`, `caddy fmt/validate`, Trivy IaC scan, cross-config integration tests |
| **IaC gate** | `iac-gate` | Runs `terraform plan` on every commit / PR that touches `deploy/terraform/**`. Posts a sticky PR comment on PRs and writes the plan summary to the GitHub Job Summary on direct pushes. Blocks merge if a destroy targets a protected resource type. Bypass: `iac:approve-destroy` label on PR only — no bypass for direct pushes to `dev`. Wired into `merge-to-main.needs`. See [Infrastructure.md → IaC plan + destroy-blocklist gate](Infrastructure.md#iac-plan--destroy-blocklist-gate). |
| **Golden** | `golden` | Cross-language wire format verification (needs `rust-test` artifact) |
| **Security** | `security-audit` | govulncheck, cargo audit, npm audit |
| **CodeQL** | `codeql-go`, `codeql-js`, `codeql-rust` | GitHub Code Scanning with `security-and-quality` queries |
| **SonarCloud** | `sonarcloud` | Static analysis + coverage aggregation via SonarSource scan action |
| **E2E** | `e2e` | Playwright end-to-end + Lighthouse CI audits via `docker-compose.test.yml` (needs all prior checks + bundle-size) |
| **Load** | `load-test` | k6 HTTP/WS and QUIC load test workflow (scheduled/dispatchable; independent of merge gating) |
| **Merge** | `merge-to-main` | Auto-merge `dev` → `main` after the required upstream jobs in [`ci.yml`](../.github/workflows/ci.yml) pass; updates Go/Rust/Web coverage badges on `dev` pushes |
| **Auto-tag** | `auto-tag` | Determines semver bump from conventional commits, generates Keep a Changelog entry, commits CHANGELOG.md, and pushes a git tag (triggers `release-agent.yml`) |
| **Notify** | `notify-failure` | Auto-creates GitHub Issues when any job fails (push/schedule/dispatch only — not PRs). One issue per failed job per branch, with error log excerpts. |

## Sequencing

The **golden verification** job is sequenced after Rust so the Go verifier always works against freshly generated fixtures — this prevents Rust ↔ Go wire-format drift from going undetected.

Pull requests execute every CI job except auto-merge/release automation. Benchmark
trends run in the separate scheduled/dispatchable
[`benchmark.yml`](../.github/workflows/benchmark.yml) workflow.

### OpenAPI Codegen Sync

The `go-lint` job verifies that generated Go code from the OpenAPI spec is up to date. It runs `go generate ./internal/api/` and then `git diff --exit-code` — if the generated output differs from what is committed, the job fails. This can also be checked locally via `make verify-codegen`.

## Coverage

All three language test jobs enforce a minimum line-coverage threshold — the build fails if coverage of production code drops below it. The enforced values and exclusion patterns are the `THRESHOLD` / `--fail-under-lines` settings in the coverage steps of [`ci.yml`](../.github/workflows/ci.yml); the table below summarizes them (verify against that file):

| Language | Tool | Threshold | Exclusions | Output | Artifact |
|----------|------|-----------|------------|--------|----------|
| Go | `go test -coverprofile` | 80% line | `testutil/`, `metrics/`, `openapi_gen.go` | `server/coverage.out` | `go-coverage` |
| Rust | `cargo-llvm-cov` | 80% line | `main.rs`, `webrtc.rs`, `terminal.rs`, `session/mod.rs`, `session/relay.rs`, `tests/` | `agent/lcov.info` | `rust-coverage` |
| TypeScript | `@vitest/coverage-v8` | 80% line | — | `web/coverage/lcov.info` | `web-coverage` |

### Coverage Badges

The `merge-to-main` job updates three coverage badges on every successful `dev` push using `schneegans/dynamic-badges-action`. Each badge writes a JSON endpoint to a GitHub Gist, which `shields.io` renders as a dynamic badge in the README:

| Badge | Gist Filename | Color Range |
|-------|---------------|-------------|
| Go Server Coverage | `opengate-coverage.json` | 50–90% (red → green) |
| Rust Agent Coverage | `opengate-rust-coverage.json` | 50–90% (red → green) |
| Web Client Coverage | `opengate-web-coverage.json` | 50–90% (red → green) |

Each CI job posts a native Markdown summary (pass/fail counts, failed test names) to the GitHub Actions job summary tab for quick triage without digging into logs.

## Docker Hub Pull Resilience

Jobs that start Docker Hub images first invoke the local
[`docker-hub-mirror` composite action](../.github/actions/docker-hub-mirror/action.yml).
The action owns the daemon mirror configuration and optionally authenticates
the direct Docker Hub fallback when the repository credentials passed by the
workflows are available. Pull requests without secret access skip the login
step and retain the mirror plus anonymous fallback behavior.

The executable
[`docker-hub-mirror.test.sh`](../scripts/tests/docker-hub-mirror.test.sh)
regression test enforces one canonical mirror definition, verifies the
composite precedes every covered image pull, and requires every consumer to
pass the optional credentials.

## SonarCloud Quality Gate

The [`sonarcloud` job](../.github/workflows/ci.yml) runs after Go unit, Rust
test, and Web test jobs complete. It downloads all three coverage artifacts
and runs the pinned SonarQube scan action against the full codebase. If the
action download path fails, the job retries the same analysis through the
Docker scanner image using the shared Docker Hub pull protection. The scan is
skipped on scheduled runs.

Configuration lives in `sonar-project.properties` at the repo root (organization: `volchanskyi`, project key: `volchanskyi`).

Quality gate thresholds (configured in the SonarCloud UI, Clean-as-You-Code model — all conditions apply to *new code* only):

| Metric | Condition | Value |
|--------|-----------|-------|
| Coverage on new code | ≥ | 80% |
| Reliability rating on new code | = | A |
| Security rating on new code | = | A |
| Maintainability rating on new code | = | A |
| Security hotspots reviewed on new code | = | 100% |
| Duplicated lines on new code | < | 3% |

The three rating conditions (Reliability / Security / Maintainability = A) implicitly forbid any new bugs, vulnerabilities, or code smells — any such issue flips the corresponding rating from A to worse and fails the gate. Overall project coverage is enforced separately by the per-language unit-test jobs (`Go Unit Tests`, `Rust Tests`, `Web Unit Tests`), each of which fails at < 80%. SonarCloud itself does not gate on overall coverage.

Gate enforcement is done with `-Dsonar.qualitygate.wait=true` on the scan action — the job polls SonarCloud until the gate resolves and fails the step if any condition is breached. A failed `sonarcloud` job blocks the auto-merge to `main`. SonarCloud.io itself is the authoritative console for findings; they are not mirrored into the GitHub Code Scanning tab (see [ADR-013](./adr/ADR-013-docs-in-repo-and-immutable-adrs.md) for why the SARIF upload was dropped).

### Local SonarCloud Analysis

The same SonarCloud scan that runs in CI can be executed locally using the `sonarsource/sonar-scanner-cli` Docker image. This catches code smells, bugs, security hotspots, duplication, and coverage gate failures before pushing.

**Prerequisites:**
- Docker running
- `SONAR_TOKEN` — a SonarCloud User Token scoped to the `volchanskyi` organization. Generate one at sonarcloud.io/account/security. Set it via environment variable or in `.env` at the repo root (gitignored).

**Makefile targets:**

| Target | What it does | When to use |
|--------|-------------|-------------|
| `make sonar` | Generates all 3 coverage files, then runs the scanner | Before pushing — full CI parity |
| `make sonar-quick` | Runs the scanner without regenerating coverage | Quick check for code quality issues only |
| `make sonar-coverage` | Generates coverage files without running the scanner | When you only need coverage reports |

All targets reuse the existing `sonar-project.properties` configuration. The scanner runs with `-Dsonar.qualitygate.wait=true` and `-Dsonar.branch.name=dev`, matching CI behavior. Results appear on the SonarCloud dashboard under the `dev` branch analysis.

**Note:** The first run pulls the `sonarsource/sonar-scanner-cli` Docker image (~600 MB). Subsequent runs use the cached image. An active internet connection is required since the analysis runs against SonarCloud (not a local SonarQube instance).

## Failure Notifications

The `notify-failure` job runs when any upstream job fails on `push`, `schedule`, or `workflow_dispatch` events (PR failures are excluded — those are expected WIP). It uses the `gh` CLI (no third-party actions) to create GitHub Issues with enough detail to triage without opening the Actions tab.

**Per-job issue creation:** Each failed job in a run produces its own issue, titled `CI failure on {branch} in {job_name}`. This lets engineers assign, track, and close failures independently per concern area.

**Deduplication:** Before creating an issue, the script searches for an open issue with the `ci-failure` label matching the same title. If found, it appends a comment with the new run details instead of creating a duplicate. This keeps recurring failures (e.g., flaky tests) consolidated in one thread.

**Issue body contents:**
- Workflow name, branch, commit link, trigger type, run URL
- Failed job name and step names
- Last 80 lines of the failed job's log output (ANSI codes stripped, wrapped in a collapsible `<details>` block)
- Body truncated to 60,000 chars with a link to the full run if the log is very large

**No auto-close on success:** Issues are not automatically closed when the job passes again. Engineers must manually close after investigation to prevent masking flaky tests.

## Auto-Tagging and Changelog

After `merge-to-main` succeeds, the `auto-tag` job analyzes the merged conventional commits to determine the appropriate semver bump:

| Commit Pattern | Bump |
|----------------|------|
| `BREAKING CHANGE` in body or `!:` in subject | Major |
| `feat(scope): ...` | Minor |
| `fix(scope): ...` | Patch |
| `ci:`, `docs:`, `style:`, `refactor:`, `test:` | Skip (no tag) |

The job then:

1. **Generates a changelog entry** in [Keep a Changelog](https://keepachangelog.com/) format, categorizing commits into Added (feat), Fixed (fix), and Changed (refactor/perf) sections
2. **Commits** `CHANGELOG.md` with `[skip ci]` to prevent infinite loops
3. **Tags** the commit with the new version (e.g., `v0.2.0`)
4. **Pushes** using `SYNC_TOKEN` (PAT) so the tag push triggers downstream workflows like `release-agent.yml`

**Idempotency:** If the tag already exists (e.g., re-run), the job exits cleanly without error.

**Concurrency:** Uses `concurrency: { group: auto-tag, cancel-in-progress: false }` to serialize tag operations, preventing race conditions from concurrent merges.

## Branch Protection

Branch protection uses **repository rulesets** (not legacy branch protection rules).

| Branch | Mechanism | Rules |
|--------|-----------|-------|
| `main` | Legacy branch protection | No force pushes, no deletion. All 19 gate jobs required as status checks — pushes are blocked until every check passes. `merge-to-main` CI job is the only authorized writer. |
| `dev` | Ruleset: **CI Gate** | All 19 gate jobs as required status checks; no deletion; no force pushes. Repository admins bypass all rules (enables direct pushes for development). |

The **CI Gate** ruleset replaces legacy branch protection on `dev`. Key differences from the legacy approach:
- **Bypass actors:** Repository admins can push directly without passing status checks (legacy protection had `enforce_admins: false` which achieved the same effect, but rulesets make the bypass explicit).
- **`merge-to-main`** uses a Fine-grained PAT (`SYNC_TOKEN` secret) instead of `GITHUB_TOKEN`. On a personal repo, `github-actions[bot]` cannot be added as a ruleset bypass actor — only the admin role can bypass. The PAT authenticates as the repo owner, who has the admin bypass.
- **Code Scanning required tools:** CodeQL only. SonarCloud is not a Code Scanning tool because `SonarSource/sonarqube-scan-action` does not upload SARIF to GitHub Code Scanning for pull_request refs (only for push events to `dev`) — leaving every Dependabot PR `BLOCKED` waiting for SARIF that never arrived. SonarCloud's quality gate is still enforced via the `SonarCloud Analysis` required status check (which posts a regular PR check, not a Code Scanning entry). CodeQL stays as a Code Scanning required tool because it uploads SARIF correctly for both branches and PRs.

## Benchmark Trend Workflow

[`benchmark.yml`](../.github/workflows/benchmark.yml) runs Go and Rust benchmarks on a
nightly schedule and by `workflow_dispatch`:

- **Go benchmarks** — `testing.B` + `-benchmem` for protocol codec, cert signing, DB, handshake.
- **Rust benchmarks** — Criterion for frame/handshake encode/decode.

The workflow publishes canonical rows to VictoriaMetrics through
[`scripts/benchmark-vm-push.sh`](../scripts/benchmark-vm-push.sh) and hard-gates
regressions in two ways, by metric class:

- **Deterministic allocation metrics** (`allocs/op`, `bytes/op`) are gated against the
  committed [`benchmarks/baseline.json`](../benchmarks/baseline.json) at ±2% — the same
  code yields the same count, so a small fixed tolerance never false-fires.
- **Machine-dependent `ns/op`** is hard-gated against a noise-robust VictoriaMetrics
  window baseline read back through [`scripts/lib/vm-query.sh`](../scripts/lib/vm-query.sh):
  a run reds when its `ns/op` exceeds **either** the 14-day window median × a frozen
  relative band **or** an absolute ceiling anchored on the committed baseline (the
  drift-proof boiling-frog backstop). The band and ceiling are calibrated from the live
  series' measured run-to-run variance, not hand-picked. The gate is fail-open: a VM or
  transport failure falls back to the absolute rule only and never reds on infra, and a
  cold-start window (too few samples) skips the relative rule. This replaces the earlier
  advisory-only `ns/op` treatment.

All benchmark trends are also rendered in Grafana's **Benchmark Trends** dashboard.

## Load-Test Trend Workflow

[`load-test.yml`](../.github/workflows/load-test.yml) runs the staging k6 and
QUIC load scenarios on its own schedule and by `workflow_dispatch`; it is not in
the `merge-to-main` gate graph. The run job uploads the canonical summary rows,
and the publish job reads the VictoriaMetrics window baseline through
[`scripts/loadtest-regression-check.sh`](../scripts/loadtest-regression-check.sh),
pushes the current rows through
[`scripts/loadtest-vm-push.sh`](../scripts/loadtest-vm-push.sh), sends Telegram on
regression, and then fails the workflow red for an audit trail.

The regression semantics are recorded in
[ADR-045](./adr/ADR-045-load-test-regression-gate.md). In short: latency and rps
are evaluated per `{source, scenario, phase}` against VM read-back baselines plus
absolute limits, error rate has hard ceilings, p99 is advisory-only, and missing
VM history or transport failure does not create a false red.

## Frontend Performance Monitoring

Four tools monitor frontend performance at different layers:

### Bundle Size (CI Gate)

The `web-bundle-size` job uses [size-limit](https://github.com/ai/size-limit) with the `@size-limit/file` plugin to enforce gzip size limits. Configuration: `web/.size-limit.json`.

| Target | Limit | Typical |
|--------|-------|---------|
| JS bundle | 250 KB gzip | ~202 KB |
| CSS bundle | 10 KB gzip | ~5 KB |
| Total | 260 KB gzip | ~207 KB |

This is a **gate job** — a size regression blocks merge-to-main. A size report table is written to the GitHub Actions step summary.

### Lighthouse CI (E2E Job)

After Playwright tests pass, [Lighthouse CI](https://github.com/GoogleChrome/lighthouse-ci) audits `http://localhost:8080/login` (3 runs, desktop preset, no throttling). Configuration: `web/.lighthouserc.json`.

| Category | Threshold | Action |
|----------|-----------|--------|
| Performance | ≥ 70 | **warn** (CI runner variance) |
| Accessibility | ≥ 90 | **error** (hard fail) |
| Best Practices | ≥ 90 | **error** (hard fail) |
| CLS | ≤ 0.1 | **error** |
| FCP | ≤ 3000ms | **warn** |
| LCP | ≤ 4000ms | **warn** |
| TBT | ≤ 500ms | **warn** |

Results are uploaded as the `lighthouse-results` artifact and a score summary is added to the step summary.

### Browser Performance Evidence

Lighthouse and bundle-size evidence stays per-run: Lighthouse uploads the
`lighthouse-results` artifact from the `e2e` job, and bundle size uploads the
`bundle-size-report` artifact from the `web-bundle-size` job.

### PageSpeed Insights (CD — Informational)

PageSpeed Insights is not part of the current CD workflow. Browser performance
evidence comes from Lighthouse CI in the `e2e` job and the bundle-size gate in
`web-bundle-size`. If PageSpeed is reintroduced, document the workflow step and
secret in [`cd.yml`](../.github/workflows/cd.yml) at the same time.

## Dependabot Flow

Dependabot PRs target `dev` directly — same target a human contributor would use. The flow:

1. Dependabot opens a grouped PR against `dev` (one PR per ecosystem)
2. CI runs on the PR (`pull_request` trigger — same 19 gate jobs as a human PR)
3. [`dependabot-auto-merge.yml`](../.github/workflows/dependabot-auto-merge.yml) classifies the update via `dependabot/fetch-metadata`:
   - **Patch + minor:** calls `gh pr merge --auto --squash`. GitHub auto-merges once required checks pass.
   - **Major:** stays open with a "needs human review" comment.
4. Once merged into `dev`, the next CI run on the `dev` push fires `merge-to-main`, forwarding the commit to `main` — same as any human commit.

Grouping configuration in [`.github/dependabot.yml`](../.github/dependabot.yml) batches all updates per ecosystem into a single PR. Auto-merge requires the repository setting "Allow auto-merge" (Settings → General).
