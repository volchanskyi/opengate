# Micro-plan: Retire the VM SSH Path

**Status:** Completed.
**Owner:** delegated engineer. **Reviewer:** Ivan (verifies against the acceptance checklist).
**Branch:** `dev` (all work — see [`.claude/rules/git.md`](../../rules/git.md)).

**Implementation note:** the QUIC harness runs in a short-lived cluster pod and
targets the ready staging server pod IP. The chart's default single-node mode
does not publish the QUIC port through the ClusterIP Service, so direct pod
networking preserves the intended in-cluster UDP path without expanding the
Service.

---

## 1. Why this exists

The compose→OKE cutover was executed and the compose VM (`163.192.34.124`) was
**decommissioned 2026-06-10** (`terraform apply` destroyed the instance; see the
"OKE Cutover Execution + Compose VM Decommission" row in
[`.claude/phases.md`](../../phases.md)). Every code path that SSHes to that VM is now
**dead** — it cannot succeed because the host no longer exists. Yet the repo still
carries:

- two **compose deploy jobs** in `cd.yml`, gated off only by a repo variable;
- a **load-test workflow** that is 100% SSH-to-VM;
- an **`ssh-docker` transport branch** in the Loki push/query scripts (kept as a
  "pre-cutover default" that is now unreachable);
- the **`oci-ssh-setup` / `oci-ssh-teardown`** composite actions.

This micro-plan deletes that dead path, rewrites load-testing to run against the
OKE cluster, makes the Loki transport kubectl-only, and adds a **reintroduction
guard** so the SSH path cannot silently come back.

The replacement primitives already exist and are the pattern to copy:
[`.github/actions/oci-kube-setup`](../../../.github/actions/oci-kube-setup/action.yml)
(kubeconfig), the `deploy-staging-k8s` / `deploy-production-k8s` jobs in `cd.yml`,
and the `LOKI_PUSH_MODE=kubectl` branch already wired in
`mutation.yml` / `pmat-trend.yml` / `terraform-drift.yml`.

---

## 2. Scope — five parts

| Part | What | Depends on |
|------|------|------------|
| A | Delete the two compose deploy jobs in `cd.yml`; make the k8s jobs unconditional | — |
| B | Rewrite `load-test.yml` to run against the OKE cluster (kubectl, no SSH) | — |
| C | Make the Loki transport kubectl-only (drop `ssh-docker`) | — |
| D | Delete the `oci-ssh-setup` + `oci-ssh-teardown` composite actions | A **and** B (last SSH consumers) |
| E | Reintroduction guard (`scripts/` grep-guard + `scripts/tests/*.test.sh`) | A–D (must pass the post-removal tree) |

**Sequencing:** do A, B, C in any order; then D; then E. E **must** be authored so
it passes only *after* A–D are done (a guard committed first would block the very
edits that remove the patterns). Land E in the same PR as the removals, or as the
final commit. (TDD nuance for E in §3.E and §7.)

---

## 3. Implementation steps

### Part A — Delete the compose deploy jobs in `cd.yml`

[`.github/workflows/cd.yml`](../../../.github/workflows/cd.yml) has four deploy jobs.
Two are compose (SSH) and two are k8s; the k8s pair is gated `vars.K8S_CUTOVER ==
'true'`, the compose pair `vars.K8S_CUTOVER != 'true'`. Post-cutover the gate is
permanently true, so the compose jobs are dead weight.

1. **Delete** the `deploy-staging` job (compose; uses `oci-ssh-setup`, `scp`/`ssh
   deploy-target`, `deploy.sh`/`smoke-test.sh`/`rollback.sh`, `oci-ssh-teardown`).
2. **Delete** the `deploy-production` job (compose; same shape).
3. **Un-gate the k8s jobs** — remove the `&& vars.K8S_CUTOVER == 'true'` /
   `if: vars.K8S_CUTOVER == 'true'` conditions from `deploy-staging-k8s` and
   `deploy-production-k8s`. They become the only deploy jobs.
4. **Rewire `needs:`** —
   - `deploy-production-k8s.needs` → `[resolve-tag, deploy-staging-k8s]` (drop the
     now-deleted `deploy-staging`).
   - `notify-failure.needs` currently lists all four jobs; drop the two deleted
     names, leaving `[resolve-tag, deploy-staging-k8s, deploy-production-k8s]`.
