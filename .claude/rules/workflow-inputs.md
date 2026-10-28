# Workflow Inputs — A Script Gets What It Needs, As a Value

**Enforced by:**
[`scripts/tests/workflow-inputs.test.sh`](../../scripts/tests/workflow-inputs.test.sh)
(gauntlet shell-tests step), each section bound to its checks by
[`scripts/tests/rule-check-binding.test.sh`](../../scripts/tests/rule-check-binding.test.sh).
**No bypass.**

What a workflow hands a script reaches it named in the calling job, as a value, by the method the
call states. Why: [ADR-129](../../docs/adr/ADR-129-workflow-verdicts-and-inputs.md).

## The rules

### An input a script refuses to run without is named where it is called

- A script's required inputs are its `:?` refusals, on a line of their own or inline where the
  value is used, and the `(required)` entries in its `Environment:` header.
- The job that calls it names each one: in its own or a step's `env:`, in the workflow-level
  `env:`, or assigned in its own `run:` text. Jobs are never pooled.
- The requirement covers every script the named one runs or sources, minus what that script sets
  for itself. A path a script only prints is data, not a call.

**Checks:** `required-input`.

### An input reaches a script as a value, never as its text

- A dispatch input, an event field and the pull request's head branch (`inputs.*`,
  `github.event.*`, `github.head_ref`) reach a `run:` block through the step's `env:` and are
  read as variables:

  ```yaml
  - name: Determine tag
    env:
      TAG: ${{ inputs.image_tag }}
    run: echo "$TAG"
  ```

- The same holds in every action under `.github/actions/`.

**Checks:** `input-as-value`.

### A read is spelled as a read

- Every `gh api` passing a field flag (`-f`, `-F`, `--field`, `--raw-field`) states its method:
  `gh api -X GET repos/o/r/actions/caches -f key=…`.

**Checks:** `read-spelled`.

## Scope

- The required-input sweep reads one job plus the workflow-level `env:`. A requirement a composite
  action meets inside itself is outside it.
- The `gh api` sweep reads every workflow and every tracked shell script.
