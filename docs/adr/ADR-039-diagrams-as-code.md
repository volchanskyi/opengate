---
number: 39
title: Diagrams are Mermaid, checked in CI
---

# ADR-039 — Diagrams are Mermaid, checked in CI

## Decision

**Mermaid fences only.** No committed SVG, no other diagram source. A diagram is
text in the document it belongs to, reviewable in a diff.

**C4 where the diagram is about structure**, with a plain flowchart fallback
arranged the same way where the renderer does not support it.

**Syntax is validated in CI**, because a diagram that does not render is
invisible on the page and nothing else notices.

**Diagram-bearing documents are pinned** by
[`docs-diagrams.test.sh`](../../scripts/tests/docs-diagrams.test.sh), with a
floor on the total, so a diagram silently dropped from a document nobody pinned
still fails.

## Consequences

A diagram is as reviewable as the prose around it, and cannot drift from the
document it illustrates without the drift being in the diff.
