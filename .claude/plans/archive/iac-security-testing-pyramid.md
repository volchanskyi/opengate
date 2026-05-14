# IaC Security Testing Pyramid — Defense-in-Depth

## Context

OpenGate's IaC base layer is solid (Trivy config, tflint, terraform fmt/validate, Cosign signing, SBOMs) but the pyramid is missing three layers a growing OCI-deployed project will need:

1. **Secrets scanning** — Trivy catches misconfigs, not credential leaks. No gitleaks/trufflehog.
2. **Policy-as-code** — No OPA/Conftest/Rego. No project-specific invariants (e.g. "compute must stay ARM64 + free-tier shape", "third-party GH Actions must be SHA-pinned").
3. **Broad static policy scanning** — Checkov covers ~750 built-in checks across TF/Dockerfile/Compose/GH Actions/cloud-init in one tool. `tfsec` was archived into Trivy in 2024 — `trivy config` is already the "tfsec slot." `dryrun.security` (SaaS) is out of scope; the user clarified "dryRun" = `terraform plan` dry-run gating in PRs.

The planned [terraform-test-and-remote-state.md](terraform-test-and-remote-state.md) (T1–T4) covers remote state, unit tests via `terraform test`, and nightly drift detection. **This plan sequences after T1–T4 lands** and adds the layers above it. Where overlap exists (Checkov vs Trivy on the Dockerfile, Checkov vs `terraform test` on SSH-CIDR), the user opted for defense-in-depth — keep both.

**Outcome:** every IaC change passes through 7 distinct gates before merge, plus a nightly drift check, with PR-time plan preview so destructive changes are visible.

## Target Pyramid (post-plan)

```
        ┌──────────────────────────────────────────────────┐
   L7   │ Drift detection — nightly refresh + alert        │  T4 (existing plan)
        └──────────────────────────────────────────────────┘
       ┌────────────────────────────────────────────────────┐
   L6  │ Plan preview — PR comment + destroy-blocklist gate │  S4 (this plan)
       └────────────────────────────────────────────────────┘
      ┌──────────────────────────────────────────────────────┐
   L5 │ Custom policy — Conftest + Rego (project invariants) │  S3 (this plan)
      └──────────────────────────────────────────────────────┘
     ┌────────────────────────────────────────────────────────┐
   L4│ Built-in policy — Checkov (~750 checks, 5 surfaces)    │  S2 (this plan)
     │ + Trivy config (existing, kept for overlap)            │
     └────────────────────────────────────────────────────────┘
    ┌──────────────────────────────────────────────────────────┐
   L3│ Unit tests — `terraform test` mock_provider invariants  │  T3 (existing plan)
    └──────────────────────────────────────────────────────────┘
   ┌────────────────────────────────────────────────────────────┐
   L2│ Secrets scanning — gitleaks (pre-commit + CI)             │  S1 (this plan)
   └────────────────────────────────────────────────────────────┘
  ┌──────────────────────────────────────────────────────────────┐
   L1│ Lint + validate — tflint, yamllint, actionlint, hadolint,│  Mostly existing;
    │ terraform fmt+validate, docker compose config, caddy      │  hadolint added in S2
    │ validate, validate-configs.sh cross-file checks            │
    └────────────────────────────────────────────────────────────┘
```

## Answers to the user's direct questions

| Question | Answer (today) | Answer (after this plan) |
|---|---|---|
| Pyramid shape? | L1, partial L4 (Trivy), supply chain — see "Current State" in conversation | Full 7-layer pyramid above |
| Policy-as-code used? | No (only `tflint` lint rules) | Yes — Conftest + 4 Rego policy bundles |
| Checkov? | No | Yes — TF + Dockerfile + Compose + GH Actions + cloud-init |
| tfsec? | No (archived into Trivy 2024; `trivy config` is the equivalent) | Still no separate tfsec; Trivy retained |
| dryRun (= `terraform plan` gating)? | No (CI runs `validate` only, never `plan`) | Yes — PR comment + destroy-blocklist gate |

## Plan: 4 PRs, sequenced after T1–T4

Each PR follows the project's mandate: TDD-first (failing fixture before scanner config), `/precommit` per PR, doc updates in-PR, no `NOSONAR`/`nolint` suppressions, commits on `dev`. PRs land in order — S2/S3/S4 each reuse infrastructure from the previous.

---

### **PR S1 — Secrets scanning (gitleaks)**

Pre-existing project state: Trivy config picks up *some* hardcoded secrets-in-config but is misconfig-shaped (severity-tuned for HIGH/CRITICAL). No purpose-built secrets scanner; no pre-commit hook; no git-history scan.

