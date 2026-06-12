# Phase 13b PR-B — Kubernetes Adoption (OKE)

**Created:** 2026-06-02 · **Parent:** [phase-13b-multiserver-scaling.md](phase-13b-multiserver-scaling.md) §4 PR-B · **Status:** Completed — repo-safe slice landed; cluster cutover is deploy-time (runbook `docs/Kubernetes-Migration.md`). Archived 2026-06-05.

## Progress (2026-06-02)

**Landed (additive, repo-safe, statically validated):** B1 app Helm chart + `policy/k8s` Rego gate + `make lint-k8s` (+ `lint-deploy`/CI wiring) + Checkov helm framework; B2 OKE terraform module + 6 free-tier tftest invariants; B6 cutover runbook; ADR-030 + decisions row + docs/Kubernetes.md + techdebt (per-replica CA/VAPID). Validated: `make lint-k8s` green, `helm lint`/`kubeconform`/`conftest` green across all overlays, `terraform -chdir=modules/oke test` 6/6, yamllint + actionlint clean, Checkov exit 0.

**Cutover-gated, now BUILT DORMANT (default-off; the live compose path is byte-for-byte unchanged until the `K8S_CUTOVER` repo variable is set at cutover):** B3 monitoring chart `deploy/helm/monitoring` (7 services, validated via `make lint-k8s`); B4 `oci-kube-setup` composite + dual-path `cd.yml` (`deploy-staging-k8s`/`deploy-production-k8s` jobs `helm upgrade --install`, gated `K8S_CUTOVER=='true'`; compose jobs gated `!='true'`; `needs` skip-propagation keeps both DAG paths correct); B5 staging-E2E via `kubectl port-forward`→localhost:18080 (reuses `smoke-test.sh` + `playwright.staging.config.ts` unchanged) + DB reset via `kubectl exec`; loki-push retarget via shared `scripts/lib/loki-push.sh` (`LOKI_PUSH_MODE` default `ssh-docker`). Runtime bits still need cluster validation at cutover (promtail RBAC/log-access, VM `kubernetes_sd`, `helm` against OKE, cert-manager ACME) — see `docs/Kubernetes-Migration.md` (removed) §4–5. Activation: set `K8S_CUTOVER` + `OKE_CLUSTER_ID`/`DEPLOY_ACME_EMAIL` secrets.

**Validation (offline):** `make lint-k8s` green (both charts, all overlays); Checkov helm green (both); OKE `terraform test` 6/6; actionlint + yamllint clean; loki-push default path preserved.

**Not committed** — awaiting `/precommit` + the user's go-ahead.

## Context

