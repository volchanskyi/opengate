# Test Determinism — No Silent Skips

**Enforced by:** [`.claude/hooks/pretooluse-test-skip-guard.sh`](../hooks/pretooluse-test-skip-guard.sh). **No bypass.**

Every test runs, deterministically, on any machine. A test that skips itself on
a missing dependency, an unset environment flag, or a focus marker is a false
green.

## Banned markers

| Language | Test files | Banned |
|---|---|---|
| Go | `*_test.go` | `t.Skip(`, `t.Skipf(`, `t.SkipNow(` |
| Web | `*.{test,spec}.{ts,tsx,js,jsx}` | `it`/`test`/`describe` `.skip`/`.skipIf`/`.only`/`.todo`/`.fixme`; `xit`/`xdescribe`/`xtest`/`fit`/`fdescribe(` |
| Rust | `*.rs` | `#[ignore]` |

## What to do instead

- **Missing service dependency:** provision it deterministically. Reference:
  [`server/internal/testpg`](../../server/internal/testpg/testpg.go) auto-starts
  a throwaway `postgres:17` via testcontainers when `POSTGRES_TEST_URL` is
  unset. `TestMain` uses `testpg.URL()`; tests use `testpg.BaseURL(t)`, which
  fails loudly.
- **Platform or file preconditions:** create what the test needs in a
  `t.TempDir()`.
- **Generator-style tests:** always do real work (temp dir plus assertion), and
  touch committed fixtures only under the generate flag. Reference:
  `TestGenerateReverseGoldens` in
  [`golden_reverse_test.go`](../../server/internal/protocol/golden_reverse_test.go).
- **Focus markers** (`.only`, `fit`, `fdescribe`) are never committed.
