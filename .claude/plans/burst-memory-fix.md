# The burst's memory fix, and the minutes its inventories take

Status: active. Every point is settled with Ivan (modals, 2026-11-27).

Split out of `catch-up-waits-for-sign-ins.md` as its step 1 (Ivan, modal, 2026-11-27), so a commit
can name an active plan while that plan's later steps wait on readings.

## Why

- The ARM burst leg (4,000 machines return at production's limits, then each sends a 1 MiB
  inventory) has had its server killed for memory every night since 13506f53 (2026-11-20), before
  storing any inventory: 46041730159 on 2026-11-27, exit 137, inventory_sent 0.
- The workstation rehearsal of the burst profile at production's limits (0.25 processor, 384 MiB,
  heap profiles every 5 s) found where it goes: at the return's end the 4,000 connections held
  128 MB live, and then quic-go held the inventory data it had accepted and the server had not read
  (a 512 KiB window a connection), which the read allowance never counts.

## Decided (Ivan, modals, 2026-11-27)

- **Fix: a shared budget.** Each machine connection opens with a 16 KiB window (stream and
  connection) and grows, to at most 1 MiB, only by drawing on one budget for the whole server, 5% of
  Go's memory limit like the read allowance, handed back when the connection closes.
- **Inventory promise: today's pace, written down.** Every machine keeps sending at once; the
  promise is all 4,000 stored within the minutes the ARM burst leg on the fix's commit measures.
- **The minutes are a measurement of the harness**, read the way it reads the last machine coming
  online: the harness asks the server for its stored count every 5 s through the inventory phase
  and keeps the seconds from the phase's start until that count first reaches the phase's machines.
  The inventory phase is 40 minutes, long enough to see it at the workstation's pace (726 stored in
  5 minutes, 2.4 a second, so about 28 minutes for 4,000).
- **The harness waits as the agent does:** a 90 s idle limit and a check-in every 30 s, the agent's
  own (`IDLE_TIMEOUT_SECS`, `KEEP_ALIVE_SECS` in `agent/crates/mesh-agent/src/client_tls.rs`).
  Tonight's catch-up leg (46040518476) lost 4 machines and 57 calls to the harness's 30 s limit.
- The commit-check work (`commit-checks-pass-record.md`, built) rides in this plan's first commit,
  which deletes that plan with its `phases.md` row (Ivan, modal, 2026-11-26).
- The plans `busy-morning-7763-ab.md` and `direct-speed-slower-processors.md` are retired (Ivan,
  2026-11-27) and leave in this plan's first commit; the rule that keeps speed nights off the
  AMD EPYC 7763 is a plan of its own, after it.

## Steps, each test first

1. The shared receive budget — built: `server/internal/agentapi/receive_budget.go`, wired in
   `server.go` (`receive` field, `machineQUICConfig(s.receive)`, `s.receive.watch(conn)` after
   Accept); seven tests in `receive_budget_test.go` (a machine the server has not read parks only
   its opening window, 524,288 bytes on quic-go's defaults; growth only while the budget has room;
   a closed connection's share comes back). ADR-044 carries "What the transport holds before a read
   is bounded too". Done: the agentapi package passes, and the rehearsal on the fix kept the server
   alive through the inventory phase (return 95% online in 49 s, all in 62 s).
2. The harness's connection settings: one function in `server/tests/loadtest/agent.go` builds the
   dial's QUIC settings (90 s idle, 30 s keep-alive), and `serveOneConnection` dials with it. A test
   in `agent_test.go` reads the two constants from `client_tls.rs` and holds the harness's settings
   equal to them, so the harness cannot drift from the agent unseen. Done: the test fails on the
   30 s idle limit and passes on the change.
3. The inventory minutes: `InventoryBurst` asks the server's stored count at every 5 s mark from
   the phase's start until it is reached (`runOnePhase` sleeps through its inventory watch, on the
   run's clock), and the phase result carries `inventory_stored_all_seconds`, absent when the count
   never reached the phase's machines. `scripts/loadtest-bundle-rows.sh` writes it as
   `inventory_all_seconds` on the `quic/inventory/<phase>` row. Tests: in `inventory_burst_test.go`,
   on the test clock, the seconds from the phase's start to the first mark that saw the count reach
   the machines, and absence when it never did; in `loadtest-bundle-rows.test.sh`, the row carries
   the field. Done: both fail before the change and pass after.
4. `load/profiles/burst.yaml`: the inventory phase runs 40 minutes; `inventory_all_seconds` is
   limited at the phase's 2,400 s, not blocking, so the row is read before it is enforced; the
   profile's version goes to 3, since its load changed, and its load is recorded in
   `scripts/tests/fixtures/profile-load-versions.json`. The burst leg's `hold` in `perf-shapes.yml`
   goes from 8m to 43m, since each machine leaves once its hold ends and the profile now runs 40.5
   minutes; the leg's 60-minute job limit stays. `burst_profile_test.go` holds the phase length and
   the limit; `Non-Functional-Testing.md` ("The burst leg") says what the new measurement reads.
   Done: the profile test, `loadtest-bundle-rows.test.sh`, `loadtest-gate-series.test.sh` and
   `profile-load-version.test.sh` pass.
5. Commit 1 (steps 1-4 with the commit-check work and the two retired plans removed): `/refactor`,
   the gauntlet in the background, the commit named `Plan: .claude/plans/burst-memory-fix.md`.
   Done: the commit is on `origin/dev`.
6. The hand-run: `perf-shapes.yml` dispatched on commit 1 (it has no input to pick a leg, so all
   four legs run, as on 2026-11-26), and its ARM burst leg read. Done: its job and steps read by
   result, the server alive, `inventory_sent` and `inventory_stored` at 4,000, and
   `inventory_all_seconds` read from its bundle.
7. Commit 2: `burst.yaml`'s `inventory_all_seconds` limit becomes the hand-run's seconds rounded up
   to the whole minute, blocking; `burst_profile_test.go` holds it; ADR-044 names the promise with
   the run that measured it; this plan is deleted with its `phases.md` row. Done: the commit is on
   `origin/dev`.

## Done when

- The ARM burst leg on the fix stores all 4,000 inventories and its minutes are the blocking limit
  in `burst.yaml`.
- This plan is deleted in commit 2, with its `phases.md` row.
