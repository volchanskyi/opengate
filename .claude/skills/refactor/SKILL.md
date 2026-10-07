---
name: refactor
description: |
  Tidy-up of the change about to be committed. Improves readability and performance
  without changing business logic. Runs after the gauntlet passes and before the
  commit; the commit is refused without it.
---

# Tidy-up Before Commit

Refactor the code this change adds — the uncommitted diff against `HEAD` — after
every check has passed on it and before it is committed. DO NOT CHANGE BUSINESS
LOGIC.

The order is enforced end to end ([`refactor.md`](../../rules/refactor.md)): the
gauntlet passes, then this skill, then the commit, which runs the gauntlet again.

## Constraints

- Do not introduce external libraries not already in the project
- Do not change API signatures
- Do not change business logic

## Steps (follow in order)

0. **Start** — from the repo root run `scripts/refactor-gate.sh start`. It refuses
   unless `./scripts/precommit-gauntlet.sh` passed on exactly the content on disk;
   when it refuses, run the gauntlet to a pass and start again.
1. **Analyze** — Review the current code and explain potential bottlenecks within the repo
2. **Slice before you cut** — Before any change that touches more than one file, build the **call slice** (what calls this; what does this call) and the **test slice** (which tests cover the rename or hoist or extraction). The slice is the verification gate: a refactor that compiles but breaks an uncovered caller path is the most expensive failure mode this skill can produce.
   - Rust: `cargo check --workspace --all-targets` after every micro-step; `rust-analyzer` gives the call slice via "find references". For non-trivial extractions, also run `cargo expand` on the affected module if macros are involved.
   - Go: `gopls references` (or `grep` the symbol across the module); `go vet ./...` after every step. Cross-package renames must run `go build ./...` before continuing.
   - TypeScript: TypeScript LSP "find all references"; `npx tsc -b` in `web/` after every step (`tsc --noEmit` there checks nothing — the root config is a solution file). Trust the type system more than your memory.
   - In all three: run the test slice (the union of test files that import the changed symbols) before moving to the next refactor step. The full test suite is the final gate, not the per-step gate.
3. **Strategize** — Describe the optimization strategy options you suggest
4. **Divide and conquer** — Break the work into smaller, manageable subtasks. Address one logical unit at a time, review and test the changes, then move to the next step
5. **Dead-code & unreachable-branch sweep** — Run `make dead-code` (clippy `-W dead_code`, staticcheck `U1000`, ts-prune) on the touched packages. For every finding, either remove it or justify it inline:
   - Rust: remove the symbol, OR add `#[allow(dead_code)]` with a one-line comment stating the binding ([`code-comments.md`](../../rules/code-comments.md)).
   - Go: remove, OR add a build-tag guard, OR add a test that exercises it. `staticcheck` U1000 has no inline-suppress; if removal is wrong, restructure so the call site exists.
   - TypeScript: remove, OR mark the export `@internal` if it is intentionally unused at the boundary but kept for future restoration. Bare comments are not justification — the comment must name what would call this and when.
   - Unreachable branches: `if false`, dead `match`/`switch` arms, code after `return`/`panic`. Remove all. Suppression here is never the right answer — the branch is either reachable (write a test) or not (delete it).
6. **Test** — Thoroughly test the changes. Review tests with tester persona. Make use of negative testing. Add new tests and/or update existing ones as needed to maintain or increase test coverage. Re-evaluate existing tests for duplication. Remove unused tests. Make use of boundary value analysis and equivalence partitioning
7. **Finish** — from the repo root run `scripts/refactor-gate.sh finish`. It records
   the content the tidy-up left, which is the only content the next commit may
   carry: any edit after it, a new file included, starts the order over. The
   commit then runs the full gauntlet on it. When nothing tracked is left
   uncommitted, the content is `HEAD`'s and `HEAD` is marked for the push guard.

## Focus Areas

- Readability and performance
- Comments held to [`code-comments.md`](../../rules/code-comments.md): delete
  what restates the code, and keep each why as one short positive statement
- Eliminate duplications, unused imports, and unused libraries
- Apply industry best practices
- Audit and fix lint/warning suppressions (`#[allow(...)]`, `//nolint:`, `eslint-disable`) — prefer
  restructuring code to eliminate the root cause; keep suppressions only for genuine language
  limitations or protocol requirements, always with clear justification comments

## Infrastructure Configs

In addition to application code, refactor infrastructure configs under `deploy/` and `.github/workflows/`:

- **Helm charts** (`deploy/helm/`) — values and templates the overlays duplicate, stale keys
- **Docker Compose** (`deploy/docker-compose*.yml`) — duplicated service definitions, unused env vars, stale volumes/networks
- **Terraform** (`deploy/terraform/`) — unused variables, stale outputs, undocumented constraints
- **CI/CD workflows** (`.github/workflows/`) — duplicated steps, unused inputs, hardcoded versions
- **Deploy scripts** (`deploy/scripts/`) — duplicated logic that should be in `common.sh`
- **Monitoring** (`deploy/helm/monitoring/`, `deploy/grafana/`) — stale scrape targets, orphaned alert rules, dashboard panels referencing removed metrics

Validate changes with `make lint-deploy && actionlint`.

## The markers

Steps 0 and 7 write `.claude/.markers/refactor.start` and `refactor.done`; the
gauntlet writes `gauntlet.pass` and the post-commit hook `refactor.head`. Never
write one by hand — the write guards refuse it, and a hand-written proof proves
nothing. **There is NO bypass** — the only way to change enforcement is editing
`.claude/settings.json`.
