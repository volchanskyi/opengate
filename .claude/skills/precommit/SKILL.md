---
name: precommit
description: |
  Run all mandatory pre-commit checks: lints, tests, benchmarks, coverage, and documentation.
  Use before EVERY commit, including docs-only and CI-only commits. Blocks commit if any check fails.
---

# Pre-Commit Checklist

Run ALL lints, ALL tests, test coverage, and ALL benchmarks. No exceptions. All tests MUST pass regardless of having pre-existing issues or being flaky.

## Scope: every commit, no exemptions

This checklist runs on **every** commit on `dev`, regardless of what the diff touches. There is no "docs-only" or "CI-only" or "trivial change" exemption. Reasoning:

- **Security audits read lockfiles, not the diff.** `cargo audit`, `govulncheck`, and `npm audit` evaluate `agent/Cargo.lock`, `server/go.sum`, and `web/package-lock.json` against the current advisory database. A new advisory published yesterday will fail the gate today even if today's commit only edits a Markdown file. Skipping the audit on a docs commit means the next push (any push) inherits a CI failure that had nothing to do with its changes — exactly the failure mode that produced RUSTSEC-2026-0104 on `dev`.
- **Lockfiles, CI workflows, and config files all gate on the full repo state.** A workflow edit can break `actionlint`; a `deploy/` tweak can fail `make lint-deploy`; a doc edit that touches a code-fenced command can break `/wiki-audit`. None of these are detectable by inspecting only the changed file.
- **Skipping selectively is how gates rot.** Once "docs are exempt" is acceptable, "small CI tweaks are exempt" follows, then "obvious one-liners," and the gate stops being a gate.

If a step is genuinely irrelevant to the change (e.g. running Rust benchmarks for a typo fix in `README.md`), it still runs — the cost of running it is far smaller than the cost of one missed regression. The only acceptable reason to skip a step is a tooling outage that prevents the step from running at all, and in that case the precommit FAILS and the user is alerted; it does not silently pass.

## Prerequisites (verify before running any step)

All prerequisites are MANDATORY. If any is missing, FAIL the precommit run immediately with a clear alert — do not skip steps that depend on them.

- **No conflicting Go install at `$HOME/go`.** Run `[ -d "$HOME/go/src/net" ] || [ -f "$HOME/go/VERSION" ] && echo CONFLICT || echo ok` — if it prints `CONFLICT`, a Go installation has been extracted into `$HOME/go`. That directory is the default `GOPATH` when `GOPATH` is unset, so the Go toolchain ends up searching stdlib in two places and `govulncheck` fails with "redeclared in this block" build errors against `$HOME/go/src/net/*.go`. Fix by removing the manual install (`rm -rf $HOME/go`), keeping a snap or apt-managed `go` binary on PATH, and ensuring `~/.bashrc` exports `GOPATH=$HOME/go-workspace` (or any path that is **not** a Go install root).

