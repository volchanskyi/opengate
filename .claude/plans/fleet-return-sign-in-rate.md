# A returning fleet's sign-in rate, and a catch-up turn asked again

Split out of the numeric-ingest master plan's §12 item 1 (Ivan, modal, 2026-11-12:
"Rate promise now, faster later"). Commit 4 holds the all-at-once return to its
fleet online (95% within 3 minutes, every machine within 5, none refused), with a
machine's own sign-in in that wave watched. This plan raises the rate and closes
the one gap the rehearsal left.

## What the rehearsal read (2026-11-12, workstation, commit 4's tree)

5,000 machines back after an hour away over one minute, the server at a quarter
processor and 384 MiB, the store at production's limits:

- Online: 500 at 19 s, 2,500 at 59 s, 4,500 at 105 s, 4,900 at 148 s, the whole
  fleet at 198 s after the return began; about 46 machines a second through the
  bulk. None refused.
- The server's sign-in timer: median 22 s and slowest 5% 55 s over the return,
  mean 127 s, many past its 60 s top (`agent_registration_duration_seconds`,
  accepted `AgentRegister` frame to the device row written and online).
- The return's two minutes of processor, server at 97.6% busy: sign-in database
  round trips 27% (`dbtx.Scoped`, each statement and `set_config` its own trip),
  the alert-rule push 10% (`pushAlertRules` reads `CatalogueAlertRuleProvider.RulesFor`
  per sign-in), TLS handshakes 11%, garbage collection 45–53% of minutes 2–5.
- Allocation, minutes 2–10 (after the encoder fix): `encodeReadings` 933 MB, the
  writer's request body 793 MB (`sendOnce` copies every queued batch into one
  buffer), `DecodeControl` 669 MB, `ReadPayload` 248 MB, `backfillSamples` 232 MB,
  of 3.9 GB.
- 3 of 5,000 machines never caught up: in the saturated minute their request for
  a catch-up turn went unanswered within the harness's wait, and the harness, like
  the agent (`backfill_loop.rs` waits for its next reconnect in `AwaitingGrant`),
  does not ask again.

## Work, each step test first

1. A turn request unanswered is asked again: the agent, after a deadline with
   jitter, and the harness the same way. Industry check before the deadline is
   chosen (Netdata's fixed reconnect delay, NATS jitter, the agent's own
   `full_jitter`); the deadline goes to Ivan as a product promise.
2. Fewer database trips per sign-in: the statements of one sign-in in one
   round trip where the driver allows (`pgx` batch), counted by a test against a
   real database.
3. Alert rules read once per customer and refreshed when they change, with a test
   that a changed rule reaches the next sign-in.
4. Less memory churn: the writer hands the store its queued batches without
   copying them into one body; pooled payload and encode buffers where the
   lifetime is provable. Each with an allocation bound (`testing.AllocsPerRun`).
5. The rehearsal again (`~/.cache/opengate-commit4-readings/catchup.sh`),
   compared with the figures above; any limit change goes to Ivan.
6. The read allowance under a burst: a harness mode where every one of 5,000
   machines sends its largest message at once, at production's limits; the server
   stays under its memory limit and keeps its machines online (commit 4 set the
   allowance to 5% of Go's memory limit from a decode measurement, Ivan, modal).

## Done when

- The catch-up night passes `machines_caught_up` on the workstation rehearsal and
  on the ARM nightly.
- The fleet-online figures and the sign-in timer are recorded here before and
  after each step.
- This plan is deleted in the commit landing its last step, with its `phases.md`
  row.
