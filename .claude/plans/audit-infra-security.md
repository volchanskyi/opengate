# Audit Plan — Infrastructure Security Sweep

**Skill:** `/infra-audit` (run in diagnostic/plan mode — no in-place fixes applied).
**Branch:** `dev`. **Owner:** engineer (infra/Terraform/Helm/CI).
**Date:** 2026-06-27. **Status:** Ready for review.

## Scope & method

Ran the `/infra-audit` checklist read-only against the OKE/Helm production path
(the compose VM is decommissioned — ADR-035 / phases.md). Every finding below is
verified against the file it references; sections with no finding are recorded as
**confirmed clean** so a reader need not re-run them.

## Confirmed clean (evidence)

- **Tracked secrets (§1):** `git ls-files` for `*.tfstate*`, `*.tfvars`, `*.pem`,
  `*.key`, `tfplan`, `.env` (non-example) → none tracked. Local
  `deploy/terraform/{terraform.tfstate,terraform.tfvars,apply.tfplan,backend.tfbackend}`
  exist on disk but are gitignored ([`deploy/terraform/.gitignore`](../../deploy/terraform/.gitignore)
  covers `*.tfstate*`, `terraform.tfvars`, `*.auto.tfvars`, `*.tfplan`,
  `backend.tfbackend`, `crash.*.log`).
- **TF identity-variable sensitivity (§2a):** `tenancy_ocid`, `user_ocid`,
  `fingerprint`, `private_key_path`, `compartment_ocid`, `ssh_allowed_cidr` all
  carry `sensitive = true`; `ssh_allowed_cidr` additionally validates against
  `0.0.0.0/0` ([`variables.tf`](../../deploy/terraform/variables.tf)).
- **Compose env hygiene (§3):** every `${VAR}` in
  [`deploy/docker-compose.yml`](../../deploy/docker-compose.yml) is documented in
  [`deploy/.env.example`](../../deploy/.env.example) (completeness diff returned
  zero undocumented vars); no inline literal secrets.
- **K8s workload hardening (§ deploy):** server + postgres + backup cronjob set
  `runAsNonRoot`, `runAsUser`, `seccompProfile`, `allowPrivilegeEscalation:false`,
  capability `drop`, and resource `requests`/`limits`
  ([`server-deployment.yaml`](../../deploy/helm/opengate/templates/server-deployment.yaml),
  [`postgres-statefulset.yaml`](../../deploy/helm/opengate/templates/postgres-statefulset.yaml)).
- **Ingress security headers (§8):** X-Content-Type-Options, X-Frame-Options,
  Referrer-Policy, CSP, Permissions-Policy, conditional HSTS all set
  ([`ingress.yaml`](../../deploy/helm/opengate/templates/ingress.yaml)).
- **Monitoring exposure (§10):** Loki/Promtail/Grafana ports are not host-published
  in compose; the Helm monitoring chart ships **no** ingress (matches ADR-035);
  production Grafana admin password is sourced from a Secret
  ([`monitoring/templates/grafana.yaml`](../../deploy/helm/monitoring/templates/grafana.yaml)).

## Findings

