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
- `make shell-check` — Bash syntax, ShellCheck, shfmt drift, and execution-class policy
- `make shell-fmt` — format tracked Bash files with the pinned shfmt
- `make shell-test` — run deterministic Shell behavioral tests
- `make shell-quality` — run `shell-check` and `shell-test`
- `scripts/shell-quality.sh changed <base>` — fast validation for changed and untracked Bash files
- `cd server && oapi-codegen -config oapi-codegen.yaml ../api/openapi.yaml > internal/api/openapi_gen.go` — regenerate Go API from OpenAPI spec
- `cd web && npm run generate:api` — regenerate TypeScript types from OpenAPI spec

## Local toolchains track the ones CI resolves

Every language-toolchain pin in the workflows floats — Rust `stable` (and
`nightly` for [`fuzz.yml`](../../.github/workflows/fuzz.yml)), Node major `24`
— so CI installs the newest release on every run while a workstation keeps
whatever it downloaded when it was set up. A workstation left behind runs a
gauntlet blind to the lints and behaviour CI will see: a green gate, a red
pipeline, and nothing in the diff to explain either.

The gauntlet's prerequisite phase refuses to run on a drifted machine
([`toolchain-parity.sh`](../../scripts/lib/toolchain-parity.sh)) and prints the
command that fixes it — `rustup update stable`, `rustup update nightly`, or
`nvm install 24 --reinstall-packages-from=current && nvm alias default 24`.
Run the command and re-run the gauntlet; never work around the gate.

Go is the exception that proves it: `server/go.mod`'s `toolchain` directive is
the single source of truth, every exact `go-version` in the workflows is held
equal to it, and `GOTOOLCHAIN=auto` makes a local `go` in `server/` re-exec
into that version.

## Use `make e2e`, not bare `npx playwright test`

`make e2e` owns the full Docker Compose lifecycle (`up --build --wait` → `playwright test` → `down -v`). The bare `npx playwright test` invocation relies on Playwright's `webServer` block with a 180s timeout that is too short for cold Docker builds; tests fail before the stack is ready. This applies inside `/precommit` and anywhere else E2E tests run.

## Past lesson: `govulncheck` vs `$HOME/go` install

Never extract a Go tarball to `$HOME/go`. That path is the default `GOPATH` when `GOPATH` is unset, so the toolchain ends up with two copies of stdlib and `govulncheck` errors with "redeclared in this block" against `$HOME/go/src/net/*.go`. The convention is a snap- or apt-managed `go` binary on `$PATH` plus `GOPATH=$HOME/go-workspace` exported in `~/.bashrc`. See the prerequisites block in [`.claude/skills/precommit/SKILL.md`](../skills/precommit/SKILL.md).
