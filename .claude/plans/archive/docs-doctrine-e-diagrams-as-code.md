# DD-E — Diagram / Docs-as-Code (long-term)

**Parent:** `current-state-docs-doctrine-and-adr-mutability.md` (Workstream E).
**Execution order:** **Last / optional** — independent of DD-A..D; lowest priority.
**Status:** Done; archived after implementation.
**Risk:** Low — but a wrong engine choice (D2/SVG) adds a heavy runtime + a drift
surface, which is why this is constrained below.

## Objective

Add a small set of hand-curated, GitHub-rendered diagrams and keep structural
drift caught by the boundary linters already enforced — within the
<1.5s / zero-network / no-new-heavy-runtime envelope.

## Decisions (from the master, do not re-litigate)

- **Engine = Mermaid fenced blocks** — GitHub renders Mermaid server-side. **Not**
  a D2/SVG pipeline (zero CLI, zero Puppeteer/JRE, zero committed SVG blobs / drift
  surface). D2→SVG only earns its keep for off-GitHub rendering, which we don't do.
- **No auto-extraction** of diagrams from code (AST sprawl + drift). Hand-curate a
  few high-level diagrams; let linters catch structural drift.
- **Drift caught by existing boundary tools** (see [`tooling.md`](../../rules/tooling.md)):
  `go-arch-lint`, `dependency-cruiser` (boundary-scoped), `cargo-modules`
  (metadata-only — **not** `cargo-expand`, which blows the 1.5s budget).
- **Link checking = DD-B's Go-native internal checker** (not Node
  `markdown-link-check`).

## Scope (curate, don't generate)

| Target | Action |
|---|---|
| `docs/Architecture.md` (or `docs/Home.md`) | Add 1–3 high-level Mermaid diagrams (system topology, request/relay flow). Replace any prose-only architecture description that a diagram clarifies. |
| Existing Mermaid (e.g. [`docs/Multiscale-Readiness.md`](../../../docs/Multiscale-Readiness.md) §7) | Confirm it renders; adopt as the house style. |
| Boundary linters | Confirm `go-arch-lint` / `dependency-cruiser` / `cargo-modules` are wired (or wire them) so structural drift fails CI rather than rotting a diagram silently. |

## Steps

1. Inventory existing Mermaid blocks; pick 1–3 high-value diagrams to add/curate.
2. Author as fenced ```mermaid blocks; verify on a GitHub preview (renders server-side).
3. Confirm/wire the boundary linters; ensure each stays <1.5s and zero-network.
4. `/precommit` → commit → `/refactor` → push.

## Reviewer checklist

- [ ] Diagrams are Mermaid fenced blocks (no committed SVG, no D2 pipeline, no new heavy runtime).
- [ ] Each renders on GitHub preview.
- [ ] Boundary linters catch structural drift; total added check time <1.5s, zero-network.
- [ ] Full `/precommit` gauntlet green.

## Done when

A few curated Mermaid diagrams render on GitHub and structural drift is linter-caught,
all within the hooks performance/network envelope.
