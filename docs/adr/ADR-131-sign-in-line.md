---
number: 131
title: Sign-ins wait in a line sized by the processor
---

# ADR-131 — Sign-ins wait in a line sized by the processor

## Context

A whole fleet comes back at once after a release replaces the server, or when an
office's internet returns: the agents redial within their random 1–30 s wait. With
nothing limiting sign-ins at once, every one ran together, the processor was shared
thousands of ways, and a rehearsal of 5,000 machines at production's limits read a
median sign-in of 22 s and a slowest 5% of 55 s. The fleet-return promise is a rate:
the server signs in at least 100 returning machines a second at production's size,
so a fleet's return time is its size divided by that rate (Azure IoT Hub's form).

## Decision

**A set number of sign-ins run at once; the rest wait connected.** The number is
8 per processor of the container's allowance, read from its quota at start
([`cpulimit.go`](../../server/internal/app/cpulimit.go)), never below one
([`sign_in_line.go`](../../server/internal/agentapi/sign_in_line.go)). On the
rehearsal at a quarter processor, with 5,000 machines back within 30 s, 1, 2, 4
and 8 at once read 29, 31, 28 and 18 sign-ins a second: the sign-in is bound by
the processor, two at once was fastest, and more contended. A machine waits after
its `AgentRegister` arrives, and the server's sign-in timer counts the wait.

**Waiting machines are served in turn by customer.** Each customer's machines
wait in arrival order and the customers take turns, so Fabrikam's 40 laptops are
not held behind Contoso's 3,000. A returning machine shows only its device number,
so the server keeps a note of each machine's customer, read at start inside each
tenant's boundary and updated by every sign-in
([`customer_note.go`](../../server/internal/agentapi/customer_note.go)). The note
orders the line and nothing else: a machine's rules come from its sign-in. A
machine the note lacks waits in a turn of its own.

**The line holds 30 seconds of the pace it measures.** The pace is read from the
slots handed straight to a waiting machine, which come one after another at the
server's own rate; until it has a pace, the line counts on the promised rate. This
is Little's law, as Azure DPS spreads retries by the rate the service achieves.
Sized from the promise instead, the rehearsal's line held about 100 s of real work.

**A full line tells a machine to come back later.** The newest machine of the
customer holding the most of the line is turned away, so a small customer still
gets a place. It is sent `ComeBackLater` with a random wait of 0–30 s, and the
connection closes once it hangs up, which frees what it held
([Wire Protocol](../architecture/Wire-Protocol.md#the-sign-in-line)). Every agent
understands it. Azure IoT Hub queues a burst at its rate and refuses only when the
queue is full; HiveMQ caps handshakes in progress; Netdata spaces sign-ins a set
time apart, which costs every turned-away child a new handshake.

**While machines wait, the console's requests wait too.** A signed-in console
request takes one of a few places, sized per processor of the same share
([`console_line.go`](../../server/internal/api/console_line.go)), while any
machine waits in the line; with none waiting, nothing limits it. Public routes,
the machine's enrolment among them, never wait. A request still waiting as its
30 s run out is answered 503 with `Retry-After`. Without it, two sign-ins at a
quarter processor shared the processor with every console request at once: a
rehearsal of 5,000 machines returning beside the busy morning's technicians
signed in 0.2-0.7 machines a second while the technicians worked and 42 a second
the minute they stopped, the fleet list alone (each a 5,000-machine body) taking
a fifth to a quarter of the processor. Kubernetes' API Priority and Fairness keeps
node check-ins in a priority level user requests cannot take, for the same reason.

**One server at production's size holds about 4,000 machines.** A quarter
processor and 384 MiB read 64 KB a signed-in machine on ARM, 357 MB with 4,972
machines catching up, and 79 KB on the burst, 314 MB with 3,450, whose server was
killed for memory as its machines sent their 1 MiB inventories. The catch-up and
burst legs run at that ceiling
([`catch-up.yaml`](../../load/profiles/catch-up.yaml),
[`burst.yaml`](../../load/profiles/burst.yaml)). EMQX's single-node benchmark
reads about 10 KB an idle connection, so a higher ceiling comes from what each
machine holds in the server.

## Consequences

A returning machine waits connected, holding what a signed-in machine holds, so
the line costs no more memory than everyone at once did. Each machine told to come back pays a secure
handshake again on its return, and handshakes happen before the line. The waiting
count and the turn-aways are published as `opengate_sign_in_line_waiting` and
`opengate_sign_ins_turned_away_total`
([Metrics Reference](../architecture/Metrics-Reference.md#registration)). The
catch-up night holds the server to the rate
([`catch-up.yaml`](../../load/profiles/catch-up.yaml)). No setting tunes the line.

The console's places were measured at a quarter processor, the same 5,000 machines
and technicians. With none, 3,074 machines caught up and about 2,900 failed to
connect. With one place, 4,987 caught up, 495 failed, sign-ins ran about 10 a
second in the 30 s return, and the fleet page took a median 5.2 s (2.2 s without
the line), about 1 in 100 console requests told to try again. Two places read the
same sign-in rate with 609 failed, so a place costs the fleet more than it gives
the console, and the line keeps 4 per processor. After the return the server
signed in 3.2 machines a second while technicians worked: the console still took
37% of the processor, 29% of it decoding every machine's health rate for each fleet
page, catch-up 25% and garbage collection 10%, leaving sign-ins about 5%.
