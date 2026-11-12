# Busy-morning sign-in on the AMD EPYC 7763 — naming the cure

Split out of the numeric-ingest master plan's §12 item 3 (Ivan, 2026-11-12: "if
other items from the plan are not blocked by this experiment make this a separate
plan"). Nothing in commit 4 waits on it.

## What is known

- In 30 nights to 2026-11-11, every busy-morning (`peak`) night on an AMD EPYC
  7763 runner (11 nights) read a sign-in median of 337–375 ms; every other chip
  read 3–4 ms. The slowest 5% read 380–920 ms on every chip.
- From the 30 saved bundles: the median jumps on every night whose server
  averaged more than about half its processor over the wave (all eleven 7763
  nights, one 9V74 night). The generator had 84% of its processor spare.
- Each sign-in makes about six database round trips through a 25-connection pool,
  against a database at one processor. The pool figures in the bundle are read
  after the run, so they read zero.
- Run 43143167932 on `main` (commits 2 and 3: one shared writer, presence written
  on change, fewer round trips per sign-in), on a 7763: sign-in median 3.3 ms,
  slowest 5% 9.9 ms; technicians' medians 2.6–12.7 ms.
- Commit 4 compares a night only with nights on the same chip, so a 7763 night
  that slows down again fails against the 7763 nights.

## What this plan answers

Which change in commits 2 and 3 removed the slowdown, so it can be held by a test
or a limit, and whether the cure holds past 2,000 machines.

## Method (Ivan's earlier choices, 2026-11-12)

1. The bundle carries the pool's waits (connections waited for, total wait) and
   the database container's throttling, read over the wave, not after the run.
   Test first, in `server/tests/loadtest` and `scripts/loadtest-bundle-rows.sh`.
2. Local A/B only: the busy morning on the code just before commit 2
   (`git archive 71edce30`) and on today's, at a 7763's processor share, with a
   CPU profile of the server over the wave.
3. The nightlies on `main` confirm the cure on real 7763 runners.

## Open question for Ivan (asked 2026-11-12, not yet answered)

How the workstation's server allowance is cut to a 7763's. No 7763 has run the
fixed benchmark (`scripts/cpu-benchmark.sh` runs on the ARM catch-up leg only).

- **Scale by the workload** (recommended): on the same scaling load the server was
  busy 36.7% of one processor on a 7763 (14 nights), 25.0% on a 9V74 (6), 19.6%
  on the newest EPYCs (6); at two processors 18.3%, 13.5% and 9.1%. Run the
  scaling load at one processor on the workstation, read its busy share, and cut
  the busy-morning allowance by its ratio to 36.7%.
- **Benchmark every leg first**: add the one-minute benchmark to every leg (about
  11 minutes a night) and run the A/B once a 7763 night has recorded it.

## Done when

- The bundle's pool and throttling readings are rows with limits (pass/over), and
  the A/B's two runs are recorded here with their medians, slowest 5%, pool waits,
  throttling and the profile's top functions.
- The change that cured it is named, and a test or a limit holds it.
- This plan is deleted in the commit that lands its last step, with its
  `phases.md` row.
