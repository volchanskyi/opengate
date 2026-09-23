# TDD Mandate

**Enforced by:** [`.claude/hooks/pretooluse-tdd-gate.sh`](../hooks/pretooluse-tdd-gate.sh), [`.claude/hooks/pretooluse-bash-source-write-guard.sh`](../hooks/pretooluse-bash-source-write-guard.sh), [`.claude/hooks/pretooluse-git-commit-guard.sh`](../hooks/pretooluse-git-commit-guard.sh). **No bypass.**

Write failing tests FIRST. Then implement. Then refactor.

Test both scenarios: positive cases and error handling.

## How the gate works

- The first `Write`/`Edit`/`MultiEdit` of a source file on a branch is blocked
  until at least one test-file change exists on that branch — committed, staged,
  unstaged, or untracked.
- Once a test change exists anywhere on the branch, the gate is silent for the
  rest of the branch's life.
- Classifier: [`scripts/tdd-check.sh`](../../scripts/tdd-check.sh).

## Worked examples

### New feature

```
1. checkout dev, pull --rebase origin dev
2. Edit  server/internal/api/handlers_test.go     # add failing test
3. Edit  server/internal/api/handlers.go          # gate now silent
4. (iterate)
5. commit                                          # guard runs the gauntlet
6. /refactor
7. commit                                          # guard runs it again
8. push origin dev
```

### Bug fix

```
1. Edit  server/internal/api/handlers_test.go    # add failing regression test
2. Edit  server/internal/api/handlers.go         # fix; gate silent
3. commit -> /refactor -> commit -> push
```

### Pure refactor

Tests pass before AND after. To touch the source, first touch the covering test
— strengthen an assertion, or add a `// covers …` annotation exercising the
behavior the refactor preserves.

```
1. Edit  server/internal/relay/relay_test.go     # strengthen assertion
2. Edit  server/internal/relay/relay.go          # refactor; gate silent
3. (run tests; confirm green)
4. commit -> /refactor -> commit -> push
```

### Rust unit tests

Rust keeps a module's unit tests inside the file they cover, so the classifier
reads the diff: a change whose every touched line falls at or after the file's
own `#[cfg(test)]` attribute is a test change. A file changed above that line as
well is a source change. Rust's alone — Go and TypeScript keep their tests in
files of their own.

### Generated code

Files matching `openapi_gen.go`, `*_gen.go`, and `*.pb.go` are excluded from the
source classifier. Running the generator via `Bash` and committing the output
requires no prior test edit. Hand-editing generated files is discouraged — they
are overwritten on the next regeneration.
