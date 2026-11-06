# The Direct link's speed on slower runners

Ivan's decision (2026-11-06): a plan of its own, after the nightly repairs (`phases.md` row "Nightly repairs").
Nothing here is approved for implementation yet; each step's options go to Ivan first.

## What is wrong

The speed nightly ([`e2e-performance.yml`](../../.github/workflows/e2e-performance.yml),
[`performance.spec.ts`](../../web/e2e/performance.spec.ts)) has been red every night since
2026-11-02 on commit `dcdb98ec`, always on a 100 Mbit/s download over Direct held to 80% of the
line. Through the server the same download reads 95.8–97.0% every night.

| Night | Run | Runner | Clean | 1% reordered | 5% reordered | With typing | Agent / browser cores (clean) |
|---|---|---|---|---|---|---|---|
| 11-02 | 41414519422 | EPYC 7763 | **77.1** | 80.1 | 81.6 | **71.6** | 0.60 / 1.30 |
| 11-03 | 41606185283 | EPYC 7763 | **77.0** | **78.2** | 77.3 | **70.6** | 0.61 / 1.27 |
| 11-04 | 41798563578 | Xeon 8370C | **76.8** | 89.1 | 77.3 | **79.6** | 0.46 / 1.13 |
| 11-05 | 41996760430 | EPYC 9V45 | 87.2 | **79.7** | 90.2 | 84.6 | 0.29 / 0.90 |
| 11-06 | 42189979276 | EPYC 7763 | **74.3** | **76.2** | 70.1 | **62.8** | 0.60 / 1.25 |

Bold is under the 80% limit (the 5% case is recorded only). Every 10 Mbit/s case reads 91–92%,
and "sent beyond what arrived" stays 8.5–8.9% at 100 Mbit/s (header overhead, no resends).

What the readings say:

- Per byte, the slow chip costs about twice the processor on both sides: the agent spends 0.78
  cores per 100 Mbit/s on EPYC 7763 against 0.33 on 9V45, the browser 1.69 against 1.03.
- Neither side is out of processor: the agent stays at 0.6 of a core, and Chrome's 1.3 cores are
  spread over its threads.
- So the link is held by something that waits, and the wait is longer on a slower chip. Session 14
  found the sender settles at its 64 KiB cushion divided by how late acknowledgements come back
  beyond the least round trip; slower processing on either side makes them later.
- The 9V45 night's 1% case (79.7) sits under the limit on the fastest runner, so the spread is
  partly the reordering case's own.

## Steps

1. Reproduce the slow chip on the local rig: hold the agent and the browser to the per-byte
   processor cost EPYC 7763 shows (CPU frequency cap or a share of a core per side), and confirm the
   clean case falls to 74–77%.
2. Find what waits: per acknowledgement, how late it is beyond the least round trip on each side —
   Chrome's acknowledgement delay, its IO and WebRTC thread turns, the agent writer's wake — read
   from the agent's link counters and a Chrome trace at both speeds.
3. Bring Ivan the options with what each costs and risks (for example a cushion sized from the
   measured lateness, or a change of where the limit is held), each proved on the rig at both chip
   speeds before any change.
4. Whatever lands: tests first, the rig re-read, the nightly read on each runner model it lands on.

Out of scope (Ivan, 2026-11-02): the agent's own processor cost — mimalloc, a core of its own.
