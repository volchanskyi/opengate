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
  bulk, against about 79 a second arriving (5,000 in a 63 s window). None refused.
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
- 3 of 5,000 machines never caught up. The run read `Catch-up: 9994 stored, 45 told
  to wait, 0 never answered`; 9,994 = 4,997 × 2, so the three stored nothing.

## What the first domain nightlies read (2026-11-13, commit 4 on `main`)

Folded in by Ivan (modal, 2026-11-13). Each reading below is part of step 9's before and after.

- Catch-up on ARM (run 43449781521): 4,986 of 5,000 machines caught up, 3 batches
  unanswered, live minutes within 5 s 94.3% (limit 99%), connect p95 533 ms (500). The
  technicians' relay journey never started: its setup's account sign-up got `503 request
  timeout` after up to 30 s, so every `k6/*` row of the leg is missing.
- Volume at 8,000 machines (run 43452952415, runner stack): sign-in median 193 ms against the
  100 ms limit (five earlier nights read 220–255 ms against a 500 ms limit), slowest 5% 642 ms;
  `device_list` p95 3.4 s, `device_detail` p95 3.3 s; **database 93.7% busy**. Steps 4–5 cut the
  database's work per sign-in, which is the same load.
- Spike (run 43451639284): database 98.7% busy, server 80%; sign-in median 149 ms during the
  spike.
- Breakpoint recovery (run 43451639284): after the ladder gave and the fleet drained, 500 machines
  signing in took a median of 24 s and slowest 5% 57 s over the 5-minute recovery, server busy
  56% → 99.4%. The system did not come back on its own, and step 10's recovery verdict covers it.

## What the review read in the code (2026-11-12)

- The server answers every turn request with a grant
  (`handleRequestBackfillSlot` → `RequestSlot`); admission happens per batch.
- The harness sets no read deadline on the turn answer (`awaitSlot`), so its
  "no answer" branch fires only on a dead connection, and the run had 0 failures.
  It quits after `maxDeferrals` (8) "wait" answers on one connection
  (`soak_backfill.go`); the agent retries without limit (`on_defer`). The 45 "wait"
  answers fit three machines told to wait nine times each, unproven.
- A sign-in runs 7 transactions of 4 trips each (begin, `set_config`, statement,
  commit): `devices.Upsert`, `settings.ScopeFor`, `ListBindings`, `ListRollouts`,
  `TagsFor`, `alerts.Limits`, the maintenance read, plus `Counts` when a rollout
  is staged. The server reaches PostgreSQL through `database/sql` over
  `pgx/v5/stdlib`.
- A machine's rule list depends on its own tags and its place in a staged
  rollout, so a cache holds the customer's inputs (bindings, rollouts, alert
  budget), cleared where `RefreshAlertRules` already runs. The server runs as one
  replica (`deploy/helm/opengate/values.yaml`).
- Nothing limits how many sign-ins run at once.

## Decided in review (Ivan, modal, 2026-11-12)

- **The cause of the three is proven before any fix** (step 1).
- **The speed work gets a finish line from research**: how RMM and device-fleet
  products state reconnect speed, brought to Ivan as a promise.
- **The database trips per sign-in**: research how comparable products cut them,
  then choose between the sign-in write handing back what it touched with the
  other reads in one transaction, the driver's batch, or both.
- **A cap on sign-ins at once**: research, then decide.
- **The speed promise is a sign-in rate** (Ivan, modal, after R1): "the server signs
  in at least N returning machines a second at production's size", the Azure and
  AWS form; a fleet's return time is its size divided by the rate. The 5,000-machine
  promise (95% within 3 minutes, all within 5) stays. N comes to Ivan next.
- R2 widened (Ivan): more popular products, with pros and cons per approach.
- R3 widened (Ivan): Netdata and Vector, with pros and cons per approach.
- **The rate is 100 returning machines a second at production's size** (Ivan,
  modal, after R1): 5,000 within 50 s, 16,000 within about 2.7 minutes; Azure IoT
  Hub's smallest paid tier promises the same floor.
- **Sign-ins wait in a line sized by the processor** (Ivan, modal, after R3): a set
  number in progress, sized from the server's processor share as its memory limit
  is from its container; the rest wait with their connections kept, served in
  turn by customer; a machine is told when to come back (a new message, obeyed
  with jitter) only when the line is full.
- **Customer-wide settings live in memory, loaded per customer** (Ivan, modal,
  after the implications): rule overrides, rollouts and the alert budget are read
  before the server takes machines, each customer inside its own boundary so the
  database still guards the read; memory is updated on every change through the
  product; a version stamp makes a sign-in that raced a change send again. A
  sign-in reads only the machine's own rows in one conversation. Holds for one
  server copy: production's server is replaced, never run beside itself
  (`Recreate` in `server-deployment.yaml`).