- **Postgres reachable on `localhost:5432`** with `POSTGRES_TEST_URL` exported. Without this, every Postgres-dependent Go test skips silently (see [server/internal/mps/mps_test.go:28-30](../../../server/internal/mps/mps_test.go#L28-L30), [server/internal/api/store_failure_test.go:21-23](../../../server/internal/api/store_failure_test.go#L21-L23), [server/internal/api/health_handler_test.go:32-34](../../../server/internal/api/health_handler_test.go#L32-L34)), the step 16 coverage gate falls below 80%, and the resulting `server/coverage.out` excludes Postgres code paths — so the local SonarCloud scan in step 19 cannot evaluate Postgres-related code. To start a disposable instance matching CI ([.github/workflows/ci.yml:142-156](../../../.github/workflows/ci.yml#L142-L156)):
  ```bash
  docker run -d --name og-precommit-pg --rm \
    -e POSTGRES_USER=opengate -e POSTGRES_PASSWORD=opengate -e POSTGRES_DB=opengate_test \
    -p 5432:5432 postgres:17-alpine
  until docker exec og-precommit-pg pg_isready -U opengate -d opengate_test >/dev/null 2>&1; do sleep 1; done
  export POSTGRES_TEST_URL='postgres://opengate:opengate@localhost:5432/opengate_test?sslmode=disable'
  ```
  Stop with `docker stop og-precommit-pg` after the run.

- **`SONAR_TOKEN` exported** (from environment or `.env` via `set -a; . ./.env; set +a`). Verify with `[ -n "$SONAR_TOKEN" ] && echo ok || echo MISSING`. A missing or invalid token is a setup defect, not a reason to bypass the SonarCloud gate.

## Lints (all must pass)

These lints mirror the CI config-lint job exactly. Every check that runs in CI MUST also run locally.

1. `cd agent && cargo fmt --all -- --check && cargo clippy --workspace -- -D warnings` — Rust format + clippy
2. `cd server && go vet ./...` — Go vet
3. `cd web && npx eslint .` — Web ESLint
4. `~/go/bin/actionlint` — GitHub Actions workflow lint (ALWAYS run locally, no exceptions). Runs with `shellcheck` and `pyflakes` for full parity with CI (both are installed locally).
5. `make taint-go && make taint-web` — Static taint linting (gosec for Go, eslint-plugin-security + eslint-plugin-no-unsanitized for web). Surfaces source→sink data-flow issues that grep-based audits miss. CI hard-gates land in PR 9 of the structural-testing rollout; until then this is the early-warning system — every finding here predicts a future CI failure.
6. `make dead-code` — Dead-code & unused-symbol sweep (clippy `-W dead_code`, staticcheck `U1000`, ts-prune). Findings here are a leading indicator of churn during the PR 3 baseline cleanup. Surface them locally so the cleanup PR does not balloon mid-flight.
7. `make lint-deploy` — Deploy config validation (yamllint, terraform, tflint, compose, caddy, trivy, integration tests). Fails loudly if any tool is missing — all are required for CI parity:
   - `yamllint -c .yamllint.yml deploy/` — YAML lint on deploy configs
   - `terraform -chdir=deploy/terraform fmt -check -recursive` — Terraform format check
   - `terraform -chdir=deploy/terraform init -backend=false && terraform -chdir=deploy/terraform validate` — Terraform validation
   - `tflint --init --chdir=deploy/terraform && tflint --chdir=deploy/terraform --format=compact` — Terraform linting
   - `docker compose config --quiet` (production, staging, test) — Docker Compose validation
   - `caddy fmt --diff` + `caddy validate` on both Caddyfiles — Caddyfile validation
   - `trivy config --severity HIGH,CRITICAL --exit-code 1 deploy/` + Dockerfile — IaC security scan
   - `bash deploy/tests/validate-configs.sh` — Cross-config consistency tests (ports, env vars, tfvars)

## Codegen sync (must pass)

8. `PATH="$HOME/go/bin:$PATH" make verify-codegen` — Verify OpenAPI generated code is in sync. This MUST actually run (not skip). If it prints "SKIP", install the tool first: `go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.6.0`. A "SKIP" is a FAILURE — do not proceed to commit.

## Tests (all must pass)

9. `cd server && go test -race -timeout 5m -coverprofile=coverage.out -covermode=atomic ./internal/...` — Go unit tests with coverage (also run `go test -race -timeout 5m ./tests/...` for integration tests)
10. `cd agent && cargo test --workspace` — Rust tests (all crates)
11. `cd web && npx vitest run --coverage` — Web tests with coverage

## E2E tests (all must pass)

12. `make e2e` — Playwright E2E tests (spins up docker-compose.test.yml, runs all specs, tears down). Requires Docker running.

## Security audit (must pass)

13. `cd server && govulncheck ./...` — Go vulnerability scan (mirrors CI Security Audit job). Install once with `go install golang.org/x/vuln/cmd/govulncheck@v1.1.4`. Any reported vulnerability fails the gate.
14. `cd web && npm audit --audit-level=high` — npm dependency vulnerability scan
15. `cd agent && cargo audit` — Rust dependency vulnerability scan (mirrors CI Security Audit job). Install once with `cargo install cargo-audit@0.22.1`. Vulnerabilities fail the gate; warnings (unmaintained/unsound/yanked) are advisory.

## Coverage (all must meet 80% threshold)

16. **Go coverage** — Run `cd server && go test -race -timeout 5m -coverprofile=coverage.out -covermode=atomic ./internal/...` then filter and check:
    ```
    grep -v -E '/(testutil|metrics|mps/wsman)/|api/openapi_gen\.go' coverage.out > coverage-prod.out
    go tool cover -func=coverage-prod.out | grep total
    ```
    Total must be >= 80%.

17. **Web coverage** — Run `cd web && npx vitest run --coverage` then check:
    ```
    node -e "const s=require('./coverage/coverage-summary.json');const l=s.total.lines.pct;console.log('Web line coverage: '+l+'%');process.exit(l<80?1:0)"
    ```
    Lines must be >= 80%.

18. **Rust coverage** — Run locally:
    ```
    cd agent && cargo llvm-cov nextest --workspace --fail-under-lines 80 \
      --ignore-filename-regex '(main\.rs|/webrtc\.rs|/terminal\.rs|/session/mod\.rs|/session/relay\.rs|/tests/)'
    ```
    Requires `cargo-llvm-cov` and `cargo-nextest` (`cargo install cargo-llvm-cov cargo-nextest`). Must be >= 80%.

## SonarCloud local scan (mandatory)

19. `make sonar-quick` — Run SonarCloud analysis locally via Docker. Catches code smells, bugs, security hotspots, and duplication that CI would flag. Requires Docker running and `SONAR_TOKEN` set (verified in the Prerequisites section above). The scan must include Postgres-related code paths — guaranteed by the Postgres prerequisite, which lets step 16 produce coverage that exercises `server/internal/db/postgres.go`, `server/internal/mps/`, and other Postgres-dependent packages. **If `SONAR_TOKEN` is missing, invalid, or the scanner reports an authentication failure, FAIL the precommit and alert the user — do NOT skip.** A missing token usually means `.env` was not sourced or the token entry was deleted; surface the issue rather than silently bypassing the gate. **If the scanner image pull from Docker Hub fails with `unexpected EOF` while the host is on a VPN**, this is the known PMTUD blackhole — alert the user to either disconnect the VPN or lower WSL2 MTU (`sudo ip link set dev eth0 mtu 1380`) before retrying.

## Benchmarks (all must run without errors)

20. `cd server && go test -bench=. -benchmem -run='^$' ./internal/...` — Go benchmarks
21. `cd agent && cargo bench -p mesh-protocol` — Rust benchmarks

## Documentation (mandatory on every commit)

22. **`README.md`** (root) — If the commit changes anything covered by existing README sections (commands, setup, architecture, etc.), update those sections to stay accurate. Do NOT add new sections.
23. **`/docs`** — Update the relevant pages under [`docs/`](../../../docs/) to reflect all changes. `/docs` is the canonical reference for senior engineers — it must be comprehensive, accurate, and always in sync with the codebase. Follow the link-over-paraphrase and ADR-immutability conventions in [`docs/README.md`](../../../docs/README.md). Run `/wiki-audit` if the commit touches CI, deploy configs, version pins, or anything a doc page might reference by literal value. New architectural decisions go in [`docs/adr/`](../../../docs/adr/) as a new file — never by editing an accepted ADR in place.

## Mutation testing (not part of /precommit)

`make mutate-{rust,go,web}` (and the umbrella `make mutate`) are **developer-only** commands. Use them when working on test-gap closure (PR 6/7/8 of the structural-testing rollout established baselines per language). They are **not** part of /precommit because:

- The PR 9 design ([phases.md](../../phases.md) row "Structural Testing PR 9: Mutation testing as observability") ships mutation testing as a **nightly scheduled workflow** (`.github/workflows/mutation.yml` at 03:00 UTC, **not** in `merge-to-main.needs[]`), with Loki/Grafana trend tracking and Telegram regression alerts. The local mutation step is **not** a commit-time gate and never became one.
- Running the full mutation tree (rust ~25 min + go ~5 min + web ~16 min ≈ 45 min) on every commit, including docs-only commits, is uneconomical and provides no signal not already captured by the nightly job.

If you suspect a specific commit may regress a mutation score, run the affected language's `make mutate-<lang>` ad-hoc and inspect the Grafana `opengate-mutation-trend` dashboard for context.

## Gate Criteria

Do NOT commit if:
- Any lint fails (steps 1–7, including new taint and dead-code surveys)
- Any test fails (unit, integration, or E2E)
- Go, Web, or Rust overall coverage is below 80% (steps 16-18)
- SonarCloud quality gate fails (step 19) — including when `SONAR_TOKEN` is missing/invalid or the scanner cannot reach SonarCloud
- Any benchmark errors out
- Any security audit fails (any govulncheck finding, or high+ severity vulnerabilities — npm or cargo)
- Documentation is stale

## Marker file (mandatory final step)

After every gate above passes, run this from the repo root as the absolute last step before returning control to the user:

    mkdir -p .claude/.markers
    git write-tree > .claude/.markers/precommit.head

`.claude/hooks/pretooluse-git-commit-guard.sh` (Claude Hooks PR 2) reads this file and blocks every `git commit` whose `git write-tree` does not match the marker. Re-staging any file invalidates the marker — re-run `/precommit`. **There is NO bypass** — the only way to change enforcement is editing `.claude/settings.json`.

If ANY gate above failed, do NOT write the marker. The marker is the proof that the full gauntlet passed.

## TDD interaction

`.claude/hooks/pretooluse-tdd-gate.sh` blocks the first `Write/Edit/MultiEdit` of a source-language file on a branch with no test changes, well before `/precommit` runs. `/precommit` no longer needs an explicit TDD-presence check; the hook fired earlier. As a defensive sanity check, alert the user if `scripts/tdd-check.sh has-test-change` is false while the branch diff contains source-language changes — that indicates a hook misconfiguration, not a missed test, and the user should investigate the hook chain.