| # | Sev | Finding | Location | CI-caught? |
|---|-----|---------|----------|-----------|
| 1 | MEDIUM | `oke_cluster_id` output lacks `sensitive = true` though its own comment says it is stored as the `OKE_CLUSTER_ID` GitHub secret (consumed by `cd.yml`) | [`outputs.tf:26`](../../deploy/terraform/outputs.tf#L26) | No |
| 2 | LOW | No top-level default-deny `permissions:` block (job-level perms exist, so blast radius is bounded; a future job added without its own block inherits the repo default token) | [`build-image.yml`](../../.github/workflows/build-image.yml), [`release-agent.yml`](../../.github/workflows/release-agent.yml) | Partly (actionlint) |
| 3 | LOW | Postgres container omits `readOnlyRootFilesystem: true` (server container + backup cronjob set it; postgres can run read-only-root with writable mounts for the socket dir + temp) | [`postgres-statefulset.yaml`](../../deploy/helm/opengate/templates/postgres-statefulset.yaml) | No |
| 4 | LOW | Ingress security headers depend on `nginx.ingress.kubernetes.io/configuration-snippet`, which needs cluster-wide `allowSnippetAnnotations=true` — a known ingress-nginx annotation-injection attack surface | [`ingress.yaml:19`](../../deploy/helm/opengate/templates/ingress.yaml#L19) | No |
| 5 | LOW | Legacy `docker-compose.monitoring.yml` defaults `GF_SECURITY_ADMIN_PASSWORD` to `admin` (decommissioned VM path; production Helm path is correct) | [`docker-compose.monitoring.yml:43`](../../deploy/docker-compose.monitoring.yml#L43) | No |

PII/log-flow (`/infra-audit` §9/§11 — server `slog` redaction, `err.Error()`
client leakage) overlaps the backend sweep and is covered in the backend audit
plan (`audit-backend-security.md`) to avoid double-reporting.

## Remediation plan

### Phase A — quick wins (~1–2 hrs, config-only, no source/TDD impact)

1. **F1:** add `sensitive = true` to the `oke_cluster_id` output. Re-run
   `terraform -chdir=deploy/terraform validate` and confirm `make lint-k8s` /
   terraform tests stay green. *(Done-when: `terraform output oke_cluster_id`
   requires `-raw`; the value no longer renders in plan diffs.)*
2. **F2:** add a top-level `permissions: {}` (default-deny) to `build-image.yml`
   and `release-agent.yml`; keep the existing job-level grants. *(Done-when:
   `actionlint` green; both workflows still pass a dispatch run.)*
3. **F3:** set `readOnlyRootFilesystem: true` on the postgres container and add
   `emptyDir` mounts for `/tmp` + `/var/run/postgresql`. Render every overlay
   under `make lint-k8s`; smoke a `helm template` + a staging apply. *(Done-when:
   postgres starts read-only-root in staging.)*

### Phase B — ingress hardening (~half day, needs a staging validation pass)

4. **F4:** migrate the five `more_set_headers` lines off the per-ingress
   `configuration-snippet` to the ingress-nginx controller `add-headers`
   ConfigMap (or the `custom-headers` ConfigMap), then set
   `controller.allowSnippetAnnotations=false`. Verify headers still land via
   `curl -I https://<staging-host>`. *(Done-when: headers present with snippet
   annotations disabled cluster-wide.)*

### Phase C — legacy cleanup (~30 min)

5. **F5:** in `docker-compose.monitoring.yml`, make `GF_SECURITY_ADMIN_PASSWORD`
   required (`${GF_SECURITY_ADMIN_PASSWORD:?set a Grafana admin password}`)
   instead of defaulting to `admin`; document it in `.env.monitoring.example`.
   *(Or delete the file if the VM monitoring stack is fully retired — confirm
   with the owner first.)*

## File inventory

**Modify:** `deploy/terraform/outputs.tf`, `.github/workflows/build-image.yml`,
`.github/workflows/release-agent.yml`,
`deploy/helm/opengate/templates/postgres-statefulset.yaml`,
`deploy/helm/opengate/templates/ingress.yaml`,
`deploy/helm/opengate/values*.yaml` (ingress snippet flag),
`deploy/docker-compose.monitoring.yml`, `deploy/.env.monitoring.example`.

## Acceptance criteria

1. All five findings either fixed or explicitly accepted with a one-line rationale.
2. `make lint-k8s`, terraform `validate` + module tests, and `actionlint` green.
3. Staging apply confirms postgres read-only-root and ingress headers post-F4.

## Reviewer checklist

- [ ] `oke_cluster_id` marked sensitive; no other GitHub-secret-backed output is missed.
- [ ] Top-level `permissions: {}` added without removing needed job grants.
- [ ] Postgres still starts with a read-only root fs (writable mounts correct).
- [ ] Ingress headers verified present after dropping snippet annotations.
- [ ] No new literal secret introduced in any compose/helm/CI file.
