# CI/CD Determinism — A Refused Step Is Not a Green One

**Enforced by:**
[`scripts/tests/ci-cd-determinism.test.sh`](../../scripts/tests/ci-cd-determinism.test.sh)
and [`scripts/tests/alert-delivery.test.sh`](../../scripts/tests/alert-delivery.test.sh)
(gauntlet shell-tests step),
[`scripts/assert-cache-written.sh`](../../scripts/assert-cache-written.sh) and
[`deploy/scripts/monitoring-config-apply.sh`](../../deploy/scripts/monitoring-config-apply.sh)
(in the workflows themselves). **No bypass.**

A step whose work was **refused** must not report success. Same defect class as a
test that skips itself ([`tests-determinism.md`](tests-determinism.md)): the run
is green, the work never happened, and the only way to find out is to go and
look.

## The rule

### Read back what you wrote

A cache entry written under a key we chose is asserted to exist, through the
cache API, before the job that wrote it may pass. The warning text is not the
signal — it is a string in a log nobody reads, and it changes when the action
does. The key coming back out is the signal.

[`assert-cache-written.sh`](../../scripts/assert-cache-written.sh) fails on an
absent key **and** on a cache list it could not read: a guard that answers yes
when it cannot ask is the false green it exists to close.

Where the save happens in an action's post step, the read-back is a separate job
with a `needs:` on the one that wrote — a post step runs after every step of its
own job, so nothing inside that job can see the result.

### An artifact nobody wrote is not an artifact

`upload-artifact` answers an empty file set with a warning and a zero exit, so a
step that produced nothing is green and the gap surfaces later as an aggregate
that will not add up, naming neither what is missing nor why.

Every artifact something downstream reads sets `if-no-files-found: error`, and
the shard that produced it is where it fails. This matters most where the step
already swallows its tool's exit code on purpose — a surviving mutant is not a
build failure, which leaves the report as the only signal that any work
happened.

Not blanket policy: an upload whose files are genuinely optional — a fuzz crash
that usually does not exist — says `ignore` and means it.

### A check that asserts an absence proves it reached something first

A check written as an absence is satisfied by the absence of the whole
conversation. An empty body matches no pattern, and no status at all is not
`404`, so a request that resolved nowhere, was refused, or died in a TLS
handshake reports the boundary green. `curl` writes `000` for a transfer that
never happened.

So every absence-shaped check in
[`smoke-test.sh`](../../deploy/scripts/smoke-test.sh) asks `edge_answered`
first, and the target is named rather than assumed: an Ingress matches on a Host
header, which need not be a name any public resolver answers for, so the run is
handed the address its controller published alongside the scheme that edge
serves. [`smoke-test-edge.test.sh`](../../scripts/tests/smoke-test-edge.test.sh)
drives it against an edge that keeps the boundary, one that breaks it, and one
that is not there, and requires a different verdict from each.

The generalisation, because absence-shaped checks are common in a deploy gate —
*the port is closed*, *the header is gone*, *the path is not served*, *no secret
appears in the log*: every one passes against a target that was never contacted.
Whatever asserts the absence proves first that it was talking to something.

### Do not declare a write you cannot make

A workflow whose cache token cannot write declares no cache at all — not an
explicit cache action, not a toolchain cache, and not the cache half of a setup
action, whose save is refused just as quietly. Losing the restore is the price; a
permanently refused save that reports success is not a trade worth making.

[`cd.yml`](../../.github/workflows/cd.yml) is that workflow. What it needs to
know about the running deployment it reads off the cluster, and the binary it
would build cold it takes as an artifact from
[`build-image.yml`](../../.github/workflows/build-image.yml), whose token does
write.

### A tool a workflow builds is built from a graph somebody has tested

`cargo install <tool>` re-resolves that tool's entire dependency graph to latest
compatible versions on every run, so a workflow that installs one compiles
software nobody has compiled before. It fails as a compile error deep inside a
transitive crate, in a job whose subject is something else, and it is invisible
on a workstation where the tool was installed once.

Every `cargo install` in a workflow passes `--locked`, which uses the lockfile
the tool's own authors tested — the only build there is evidence about.

### A read is spelled as a read

`gh api` chooses its own request method: a read normally, and a write the moment
any `-f`, `-F`, `--field` or `--raw-field` is present. Those flags are also how a
read narrows what it asks for, so filtering a listing turns it into a write
against an address that only answers reads. Every such address answers `404 Not
Found`, which reads as *that workflow does not exist* rather than *you asked the
wrong way* — and the shapes built around these calls treat a run that cannot be
found as a reason to stand down quietly.

So every `gh api` passing a field flag states its method.

The generalisation: **where a tool infers a verb from the arguments, the verb is
written down.** An inferred verb is a decision nobody recorded, and it surfaces
as an error about the noun.

### An input a script refuses to run without is named where it is called

A script that documents an input as required, or refuses to start without one,
holds a contract with every workflow that calls it, and nothing reads that
contract. A name is checkable from the text alone.

The sweep reads the required inputs off each script — the `:?` refusals the shell
makes, and the `(required)` entries in its Environment header — and looks for
them in the calling job together with the workflow-level `env` that job inherits,
which is the scope a name can reach, since `$GITHUB_ENV` does not cross a job
boundary.

