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
- **The line knows a waiting machine's customer from a note** (Ivan, modal, 2026-11-20, after
  pros, cons, speed and upkeep): a returning machine shows only its device number, so the server
  keeps each machine's customer, read at start per tenant beside the customer settings and
  updated by every sign-in, dropped on erasure; a machine the note lacks waits in a turn of its
  own. The note orders the line only; rules come from the sign-in. A full line turns away the
  newest of the customer holding the most of it. Kubernetes' API server knows its caller at the
  door; AWS IoT and Azure IoT Hub limit per account or hub, which the address tells.
- **The catch-up return comes back in 30 s** (Ivan, modal, 2026-11-20, after the history check):
  a rate shows only when more than it is offered, and the one-minute return offered about 83 a
  second. The return brings all 5,000 within 30 s (a release restart's shape: agents redial within
  their 1–30 s wait); the rate is sign-ins finished per second while any machine waited, at least
  100, blocking. volume-8000's 30 s ramp (backed out in `27e2ba53`) failed on enrolment and filing
  in the walk; catch-up installs and files before the clock. Handshakes run before the line, about
  40% of a quarter processor at 167 a second; a night under 100 is step 9's size finding.
- **The line holds 30 s of the pace it measures** (Ivan, modal, 2026-11-20, after the sizing
  readings): the pace is read from the slots handed straight to a waiting machine, the promised
  rate standing until it has one (Little's law; Azure DPS spreads by the rate achieved). Sizing at a
  quarter processor, 5,000 back within 30 s: 1, 2, 4, 8 at once read 29, 31, 28, 18 sign-ins a
  second, so 8 per processor; the processor was full throughout, nobody new got in for about 50 s
  after the first ~750 while their catch-up ran, and ~2,000 were told to come back.
- **The burst runs on a second spike leg, on ARM at production's limits** (Ivan, modal,
  2026-11-20): the shapes nightly keeps its x86 spike and gains an ARM spike leg beside it, through
  the shared leg workflow's ARM switch and production's limits, with 5,000 machines: the return
  with technicians pulling a log page a second, then every machine's 1 MiB package inventory (the
  one machine message allowed that size, sent by every machine after a fleet-wide reinstall), then
  the recovery.
- **The safety case sends a 1 MiB inventory, stored in one statement** (Ivan, modal, 2026-11-20):
  no real agent sends 1 MiB (its inventory caps at about 5,200 entries, roughly 300 KB), so each
  machine sends an inventory padded to the server's 1 MiB message ceiling, testing the ceiling and
  the read allowance at their limit. The server wrote an inventory one row per round trip (about
  5,200 trips for one full report, hours for a fleet-wide reinstall, reports past a connection's
  write slots dropped); commit 3 writes a whole inventory in one statement, its trips counted
  against a real database (PostgreSQL and pgx: many rows per statement).
- **Step 13's findings** (2026-11-20, A/B on one machine, builds interleaved): the alert check was
  real and fixed in commit 1; the control encoder (+12%) and the file-piece relay (+11%) are code
  placement (same function sizes, moved addresses; the relay's code unchanged); the agent store's
  commit (+19%) is real, the price of the raw-read index that made an hour's read 3.4x faster.
  Ivan (modal): trim the index upkeep (skip an unchanged label, keep the tail's in memory), no
  format change, measured before and after.
- **The start-up reset covers every tenant** (Ivan, modal, 2026-11-20, found wiring the note): it
  ran in the default tenant only, so other tenants' machines stayed online after a restart; it now
  runs inside each tenant's boundary, as ADR-130 states.
- **The burst safety case is judged on recovery** (Ivan, modal): memory under the
  limit, nobody offline, and within 5 minutes every message stored and live
  readings back within 5 s, the fleet-return promise's 5 minutes.
- **The burst's log pages come a page a second** (Ivan, modal, 2026-11-20, after CI on
  `13506f53`): twenty at once fired before any machine was back and fit neither kind of
  technician load the test kit reads. Technicians open a log page a second through the 30 s
  return, all 30 owed within 30 s; the return is the measured phase, so the server and database
  watches read over it, and the returning wave gains two watches (all online, 95% within 3 min).
- **The pre-commit checks time their steps on a forward-only clock** (Ivan, modal, 2026-11-23,
  found landing the fast-follow): the step and total timers read the wall clock, which steps back
  1-2.5 s on the workstation, so two steps printed `(-2s)`. Display only; folded into commit 4.
- **Sign-ins go first when the processor is short** (Ivan, modal, 2026-11-24, after step 9's
  rehearsal): while machines wait to sign in, console requests wait in a short line of their own,
  a few at once, so the returning fleet gets the processor first (Kubernetes' API Priority and
  Fairness keeps node check-ins in a priority level of their own). In commit 4.
- **The rate promise is decided after the fix** (Ivan, modal, 2026-11-24): commit 4 lands the
  priority fix; the next ARM night's rate brings the promise and the server's size back to Ivan.
- **The driver's batch is dropped; sign-ins ahead of catch-up stays open** (Ivan, modal,
  2026-11-24): the database ran 6% busy on the ARM catch-up night and 13% at 8,000 machines;
  catch-up's 8-13% of the return is read again on the next ARM night's profile.
