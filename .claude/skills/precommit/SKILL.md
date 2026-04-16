---
name: precommit
description: |
  Run all mandatory pre-commit checks: lints, tests, benchmarks, coverage, and documentation.
  Use before every commit. Blocks commit if any check fails.
---

# Pre-Commit Checklist

Run ALL lints, ALL tests, test coverage, and ALL benchmarks. No exceptions. All tests MUST pass regardless of having pre-existing issues or being flaky.

## Prerequisites (verify before running any step)

Both prerequisites are MANDATORY. If either is missing, FAIL the precommit run immediately with a clear alert — do not skip steps that depend on them.

- **Postgres reachable on `localhost:5432`** with `POSTGRES_TEST_URL` exported. Without this, every Postgres-dependent Go test skips silently (see [server/internal/mps/mps_test.go:28-30](../../../server/internal/mps/mps_test.go#L28-L30), [server/internal/api/store_failure_test.go:21-23](../../../server/internal/api/store_failure_test.go#L21-L23), [server/internal/api/health_handler_test.go:32-34](../../../server/internal/api/health_handler_test.go#L32-L34)), step 13 coverage falls below 80%, and the resulting `server/coverage.out` excludes Postgres code paths — so the local SonarCloud scan in step 16 cannot evaluate Postgres-related code. To start a disposable instance matching CI ([.github/workflows/ci.yml:142-156](../../../.github/workflows/ci.yml#L142-L156)):
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
5. `make lint-deploy` — Deploy config validation (yamllint, terraform, tflint, compose, caddy, trivy, integration tests). Fails loudly if any tool is missing — all are required for CI parity:
   - `yamllint -c .yamllint.yml deploy/` — YAML lint on deploy configs
   - `terraform -chdir=deploy/terraform fmt -check -recursive` — Terraform format check
   - `terraform -chdir=deploy/terraform init -backend=false && terraform -chdir=deploy/terraform validate` — Terraform validation
   - `tflint --init --chdir=deploy/terraform && tflint --chdir=deploy/terraform --format=compact` — Terraform linting
   - `docker compose config --quiet` (production, staging, test) — Docker Compose validation
   - `caddy fmt --diff` + `caddy validate` on both Caddyfiles — Caddyfile validation
   - `trivy config --severity HIGH,CRITICAL --exit-code 1 deploy/` + Dockerfile — IaC security scan
   - `bash deploy/tests/validate-configs.sh` — Cross-config consistency tests (ports, env vars, tfvars)

## Codegen sync (must pass)

6. `PATH="$HOME/go/bin:$PATH" make verify-codegen` — Verify OpenAPI generated code is in sync. This MUST actually run (not skip). If it prints "SKIP", install the tool first: `go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.6.0`. A "SKIP" is a FAILURE — do not proceed to commit.

## Tests (all must pass)

7. `cd server && go test -race -timeout 5m -coverprofile=coverage.out -covermode=atomic ./internal/...` — Go unit tests with coverage (also run `go test -race -timeout 5m ./tests/...` for integration tests)
8. `cd agent && cargo test --workspace` — Rust tests (all crates)
9. `cd web && npx vitest run --coverage` — Web tests with coverage

## E2E tests (all must pass)

10. `make e2e` — Playwright E2E tests (spins up docker-compose.test.yml, runs all specs, tears down). Requires Docker running.

## Security audit (must pass)

11. `cd web && npm audit --audit-level=high` — npm dependency vulnerability scan
12. `cd agent && cargo audit` — Rust dependency vulnerability scan (mirrors CI Security Audit job). Install once with `cargo install cargo-audit@0.22.1`. Vulnerabilities fail the gate; warnings (unmaintained/unsound/yanked) are advisory.

## Coverage (all must meet 80% threshold)

13. **Go coverage** — Run `cd server && go test -race -timeout 5m -coverprofile=coverage.out -covermode=atomic ./internal/...` then filter and check:
    ```
    grep -v -E '/(testutil|metrics|mps/wsman)/|api/openapi_gen\.go' coverage.out > coverage-prod.out
    go tool cover -func=coverage-prod.out | grep total
    ```
    Total must be >= 80%.

14. **Web coverage** — Run `cd web && npx vitest run --coverage` then check:
    ```
    node -e "const s=require('./coverage/coverage-summary.json');const l=s.total.lines.pct;console.log('Web line coverage: '+l+'%');process.exit(l<80?1:0)"
    ```
    Lines must be >= 80%.

15. **Rust coverage** — Run locally:
    ```
    cd agent && cargo llvm-cov nextest --workspace --fail-under-lines 80 \
      --ignore-filename-regex '(main\.rs|/webrtc\.rs|/terminal\.rs|/session/mod\.rs|/session/relay\.rs|/tests/)'
    ```
    Requires `cargo-llvm-cov` and `cargo-nextest` (`cargo install cargo-llvm-cov cargo-nextest`). Must be >= 80%.

## SonarCloud local scan (mandatory)

16. `make sonar-quick` — Run SonarCloud analysis locally via Docker. Catches code smells, bugs, security hotspots, and duplication that CI would flag. Requires Docker running and `SONAR_TOKEN` set (verified in the Prerequisites section above). The scan must include Postgres-related code paths — guaranteed by the Postgres prerequisite, which lets step 13 produce coverage that exercises `server/internal/db/postgres.go`, `server/internal/mps/`, and other Postgres-dependent packages. **If `SONAR_TOKEN` is missing, invalid, or the scanner reports an authentication failure, FAIL the precommit and alert the user — do NOT skip.** A missing token usually means `.env` was not sourced or the token entry was deleted; surface the issue rather than silently bypassing the gate. **If the scanner image pull from Docker Hub fails with `unexpected EOF` while the host is on a VPN**, this is the known PMTUD blackhole — alert the user to either disconnect the VPN or lower WSL2 MTU (`sudo ip link set dev eth0 mtu 1380`) before retrying.

## Benchmarks (all must run without errors)

17. `cd server && go test -bench=. -benchmem -run='^$' ./internal/...` — Go benchmarks
18. `cd agent && cargo bench -p mesh-protocol` — Rust benchmarks

## Documentation (mandatory on every commit)

19. **`README.md`** (root) — If the commit changes anything covered by existing README sections (commands, setup, architecture, etc.), update those sections to stay accurate. Do NOT add new sections.
20. **`/docs`** — Update the relevant pages under [`docs/`](../../../docs/) to reflect all changes. `/docs` is the canonical reference for senior engineers — it must be comprehensive, accurate, and always in sync with the codebase. Follow the link-over-paraphrase and ADR-immutability conventions in [`docs/README.md`](../../../docs/README.md). Run `/wiki-audit` if the commit touches CI, deploy configs, version pins, or anything a doc page might reference by literal value. New architectural decisions go in [`docs/adr/`](../../../docs/adr/) as a new file — never by editing an accepted ADR in place.

## Gate Criteria

Do NOT commit if:
- Any lint fails
- Any test fails (unit, integration, or E2E)
- Go, Web, or Rust overall coverage is below 80% (steps 13-15)
- SonarCloud quality gate fails (step 16) — including when `SONAR_TOKEN` is missing/invalid or the scanner cannot reach SonarCloud
- Any benchmark errors out
- Any security audit fails (high+ severity vulnerabilities — npm or cargo)
- Documentation is stale