- **A change made outside the product is picked up by a restart** (Ivan, modal,
  once the real cases were laid out: hand SQL in production does not happen there,
  restores have no procedure, and a release's database changes run inside the
  server before it takes machines). No timed re-check.
- **An alert-budget change reaches connected machines** (Ivan, modal): found in
  review — `PutAlertLimits` stores the budget and pushes nothing, so a connected
  machine keeps the old budget until it reconnects. A budget change re-sends the
  customer's rules, as an override change does.
- **The line holds 30 seconds of work** (Ivan, modal): about 3,000 machines at 100 a
  second; past that a machine is told to come back after a random wait up to the
  line's length (Azure DPS's rule).
- Rollout stages move only through the product (`RolloutPercent` changes by the
  gated advance in `rules/gate.go`), so a stored rollout read from memory gives the
  same stage as one read from the database.
- **The rate check goes on first** (Ivan, modal, after R4): step 8 lands before step
  9 measures; a night under 100 a second fails, and that night is the finding that
  brings the size question at step 9. Azure IoT Hub and AWS IoT publish the rate
  and build to it.
- **A turn request with no answer is asked again** (Ivan, modal): after about 60 s
  plus a random few seconds per machine, by the agent and the harness, every repeat
  counted. Step 2 still proves the three's cause first. gRPC, Vector and Netdata
  each give a request 60 s.
- **A sign-in's own rows travel in one conversation** (Ivan, modal): one
  transaction of about 5 trips, the customer boundary as today. The driver's batch
  returns only if step 9 is still short.
- **Every agent understands "come back later"** (Ivan, modal: all agents will be
  reinstalled): the server sends it with no check of the agent's capability list.
- **The number in progress is measured and written down** (Ivan, modal): the rule
  per processor and its readings go in the decision record with step 6. A machine
  turned away waits a random 0–30 s.
- **Each new test sits in the tier ADR-081 names** (Ivan, modal): acceptance gains
  two tests in `rule_changes_reach_machines_test.go`, each named for its
  behaviour, the opening the four share in the one setup helper step 11 leaves
  (Google's Go style: separate functions where the logic differs, helpers for
  setup). The rest go to real-database, unit, integration and load tests.
- **The acceptance tier's own four gaps are fixed in commit 1** (Ivan, modal):
  checks read what a technician sees, "nothing happened" waits for a later
  marker, a message says only what its check proves, one setup helper (step 11).
- **The burst safety case is judged on recovery** (Ivan, modal): memory under the
  limit, nobody offline, and within 5 minutes every message stored and live
  readings back within 5 s, the fleet-return promise's 5 minutes.

## Research, before the steps it gates

- R1. Reconnect promise: what RMM agents, IoT hubs and message brokers publish
  for a fleet returning at once (rates, limits, back-off on refusal).
- R2. Sign-in database cost: how comparable servers keep per-connection setup
  cheap under row-level security (folded reads, one transaction, pipelined batch,
  cached scope).
- R3. Sign-ins at once: how brokers and IoT hubs limit simultaneous or per-second
  connections, what the device is told, and what it does next.
- R4. A request left unanswered, agents that predate a message, a burst's pass
  mark, and whether a promise is checked before it is measured.

### What the research found (2026-11-12)