- **The console line's shape** (Ivan, modal, 2026-11-24): every signed-in console request waits
  (a machine's enrolment, health checks and live sessions do not), only while a machine waits in
  the sign-in line; a request waiting past the API's 30 s limit gets 503 with a retry time. How
  many run at once is a number per processor, rehearsed at 1 and 2 at a quarter processor with
  technicians working, keeping the one where sign-ins reach their rate while the fleet page loads.
- **The console line holds one place per quarter processor; commit 4 lands, the ARM night next**
  (Ivan, modals, 2026-11-24, after the rehearsals `c1` and `c2`): 1 and 2 places read the same
  sign-in rate (about 10 a second in the return, 3.2 after), 495 against 609 machines failing to
  connect; the next cut (the fleet page's health-rate lookup, 29% of the processor, or catch-up
  waiting for sign-ins) comes back with the ARM night's numbers.
- **A leg records why its server stopped** (Ivan, modal, 2026-11-24): a server not running at a
  leg's end has its exit code, whether it was killed for memory and its last log lines written into
  the run's evidence. In commit 4.
- **The install scripts retry every failed download** (Ivan, modal, 2026-11-24): the shell-tools,
  release-tools and dump-tools installs retry any error with a pause, as the dependency check does;
  GitHub answered 403 once on CD 45316115369's first attempt. In commit 4.
- **A "come back later" counts apart, with no cap** (Ivan, modal, 2026-11-24, after the research:
  AWS IoT counts connects as Success, Throttle, ClientError and ServerError; Azure IoT Hub counts
  attempted, succeeded and throttled; k6's expected statuses leave the failure rate; Google SRE
  writes down which events count): the harness files a turn-away with the refusals made on purpose
  and publishes it as its own number; a night is judged on its rate and its time-online promises,
  and "none refused" reads as none refused for good. In commit 4.
- **Both databases log a slow statement and a lock wait** (Ivan, modal, 2026-11-24, the staging
  device-list item): the list plans in 3.9 ms and runs in 2.3 ms over 407 machines as the app's
  login with the tenant set, and its daily median read about 3 ms for 16 days. One window was slow,
  11-23 16:51-16:57 in CD 45133238985's browser tests: 28 of 58 list calls took 0.5-1 s (about
  630 ms each), with no connection-pool waits, the processor idle, 2 machines, every other call
  normal; CD 45316115369 read 3 ms. Neither log is on, so the window cannot be explained. The
  database container runs with `log_min_duration_statement=1000` and `log_lock_waits=on` (Heroku
  logs past 2 s by default; PostgreSQL 19 turns lock-wait logging on by default). Folded into
  commit 4; the pod template change restarts each database once at its deploy.
- **Commit 4's first check run failed twice; the workstation's clock and a test are fixed**
  (Ivan, modals, 2026-11-24): the machine-lifecycle browser test and a session-tab unit test. The
  browser test's machine removed itself on time; Docker 29.3.0 dropped the container's exit
  because the workstation's clock stepped back 0.6 s every 32 s (Ubuntu's time service against Windows' push),
  and a hunt caught the same drop freezing a helper. Ivan turned the Ubuntu time service off; no
  time limits go into the helpers. The session-tab test waits for the page's timers before moving
  the clock, and the gauntlet names a failed check's real exit code. In commit 4.
- **After the first ARM night on commit 4** (Ivan, modals, 2026-11-25, after the research: k6 starts
  each stage where the last target ended, and a generator that eases off hides the slowness; the
  Prometheus remote-write spec retries a 5xx with a growing wait, Datadog's agent with jitter, the
  OpenTelemetry collector from 5 s to 30 s for 5 minutes; Datadog throttles backlog recovery so old
  data does not preempt live, Kubernetes keeps node check-ins in a level of their own):
  - a phase climbs from the level the fleet holds (step 22) and the trend push is retried, then
    read back (step 23), in a commit of their own before commit 5; then the ARM catch-up and burst
    legs run by hand on it;
  - the rate promise is lowered to what that clean run reads on ARM, keeping the 3- and 5-minute
    promises at 4,000 machines (at least 21 a second);
  - production's server keeps a quarter processor and 384 MiB, with a stated ceiling of about
    4,000 machines per server (64-79 KB a machine measured); the catch-up and burst legs run 4,000
    machines, in the tool-fix commit, the ceiling written into the decision record;
  - the next cut is catch-up waiting while machines wait to sign in (step 24);
  - the rate rows stay filed beside an online time (not chosen);
  - commit 5 deletes this plan as decided; step 24 and the rate number move into a plan of their
    own, written before commit 5 lands.

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

