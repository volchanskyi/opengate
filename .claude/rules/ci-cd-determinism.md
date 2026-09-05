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

### A check that asserts an absence proves it reached something first

The two rules above are about a step that produced nothing. This one is about a
step that *read* nothing, which is harder to see, because a check written as an
absence is satisfied by the absence of the whole conversation.

The smoke run through the public edge asserts that the exposition and the
profiler are not what the edge answers with. An empty body matches neither
pattern, and no status at all is not `404` — so a request that resolved nowhere,
was refused, or died in a TLS handshake reports the boundary green. `curl` says
so plainly, writing `000` for a transfer that never happened, and nothing was
reading it.

So every absence-shaped check in
[`smoke-test.sh`](../../deploy/scripts/smoke-test.sh) asks `edge_answered` first,
and the target it is pointed at is named rather than assumed: an Ingress matches
on a Host header, which need not be a name any public resolver answers for, so
the run is handed the address its controller published alongside the scheme that
edge actually serves. [`smoke-test-edge.test.sh`](../../scripts/tests/smoke-test-edge.test.sh)
drives the script against a stub edge that keeps the boundary, one that breaks
it, and one that is not there at all, and requires a different verdict from each.

The generalisation is worth stating, because absence-shaped checks are common in
a deploy gate: *the port is closed*, *the header is gone*, *the path is not
served*, *no secret appears in the log*. Every one of them passes on a target
that was never contacted. Whatever asserts the absence has to first prove it was
talking to something.

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

### A tool a workflow builds is built from a graph somebody has tested

`cargo install <tool>` re-resolves that tool's entire dependency graph to
"latest compatible versions" every time it runs, so a workflow that installs one
is compiling software nobody has ever compiled before. It does not fail as a
version bump or an advisory. It fails as a compile error deep inside a
transitive crate, in a job whose subject is something else — and it is invisible
on a workstation, where the tool was installed once and is never rebuilt again.

That is how a green gauntlet sat beside a red Security Audit: the workstation's
`cargo-audit` was years old and working, while CI rebuilt it every run and one
day resolved a `tinyvec` that does not compile. Two of the three installs that
lacked `--locked` were building the release cross-compiler.

So every `cargo install` in a workflow passes `--locked`, which uses the
lockfile the tool's own authors tested — the only build there is evidence
about. [`ci-cd-determinism.test.sh`](../../scripts/tests/ci-cd-determinism.test.sh)
holds all of them to it.

### A read is spelled as a read

The rules above are about a step whose work was refused. This one is about a
step that was never the step anybody wrote, because the tool picked a different
one — and the error it comes back with describes the wrong thing entirely.

`gh api` chooses its own request method: a plain read normally, and a write the
moment any `-f`, `-F`, `--field` or `--raw-field` is present. Those flags are
also how a read narrows what it asks for, so the ordinary act of filtering a
listing turns it into a write against an address that only answers reads. Every
such address answers with `404 Not Found`, which reads as *that workflow does
not exist* rather than *you asked the wrong way*. Under `set -euo pipefail` the
step then dies pointing at the wrong thing, and the shapes built around these
calls make it worse: a run that cannot be found is exactly the condition they
are written to treat as a reason to stand down quietly.

The nightly link drill lost a night to it. Its search for the image build
carrying the machine binary passed three filters, so it was posted, so it was
refused, so the step reported no machine to measure — on a repository where that
build had succeeded hours earlier and its binary was sitting in the artifact
store the whole time. The same call sits on the deploy's manual path, where it
had never yet been asked.

So every `gh api` that passes a field flag states its method rather than letting
the tool infer one, and
[`ci-cd-determinism.test.sh`](../../scripts/tests/ci-cd-determinism.test.sh)
holds all of them to it — reading each invocation whole, across the line
continuations they are written over, and counting what it reached so a sweep
that matched nothing fails instead of passing.

The generalisation is the one worth carrying: **where a tool infers a verb from
the arguments, the verb is written down.** An inferred verb is a decision nobody
recorded, and it surfaces as an error about the noun.

### An input a script refuses to run without is named where it is called

The section above is about a caller that left the verb to the tool. This one is
about a caller that left an input to nobody at all — and it is the harder half,
because the script says plainly what it needs and the saying is what nothing
reads.