PR-A landed (`4e8e8e6`) — `InProcessRegistry` is on the live relay path; §6 decisions are resolved (OKE; in-cluster Postgres StatefulSet+PVC on `oci-bv`; in-place convert of the prod VM; Redis Sentinel HA — Redis itself is PR-C). PR-B is the *precondition for everything multi-*: take the **same single-replica app, now on k8s**. It does not scale yet (that's PR-E) and does not add Redis (PR-C).

This means translating the whole single-VM `docker compose` stack — app, Postgres, the Caddy edge, **and the monitoring stack** — onto OKE, provisioning the cluster in terraform, **rewiring CD and every workflow that targets the live staging environment**, and writing the cutover runbook.

User decisions (this session): **Helm** packaging · **ingress-nginx + cert-manager** for TLS/ACME · **all repo deliverables** (manifests + OKE terraform + CD + runbook) · monitoring stack migrated · staging-targeting workflows rewired.

**Load-bearing simplification (verified):** the Go server already serves the SPA itself with index.html fallback via `os.OpenRoot` ([api.go:280-323](../../server/internal/api/api.go#L280-L323)) when `-web-dir` is set. So on k8s we run the server with `-web-dir /srv/web` and **delete the `web-init` initContainer + shared `web-assets` volume** entirely. The TLS edge becomes a pure terminator/proxy — it serves nothing static.

## Constraints carried in

- **Always-Free 4 OCPU / 24 GB**, single-node start. OKE control plane is free on **BASIC** cluster; workers are the A1.Flex.
- **Non-HTTP L4 ports**: QUIC `9090/udp`, MPS `4433/tcp` cannot ride an HTTP Ingress — exposed via ingress-nginx `tcp-services`/`udp-services` ConfigMaps (single-node `NodePort`/`hostPort` fallback documented).
- **TDD gate stays silent**: `SOURCE_EXT_RE='\.(go|rs|tsx?|jsx?)$'` ([tdd-check.sh:22](../../scripts/tdd-check.sh#L22)) excludes yaml/tf/rego/sh, and PR-B touches no Go/Rust/TS. "Tests-first" discipline = Rego `*_test.rego`, terraform `*.tftest.hcl`, bash `scripts/tests/*.test.sh` — written before the artifact they cover, exactly the IaC-pyramid pattern.
- **precommit-per-PR**: every commit below runs the full gauntlet.
- ADRs are immutable and **must not link this plan file** (write-guard `adr-plan-link`) — rationale inline, pointer in `decisions.md`.

## Deliverables — sequenced commits (each gauntlet-gated)

### B1 — Helm chart for the app + validation gate · `deploy/helm/opengate/`
- `Chart.yaml`, `values.yaml` + `values-staging.yaml` + `values-production.yaml` (mirrors the `docker-compose.yml` / `docker-compose.staging.yml` split).
- `templates/`:
  - **server** — Deployment (single replica, `-web-dir /srv/web`, args + resources from compose: 512M/1.0), Service (`8080` HTTP ClusterIP + `9090/udp` + `4433/tcp`), ServiceAccount, readiness/liveness on `/api/v1/health` + `/healthz`.
  - **postgres** — StatefulSet + `volumeClaimTemplate` (StorageClass `oci-bv`), headless Service, `init.sql` via ConfigMap, limits from compose.
  - **postgres-backup** — CronJob (`@daily`, `pg_dump`, `BACKUP_KEEP_DAYS=7` parity) replacing the `prodrigestivill` sidecar.
  - **ingress** — Ingress (`{{ .Values.domain }}` + `status.…`; `/`,`/api`,`/ws` → `server:8080`) carrying the **exact CSP/HSTS/security headers from the Caddyfile** via ingress-nginx `server-snippet`/`configuration-snippet`; cert-manager annotation.
  - **cert-manager** — `ClusterIssuer` (Let's Encrypt prod+staging, HTTP-01 via ingress-nginx), gated on `.Values.certManager.create`.
  - **secrets** — chart references an **`existingSecret`** (JWT_SECRET, POSTGRES_PASSWORD, AMT_PASS, VAPID_CONTACT) created out-of-band; a documented `secrets.example.yaml` only — **no secret material committed**. (Minimally forces the deprioritized CD Phase E secrets surface.)
  - **L4** — ingress-nginx `tcp-services`/`udp-services` ConfigMap (9090/udp, 4433/tcp → server Service); single-node NodePort fallback documented.
- **Validation gate (the test layer, written first):**
  - `policy/k8s/*.rego` + `*_test.rego` — no `:latest`, resource limits present, `runAsNonRoot`, probes present, no undeclared `hostNetwork` (mirrors [policy/docker_compose/images.rego](../../policy/docker_compose/images.rego)).
  - `Makefile` `lint-deploy`: add `helm lint`, `helm template … | kubeconform -strict`, `helm template … | conftest test -p policy/k8s -`.
  - `.checkov.yaml`: add `kubernetes`/`helm` frameworks (baseline any pre-existing).
  - CI `config-lint` job runs all of the above.
- **ADR-030** (next free number; 029 is latest) — "Kubernetes adoption: Helm + ingress-nginx + cert-manager + in-cluster Postgres StatefulSet"; + `decisions.md` row; new `docs/Kubernetes.md`.

### B2 — OKE terraform · `deploy/terraform/modules/oke/`
- `oci_containerengine_cluster` (**BASIC** = free, pinned k8s version) + node pool (A1.Flex, **≤4 OCPU / ≤24 GB** bounded; start 1 node @ 2 OCPU/12 GB). OKE subnets/NSGs (API-endpoint + node + LB) — extend `modules/networking` or new `modules/oke-networking`; **reuse the existing [bastion module](../../deploy/terraform/modules/bastion/)** for private-cluster `kubectl` access.
- tftest `modules/oke/tests/free_tier.tftest.hcl` mirroring [modules/compute/tests/free_tier.tftest.hcl](../../deploy/terraform/modules/compute/tests/free_tier.tftest.hcl): shape pinned A1.Flex, ≤4/≤24 cap, BASIC cluster, node-count bound; + conftest/Checkov terraform-policy extension for OKE. Wire into `make terraform-test`.
- **Honest note for the runbook:** OKE node pools are OKE-provisioned — "in-place convert" is *tear down compose → free the budget → OKE stands up node 1 → DNS re-points*, not a literal running-VM conversion.

### B3 — Monitoring stack on k8s (your ask #1)
- Port [docker-compose.monitoring.yml](../../deploy/docker-compose.monitoring.yml) (victoriametrics, grafana, loki, promtail, postgres-exporter, node-exporter, uptime-kuma) into a `monitoring` namespace — sibling `deploy/helm/monitoring/` chart:
  - VictoriaMetrics (vmsingle) + vmagent scraping Services; node-exporter + postgres-exporter as DaemonSet/Deployment.
  - Loki (single-binary) Service; **promtail/Alloy DaemonSet** scraping *pod* logs (replaces the VM promtail tailing docker logs).
  - Grafana: port [deploy/grafana/provisioning/](../../deploy/grafana/provisioning/) `{datasources,dashboards,alerting}` → ConfigMaps (datasource URLs → in-cluster Services; preserve `opengate-pmat-trend` / `opengate-mutation-trend` / terraform-drift dashboards; **port the Telegram alerting rules**).
  - uptime-kuma Deployment + PVC.
- **Ripple — Loki push scripts:** `scripts/{mutation,pmat,terraform-drift}-loki-push.sh` SSH→docker→`loki:3100`. Retarget to the in-cluster Loki Service (workflow `kubectl port-forward`, or internal endpoint). Update the 3 scripts **+ their `scripts/tests/*.test.sh`**.

### B4 — CD to k8s · `cd.yml` rewire
- New composite `.github/actions/oci-kube-setup` (mirrors [oci-ssh-setup](../../.github/actions/oci-ssh-setup/action.yml)): OCI CLI auth → `oci ce cluster create-kubeconfig` → helm/kubectl ready.
- `deploy-staging` + `deploy-production`: replace `scp` + `deploy.sh` (compose) with `helm upgrade --install opengate-{staging,prod} deploy/helm/opengate -f values-{staging,production}.yaml --set image.tag=…`. Helm is idempotent → retire or replace the sentinel/`.last-deployed-staging` digest-skip dance (ADR-025) with `helm diff`/release-revision check.

### B5 — Staging-targeting workflow steps (your ask #2)
Split **live-staging (must change)** from **ephemeral-CI-compose (keep)**:
- **cd.yml live-staging steps — rewrite:**
  - smoke-test `--host 127.0.0.1 --port 18080` (SSH) → against the staging **ingress URL**; `smoke-test.sh` gains a url/k8s mode.
  - DB reset `docker exec opengate-postgres-staging psql …` → `kubectl exec sts/opengate-staging-postgres -- psql …` (or one-shot `kubectl run` Job).
  - Playwright `ssh -fN -L 18080` tunnel → run against the staging ingress hostname directly (or `kubectl port-forward`).
- **Keep on compose (verify each, leave unless it targets the live VM):** `ci.yml` integration/e2e jobs, `e2e-cross-browser.yml`, `load-test.yml` — throwaway stacks that test the *app*, not the deployment. Confirm `load-test.yml` doesn't hit live staging; if it does, repoint to the cluster ingress.

### B6 — In-place-convert runbook · `docs/Kubernetes-Migration.md`
`terraform apply` OKE → `helm install` ingress-nginx + cert-manager → `helm install` app (staging ns first) → **`pg_dump` from compose Postgres → restore into StatefulSet PVC** → validate → **DNS cutover** → decommission compose stack (app **+ monitoring**) → reclaim VM budget → grow node pool toward 4/24. Includes the monitoring migration + Loki-push retarget steps and the "OKE-provisioned node, not literal VM conversion" note.

## Verification

- **Local (no cluster):** `make lint-deploy` → helm lint + `helm template | kubeconform -strict` + `helm template | conftest test -p policy/k8s` + Checkov k8s + `conftest verify` on the Rego unit tests + OKE `terraform test`. Full `/precommit` gauntlet per commit.
- **Cluster (with you — needs OCI creds):** `terraform apply` OKE → `oci ce … create-kubeconfig` → `helm install` staging → smoke + Playwright against the staging ingress → monitoring up (Grafana dashboards render, Loki receives pod logs + retargeted push scripts) → cutover dry-run.

## Out of scope (later Phase-13b PRs)

- RedisRegistry + cross-server proxy + Redis Sentinel + the ADR superseding ADR-023's HA deferral (**PR-C**).
- HPA / multi-replica / PodDisruptionBudget tuning (**PR-E**).
- `make e2e-multiserver` + load baseline (**PR-D**).
