---
number: 100
title: A bundle field is a reading, or it is absent
status: Accepted
date: 2026-09-06
---

# ADR-100 — A bundle field is a reading, or it is absent

## Context

The evidence bundle is what a performance run is. The metrics store keeps thirty
days, so any comparison older than that reads the bundle, and
[ADR-082](ADR-082-load-runs-measure-the-system-or-say-they-did-not.md) makes it
the artifact a run is judged from.

Eleven of its fields were not measurements. Each was declared, documented with
the reason it mattered, validated by a rule — and assigned from a literal or from
the thing it was supposed to be compared against. Read off the 2026-09-05 runs:

| Field | What it held | The rule it fed |
|---|---|---|
| `achieved_arrivals_per_second` | the offered rate, copied | a phase below 80% attainment invalidates |
| `generator_headroom` | `100` free, `0` used, unconditionally | a generator below 20% headroom invalidates |
| `target` fingerprint | `cpus: 1, memory_bytes: 1` on every leg | a fingerprint with no shape fails validation |
| `generator` fingerprint | the same one byte | the same |
| `achieved_connected_agents` | machines started, not machines up | the gap between offered and achieved is the finding |
| `latency_p50/p95/p99_ms` | the last *finished* machine's connect time | — |
| `error_rate`, `faults`, `expected_rejections` | never assigned in a profiled run | the profile's own error ceiling |
| `devices` | the plan, not the fleet | — |
| `run.commit` | the string `unknown` | a run with no revision cannot be attributed |
| `journeys` | `null`, while three journeys were timed all along | — |
| `database_bytes`, `telemetry_series` | never written | the volume family's whole finding |

The attainment ratio was therefore exactly `1.0` on every run ever recorded. The
headroom rule had never fired. Four bundles from a sweep whose only subject was
the processor count reported identical hardware — and because a latency figure is
a property of the pair, every number beside them was uninterpretable. A phase
latency read off the last finished machine is `null` in a profiled run, because
no machine finishes while the walk is running.

This is the defect class
[`ci-cd-determinism.md`](../../.claude/rules/ci-cd-determinism.md) and
[`resource-conservation.md`](../../.claude/rules/resource-conservation.md) exist
for, in a third place: a number that reports success without measuring. A counter
the teardown path maintains says the teardown ran; only a reading says the
resource came back. Every field above was the first kind wearing the second's
name.

## Decision

**A field carries a reading or it is absent, and validation refuses the middle.**

1. **Both sides of the measurement are passed in and recorded.** The target's own
   limits are known to whoever started it — the sweep's matrix value, the
   cluster's own container spec — and to nothing inside the harness, which sees a
   network address. So they are handed in (`-target-cpus`,
   `-target-memory-bytes`), converted out of the cluster's notation by
   [`k8s-quantity.sh`](../../scripts/k8s-quantity.sh), and `Fingerprint.CPUs`
   becomes fractional because half a processor is one of the sweep's rungs.
   `Bundle.Validate()` refuses a memory figure below a mebibyte: no machine this
   runs on could be limited to less, and the number being refused is one byte.

2. **The generator measures itself.** `ReadGeneratorHeadroom` reads the same
   kernel accounts the node reader beside it does, and `Headroom.Measured` says
   whether anyone looked. An unmeasured generator invalidates the run — a reading
   nobody took is not a reading of plenty. This is what makes a sweep's top rung,
   where the generator is squeezed onto the same processors as the stack it
   drives, report its own starvation rather than answer wrongly.

3. **The arrival rate is split by side, and the schema version says so.** The old
   pair's offered half was technician load and its achieved half was that figure
   copied across, so it was unreadable in both directions. Bundle schema 2 carries
   `offered_agent_arrivals_per_second` and
   `achieved_agent_arrivals_per_second` — the machine side, which this process
   drives and therefore measures — beside
   `offered_operator_arrivals_per_second`, which the profile declares, and an
   `achieved_operator_arrivals_per_second` that stays **absent** until a
   technician-side generator fills it. An absent figure is readable; a copied one
   is not.

4. **A phase reports what it saw.** The fleet keeps a cumulative tally —
   arrived, failed, severed, rejected — and a phase is the difference between two
   readings of it, which is the only way to separate a phase from the run around
   it when a machine reports once at the end of its own life. A refusal the
   server made on purpose is counted apart from a fault, because counting a
   correctly enforced limit as a defect buries the real ones.

5. **A phase's latency is a live round trip.** A machine connects, handshakes,
   registers and hangs up at each step of the phase's climb. It is a fresh arrival
   rather than a message on a connection already open because the control stream
   has no reply to a heartbeat — a connect-handshake-register is the only round
   trip the machine side has. A probe that could not be taken contributes no
   sample rather than a zero.

6. **A machine that has ended is not one of the connected**, whichever way it
   ended. Removing only the ones that errored made the count a count of machines
   *started*.

7. **The fleet is the machines that enrolled.** What was planned travels beside it
   as `planned_devices`. A bundle had said two thousand machines while the
   database, weighed in the same job, held five hundred.

8. **A run states its revision or is refused.** `Bundle.Validate()` rejects the
   literal `unknown`, and the workflows pass `-commit`.

9. **Readings taken by other steps of the same run reach the bundle.** The fleet's
   weight on disk is read from the database after the fleet exists; the journeys
   are timed by a generator in another pod. Neither can be inside the harness's
   own process, and each was writing into a file no later reader opened, so
   [`loadtest-bundle-merge.sh`](../../scripts/loadtest-bundle-merge.sh) folds them
   in — failing when there is nothing to fold, because a step that reports success
   for work it did not do is the shape this whole record is about.

   This does not reopen the bundle to later queries of the system under test.
   Those are still refused: a bundle assembled from a query describes the system
   at the time of the query, which is a different moment from the one being
   reported. These are measurements *of this run, by this run*, arriving where a
   reader can find them.

## Consequences

Runs that used to pass can now be marked invalid, and that is the point: the
first night after this lands must show at least one figure that could not have
been produced before — a headroom below 100, a fingerprint that is not `1/1`, an
attainment that is not exactly `1.0`, a phase error rate that is not zero, or a
revision that is real.

Bundle schema 1 and 2 do not mix. The version travels inside the document for
exactly this reason: a trend that silently spans two meanings of a field is worse
than one with a gap in it.

Each phase's climb now costs ten probe arrivals, which enrol ten devices. Making
a machine's identity reusable is separate work; until it lands, a profiled run's
fleet is its held machines plus its probes.

The [`test-value.md`](../../.claude/rules/test-value.md) standard applies to every
rule above: each was verified by reintroducing the defect and watching a named
test go red.
