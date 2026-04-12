# CI Pipeline

## Triggers

Every push to `dev`, `main`, or `dependabot-dev` and every pull request targeting `main`, `dev`, or `dependabot-dev` runs the CI pipeline. CodeQL and security scanning run on every push and PR (no separate schedule). Load testing and branch sync have their own scheduled workflows.

## Branching Flow

```
dependabot/* ──► dependabot-dev ──┐
                 (CI-gated)       ├──► main
dev (features) ───────────────────┘    │
                 (CI-gated)            ├──► dev (auto-sync)
                                       └──► dependabot-dev (auto-sync)
```

- **`dependabot-dev`** — integration branch for all Dependabot dependency updates. Dependabot PRs target this branch. After all CI checks pass, the `merge-to-main` job automatically merges directly into `main`.
- **`dev`** — primary development branch. All feature work lands here. After all CI checks pass, the `merge-to-main` job automatically merges into `main`.
- **`main`** — stable branch. Receives code from both `dev` and `dependabot-dev` via automated CI merge jobs. Protected: requires 1 PR review for non-admin pushes; force-push and deletion disabled.
- **Auto-sync** — after `merge-to-main` completes, a separate `sync-branches` job syncs `main` back to the other branch (the one that did not trigger the merge). A nightly workflow (04:00 UTC) also syncs `main` → both branches as a safety net.

## Job Graph

```
                          CI Workflow
   push → dev|main|dependabot-dev  /  pull_request → main|dev|dependabot-dev
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
                                                    Auto-merge {dev|dependabot-dev} → main
                                                    └─ Update coverage badge (only on push to dev)
                                                                        │
                                                                        ▼
                                                    Auto-tag release (needs merge-to-main)
                                                    └─ Conventional commits → semver bump → CHANGELOG.md + git tag
                                                                        │
                                                                        ▼
                                                    Sync branches (needs merge-to-main, auto-tag)
                                                    └─ Sync main → other branch (loop-safe via GITHUB_TOKEN)

               └─────────────── any job fails (non-PR) ─────────────────┐
                                                                        ▼
                                                    Notify failure (needs all 22 upstream jobs)
                                                    └─ Creates/updates GitHub Issue per failed job

           Go Benchmarks Workflow      Rust Benchmarks Workflow
           (CI success on dev)         (CI success on dev)
                    │                           │
                    ▼                           ▼
              Go Benchmarks              Rust Benchmarks
                    │                           │
                    └────── stored in gh-pages ──┘

        Perf Publish (needs e2e + bundle-size, dev push only → gh-pages)
        └─ Lighthouse history + bundle size trending

        Build & Push Container Image Workflow
        (main push / CI success on dev)
                    │
                    ▼
              Build multi-arch → Push GHCR → Cosign sign → SBOM attest → Trivy scan
                    │
                    ▼
              CD Workflow (cd.yml)
              ├─ resolve-tag + cosign verify
              ├─ deploy-staging (manual approval) → cosign verify (VPS) → smoke-test → PSI → [rollback]
              └─ deploy-production (manual approval) → cosign verify (VPS) → health-check → [rollback]
```

## Jobs

The CI workflow contains **26 jobs** grouped by concern:

