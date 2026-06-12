# TD6 — Docs + ADRs (reflect the teardown)

**Parent:** [`dormant-scale-out-teardown.md`](dormant-scale-out-teardown.md) (§2 Docs/ADRs).
**Execution order:** **6th / last** (after TD5 — docs reflect the final code).
**Status:** Complete.
**Risk:** Low.

## Governance prerequisite

ADR-036 and the write-guard now permit in-place ADR maintenance. TD6 can amend
the affected records directly under the current-state documentation doctrine.

## Verified file inventory

| Path | Action | Verified anchor |
|---|---|---|
| `docs/adr/ADR-031-redis-sentinel-session-registry.md` | Mark **reverted/removed** (free-tier YAGNI; design retained in readiness doc). | exists |
| `docs/adr/ADR-033-cross-server-relay-proxy-pod-ip.md` | Mark **reverted/removed**. | exists |
| `docs/adr/ADR-034-scale-out-keda-shared-keys.md` | **Split:** KEDA/PDB reverted; **shared keys stays** (live in prod). | exists |
| `docs/adr/ADR-023-relay-extraction-redis-session-registry.md` | Annotate: Redis adapter + cross-server proxy removed; port slimmed per TD1 §3. | exists |
| `docs/adr/ADR-030-kubernetes-adoption-oke-helm.md` | Drop the "Redis/cross-server deferred to PR-C" + multi-node-L4 references with no remaining target. | exists |
| [`docs/Multiscale-Readiness.md`](../../../docs/Multiscale-Readiness.md) | **Reframe** §3 from "dormant (built)" to "removed — design retained here as the rebuild spec"; remains the SSOT. | exists |
| [`.claude/decisions.md`](../../decisions.md) | Update rows for ADR-023/030/031/033/034. | sweep |
| [`.claude/phases.md`](../../phases.md) | Update Phase 13b PR-C/PR-E rows. | sweep |
| `docs/Architecture.md`, `docs/Kubernetes.md`, `docs/Testing.md`, `README.md` | Remove cross-server / Redis / multiserver prose. | sweep hits |
| [`.claude/techdebt.md`](../../techdebt.md) | Drop entries now resolved by removal; keep the intentionally-retained items (readiness §10). | review |

## Known false positives (do NOT edit as part of the teardown)

- `docs/adr/ADR-020-*.md`, `docs/adr/ADR-027-*.md` mention these terms **in
  passing** — leave unless the mention is now factually wrong.

## Steps

1. Read [`docs/README.md`](../../../docs/README.md) first (link-don't-paraphrase; ADR
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

- [x] Governance flip permits in-place ADR maintenance.
- [x] ADRs 031/033 consolidated as reverted amendments; 034 split (shared keys retained); 023/030 updated.
- [x] Readiness doc reframed as the rebuild specification.
- [x] `decisions.md` + `phases.md` rows updated; prose docs + README scrubbed.
- [x] Master §1 dangling-reference sweep returns zero in scope.
- [x] Documented false positives (ADR-020/027, docker-hub-mirror) untouched.
- [x] Full `/precommit` gauntlet green.

## Done when

Every doc/ADR reflects the single-server reality, the readiness doc is the
rebuild spec, and the project-wide reference sweep is clean.