## What the nightlies read, 2026-11-13 to 2026-11-17 (Ivan asked for a cross-check)

All on `4e6cbd48` from 11-14. What the plan already covers: the breakpoint's recovery
sign-ins (median 24–49 s), the 8,000-machine sign-in median (160–193 ms, green since
11-15), catch-up's machines caught up (3,948–4,986) and unanswered batches (3–91). Not
covered, fixed first in commit 0 (Ivan, modals, 2026-11-17):

- Load test (staging), invalid every night since 11-14: the summary carries the server's
  and database's level rows (`loadtest-levels.sh`), which `loadtest-run-completeness.sh`
  does not list as owed. The levels become owed scenarios.
- Soak (run 43724679797): two `quic/quic-agents/wave` limits on a profile with no measured
  phase, so no row ever arrives. The two limits go, and the profile check refuses a wave
  limit on a profile with no measured phase.
- Mutation, Rust 91.4 under its 91.5 floor on 11-17 (91.5 on 11-14, same commit): 41
  steady survivors sit in files changed since the floor was set, most in catch-up code
  (`drain/read.rs`, `backfill/mod.rs`, `backfill_loop.rs`, `control_loop.rs`
  `dispatch_backfill`, `device_logs.rs`). Tests kill them; the floor stays.
- Mutation shards past the 90-minute cap: `rust-agent-loops` 76–82 min, cancelled on 3
  of 6 nights, splits in two, and the pre-flight's cost per mutant is read again from the
  measured times. `go-observability-harness` 42–90 min, cancelled once: its swing is 14
  telemetry-writer mutants whose tests waited with no deadline, each holding a worker for
  the 15-minute leash; the waits are bounded and the shard stays whole (Ivan, modal,
  2026-11-18).
- Catch-up, every night: the technicians' sign-up runs as 5,000 machines arrive and gets
  `503 request timeout`, so the relay journey never runs. The technicians' accounts and
  sign-ins exist before the fleet starts (Ivan, modal: k6 creates test data before the
  load; a technician is signed in before a fleet returns). A sign-in step of its own runs
  once the harness says its administrator exists, and the walk waits for its word, on
  every leg running technicians beside the fleet (Ivan, modal, 2026-11-18).
- Catch-up 11-16: the generator was refused the processor for 27.7% of the run (ceiling
  20%); it is refused 15–31% of every drain, and the technicians share its processor.
  The technicians get half a processor and 1 GiB of their own (Ivan, modals), 3.25 of the
  runner's 4 processors.
- Catch-up 11-17: the harness held 4,994 machines, the server 4,988 after 180 s. The
  harness marks a machine online when it writes `AgentRegister`; the server counts it
  after two setup reads (`TenantForDevice`, `devices.Get`), each its own transaction.
  Step 1 moves "online" to the server's first answer as well (Ivan, modal), and step 5
  folds the two setup reads in: a return runs 9 transactions, not 7.

## Found while landing commit 0 (2026-11-18)

Ivan (modal): commit 0 lands as it stands; these two go into commits 1 and 2.

- `TestATunedThresholdReachesOneCustomerAndNotTheOther` failed once in about 27 runs (62
  expected, 95 read) and not again in 58 tries. Cause, proven 2026-11-19 (6 failures in 300
  runs under load, the sign-in captured): the machine was filed under the wrong customer. A
  machine naming no customer took the tenant's oldest by creation time, in the device write,
  the customer store's default and the site fallback; the workstation's clock steps back about
  0.93 s every 31 s, so Fabrikam, made after Contoso, read older, and Contoso's new machine
  joined Fabrikam and got its 95. A failed `ScopeFor` was not the path. Fixed in commit 1 (Ivan,
  modal: "mark the default"): each tenant's default customer is marked once, by its first
  create, a delete of the default hands the mark to the oldest left, migration 023 marks
  today's oldest with one default per tenant held by the database, and all four readers and
  the staging reset read or write the mark; the test then passed 300 of 300 under `-race`,
  across about nine of the clock's steps. A machine whose customer cannot be read is
  turned away (Ivan, modal, 2026-11-19): with step 5's one conversation a failed read is a failed
  sign-in, and the agent reconnects after its random 1–30 s wait, as on a failed device write
  today; the shipped-rules fallback in `settingsScope` goes with commit 2.
