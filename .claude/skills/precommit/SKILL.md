---
name: precommit
description: |
  Run all mandatory pre-commit checks: lints, tests, benchmarks, coverage, and documentation.
  Use before every commit. Blocks commit if any check fails.
---

# Pre-Commit Checklist

Run ALL lints, ALL tests, test coverage, and ALL benchmarks. No exceptions. All tests MUST pass regardless of having pre-existing issues or being flaky.

## Lints (all must pass)

1. `cd agent && cargo fmt --all -- --check && cargo clippy --workspace -- -D warnings` — Rust format + clippy
2. `cd server && go vet ./...` — Go vet
3. `cd web && npx eslint .` — Web ESLint
4. `~/go/bin/actionlint` — GitHub Actions workflow lint (ALWAYS run locally, no exceptions)
5. `make lint-deploy` — Deploy config validation (yamllint, terraform, tflint, compose, caddy, trivy, integration tests)

## Tests (all must pass)

6. `cd server && go test -race -timeout 5m ./...` — Go tests (unit + integration, race detector)
7. `cd agent && cargo test --workspace` — Rust tests (all crates)
8. `cd web && npx vitest run` — Web tests

## Security audit (must pass)

9. `cd web && npm audit --audit-level=high` — npm dependency vulnerability scan

## Benchmarks (all must run without errors)

10. `cd server && go test -bench=. -benchmem -run='^$' ./internal/...` — Go benchmarks
11. `cd agent && cargo bench -p mesh-protocol` — Rust benchmarks

## Documentation (mandatory on every commit)

12. **`README.md`** (root) — If the commit changes anything covered by existing README sections (commands, setup, architecture, etc.), update those sections to stay accurate. Do NOT add new sections.
13. **GitHub Wiki** — Update the relevant wiki pages to reflect all changes. The wiki is the primary reference for senior engineers — it must be comprehensive, accurate, and always in sync with the codebase. Add new pages or sections as needed when introducing new features, APIs, or architectural changes.

## Gate Criteria

Do NOT commit if:
- Any lint fails
- Any test fails
- New code coverage is below 80% or overall coverage below 70%
- Any benchmark errors out
- Any security audit fails (high+ severity vulnerabilities)
- Documentation is stale
