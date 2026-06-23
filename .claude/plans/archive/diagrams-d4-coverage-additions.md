# Micro-Plan D4: Coverage Standard + High-Value Diagrams

**Parent master:** `diagrams-as-code-part-2.md` (§5 D4). **Branch:** `dev`.
**Owner:** docs. **Depends on:** D1 (pin the new diagrams), D3 (validate), D2 (C4 style).

## 1. Goal

Replace ad-hoc diagram coverage with a minimal **"what must be diagrammed" standard**,
then add the highest-value missing diagrams and pin them.

## 2. Coverage standard (to document in `docs/README.md`)

A doc set **must** carry at least:
1. **System context** — one C4Context (L1) — *(in Architecture.md via D2)*.
2. **Container topology** — one C4Container — *(Architecture.md via D2)*.
3. **Each cross-component protocol flow** — a `sequenceDiagram` *(handshake, relay —
   already present)*.
4. **Deploy topology** — the OKE cluster shape.
5. **CI/CD flow** — pipeline → merge-to-main → deploy.
6. **Session lifecycle** — establish → relay → teardown.

## 3. Scope

**In:** document the standard; add diagrams for the gaps (#4 deploy topology, #5 CI/CD
flow, #6 session lifecycle); pin each in D1's test.
**Out:** diagramming every doc; web-perf diagrams; data-model ERDs (defer unless a need
surfaces).

## 4. File inventory

| File | Change |
|---|---|
| [`docs/README.md`](../../../docs/README.md) | Add the §2 coverage standard. |
| [`docs/Kubernetes.md`](../../../docs/Kubernetes.md) | **New** `flowchart` deploy/OKE topology (node, namespaces, server/postgres/monitoring, ingress). |
| [`docs/CI-Pipeline.md`](../../../docs/CI-Pipeline.md) / [`docs/Continuous-Deployment.md`](../../../docs/Continuous-Deployment.md) | **New** `flowchart` of dev → gauntlet/CI → merge-to-main → CD deploy. |
| [`docs/Architecture.md`](../../../docs/Architecture.md) | **New** `sequenceDiagram` session lifecycle (browser ↔ server ↔ agent ↔ relay: establish → stream → teardown). |
| [`scripts/tests/docs-diagrams.test.sh`](../../../scripts/tests/docs-diagrams.test.sh) | Pin a minimum for each newly-diagrammed doc + bump the total floor (extends D1). |

## 5. Approach

1. Land D1–D3 first (guard, C4 style, validator).
2. Document the coverage standard in README.
3. Add the three diagrams (deploy topology, CI/CD flow, session lifecycle); prefer plain
   `flowchart`/`sequenceDiagram` for deploy/CI/CD (Mermaid C4 has no robust deployment
   diagram); apply D2's render-verify to any C4 block.
4. Run the D3 validator; **GitHub render-verify** each new block.
5. Extend D1's test to pin the new diagrams; confirm removal reds the run.
6. `/precommit` green.

## 6. Acceptance criteria / DoD

- [ ] README documents the coverage standard.
- [ ] Deploy topology, CI/CD flow, and session-lifecycle diagrams exist, **render on
      GitHub** (evidence), and pass the D3 validator.
- [ ] Each new diagram is pinned in `docs-diagrams.test.sh`; removal reds the run.
- [ ] `/precommit` + `make shell-quality` green.

## 7. NFRs

- **Maintainability:** the standard makes coverage a rule, not a judgment call; new
  diagrams reuse proven notation.
- **Performance/Security:** none (Markdown only).

## 8. Reviewer/QA checklist

- [ ] Each gap (#4–#6) has a diagram in the right doc; render evidence attached.
- [ ] New diagrams pinned in the test; total floor bumped.
- [ ] Notation choice justified (plain vs C4) per render robustness.
- [ ] Standard in README matches what is actually pinned.
