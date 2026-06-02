# ADR-029: Test Determinism — No Silent Skips

**Status:** Accepted
**Date:** 2026-06-01
**Supersedes:** none

## Context

Postgres-backed integration tests skipped themselves when `POSTGRES_TEST_URL`
was unset — a single `t.Skip` chokepoint in `server/internal/testutil`
(`NewTestStore`) plus four standalone sites (`db`, `amt/transport`, two in
`api`). A skip on a missing dependency is a **false green**: a developer running
`go test ./...` without the env var sees a passing run in which the tests never
executed. Two further skip shapes existed: golden-file *generators* gated on
`GENERATE_GOLDEN` (`t.Skip` when not generating), and one platform skip that
required `/etc/hostname` to exist.

This is the same anti-pattern as "Never claim SKIP passes in /precommit"
([`.claude/rules/editing-and-scope.md`](../../.claude/rules/editing-and-scope.md)),
generalised to the test suite: the same `go test` invocation must run the same
tests with the same result on any machine.

## Decision

1. **Tests always run, deterministically — no silent skips.** The result of a
   test invocation does not depend on optional environment configuration.

2. **Postgres is auto-provisioned.** A new leaf package
   [`server/internal/testpg`](../../server/internal/testpg/testpg.go) returns a
   base database URL: `POSTGRES_TEST_URL` when set (CI, or `make
   postgres-test-up`); otherwise it starts a throwaway `postgres:17-alpine`
   container via **testcontainers-go** (reaped by Ryuk at process exit).
   `testpg.URL()` serves `TestMain`; `testpg.BaseURL(t)` serves tests and fails
   loudly rather than skipping. `testpg` imports no `internal/*` package, so
   even `package foo` internal tests can use it without an import cycle.

3. **A skip-guard hook enforces it at edit time.**
   [`pretooluse-test-skip-guard.sh`](../../.claude/hooks/pretooluse-test-skip-guard.sh)
   (PreToolUse Write|Edit|MultiEdit, registered in `.claude/settings.json`)
   blocks introducing a skip/focus marker in a test file across all three
   languages: Go `t.Skip`/`t.Skipf`/`t.SkipNow` (`*_test.go`); web
   `it`/`test`/`describe`.`skip`/`skipIf`/`only`/`todo`/`fixme` and
   `xit`/`xdescribe`/`xtest`/`fit`/`fdescribe(` (`*.{test,spec}.{ts,tsx,js,jsx}`);
   Rust `#[ignore]` (`*.rs`). No bypass — editing `.claude/settings.json` is the
   only way to change enforcement. Rule:
   [`.claude/rules/tests-determinism.md`](../../.claude/rules/tests-determinism.md).

4. **Generator-style tests always do real work.** The golden generators
   (`TestGenerateReverseGoldens`, `TestGenerateForwardSidecars`) write into a
   `t.TempDir()` and assert by default, touching the committed `testdata/golden`
   tree only under `GENERATE_GOLDEN=1`; `TestGoldenSidecarsExist` always
   verifies. Platform preconditions are created in `t.TempDir()` rather than
   read from the host.

## Consequences

- **A new dependency** (`testcontainers-go` + its Postgres module) is added to
  the server module. Running Postgres-backed tests without an external DB now
  requires Docker — the same requirement the `make e2e` and `make sonar` paths
  already carry — and pays a one-time container cold-start (a few seconds) per
  test binary. CI is unaffected: it sets `POSTGRES_TEST_URL`, so no container is
  started.
- **No skips remain in the Go suite**, and new ones are blocked at edit time
  across Go/web/Rust. The web and Rust suites had zero skips at adoption; the
  hook keeps them that way.
- The skip-guard is an **edit-time** PreToolUse hook, complementing the
  **commit-time** gates (the gauntlet via the commit-guard). A focus marker
  (`.only`/`fit`) — which silences every *other* test — is caught the moment it
  is written.
- `max_connections=400` on the auto-provisioned container matches the
  `make postgres-test-up` target so the existing `maxLiveStores` concurrency
  budget holds.
