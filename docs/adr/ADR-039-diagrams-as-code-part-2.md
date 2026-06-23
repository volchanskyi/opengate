---
adr: 039
title: Diagrams as Code — Part 2 (C4, CI syntax validation, drift guard, coverage standard)
status: Accepted
date: 2026-06-23
---

# ADR-039: Diagrams as Code — Part 2

## Status

Accepted. This ADR **extends** the Part 1 docs-as-code decision (Mermaid-only,
hand-curated, no rendered-image pipeline) recorded in
[`docs/README.md`](../README.md) and the archived
[Docs Doctrine E plan](../../.claude/plans/archive/docs-doctrine-e-diagrams-as-code.md).
It supersedes nothing — Part 1 remains valid.

## Context

Part 1 established Mermaid-only docs-as-code and shipped a first set of diagrams,
but review surfaced four gaps:

1. **Weak drift enforcement.** The diagram guard pinned only 2 of the 6
   diagram-bearing docs, so four diagrams could vanish silently (the planning
   draft itself drifted on the count — direct evidence of the risk).
2. **No methodology.** Diagrams were ad-hoc `flowchart`/`sequenceDiagram` with no
   C4 levels, so "system context" vs "container" vs "component" was not expressed.
3. **No syntax validation.** A malformed Mermaid fence reached GitHub as an error
   box with nothing catching it first.
4. **Ad-hoc coverage.** There was no statement of what *must* be diagrammed, so
   gaps (deploy topology, CI/CD flow, session lifecycle) went undrawn.

The hard constraint inherited from Part 1: the local pre-commit hook stays
grep-only / zero-network with no heavy runtime; structural-boundary drift remains
the boundary linters' job, not auto-extracted graphs.

## Decision

### 1. Adopt native Mermaid C4, gated by a GitHub render check

Architecture-level structure uses native Mermaid C4 blocks — `C4Context` (L1) and
`C4Container` (L2) in [`docs/Architecture.md`](../Architecture.md), with
`C4Component`/`C4Dynamic` available but used sparingly. Native C4 keeps one engine
(Mermaid) and stays reviewable as plain Markdown.

Native Mermaid C4 is marked experimental and has historically been fragile on
GitHub's renderer. So the decision carries a **mandatory render-verification
gate**: every C4 block is confirmed to render as a diagram (not an error box) on
GitHub's own renderer before it ships. If a block will not render, it **falls
back** to a plain `flowchart`/`sequenceDiagram` arranged along the same C4 level —
identical containers and relationships, robust rendering. The current C4Context
and C4Container blocks were render-verified before landing; no fallback was
needed. The convention and fallback rule live in [`docs/README.md`](../README.md).

### 2. CI-only Mermaid syntax validation

A path-filtered CI workflow
([`docs-validate.yml`](../../.github/workflows/docs-validate.yml)) parses every
`mermaid` fence under `docs/` with the **official Mermaid parser** (so it accepts
experimental C4), failing CI on a syntax error. The validator is isolated in
[`tools/mermaid-validate/`](../../tools/mermaid-validate/) with a pinned lockfile —
kept out of the `web/` app dependencies and out of the local gauntlet, honoring
the grep-only/zero-network local constraint. Its Mermaid version is pinned for
alignment with GitHub's renderer, but the **render gate is authoritative**; the
validator is a fast pre-filter that catches syntax, not render drift.

### 3. Drift-guard hardening

[`scripts/tests/docs-diagrams.test.sh`](../../scripts/tests/docs-diagrams.test.sh)
now pins a minimum Mermaid count for **every** diagram-bearing doc plus a
total-fence floor, so removing any guarded diagram reds the gauntlet's shell-tests
step. A [`.github/CODEOWNERS`](../../.github/CODEOWNERS) record and a
[PR-template](../../.github/pull_request_template.md) checklist item nudge a
diagram-drift review when handshake/relay/topology/agentapi code changes (on a
solo repo CODEOWNERS adds no required reviewer — the PR checklist is the effective
nudge). The guard stays structural/advisory: it counts fences, it does not extract
graphs from source.

### 4. Diagram coverage standard

[`docs/README.md`](../README.md) documents the minimal set a doc must carry —
system context (C4Context), container topology (C4Container), each cross-component
protocol flow (`sequenceDiagram`), deploy topology, CI/CD flow, and session
lifecycle — each pinned by the drift guard. Part 2 added the three missing
diagrams: the OKE serving topology in [`docs/Kubernetes.md`](../Kubernetes.md), the
CI/CD flow in [`docs/Continuous-Deployment.md`](../Continuous-Deployment.md), and
the end-to-end session lifecycle in [`docs/Architecture.md`](../Architecture.md).

## Consequences

- A removed or malformed diagram now fails fast: the count guard reds the local
  gauntlet, and the CI validator reds the PR before GitHub renders an error box.
- C4 gives the architecture docs a shared vocabulary (context vs container) while
  the documented fallback means a future GitHub renderer regression can never
  block docs — the diagram degrades to robust notation instead.
- Mermaid parsing cost stays entirely in CI; the local hook remains grep-fast and
  network-free.
- The validator is a maintained dependency in the trusted CI path — pinned by
  lockfile, scoped to `docs/**`, and isolated from app code.

## Out of scope

Any local-hook heavy runtime; committed image blobs; a D2/SVG render pipeline;
source-AST graph extraction (structural drift stays the boundary linters' job —
`go-arch-lint`, `cargo modules`, `dependency-cruiser`).