- `TestMPSRequestsConserveGoroutinesAndHeap` failed once under `-race` on a heap that fell
  (−1773 B per request against 1024): the three conservation checks hold the slope's size,
  so memory given back reads as a leak. Commit 1 holds the retained side alone.

## What the first nightlies after commit 0 read (2026-11-19, `df4dfdbf`)

Read by job and step result.

- Load test (staging, run 44538860137): valid; `server` and `database` owed and produced, no
  breach.
- Mutation (run 44509053241): Rust 92.5 (floor 91.5), Go 88.5, web 85.3; `rust-agent-loops` 32 min,
  `rust-agent-host` 52 min, `go-observability-harness` 12 min. A raised floor is on offer (Rust 92,
  Go 88.3).
- Technicians: on the spike, peak, breakpoint and catch-up legs both sign-ins finished 10–25 s
  before the walk began.
- Soak (run 44626427362, dispatched by hand on `df4dfdbf`): valid, 0 of 251,866 requests refused,
  46 limits read and none breached, both ends had their receive buffer, not past the weeks before
  it, no alert sent; the technicians' two journeys were folded in. Commit 0's wave-limit removal
  holds.
- Catch-up (run 44565500921): technicians in their own half processor and 1 GiB, refused 0.08% of
  it. Red: the census (harness 4,995, server 4,986 after 180 s) made the run invalid, which step 1
  removes; 4,093 of 5,000 caught up, 50 batches unanswered, connect p95 557 ms. The technicians'
  journeys ran for the first time and breached: command p95 16.2 s, device detail 6.7 s, device
  list 8.0 s, relay 406 ms; only 697 of 15,820 relay sessions were created (cause unread).
- Breakpoint (run 44566882413): invalid, recovery error rate 0.344 past 0.25 (11-18 was valid and
  over its recovery sign-in limits); step 10's recovery verdict covers it.
- Benchmark Trends (run 44526590309): `BenchmarkAdmitAlertWithFullEvidence` +11.5% against the
  anchor `9a7c64f3`, slower in 9 of 10 interleaved rounds; the other 20 Go benchmarks moved under 2%.
  Ivan (modal): a real regression, to be addressed. Cause (2026-11-19): the code under it is the same
  on both sides (the two commits' test binaries are byte-identical with `-trimpath`, and at the CI
  checkout paths every function sits at the same address); each check allocated and cleared a fresh
  40 KB decompressor, so the time was mostly garbage collection on the runner's other processors.
  The same binary read +15.4% (p=0.000) with one of four processors busy, and identical code read
  −4.9% on 11-17. Fixed in commit 1: the check reuses a held decompressor, 12.8 µs → 1.95 µs and
  40 KB → 8 B a check, and one busy processor moves it +4.7%.
- Catch-up's `catchup/watched/drain within_5s_share` limit gets no row on any night: its source,
  the harness's `live_delay`, left in `f25263a2` (11-11) and the profile and
  `loadtest-bundle-rows.sh` still name it. Why it left: the numeric-ingest review settled that the
  5 s live promise runs from the server's receipt to the store's acceptance, a reading never stored
  counted late, on the server's own clock (`catchup/live/drain`, still gating). `74d1435c` (11-12)
  then added the limit, the row and a dashboard chart reading the removed field. Ivan (modal):
  removed in commit 1, the chart pointed at the server's count.

## What commit 1's rehearsal read (2026-11-19, workstation, the catch-up profile at production's limits)

`before` on commit 0's server with the step-2 harness; `after` on commit 1's tree. Both read
invalid for the same two reasons: the server's start time read 32–50 s apart at the two ends
(one start in its log, no restarts: the workstation clock stepping back), and the drain's error
rate (0.091, then 0.068), which is the sign-ins left unanswered at 5 minutes that steps 4–6 address.

| Reading | before | after |
|---|---|---|
| Machines caught up (per machine after step 2) | 8,083 endings per connection, 18 with the run | 5,000 of 5,000, none with the run |
| Fleet online share / online p95 | 99.63% / 154 s | 100% / 138 s |
| Sign-in, send to the server's answer: p50 / p95 / mean | 79.5 s / 275 s / 105 s | 68.1 s / 273 s / 99 s |
| Sign-ins unanswered at 5 minutes, then redialed | 673 | 506 |
| Machines cut off mid-hold / first dials retried | 18 / 102 | 0 / 20 |
| Live readings stored within 5 s | 99.76% | 100% |
| Connect p95 / handshake p95 | 685 / 504 ms | 609 / 500 ms |
| Catch-up told to wait / turns asked again | 19 / 1 | 5 / 1 |
| Server busy over the return and the drain | 97.6% / 97.3% | 97.0% / 97.3% |

