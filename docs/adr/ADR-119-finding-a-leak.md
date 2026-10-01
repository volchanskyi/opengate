---
number: 119
title: Finding a leak in a long run
---

# ADR-119 — Finding a leak in a long run

## Context

The conservation reading says whether a run gave back what it took
([ADR-093](ADR-093-relay-session-lifetime.md)). It does not say where, and the
distance between the two is a working week: a red endurance run hands back one
number and an instruction to reproduce five hours of load with a profiler
attached. The run that produced the evidence is gone, the machine is destroyed,
and the next one is seven days away.

The answer was already on a port the harness reads. The server publishes its own
profiler on the cluster-only listener
([ADR-095](ADR-095-two-listeners.md)), and a goroutine profile is not a hint
about a leak of this shape — it is the finding: a count, and the stack it is
parked on.

## Decision

**A run asked for an interval takes the target's goroutine and heap profiles on
that interval, keeps every one, and reports the difference between consecutive
readings as growth at a named line.**

**Every profile is kept.** The finding is a difference, and a difference cannot
be recovered from a summary of either side. Sixty readings of both profiles
across a five-hour walk is about fifteen megabytes.

**They are kept as text the target has already resolved**, not as the binary
form. Resolving the binary form needs the exact binary that produced it, which
is destroyed with the job, and a profile nobody can resolve names no line at
all.

**The site is the first frame that is not standard library.** Every parked
goroutine's own top frame is the runtime parking it, so a site taken from the
top would report every leak in the product at the same line.

**A profile that could not be taken is counted, not skipped.** A page that is
not a profile parses as no stacks, and no stacks reads as nothing grew — the
healthiest answer a leak detector can give. Each reading is checked, every
unanswered fetch is counted, and fewer than two readings fails the run outright,
because one reading has no difference in it.

**Growth is reported with how many intervals it grew in**, which is what
separates a leak from a working set that got bigger once and then held.

### Following what holds an object

**The endurance run takes a core off the running server and walks what is
keeping the heaviest live types alive.** Go's heap profile records where an
object was born, and a leak is not about where something was made — it is about
what is still pointing at it.

**The target is built with its debugging information kept**, or the walk names
nothing. The endurance family asks for that by handing the build an empty link,
and an empty value has to survive being read: a default supplied for an unset
variable is supplied for an emptied one too unless it is written not to be, so
the empty link was replaced by the release one and the first walk ever taken
refused at its first check. What a workflow empties on purpose and what the
stack reads it with are checked against each other, and what the stack renders
is read back rather than inferred from the punctuation.

**Every way the walk cannot happen is a failure, never an empty report.**

**The core leaves the runner encrypted, or not at all.** It is most of the
process's memory, and the repository and its artifacts are public. It is taken
outside the bundle, compressed with zstd and encrypted with `age` to a key made
for this alone, before the walk reads it; the plain copy is removed on every
exit, and the encrypted file is uploaded as an artifact of its own
([`loadtest-reference-walk.sh`](../../scripts/loadtest-reference-walk.sh)). The
public half of that key is the repository secret `SOAK_DUMP_AGE_RECIPIENT`, and
the walk refuses before it starts when that value is empty, an SSH key, a
private key or anything but a native age recipient. The private half exists only
on the maintainer's machine, never in the repository's secrets, so no workflow
can open a dump. A native age key rather than an SSH one, because an SSH
recipient leaves a marker of the key in every file it encrypts. How to open one
is in [Testing](../infrastructure/Testing.md#opening-a-soak-dump). The program
copy travels in the plain bundle: it is built from public source.

**The reader is proved against the toolchain whenever either moves.** It reads
the runtime's unexported heap structures, which change between Go releases, and
it publishes no tagged releases, so it is pinned by commit in
[`tool-versions.sh`](../../scripts/lib/tool-versions.sh). The
[`core-walk.yml`](../../.github/workflows/core-walk.yml) workflow runs whenever
`server/go.mod`, that pin or the walk changes: it builds a small program with the
pinned toolchain, takes a core of it, puts it through the same walk, and passes
only when the reader names a live object and follows it back to its global root
([`core-walk-check.sh`](../../scripts/core-walk-check.sh)). A reader the
toolchain has outrun fails the commit that moved one of them, rather than a soak
up to a week later. The reader names small objects and reports ones large enough
to carry an allocation header by their size class, on the toolchain before this
one as on this one.

**The reader is patched for large pointer maps, and the patch retires itself.**
Go reaches the pointer map of a type with more than 128 pointer words through one
more pointer, which the runtime fills in on first use; the pinned reader read
that slot as the map and walked off the end of the binary's data, so a dump
holding a large compressor was unreadable. The reader is built with
[`viewcore-gcmask-on-demand.patch`](../../scripts/patches/viewcore-gcmask-on-demand.patch)
applied ([`install-viewcore.sh`](../../scripts/install-viewcore.sh)), which
follows that pointer and treats a map not yet built as every word a possible
pointer. The check's program keeps alive a type that large and walks through it.
The workflow also runs nightly, and each run builds the newest upstream reader
unpatched beside the patched one: the night upstream reads that core too, the
check fails and names the swap, so the patch is removed as soon as it is not
needed.

## Consequences

The trail is diagnostic and gates nothing: the conservation slope already
decides whether a run failed, and it decides it on a reading of the resource
rather than on a profiler's account of where allocations came from. What the
trail adds is the line to open once that verdict is red.
