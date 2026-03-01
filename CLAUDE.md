# OpenGate — Development Conventions

## TDD Mandate
Write failing tests FIRST. Then implement. Then refactor. No exceptions.

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
Before every commit, run tests AND benchmarks locally to catch regressions:
1. `make test` — all tests must pass
2. `cd server && go test -bench=. -benchmem -run='^$' ./internal/...` — Go benchmarks
3. `cd agent && cargo bench -p mesh-protocol` — Rust benchmarks

## Commands
- `make build` — build all components
- `make test` — run all tests
- `make lint` — clippy + go vet + eslint
- `make golden` — cross-language compatibility check
