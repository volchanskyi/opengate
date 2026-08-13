# EF-B10 — Retroactive rule evaluation over local history, and the Q11 reach measurement

**Master plan:** `edge-first-telemetry-and-investigations.md` §5.4, §6.5 (retroactive), §9.1.1,
D12, D34, E8, E21, E22, step 13.
**Acceptance criteria owned:** **B9**, **B10**, **B14**.
**Dependencies:** EF-B8 (grammar), EF-B9 (rule versions arrive with a catalogue), EF-B6 (the alert
sink).
**Blocks:** nothing. **Co-verified with EF-C3:** the "one incident per `(rule, scope)`" half of B9 is
asserted there, over a fixture of `backfilled` alerts this plan produces.

## Context

"Has this happened before?" is answered by **re-running the detector over edge history**, not by a
central flight recorder (§5.4: ~5.5 GB / 48 h at 5 000 agents, a third storage path, and only 48 h of
reach). Pushing a rule version schedules a bounded, resumable, idle-scheduled job that evaluates it
against the local T1 tier; findings return as alerts marked `backfilled`.

**A retro scan yields one incident per `(rule, scope)`**, never N live incidents — learning a new
failure mode must not page-flood Contoso's queue.

**The reach claim is currently arithmetic, not a measurement.** §2.6's "years at 60 s" is
`cap ÷ density`, and the density half is genuinely gated (`bytes_per_sample < 12.0`, measured ≈ 7.3
in [gates_test.rs](../../../agent/crates/edge-tsdb/tests/gates_test.rs#L56)) — but the division assumes
the store holds only today's collectors and that T1 is never evicted. §5.4's rejection of the flight
recorder leans on that number, so it gets measured.

## Steps (TDD-first)

1. **Test first (B9):** a local store seeded with a known history plus a rule version that would have
   fired three times → exactly three alerts, each marked `backfilled`, each carrying its **real event
   time** (not scan time), all sharing one `(rule, scope)` grouping key. Hand the fixture to EF-C3.
2. **Test first — resumability:** a scan interrupted mid-history resumes from its cursor and produces
   the same alert set as an uninterrupted run — no duplicates, no gap. Then: a scan whose rule version
   is superseded mid-run stops rather than finishing against a stale definition.
3. **Test first (E22):** a newly enrolled device with no history produces **zero** findings and
   reports its retroactive scope as empty — reported honestly, not as a completed scan.
4. **Test first (B10, Q6):** the job is hard-throttled — assert the throttle by construction (a
   budget the scan must yield against between chunks), not by timing a run, which is untestable
   deterministically. Assert it yields, that a chunk is bounded in samples, and that CPU accounting
   is reported.
5. **Test first (B10, E21):** under simulated host disk pressure the retroactive job **suspends
   first**, before the store's own cap eviction changes behaviour, and resumes when pressure clears.
6. **Test first — idle scheduling:** a scan does not start while the sampler reports load above the
   idle threshold, and never runs during maintenance mode (join the existing `MaintenanceGate`).
7. Implement the job in `mesh-agent` with the evaluator in `mesh-agent-core`; findings go to EF-B6's
   sink under the same per-device ceiling as live alerts (a retro scan must not be able to blow the
   20/h budget — assert it).
8. **Measure Q11 (B14) and stop with the number.** Method: drive a store to **steady state** (cap
   eviction active) at the real vitals shape, then read the oldest surviving T1 timestamp.
   512 MB at ~7.3 B/sample is weeks of wall-clock to fill honestly, so run at **≥ 2 reduced cap
   sizes**, show reach scales linearly with cap, and extrapolate to 512 MB with the linearity
   evidence attached. An extrapolation without the two-point linearity check is the same
   unmeasured-arithmetic mistake in a new coat.
9. Report the measured reach against §9.1.1's table. **A reach under 48 h stops here** for the
   owner's decision (accept and restate; raise `EDGE_STORE_CAP_MB`; change tier retention to favour
   T1; or narrow the retroactive claim) — it does not proceed on the implementer's judgement.

## File inventory

- **Create:** `agent/crates/mesh-agent-core/src/alerts/retro.rs` — bounded, resumable scan over T1.
- **Modify:** [main.rs](../../../agent/crates/mesh-agent/src/main.rs) — scheduling, idle gating,
  disk-pressure suspend (`EDGE_STORE_CAP_MB` lives at
  [:309](../../../agent/crates/mesh-agent/src/main.rs#L309)).
- **Modify:** [alerts/sink.rs](../../../agent/crates/mesh-agent-core/src/alerts/) — accept `backfilled`
  findings under the shared ceiling.
- **Create:** a reach-measurement harness beside
  [gates_test.rs](../../../agent/crates/edge-tsdb/tests/gates_test.rs) — always-running at small caps,
  no skips.
- **Docs:** [Monitoring.md](../../../docs/Monitoring.md).

## Traps

- **Event time, not scan time.** A backfilled alert stamped at scan time destroys grouping (E8 folds
  by real event time) and makes a 30-day-old freeze look like it happened this minute.
- T1 is 60 s data. A rule whose `sustain_secs` is finer than 60 s **cannot** be evaluated
  retroactively — classify it `unsupported` for retro rather than evaluating it wrongly at a coarser
  resolution.
- The store offers MVCC snapshot reads; a scan must not block the sampler's writes.
- Reach depends on what the store holds. The deferred per-entity collectors program (§4.2) will
  multiply that, so record the vitals shape the measurement assumed — the number is only meaningful
  next to it.
- Do not let a retro scan bypass the per-device ceiling "because it is historical". A 5 000-finding
  scan is exactly the flood the ceiling exists for.

## Out of scope

Incident grouping (EF-C3). Rollout stages (EF-B11). Any central storage of history (§4.2).

## Reviewer checklist

- [ ] Findings marked `backfilled`, stamped with event time, one grouping key.
- [ ] Resumable, no duplicates, superseded-version abort.
- [ ] Throttle asserted structurally; suspends under disk pressure; idle-gated; maintenance-gated.
- [ ] Empty-scope device reports honestly (E22).
- [ ] Q11 measured at ≥ 2 cap sizes with linearity shown; number recorded with the assumed shape.
- [ ] A sub-48 h reach is escalated to the owner, not absorbed.

## Verification

`cd agent && cargo test -p mesh-agent-core -p mesh-agent -p edge-tsdb`, the reach measurement run,
`/precommit`.

## Close-out (mandatory)

`git mv` this plan to `archive/` in the landing commit, bump internal links one `../` deeper,
repoint the master-plan index row. The Q11 number goes into EF-Z1's evidence section.