**Deliverables:**
- `/.gitleaks.toml` — config with project-specific allowlist (test fixtures, example values)
- `/.gitleaksignore` — known-safe finding suppressions (audited entries only)
- New Makefile target `secrets-scan` → `gitleaks detect --config .gitleaks.toml --no-banner --redact`
- CI job: new step in [.github/workflows/ci.yml](.github/workflows/ci.yml) `config-lint` job — runs `make secrets-scan` after `actionlint`
- Pre-commit integration: add `gitleaks protect --staged` invocation to the `/precommit` skill (read `.claude/skills/precommit/` first to see current shape; do not duplicate)
- TDD fixture: `deploy/tests/fixtures/leaked-secret.txt` (a deliberately-leaked dummy AWS key) committed in same PR with `.gitleaksignore` entry — proves the scanner is wired correctly without leaking a real secret
- Doc updates: [docs/Security-and-Dependencies.md](docs/Security-and-Dependencies.md) — new "Secrets scanning" section linking to `.gitleaks.toml`

**Critical files to read first:**
- [.github/workflows/ci.yml](.github/workflows/ci.yml) — `config-lint` job structure (lines ~620–717)
- [Makefile](Makefile) — existing `lint-deploy` target pattern (lines 41–60)
- `.claude/skills/precommit/SKILL.md` — pre-commit skill contract

**Verification:**
1. `make secrets-scan` exits 0 on clean tree
2. Introduce a fake `AKIA[A-Z0-9]{16}` literal in a non-test file → `make secrets-scan` exits 1
3. CI run on a PR with the same literal fails the `config-lint` job
4. `/precommit` rejects the commit locally before push

---

### **PR S2 — Checkov + Hadolint (built-in policy scanning, 5 surfaces)**

Defense-in-depth over Trivy: same Dockerfile/TF inputs, different rule engines. The user explicitly accepted the overlap.

