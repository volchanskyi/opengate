# CI/CD Determinism — A Refused Step Is Not a Green One

**Enforced by:**
[`scripts/tests/ci-cd-determinism.test.sh`](../../scripts/tests/ci-cd-determinism.test.sh)
(gauntlet shell-tests step) and
[`scripts/assert-cache-written.sh`](../../scripts/assert-cache-written.sh)
(in the workflows themselves). **No bypass.**

A workflow step whose work was **refused** must not report success. This is the
same defect class as a test that skips itself
([`tests-determinism.md`](tests-determinism.md)) — the run is green, the work
never happened, and the only way anyone finds out is by going to look.

## What it cost

The deploy's cache token carries read scope only. Two saves were refused on
every run for two months, each printing a warning and exiting zero, each step
green: the deploy-state entry the pre-flight skip stood on, and a toolchain
cache added later that never once wrote. A skip that had been firing on 45.8%
of runs went to zero, and the agent cross-build paid a cold start every time.
Nothing in any run's conclusion said so.

## The rule

### Read back what you wrote

A cache entry written under a key we chose is asserted to exist, through the
cache API, before the job that wrote it is allowed to pass. The warning text is
not the signal — it is a string in a log nobody reads, and it changes when the
action does. The key coming back out is the signal.

[`assert-cache-written.sh`](../../scripts/assert-cache-written.sh) does the
asking. It fails on an absent key **and** on a cache list it could not read: a
guard that answers yes when it cannot ask is the false green it was written to
close.

Where the save happens in an action's post step, the read-back is a separate job
with a `needs:` on the one that wrote — a post step runs after every step of its
own job, so nothing inside that job can see the result.

### An artifact nobody wrote is not an artifact

The same reasoning covers what a job hands to the job after it. `upload-artifact`
answers an empty file set with a warning and a zero exit, so a step that produced
nothing is green and the gap surfaces later, somewhere else, as an aggregate that
will not add up — naming neither what is missing nor why.

Every artifact [`mutation.yml`](../../.github/workflows/mutation.yml) uploads is
an input to the aggregation that scores the night, so every one of its uploads
sets `if-no-files-found: error`. The shard is where the absence is known, so the
shard is where it fails. This matters most where the step already swallows its
tool's exit code on purpose — a surviving mutant is not a build failure, which
leaves the report coming back out as the only signal that any work happened.

The setting is not blanket policy: an upload whose files are genuinely optional —
a fuzz crash that usually does not exist — says `ignore` and means it. The rule
binds an artifact **something downstream reads**.

### Do not declare a write you cannot make

A workflow whose cache token cannot write declares no cache at all — not an
explicit cache action, not a toolchain cache, and not the cache half of a setup
action, whose save is refused just as quietly as any other. Losing the restore
alongside it is the price; a permanently refused save that reports success is
not a trade worth making.

[`cd.yml`](../../.github/workflows/cd.yml) is that workflow today. What it needs
to know about the running deployment it reads off the cluster, and the binary it
used to build cold it takes as an artifact from
[`build-image.yml`](../../.github/workflows/build-image.yml), whose token does
write. See [ADR-086](../../docs/adr/ADR-086-the-cluster-is-the-source-of-truth-for-what-is-deployed.md).

### An exemption is re-earned

The list of workflows that may not cache is a statement about tokens, not about
places. If a workflow gains write scope — a trusted trigger would do it — the
row comes out and the cache comes back, in the same commit that proves it.

## Scope

The read-back covers every cache write whose key we choose. A cache an action
computes and writes entirely on its own — the container layer cache, the scanner
databases inside `trivy-action` and `setup-qemu-action` — declares nothing a
static gate can see and names no key a caller can assert. Those are outside what
this rule can hold, and saying so is part of the rule rather than a gap in it.
