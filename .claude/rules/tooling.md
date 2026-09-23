# Tooling

## Commands

- `make build` — build all components
- `make test` — run all tests
- `make lint` — clippy + go vet + eslint + actionlint
- `make golden` — cross-language compatibility check
- `make e2e` — run Playwright end-to-end tests
- `make sonar` — full local SonarCloud scan
- `make sonar-quick` — code-quality-only scan (no coverage generation)
- `make sonar-coverage` — generate all coverage files for SonarCloud
- `make mutate` (and `mutate-rust` / `mutate-go` / `mutate-web`) — mutation tests
- `make taint-go` / `make taint-web` — static taint linting
- `make dead-code` — dead-code sweep (clippy `-W dead_code`, staticcheck `U1000`, ts-prune)
- `make shell-check` — Bash syntax, ShellCheck, shfmt drift, execution-class policy
- `make shell-fmt` — format tracked Bash files with the pinned shfmt
- `make shell-test` — run deterministic Shell behavioral tests
- `make shell-quality` — run `shell-check` and `shell-test`
- `scripts/shell-quality.sh changed <base>` — fast validation for changed and untracked Bash files
- `cd server && oapi-codegen -config oapi-codegen.yaml ../api/openapi.yaml > internal/api/openapi_gen.go` — regenerate Go API
- `cd web && npm run generate:api` — regenerate TypeScript types

## Local toolchains track the ones CI resolves

- Every language-toolchain pin in the workflows floats — Rust `stable` (and
  `nightly` for [`fuzz.yml`](../../.github/workflows/fuzz.yml)), Node major
  `24` — so CI installs the newest release on every run.
- The gauntlet's prerequisite phase refuses to run on a drifted machine
  ([`toolchain-parity.sh`](../../scripts/lib/toolchain-parity.sh)) and prints
  the command that fixes it. Run the command and re-run the gauntlet; never work
  around the gate.
- Go is the exception: `server/go.mod`'s `toolchain` directive is the single
  source of truth, every exact `go-version` in the workflows is held equal to
  it, and `GOTOOLCHAIN=auto` makes a local `go` in `server/` re-exec into that
  version.

## Use `make e2e`, not bare `npx playwright test`

`make e2e` owns the full Docker Compose lifecycle (`up --build --wait` →
`playwright test` → `down -v`). Bare `npx playwright test` relies on
Playwright's `webServer` block with a 180s timeout too short for cold Docker
builds. This applies inside the gauntlet and anywhere else E2E tests run.

## Never extract a Go tarball to `$HOME/go`

That path is the default `GOPATH` when `GOPATH` is unset, so the toolchain ends
up with two copies of stdlib and `govulncheck` errors with "redeclared in this
block". The convention is a snap- or apt-managed `go` binary on `$PATH` plus
`GOPATH=$HOME/go-workspace` exported in `~/.bashrc`. The gauntlet's
prerequisite phase refuses a shadow install
([`precommit-gauntlet.sh`](../../scripts/precommit-gauntlet.sh)).