## What the first ARM night after commit 3 read (2026-11-23, `dc2c9821`)

Read by job and step result and from each leg's bundle. Both ARM legs invalid; the server at a
quarter processor was throttled in 99.5% of its periods and held 343-365 MB of 384 MiB.

| Reading | catch-up 11-21 / 11-22 (commit 2) | catch-up 11-23 (commit 3) | burst 11-23 (commit 3) |
|---|---|---|---|
| Verdict | valid / valid | invalid: return error rate 0.858, drain 0.749 | invalid: server stopped mid-run |
| Online in the return / machines caught up | 2,170 / 4,998; 2,535 / 5,000 | 252 / 3,185 | 4,007 of 5,000 |
| Sign-ins a second over the return | (no row) | 0.35 | 13.3 |
| Sign-in p50 / p95 (return) | 3.4 s / 24.6 s; 2.5 s / 21.5 s | 7.1 s / 26.3 s (drain p95 300 s) | 10.2 s / 41.1 s |
| Server UDP receive errors (return) | 0; 2,210 | 15,802 | 4,611 |

- Catch-up's failures: 2,578 dials timed out, 1,660 `CONNECTION_REFUSED` from the server's QUIC
  listener, 808 "the sign-in line is full", 219 sign-ins unanswered; 638 stood down.
- Burst: 3,373 machines lost their connection on a failed heartbeat; the server's metrics port then
  refused connections and the inventory phase read error rate 1.0 (exit cause unread).
- The technicians' journeys: device list p95 9.5 s, command p95 22 s, relay p95 15.2 s.

## What step 9's rehearsal read (2026-11-24, workstation, `b4058707`)

The nightly's catch-up leg at production's limits, technicians on half a processor of their own
and signed in before the walk; kit `~/.cache/opengate-step9-readings`, run `after3`.

- 3,077 of 5,000 online; 808 `CONNECTION_REFUSED`, 1,407 + 653 handshake and dial timeouts, 2,483
  told to come back, 335 "stream accept failed". Invalid: return error rate 0.685, drain 0.809.
- **The technicians' requests took the processor from the sign-ins.** While k6 ran (return start
  to +1,500 s) the server signed in 0.2-0.7 machines a second and held 277-492 connected; k6 ended
  at +1,500 s and the next minute signed in 2,582 at 42 a second. The line runs 2 sign-ins at a
  quarter processor; console requests run without a cap.
- Server processor by minute of the return (CPU profiles): minute 1 handshakes 32%, UDP 25%,
  sign-ins 13%; minutes 2-5 the console's device list 19-26% (each a 5,000-machine JSON body,
  `enrichAnomalyRates` a metrics-store query per list), garbage collection 9-11%, catch-up 8-13%,
  UDP 5-14%, handshakes 5-12%, sign-ins 1.5-5%. Throttled throughout.
- `CONNECTION_REFUSED` is quic-go's 32-slot queue of finished handshakes overflowing
  (`handleNewConn`): a handshake paid for is thrown away when the accept loop falls behind.
- Processor per sign-in once the technicians stopped: about 6 ms of a quarter processor at 42 a
  second, the newly signed-in machines' catch-up included. Memory: resident 334 MB at +61 s, about
  64 KB per added connected machine at the end.
- The nightlies after steps 4-8 (11-23, `dc2c9821`): volume-8000 sign-in p50 0.6 ms, database 13%
  busy (93.7%); spike database 98.8% busy (98.7%), sign-in p50 47.9 ms (149 ms); breakpoint
  recovery sign-in p50 43.9 s, p95 95.4 s (24 s / 57 s); catch-up technicians signed in before
  the walk, 10,862 journeys run.

## What the first ARM night after commit 4 read (2026-11-25, `47a7b90e`)

Read by job and step result and from each leg's bundle. CD 45540997814 green, every step run.

- **Catch-up (run 45668874269): valid**, turn-aways filed apart (2,568). Return: 5,000 offered in
  30 s, 395 online at 42 s (9.4 a second), 16.5 a second while any waited, register p50 5.2 s /
  p95 27.5 s. The online and rate rows never arrived. 4,971 of 5,000 caught up, fill 1,535 s
  (960). Server throttled in 99% of periods, 357 MB of 384 MiB at 4,972 machines (about 64 KB a
  machine), database 11%. Technicians: device list p50 3.7 s / p95 14.7 s, command p95 17.6 s.
