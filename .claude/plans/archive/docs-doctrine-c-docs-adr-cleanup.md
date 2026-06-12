# DD-C — Documentation + ADR-body cleanup

**Parent:** `current-state-docs-doctrine-and-adr-mutability.md` (Workstream C).
**Execution order:** **4th** (after DD-INV). **Coordinate with teardown
`dormant-scale-out-td6-docs-adrs.md`** — see Coordination.
**Status:** Ready (after DD-A makes ADRs editable + DD-INV defines scope).
**Risk:** **Highest-content** — purging mechanical noise from ADR *bodies* (013+)
can gut rationale. Guardrail: **rewrite to preserve the fact + the why; never
delete substantive rationale; KEEP `date:`/`status:`/`supersedes:` frontmatter.**

## Objective

Apply the doctrine to **every DD-INV hit** in `docs/**` (incl. `docs/adr/**` for
013+), `.claude/decisions.md`, and `.claude/phases.md`. Scope = the full
inventory; the batches below are ordering, not the limit.

## Coordination (avoid double-editing ADRs with teardown TD6)

Teardown TD6 amends ADR-023/030/031/033/034 + `decisions.md` + `phases.md` for
**content** (mark reverted/removed). DD-C cleans **noise** from those same files.
**Run TD6 first, then DD-C** over the settled content — otherwise DD-C edits get
re-touched/conflicted by TD6. If TD6 is still blocked or deferred, DD-C may
proceed on the **non-teardown** docs (Testing.md, CI-Pipeline.md, Infrastructure.md,
ADR-013, ADR-023 links) and leave the five teardown ADRs for a final batch after TD6.

## Ordered batches (each = one commit, gauntlet green)

1. **ADR-023 broken plan links** ([`:17`](../../docs/adr/ADR-023-relay-extraction-redis-session-registry.md#L17), `:136`):
   repoint `](modular-monolith-evaluation.md)` →
   `plans/archive/modular-monolith-evaluation.md` (confirm it's archived; if not,
   fold the pointer into [`decisions.md`](../decisions.md)). Drop the
   chronological "Resolved 2026-05-19" token, keep the substance.
2. **Testing.md** ([`:128`](../../docs/Testing.md#L128) `(PR 9)`, `:131` `03:00 UTC`,
   bare links `:216/:220/:273/:349`): drop `(PR 9)`; replace the time with a
   [`mutation.yml`](../../.github/workflows/mutation.yml) link; linkify
   `auth.spec.ts`, `security-permissions.spec.ts`, `ErrorBoundary.test.tsx`,
   `api-baseline.js` (resolve real paths via `find`).
3. **Schedule + negative-state purge:**
   - Infrastructure.md [`:168`](../../docs/Infrastructure.md#L168) "nightly at 03:00 UTC" → link `terraform-drift.yml`, drop the time.
   - CI-Pipeline.md **`:178`** (SARIF negative-state, drop the SHA token + "no SARIF export" framing → keep the behavior+reason) **and `:250`** (the second SonarCloud-removal instance: drop the `2026-05-29` date token, keep why). *(Both confirmed this pass — the master's `:158` drifted.)*
4. **ADR bodies 013+** — strip dates-in-prose, commit/PR IDs, phase/PR tokens, but
   **rewrite to preserve fact + why**. Worked example (ADR-013 §SARIF): "removed in
   commit 9236826 (the dismissed-fingerprint bug…)" → "removed because the
   dismissed-fingerprint bug made new alerts invisible." **Frozen 001–012 log
   excluded.**
5. **decisions.md / phases.md** — strip chronological prose + PR/phase logistics
   from free-text cells; keep the structural table + decision substance; add
   rationale-trace working links where useful.

## Steps per batch

- Pull the batch's hits from DD-INV; apply purge/keep + reconciliation rule.
- Run the DD-B link-checker (no broken/active-plan links introduced).
- `/precommit` → commit → `/refactor` → push. Repeat next batch.

## Reviewer checklist

- [ ] Every DD-INV `docs/**`+`.claude` hit resolved or KEEP-justified; M4 handled per its split.
- [ ] No substantive ADR rationale deleted; ADR frontmatter (`date:`/`status:`/`supersedes:`) intact.
- [ ] Frozen `Architecture-Decision-Records.md` untouched.
- [ ] DD-B link-checker green; the four Testing.md links resolve.
- [ ] No double-edit conflict with teardown TD6 (TD6 content settled first, or teardown ADRs deferred to a final batch).
- [ ] Full `/precommit` gauntlet green per batch.

## Done when

Every scoped doc/ADR-body inventory hit is resolved per the doctrine, links
resolve, and rationale is preserved.
