# Micro-Plan FI4 — Ingress fault profiles (Option A, edge)

**Master:** `context-driven-fault-injection.md` §11 (FI4), §3 (Edge), §7 (Edge 502/504), §14 open-item 2.
**Branch:** `dev`. **Owner:** engineer (k8s/ingress + scripts). **Sequence:** after the FI1 spec. **Depends on:** the settled FI1 edge contract ([`docs/Fault-Injection.md`](../../../docs/Fault-Injection.md)) + a green staging deploy. FI3 was obsoleted by the no-ship pivot (ADR-055) — there is no server-side staging fault config, and the 504 backend delay comes from Chaos Mesh (FI5), not the server.
**Status:** Implemented (Go/shell TDD, gauntlet green); archived on the completing commit.

## Goal

Reproduce edge 502/504/timeout at ingress-nginx using **reviewed,
version-controlled, staging-only** annotation templates, with save/apply/restore
tooling and a policy test that the **production** Ingress can never receive fault
annotations.

## Context (verified)

- HTTP edge is ingress-nginx + cert-manager (ADR-030). Production + staging share
  one worker → fault annotations must target the **staging** Ingress only (master
  §4).
- Master §14 open-item 2 is **settled** in [`docs/Fault-Injection.md`](../../../docs/Fault-Injection.md):
  504 via a **backend/upstream delay** (Chaos Mesh, FI5) exceeding the ingress
  proxy-read timeout — never a critical-risk raw snippet; 502 via **upstream
  unavailability** (server Deployment scaled to zero), since the controller runs
  with snippet annotations disabled. No reviewed-snippet path is used.

## File inventory

**Create**
- `deploy/fault/ingress/` — version-controlled annotation **templates** (502, 504
  via timeout) scoped to the staging Ingress only.
- `scripts/fault/ingress-apply.sh`, `ingress-restore.sh` — idempotent
  save-current → apply-template → restore tooling with a **namespace guard**
  (refuse anything but the staging namespace) and a `trap`-based restore.

**Modify**
- `deploy/helm/opengate/...` — if any policy template is the cleanest place to
  assert "production Ingress carries no fault annotations," add it there.

**Tests (write first — TDD)**
- `scripts/tests/fault-ingress.test.sh` — assert: (a) apply/restore is idempotent
  and leaves the Ingress byte-identical after restore; (b) the scripts refuse a
  non-staging namespace; (c) a render/policy check proves the **production**
  Ingress has no fault annotations; (d) the 504 path uses an upstream delay where
  chosen, not a raw critical snippet.

## Steps (TDD)

1. Branch current after FI3.
2. **Test first:** idempotent save/restore + namespace-guard tests (red).
3. Add the annotation templates + apply/restore scripts with the namespace guard
   and `trap` restore.
4. Decide 502 (reviewed annotation) vs 504 (upstream delay) per master §14-2;
   document in `docs/Fault-Injection.md`.
5. Verify 502/504 through the **public staging host** manually once; capture the
   evidence shape FI6 will upload.
6. `make shell-check` + the new test green.
7. `/precommit` → commit → `/refactor` → `/precommit` → commit → push.

## Acceptance criteria (master §13)

1. A public staging test produces and recovers from a controlled edge timeout.
2. Apply→restore leaves the staging Ingress unchanged (no residue).
3. Production Ingress provably cannot receive fault annotations.
4. Scripts refuse any non-staging namespace.

## Reviewer checklist

- [ ] Templates are version-controlled and staging-scoped; no inline cluster edits.
- [ ] Namespace guard + `trap`-based restore present and tested.
- [ ] 502/504 approach matches the master §14-2 decision; no unreviewed
      critical-risk raw snippet without explicit approval.
- [ ] Restore is byte-idempotent (test proves it).
- [ ] Shell passes `make shell-check`; no `|| echo SKIP` masking.