5. **Rename (optional, recommended):** drop the `-k8s` suffix from the two
   surviving job ids/names now that there is only one deploy path. If you rename,
   update **every** `needs:` reference and the `notify-failure.needs` list in the
   same edit (grep `deploy-staging-k8s\|deploy-production-k8s` across the file
   first). If a rename risks missing a reference, **keep the names** — clarity of
   `needs:` wiring beats cosmetics.
6. **Remove `env.DEPLOY_DIR: /opt/opengate`** at the top of `cd.yml` (it was only
   read by the compose jobs — confirm with `grep -n DEPLOY_DIR .github/workflows/cd.yml`
   returns nothing after the deletions).
7. **`K8S_CUTOVER` repo variable** is now unreferenced in the repo. Note it in the
   PR description for the reviewer to delete from repo settings (Settings →
   Variables); deleting the variable itself is a reviewer/admin action, not a code
   change. Assert in §3.E that no workflow references it any more.

> Do **not** delete `deploy/docker-compose*.yml`, `deploy/scripts/deploy.sh`,
> `rollback.sh`, the Caddyfiles, or the `compute` terraform module. The cutover
> deliberately **kept** the compose stack + compute module as a tested rollback
> path (see the phases.md decommission row). This micro-plan retires the **CI SSH
> path**, not the rollback artifacts. `smoke-test.sh` is still used by the k8s job
> (`deploy/scripts/smoke-test.sh --host 127.0.0.1 --port 18080 ...`) — leave it.

### Part B — Rewrite `load-test.yml` against the cluster

[`.github/workflows/load-test.yml`](../../../.github/workflows/load-test.yml) today:
`oci-ssh-setup` → `ssh deploy-target` to install k6, copy scenarios, run k6 +
the Go QUIC `loadtest` harness against the compose staging stack, `docker cp` the
CA out of `opengate-server-staging`, then `oci-ssh-teardown`. All of this targets
the dead VM. Rewrite it to run against OKE staging.

**Canonical pattern to copy:** the `deploy-staging-k8s` job's "Port-forward server
→ localhost:18080" step (`kubectl -n "$NAMESPACE" port-forward "svc/${RELEASE}-server"
18080:8080`) and its `oci-kube-setup` usage. The staging server Service is
`{RELEASE}-server` with named ports `http` (8080), `quic`, `mps` — see
[`deploy/helm/opengate/templates/server-service.yaml`](../../../deploy/helm/opengate/templates/server-service.yaml)
and `server.quicPort` / `server.mpsPort` in
[`deploy/helm/opengate/values.yaml`](../../../deploy/helm/opengate/values.yaml).

Concrete rewrite:

1. **Swap setup/teardown:** replace `oci-ssh-setup` with `oci-kube-setup`
   (tenancy/user/fingerprint/private-key/region + `cluster-id: ${{ secrets.OKE_CLUSTER_ID }}`).
   **Delete** the `oci-ssh-teardown` step entirely (kubeconfig needs no NSG/SSH
   teardown). Delete the `env.DEPLOY_DIR` and the `Detect VPS architecture`
   (`uname -m`) step — the harness now runs on the runner, so build for the
   runner arch.
2. **Phase 1 (k6 HTTP/WS):** run k6 **on the runner**, not over SSH. Install k6
   with `grafana/setup-k6-action` (or download the linux-amd64 release to the
   runner). Port-forward staging (`kubectl -n "$NAMESPACE" port-forward
   "svc/${RELEASE}-server" 18080:8080 &`, then poll `/api/v1/health` like the
   smoke-test step does), and run the existing scenarios in
   [`load/k6/scenarios/`](../../../load/k6/scenarios/) with
   `--env BASE_URL=http://127.0.0.1:18080`. Resolve `NAMESPACE`/`RELEASE` the same
   way `deploy-staging-k8s` does (read that job for the exact derivation).