The generalisation: **a contract stated in one file and satisfied in another is
checked in neither unless something is made to read both.**

### A status a step branches on is read with errexit turned off

GitHub runs every `run:` block under `bash -e`. A step that wants to interpret an
exit code writes `cmd; rc=$?; if [ "$rc" -eq 2 ]; then …` — and under errexit a
non-zero `cmd` ends the step *at* `cmd`. The `rc=$?` never runs, the branch below
it is unreachable, and the step reports the failure it was written to interpret.
`set -uo pipefail` does not help: it turns two options on and none off.

A block that reads `$?` turns errexit off around the command and back on after:

```bash
set +e
some-check "$input"
rc=$?
set -e
```

The status may also be taken on the failing command's own line — `cmd || rc=$?` —
which errexit does not fire on, because a command on the left of `||` is tested
rather than run for its success.

The generalisation: **a fact the shell has already acted on is not a fact the
script can still read.** Errexit is a decision; a script that wants to make that
decision itself says so.

### An alert conditioned on a healthy run cannot report an unhealthy one

A send runs under `always()` and decides for itself, reading the job's own status
beside whatever flag it was watching. A condition that depends on an earlier step
having succeeded is unreachable on the night with the most to report.

The message names which of the three happened — a finding, a failure, or a
cancellation — because they call for different responses.

Where a job can die before its steps run, the alert is a **job**, not a step: a
job killed at its timeout may run no step at all. A separate job with `needs:`
and `if: always()` starts on a fresh runner whatever happened upstream, and it
reads `needs.<job>.result` rather than calling `failure()` — the result is a fact
about the job named, and it distinguishes a cancellation from a failure.

### A refused send is a failure

[`telegram-alert.sh`](../../scripts/telegram-alert.sh) is the only path to the
alert chat, and it fails on a refused token, a chat the bot is not in, a 200
carrying `ok:false`, an absent credential, and a request that reached nothing. A
send that returns success for a message nobody received is this file's subject
wearing different clothes.

It can fail safely because the verdict lives elsewhere: each of these workflows
carries its regression verdict in a gate job of its own. Where one job both
publishes the verdict and sends, something downstream reads that verdict off
`needs.<job>.result` — otherwise a refused send and a clean night are the same
colour.

### A configuration the cluster was never given is not a configuration

Alert rules, dashboards and scrape targets are rendered from this repository,
applied, and read back. The apply is not the guarantee: an apply that lands
nothing answers exactly like one that lands everything. Without the read-back a
rule can be added, pinned by a gate, reviewed, merged, and never exist.

The alert destination is provisioned from a file rather than through the API or
the user interface. Grafana's database is on an ephemeral volume, so anything
written the other two ways lasts until the next reschedule, and a nightly check
that notices it died is a repair loop standing in for configuration. The
trade-off is that a file-provisioned destination is read-only in the interface:
routing and silencing become changes to this repository.

And one nightly job sends a real message through the channel and fails when it
does not arrive, because everything above can hold and still reach nobody. That
job cannot alert through the channel it is testing, so the workflow's result is
the signal that survives.

### An exemption is re-earned

The list of workflows that may not cache is a statement about tokens, not about
places. A workflow that gains write scope loses the row and gains the cache, in
the same commit that proves it.

### A budget covers every term, including the one nothing counts

A pre-flight that projects a job's cost is only as good as the terms it carries,
and the term it leaves out is invisible precisely because nothing counts it. A
mutation shard's wall clock has a second term beside `mutants × per-mutant cost`:
gremlins gives every mutant a leash of the coverage run's elapsed time times a
coefficient, and a mutant that removes a loop's exit condition holds a worker for
all of it. Such a mutant is recorded as `TIMED OUT` — neither a kill nor a
survivor — so it moves no score and appears in no report field.

**A projection states every term of the cost, and a term that is a bound is
bounded where it is set.** The leash is declared per shard
([`mutation-shards.sh`](../../scripts/lib/mutation-shards.sh)) and added to the
projection; the per-mutant cost is measured over the mutants that *finish*; and
the coefficient in [`.gremlins.yaml`](../../server/.gremlins.yaml) is held by
[`mutation-workflow.test.sh`](../../scripts/tests/mutation-workflow.test.sh) to a
value whose leash fits in what a fully-spent shard has left of the cap.

The generalisation: wherever a gate answers *will this fit*, the answer is worth
no more than the slowest thing it forgot to add up.

## What the sweeps do

Each sweep demonstrates its defect first — running both shapes and requiring the
broken one to lose — so a guard that has stopped reproducing anything fails
rather than policing a non-problem. Each reads its subject whole, joining line
continuations and reading workflows as structure rather than as text, and counts
what it reached, so a sweep that matched nothing fails instead of passing.

## Scope

The read-back covers every cache write whose key we choose. A cache an action
computes and writes on its own — the container layer cache, the scanner databases
inside `trivy-action` and `setup-qemu-action` — declares nothing a static gate
can see and names no key a caller can assert. Those are outside what this rule
can hold, and saying so is part of the rule rather than a gap in it.

The alert sweep covers what a schedule runs, because a scheduled run is one
nobody is watching. `cd.yml` has no alert path and is deliberately outside it: a
deploy is triggered by someone already watching. Whether that remains true is a
separate question, recorded here rather than left as a gap.
