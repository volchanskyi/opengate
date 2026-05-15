# TDD Mandate

**Enforced by:** [`.claude/hooks/pretooluse-tdd-gate.sh`](../hooks/pretooluse-tdd-gate.sh), [`.claude/hooks/pretooluse-bash-source-write-guard.sh`](../hooks/pretooluse-bash-source-write-guard.sh), [`.claude/hooks/pretooluse-git-commit-guard.sh`](../hooks/pretooluse-git-commit-guard.sh) (defense-in-depth at commit time). **No bypass.**

Write failing tests FIRST. Then implement. Then refactor.

Test both scenarios: positive cases (expected behavior) and negative cases (error handling).

## How the gate works

The first `Write`/`Edit`/`MultiEdit` of a source file on a branch is blocked until at least one test-file change exists on that branch (committed, staged, unstaged, or untracked). Once a test change exists anywhere on the branch, the gate is silent for the rest of the branch's life.

Source/test classifier: [`scripts/tdd-check.sh`](../../scripts/tdd-check.sh).

## Worked examples

### New feature

```
1. git checkout dev && git pull --rebase origin dev
2. Edit  server/internal/api/handlers_test.go     # add failing test
3. Edit  server/internal/api/handlers.go          # gate now silent — test exists on branch
4. (iterate)
5. /precommit                                     # writes marker
6. git commit
7. /refactor                                       # writes marker
8. /precommit                                     # re-validate
9. git commit
10. git push origin dev
```

### Bug fix

```
1. Edit  server/internal/api/handlers_test.go    # add failing regression test
2. Edit  server/internal/api/handlers.go         # fix; gate silent
3. /precommit → commit → /refactor → /precommit → commit → push
```

### Pure refactor

Tests must pass before AND after the refactor. To touch the source, *first* touch the covering test — strengthen an assertion, or add a `// covers …` annotation that exercises the behavior the refactor preserves. This costs almost nothing and keeps the discipline.

```
1. Edit  server/internal/relay/relay_test.go     # strengthen assertion / add covers comment
2. Edit  server/internal/relay/relay.go          # refactor; gate silent
3. (run tests; confirm green)
4. /precommit → commit → /refactor → /precommit → commit → push
```

### Generated code

Files matching `openapi_gen.go`, `*_gen.go`, and `*.pb.go` are excluded from the source classifier. Running the generator via `Bash` (e.g. `oapi-codegen`) and committing the output requires no prior test edit. Hand-editing generated files is discouraged for other reasons — they will be overwritten on the next regeneration.