3. **Phase 2 (Go QUIC harness):** build `server/tests/loadtest` on the runner
   (`CGO_ENABLED=0 go build -trimpath -o /tmp/loadtest ./tests/loadtest/`). The CA
   for mTLS now comes from the cluster, not `docker cp`:
   `kubectl -n "$NAMESPACE" exec "deploy/${RELEASE}-server" -- cat /data/ca.crt`
   (and `ca.key`) piped to local files, **or** read from the server Secret if the
   chart mounts the CA there (check
   [`deploy/helm/opengate/templates`](../../../deploy/helm/opengate/templates)).
   - **QUIC reachability is the one real design choice.** `kubectl port-forward`
     tunnels **TCP only** — it will not carry QUIC's UDP. Two viable options;
     **pick the in-cluster Job** unless you prove the alternative:
     - **(Recommended) In-cluster Job:** `kubectl apply` a short-lived `Job` that
       runs the `loadtest` image/binary against the in-cluster Service DNS
       (`{RELEASE}-server.{NAMESPACE}.svc:9090`), then `kubectl wait` + stream
       `kubectl logs`. Robust, mirrors how Loki push runs a throwaway pod
       (§3.C). Requires the harness to be available in-cluster (build+push a tiny
       image, or `kubectl cp` the static binary into a `kubectl run` pod).
     - **(Only if proven) UDP port-forward:** modern `kubectl` added UDP
       port-forward; if you can demonstrate a stable QUIC handshake through it,
       it keeps the harness on the runner. Do **not** assume it works — validate
       in a scratch run first.
   - This is the single judgement call in Part B. State the chosen approach in the
     PR description with the evidence (a green scratch run).
4. **Keep the cron/dispatch triggers and the `agents` input.** Keep the comment
   explaining why the job deliberately avoids `environment: staging` (unattended
   cron) — it still applies. Update any wording that says "VPS".