[`loadtest-quic-incluster.sh`](../../scripts/loadtest-quic-incluster.sh) launches
the fleet harness into the pod named in `LOADTEST_POD`, documents it as required
and refuses without it. The nightly link drill stands up several pods and holds
this one in `FLEET_POD`, a name the shim has never heard of, so the call was
refused at its first line. The drill had started the machine, the shaper and the
probe, minted its credentials and waited for the machine to come online — twelve
steps of setup — and then measured nothing, on a cluster where every piece it
needed was already up and answering.

The refusal was loud and immediate, which is what makes the shape worth a rule
rather than a fix: nothing about it was subtle at run time, and it still cost two
nights, because the only place it could be discovered was a scheduled run against
a live cluster. A name is checkable from the text alone, and it was checked by
nothing.

So a workflow step that calls a script names every input that script refuses to
run without, and
[`ci-cd-determinism.test.sh`](../../scripts/tests/ci-cd-determinism.test.sh)
sweeps for it: the required inputs are read off each script — the `:?` refusals
the shell itself makes, and the `(required)` entries in the script's own
Environment header — and looked for in the calling job together with the
workflow-level `env` that job inherits, which is the scope a name can actually
reach, since `$GITHUB_ENV` does not cross a job boundary. Like the sweep above it
counts the calls it reached, so a sweep that matched nothing fails.

The generalisation: **a contract stated in one file and satisfied in another is
checked in neither unless something is made to read both.** Where the two are
text, that something is a sweep, and it costs less than one night of a nightly.

### An exemption is re-earned

The list of workflows that may not cache is a statement about tokens, not about
places. If a workflow gains write scope — a trusted trigger would do it — the
row comes out and the cache comes back, in the same commit that proves it.

### A budget covers every term, including the one nothing counts

The rules above are about a step that produced nothing, read nothing, or wrote
nothing. This one is about a step that was *predicted* — a pre-flight that
projects a job's cost and refuses the ones that will not fit. Such a projection is
only as good as the terms it carries, and the term it leaves out is invisible
precisely because nothing counts it.

The mutation pre-flight projected each Go shard as `mutants × per-mutant cost`
and cleared them all. `go-domain-alerts` was projected at 31 minutes and was shot
at the 90-minute cap, taking the night's canonical score row with it, because a
shard's wall clock has a second term: gremlins gives every mutant a leash of the
coverage run's elapsed time times a coefficient, and a mutant that removes a
loop's exit condition never terminates and holds a worker for all of it. At the
coefficient then in force that leash was 46 to 75 minutes — comparable to the
whole cap, on a shard whose entire projected cost was 31.

The term hid well. gremlins records such a mutant as `TIMED OUT`, which is
neither a kill nor a survivor, so it moves no score and appears in no report
field; the only trace is wall clock. It also corrupted the first term, because the
per-mutant costs were being measured as `elapsed_time / mutants_total` — dividing
one mutant's leash across all of them. That read `go-updates-certificates` at 21
seconds a mutant when 143 of its 144 finished in 106 seconds, and it is why the
declared costs had drifted in both directions at once.

So: **a projection states every term of the cost, and a term that is a bound is
bounded where it is set.** The leash is now declared per shard
([`mutation-shards.sh`](../../scripts/lib/mutation-shards.sh)) and added to the
projection, the per-mutant cost is measured over the mutants that *finish*, and
the coefficient in [`.gremlins.yaml`](../../server/.gremlins.yaml) is held by
[`mutation-workflow.test.sh`](../../scripts/tests/mutation-workflow.test.sh) to a
value whose leash still fits in what a fully-spent shard has left of the cap — so
a mutant that starts blocking between one nightly and the next, before any run has
declared it, still cannot carry the job past the cap alone.

The generalisation: wherever a gate answers *will this fit*, the answer is worth
no more than the slowest thing it forgot to add up. A cost that no counter
reports is the one to go looking for.

## Scope

The read-back covers every cache write whose key we choose. A cache an action
computes and writes entirely on its own — the container layer cache, the scanner
databases inside `trivy-action` and `setup-qemu-action` — declares nothing a
static gate can see and names no key a caller can assert. Those are outside what
this rule can hold, and saying so is part of the rule rather than a gap in it.
