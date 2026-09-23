# CI/CD Determinism — A Refused Step Is Not a Green One

**Enforced by:**
[`scripts/tests/ci-cd-determinism.test.sh`](../../scripts/tests/ci-cd-determinism.test.sh)
and [`scripts/tests/alert-delivery.test.sh`](../../scripts/tests/alert-delivery.test.sh)
(gauntlet shell-tests step),
[`scripts/assert-cache-written.sh`](../../scripts/assert-cache-written.sh) and
[`deploy/scripts/monitoring-config-apply.sh`](../../deploy/scripts/monitoring-config-apply.sh)
(in the workflows themselves). **No bypass.**

A step whose work was refused must not report success.

## The rules

### Read back what you wrote

- A cache entry written under a key we choose is asserted to exist, through the
  cache API, before the job that wrote it may pass.
- [`assert-cache-written.sh`](../../scripts/assert-cache-written.sh) fails on an
  absent key and on a cache list it could not read.
- Where the save happens in an action's post step, the read-back is a separate
  job with a `needs:` on the one that wrote.

### An artifact nobody wrote is not an artifact

- Every artifact something downstream reads sets `if-no-files-found: error`, and
  fails in the shard that produced it.
- An upload whose files are genuinely optional sets `ignore`.

### A check that asserts an absence proves it reached something first

- Every absence-shaped check in
  [`smoke-test.sh`](../../deploy/scripts/smoke-test.sh) asks `edge_answered`
  first.
- The target is named rather than assumed: the run is handed the address its
  Ingress controller published, with the scheme that edge serves.
- Whatever asserts an absence proves first that it was talking to something.

### Do not declare a write you cannot make

- A workflow whose cache token cannot write declares no cache at all — not an
  explicit cache action, not a toolchain cache, not the cache half of a setup
  action.
- [`cd.yml`](../../.github/workflows/cd.yml) is that workflow. It reads the
  running deployment off the cluster and takes its binary as an artifact from
  [`build-image.yml`](../../.github/workflows/build-image.yml).

### A tool a workflow builds is built from a graph somebody has tested

- Every `cargo install` in a workflow passes `--locked`.

### A read is spelled as a read

- Every `gh api` passing a field flag (`-f`, `-F`, `--field`, `--raw-field`)
  states its method.
- Where a tool infers a verb from the arguments, the verb is written down.

### An input a script refuses to run without is named where it is called

- The sweep reads each script's required inputs — the `:?` refusals the shell
  makes, and the `(required)` entries in its Environment header — and requires
  them in the calling job plus the workflow-level `env` that job inherits.
- A refusal counts wherever it is written: on a line of its own, or inline at
  the point the value is used.
- A requirement belongs to the process, not to the path a workflow spells. The
  closure covers every script the named one runs or sources, minus what that
  script sets for itself.
- The scope is one job plus the workflow-level `env`. Jobs are never pooled.
- A contract stated in one file and satisfied in another is checked in neither
  unless something is made to read both.

### A status a step branches on is read with errexit turned off

- A block that reads `$?` turns errexit off around the command and back on
  after:

  ```bash
  set +e
  some-check "$input"
  rc=$?
  set -e
  ```

- The status may instead be taken on the failing command's own line:
  `cmd || rc=$?`.
- A fact the shell has already acted on is not a fact the script can still read.

### An alert conditioned on a healthy run cannot report an unhealthy one

- A send runs under `always()` and reads the job's own status beside whatever
  flag it was watching.
- The message names which of the three happened — a finding, a failure, or a
  cancellation.
- Where a job can die before its steps run, the alert is a **job** with `needs:`
  and `if: always()`, reading `needs.<job>.result` rather than calling
  `failure()`.

### A refused send is a failure

- [`telegram-alert.sh`](../../scripts/telegram-alert.sh) is the only path to the
  alert chat. It fails on a refused token, a chat the bot is not in, a 200
  carrying `ok:false`, an absent credential, and a request that reached nothing.
- Each workflow carries its regression verdict in a gate job of its own. Where
  one job both publishes the verdict and sends, something downstream reads that
  verdict off `needs.<job>.result`.

### A configuration the cluster was never given is not a configuration

- Alert rules, dashboards and scrape targets are rendered from this repository,
  applied, and read back.
- The alert destination is provisioned from a file, not through the API or the
  user interface. Routing and silencing are changes to this repository.
- One nightly job sends a real message through the channel and fails when it
  does not arrive. That job's own result is the signal.

### An exemption is re-earned

- The list of workflows that may not cache is a statement about tokens. A
  workflow that gains write scope loses the row and gains the cache, in the same
  commit that proves it.

### A budget covers every term, including the one nothing counts

- A projection states every term of the cost, and a term that is a bound is
  bounded where it is set.
- The mutation leash is declared per shard
  ([`mutation-shards.sh`](../../scripts/lib/mutation-shards.sh)) and added to the
  projection; per-mutant cost is measured over the mutants that finish; the
  coefficient in [`.gremlins.yaml`](../../server/.gremlins.yaml) is held by
  [`mutation-workflow.test.sh`](../../scripts/tests/mutation-workflow.test.sh) to
  a leash that fits in what a fully-spent shard has left of the cap.

## What the sweeps do

- Each sweep demonstrates its defect first, requiring the broken shape to lose.
- Each reads its subject whole, joining line continuations and reading workflows
  as structure rather than as text.
- Each counts what it reached and fails on a sweep that matched nothing.

## Scope

- The read-back covers every cache write whose key we choose. A cache an action
  computes and writes on its own — the container layer cache, the scanner
  databases inside `trivy-action` and `setup-qemu-action` — is outside it.
- The alert sweep covers what a schedule runs. `cd.yml` is outside it: a deploy
  is triggered by someone already watching.
