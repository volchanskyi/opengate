# Tooling

## Commands

- `make build` — build all components
- `make test` — run all tests
- `make lint` — clippy + go vet + eslint + actionlint
- `make golden` — cross-language compatibility check
- `make e2e` — run Playwright end-to-end tests
- `make sonar` — full local SonarCloud scan (generates coverage + runs scanner via Docker)
- `make sonar-quick` — code-quality-only SonarCloud scan (no coverage generation)
- `make sonar-coverage` — generate all coverage files for SonarCloud
- `make mutate` (and `mutate-rust` / `mutate-go` / `mutate-web`) — mutation tests across all three languages (cargo-mutants / gremlins / stryker)
- `make taint-go` / `make taint-web` — static taint linting (gosec; eslint-plugin-security + eslint-plugin-no-unsanitized via `web/eslint.security.config.js`)
- `make dead-code` — dead-code sweep (clippy `-W dead_code`, staticcheck `U1000`, ts-prune)
- `cd server && oapi-codegen -config oapi-codegen.yaml ../api/openapi.yaml > internal/api/openapi_gen.go` — regenerate Go API from OpenAPI spec
- `cd web && npm run generate:api` — regenerate TypeScript types from OpenAPI spec

## Use `make e2e`, not bare `npx playwright test`

`make e2e` owns the full Docker Compose lifecycle (`up --build --wait` → `playwright test` → `down -v`). The bare `npx playwright test` invocation relies on Playwright's `webServer` block with a 180s timeout that is too short for cold Docker builds; tests fail before the stack is ready. This applies inside `/precommit` and anywhere else E2E tests run.

## Past lesson: `govulncheck` vs `$HOME/go` install

Never extract a Go tarball to `$HOME/go`. That path is the default `GOPATH` when `GOPATH` is unset, so the toolchain ends up with two copies of stdlib and `govulncheck` errors with "redeclared in this block" against `$HOME/go/src/net/*.go`. The convention is a snap- or apt-managed `go` binary on `$PATH` plus `GOPATH=$HOME/go-workspace` exported in `~/.bashrc`. See the prerequisites block in [`.claude/skills/precommit/SKILL.md`](../skills/precommit/SKILL.md).
