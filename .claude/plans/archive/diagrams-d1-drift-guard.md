# Micro-Plan D1: Drift-Guard Hardening

**Parent master:** `diagrams-as-code-part-2.md` (§5 D1). **Branch:** `dev`.
**Owner:** Bash/CI. **Depends on:** nothing. **Do first** (closes a proven gap).

## 1. Goal

Close the enforcement hole: today [`docs-diagrams.test.sh`](../../../scripts/tests/docs-diagrams.test.sh)
pins only **2 of 6** diagram-bearing docs, so 4 diagrams can vanish silently. Pin them
all, add a total-fence floor, and add a lightweight human nudge to review diagrams when
the code they depict changes.

## 2. Scope

**In:** extend the existing shell test to guard every diagram-bearing doc; add a
CODEOWNERS entry + a PR-template checklist nudge.
**Out:** C4 (D2), syntax validation (D3), new diagrams (D4).

## 3. File inventory

| File | Change |
|---|---|
| [`scripts/tests/docs-diagrams.test.sh`](../../../scripts/tests/docs-diagrams.test.sh) | Add `assert_mermaid_count_at_least` for [`docs/Wire-Protocol.md`](../../../docs/Wire-Protocol.md) (≥1), [`docs/Monitoring.md`](../../../docs/Monitoring.md) (≥1), [`docs/adr/ADR-025-cd-preflight-digest-check.md`](../../../docs/adr/ADR-025-cd-preflight-digest-check.md) (≥1); convert the inline Multiscale check to the helper; add a **total-fence floor** (`grep -rc '^```mermaid$' docs` ≥ current count). Keep all existing assertions + the blob/fence bans. |
| `.github/CODEOWNERS` | **New.** Own `docs/Architecture.md`, `docs/Wire-Protocol.md`, `docs/Multiscale-Readiness.md`, `docs/Monitoring.md` and the diagram-source code paths (`agent/crates/mesh-agent/src/main.rs`, `server/internal/agentapi/**`, relay). **Caveat:** on a solo-maintainer repo CODEOWNERS does not add a reviewer — it is a declared-ownership record; the effective nudge is the PR-template item below. |
| `.github/pull_request_template.md` | **New.** Checklist line: "Touched handshake/relay/topology/agentapi code? → reviewed the affected `docs/` diagram for drift." |

## 4. Approach (TDD)

1. Add the new `assert_mermaid_count_at_least` calls + total floor to the test.
2. **Prove the guard bites:** temporarily delete one Mermaid block from each newly-pinned
   doc and confirm the shell-tests step **fails**; restore. (Document this manual check in
   the PR.)
3. Add `.github/CODEOWNERS` + `.github/pull_request_template.md`.
4. `make shell-quality` (shellcheck + shfmt + behavioral tests) green.
5. `/precommit` → commit → `/refactor` → `/precommit` → commit → push.

## 5. Acceptance criteria / DoD

- [ ] `docs-diagrams.test.sh` pins **all 6** diagram-bearing docs + a total-fence floor.
- [ ] Removing any guarded diagram **reds** the gauntlet's shell-tests step (demonstrated).
- [ ] `.github/CODEOWNERS` + PR template added; the template checklist item is present.
- [ ] `make shell-quality` + full `/precommit` green; the test stays grep-only/zero-network.

## 6. NFRs

- **Performance:** still pure grep — sub-second, zero-network; no new runtime.
- **Maintainability:** structural guard only (no AST extraction); advisory nudge, never a
  false gate.

## 7. Reviewer/QA checklist

- [ ] Every diagram-bearing doc has a pinned minimum; total floor present.
- [ ] Manual "delete-a-diagram reds the run" evidence attached.
- [ ] CODEOWNERS caveat (solo repo) noted; PR-template checklist is the real nudge.
- [ ] No blob/fence bans removed.