- **The harness cut its own return short**: a phase climbs from the count online when the last
  phase ended (`safety.go` `from = result.AchievedConnectedAgents`, 2026-08-26), not the level the
  fleet holds. The drain's first step aimed at 855, stood down 4,145 machines still in the line
  (2,568 waiting to come back, 701 dialling), and climbed back with new machines at 3.2 a second.
  `loadtest-bundle-rows.sh` files the rate only beside an online time, so 16.5 was dropped.
- The same commit's run by hand at 05:37 (45554916357) read invalid: return error rate 0.760, 1,365
  dials timed out against tonight's 133, same chip.
- **Burst (run 45669954087): invalid**, the server killed for memory (exit 137) at 18:03:50 in the
  inventory phase: 3,450 online after the return (32.7 a second while any waited), 314 MB, about
  79 KB a machine; the harness was re-dialling the inventory phase's climb.
- Breakpoint valid: recovery sign-in p50 1.3 s / p95 8.1 s (11-23: 43.9 s / 95.4 s).
- Volume-8000 failed on technicians' pages (device list p95 5.8 s), database 95%, on an AMD EPYC
  7763 runner; 11-24 read 14% on a Xeon 6975P-C, and 11-22's failure (78%) ran on a Xeon 8370C on
  the code that passed 11-21. The runner's chip, not commit 4. Its trend push failed once on the
  cluster (`kubectl run` attach EOF in `vm-push.sh`, not retried).

## Work, each step test first