| Product | Reconnect promise | Limit on sign-ins | Told when over | Sign-in work |
|---|---|---|---|---|
| Azure IoT Hub ([quotas](https://learn.microsoft.com/azure/iot-hub/iot-hub-devguide-quotas-throttling)) | a rate: S1 at least 100 new connections/s, S3 6,000/s per unit; "to connect 100,000 devices, it takes at least 1,000 seconds" | rate, separate from how many stay connected | burst served at once, then queued at the rate ("traffic shaping"), `429` only when the queue fills | — |
| Azure DPS ([at scale](https://learn.microsoft.com/azure/iot-dps/concepts-deploy-at-scale)) | — | — | device waits `retry-after`, else a random delay up to fleet size ÷ service rate | a returning device reuses what it cached; it does not register again |
| HiveMQ ([limits](https://docs.hivemq.com/hivemq/latest/user-guide/restrictions.html)) | — | connect rate per listener, burst twice the rate; a cap on TLS handshakes in progress (for bulk reconnects) | stops reading new sockets and disconnects newcomers | — |
| EMQX ([rate limit](https://docs.emqx.com/en/emqx/latest/rate-limit/rate-limit.html)) | — | `max_conn_rate` per listener, default none | pauses accepting | — |
| AWS IoT Core ([2018 limits](https://aws.amazon.com/about-aws/whats-new/2018/03/iot-core-increases-default-limits)) | a rate: 500 connects/s per account (2018 figure) | rate | refused | — |
| MeshCentral ([meshagent.js](https://github.com/Ylianst/MeshCentral/blob/master/meshagent.js)) | — | none for sign-ins (a limiter only for agent updates) | — | one device read; device groups and users served from memory; a write only when the device changed |
| PostgREST ([#1941](https://github.com/PostgREST/postgrest/issues/1941)) | — | — | — | folds every per-request setting into one `set_config` call; an extra round trip counts as the cost to avoid |
| pgx ([batch](https://pkg.go.dev/github.com/jackc/pgx/v5)) | — | — | — | a batch is one round trip in one implicit transaction; a `set_config(…, true)` at its head covering the rest needs a test to prove it |

#### R2 widened: where a sign-in's settings come from

| Approach | Who | Gains | Costs |
|---|---|---|---|
| Settings held in memory, changed when the owner changes them | MeshCentral (groups, users); Kubernetes API server ([watch cache](https://kubernetes.io/blog/2025/09/09/kubernetes-v1-34-snapshottable-api-server-cache/), kept current by a change feed) | a sign-in reads only the machine's own rows; cost stays flat as customers grow | memory per customer; the cache starts cold after a restart, the moment a whole fleet returns ([KEP-1904](https://www.kubernetes.dev/resources/keps/1904/)); more than one server copy needs a change feed between them |
| Settings held in memory, reloaded on a timer | Zabbix ([configuration cache](https://www.zabbix.com/documentation/8.0/en/manual/concepts/server/server_params), `CacheUpdateFrequency`, changes only since the first full load) | simple; no change hook needed | a change waits up to one reload period ([Zabbix blog](https://blog.zabbix.com/reloading-configuration-cache)); full reloads cost the database in bursts |
| Fewer, fuller conversations | PostgREST ([#1941](https://github.com/PostgREST/postgrest/issues/1941)) | nothing can be stale; no memory | still one statement per read |
| One envelope | pgx ([batch](https://pkg.go.dev/github.com/jackc/pgx/v5)) | fewest trips; nothing stale | a lower-level connection; the customer boundary inside the envelope needs its own proof |
| Write only what changed | MeshCentral | no write for an unchanged machine | a returning machine still flips to online, so its row is written anyway |

#### R3 widened: Netdata, Vector and Kubernetes

| Product | How it limits | What the sender does | Gains | Costs |
|---|---|---|---|---|
| Netdata parent ([streaming](https://learn.netdata.cloud/docs/observability-centralization-points/streaming-and-replication-reference), [README](https://sources.debian.org/src/netdata/1.37.1-2/streaming/README.md)) | `accept a streaming request every seconds`, default 0 = none; when hit, "too busy to accept new streaming request" | retries after `reconnect delay`, default 5 s | one number; a parent of 600+ children needed it to survive reconnects ([forum](https://community.netdata.cloud/t/frequent-crashes-on-netdata-parent-high-throughput-high-child-count/2952)) | one a second puts 5,000 machines 83 minutes out; each turned-away child pays its handshake again |
| Vector ([buffering](https://vector.dev/docs/architecture/buffering-model/), [ARC](https://vector.dev/blog/adaptive-request-concurrency/)) | none refused: a full buffer stops the receiver reading, and the pressure travels back | each sender raises how many requests it has open while round trips stay flat and cuts it sharply when they rise or it hears 429/503 | no number to tune; Vector found fixed rate limits "were limiting performance by over 80%" and the right value moves with fleet size | built for a sender with a steady stream of requests; work held open piles up at the edge |
| Kubernetes API server ([priority and fairness](https://v1-34.docs.kubernetes.io/docs/concepts/cluster-administration/flow-control)) | a set number in progress, the rest queued fairly between flows, 429 once a queue is full | waits, or retries after the answer | bounded memory; one heavy caller cannot starve a light one | the most machinery |

#### R4: a request unanswered, older agents, a burst, the order of a promise

| Product | Behaviour |
|---|---|
| gRPC ([deadlines](https://grpc.io/blog/deadlines/)) | "always set a deadline"; with none, a call waits forever |
| Vector ([ARC](https://vector.dev/blog/adaptive-request-concurrency/), [buffering](https://vector.dev/docs/architecture/buffering-model/)) | 60 s per request, retries from 1 s growing to 30 s; a full buffer makes senders wait, no lateness promise |
| Netdata ([reference](https://learn.netdata.cloud/docs/observability-centralization-points/streaming-and-replication-reference)) | child waits 60 s for its parent, reconnects after 5 s; a full 10 MB send buffer closes and reopens the link |
| MeshCentral ([meshagent.js](https://github.com/Ylianst/MeshCentral/blob/master/meshagent.js)) | picks what to send from the agent's capability bits; holds connections above `maxagentsessions`; core push ahead of binary updates in its task limiter; no wait limit on agent replies; 3–4 reads per connect |
| NinjaOne ([incident, 2026-07-24](https://status.ninjaone.com/incidents/1lr9nq07x604)) | agents reconnect slowly with random waits; device-status processing was the bottleneck; about 10 hours to resolve; data kept on the machine and resent |
| HiveMQ ([limits](https://docs.hivemq.com/hivemq/latest/user-guide/restrictions.html)) | `concurrent-handshake-limit` for bulk reconnects, off by default |
| MQTT 5 | CONNACK 0x89 "server busy", 0x9F "connection rate exceeded"; 3.1.1 clients only see "unavailable" and reconnect |
| Grafana k6 ([spike recovery](https://grafana.com/docs/learning-paths/k6-spike-testing/measure-recovery/)) | a spike graded on recovery: under 30 s fast, 30 s–3 min slow, or none |
| Azure IoT Hub ([throttling](https://learn.microsoft.com/azure/iot-hub/iot-hub-devguide-quotas-throttling)) | a burst queued at the rate, delays rise before any refusal |

Reading: the platforms built for fleets state reconnect speed as a rate, so a
fleet's return time is its size divided by that rate. They absorb a burst, queue at
the rate, and refuse with a retry time only when the queue is full. The one RMM
in the table keeps per-customer settings in memory and does one device read per
sign-in.

## Resource budget at production's size (Ivan asked, 2026-11-12)

Production (`values-production.yaml`): server 250m processor and 384 MiB (Go's
memory limit 90% of it); PostgreSQL 175m requested, one processor at most, 384 MiB.
The node had about 0.19 processors and 6.6 GiB unreserved when read on 2026-11-08.

- **Processor.** 100 sign-ins a second on a quarter processor leaves 2.5 ms of
  processor per sign-in, everything included. The rehearsal spent about 5.3 ms
  each (97.6% busy at 46 a second), of which database trips about 1.4 ms, rule
  push 0.5 ms, secure handshake 0.6 ms, and garbage collection about half,
  driven mostly by catch-up. After steps 4–5 a sign-in alone is estimated at
  about 1.2 ms, so 100 a second takes about half the processor; catch-up ingest,
  live readings and garbage collection share the other half. Whether sign-in
  goes ahead of catch-up when both wait is undecided.
- **Memory.** A connected machine holds about 38 KB in the copy of quic-go after
  commit 4's fix (25 KB of its state plus three goroutine stacks, from the
  library benchmark) and read about 58 KB in the server before it; 5,000 machines
  hold about 190–290 MB of a 346 MiB limit before queues (4 + 8 MiB), the read
  allowance (about 17 MiB) and the program itself. A machine waiting in the line
  holds its connection either way, so the line costs no more than today's
  everyone-at-once; turning a machine away frees it. At 384 MiB the server holds
  roughly 6,000–8,000 machines, so 16,000 is a figure for a larger server.
  Memory near the limit is also what drives garbage collection.
- **Database.** 100 sign-ins a second at about 5 exchanges each is about 500
  statements a second, against about 1,300 a second today (46 × 28).
- **Settings in memory.** Grows with customers' overrides and rollouts, not
  machines.
- **What follows** (Ivan, modal): step 9's rehearsal records processor per sign-in
  and memory per connected machine after the fixes; then the options go to Ivan —
  keep production's size and state its fleet ceiling, give the server more memory
  (free on the node), or let sign-ins go ahead of catch-up.

## Work, each step test first

1. Timers that reach the 5-minute promise (Ivan, modal): the server's sign-in
   buckets (`registrationDurationBuckets`) run to 300 s; the harness times a
   sign-in from its send to the server's first answer, so a wait in the line is
   counted. The harness figure changes meaning, one break in its history. The
   "before" readings are taken with these timers.
2. Why the three stopped: the harness records per machine why its catch-up ended
   (turn never granted, told to wait N times, connection ended, run ended); the
   rehearsal runs again; the fix follows the cause. Then a turn request with no
   answer within about 60 s, plus a random few seconds per machine, is asked
   again by the agent and the harness, each repeat counted.
3. An alert-budget change re-sends the customer's rules to its connected
   machines. `Rule-Administration.md` already promises the change "lands on the
   next alert"; `PutAlertLimits` sends nothing. Acceptance first: a changed alert
   budget reaches a machine that never disconnects.
4. Customer-wide settings in memory: loaded per customer inside its boundary
   before the server takes machines; updated by every product change (overrides,
   rollouts, alert budget); a version stamp re-sends to a sign-in that raced a
   change. Tests: a changed rule reaches the next sign-in and another customer's
   never does (the existing acceptance tests); a removed tuning leaves a machine
   that never disconnects (acceptance); settings stored before a start reach the
   first sign-in (against a real database, the server-assembly test holding the
   load ahead of taking machines).
5. A sign-in's own rows in one conversation (one transaction, about 5 trips,
   the customer boundary as today): the device write hands back the
   machine's place and maintenance flag, then its tags, and the customer's
   machine count while a rollout is staged. The reads (only the customer's own
   machine) and the trips proven against a real database.
6. The sign-in line: a number in progress sized from the server's processor share
   (measured on the rehearsal; the rule per processor and its readings written
   into the decision record), the rest waiting with their connections kept and
   served in turn by customer, up to 30 seconds of work; past that a "come back
   later" message with a random wait of 0–30 s, sent to every agent, in Go and
   Rust with goldens both ways, obeyed by the agent and by the harness. Tests:
   the line's order and turn-away with a fake clock; "come back later" over a
   real connection; the rate and the 30-second line in the load tests. No new
   server setting.
7. Less memory churn: the writer hands the store its queued batches without
   copying them into one body; pooled payload and encode buffers where the
   lifetime is provable. Each with an allocation bound (`testing.AllocsPerRun`).
8. The rate promise as a limit: sign-ins at 100 a second over the return, in the
   catch-up profile and the ARM nightly, on from the start; a night under it
   fails.
9. The rehearsal again (`~/.cache/opengate-commit4-readings/catchup.sh`),
   compared with the figures above, recording processor per sign-in and memory
   per connected machine; any limit change and the resource options go to Ivan.
   The nightlies' 2026-11-13 readings are read again after steps 4–8: the
   8,000-machine sign-in median and database share, the spike's database share,
   the breakpoint's recovery sign-ins, and the catch-up leg's technician sign-up.
10. The burst, two cases at production's limits (Ivan, modal). The real peak:
    5,000 machines returning while 20 technicians each pull a log page; live
    readings within 5 s, every log page shown within 30 s, every machine online.
    The safety case: every machine sends a 1 MiB message at once; memory stays
    under the limit, no machine goes offline, and within 5 minutes every message
    is stored and live readings are back within 5 s (commit 4 set the read
    allowance to 5% of Go's memory limit, Ivan, modal). Other products
    cap a message (Azure IoT Hub 256 KB, AWS IoT 128 KB, NATS and EMQX 1 MB by
    default) and make senders wait, which is this design.
11. The acceptance tests held to the practice the review read (*Software
    Engineering at Google* ch. 12, Fowler's "Eradicating Non-Determinism in
    Tests"): `AwaitOnline` and the device-record checks
    (`machine_certificates_test.go`, `data_erasure_test.go`) read the device list
    through the web API; each "nothing happened" check (`require.Never`,
    `assert.Never`, the timed loops in `data_erasure_test.go`) waits for a later
    marker to arrive, then checks absence; the expired-machine check confirms
    nothing was stored, as its message says; `onlineAtContoso` and
    `contosoOnlineMachine` become one helper.

## Commits (Ivan, modal, 2026-11-12: four)

1. Steps 1–3 and 11: the timers, the three's cause and its fix, a turn request
   asked again, the alert-budget push, the acceptance tests' own fixes.
2. Steps 4–5: customer-wide settings in memory, a sign-in's own rows in one
   conversation.
3. Steps 6–8 and 10: the sign-in line and "come back later" in Go and Rust, less
   memory churn, the rate check, the burst.
4. After the first ARM night and step 9's readings: what the open question needs,
   the readings in the decision record, this plan deleted with its `phases.md`
   row.

## Open, for Ivan

- Whether a sign-in goes ahead of catch-up when both wait, the server's size, and
  the driver's batch if sign-in is still short: brought after step 9's readings.

## Done when

- The catch-up night passes `machines_caught_up` and the 100-a-second sign-in rate
  on the workstation rehearsal and on the ARM nightly.
- The fleet-online figures and the sign-in timer are recorded here before and
  after each step.
- This plan is deleted in the commit landing its last step, with its `phases.md`
  row.
