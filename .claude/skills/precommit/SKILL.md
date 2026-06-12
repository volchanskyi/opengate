---
name: precommit
description: |
  Run every mandatory pre-commit check via scripts/precommit-gauntlet.sh.
  Use before EVERY commit, including docs-only and CI-only commits.
---

# Pre-Commit Checklist

Run the gauntlet:

```bash
./scripts/precommit-gauntlet.sh
```

Exit 0 = all checks passed; commit allowed. Exit 1 = one or more checks failed (the failing checks' output is printed inline). Exit 2 = a prerequisite is missing (`POSTGRES_TEST_URL`, `SONAR_TOKEN`, `$HOME/go` shadow install). Fix the prerequisite and re-run; **do not bypass**.

The script is the single source of truth — running it here matches what `.claude/hooks/pretooluse-git-commit-guard.sh` runs at commit time, so a passing local run guarantees the hook will not block. The two callers share the same code path; there is no marker shortcut and no way to bypass with a manual file write.

## What runs

In order, with elapsed time printed per step:

1. **Prerequisites** — `$HOME/go` not a shadow install, `POSTGRES_TEST_URL` set, `SONAR_TOKEN` set.
2. **Lints** — `cargo fmt --check`, `cargo clippy -D warnings`, `go vet`, `eslint`, `actionlint`, `make taint-go`, `make taint-web`, `make dead-code`, `gitleaks protect --staged`, `make lint-deploy`.
3. **Codegen sync** — `make verify-codegen` (`oapi-codegen` re-run + clean-diff assertion).
4. **Tests** — Go unit + integration with `-race`, Rust workspace, Vitest with coverage.
5. **Coverage thresholds** — Go ≥ 80% (excluding `testutil/`, `metrics/`, `amt/transport/wsman/`, `openapi_gen.go`), Web ≥ 80% lines, Rust ≥ 80% lines (excluding `main.rs`, `webrtc.rs`, `terminal.rs`, `session/mod.rs`, `session/relay.rs`, `tests/`).
6. **Security audits** — `govulncheck`, `npm audit --audit-level=high`, `cargo audit`.
7. **Benchmarks** — Go `go test -bench` and Rust `cargo bench -p mesh-protocol`. Skip with `PRECOMMIT_SKIP_BENCH=1` only for clearly non-perf-touching iterations.
8. **E2E** — `make e2e` (full Playwright suite against the docker-compose test stack).
9. **SonarCloud** — `make sonar` (full scan with fresh coverage upload). **No skip.** Quality-gate evaluation against stale coverage previously let `new_coverage` regressions surface only in CI; the gauntlet now uploads fresh coverage on every commit.

## Why every commit

Lockfile audits (`cargo audit`, `govulncheck`, `npm audit`) gate on the **current advisory database**, not the diff — a vuln published today fails a docs-only commit tomorrow. SonarCloud, lints, and e2e gate on full-repo state. Selective skipping rots the gate; the script never skips.

## TDD interaction

`.claude/hooks/pretooluse-tdd-gate.sh` fires before this script — it blocks the first source-file Write/Edit/MultiEdit on a branch with no test changes. By the time the gauntlet runs, TDD presence is already established. The commit-guard hook's TDD backup check (step 7 in that hook) is a defense in depth — it should never fire after a normal flow.

## Documentation reminder

The gauntlet does not assert documentation freshness. After it passes, before committing, update [`README.md`](../../../README.md) sections and [`/docs`](../../../docs/) pages that the diff invalidates (link-over-paraphrase + mutable-per-file-ADRs conventions per [`docs/README.md`](../../../docs/README.md)). Per-file ADRs (013+) in [`docs/adr/`](../../../docs/adr/) are mutable — edit to keep them current; add a new superseding ADR only for a genuine decision change.