1. Timers that reach the 5-minute promise (Ivan, modal): the server's sign-in
   buckets (`registrationDurationBuckets`) run to 300 s; the harness times a
   sign-in from its send to the server's first answer, so a wait in the line is
   counted, and marks the machine online at that answer, which the fleet-online
   figures and the server's census read (Ivan, modal, 2026-11-17). The harness
   figures change meaning, one break in their history. The "before" readings are
   taken with these timers.
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
   machine count while a rollout is staged. The connection's two setup reads
   (`TenantForDevice`, `devices.Get` in `server_connection.go`) join it. The reads
   (only the customer's own machine) and the trips proven against a real database. A
   failed conversation ends the sign-in: no machine runs on rules that are not its
   customer's (Ivan, modal, 2026-11-19).
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
    5,000 machines returning while technicians pull a log page a second; live
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
12. The benchmark nightly's two sides kept apart (Ivan, modal, 2026-11-20: both fixes). Run
    `44737728144` failed at the anchor (`9a7c64f3`), round 2: tonight's round 1 took the shared
    test database to migration 23 (`023_default_customer`), and the anchor's `db` package
    startup then refused it ("no migration found for version 23", reproduced locally). The
    log named no cause: a round's output goes to its raw file, lost with the job. Best
    practice: a test owns its own database space and setup touches nothing shared (pgtestdb,
    Encore), old code is refused on a newer schema by design (Coder, GoAlert, Bytebase), and an
    A/B benchmark starts both sides from the same conditions (Quansight, benchdiff). Fixes,
    test first in `benchmark-anchor.test.sh`: each side gets a database of its own for all
    ten rounds; a failed round prints its own output; the `db` package's `TestMain` sets up
    its schema over a plain connection, as `testutil` does, and leaves the shared database's
    `public` schema alone.
13. The nightly's flagged slowdowns, read in commit 3 from the three red nights' own rounds and an
    A/B run of the anchor and tonight on one machine (Ivan, modal,
    2026-11-20): `AdmitAlertWithFullEvidence` +11.5% (11-19), `RelayForward_FilePiece`
    +10.9% (11-13), `Codec_EncodeControl` +12.4% (11-12), Rust `tsdb_commit_window` +18–19%
    (11-12, 11-13), each past the 10% limit; whether each is real or the machine, and the
    fix where real.
14. A restart judged by a per-start ID (Ivan, modal, 2026-11-20). The Prometheus process
    collector recomputes `process_start_time_seconds` from the kernel's boot time on every
    read, and that moves with each wall-clock step, so the workstation rehearsals read
    "target restarted" with restarts=0. Measured 2026-11-20: the wall clock steps back
    0.59 s every ~32 s, and the steady clock runs 2.26% fast against the Windows clock
    (clocksource `tsc`, systemd-timesyncd beside Hyper-V's sync). The server publishes a
    random ID made once at each start (OpenTelemetry's `service.instance.id` idea); the
    harness's `Restarted` compares IDs (`target_health.go`). Test first: a clock step with
    one process reads no restart, a new ID reads one.
15. The tracing library to 1.47.0 (Ivan, modal, 2026-11-20): Dependabot alert 49,
    GHSA-q7gw-24fr-5p69 (moderate, an attribute length limit bypassed by U+FFFD values),
    `go.opentelemetry.io/otel/trace` 1.44.0, reached only through testcontainers → the
    moby client; `otel` and `otel/metric` move with it.
16. Commit 1's empty-tag change undone (Ivan, modal, 2026-11-20): `NoBuild` and the pass-over
    in `scripts/dependency-fixes/` (`images.py` `newer`, `registry.py` `platforms`) go;
    Docker Hub has since filled `postgres:19-alpine` (7 builds), so the check passes today
    either way.
17. Rule rollouts move by hand (Ivan, modal, 2026-11-20; the plan's last item). Nothing calls
    `rules.DecideStage` or `Store.SetRolloutStage`, and `GateReporter` has no
    implementation, while `Rule-Administration.md` and ADR-070 promise earned stages and an
    automatic pull-back. Research: LaunchDarkly guarded rollouts and Argo Rollouts promote
    and roll back on metrics; NinjaOne's rings are moved by an administrator; Hexnode and
    Ivanti offer either. Decided: remove what does nothing. The hold times leave the rule
    screen (`RolloutPanel.tsx`), the API and the database (one migration dropping
    `canary_hold_secs`, `staged_hold_secs`, `stage_entered_at`); the gate code and
    `SetRolloutStage` go; the docs and ADR-070 say an administrator moves a rule's stage.
    **The stage moves by hand** (Ivan, modal, 2026-11-25, after the research: NinjaOne's patch
    rings are approved by hand, Microsoft Autopatch moves its rings on a schedule, LaunchDarkly
    offers a share set by hand, on a timer or watched, and Datadog and NinjaOne conditions aim an
    alert rule at the machines picked with no shares): nothing in the product could move a
    stage, so the rollout request takes the stage an administrator picks (first share, second
    share or everyone) and the rule screen gets a control for it; the reach follows the share
    that stage names.

18. Console requests wait while machines do (Ivan, modal, 2026-11-24): a line for signed-in
    console requests, closed to none while the sign-in line is empty, a set number per processor
    at once while it is not; past the request limit a 503 with a retry time. Tests: the line opens
    and closes with the sign-in line's waiting count; a request past its wait answers 503; the
    enrolment route and the relay never wait. Rehearsed at 1 and 2 at a quarter processor.
19. A "come back later" counts apart (Ivan, modal, 2026-11-24): the harness files a turn-away
    with the expected rejections, so the error rate leaves it out; its own count stays published.
20. A leg records why its server stopped (Ivan, modal, 2026-11-24).
21. The three install scripts retry every failed download (Ivan, modal, 2026-11-24).
22. A phase climbs from the level the fleet holds (Ivan, modal, 2026-11-25): `runProfile`'s next
    `from` is the level the last phase asked for, so a wave still in the line keeps its place and
    the next phase offers only what it adds. Test: a fleet holding its level with few online gets
    no wind-down and no offered arrivals in a phase at the same level.
23. The trend push retried, then read back (Ivan, modal, 2026-11-25): `vm-push.sh` sends through
    `kubectl_retry`; after a dropped connection it reads the run's `ci_run_info` back and sends
    again only when it is absent. Tests: a dropped send that landed is not re-sent; one that did
    not land is.
24. Catch-up waits while machines wait to sign in (Ivan, modal, 2026-11-25): moves to its plan of
    its own with the rate number.

## Commit 2's shape, as the code reads (2026-11-19)

What a sign-in reads today, in order: `TenantForDevice` (admin) and `devices.Get` at accept, then on
`AgentRegister` the device write, `ScopeFor`, `ListBindings`, `ListRollouts`, `TagsFor`, `Counts`
(rollout staged), `alerts.Limits`, and `devices.Get` again for the maintenance flag: nine
transactions of about four trips. The agent sends `AgentRegister` first on every connection
(`control_loop.rs` `open`), and the sign-in write is the only place a device row is created.

- **Customer settings in memory** (step 4): `CatalogueAlertRuleProvider` holds, per customer,
  its bindings, rollouts and alert budget with a version. Loaded for every tenant's customers,
  each inside its tenant's boundary, before the agent listener starts (`internal/app`); reloaded
  for one customer (or every customer of a tenant) by `RefreshAlertRules` /
  `RefreshAlertRulesForTenant`, which every product change already calls through
  `deliverRuleChange` (tuning upsert and delete, stop and resume, alert budget). A customer the
  memory lacks is read on first use. A sign-in that read version v pushes again when the
  customer's version has moved by the time its push is sent.
- **One conversation** (step 5): `devices.SignIn` runs one transaction: the admin scope, the
  device's tenant read and the switch to that tenant in one statement, the write returning the
  machine's customer, site and maintenance flag with its tags in one statement, and the customer's
  machine count only when the customer's rollouts in memory need it. Five trips, six with the count. `accept`
  reads the first control frame, requires `AgentRegister`, runs the sign-in, and only then
  builds and registers the connection with the tenant, customer and site it returned.
- **Turned away**: a failed sign-in closes the connection with the failure's close code and
  registers nothing; the agent redials after its 1–30 s wait. A later push to a connected machine
  (a rule change) reads its scope again and keeps the scope the sign-in established on failure.
- **A removed tuning** (found writing its acceptance test, 2026-11-19): `DeleteRuleBinding` pushed
  nothing, so with settings in memory a removed number would have stayed until the next start. The
  delete returns the customer it tuned and goes out as a retune does.
- **Tests**: real-database `SignIn` tests (place, flag, tags, the count only when asked, the
  tenant's own machine only, the trips counted through a pgx query tracer); a failed sign-in
  turns the machine away (unit); a change racing a sign-in pushes again (unit); settings stored
  before a start reach the first sign-in (server assembly); a removed tuning reaches a machine
  that never disconnects (acceptance).

## Commits (Ivan, modal, 2026-11-12: four; commit 0 added 2026-11-17, commit 5 2026-11-20)

0. The nightlies' unplanned failures, before step 1: the load test's owed levels, the
   soak's wave limits and the profile check, the Rust survivors, the two mutation
   shards, the technicians' accounts before the return and their own half processor.
1. Steps 1–3 and 11: the timers, the three's cause and its fix, a turn request
   asked again, the alert-budget push, the acceptance tests' own fixes; the two
   commit-0 flakes (the default customer marked, the conservation slopes' retained
   side), the benchmark's held decompressor and the dead catch-up limit.
2. Steps 4–5: customer-wide settings in memory, a sign-in's own rows in one
   conversation.
3. Steps 6–8, 10 and 12–16: the sign-in line and "come back later" in Go and Rust, less
   memory churn, the rate check, the burst, the benchmark nightly's two sides kept apart and
   its flagged slowdowns, the per-start ID, the tracing library, the empty-tag change undone.
   Built and checked, uncommitted (2026-11-20): steps 12, 15, 16 (session 5); step 14 (per-start
   ID, ADR-082); step 6 (the line, the customer note, `ComeBackLater` both ways, ADR-131); step 7
   (`bufpool`, the writer's parts, pooled bodies and payloads); step 8 (30 s return, the busy-span
   rate row); step 10 (the ARM burst leg, `log-pages.js`, the inventory call and its stored count,
   the inventory in one statement); step 13 (ADR-088 findings, the store's label trim); the
   start-up reset in every tenant.
4. After the first ARM night and step 9's readings: what the open question needs,
   the readings in the decision record; the pre-commit checks' step timers on a forward-only clock;
   the databases' slow-statement and lock-wait logs; steps 18-21. Built and checked, uncommitted
   (2026-11-24): the step clock (`scripts/lib/step-clock.sh`); the database logs (ADR-014,
   `Database.md`); step 18's console line (`console_line.go`, `MachinesWaiting`, ADR-131,
   `API-Reference.md`, `Fleet-and-Devices.md`), its place count rehearsed at 1 and 2; step 19's
   turn-aways (`quic_fleet.go`, ADR-082); step 20 (`perf-runner-record.sh`, `perf-leg.yml`); step 21
   (`scripts/lib/download.sh`).
   Then (Ivan, modals, 2026-11-25), in a commit of its own before commit 5: steps 22-23, the
   catch-up and burst legs at 4,000 machines, the per-server ceiling in the decision record; then
   both ARM legs run by hand on it.
5. Step 17, on its own (Ivan, modal, 2026-11-20): rule rollouts move by hand; this plan
   deleted with its `phases.md` row, step 24 and the rate number written into a plan of their own.

## Open, for Ivan

- The rate number: from the hand-run after the tool-fix commit.

## Done when

- The catch-up night passes `machines_caught_up` and the 100-a-second sign-in rate
  on the ARM nightly (the workstation run dropped, Ivan, modal, 2026-11-20: its steady
  clock runs 2.26% fast, so its rehearsal is a reading, never the verdict).
- The fleet-online figures and the sign-in timer are recorded here before and
  after each step.
- This plan is deleted in the commit landing its last step, with its `phases.md`
  row.
