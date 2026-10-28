---
number: 129
title: A workflow reports only work that happened, and hands its scripts their inputs as values
---

# ADR-129 — A workflow reports only work that happened, and hands its scripts their inputs as values

## Context

GitHub calls a step green when its last command exits zero, and a job green when no step failed.
Five shapes in the workflows turned work that never happened into that green.

A `run:` block runs under `bash -e`, so a failing command ends the step before an `rc=$?` on the
next line reads it, and the branch meant for the failure is unreachable. A `cat` or `printf`
between the command and the read hands the read the print's own status, and a failing suite
reported green that way.

An `if:` that names no status function has `success()` prepended, and `success()` reads every job
upstream, not only the job's own `needs`. In [`cd.yml`](../../.github/workflows/cd.yml) the
staging drills needed the staging deploy, which ran past a skipped cluster job under `always()`.
The drills' implicit `success()` read that skipped cluster job and skipped them too, and
production accepted a skipped drill job as a pass. From 2026-10-22, when the cluster job began
running only when the cluster-wide pieces changed, every deploy but one promoted to production
with no drill; run 39936850693 did. The test over it asserted only that the name
`STAGING_FAULT_TESTS` appeared in the file.

A job whose condition holds `always()` keeps running after a cancel: in run 39913420184 the alert
job started 3 s after the run was cancelled. That is right for an alert, which reports the cancel,
and for a clean-up. For a deploy or a nightly's publish job it means a cancel does not stop it.

A mutation shard that wrote no report uploaded nothing, and the upload's default only warns.

A nightly's `kubectl exec` that loses its connection to the cluster fails the night with nothing
learned about the product.

Three more went wrong between a workflow and the scripts it runs.

A script refuses to run without an input — `${VAR:?}` where the value is used, or `(required)` in
its `Environment:` header — and often inside a second script the named one calls. A job that
never names the input fails on the night it first runs, and a reader of the named script sees no
requirement, because the wrapper holds none of its own.

GitHub writes a `${{ }}` expression into a `run:` script before the shell reads it, so a
dispatched input such as `x"; curl … | sh; "` becomes code on the runner. Four `run:` blocks
pasted dispatch inputs and event fields that way. The pinned `actionlint` flags
`github.event.pull_request.title` and `github.head_ref` in a `run:` block, and not `inputs.*` or
`github.event.before` (tested 2026-10-26).

`gh api` sends a POST whenever a field flag is passed, and a list endpoint answers a POST with
404, so a read written without its method fails as a missing resource.

## Decision

### What a workflow reports

**A status a step branches on is read with errexit turned off**, around the command or on the
command's own line (`cmd || rc=$?`), and on the line straight after the command it judges.

**A job below a job that can be skipped states the status it runs under.** It writes `success()`
where it runs only when everything above it succeeded, and `!cancelled()` with the results it
accepts where it runs past a skip. The staging drills run on
`!cancelled() && needs.deploy-staging-k8s.result == 'success'`, and production promotes past a
skipped drill job only while `STAGING_FAULT_TESTS` is not `true`; with the drills switched on,
only a passing drill job promotes.

**Only an alert or a clean-up job outlives a cancel.** Every other job that runs past a failure —
both deploys, and each nightly's publish, gate and curve jobs — states `!cancelled()`. A publish
job a cancel skipped is reported by the alert as cancelled before publishing.

**A mutation shard's upload fails on an empty file set** (`if-no-files-found: error`).

**A nightly's cluster call goes through the retry helper**
([`kubectl-retry.sh`](../../scripts/lib/kubectl-retry.sh)), or names in the comment above it why
a repeat would only repeat the fault (`Not retried`).

[`ci-cd-determinism.test.sh`](../../scripts/tests/ci-cd-determinism.test.sh) holds each of these
with a sweep that reads every workflow once, as structure, and shows its defect first;
[`fault-ci.test.sh`](../../scripts/tests/fault-ci.test.sh) evaluates the drill and deploy
conditions over each outcome the jobs above them can have.

### What a workflow hands its scripts

**An input a script refuses to run without is named in the job that calls it** — in the job's or
a step's `env:`, the workflow-level `env:`, or assigned in the job's own `run:` text. The
requirement belongs to the process: it covers every script the named one runs or sources, minus
what each sets for itself, and a path a script only prints is data. The scope is one job plus the
workflow-level `env:`, never jobs pooled, since one job naming an input does not hand it to
another.

**An input reaches a script as a value.** `inputs.*`, `github.event.*` and `github.head_ref`
reach a `run:` block through the step's `env:`. A value the script hands on is checked for its
shape first: [`cd.yml`](../../.github/workflows/cd.yml) refuses a tag that is not an image tag
before it reaches `$GITHUB_OUTPUT`.

**A read is spelled as a read.** A `gh api` passing a field flag states its method.

[`workflow-inputs.test.sh`](../../scripts/tests/workflow-inputs.test.sh) holds all three. It
reads every workflow and action once as structure and every tracked shell script once as text,
and shows each defect first against a tree built to hold it.

## Consequences

A job that runs past a skip spells out the results it accepts, so its `if:` is longer than
GitHub's default; the default reads jobs the condition never names.

A night cancelled by hand leaves no trend row: the publish job does not start, and the alert
names the cancel.

The sweep knows a clean-up job by its name — "Clean up", "Remove", "Release", "Tear", the words
the workflows' clean-up steps already use. Every clean-up today is a step under `always()` inside
the job it tidies.

A contract stated in one file and met in another is checked only where something reads both: a
requirement a composite action meets inside itself is outside the input sweep.

The paste sweep stays beside `actionlint`, since the pinned release misses dispatch inputs and
most event fields.