| Group | Jobs | Purpose |
|-------|------|---------|
| **Rust** | `rust-lint`, `rust-test` | `cargo fmt` + clippy, nextest + golden file generation + llvm-cov coverage |
| **Go** | `go-lint`, `go-unit`, `go-integration` | `go vet` + OpenAPI codegen sync check, unit tests with coverage, QUIC integration tests |
| **Web** | `web-lint`, `web-unit`, `web-integration` | ESLint; unit/component tests (with v8 coverage) + Vite build; integration tests |
| **Bundle Size** | `web-bundle-size` | `size-limit` gzip size check (JS ≤250KB, CSS ≤10KB, Total ≤260KB). Runs in parallel with other web jobs. |
| **API Docs** | `deploy-api-docs` | Deploys OpenAPI spec + Scalar viewer to gh-pages (dev push only) |
| **Config** | `config-lint` | actionlint, yamllint, `terraform fmt/validate`, tflint, `docker compose config`, `caddy fmt/validate`, Trivy IaC scan, cross-config integration tests |
| **Golden** | `golden` | Cross-language wire format verification (needs `rust-test` artifact) |
| **Security** | `security-audit` | govulncheck, cargo audit, npm audit |
| **CodeQL** | `codeql-go`, `codeql-js`, `codeql-rust` | GitHub Code Scanning with `security-and-quality` queries |
| **SonarCloud** | `sonarcloud` | Static analysis + coverage aggregation via SonarSource scan action |
| **E2E** | `e2e` | Playwright end-to-end + Lighthouse CI audits via `docker-compose.test.yml` (needs all prior checks + bundle-size) |
| **Perf Publish** | `perf-publish` | Publishes Lighthouse scores and bundle size history to gh-pages for trending (dev push only, not gated) |
| **Load** | `load-test` | k6 HTTP/WS load test scenarios (on-demand/scheduled only) |
| **Merge** | `merge-to-main` | Auto-merge `dev` or `dependabot-dev` → `main` after all 19 gate jobs pass (including E2E + bundle-size + benchmarks); updates Go/Rust/Web coverage badges on `dev` pushes |
| **Auto-tag** | `auto-tag` | Determines semver bump from conventional commits, generates Keep a Changelog entry, commits CHANGELOG.md, and pushes a git tag (triggers `release-agent.yml`) |
| **Sync** | `sync-branches` | Sync `main` back to the other branch after a successful merge (runs as a separate job so sync failures don't block the merge or badge update) |
| **Notify** | `notify-failure` | Auto-creates GitHub Issues when any job fails (push/schedule/dispatch only — not PRs). One issue per failed job per branch, with error log excerpts. |

## Sequencing

The **golden verification** job is sequenced after Rust so the Go verifier always works against freshly generated fixtures — this prevents Rust ↔ Go wire-format drift from going undetected.

Pull requests execute every job except auto-merge. Benchmarks only run on `dev` pushes.

### OpenAPI Codegen Sync

The `go-lint` job verifies that generated Go code from the OpenAPI spec is up to date. It runs `go generate ./internal/api/` and then `git diff --exit-code` — if the generated output differs from what is committed, the job fails. This can also be checked locally via `make verify-codegen`.

## Coverage

All three language test jobs enforce an **80% minimum coverage** threshold — the build fails if coverage of production code drops below this level.

| Language | Tool | Threshold | Exclusions | Output | Artifact |
|----------|------|-----------|------------|--------|----------|
| Go | `go test -coverprofile` | 80% line | `testutil/`, `metrics/`, `mps/wsman/`, `openapi_gen.go` | `server/coverage.out` | `go-coverage` |
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

## SonarCloud Quality Gate

The `sonarcloud` job runs after Go unit, Rust test, and Web test jobs complete. It downloads all three coverage artifacts and runs `SonarSource/sonarqube-scan-action@v7` against the full codebase. The scan is skipped on scheduled runs.

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

Gate enforcement is done with `-Dsonar.qualitygate.wait=true` on the scan action — the job polls SonarCloud until the gate resolves and fails the step if any condition is breached. A failed `sonarcloud` job blocks the auto-merge to `main`. SonarCloud.io itself is the authoritative console for findings; there is no SARIF export or duplication into the GitHub Code Scanning tab (a previous SARIF upload step was removed in commit [9236826](https://github.com/volchanskyi/opengate/commit/9236826) because dismissed-fingerprint matching kept new alerts invisible).

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
| `dev`, `dependabot-dev` | Ruleset: **CI Gate** | All 19 gate jobs as required status checks; no deletion; no force pushes. Repository admins bypass all rules (enables direct pushes for development and CI sync pushes). |

The **CI Gate** ruleset replaces legacy branch protection on `dev` and `dependabot-dev`. Key differences from the legacy approach:
- **Bypass actors:** Repository admins can push directly without passing status checks (legacy protection had `enforce_admins: false` which achieved the same effect, but rulesets make the bypass explicit).
- **CI sync pushes:** The `sync-branches` job and `sync-from-main.yml` use a Fine-grained PAT (`SYNC_TOKEN` secret) instead of `GITHUB_TOKEN`. On a personal repo, `github-actions[bot]` cannot be added as a ruleset bypass actor — only the admin role can bypass. The PAT authenticates as the repo owner, who has the admin bypass.
- **Single ruleset covers both branches:** One "CI Gate" ruleset targets `refs/heads/dev` and `refs/heads/dependabot-dev`, reducing configuration drift.

## Benchmark Workflows

Two independent workflows, each triggered by `workflow_run` when CI completes successfully on `dev`:

- **Go Benchmarks** — `testing.B` + `-benchmem` for protocol codec, cert signing, DB, handshake
- **Rust Benchmarks** — Criterion for frame/handshake encode/decode

Results are committed to `gh-pages` for historical tracking.

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

### Performance Trend Tracking (gh-pages)

The `perf-publish` job (dev push only, non-blocking) pushes Lighthouse scores and bundle size data to `gh-pages/dev/perf/` for historical trending. Keeps the last 100 entries.

### PageSpeed Insights (CD — Informational)

During staging deployment in `cd.yml`, a PageSpeed Insights API call audits the production `/login` page. This provides real-world CrUX field data that Lighthouse CI cannot measure. Skips gracefully if `PSI_API_KEY` is not configured.

## Dependabot Isolation

Dependabot PRs target `dependabot-dev` (not `dev` directly) to prevent broken dependency updates from interfering with feature development:

1. Dependabot opens a grouped PR against `dependabot-dev` (one PR per ecosystem)
2. `dependabot-auto-merge.yml` approves and enables GitHub auto-merge (squash)
3. CI runs on the PR — if all checks pass, GitHub merges into `dependabot-dev`
4. CI runs on the `dependabot-dev` push — if all 19 gate jobs pass, `merge-to-main` merges directly into `main`
5. `sync-branches` job syncs `main` back into `dev` (loop-safe: `GITHUB_TOKEN` pushes do not re-trigger workflows)
6. A nightly sync workflow (`sync-from-main.yml`, 04:00 UTC) syncs `main` → both `dev` and `dependabot-dev` as a safety net

Grouping configuration in `.github/dependabot.yml` batches all updates per ecosystem into a single PR.
