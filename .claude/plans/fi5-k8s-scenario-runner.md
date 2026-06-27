# Micro-Plan FI5 — Kubernetes scenario runner (Option C, reduced)

**Master:** `context-driven-fault-injection.md` §11 (FI5), §3 (Kubernetes C1/C2), §7, §8, §14 (SLOs).
**Branch:** `dev`. **Owner:** engineer (k8s + scripts). **Sequence:** after FI4. **Depends on:** FI3 (staging deploy) and FI1 (measured reconnect/rollback SLOs).
**Status:** Ready after FI4.

## Goal

Idempotent, cleanup-safe scripts for the two infra scenarios that survive
teardown: **C1 single-pod deletion** and **C2 bad-rollout + Helm rollback**
(single-replica `Recreate`), each with exact selectors, namespace guards, and
evidence capture. These run **scheduled/manual** and **do not gate promotion**
until they have a clean scheduled-run history (master Decision 4).

## Context (verified)

- Server Deployment is `Recreate`, single replica post-teardown — C2 tests
  rollback of a bad revision, **not** surge behavior (master §4).
- Production + staging share one worker; the production server binds QUIC/MPS host
  ports → infra faults target **only** the staging namespace, never the shared
  node (master §4). No node-outage scenario (master §3 deferred C4).
- SLO: pod replacement **120 s** (master Decision 8); relay/agent reconnect +
  rollback SLOs come from FI1's measured drills.

## File inventory

**Create**
- `scripts/fault/pod-delete.sh` (C1) — delete the single staging server pod by
  exact selector; assert replacement Ready within the 120 s SLO; confirm clients
  reconnect.
- `scripts/fault/bad-rollout.sh` (C2) — deploy a deliberately-failing revision to
  staging, assert the rollout fails, `helm rollback`, assert the prior image is
  healthy.
- both with: namespace guard (staging only), `trap`-based cleanup, idempotent
  re-run, and evidence capture (`kubectl get events`, rollout status, pod
  restarts, readiness transitions).

**Modify**
- `docs/Fault-Injection.md` — document C1/C2 selectors, SLOs, and the
  "scheduled-evidence, non-gating-until-clean-history" status.

**Tests (write first — TDD)**
- `scripts/tests/fault-k8s.test.sh` — assert (against a kind/throwaway ns or with
  a mocked kubectl): namespace guard refuses non-staging; cleanup runs on failure
  (`trap` + `always`); scripts are idempotent; evidence files are produced.

## Steps (TDD)

1. Branch current after FI4.
2. **Test first:** namespace-guard + cleanup-on-failure + idempotency tests (red).
3. Implement `pod-delete.sh` (C1) with the SLO assertion + evidence capture.
4. Implement `bad-rollout.sh` (C2) with rollback + health re-check.
5. Run both against staging once; record observed reconnect/rollback p95 to feed
   the FI1 SLO thresholds.
6. `make shell-check` + the new test green.
7. `/precommit` → commit → `/refactor` → `/precommit` → commit → push.

## Acceptance criteria (master §13)

1. Pod deletion recovers within the **120 s** SLO; clients reconnect.
2. Bad rollout fails and Helm rollback restores the prior healthy image.
3. Every failure path removes fault resources and verifies **no residue**.
4. Scripts refuse any non-staging namespace; never touch the shared node.

## Reviewer checklist

- [ ] Exact selectors target only the staging server pod/deployment.
- [ ] Namespace guard + `trap` cleanup + `always()`-style teardown present.
- [ ] Idempotent re-run; evidence (events/rollout/readiness) captured as files.
- [ ] No node-level disruption; production QUIC/MPS host ports untouched.
- [ ] SLO assertions use FI1's measured thresholds, not guesses.
- [ ] Shell passes `make shell-check`.
