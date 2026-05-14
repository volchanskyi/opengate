# Terraform: Remote State + Module Decomposition + `terraform test` Invariants

## Context

Today's terraform setup ([deploy/terraform/](../../deploy/terraform/)):
- **Single flat module**, ~212 lines in [main.tf](../../deploy/terraform/main.tf) with [variables.tf](../../deploy/terraform/variables.tf) / [outputs.tf](../../deploy/terraform/outputs.tf), 7 resources + 2 data sources, low churn (~1 commit/month; last terraform-tracked change 2025-03-12)
- State lives **only** on the operator's laptop ([deploy/terraform/terraform.tfstate](../../deploy/terraform/terraform.tfstate), 28 KB; already gitignored at [deploy/terraform/.gitignore](../../deploy/terraform/.gitignore) line 2)
- CI runs `fmt -check` + `init -backend=false` + `validate` + `tflint` ([Makefile:38-48](../../Makefile#L38-L48)); no `plan`, no `test`
- CD ([.github/workflows/cd.yml](../../.github/workflows/cd.yml)) does **not** invoke terraform — provisioning is a one-shot manual op per [docs/Infrastructure.md:45-47](../../docs/Infrastructure.md#L45-L47); ongoing deploys mutate NSG rules at runtime ([commit bd80684](../../) "stale NSG rule cleanup")

User-confirmed concerns (priority order):
1. **tfstate loss / single-machine risk**
2. **HCL regression silently passes `validate`** (e.g. SSH opens to `0.0.0.0/0`)
3. **OCI drift from state**

User-confirmed constraints:
- Low churn expected (<1×/month)
- Existing OCI CD creds in GitHub Secrets are reusable, but **this plan does not require CI to hold OCI creds** — every test runs with `command = plan` and a mock provider
- **Module decomposition is in scope** (per user direction)

Selected approach: **Option B (remote state + `terraform test`) + module decomposition + Option C (scheduled drift detection)**. Four PRs T1–T4. Order: T1 → T2 → T3 → T4 — T4 has a hard dependency on T1 (CI cannot read laptop state).

## Why decompose now

The flat module is small today, but adding `terraform test` files multiplies the cost of staying flat:
- A single `tests/` directory testing a 310-line module produces tests that touch unrelated concerns (networking AND compute AND IAM all in one test run). Failures become harder to attribute.
- Mock-provider stubs grow if every test must satisfy every resource's required attributes, even those it doesn't care about.
- Future phase-13b multiserver work will introduce a second compute target; decomposing now means networking is reusable rather than duplicated.

Decomposition is also the cheapest invariant we can codify: "this module owns these concerns and nothing else." The structure itself becomes part of what tests assert.

Target layout:

```
deploy/terraform/
├── main.tf                # provider, backend, locals, module calls
├── variables.tf           # root inputs (compartment, OCI auth, SSH key, sizing)
├── outputs.tf             # re-exports submodule outputs
├── .terraform.lock.hcl    # committed; provider version pinning
├── modules/
│   ├── networking/
│   │   ├── main.tf        # VCN, IGW, route table, NSG, security list, subnet
│   │   ├── variables.tf
│   │   ├── outputs.tf     # vcn_id, subnet_id, nsg_id, security_list_id
│   │   └── tests/
│   │       └── security.tftest.hcl
│   └── compute/
│       ├── main.tf        # instance + image/AD data sources
│       ├── variables.tf
│       ├── outputs.tf     # instance_id, public_ip
│       └── tests/
│           └── free_tier.tftest.hcl
└── tests/
    └── integration.tftest.hcl  # root-level: composition + sensitivity
```

## What we are NOT doing (explicit non-goals)

- **PR-comment bots posting `plan` output** — would need real OCI creds in CI; defer
- **State locking via DynamoDB-equivalent** — OCI Object Storage S3 emulation does not support it; single-operator + occasional manual apply makes this acceptable
- **Migrating to HCP Terraform** — viable alternative, but keeps state inside HashiCorp's account; OCI Object Storage chosen for lower trust surface
- **Splitting state per submodule** — submodules share root state; we are decomposing for code organisation and test scoping, not for blast-radius isolation
- **Multi-environment (staging/prod) state separation** — current setup is single-environment. Revisit at phase-13b

## Concept ownership

| Aspect | Owning skill / file |
|---|---|
| Module-invariant tests in CI | `lint-deploy` job in [.github/workflows/ci.yml](../../.github/workflows/ci.yml); local `make lint-deploy` |
| Remote-state operator runbook | [docs/Infrastructure.md](../../docs/Infrastructure.md) |
| Submodule contract documentation | Per-submodule `README.md` (created in PR T2) |
| Pre-commit checklist coverage | [.claude/skills/precommit/SKILL.md](../../.claude/skills/precommit/SKILL.md) lint section |
| Post-deployment control-flow validation | [admin-infra-oci](file:///home/ivan/.claude/skills/admin-infra-oci/SKILL.md) — out of scope here |

## Rollout sequence

Three PRs. Order is deliberate:
1. **T1 first** — eliminate the laptop-SPOF before doing anything risky. This is the smallest, lowest-blast-radius change.
2. **T2 second** — decompose with `moved` blocks. This is structural-only, must produce a `plan` of "no changes" against existing state. Doing this after state migration means a single state file moves once and stays in the same backend.
3. **T3 last** — write the tests once the module structure is final, so tests don't need to be rewritten when boundaries shift.

### PR T1 — Migrate state to OCI Object Storage (S3 backend)

**Goal:** eliminate the laptop-SPOF for tfstate. No structural code changes, no CI changes.

**Pre-work (manual, one-time, by operator):**

1. In OCI Console (or via `oci-cli`) create:
   - Object Storage bucket `opengate-tfstate` in the same region as the rest of the infra (`us-sanjose-1` per [variables.tf:28](../../deploy/terraform/variables.tf#L28)). **Versioning ON**, **public access OFF**.
   - **Separate IAM user** `tf-state-writer` with a policy allowing only `manage object-family` on `opengate-tfstate` (least privilege — distinct from the deploy user).
   - Customer Secret Key for that user (S3-compatible access key + secret).
2. Store the secret key locally in `~/.oci/terraform-credentials` (AWS-style INI). Add to operator's password manager as backup. Do NOT commit.

**Code changes:**

- [deploy/terraform/main.tf](../../deploy/terraform/main.tf) — uncomment and fill the `backend "s3"` block (lines 1-15 already have the template). Use the actual namespace (`oci os ns get`) and region.
- [deploy/terraform/.gitignore](../../deploy/terraform/.gitignore) — already ignores `*.tfstate*`; verify it also ignores `.terraform/` (the provider cache). Decision: **commit** `.terraform.lock.hcl` (provider version pinning is valuable); ignore everything else.
- Root [.gitignore](../../.gitignore) — verify it does not contradict the scoped one in `deploy/terraform/`.
- [docs/Infrastructure.md](../../docs/Infrastructure.md) — new subsection "State backend":
  - Why state lives in OCI Object Storage (concern: tfstate loss)
  - Where credentials INI file lives and how to rotate it
  - Bootstrap recovery: `terraform init` against the backend with the credentials file is sufficient
  - Locking caveat: S3 emulation has no DynamoDB equivalent on OCI; single-operator constraint must hold
  - Backup: bucket versioning is the rollback mechanism; document how to restore a prior version

**Migration steps (operator runs once locally):**

1. `terraform -chdir=deploy/terraform init -migrate-state` — copies local state to bucket
2. Verify with `terraform -chdir=deploy/terraform state list` — list matches pre-migration
3. Verify with `terraform -chdir=deploy/terraform plan` — reports no resource changes
4. Move local `terraform.tfstate*` to a one-time offline encrypted backup, then delete from working tree
5. Commit and push

**Verification:**

- `terraform -chdir=deploy/terraform plan` reports no changes after migration
- `git ls-files deploy/terraform/ | grep -E 'tfstate|^\\.terraform'` returns nothing
- `make lint-deploy` still passes (the new `backend "s3"` block is skipped by `init -backend=false`)

**Rollback:**

- Keep offline tfstate backup until at least one successful plan/apply cycle against the remote backend confirms it works
- If bucket misconfigured, `terraform init -migrate-state` from the offline backup restores local state

### PR T2 — Decompose into `networking/` and `compute/` submodules

**Goal:** structure-only refactor. Plan against post-migration state must show **zero resource changes** — only `moved` blocks reconciling addresses.

**File operations:**

Create:
- `deploy/terraform/modules/networking/main.tf` — receives the 6 networking resources from the current root [main.tf](../../deploy/terraform/main.tf):
  - `oci_core_vcn.opengate`
  - `oci_core_internet_gateway.opengate`
  - `oci_core_route_table.opengate`
  - `oci_core_network_security_group.cd_deploy`
  - `oci_core_security_list.opengate`
  - `oci_core_subnet.opengate_public`
- `deploy/terraform/modules/networking/variables.tf` — inputs: `compartment_id`, `ssh_allowed_cidr`
- `deploy/terraform/modules/networking/outputs.tf` — exports: `vcn_id`, `subnet_id`, `nsg_id` (cd_deploy), `security_list_id`
- `deploy/terraform/modules/networking/README.md` — one-paragraph contract: what it owns, what it returns

- `deploy/terraform/modules/compute/main.tf` — receives:
  - `data "oci_identity_availability_domains" "ads"`
  - `data "oci_core_images" "ubuntu"`
  - `oci_core_instance.opengate`
- `deploy/terraform/modules/compute/variables.tf` — inputs: `compartment_id`, `tenancy_ocid` (for AD lookup), `subnet_id`, `nsg_ids`, `instance_shape`, `instance_ocpus`, `instance_memory_gb`, `boot_volume_gb`, `ssh_public_key_path`, `cloud_init_path`
- `deploy/terraform/modules/compute/outputs.tf` — exports: `instance_id`, `public_ip`
- `deploy/terraform/modules/compute/README.md`

Rewrite (root):
- [deploy/terraform/main.tf](../../deploy/terraform/main.tf) — keeps only `terraform { backend ... required_providers ... }`, `provider "oci"`, `locals { compartment_id }`, plus two `module` blocks calling the submodules. Includes `moved` blocks (see below).
- [deploy/terraform/outputs.tf](../../deploy/terraform/outputs.tf) — re-exports submodule outputs (`instance_public_ip`, `instance_id`, `vcn_id`, `subnet_id`, `cd_nsg_id`) preserving exact names and `sensitive` flags so consumers (`docs/Infrastructure.md`, `cd.yml`'s GitHub Secrets reference) keep working.
- [deploy/terraform/variables.tf](../../deploy/terraform/variables.tf) — unchanged.

**`moved` blocks** (in root `main.tf`) — required so the existing state file finds resources at their new addresses without destroy/recreate:

```hcl
moved { from = oci_core_vcn.opengate                    to = module.networking.oci_core_vcn.opengate }
moved { from = oci_core_internet_gateway.opengate       to = module.networking.oci_core_internet_gateway.opengate }
moved { from = oci_core_route_table.opengate            to = module.networking.oci_core_route_table.opengate }
moved { from = oci_core_network_security_group.cd_deploy to = module.networking.oci_core_network_security_group.cd_deploy }
moved { from = oci_core_security_list.opengate          to = module.networking.oci_core_security_list.opengate }
moved { from = oci_core_subnet.opengate_public          to = module.networking.oci_core_subnet.opengate_public }
moved { from = oci_core_instance.opengate               to = module.compute.oci_core_instance.opengate }
```

(Data sources do not need `moved` — they re-resolve at plan time.)

**Verification:**

- `terraform -chdir=deploy/terraform init` succeeds (downloads provider for new submodule sources)
- `terraform -chdir=deploy/terraform validate` passes
- `terraform -chdir=deploy/terraform plan` reports **zero resource changes**, only address relocations under `moved`. This is the load-bearing assertion of T2 — if `plan` shows any resource change, stop and investigate before applying.
- `make lint-deploy` passes (fmt/init/validate/tflint all run on the new structure)
- Operator runs `terraform apply` once to flush the `moved` reconciliation into state. State list should still show the same resource count, just at module-prefixed addresses.

**Rollback:**

- `git revert` the PR → next `plan` will show another set of `moved` reconciliations back to the flat addresses. State remains intact.

### PR T3 — Add `terraform test` invariants + CI wiring

**Goal:** catch HCL regressions before `apply`. No OCI API calls, no creds.

#### Test files

##### `modules/networking/tests/security.tftest.hcl`

Owns: security list shape and SSH-CIDR validation.

```hcl
mock_provider "oci" {}

variables {
  compartment_id   = "ocid1.compartment.oc1..fake"
  ssh_allowed_cidr = "203.0.113.42/32"  # RFC 5737 documentation range
}

run "ssh_must_not_be_open_to_world" {
  command = plan
  assert {
    condition = alltrue([
      for r in oci_core_security_list.opengate.ingress_security_rules :
      !(length(r.tcp_options) > 0 && r.tcp_options[0].min == 22 && r.source == "0.0.0.0/0")
    ])
    error_message = "SSH port 22 must not be open to 0.0.0.0/0 — pin to operator CIDR via var.ssh_allowed_cidr"
  }
}

run "ssh_cidr_input_validation" {
  command = plan
  variables { ssh_allowed_cidr = "0.0.0.0/0" }
  expect_failures = [var.ssh_allowed_cidr]
  # Requires the validation block on var.ssh_allowed_cidr added in this PR.
}

run "expected_public_ports_only" {
  command = plan
  assert {
    condition = setequal(
      [for r in oci_core_security_list.opengate.ingress_security_rules :
        r.tcp_options[0].min if r.protocol == "6" && r.source == "0.0.0.0/0" && length(r.tcp_options) > 0],
      [80, 443, 4433]
    )
    error_message = "Public TCP ingress must be exactly {80, 443, 4433}; SSH is operator-only"
  }
  assert {
    condition = setequal(
      [for r in oci_core_security_list.opengate.ingress_security_rules :
        r.udp_options[0].min if r.protocol == "17" && r.source == "0.0.0.0/0" && length(r.udp_options) > 0],
      [443, 9090]
    )
    error_message = "Public UDP ingress must be exactly {443 HTTP/3, 9090 QUIC}"
  }
}

run "egress_is_unrestricted_but_stateful" {
  command = plan
  assert {
    condition = alltrue([for r in oci_core_security_list.opengate.egress_security_rules : r.stateless == false])
    error_message = "Egress rules must be stateful (stateless = false) for return-traffic compatibility"
  }
}
```

##### `modules/compute/tests/free_tier.tftest.hcl`

Owns: Always-Free shape and sizing limits.

```hcl
mock_provider "oci" {}

variables {
  compartment_id      = "ocid1.compartment.oc1..fake"
  tenancy_ocid        = "ocid1.tenancy.oc1..fake"
  subnet_id           = "ocid1.subnet.oc1..fake"
  nsg_ids             = ["ocid1.nsg.oc1..fake"]
  ssh_public_key_path = "/dev/null"
  cloud_init_path     = "/dev/null"
}

run "free_tier_shape" {
  command = plan
  assert {
    condition     = oci_core_instance.opengate.shape == "VM.Standard.A1.Flex"
    error_message = "Instance shape must remain VM.Standard.A1.Flex (Always Free ARM64). Override only with explicit cost approval."
  }
}

run "free_tier_compute_limits" {
  command = plan
  assert {
    condition     = oci_core_instance.opengate.shape_config[0].ocpus <= 4
    error_message = "Always Free A1.Flex tenant cap is 4 OCPUs total"
  }
  assert {
    condition     = oci_core_instance.opengate.shape_config[0].memory_in_gbs <= 24
    error_message = "Always Free A1.Flex tenant cap is 24 GB memory total"
  }
}

run "free_tier_boot_volume_limit" {
  command = plan
  assert {
    condition     = oci_core_instance.opengate.source_details[0].boot_volume_size_in_gbs <= 200
    error_message = "Always Free boot volume cap is 200 GB"
  }
}
```

##### `tests/integration.tftest.hcl` (root)

Owns: composition (modules wire together) and output sensitivity.

```hcl
mock_provider "oci" {}

variables {
  tenancy_ocid     = "ocid1.tenancy.oc1..fake"
  user_ocid        = "ocid1.user.oc1..fake"
  fingerprint      = "aa:bb:cc:dd:ee:ff:00:11:22:33:44:55:66:77:88:99"
  private_key_path = "/dev/null"
  ssh_allowed_cidr = "203.0.113.42/32"
  ssh_public_key_path = "/dev/null"
}

run "subnet_uses_security_list_from_networking" {
  command = plan
  assert {
    condition     = length(module.networking.oci_core_subnet.opengate_public.security_list_ids) == 1
    error_message = "Subnet must reference exactly the networking module's security list"
  }
}

run "instance_attached_to_cd_nsg" {
  command = plan
  assert {
    condition     = length(module.compute.oci_core_instance.opengate.create_vnic_details[0].nsg_ids) == 1
    error_message = "Instance VNIC must attach the cd_deploy NSG so cd.yml can mutate ingress rules at runtime"
  }
}
```

> Implementation note: output-sensitivity assertions are awkward to express in `tftest.hcl` — `terraform test` cannot directly read the `sensitive` flag. We enforce sensitivity instead with a grep-based check in `make lint-deploy` (cheaper, deterministic):
> ```bash
> # Each of these outputs must be marked sensitive in deploy/terraform/outputs.tf
> for out in instance_id cd_nsg_id; do
>   grep -A3 "output \"$out\"" deploy/terraform/outputs.tf | grep -q "sensitive *= *true" \
>     || { echo "ERROR: output \"$out\" must have sensitive = true"; exit 1; }
> done
> ```

#### Variable validation block

Add to [deploy/terraform/variables.tf](../../deploy/terraform/variables.tf) for `ssh_allowed_cidr` (also propagate into `modules/networking/variables.tf`):

```hcl
validation {
  condition     = var.ssh_allowed_cidr != "0.0.0.0/0"
  error_message = "ssh_allowed_cidr must be a specific operator CIDR, never 0.0.0.0/0."
}
```

This is the actual gate; the corresponding `tftest` run just exercises it.

#### Tooling

- [Makefile](../../Makefile) — new target:
  ```makefile
  terraform-test:
  	terraform -chdir=deploy/terraform init -backend=false -input=false >/dev/null
  	terraform -chdir=deploy/terraform test
  ```
  Wire into `lint-deploy`. Add `terraform-test` to the `.PHONY` list.
- Bump `required_version` in root `main.tf` from `>= 1.5` to `>= 1.6.0` (`terraform test` requires 1.6+; `expect_failures` for variable validation requires 1.7+, so practically pin `>= 1.7.0`).
- [.terraform-version](../../deploy/terraform/.terraform-version) (new, optional) — pin local terraform version for tfenv users.

#### CI

- [.github/workflows/ci.yml](../../.github/workflows/ci.yml) — `setup-terraform` action: pin `terraform_version: 1.9.x` (or current LTS-equivalent) so `terraform test` is available in the runner. Verify the `config-lint` job already runs `make lint-deploy`; if so, no further wiring needed — `terraform-test` runs through `lint-deploy`. Otherwise, add a discrete `terraform-test` job step.

#### Pre-commit skill update

- [.claude/skills/precommit/SKILL.md](../../.claude/skills/precommit/SKILL.md) — under step 7 (`make lint-deploy`), append a bullet: `terraform -chdir=deploy/terraform test — module-invariant assertions (security list shape, free-tier limits, output sensitivity grep)`.

#### Verification

- `make terraform-test` exits 0 locally with all runs passing
- Deliberately break each invariant in a feature branch (e.g. add `0.0.0.0/0` SSH rule, change shape to `VM.Standard.E4.Flex`, raise boot volume to 300 GB) → confirm the corresponding `run` fails with the documented error message
- Revert. Push to dev → CI `config-lint` job stays green
- Confirm none of the test runs make real OCI API calls (no creds in CI; `mock_provider` is sufficient)

### PR T4 — Scheduled drift detection (Option C)

**Goal:** catch out-of-band changes to OCI resources — operator clicks in the console, NSG runtime mutation by `cd.yml`, manual security-list edits — by running `terraform plan -refresh-only -detailed-exitcode` nightly. Exit code 2 (drift detected) fires a Telegram alert, pushes a record to Loki, and exits the workflow red for audit-trail visibility. Same pattern as [.github/workflows/mutation.yml](../../.github/workflows/mutation.yml).

**Dependencies:** hard dependency on T1 (CI must read state from the bucket). Soft dependency on T2/T3 so drift output describes module-prefixed addresses and includes the variable validation. Ship last.

**Pre-work (manual, one-time, by operator):**

1. In OCI Console (or via `oci-cli`):
   - Create group `tf-drift-readers`.
   - Create user `tf-drift-reader`; add to the group; generate API signing key pair (save the fingerprint + `.pem`).
   - Policies (least privilege):
     - `Allow group tf-drift-readers to inspect all-resources in compartment opengate`
     - `Allow group tf-drift-readers to read object-family in bucket opengate-tfstate`
2. Add repo-level GitHub Secrets:
   - `OCI_DRIFT_USER_OCID`
   - `OCI_DRIFT_FINGERPRINT`
   - `OCI_DRIFT_PRIVATE_KEY` (PEM contents; multiline secret)
3. Confirm `TFSTATE_S3_ACCESS_KEY` and `TFSTATE_S3_SECRET_KEY` (from T1) exist — the drift workflow reuses these. `OCI_TENANCY_OCID` and `OCI_REGION` are reused from existing CD secrets.

**File operations:**

Create:
- `.github/workflows/terraform-drift.yml` — scheduled workflow (template below).
- `scripts/terraform-drift-summarize.sh` — parse `terraform show -json drift.tfplan` → canonical drift record `{timestamp, run_id, resource_changes: [{address, action, type}], summary}`. Mirrors [scripts/mutation-summarize.sh](../../scripts/mutation-summarize.sh) style.
- `scripts/terraform-drift-loki-push.sh` — push the summary record to Loki via SSH+docker (mirrors [scripts/mutation-loki-push.sh](../../scripts/mutation-loki-push.sh) verbatim; swap the stream label to `{app="opengate", source="terraform-drift"}`).

Modify:
- [Makefile](../../Makefile) — new target `terraform-drift` (local mirror; runs the same `plan -refresh-only -detailed-exitcode` against the operator's local OCI creds; useful for ad-hoc validation before the nightly).
- [docs/Infrastructure.md](../../docs/Infrastructure.md) — new subsection "Drift detection" explaining:
  - what the workflow does, when it runs (nightly 03:00 UTC), what alerts mean, how to respond
  - the `tf-drift-reader` IAM user (distinct from `tf-state-writer` (T1) and the CD-deploy user)
  - the workflow has no write perms — drift is **investigated by the operator**, never auto-corrected
  - cd.yml's runtime NSG injection: if it surfaces as drift every night, add `ignore_changes = [ingress_security_rules]` on the `cd_deploy` NSG resource. Document as a known interaction; decide post-soak.

Not modified:
- [.claude/skills/precommit/SKILL.md](../../.claude/skills/precommit/SKILL.md) — terraform-drift is a scheduled CI concern, not a precommit gate.

**Workflow template (`.github/workflows/terraform-drift.yml`):**

```yaml
name: terraform-drift

on:
  schedule:
    - cron: '0 3 * * *'   # nightly 03:00 UTC
  workflow_dispatch: {}

concurrency:
  group: terraform-drift
  cancel-in-progress: false

permissions:
  contents: read

jobs:
  drift:
    runs-on: ubuntu-latest
    timeout-minutes: 15
    steps:
      - uses: actions/checkout@v4

      - uses: hashicorp/setup-terraform@v3
        with:
          terraform_version: 1.9.x

      - name: Write OCI config
        run: |
          mkdir -p ~/.oci
          printf '%s' "${{ secrets.OCI_DRIFT_PRIVATE_KEY }}" > ~/.oci/drift.pem
          chmod 600 ~/.oci/drift.pem
          cat > ~/.oci/config <<EOF
          [DEFAULT]
          tenancy=${{ secrets.OCI_TENANCY_OCID }}
          user=${{ secrets.OCI_DRIFT_USER_OCID }}
          fingerprint=${{ secrets.OCI_DRIFT_FINGERPRINT }}
          region=${{ secrets.OCI_REGION }}
          key_file=~/.oci/drift.pem
          EOF

      - name: Terraform init (S3 backend)
        env:
          AWS_ACCESS_KEY_ID:     ${{ secrets.TFSTATE_S3_ACCESS_KEY }}
          AWS_SECRET_ACCESS_KEY: ${{ secrets.TFSTATE_S3_SECRET_KEY }}
        run: terraform -chdir=deploy/terraform init -input=false

      - name: Terraform plan (refresh-only, detailed exit)
        id: plan
        env:
          AWS_ACCESS_KEY_ID:     ${{ secrets.TFSTATE_S3_ACCESS_KEY }}
          AWS_SECRET_ACCESS_KEY: ${{ secrets.TFSTATE_S3_SECRET_KEY }}
        run: |
          set +e
          terraform -chdir=deploy/terraform plan \
            -refresh-only -detailed-exitcode \
            -out=drift.tfplan 2>&1 | tee drift.txt
          ec=${PIPESTATUS[0]}
          echo "exitcode=$ec" >> "$GITHUB_OUTPUT"
          # 0 = no drift, 2 = drift, anything else = error
          if [ "$ec" != "0" ] && [ "$ec" != "2" ]; then exit "$ec"; fi

      - name: Drift summary JSON
        if: steps.plan.outputs.exitcode == '2'
        run: |
          terraform -chdir=deploy/terraform show -json drift.tfplan > drift.json
          ./scripts/terraform-drift-summarize.sh drift.json > drift-summary.json

      - name: Telegram alert
        if: steps.plan.outputs.exitcode == '2'
        env:
          BOT:  ${{ secrets.DEPLOY_TELEGRAM_BOT_TOKEN }}
          CHAT: ${{ secrets.DEPLOY_TELEGRAM_CHAT_ID }}
          RUN:  ${{ github.run_id }}
        run: |
          MSG="$(head -c 3500 drift.txt)"
          curl -sS --fail --max-time 30 \
            -X POST "https://api.telegram.org/bot${BOT}/sendMessage" \
            --data-urlencode "chat_id=${CHAT}" \
            --data-urlencode "text=⚠️ Terraform drift detected on dev (run ${RUN}):\n\n${MSG}" \
            --data-urlencode "disable_web_page_preview=true"

      - name: Loki push
        if: steps.plan.outputs.exitcode == '2'
        env:
          DEPLOY_SSH_PRIVATE_KEY: ${{ secrets.DEPLOY_SSH_PRIVATE_KEY }}
          DEPLOY_SSH_HOST:        ${{ secrets.DEPLOY_SSH_HOST }}
        run: ./scripts/terraform-drift-loki-push.sh drift-summary.json

      - name: Fail workflow on drift (audit trail)
        if: steps.plan.outputs.exitcode == '2'
        run: exit 1
```

**Grafana:**

Add to the existing monitoring dashboard (or a new `opengate-terraform-drift` board):
- Stat: **days since last drift event** (Loki query counter)
- Time series: **drift events per week** (rolling 90-day window)
- Table: **most-recent drift summary** (resource address, action, run ID, timestamp)

Provisioned via the Grafana-as-code setup under [deploy/grafana/](../../deploy/grafana/) if applicable; otherwise a one-shot dashboard JSON committed alongside the workflow.

**Verification:**

- `make terraform-drift` locally on a clean state → exit 0, no plan changes.
- `workflow_dispatch` once after T4 lands → green, no alert.
- Mutate one OCI resource via console (e.g. bump instance memory by 1 GB) → next run → exit 2, Telegram fires, Loki record appears, workflow exits red.
- Revert the OCI mutation → next run is clean again.
- Verify `tf-drift-reader` cannot WRITE: attempt `terraform apply` with those credentials → should fail at the state-write or resource-create step.

## Phases.md update

One row added under Completed after the fourth PR lands (or four rows if landed separately):

| Terraform: state + decomposition + tftest + drift | Migrated tfstate to OCI Object Storage S3 backend (laptop-SPOF eliminated, bucket versioning for rollback). Decomposed flat module into `networking/` (VCN/IGW/RT/NSG/SL/Subnet) and `compute/` (instance + image lookup) submodules with `moved` blocks for zero-change state migration. Added `terraform test` invariants enforcing security-list shape (no SSH to world, public ports = {80,443,4433,UDP 443/9090}), Always Free shape/sizing limits, and inter-module wiring. Added `var.ssh_allowed_cidr` validation rejecting `0.0.0.0/0`. Output-sensitivity enforced via grep in `lint-deploy`. CI wires `terraform test` into `lint-deploy` via `make terraform-test`. No CI creds required for T1–T3 (mock_provider). T4 adds `.github/workflows/terraform-drift.yml` running nightly `0 3 * * *`: `terraform plan -refresh-only -detailed-exitcode` against the remote backend, authenticated as a separate read-only IAM user `tf-drift-reader` (inspect-only on compartment + read-only on tfstate bucket; new `OCI_DRIFT_*` secrets); on drift the workflow posts to Telegram (existing `DEPLOY_TELEGRAM_BOT_TOKEN`/`DEPLOY_TELEGRAM_CHAT_ID`), pushes a summary record to Loki via SSH+docker (mirroring `mutation-loki-push.sh`), and exits red for audit-trail visibility. No auto-remediation. | — | [terraform-test-and-remote-state.md](plans/terraform-test-and-remote-state.md) |

## Risk register

- **Migration failure corrupts state (T1).** Mitigated by keeping offline encrypted backup of pre-migration tfstate until two successful plan/apply cycles confirm the remote backend.
- **Decomposition `plan` shows resource changes (T2).** This is a stop-the-line condition — never apply if `plan` shows anything beyond `moved` reconciliations. If a resource change appears, the address mapping is wrong; fix the `moved` block before continuing.
- **`mock_provider` semantics evolve** in recent terraform versions; tests against the resource schema today may need updates when the OCI provider updates. Pin `oracle/oci` in `.terraform.lock.hcl` and bump deliberately.
- **`expect_failures` for variable validation requires Terraform 1.7+.** Confirm CI version supports it; otherwise the `ssh_cidr_input_validation` run becomes a separate `validate`-time check.
- **Sensitivity assertions are fragile in `tftest.hcl`.** Resolved up-front: enforce via grep in `lint-deploy` instead.
- **`cd.yml` references output names like `cd_nsg_id`.** Re-exports in root `outputs.tf` must preserve exact names and `sensitive` flags. Verify by grepping `cd.yml` and any deploy script for output names before committing T2.
- **Bucket region mismatch.** If operator typos the region in the backend block, `init` fails. Caught at migration time; not a runtime risk.
- **(T4) `cd.yml` runtime NSG injection looks like drift.** `cd.yml` mutates `cd_deploy` NSG ingress rules at deploy time (per `bd80684` "stale NSG rule cleanup"). If the nightly drift workflow alerts on this every night, options: (a) `ignore_changes = [ingress_security_rules]` on `oci_core_network_security_group.cd_deploy` (loses tfstate tracking but stops alerting), (b) split ingress rules into separate `oci_core_network_security_group_security_rule` resources and `ignore_changes` on those. Decide after one week of soak.
- **(T4) Transient OCI API errors mislabeled as drift.** `-refresh-only -detailed-exitcode` returns 1 for API errors, 2 only for actual state divergence. The workflow's exit-code branch (`if ec != 0 && ec != 2: exit ec`) prevents misclassification; verify on first error in the wild.
- **(T4) `tf-drift-reader` permissions creep.** The user has only `inspect all-resources` + `read object-family on bucket`. Quarterly audit that the policy document hasn't been broadened.
- **(T4) Telegram message size.** Plan output can exceed Telegram's 4 KB limit. Workflow truncates to 3500 chars; full output remains in workflow artifacts. Loki record holds the JSON summary, not the raw plan text.
- **(T4) Concurrent runs.** `concurrency.group` plus `cancel-in-progress: false` lets a running scan finish (typical run is <2 min; no need to cancel).

## Out of scope (revisit later)

- **PR-time `plan` comment bot** — defer; needs real backend creds in CI
- **Multi-environment state separation** — revisit at phase-13b multiserver work, when a second deployment target appears
- **Module reuse across environments** — submodules now have clean contracts, so this becomes possible later without further refactor
