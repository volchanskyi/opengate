# OpenGate — Development Conventions

## Branching Rules
**MANDATORY** — All work happens on `dev`. No exceptions.
- Always start from the `dev` branch: `git checkout dev && git pull origin dev`
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
