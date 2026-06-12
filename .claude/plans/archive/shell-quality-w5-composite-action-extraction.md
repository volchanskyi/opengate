# W5 — Composite-Action Shell Extraction

**Parent:** [`shell-quality-hardening.md`](shell-quality-hardening.md) · Commit 5
of 7.

## Goal

Move complex multiline shell out of local composite actions into adjacent `.sh`
files so the W4 gate (ShellCheck + shfmt + policy + tests) covers it. actionlint
parses workflow `run:` blocks but **not** composite-action `run:` blocks, so this
shell is currently entirely unlinted.

## Empirical targets (verified 2026-06-11)

Three composite actions exist under [`.github/actions/`](../../../.github/actions/):

| Action | `action.yml` size | Priority |
|---|---:|---|
| [`verify-oci-tfstate-creds`](../../../.github/actions/verify-oci-tfstate-creds/action.yml) | 247 lines | **First** — largest inline shell |
| [`oci-kube-setup`](../../../.github/actions/oci-kube-setup/action.yml) | 87 lines | Second |
| [`docker-hub-mirror`](../../../.github/actions/docker-hub-mirror/action.yml) | 48 lines | Evaluate; extract only if multiline logic remains |

## Extraction contract

- Multiline logic moves into `.github/actions/<name>/<name>.sh` (or a
  `scripts/` subdir if shared), classified per W3 (standalone fail-fast).
- The action passes inputs as **explicit environment variables or arguments** —
  never templated GitHub-expression source code. `${{ inputs.x }}` interpolation
  stays in `action.yml`; only validated values cross into the script.
- `action.yml` steps become short invocations (`run: ./<name>.sh` with `env:`).
- Each extracted script gets an offline command-stub test (no live OCI/kube).
- Existing action-metadata validation (the config gate / `make lint-deploy`
  Conftest `policy/github_actions`) still passes.

## File inventory

**New:**

- `.github/actions/verify-oci-tfstate-creds/verify-oci-tfstate-creds.sh`
- `.github/actions/oci-kube-setup/oci-kube-setup.sh`
- (`docker-hub-mirror` only if it retains real logic after review)
- `scripts/tests/verify-oci-tfstate-creds.test.sh`,
  `scripts/tests/oci-kube-setup.test.sh` — command-stub tests asserting the
  decision/branch logic with stubbed `oci`/`kubectl`/`curl` on `PATH`. **Fail
  before** the scripts exist.

**Modified:**

- The three `action.yml` files — replace inline `run:` bodies with short
  invocations + `env:` input wiring.

## Steps (TDD order)

1. Write the stub tests for the extracted scripts (failing) + `chmod +x`.
2. Extract `verify-oci-tfstate-creds` first (highest value), then
   `oci-kube-setup`; evaluate `docker-hub-mirror`.
3. Wire inputs as `env:`/args; keep `${{ }}` interpolation in YAML.
4. Run the extracted scripts through `make shell-check` (now gated by W4).
5. Validate every affected workflow + composite action (actionlint + the config
   gate). Confirm no execution-boundary regression (the action still receives
   and forwards inputs correctly).
6. `/precommit` → commit → `/refactor` → push; watch CI.

## Risk notes

- Extraction changes execution boundaries (a script process vs inline step).
  Confirm working-directory, env inheritance, and exit-code propagation match
  the prior inline behavior. The stub tests assert exit-code propagation.
- Do not interpolate secrets into the script source; pass via `env:` so they are
  not baked into committed/templated text.

## Out of scope

- The non-action behavioral tests (installer/bastion/deploy) — **W6**.

## Reviewer checklist

- [ ] `verify-oci-tfstate-creds` (and `oci-kube-setup`) inline shell is extracted
      to gated `.sh`; `action.yml` steps are short invocations.
- [ ] Inputs cross via `env:`/args, never templated source; `${{ }}` stays in
      YAML; no secret is interpolated into script text.
- [ ] Each extracted script has an offline stub test asserting branch logic and
      exit-code propagation; tests need no live OCI/kube/network.
- [ ] actionlint + the github_actions Conftest policy pass for every touched
      action.
- [ ] Extracted scripts are clean under `make shell-check`.
