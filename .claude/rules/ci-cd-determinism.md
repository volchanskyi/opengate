# CI/CD Determinism — A Refused Step Is Not a Green One

**Enforced by:**
[`scripts/tests/ci-cd-determinism.test.sh`](../../scripts/tests/ci-cd-determinism.test.sh)
(gauntlet shell-tests step), each section bound to its checks by
[`scripts/tests/rule-check-binding.test.sh`](../../scripts/tests/rule-check-binding.test.sh),
and [`scripts/assert-cache-written.sh`](../../scripts/assert-cache-written.sh) (in the workflows
themselves). **No bypass.**

A step whose work was refused must not report success. Why each section has its shape:
[ADR-129](../../docs/adr/ADR-129-workflow-verdicts-and-inputs.md),
[ADR-086](../../docs/adr/ADR-086-deploy-reads-the-cluster.md) for caches,
[ADR-107](../../docs/adr/ADR-107-where-a-run-happens.md) for the staging check and
[ADR-123](../../docs/adr/ADR-123-alert-delivery.md) for environments.

## The rules

### Read back what you wrote

- A cache entry written under a key the workflow chooses is read back through the cache API
  before the job that wrote it may pass, by
  [`assert-cache-written.sh`](../../scripts/assert-cache-written.sh).
- Example: [`build-image.yml`](../../.github/workflows/build-image.yml) reads back the agent
  build's entry for both targets in a job of its own.

**Checks:** `cache-read-back`.

### Do not declare a write you cannot make

- A workflow whose cache token cannot write declares no cache at all — not an explicit cache
  action, not a toolchain cache, not the cache half of a setup action.
- [`cd.yml`](../../.github/workflows/cd.yml) is that workflow.

**Checks:** `cache-undeclared`.

### A mutation shard that wrote no report fails where it ran

- Every artifact upload in [`mutation.yml`](../../.github/workflows/mutation.yml) sets
  `if-no-files-found: error`.

**Checks:** `mutation-report`.

### A run proves its target is the build it tests

- Every job that takes the staging claim runs
  [`staging-version-check.sh`](../../scripts/staging-version-check.sh) in its next step, with a
  full checkout, and hands the check's `not_started` reason to its outputs for the alert.
- [`cd.yml`](../../.github/workflows/cd.yml) is the exception: it installs the build.

**Checks:** `staging-build`.

### A status a step branches on is read with errexit turned off

- A block that reads `$?` turns errexit off around the command and back on after:

  ```bash
  set +e
  some-check "$input"
  rc=$?
  set -e
  ```

- The status may instead be taken on the failing command's own line: `cmd || rc=$?`.
- The read sits on the line straight after the command it judges. A `cat`, `echo` or `printf`
  between them hands the read the print's own status.

**Checks:** `errexit-status`.

### A job names an environment only where a person approves it

- The only environments a workflow names are `staging` and `production`, in either form of
  `environment:`.

**Checks:** `approved-environment`.

### A nightly's cluster call is retried or says why it is not

- Each `kubectl exec` and `kubectl cp` in the nightlies goes through `kubectl_retry`
  ([`kubectl-retry.sh`](../../scripts/lib/kubectl-retry.sh)), or carries `Not retried` and its
  reason in the comment above it.

**Checks:** `cluster-retry`.

### A job below a skippable job states its own status, and only an alert or clean-up job outlives a cancel

- A job whose upstream, followed through `needs` to the top, holds a job with an `if:` of its own
  states a status function in its `if:`: `success()` where it runs only when every job above it
  succeeded, `!cancelled()` with the results it accepts where it runs past a skip or a failure.
- A job-level `always()` belongs to a job that sends the alert or cleans up — one whose name
  starts with "Clean up", "Remove", "Release" or "Tear". Every other job that runs past a failure
  states `!cancelled()`, so a cancel stops it.
- Example: `staging-fault-drills` in [`cd.yml`](../../.github/workflows/cd.yml) runs on
  `!cancelled() && needs.deploy-staging-k8s.result == 'success'`, so a skipped cluster job leaves
  the drills running.

**Checks:** `stated-status`, `outlives-cancel`.

## Scope

- The read-back covers every cache write whose key the workflow chooses. A cache an action
  computes and writes on its own — the container layer cache, the scanner databases inside
  `trivy-action` and `setup-qemu-action` — is outside it.
- The cluster-call sweep reads the nightly workflows and scripts its guard names.
