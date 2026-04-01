# OpenGate — Development Conventions

## Project State — Read Before Starting Work

**MANDATORY** — Before beginning any session, read these three files to understand current project state:

- [`.claude/phases.md`](.claude/phases.md) — implementation phases (completed / in-progress / planned)
- [`.claude/techdebt.md`](.claude/techdebt.md) — known tech debt by severity
- [`.claude/decisions.md`](.claude/decisions.md) — ADR index (full text in [wiki](https://github.com/volchanskyi/opengate/wiki/Architecture-Decision-Records))

**[Wiki](https://github.com/volchanskyi/opengate/wiki)** — The canonical developer documentation for the project. Covers architecture, API reference, wire protocol, platform abstraction, database, testing, CI/CD pipeline, continuous deployment, container images, monitoring, infrastructure, agent updates, security, and ADRs. Consult the wiki when you need context on how a system works before making changes. The wiki repo is cloned locally at `/home/ivan/opengate.wiki/` (push to `master` branch).

**After completing any significant work**, update the relevant files:
- Mark phases complete / update in-progress status in `phases.md`
- Add or resolve debt items in `techdebt.md`
- Record new architectural decisions in `decisions.md` (index row) AND the [wiki ADR page](https://github.com/volchanskyi/opengate/wiki/Architecture-Decision-Records) (full text)

**All agent plans** must be created in `.claude/plans/` with a descriptive kebab-case name (e.g. `fix-auth-bug.md`, `phase-16-feature.md`). Never use auto-generated random names. Completed plans are archived to `.claude/plans/archive/`.

**Plans vs memory** — Plans and memory serve different purposes. Never confuse them:
- **Plans** (`.claude/plans/`) — implementation details, steps, and task breakdowns. Always a `.md` file in this directory.
- **Memory** (`~/.claude/projects/.../memory/`) — only for cross-session recall: user preferences, project context, references. Never store plans or task details here.

---

## Branching Rules
**MANDATORY** — All work happens on `dev`. No exceptions.
- **Before starting any work**, pull latest: `git checkout dev && git pull origin dev`
- **Before every push**, pull again: `git pull --rebase origin dev` then push
- Commit and push to `dev` only: `git push origin dev`
- **Never commit or push directly to `main`** — `main` receives code exclusively via the automated `merge-to-main` CI job after all checks pass on `dev`

## Git Identity
**MANDATORY** — Every commit must be authored by Ivan Volchanskyi. No co-authors, no Co-Authored-By trailers.
- `git config user.name "Ivan Volchanskyi"`
- `git config user.email "ivan.volchanskyi@gmail.com"`

## TDD Mandate
Write failing tests FIRST. Then implement. Then refactor. No exceptions.
Test Both Scenarios: positive cases (expected behavior) and negative cases (error handling)

## Rust Conventions
- `thiserror` for library crate errors, `anyhow` for binary crates only
- No `unwrap()` in production code — use `?` operator
- `#[non_exhaustive]` on all public enums
- `tokio` for async, `tracing` for logging
- All public items documented with `///` doc comments
- Workspace dependencies in root `Cargo.toml`

## Go Conventions
- `context.Context` as first argument on all functions
- `errors.Is` / `errors.As` for error checking — no string comparison
- Table-driven tests with `t.Run`
- `testify/assert` and `testify/require` for assertions
- All exported types have doc comments

## TypeScript Conventions
- Strict mode — no `any` in production code
- Vitest for all tests, React Testing Library for components
- Tailwind CSS for styling — no custom CSS files
- Zustand for state management

## Wire Protocol
- MessagePack encoding for control messages
- Frame format: [1-byte type][4-byte BE length][payload]
- Golden file tests verify Rust ↔ Go compatibility

## Pre-Commit Checklist
**MANDATORY** — Run `/precommit` before EVERY commit. No exceptions.

## Post-Commit Refactoring
**MANDATORY** — After all pre-commit checks pass, run `/refactor`. No exceptions.

## Commands
- `make build` — build all components
- `make test` — run all tests
- `make lint` — clippy + go vet + eslint + actionlint
- `make golden` — cross-language compatibility check
