# Catch-up waits while machines wait to sign in, and the rate written down

Split out of the fleet-return sign-in-rate plan when its commit 5 lands (Ivan, modals,
2026-11-25). That plan's readings and research travel here only as far as this work needs them.

## Why

- The ARM catch-up night on `47a7b90e` (2026-11-25) signed in 9.4 returning machines a second over
  the return's first 42 s, 16.5 a second while any waited; the server was throttled in 99% of its
  periods. Catch-up filled 4,971 of 5,000 machines in 1,535 s against the 960 s promise.
- The workstation's processor profile from 2026-11-24, before technicians' requests waited: the
  fleet page 19-26% of the processor, catch-up 8-13%, sign-ins 1.5-5%. Technicians' requests now
  wait while machines do (ADR-131); catch-up does not.
- How others do it: Datadog throttled backlog recovery after its 2023 outage "so old data does not
  preempt live data"; Netdata fills a returning child's gap on one thread by default; Kubernetes
  keeps node check-ins in a priority level of their own.

## Decided (Ivan, modals, 2026-11-25)

- **Catch-up waits while any machine waits to sign in.** Example: after a data-centre restart an
  MSP sees its 4,000 machines online first, and their missing hour fills in after.
- **The rate promise is lowered to what the clean ARM run reads** on `db414772` (the harness holds
  the return's level, the legs at 4,000 machines), as long as it keeps 95% online within 3 minutes
  and all within 5 at 4,000 machines: at least 21 a second.
- Production's server keeps a quarter processor and 384 MiB, its ceiling about 4,000 machines
  (ADR-131). The rate rows stay filed beside an online time.

## The code as it reads (2026-11-25)

- A turn request is always granted (`handleRequestBackfillSlot` → `BackfillScheduler.RequestSlot`,
  `server/internal/agentapi/`); each batch is admitted or told to wait in `Admit`, which reads a
  `headroom` signal (0..1) and the queue's fill.
- `AgentServer.MachinesWaiting()` (`sign_in_line.go`) is what the console line reads.

## Steps, each test first

1. The rate number: read the hand-run catch-up (run 45740227880) and burst (run 45740231130) legs
   by job and step result and from their bundles; bring the reading to Ivan with the number it
   supports; write it into `catch-up.yaml`, `burst.yaml` (`sign_ins_per_second`), their profile
   tests and ADR-131's context line.
2. Catch-up waits: the batch admission tells a catch-up batch to wait while any machine waits in the
   sign-in line, with the retry time it gives today; live readings are never held. Where the hold
   sits (the headroom signal, `Admit`, or the turn grant) is read from the code first and brought
   to Ivan if more than one shape fits. Tests: a batch told to wait while a machine waits, admitted
   once the line empties (fake clock, unit); a live reading stored while the line is full
   (integration); the ARM night's fill and sign-in rate.
3. Docs: ADR-044 (catch-up), ADR-131 (who waits while machines do), `Device-Health.md` if what a
   technician sees changes.

## Commits

1. Steps 1-3 together, after the clean run's reading and Ivan's number.

## Done when

- The ARM catch-up night passes `machines_caught_up`, `fill_seconds` and the written rate.
- This plan is deleted in the commit landing its last step, with its `phases.md` row.
