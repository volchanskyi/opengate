# /refactor and the commit-time gate

**Enforced by:** [`.claude/hooks/pretooluse-git-commit-guard.sh`](../hooks/pretooluse-git-commit-guard.sh) (runs the gauntlet directly), [`.claude/hooks/pretooluse-git-push-guard.sh`](../hooks/pretooluse-git-push-guard.sh) (refactor marker). **No bypass.**

## The gate runs at commit time

- [`scripts/precommit-gauntlet.sh`](../../scripts/precommit-gauntlet.sh) is the
  single source of truth for what a commit must pass.
- The commit guard executes it on every commit attempt — including docs-only and
  CI-only commits. There is no marker shortcut and no way to bypass it.
- A failed attempt costs a full run. Fix what the output names and re-attempt.
- To run the same checks without attempting a commit, run the script directly:

  ```bash
  ./scripts/precommit-gauntlet.sh
  ```

  Exit 0 = every check passed. Exit 1 = one or more checks failed. Exit 2 = a
  prerequisite is missing (`POSTGRES_TEST_URL`, `SONAR_TOKEN`, a reachable
  VictoriaMetrics, a `$HOME/go` shadow install). Fix the prerequisite and
  re-run; never bypass.

- The gauntlet does not assert documentation freshness. Update
  [`README.md`](../../README.md) and [`/docs`](../../docs/) pages the diff
  invalidates before committing.

### What the gauntlet runs

In order, with elapsed time printed per step:

1. **Prerequisites** — `$HOME/go` not a shadow install, `POSTGRES_TEST_URL` set
   and reachable, VictoriaMetrics reachable (auto-started via
   `make victoriametrics-test-up`, then exported as `VICTORIAMETRICS_TEST_URL`
   so every Go package shares one store), `SONAR_TOKEN` set.
2. **Lints** — `cargo fmt --check`, `cargo clippy -D warnings`, `go vet`,
   `eslint`, `actionlint`, `make taint-go`, `make taint-web`, `make dead-code`,
   `gitleaks protect --staged`, `make lint-deploy`.
3. **Codegen sync** — `make verify-codegen`.
4. **Tests** — Go unit + integration with `-race`, Rust workspace, Vitest with
   coverage.
5. **Coverage thresholds** — Go, Web and Rust each ≥ 80%, per the exclusion
   lists in [`coverage-exclusions.md`](coverage-exclusions.md).
6. **Security audits** — `govulncheck`, `npm audit --audit-level=high`,
   `cargo audit`.
7. **Benchmarks** — Go `go test -bench` and Rust `cargo bench -p mesh-protocol`.
   Skip with `PRECOMMIT_SKIP_BENCH=1` only for clearly non-perf-touching
   iterations.
8. **PMAT TDG gate** — `pmat tdg check-quality` on each changed code file at the
   **B+** floor. Ordered before the slow phase because it is fast and frequently
   the failing check.
9. **E2E** — `make e2e`.
10. **SonarCloud** — `make sonar`, full scan with fresh coverage upload. No skip.

Steps 9–10 are deferred when an earlier check has already failed. That changes
runtime, never the pass/fail outcome.

### Why every commit

Lockfile audits (`cargo audit`, `govulncheck`, `npm audit`) gate on the current
advisory database, not the diff — an advisory published today fails a docs-only
commit tomorrow. SonarCloud, lints and e2e gate on full-repo state.

## /refactor

- Run `/refactor` after a commit lands, before pushing.
- The marker `.claude/.markers/refactor.head` (= `git rev-parse HEAD` after
  `/refactor` finishes) is checked by the push guard.
- The push guard blocks a push whenever there are any commits since
  `origin/dev` unless the marker equals HEAD, regardless of what files they
  touch. There is no doc-only or CI-only exemption.
- The post-commit hook refreshes the marker to HEAD, so the normal
  commit-then-push flow satisfies this. A manual push of a non-code change still
  needs `/refactor`.

## Multi-PR rollouts

- Every PR passes the gate, including docs-only PRs.
- Exception: a fast-follow commit fixing a CI failure already reported by a
  previous push.
