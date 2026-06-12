# TD6 — Docs + ADRs (reflect the teardown)

**Parent:** [`dormant-scale-out-teardown.md`](dormant-scale-out-teardown.md) (§2 Docs/ADRs).
**Execution order:** **6th / last** (after TD5 — docs reflect the final code).
**Status:** **BLOCKED on a governance prerequisite** — see below.
**Risk:** Low on content; the blocker is process, not code.

## ⚠ Hard prerequisite — the ADR-immutability hook will block in-place ADR edits

Master §0 directs ADRs to be **amended in place**. But the write-guard hook
**refuses any `Edit`/`Write` to an existing `docs/adr/ADR-*.md`**
([`pretooluse-write-guard.sh:35-40`](../hooks/pretooluse-write-guard.sh#L35-L40),
`adr-immutable`). **No bypass exists.** Therefore TD6 **cannot run** until
[`current-state-docs-doctrine-and-adr-mutability.md`](current-state-docs-doctrine-and-adr-mutability.md)
lands and modifies that hook to permit in-place ADR amendment.

**Until the flip lands, TD6 has two options — pick one with the user:**
- **(A) Wait** for the governance flip, then amend ADRs in place (master's intent).
- **(B) Supersede** ADRs 031/033 with new "reverted" ADR files (the *current*
  immutability rule), splitting ADR-034. Avoids the hook, but contradicts master §0.

Do not attempt to edit an ADR before the flip — the hook will hard-block the commit.

## Verified file inventory

| Path | Action | Verified anchor |
|---|---|---|
| `docs/adr/ADR-031-redis-sentinel-session-registry.md` | Mark **reverted/removed** (free-tier YAGNI; design retained in readiness doc). | exists |
| `docs/adr/ADR-033-cross-server-relay-proxy-pod-ip.md` | Mark **reverted/removed**. | exists |
| `docs/adr/ADR-034-scale-out-keda-shared-keys.md` | **Split:** KEDA/PDB reverted; **shared keys stays** (live in prod). | exists |
| `docs/adr/ADR-023-relay-extraction-redis-session-registry.md` | Annotate: Redis adapter + cross-server proxy removed; port slimmed per TD1 §3. | exists |
| `docs/adr/ADR-030-kubernetes-adoption-oke-helm.md` | Drop the "Redis/cross-server deferred to PR-C" + multi-node-L4 references with no remaining target. | exists |
| [`docs/Multiscale-Readiness.md`](../../docs/Multiscale-Readiness.md) | **Reframe** §3 from "dormant (built)" to "removed — design retained here as the rebuild spec"; remains the SSOT. | exists |
| [`.claude/decisions.md`](../decisions.md) | Update rows for ADR-023/030/031/033/034. | sweep |
| [`.claude/phases.md`](../phases.md) | Update Phase 13b PR-C/PR-E rows. | sweep |
| `docs/Architecture.md`, `docs/Kubernetes.md`, `docs/Kubernetes-Migration.md`, `docs/Testing.md`, `README.md` | Remove cross-server / Redis / multiserver prose. | sweep hits |
| [`.claude/techdebt.md`](../techdebt.md) | Drop entries now resolved by removal; keep the intentionally-retained items (readiness §10). | review |

## Known false positives (do NOT edit as part of the teardown)

- `docs/adr/ADR-020-*.md`, `docs/adr/ADR-027-*.md` mention these terms **in
  passing** — leave unless the mention is now factually wrong.

## Steps (after the governance flip lands)

1. Read [`docs/README.md`](../../docs/README.md) first (link-don't-paraphrase; ADR
   conventions).
2. Amend the ADRs (per chosen option A/B), reframe the readiness doc §3, update
   `decisions.md` + `phases.md`, scrub the prose docs + README.
3. Run the **final dangling-reference sweep** (master §1 grep) — it must return
   **zero** outside the rebuild-spec doc, the intentionally-annotated ADRs, and
   the teardown plan files.
4. `make docs`/`wiki-audit` (if present) clean.
5. `/precommit` → commit → `/refactor` → push. (Docs-only still runs the full
   gauntlet per the precommit rule.)

## Reviewer checklist

- [ ] Governance flip confirmed landed (hook permits ADR edits) **before** any ADR was touched — or option B (supersede) was used.
- [ ] ADRs 031/033 marked reverted; 034 split (shared keys retained); 023/030 annotated.
- [ ] Readiness doc §3 reframed to "removed — rebuild spec"; still the SSOT.
- [ ] `decisions.md` + `phases.md` rows updated; prose docs + README scrubbed.
- [ ] Master §1 dangling-reference sweep returns zero in scope.
- [ ] Documented false positives (ADR-020/027, docker-hub-mirror) untouched.
- [ ] Full `/precommit` gauntlet green.

## Done when

Every doc/ADR reflects the single-server reality, the readiness doc is the
rebuild spec, and the project-wide reference sweep is clean.