**Deliverables:**
- `/.checkov.yaml` — config enabling frameworks: `terraform`, `dockerfile`, `docker_compose`, `github_actions`, `secrets`. Disable `secrets` framework (delegated to gitleaks in S1). Set `--quiet --compact`. Initial baseline via `--create-config` then committed.
- `/.checkov.baseline` — generated baseline file pinning current findings (zero, if clean); future runs fail only on *new* findings until each baseline entry is triaged
- New Makefile targets:
  - `iac-policy` → runs Checkov against all 5 frameworks
  - `iac-policy-fix` → runs Checkov with `--soft-fail` for triage (prints, doesn't fail)
- `hadolint` for Dockerfile: new Makefile target `lint-dockerfile` → `hadolint Dockerfile` (covers BIDI smuggling, layer ordering, pinning — orthogonal to Checkov's policy view)
- CI integration: extend the `config-lint` job in [.github/workflows/ci.yml](.github/workflows/ci.yml) — add `make iac-policy` and `make lint-dockerfile` steps (parallel-friendly via job matrix if runtime grows past ~2 min)
- TDD fixtures (failing-first):
  - `deploy/tests/fixtures/bad.tf` — committed alongside Checkov config, then deleted in same PR after proving Checkov flags it (use `terraform fmt -recursive` exclude pattern)
  - OR: introduce a known-bad fixture under a `--skip-path` and assert Checkov sees it via a smoke test
- Doc updates:
  - New ADR: `docs/adr/NNNN-iac-defense-in-depth.md` — records the *deliberate* Checkov-Trivy overlap, the framework list, baseline-as-suppression model, why Hadolint runs alongside Trivy
  - Add index row to `.claude/decisions.md`
  - Update [docs/Infrastructure.md](docs/Infrastructure.md) and [docs/CI-Pipeline.md](docs/CI-Pipeline.md) to link to ADR and Checkov config — no paraphrasing per `docs/README.md`

**Critical files to read first:**
- [docs/README.md](docs/README.md) — link-don't-paraphrase rule + ADR immutability
- `.claude/decisions.md` — to pick next ADR number
- Existing ADRs in [docs/adr/](docs/adr/) — for ADR file format
- [Dockerfile](Dockerfile) — to anticipate Hadolint findings (multi-stage Alpine; likely flags missing version pins on `apk add`)

**Verification:**
1. `make iac-policy` runs in <60s and exits 0 on clean tree
2. Introduce a known-bad pattern (e.g. `oci_core_security_list` ingress rule `source = "0.0.0.0/0"` for SSH) → Checkov flags it under CKV_OCI_* rule, exits 1
3. `make lint-dockerfile` exits 0; introducing `apk add curl` without version pin → exits 1 (DL3018)
4. CI fails on a deliberately-broken PR; passes after revert
5. Coverage check: Checkov scans all 5 declared frameworks (assert via `--list` or run report)

---

### **PR S3 — Conftest + Rego (project-specific custom policies)**

Built-in scanners can't encode OpenGate-specific invariants. This PR adds the 4 highest-value custom policies.

**Deliverables:**
- New `/policy/` directory at repo root (Conftest's default location):
  - `policy/terraform/compute.rego` — assert `oci_core_instance.shape == "VM.Standard.A1.Flex"`, OCPU ≤ 4, memory ≤ 24 GB (Always-Free invariants — overlaps T3's `terraform test`, intentional)
  - `policy/terraform/tags.rego` — assert every taggable OCI resource has `freeform_tags.env` and `freeform_tags.component`
  - `policy/docker_compose/images.rego` — reject services with `image:` ending in `:latest` or no tag; require digest pinning (`@sha256:`) for non-build services
  - `policy/github_actions/pinning.rego` — third-party actions (anything not under `actions/`, `github/`, `oracle-actions/`, `docker/`) must be pinned by 40-char SHA, not tag — closes a real supply-chain vector
- `policy/.conftest.yaml` — config: namespace `main`, parser overrides for HCL via `terraform show -json`, GitHub Actions YAML parser
- New Makefile target `iac-policy-custom`:
  ```makefile
  iac-policy-custom:
      terraform -chdir=deploy/terraform show -json terraform.tfplan.binary > /tmp/tfplan.json
      conftest test --policy policy/terraform /tmp/tfplan.json
      conftest test --policy policy/docker_compose deploy/docker-compose.yml deploy/docker-compose.staging.yml
      conftest test --policy policy/github_actions .github/workflows/*.yml
  ```
  (Plan-file generation requires T1's remote state — that's why S3 sequences after T1–T4)
- CI integration: extend `config-lint` job, run after Checkov
- TDD fixtures:
  - `policy/terraform/compute_test.rego` — Conftest's native test format; assert each rule fires on a synthetic bad input and passes on good input
  - Same for the other 3 policy bundles
- Doc updates: new section in [docs/Infrastructure.md](docs/Infrastructure.md) — "Custom IaC policies"; ADR optional (single policy bundle ≠ architectural decision, but mention rationale for each rule inline as a Rego doc comment)

**Critical files to read first:**
- [.github/workflows/](.github/workflows/) — enumerate third-party actions currently used → tune `pinning.rego` allowlist
- [deploy/docker-compose.yml](deploy/docker-compose.yml) — inventory existing image references → decide on digest-pin vs tag policy
- [deploy/terraform/main.tf](deploy/terraform/main.tf) — current tags? if none, S3 will fail on first run; either add tags in this PR or skip the rule until a separate "tagging" PR

**Verification:**
1. `conftest verify --policy policy/` passes (Rego unit tests green)
2. `make iac-policy-custom` exits 0 on clean tree
3. Bump a service in `docker-compose.yml` from `image: postgres:16.4` to `image: postgres:latest` → exits 1 with policy violation
4. Add a new action to a workflow with a tag instead of SHA → exits 1
5. CI fails on PR introducing any violation; passes after fix

---

### **PR S4 — `terraform plan` PR preview + destroy-blocklist gate**

The "dryRun" the user asked about: every PR that touches `deploy/terraform/**` posts a plan preview as a comment and *blocks merge* if the plan destroys protected resources unless an override label is present.

**Deliverables:**
- New workflow `.github/workflows/iac-plan-preview.yml`:
  - Trigger: `pull_request` with `paths: ['deploy/terraform/**']`
  - Auth: read-only OCI IAM user `tf-plan-reader` (created in T1 if not already; new role binding: `inspect all-resources` + `read object-family` on state bucket)
  - Steps: `terraform init` (remote state) → `terraform plan -out=tfplan.binary -no-color` → `terraform show -json tfplan.binary > tfplan.json` → custom parser script
- New script `deploy/scripts/parse-tfplan.sh` (or `.py` if jq gymnastics get ugly):
  - Reads `tfplan.json`
  - Emits human-readable summary (added/changed/destroyed counts + resource list)
  - Returns non-zero if any `destroy` action targets a resource type in the blocklist: `oci_core_security_list`, `oci_core_subnet`, `oci_core_vcn`, `oci_objectstorage_bucket`
  - Override: if PR has label `iac:approve-destroy`, blocklist is bypassed
- PR comment via `actions/github-script@<SHA>` or `peter-evans/create-or-update-comment@<SHA>` (SHA-pinned per S3 policy)
- TDD: fixture-based smoke test for `parse-tfplan.sh` — three canned plan JSON files (no-change / safe-add / destroy-security-list); each asserted via Bats or plain bash
- Doc updates: new section in [docs/CI-Pipeline.md](docs/CI-Pipeline.md) — "PR-time IaC plan preview"; update [docs/Infrastructure.md](docs/Infrastructure.md) to describe override label

**Critical files to read first:**
- terraform-test-and-remote-state.md T1 section — confirms bucket name, IAM user pattern, credential injection mechanism
- [.github/workflows/cd.yml](.github/workflows/cd.yml) — existing OCI auth pattern in workflows
- [.github/workflows/ci.yml](.github/workflows/ci.yml) — existing `pull_request` trigger conventions

**Verification:**
1. PR that adds a new `freeform_tag` to compute → plan preview comment shows 1 change, 0 destroy → workflow passes
2. PR that removes a security_list ingress rule → plan shows in-place update → workflow passes
3. PR that replaces a security_list (force-new) → plan shows 1 destroy + 1 create → workflow fails
4. Add `iac:approve-destroy` label, re-run → workflow passes with warning comment
5. PR that doesn't touch `deploy/terraform/**` → workflow does not run (path filter)

---

## Critical files for the whole rollout

These are read by every PR; list here so the executor doesn't re-discover them per PR:

- [CLAUDE.md](CLAUDE.md) — TDD mandate, `/precommit` gate, no NOSONAR, plans-location, branching rules
- [.github/workflows/ci.yml](.github/workflows/ci.yml) — `config-lint` job is the integration point
- [Makefile](Makefile) — `lint-deploy` is the per-surface scaffolding to mirror
- [docs/README.md](docs/README.md) — link-don't-paraphrase, ADRs immutable
- [.claude/decisions.md](.claude/decisions.md) — ADR index (S2 adds one entry)
- [.claude/phases.md](.claude/phases.md) — append a new phase entry after the plan lands
- [.claude/techdebt.md](.claude/techdebt.md) — close out any "no policy-as-code" entries
- [deploy/terraform/.tflint.hcl](deploy/terraform/.tflint.hcl) — coexists with Checkov; not replaced
- [deploy/tests/validate-configs.sh](deploy/tests/validate-configs.sh) — custom invariants stay; extend only if needed
- terraform-test-and-remote-state.md — sequencing prerequisite; S3/S4 depend on T1

## Verification plan (end-to-end)

After all 4 PRs land:

1. **Clean-tree green path**: `make lint-deploy && make secrets-scan && make iac-policy && make iac-policy-custom && make lint-dockerfile` all exit 0 under ~3 min total
2. **Pyramid trip wires** — for each layer L1–L6, deliberately introduce a violation and confirm the *correct* layer catches it (no cross-layer escapes):
   - L1: malformed HCL → `terraform validate` fails
   - L2: AWS key literal in a comment → gitleaks fails
   - L3: SSH CIDR `0.0.0.0/0` → `terraform test` fails (T3)
   - L4: missing Dockerfile USER instruction → Checkov fails (CKV_DOCKER_3)
   - L5: compute shape changed to `VM.Standard.E5.Flex` → Conftest fails
   - L6: PR removes the public subnet → plan-preview workflow fails
   - L7: someone clicks-deletes the VCN in OCI console → nightly drift workflow alerts (T4)
3. **CI runtime budget**: total `config-lint` job time stays under 5 min (it's currently ~90s; budget +3 min)
4. **Docs sync**: `docs/Infrastructure.md`, `docs/CI-Pipeline.md`, and the new ADR cross-link without paraphrasing tool versions or paths (verified by `/wiki-audit`)
5. **No suppressions**: grep the diff for `NOSONAR`, `nolint`, `checkov:skip`, `tfsec:ignore`, `eslint-disable*` — should be zero except in audited `.gitleaksignore` and `.checkov.baseline` entries

## Risks and tradeoffs accepted

- **Tool overlap (Checkov vs Trivy on Dockerfile, Checkov vs `terraform test` on SSH-CIDR)** — user explicitly chose defense-in-depth. Cost: CI runtime +30s, some duplicate findings. Benefit: each scanner catches edge cases the other misses (Checkov has 200+ Dockerfile rules; Trivy has 60).
- **Checkov OCI coverage is thinner than AWS/Azure** — Checkov has ~30 OCI checks vs ~400 AWS. The custom Rego policies in S3 fill the OCI-specific gap.
- **`terraform plan` PR preview needs OCI credentials in PR CI** — uses a read-only IAM user (`tf-plan-reader`), credentials stored as GH Actions secret, scoped to inspect-only. Lower risk than the existing `cd.yml` NSG mutation user.
- **Sequencing dependency on T1–T4** — S3 needs `terraform show -json` against a real plan (best with remote state in T1); S4 needs the same. If T1–T4 slips, S1/S2 can land independently; S3/S4 wait.
