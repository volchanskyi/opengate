---
number: 120
title: What holds an object is followed on the box the run destroys
status: Accepted
date: 2026-09-10
---

# ADR-120 — What holds an object is followed on the box the run destroys

## Context

[ADR-119](ADR-119-a-long-run-names-the-line-that-grew.md) gives the endurance run
a goroutine profile and a heap profile on an interval, and reports what grew
between them at a line. For a stuck-goroutine leak that is the whole answer: the
profile carries a count and the stack the count is parked on.

For a held-object leak it is half of one, and the missing half is the half that
matters. Go's heap profile records the **allocation site** — where an object was
born. A leak is not a statement about where something was made; it is a statement
about what is still pointing at it. A map that never has entries removed reports
its growth at whatever line allocated the values, which is ordinary code doing
ordinary work, on a path that is correct everywhere else it is used.

Nothing the target publishes about itself can close that, because the answer is
not a property of the process's own accounting — it is the shape of the live
heap. Reading it means reading the heap: every object, its type, and every
pointer between them, walked back to the root that keeps a given object alive.

Two things are needed for that and neither is available on the cluster. The first
is a core dump, which means attaching to a running process — the staging pod
drops every capability, runs as a non-root user, and has a read-only filesystem,
and the node it sits on is production's neighbour. The second is a binary that
still carries its symbol table and its debugging information; the shipped image
is linked with `-s -w`, which removes exactly the tables that turn an address
back into a type and a frame.

Both are available on the endurance venue, and that venue was chosen partly for
this: the run creates the machine, owns the whole of it, and destroys it at the
end of the job.

## Decision

**The endurance run takes a core off the running server and walks the reference
graph back from the heaviest live types to the root that holds them.**

`gcore` takes the core without stopping the server, so the walk is something the
run does to a target it has finished measuring rather than a way of ending it.
`viewcore` reads the core as a Go heap
([`golang.org/x/debug`](https://pkg.go.dev/golang.org/x/debug/cmd/viewcore),
pinned in [`tool-versions.sh`](../../scripts/lib/tool-versions.sh)) and its
`reachable` command is the walk itself: it prints the path from a global, or from
a named variable in a live goroutine's frame, down to the object.

**The endurance target is built with its debugging information kept.** The
`GO_LDFLAGS` build argument defaults to the `-s -w` every shipped image is linked
with, and the soak alone overrides it to empty. It changes no generated code —
only what travels beside it — so the target is the same server measured the same
way, carrying the tables that make the core readable.

**Every way the walk cannot happen is a failure, never an empty report.** A
container that is not running, a binary whose tables were stripped, a core the
debugger never wrote, a core `viewcore` cannot open, a heap with no live objects
in it: each of those ends in a directory of short files and a step that exited
zero, which is the shape
[`ci-cd-determinism.md`](../../.claude/rules/ci-cd-determinism.md) exists to
refuse. So [`loadtest-reference-walk.sh`](../../scripts/loadtest-reference-walk.sh)
refuses each of them by name, and the overview is read back before anything else
is written so a core nobody could open fails where the message says so.
[`loadtest-reference-walk.test.sh`](../../scripts/tests/loadtest-reference-walk.test.sh)
drives the script against stub tools arranged each of those ways and requires a
different verdict from each.

**The core is read where it is taken and never carried out.** It is most of the
target's address space, including every row of the fixture, so what leaves the
job is the text: the overview, the memory breakdown, the type histogram, the
goroutine list, and the reference walk itself.

## Consequences

The endurance artifact now answers both halves of a leak. `leak_trail` in the
bundle says what grew and at which line it was born; `reference-walk/` says what
is still pointing at it, named as a global or as a variable in a live
goroutine's frame.

The soak job installs `gdb` and `viewcore` before the five-hour walk rather than
after it, so a run that could not be followed says so in its first minute instead
of at the end of an afternoon.

The endurance target's binary is larger than the shipped one and is not
byte-identical to it. That is stated rather than hidden: the linker flags removed
by the default affect only the symbol and debug tables, so the code paths being
measured are the same ones. Any comparison between this family's absolute figures
and another venue's was already a comparison across processor architectures
([`docker-compose.perf.yml`](../../deploy/docker-compose.perf.yml) runs on x86_64
and production is ARM64), which is the larger of the two differences by a wide
margin.

What this cannot do is run anywhere else, and that asymmetry is the point of the
two decisions being separate.
[ADR-119](ADR-119-a-long-run-names-the-line-that-grew.md)'s profiles need nothing
but an HTTP request to a port the harness already reads, so any venue can be
asked for them by declaring an interval. The walk needs a debugger, a process it
may attach to, and a binary nobody stripped — so it belongs to the one family
whose venue supplies all three, and closing that gap by loosening the staging pod
would be the wrong trade.
