# Micro-Plan D2: C4 Adoption (native Mermaid C4 + render gate)

**Parent master:** `diagrams-as-code-part-2.md` (§5 D2, §3.1 risk control). **Branch:**
`dev`. **Owner:** docs. **Depends on:** D1 (guard), D3 (validator for C4 blocks).

## 1. Goal

Adopt the C4 model using **native Mermaid C4 blocks**. Because native C4 is
**experimental and GitHub-render-fragile** ([sources](https://github.com/mermaid-js/mermaid/issues/3217)),
every C4 block ships **only after** it is verified to render on GitHub, with a documented
fallback.

## 2. Scope

**In:** a **C4Context (L1)** view; the topology re-expressed as **C4Container**; optional
**C4Component** for server internals; update the README convention. Per-block GitHub
render verification.
**Out:** Structurizr or any non-Mermaid C4 toolchain (violates Mermaid-only); converting
the sequence diagrams (they stay `sequenceDiagram`).

## 3. File inventory

| File | Change |
|---|---|
| [`docs/Architecture.md`](../../docs/Architecture.md) | Add a `C4Context` block (person = operator/admin; system = OpenGate; external = agents/browsers). Re-express the existing `flowchart` topology as `C4Container` (web, server, agent, relay, Postgres, monitoring). Keep the 2 `sequenceDiagram` flows (or promote one to `C4Dynamic` only if it render-verifies). |
| [`docs/README.md`](../../docs/README.md) | Extend the "Mermaid diagrams only" convention to standardize the allowed C4 block types and **document the fallback rule** (§5). |
| (optional) `docs/Architecture.md` | `C4Component` for server internals if it render-verifies and adds value. |

## 4. The render-verification gate (mandatory — §3.1 of the master)

For **each** C4 block:
1. Push to a PR/branch and open the rendered Markdown on **GitHub** (the authoritative
   renderer — not the Mermaid Live editor, not the D3 validator).
2. Confirm it renders as a diagram, **not** an error box. Attach a screenshot/link.
3. If it fails to render: **fall back** to the proven `flowchart`/`sequenceDiagram`
   notation arranged along the C4 *level* (same containers/relationships, robust
   rendering), and record why in the PR.

## 5. Approach

1. Land D1 (guard) and D3 (validator) first so C4 blocks are counted + syntax-checked.
2. Author the `C4Context` + `C4Container` blocks.
3. Run the D3 validator locally/CI; then **render-verify on GitHub** (§4).
4. Update the README convention + fallback rule.
5. Ensure D1's test still counts these as Mermaid blocks (C4 blocks use the
   ` ```mermaid ` fence — the count guard still applies).
6. `/precommit` → commit → `/refactor` → `/precommit` → commit → push.

## 6. Acceptance criteria / DoD

- [ ] A C4Context and a C4Container view exist in `docs/Architecture.md`.
- [ ] **Each C4 block render-verified on GitHub** (evidence attached); any that wouldn't
      render uses the documented fallback (also evidenced).
- [ ] README documents the C4 convention **and** the fallback rule.
- [ ] D1's count guard still passes; D3 validator passes on the C4 blocks.
- [ ] `/precommit` green.

## 7. NFRs

- **Maintainability:** one engine (Mermaid); documented fallback means a renderer
  regression never blocks docs.
- **Performance/Security:** none (Markdown only; GitHub renders server-side).

## 8. Reviewer/QA checklist

- [ ] GitHub render evidence present for **every** C4 block (not just the Live editor).
- [ ] Fallbacks (if any) preserve the same containers/relationships.
- [ ] C4 blocks still use the ` ```mermaid ` fence (so D1 counts them).
- [ ] No non-Mermaid C4 toolchain introduced.

## 9. Risks

- A future GitHub Mermaid bump could break a previously-rendering C4 block — the fallback
  rule + D1's count guard contain the blast radius; D3 catches syntax, not render drift.
