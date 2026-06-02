# Test Determinism — No Silent Skips

**Enforced by:** [`.claude/hooks/pretooluse-test-skip-guard.sh`](../hooks/pretooluse-test-skip-guard.sh). **No bypass.**

Every test must **always run, deterministically**, on any machine. A test that
skips itself when a dependency is missing, an environment flag is unset, or a
focus marker is present is a **false green** — it can pass locally while never
executing. This is the same anti-pattern as "Never claim SKIP passes in
/precommit" ([`editing-and-scope.md`](editing-and-scope.md)), applied to the
test suite.

## Banned markers (across all three languages)

The guard refuses any `Write`/`Edit`/`MultiEdit` that introduces these in a
test file:

| Language | Test files | Banned |
|---|---|---|
| Go | `*_test.go` | `t.Skip(`, `t.Skipf(`, `t.SkipNow(` |
| Web | `*.{test,spec}.{ts,tsx,js,jsx}` | `it`/`test`/`describe` `.skip`/`.skipIf`/`.only`/`.todo`/`.fixme`; `xit`/`xdescribe`/`xtest`/`fit`/`fdescribe(` |
| Rust | `*.rs` | `#[ignore]` |

## What to do instead

- **Missing service dependency (DB, etc.):** provision it deterministically.
  The reference is [`server/internal/testpg`](../../server/internal/testpg/testpg.go):
  when `POSTGRES_TEST_URL` is unset it auto-starts a throwaway `postgres:17`
  container via testcontainers, so Postgres-backed tests always run. `TestMain`
  uses `testpg.URL()`; tests use `testpg.BaseURL(t)` (fails loudly, never skips).
- **Platform/file preconditions:** create what the test needs in a `t.TempDir()`
  rather than depending on host files (e.g. don't read `/etc/hostname` — write
  your own fixture).
- **Generator-style "tests":** make them always do real work (write to a temp
  dir + assert) and only touch committed fixtures under the generate flag — see
  `TestGenerateReverseGoldens` in
  [`golden_reverse_test.go`](../../server/internal/protocol/golden_reverse_test.go).
- **Focus markers** (`.only`, `fit`, `fdescribe`) are never committed — they
  silence every *other* test.