5. **UDP buffer sysctl** (`net.core.rmem_max`/`wmem_max`) was tuning the VM; for an
   in-cluster Job it belongs on the node/pod, not the runner — drop it from the
   runner side and note in the Job manifest if needed (cluster nodes are already
   sized for the prod server's QUIC listener).

### Part C — Make the Loki transport kubectl-only

The workflows already pass `LOKI_PUSH_MODE: kubectl` (mutation.yml, pmat-trend.yml,
terraform-drift.yml) and already use `oci-kube-setup`. The `ssh-docker` branch is
the dead default. Remove it.

1. [`scripts/lib/loki-push.sh`](../../../scripts/lib/loki-push.sh): delete the
   `ssh-docker` `case` arm and the `LOKI_PUSH_MODE` `case` switch entirely. The
   function unconditionally runs the throwaway `curlimages/curl` pod via `kubectl`
   (keep the `LOKI_NAMESPACE` default `monitoring` / `LOKI_SERVICE` default
   `monitoring-loki` knobs — they parameterize the kept path). Update the header
   comment to describe only the kubectl transport.
2. [`scripts/pmat-loki-query.sh`](../../../scripts/pmat-loki-query.sh): drop the
   `case "${LOKI_PUSH_MODE...}"` switch and the `ssh -o StrictHostKeyChecking ...
   deploy-target ... docker run --network opengate-monitoring_monitoring ...`
   default branch; keep only the `kubectl run loki-query-$$ ...` path. Update the
   comment block (remove "production VPS" / "ssh-docker is the pre-cutover
   default").
3. [`scripts/mutation-loki-push.sh`](../../../scripts/mutation-loki-push.sh),
   [`scripts/pmat-loki-push.sh`](../../../scripts/pmat-loki-push.sh),
   [`scripts/terraform-drift-loki-push.sh`](../../../scripts/terraform-drift-loki-push.sh):
   they `source` the lib, so behavior follows. **Update their header comments** —
   remove "via the production VPS", "SSH tunnel", "monitoring docker network",
   "`LOKI_PUSH_MODE=ssh-docker` keeps the pre-cutover path", and the
   `Required env: DEPLOY_SSH_PRIVATE_KEY, DEPLOY_HOST` lines (no longer used).
4. **Remove the now-redundant `LOKI_PUSH_MODE: kubectl` env** from
   `mutation.yml`, `pmat-trend.yml`, `terraform-drift.yml` (the variable no longer
   exists). **Keep** `LOKI_NAMESPACE`/`LOKI_SERVICE` if a workflow overrides the
   defaults. Confirm each of these three workflows still has its `oci-kube-setup`
   step (they do — do not remove it).
5. Per [`.claude/rules/code.md`] there are shell tests; if any
   `scripts/tests/*.test.sh` references `LOKI_PUSH_MODE`/`ssh-docker`, update it.

### Part D — Delete the SSH composite actions

After A + B, nothing references the SSH composites. Confirm, then delete:

1. `grep -rn "oci-ssh-setup\|oci-ssh-teardown" .github/ scripts/` → must be empty.
2. `rm -rf .github/actions/oci-ssh-setup .github/actions/oci-ssh-teardown`.
3. **Do not** touch `.github/actions/oci-kube-setup` or
   `.github/actions/verify-oci-tfstate-creds` (both still in use).
4. The SSH-only secrets `DEPLOY_SSH_PRIVATE_KEY`, `DEPLOY_HOST`, `OCI_CD_NSG_ID`
   are now unused by code. Note them in the PR for the reviewer to remove from
   repo secrets (admin action). Assert in §3.E that no workflow/script references
   them.

### Part E — Reintroduction guard (TDD artifact — author its TEST first)

Add a grep-based guard so the SSH path cannot return. Model on
[`scripts/arch-lint-flip.sh`](../../../scripts/arch-lint-flip.sh) (script style) and
[`scripts/tests/build-image-workflow.test.sh`](../../../scripts/tests/build-image-workflow.test.sh)
(test harness style).

1. **Create `scripts/no-vm-ssh-guard.sh`** — exits non-zero if any banned pattern
   appears, **scoped to `.github/workflows/`, `.github/actions/`, and `scripts/`**
   (never `docs/`, ADRs, or `.claude/plans/` — those legitimately record history).
   Banned patterns:
   - `oci-ssh-setup` / `oci-ssh-teardown`
   - `deploy-target` (the SSH host alias)
   - `LOKI_PUSH_MODE` and `ssh-docker` and `opengate-monitoring_monitoring`
   - `DEPLOY_DIR` / `/opt/opengate`
   - `K8S_CUTOVER`
   - `DEPLOY_SSH_PRIVATE_KEY` / `DEPLOY_HOST` / `OCI_CD_NSG_ID`
   - `ssh deploy-target` / `scp ... deploy-target`
   Print each offending `file:line` and a one-line remediation. This script itself
   must be referenced by the gauntlet — add a `run_check "no-vm-ssh-guard" --
   bash scripts/no-vm-ssh-guard.sh` line in
   [`scripts/precommit-gauntlet.sh`](../../../scripts/precommit-gauntlet.sh) near the
   other guard checks (`lint-deploy`/`dead-code`).
2. **Create `scripts/tests/no-vm-ssh-guard.test.sh`** (executable; auto-discovered
   by the gauntlet's `scripts/tests/*.test.sh` loop). Cases:
   - **positive:** a temp tree containing `oci-ssh-setup` (and one for each pattern
     class) makes the guard exit non-zero with the offending path reported;
   - **negative:** the real repo tree (post-removal) makes the guard exit 0;
   - **scope:** a `docs/`-only occurrence does **not** trip the guard (history is
     allowed).
   Write this test **first** (§7) — it is the TDD-gate-satisfying change and will
   fail until the guard script + A–D removals exist.

> Why a guard and not just deletion: the patterns are easy to reintroduce by
> copy-paste from git history or an old runbook. A deterministic gauntlet check is
> the project's idiom for "this must not come back" (cf. the test-skip guard, the
> suppression-string write-guard).

---

## 4. Documentation update (required — `/docs` is canonical)

Per [`.claude/rules/editing-and-scope.md`](../../rules/editing-and-scope.md), each
change set ends with a `/docs` update. There is **already an open Medium techdebt
item** — "Cutover doc drift — Monitoring.md / Continuous-Deployment.md still
describe the VPS path" (see [`.claude/techdebt.md`](../../techdebt.md)). This
micro-plan's CD/load-test changes make
[`docs/Continuous-Deployment.md`](../../../docs/Continuous-Deployment.md) more stale
(it still documents `ssh ubuntu@<VPS>` rollback + `/opt/opengate`). Either:

- fold the CD-doc repointing into this PR (kubectl/helm deploy jobs, no SSH), and
  update the techdebt item to reflect the narrowed remainder; **or**
- if the reviewer prefers to keep the doc rewrite as the tracked `/wiki-audit`
  pass, at minimum update the techdebt entry to note that the SSH **code** path is
  now gone (so the doc is the only thing left describing it). State which in the PR.

Do not paraphrase numbers/paths into the docs — link to source per
[`docs/README.md`](../../../docs/README.md).

---

## 5. Reviewer acceptance checklist

- [ ] `cd.yml` has exactly two deploy jobs (staging + production, k8s), both
      unconditional; `deploy-staging`/`deploy-production` (compose) are gone;
      `needs:` and `notify-failure.needs` updated; `DEPLOY_DIR` removed.
- [ ] `grep -rn "K8S_CUTOVER\|deploy-target\|/opt/opengate\|oci-ssh" .github/ scripts/`
      → empty.
- [ ] `load-test.yml` uses `oci-kube-setup`, no `ssh`/`scp`/`oci-ssh-*`; k6 runs
      against a port-forwarded staging server; the QUIC harness reaches the cluster
      by the chosen method, with a green scratch run linked in the PR.
- [ ] `scripts/lib/loki-push.sh` + `pmat-loki-query.sh` are kubectl-only; no
      `LOKI_PUSH_MODE`/`ssh-docker` anywhere in `scripts/`; the three push-script
      headers no longer mention SSH/VPS/compose network; `LOKI_PUSH_MODE: kubectl`
      env removed from the three workflows (which still have `oci-kube-setup`).
- [ ] `.github/actions/oci-ssh-setup` + `oci-ssh-teardown` deleted; `oci-kube-setup`
      + `verify-oci-tfstate-creds` untouched.
- [ ] `scripts/no-vm-ssh-guard.sh` exists, is wired into the gauntlet, and trips on
      each banned pattern; `scripts/tests/no-vm-ssh-guard.test.sh` covers
      positive/negative/scope and passes.
- [ ] `actionlint`, `make lint`, full `/precommit` gauntlet green.
- [ ] PR description lists the now-unused repo variable (`K8S_CUTOVER`) and secrets
      (`DEPLOY_SSH_PRIVATE_KEY`, `DEPLOY_HOST`, `OCI_CD_NSG_ID`) for admin cleanup.
- [ ] Doc/techdebt decision (§4) recorded in the PR.
- [ ] The Loki trend pipelines (mutation/pmat/terraform-drift) still push
      successfully on their next scheduled run (or a manual `workflow_dispatch`).

---

## 6. Out of scope / do not touch

- `deploy/docker-compose*.yml`, `deploy/scripts/{deploy,rollback}.sh`,
  `deploy/caddy/*`, the terraform `compute` module — **kept** as the documented
  rollback path.
- `deploy/scripts/smoke-test.sh` — still invoked by the k8s deploy job.
- `bastion-session.sh` / `make tunnel` — already repointed to OKE during the
  decommission; not part of this path.
- The Docker Hub mirror work — sibling micro-plan
  [`download-spof-hardening.md`](../download-spof-hardening.md).
- Multi-replica / Redis / NetworkPolicy items — separate techdebt, gated on the
  multi-replica cutover.

---

## 7. Execution workflow (enforced — no bypass)

Per [`CLAUDE.md`](../../../CLAUDE.md) and `.claude/rules/`:

1. `git checkout dev && git pull --rebase origin dev`.
2. **TDD first:** write `scripts/tests/no-vm-ssh-guard.test.sh` (Part E) before the
   guard script / removals — it satisfies the TDD gate and will be red until A–E
   land. (The workflow/script edits in A–D are not "source" under the classifier,
   but authoring the test first is the cleanest way to keep the gate silent for the
   whole branch and gives an immediate regression net.)
3. Implement A → B → C → D → E. Run `actionlint`, `bash scripts/no-vm-ssh-guard.sh`,
   and the new test locally until green.
4. `/precommit` (full gauntlet — lints, shell tests incl. the new guard test,
   sonar) → must pass.
5. `git commit` (author = Ivan Volchanskyi, **no** `Co-Authored-By`).
6. `/refactor` → `/precommit` again → commit → `git push origin dev`.
7. After CI is green, trigger a `load-test.yml` `workflow_dispatch` and a Loki
   push workflow (e.g. `pmat-trend`/`terraform-drift`) `workflow_dispatch` to prove
   the rewritten paths work against the cluster; link both runs to the reviewer.

---

## 8. Suggested commit slicing

One PR is fine, but if split, keep each commit independently green (each must pass
`/precommit`):

1. **C** (Loki kubectl-only) — smallest, self-contained, no cross-deps.
2. **A** (cd.yml compose-job removal).
3. **B** (load-test rewrite).
4. **D + E** (delete SSH composites + land the guard together, so the guard's
   negative case passes on the same tree that removes the last references).
