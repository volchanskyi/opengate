# Numeric ingest, catch-up and presence — specification

**Status:** Specification agreed with Ivan on 2026-11-06 and revised after
three reviews (section 2, decisions I–AK). Commit 1 landed as `4e7ef5a3`
(2026-11-09) and commit 2 on 2026-11-11. Commit 2 keeps the live promise and
misses the catch-up one at production's processor, so this plan stays open for
the fixes in section 12 (Ivan, 2026-11-11). Every decision is recorded in
section 9.

This replaces the third-party draft of the same name. Every claim in that draft
was checked against the code, the live metrics and the metrics store's own
documentation; section 2 gives each verdict with its proof.

## Contents

1. [What problem this solves](#1-what-problem-this-solves)
2. [The draft, checked claim by claim](#2-the-draft-checked-claim-by-claim)
3. [Defects found that the draft missed](#3-defects-found-that-the-draft-missed)
4. [Things, how they work together, and where the lines are](#4-things-how-they-work-together-and-where-the-lines-are)
5. [Contracts the change must keep](#5-contracts-the-change-must-keep)
6. [Edge cases and how each is handled](#6-edge-cases-and-how-each-is-handled)
7. [Quality bar, acceptance criteria and definition of done](#7-quality-bar-acceptance-criteria-and-definition-of-done)
8. [Work packages, in order](#8-work-packages-in-order)
9. [Decisions](#9-decisions)
10. [Starting values](#10-starting-values)
11. [Proof still owed](#11-proof-still-owed)
12. [Fixes](#12-fixes)

## 1. What problem this solves

A machine sends two kinds of numeric readings:

- **Live readings** — one window a minute, sent right after the heartbeat.
- **Catch-up** — history from the machine's own store after it was offline,
  sent in batches of at most 1,000 readings; the machine moves its position
  forward only when the server confirms a batch.

What a technician gets when this lands:

- A live reading is accepted by the store within **5 seconds** of reaching the
  server, during a catch-up storm as on a quiet day (today: up to about 60
  seconds, held until the machine's next heartbeat).
- After **5,000 machines reconnect with a one-hour gap each, every gap is
  filled within 15 minutes**, without holding up heartbeats, remote sessions or
  alerts.
- One customer's catch-up never starves another customer's machines.
- Every minute is delivered at least once: nothing confirmed is sent again
  unless the server asks for it after a store crash or a drop (decisions N, O);
  a repeat otherwise comes from a lost answer or a machine crash; nothing is
  lost silently — including alerts (decision J).

## 2. The draft, checked claim by claim

| Draft claim | Verdict | Proof |
|---|---|---|
| The control loop handles one message at a time; a catch-up batch waits up to 5 s for the write | **True, incomplete** | `conn_backfill.go:22-24,80-88`, `server_connection.go:265-277`. The heartbeat (`conn.go` `handleHeartbeat`, `SetStatus`) and the hardware report also write to Postgres on the same loop. |
| Live readings are coalesced per connection and written through four slots per connection | **True, incomplete** | `conn_telemetry.go:156-182`, `conn_accounting.go:50-74`. The burst is held until the *next* heartbeat flushes it, so a live reading reaches the store up to about 60 s after it arrived. Alerts and discovery share those four slots. |
| The writer stamps one tenant and one device per call | **True, incomplete** | `types.go:21-23`, `vm.go` `WriteSamples`. One not-a-number or infinite value fails the whole request. |
| The machine sends one batch at a time, moves its position only on confirmation, and has no timeout | **True, incomplete** | `backfill_loop.rs:110-122,165-193`. The grant-expiry check sits behind the wait, so it never fires while waiting; the machine accepts any confirmation without matching it to the batch in flight. |
| Production gives the scheduler no load signal and ignores the hints; the minimum rate can exceed the budget | **True; the overshoot is latent** | `server.go:121` passes `nil, nil`; `backfill_scheduler.go:99` discards `SlotRequest`; `rate()` floors at 500. Headroom is always 1.0 in production, so the overshoot never happens today. |
| Alerts share the per-connection slots with numeric writes | **True, no measured harm** | `conn_alerts.go:67-70`. Live metrics, 30 days: `persist_slots_full` = 0 in production and staging. |
| "Duplicate writes remain safe by series and timestamp" | **False for this deployment** | The store keeps every duplicate unless `-dedup.minScrapeInterval` is set (VictoriaMetrics documentation, Deduplication); the chart passes no such flag (`deploy/helm/monitoring/templates/victoriametrics.yaml:72-78`). The comment at `conn_backfill.go:13-14` makes the same false claim. |
| Fair service by tenant, with byte caps per tenant | **No effect today** | A tenant is the whole managed-services provider, and tenants exist only as seeded rows (`techdebt.md`, "Load-test identities live in the default tenant"). The provider's customers are what need protecting from each other. |
| A response-time formula with a floor and a ceiling drives admission | **Not taken** | Needs calibrating per environment; the shared store's half-core response time is noisy; known to miss a queue that is building. Decision B. |
| New pacing fields on the confirmation are needed to pause a machine; a shipped machine cannot be paused at once | **False** | `DeferBackfill` already pauses every shipped agent at a batch boundary, in any phase (`control_loop.rs:746-753`, `backfill_loop.rs:100-106`). |
| A bonus for the oldest backlog | **Not taken** | It puts a laptop back from two weeks away ahead of 500 desktops with a five-minute gap. Decision F. |
| A machine-side deadline for the confirmation | **Kept, as a second line** | On a live connection a reply cannot be lost without the connection failing; the primary fix is that the server answers every batch. |
| Measure with "the representative load profile" | **No such profile exists** | `-telemetry-cycles` defaults to 1 (`main.go:84`); catch-up runs only in the network drill's 20-machine herd (`network-drill.yml:378-385`). P7 adds it. |
| An empty or fully filtered batch can be confirmed without a write | **True** | `conn_backfill.go:90`. |

### The second review

A second third-party review raised ten points, each checked the same way.

| Review claim | Verdict | Proof and fate |
|---|---|---|
| Live readings dropped while machines stay connected are never re-sent in that session | **True** | Catch-up starts only at session start or on a `DeferBackfill` retry (`control_loop.rs:420,471`) and goes quiet once drained (`backfill_loop.rs:151-153`). The server now asks each machine whose readings it dropped, or every machine after a store restart, to send again from a given minute (section 4, step 9). P3, P6 (decisions N, O) |
| "Stored once" conflicts with retries | **True for the wording** | The store keeps every copy (VictoriaMetrics documentation, Data updates), so Q9 as first written would fail the link-cutting drill on random nights. Every chart reads averages, lows and highs (`vm_query.go:27-38`), so a copy changes none. Decision J |
| An acceptance by the store does not mean the reading is on disk | **True, small** | The store saves to disk every 5 s by default and a crash can lose its last few seconds (Storage, `-inmemoryDataFlushInterval`); a clean stop saves first. Contract 2 says accepted |
| A new agent cannot tell whether an old server answers | **Not a case** | Ivan reinstalls agents by hand. Decision M |
| The runner cannot prove the promise, and the run does not gate promotion | **True, and wider** | The workflow says its numbers only compare nights (`perf-stack.yml:3-4`); its server gets a whole processor and 1 GiB (`perf-stack.yml:25-28`) against production's quarter and 384 MiB (`values-production.yaml:7-13`). Gating promotion was not taken. Decision K |
| Strict priority can starve catch-up; bursts overflow the live bound; shares for three customers; no last-batch signal | **Partly true** | Live demand is capped by fleet size, so catch-up starves only once live alone misses its promise. The burst is real: the heartbeat clock starts at connect (`control_loop.rs:429`), reconnects retry within seconds (`control_loop.rs:275`) and nothing paces them, so a fleet that came back together sends in the same seconds every minute (decision L). "Half while another waits" let two customers lock out a third, and turns per machine gave a large customer most of the turns; turns now go by customer first. The server cannot tell a last batch, so a place is held only while it holds a batch. P1, P4, P6 |
| Byte counts do not bound memory | **True** | Each `Sample` carries its own label map (`types.go:13-18`) on a 384 MiB server. Readings are encoded at hand-off and queued as bytes under one budget. P3 |
| The erasure check can race the write | **True** | Ordering rule in contract 7. P3 |
| "Reaches the store" needs a precise clock | **True for the clock; the query hold-back adds nothing** | The 5 s runs from receipt to acceptance, drops counted as misses. The store hides its newest 30 s from queries (`-search.latencyOffset`), but a window is stamped with its first second (`host_metric_stream.rs:117`) and arrives at least 60 s later. P3, P7 |
| New messages can cross the union's allocation size class; the default-tenant fallback conflicts with "scope is the server's" | **True for size, not for scope** | The union is 1,512 bytes, 24 under its 1,536-byte class, pinned by `control_size_test.go`; new messages reuse fields (P5, P11). The fallback is a server decision a reading cannot override (D13). A fairness test across three customers is added (Q16) |

The review also surfaced two gaps of its own: no rule for an answer that never
comes (decision I), and the 20,000-readings-a-second budget is the scheduler's
configured figure, not a measurement of the store (section 11).

### The third review

A third review, on 2026-11-07, raised ten points and one more, each checked the
same way.

| Review claim | Verdict | Proof and fate |
|---|---|---|
| The 15-minute promise needs about 10,000 readings a second, unproven at half a processor | **True; section 11 said so, with no rule for a failure** | 7.2 million readings in 900 s plus 2,000 a second live. One sender with 256 KiB requests (about 1,640 readings) must finish each in about 160 ms. Decision Q |
| The test store does not carry production's other load | **True; "each server spends the whole budget" is wrong** | Test store: `-retentionPeriod=1d`, `-memory.allowedPercent=40`, no scraping (`docker-compose.perf.yml:101-104`); production: 30 days, the default 60%, scraping, a network disk (`victoriametrics.yaml:72-77`). Admission follows each server's own queue, which fills when the store slows, so a slow store throttles both servers. Decision T |
| The byte budget does not bound memory before the queue | **True, and worse** | The frame reader sets aside the declared length, up to 16 MiB, before reading (`codec.go:128-132`); the 64 KiB check runs after decoding (`conn_backfill.go:33`); no Go memory limit is set; today's cap of 8 machines catching up (`backfill_scheduler.go:70`) goes in P4. D14, decision S |
| One writer lets catch-up hold up live readings | **True for the design; Q10 does measure it** | A live reading waits for the catch-up request in flight, whose 5 s timeout equals the promise. Decision P, Q18 |
| A confirmed reading lost in a store crash is never sent again | **True** | The store buffers up to 1 s and saves every 5 s (`-inmemoryDataFlushInterval`); contract 2 said the machine "still holds" them, but decision A skips confirmed minutes. D15, decision N |
| Repeats are not harmless | **Partly true** | Charts read only average, low and high (`vm_query.go:27-38`), and live and catch-up share minute boundaries (`window_start_60s`), so a repeat changes no chart; Q9 counts repeats. Found beside it: the machine saves its position without forcing it to disk (`backfill/mod.rs:186`), made durable by the next forced save every 60 readings (`edge_sentinel/mod.rs:225`), so a machine crash also repeats about a minute, and the comment above it claims the store removes copies. P6 |
| Live readings dropped by a full queue are never sent again | **True, and step 9 as first written caused a storm** | Step 9 fired only after an outage; an old agent restarts from its last catch-up position (`drain.rs:145`), so a restart sent to every machine re-sent whole sessions. D16, decision O |
| Customer fairness needs a rule that scales | **True for large batches; unknown customers were handled** | A full batch is about 160 KB encoded; split equally, 8 MiB falls below one batch once about 52 customers wait, and a strict share check admits nobody. A failed customer lookup falls back to none (`alert_rules.go:118-128`) and joins "unassigned". D17, decision R |
| Agents can run ahead of the server | **True of the pipeline; Ivan reinstalls by hand** | CD installs the new agent only on two throwaway staging machines (`cd.yml:521-536`); an over-the-air update refuses an older version (ADR-121). Decision M |
| The nightly may not publish or gate the new job; promotion does not wait for it | **True** | `publish` needs only started, scaling, volume and shapes (`perf-stack.yml:702`); production waits on staging and the drills (`cd.yml:604-611`). P7a; decision K |
| No test measures sessions or the database during the storm | **True for sessions; the sweep is light, but cannot be one statement** | The harness already holds technician sessions (`perf-stack.yml:471-472`). The sweep updates about 17 rows a second against today's 83 heartbeat transactions; `devices` is under row-level security (`001_initial.up.sql:121-123`), so the sweep runs one statement per tenant. Decision U, P9 |

### The fourth pass

On 2026-11-08 the whole plan was read again for anything left unanswered, with
the acceptance tier, the nightlies and the infrastructure limits in view.

| Finding | Proof | Fate |
|---|---|---|
| Twelve commits, one per package, each a full gauntlet run | Section 8 as first written | Two commits (decision V) |
| Rollup removal waited for the machine side only for a store restart a planned stop does not cause | The store saves before a clean stop; production has no agents during the work (decision M) | P10 joins the first commit (decision W) |
| A hard failure of the one worker node kills the server and the store together, and the new server cannot see what the store lost | One A1.Flex node of 2 processors holds production, staging and monitoring (`deploy/terraform/modules/oke/variables.tf`) | Accepted and stated (decision X) |
| T read as "the restart less 30 s" misses the lost readings when the store takes minutes to come back | The readings lost are the ones accepted just before the crash, not before the new start | T is the last time the server saw the old store alive, less 30 s (section 4, step 9) |
| The go/no-go had no pass mark | Decision Q | Decision Y |
| GitHub's ARM runner is a newer chip than production's | Runner: Azure Cobalt 100, Arm Neoverse N2; production: Ampere A1, Neoverse N1 | Measured once, then stated (decision Z) |
| Today's code misses decision E by design, so the new job would fail every night until the writer lands | P7a measures today's code | E gates from the second commit (decision AA) |
| No acceptance outcome proves "Offline machines lose nothing" | `capabilities_test.go` binds four Device Health outcomes, none on catch-up | Q19–Q29 (section 7); no browser test (decision AB) |
| The acceptance harness writes readings by sending a heartbeat | `settle()` in `device_health_test.go`, six uses; P3 removes the heartbeat flush | P3 replaces it with a wait on the chart, bounded by the 5 s promise |
| A re-offered alert could land twice | The duplicate check exists (`alerts.Duplicate`, `conn_alerts.go:199-201`) | P11 answers a duplicate "stored" |
| Scheduled nightlies run the default branch's code, and CD deploys only what reached `main` | `main` is the default branch; `cd.yml` runs on `main`'s image build | The go/no-go runs dispatched on `dev` after the push (section 8) |
| The go/no-go would measure today's per-connection writes, not the design P3 builds | P7a as first written | The store probe uses P3's request shape (section 8) |
| The 30-day parity test would fail in P2 while the agent still says ninety | `backfill/mod.rs:86` | P2 changes the agent's floor too |
| A fix to a broken measurement job has no later piece to ride in | Decision Q waits on that job | Rehearsed first, one fix commit allowed (decision AC) |
| The production readings in section 11 include the production machine's own series, which erasure deletes | ADR-054; production agents are deleted before the work starts | Read before the deletion (section 11) |

## 3. Defects found that the draft missed

| # | Defect | Proof | Fate |
|---|---|---|---|
| D1 | A finished catch-up holds its place for 60 s; 5,000 machines in one tenant would all start only after about 21 hours | `server_connection.go:158` is the only `Release`; `backfill_loop.rs:151-153` goes quiet silently. Staging, 30 days: 7,840 deferrals, 470 grants, active places peaking at the per-tenant cap of 4 | P1, P4 |
| D2 | Live readings are re-sent on every reconnect, and again at the one-minute tier after 48 hours; the store keeps every copy | `record_ack` is the only writer of the position (`backfill/mod.rs:200-209`) | P5, P6 (decision A) |
| D3 | Catch-up ships the minute that has not finished | `drain.rs` `band()`/`emit_ok()`; the passing test `drains_a_real_local_store_snapshot` ships window 999,960 at `now` 1,000,000 (run 2026-11-06) | P6 |
| D4 | One bad value fails the whole write, and a catch-up batch holding one is refused for good | `vm.go` `writePrometheusSample` | P2 |
| D5 | Code says 90 days of central history; the store keeps 30 and rejects older | `conn_backfill.go:19`, `backfill/mod.rs:86`, `monitoring/values.yaml`; VictoriaMetrics documentation, Retention; ADR-124 also says ninety | P2, P6 |
| D6 | Catch-up is accepted without a grant | `handleMetricBackfillBatch` never consults the scheduler | P1 |
| D7 | About one health summary in five is dropped in production: breach (15 s) and anomaly (60 s) summaries share one message type and its 10-second floor | `tick.rs:168-197`, `mod.rs:123,198`, `conn_accounting.go:31-41`; production, 30 days: 13,488 dropped vs 55,647 kept. The "drop ratio > 5%" alert reads about 19% by its own formula | P8 (decision D) |
| D8 | A database blip on a heartbeat ends the machine's connection; every heartbeat is a database transaction | `conn.go` `handleHeartbeat`; about 83 transactions a second at 5,000 machines | P9 (decision H) |
| D9 | Store-side rollups nothing reads triple the edge series and fold catch-up into the current minute | `files/edge-sentinel-stream-aggr.yaml`; history in decision G | P10 (decision G) |
| D10 | The nightly drill's simulated fleet assumes the confirmation is the very next message | `soak_backfill.go` `sendBackfillBatch` | P1 |
| D11 | Erasure can race a queued write | ADR-054 checks the deny-list at admission only | P3 |
| D12 | An alert the database fails to store is lost | `conn_alerts.go:67-70`; the machine removes it once sent (ADR-124) | P11 (decision D) |
| D13 | A failed tenant lookup files the machine under the default tenant | `server_connection.go:244-254` | Out (decision D); not recorded as debt (Ivan) |
| D14 | The server sets aside a message's declared length, up to 16 MiB, before reading it, and nothing tells Go about the 384 MiB limit | `codec.go:128-132`; `values-production.yaml:7-13` | P3 (decision S) |
| D15 | A reading the store accepted and then lost in a crash is never sent again | Decision A; the store's 5 s save interval | P3, P5, P6 (decision N) |
| D16 | Live readings dropped while the machine stays connected and the store stays up are never sent again | `control_loop.rs:420,471`; step 9 as first written | P3, P6 (decision O) |
| D17 | An equal share smaller than one batch admits no customer | Section 10 as first written | P4 (decision R) |
| D18 | Production shows 1 repeated minute in 1,848 across about 24 reconnects, where the agent's code re-sends each previous session's live minutes; the agent logs each grant and nothing about its batches, and the server counts no catch-up outcome | Section 11, 2026-11-08 reading; the local agent's log | Mostly explained by D21: the copies were deleted with everything else. Read again once D21 is fixed |
| D21 | Each environment's hourly orphan sweep deletes the other environment's machine readings: it lists every device in the shared store (`vm_delete.go:75-78`, `{device_id=~".+"}`) and deletes by tenant and device only, while both environments use the default tenant | Production logs, 14 days: staging's sweep purged the production WSL machine 124 times and the CentOS VM 45 times (`reconcile: purged orphan telemetry series`, at :11 each hour); production's purged 186 staging machines, the drill fleets among them. A production machine's chart holds at most an hour of history | **Fixed** in its own commit before commit 1 (decisions AF, AG): staging's readings in a store of its own, the sweep and deletions scoped to their environment, unfinished deletions retried hourly ([ADR-054](../../docs/adr/ADR-054-data-lifecycle.md)) |
| D19 | The drop-ratio alert can fire only above one message a second, about 40 machines | `alert-rules.yml` `edge-telemetry-drop-ratio`, `clamp_min(…, 1)`; section 11 | Removed with the other two product rules (decision AE) |
| D20 | The store sits at 85% of its memory limit while almost idle, past its own 80% warning | Section 11 | Measured by P7a, raised only if the go/no-go fails on memory (decision AD) |
| D22 | CI passes a serious hole the image build then refuses. govulncheck fails only on code the server calls; the dependency check lists a hole fixed on a newer line for Ivan and passes, and the gauntlet prints those as a count (487); Trivy refuses any HIGH or CRITICAL with a fix, but scans the image only after the merge to `main`, and its database took the CVE about ten days after the Go one | D21's commit `00730882`: CI green (run 42550575574), the image build red (run 42551697511) on `golang.org/x/net` v0.61.0 in `meshserver`, CVE-2026-78661 (HIGH; GO-2026-6860, published 2026-10-27) and CVE-2026-97045 (GO-2026-6866), both fixed in 0.62.0. The image build of 2026-11-06 scanned the same x/net and passed. `cargo audit` and `npm-audit.sh` already refuse a serious hole whatever line fixes it; Go and the images do not | P12 (decisions AH, AI) |
| D23 | The image is published before it is scanned, and a reused image is never scanned again: the failed image is `:latest`, `sha-478a676` and `v0.113.7`, and a later commit touching no image input copies `:latest` to its own tag, which CD deploys | `build-image.yml`: `push: true` precedes the Trivy step; `tag-forward` copies `:latest` without a scan. D21 has not reached staging or production, since CD runs only on a green image build | P12 |
| D24 | The breakpoint leg's generator runs out before the server does: since 2026-11-09 the server holds more machines at the middle rungs, and the generator's one shared allowance cannot carry them, so every night reads invalid and its rows stay out of the trend | Fourteen nights' bundles and a local A/B (section 11, "The breakpoint leg") | P13, last (decision AK) |

## 4. Things, how they work together, and where the lines are

### The things involved

| Thing | What it is | Lives in |
|---|---|---|
| Machine connection | One ordered control stream per machine; tenant, customer and device fixed by the server | `agentapi.AgentConn` |
| Provider, customer, site | Tenant, organization and site ([ADR-064](../../docs/adr/ADR-064-tenancy.md)) | Postgres |
| Live window | One minute of vitals, sent once | `AgentMetricWindow` |
| Health summary | Anomaly rate (every 60 s) or breaches (every 15 s while firing); not caught up | `AgentHealthSummary` |
| Catch-up batch | At most 1,000 readings of one tier, ending at a position | `MetricBackfillBatch` |
| Catch-up answer | Stored (`MetricBackfillAck`) or not now (`DeferBackfill`) | existing messages |
| Live confirmation (new) | "Window *t* is stored" | new message, new capability |
| Alert answer (new) | "Alert stored / refused for good / try again" | new message, new capability |
| Re-send request (new) | "Send again everything after T", after a store restart or a drop | new field on an existing message, new capability |
| Grant | Permission to start, with a pace; holds no place | `BackfillScheduler` |
| Shared numeric writer (new) | One server-wide queue with two classes, one sender each | `server/internal/telemetry` |
| Confirmed ranges (new, machine side) | The time ranges the server has confirmed, at any resolution | the machine's local store |
| Metrics store | Single-node VictoriaMetrics, 500 millicores, 512 MiB, 30-day retention, shared by production, staging, CI trends and scraping | `monitoring` namespace |
| Presence | Connected or not: the server's in-memory connection list; the database records changes and a periodic last-seen | `AgentServer.conns`, `devices` table |
| Deny-list | Erased machines, in memory | [ADR-054](../../docs/adr/ADR-054-data-lifecycle.md) |

### How they work together

1. **Hand-off without waiting.** A connection checks a message's declared
   length against the largest message a machine sends and reserves it from a
   server-wide reading allowance before reading it (D14), validates it exactly
   as today, turns each reading into a *scoped reading* (tenant and device from the
   connection) and hands it to the shared writer. The control loop never waits
   on the metrics store.
2. **Two classes, one sender each** (decision P). Live readings go out on
   their own sender and never wait behind a catch-up request. Catch-up has its
   own sender and its own smaller byte bound, so it can never push out a live
   reading.
3. **Batching across machines.** Each reading is checked on its own when it is
   handed over — a bad one is dropped under a named reason — and encoded at
   once, so the queue holds encoded bytes beside each device and its bound
   counts what is held. The writer flushes on 256 KiB or 1 second of age,
   whichever comes first, and checks the deny-list for each device just before
   a request goes out, in the order contract 7 states.
4. **Every submission is answered.** A live window's readings, once the store
   accepts them, produce a live confirmation to a machine that supports it. A catch-up batch
   gets exactly one answer — `MetricBackfillAck` when every reading is stored or
   deliberately filtered, `DeferBackfill` otherwise — within 15 seconds.
5. **Admission per batch, from the queue's own fill** (decision B). A batch is
   accepted only if the machine holds a grant, has no other batch in flight, its
   customer is within its share (a customer may always send one batch a turn,
   whatever its size, and the overshoot is charged to its later turns; decision
   R), and the catch-up queue is in normal mode. Above
   the high mark the queue switches to throttled mode and gives turns to the
   waiting customers in rotation, and within a customer to its machines in
   rotation, until it drains below the low mark. Refused batches get
   `DeferBackfill` with a retry time that spreads waiting machines out.
6. **The machine sends only what was never confirmed** (decision A). It keeps
   the ranges the server confirmed, live or catch-up, at any resolution, and its
   catch-up skips them. It never sends a minute that has not ended. It drains its
   newest 48 hours first, then older history. A batch with no answer by the
   deadline pauses catch-up, and the same batch is sent again after 1, 2, 4…
   minutes, up to 30, with no limit on attempts; the connection stays up
   (decision I). Each live window goes out at a fixed point in the minute taken
   from the machine id, so a fleet that reconnected together still sends evenly
   (decision L).
7. **Presence lives in the connection** (decision H). The database is written
   when a machine connects and disconnects, and a sweep every 5 minutes refreshes
   last-seen for every connected machine in one statement and marks offline any
   row still saying online with no connection behind it. A heartbeat touches no
   database.
8. **Alerts are answered** (decision D). The machine keeps a sent alert until the
   server says stored, refused for good, or try again.
9. **Lost and dropped readings are asked for again** (decisions N, O). The
   server reads the store's start time every few seconds. When it changes, it
   sends every connected machine that supports the re-send request "send again
   everything after T", T being the last time the server read the old start
   time, less 30 s; a machine offline then is told at its next connect while
   the server stays up. A server that restarts with the store, as on a hard
   failure of the one worker node, cannot see the change (decision X). When the writer drops a machine's live
   readings, the server notes that machine's first dropped minute and, once the
   queue is below its low mark, sends only those machines the same request,
   spread over a minute. The machine forgets its confirmations after T and
   catches up from there. A machine on an old build gets neither; its gaps fill
   at its next reconnect.

### Where the lines are

- **In:** the numeric write path for live and catch-up, the length check before
  a message is read, catch-up admission, the
  machine's catch-up position and answer handling, live and alert confirmations,
  the re-send request,
  presence writes, the health-summary floor, removal of the store-side rollups,
  the load generator's catch-up client, the new load profile, and the
  documentation of all of these.
- **Out:** the default-tenant fallback (D13); the store's duplicate collapse
  setting (delivery is at least once, and a repeat comes only from an answer
  lost to a cut link or a server fault; decision J); a server message saying
  what it supports (decision M); discovery persistence; the hardware report's
  synchronous write; several server copies.
- **Never:** a tenant or device taken from a message; a device or tenant label
  on the server's own metrics; fault code in the shipped binary
  ([ADR-055](../../docs/adr/ADR-055-fault-injection.md)); a change to the
  24-series vitals contract ([ADR-065](../../docs/adr/ADR-065-vitals.md)).

## 5. Contracts the change must keep

1. **Scope is the server's.** Tenant, device and environment labels come from
   the connection and the server configuration; a reading cannot set or
   override them ([ADR-044](../../docs/adr/ADR-044-telemetry-ingest.md)).
2. **A confirmation means accepted by the store.** A live confirmation, a
   catch-up answer and an alert answer are sent only after the store accepts the
   write, or after a deliberate, counted refusal. The store buffers what it
   accepts for up to a second and saves it to disk every 5 seconds, so a store
   that crashes, rather than stopping, can lose about 6 seconds of accepted
   readings; the server sees the restart and asks for them again (decision N).
   When the server goes down with the store, those seconds stay lost, and the
   Device Health chapter says so (decision X).
3. **Every catch-up batch and every alert is answered once,** within a stated
   bound, whatever happens to it.
4. **Mixed fleets work.** Every server package works with every shipped agent,
   since a machine offline during a deploy returns on its old build. Agents are
   reinstalled by hand, so none runs ahead of the server (decision M). New server-to-agent
   messages are gated on a capability, and a server that does not know a new
   agent-to-server message ignores it
   ([ADR-042](../../docs/adr/ADR-042-control-protocol-compatibility.md)).
5. **Memory and goroutines are bounded** independently of how many machines are
   connected — one byte budget, counted on the encoded bytes held, covers both
   classes and every customer, a reading allowance covers messages not yet
   decoded, and Go's memory limit sits below the container's (D14) — and a
   completed operation gives back what it took
   ([`resource-conservation.md`](../rules/resource-conservation.md)).
6. **Nothing is lost silently.** Every reading and alert accepted is stored,
   refused under a named reason, or handed back to the machine, and the counts
   reconcile.
7. **Erasure wins.** Nothing is written for a machine after its deny-list entry,
   and the erasure job deletes series only after in-flight writes for that
   machine have finished. A write is registered before its deny-list check; the
   erasure job writes the entry and then waits for every write registered
   before it, so a check that passed is always waited for.

## 6. Edge cases and how each is handled

| Situation (real-world example) | Handling |
|---|---|
| The store restarts for an upgrade while 5,000 machines stay connected | Live readings queue to their bound, then drop with a count. Catch-up gets `DeferBackfill` meanwhile. Once the store is back, its new start time makes the server ask every connected machine to send again from 30 s before the restart, and each machine with a dropped minute to send again from it (section 4, step 9), so nothing waits for a reconnect. Heartbeats, sessions and alerts never wait. |
| The store refuses a reading as malformed | That reading is dropped and counted; the rest is stored; the batch is confirmed with it counted as filtered. |
| A laptop on hotel Wi-Fi drops every few minutes | A batch in flight on a closed connection may still be stored; its answer is discarded; the next session re-sends it, and Q9 counts that repeat against the lost answer. |
| A 2,000-machine hospital site comes back after a fibre cut, while a five-PC dental clinic is online | The hospital is held to its customer share; the clinic's machines are admitted alongside it. |
| A router reboot gives 500 desktops a 5-minute gap while a laptop returns with a 2-week gap | In throttled mode, waiting machines are admitted in turns. The desktops finish in one or two turns; the laptop's newest 48 hours land early and its older days follow. |
| Every machine reconnects at once after a server upgrade | Heartbeats line up, live windows do not: each machine sends at its own point in the minute (decision L), and the writer merges what arrives into large requests. |
| A router firmware push reboots a 2,000-seat call centre and every machine sends a batch at once | Each message's declared length is checked and reserved from the reading allowance before it is read; a machine past the allowance waits to be read, and the server stays inside its memory limit (D14). |
| The morning login rush at a 300-PC office slows the store for 20 s and the live queue fills | The machines whose minutes were dropped are asked to send again from their first dropped minute once the queue drains, spread over a minute; the others hear nothing (decision O). |
| A managed-services provider's 60 dental clinics each have one laptop waiting | Each customer sends one batch a turn, whatever its size, and the overshoot is charged to its later turns (decision R). |
| A technician watches a server's CPU live while a slow catch-up write is in flight | Live readings go out on their own sender (decision P). |
| Catch-up arrives without a grant (an old build or a hostile agent) | `DeferBackfill`; nothing is queued. |
| The same batch arrives twice (a reconnect while the first is still queued) | The second joins the first: one write, and the answer goes to the live connection. |
| A machine whose customer is not yet resolved, or whose customer lookup failed | It shares the "unassigned" group's share. |
| A machine is erased while its readings are queued | Dropped as `tombstoned` at encoding; the erasure job waits for in-flight writes before deleting series. |
| A machine's clock is wrong | Today's bounds apply unchanged; the catch-up floor becomes the store's 30 days. |
| The server shuts down with readings queued | The writer drains for a bounded grace period; whatever is left stays unconfirmed and is re-sent after reconnect. |
| An old agent waits on a catch-up answer | It always gets one, so it is never stuck while the connection lives. |
| The server stops answering catch-up (a fault) while an office desktop stays connected for weeks | The 60 s deadline passes; the machine pauses catch-up and sends the same batch again after 1, 2, 4… minutes, up to 30, with no limit; the connection and any remote session stay up; each miss is counted (decision I). |
| The store runs out of memory during a catch-up storm and crashes | Up to about 6 s of accepted readings are lost centrally; the server sees the store's new start time and asks every connected machine to send again from 30 s before the restart (decision N). The store's memory reading (section 11) is taken before P4 fixes its numbers. |
| The one worker node fails hard at 10:14 (a host fault at the cloud provider) and the server and the store restart together | Up to about 6 s of readings accepted before 10:14 are lost centrally; the new server cannot see the store's restart, so nobody is asked for them. The Device Health chapter states this loss, as Prometheus remote write and Datadog accept it (decision X). Every other minute is re-sent by catch-up, since the machines reconnect. |
| A machine re-offers an alert whose "stored" answer was lost on a cut link | The server finds it a duplicate (`alerts.Duplicate`) and answers "stored"; the triage queue holds it once. |
| A deploy is rolled back | Ivan reinstalls the agents at the server's build by hand; an over-the-air update refuses an older version ([ADR-121](../../docs/adr/ADR-121-agent-auto-update.md)) (decision M). |
| The database restarts for 30 seconds | No machine disconnects; the presence sweep retries; connect and disconnect writes that failed are reconciled by the next sweep. |
| A disk-full alert is raised during a database restart | The server answers "try again", and the machine re-offers it on the next heartbeat. |
| A machine sends an alert for a rule the server does not ship | The server answers "refused for good", so the machine stops offering it. |

## 7. Quality bar, acceptance criteria and definition of done

### Measurable criteria

| # | Criterion | How it is proven |
|---|---|---|
| Q1 | With every store write held for 10 s, a heartbeat and an alert sent behind a catch-up batch are handled within 100 ms | Go test with a writer that blocks on a channel, and an injected clock |
| Q2 | Every catch-up batch and every alert gets exactly one answer within its bound | Go tests for stored, filtered, refused, queue full, waited too long, failed write, closed connection and shutdown |
| Q3 | Queued plus in-flight encoded bytes never exceed the one budget, per class and per customer, and a full queue's heap growth is measured against it; 5,000 machines sending a full batch at once stay inside the reading allowance and the server's memory limit | Go test from many goroutines under the race detector; a heap reading with the queue full and one with 5,000 simultaneous senders |
| Q4 | Writer goroutines are constant as connected machines grow | Go test at 10, 100 and 1,000 connections |
| Q5 | Completed catch-up sessions give back their goroutines and heap | Conservation slope test in `server/tests/integration/` |
| Q6 | Every line carries the connection's tenant and device; a reading cannot override them; one bad reading fails nothing else | Go tests on the encoder |
| Q7 | Readings accepted equal readings stored plus readings refused by reason | Go accounting test |
| Q8 | Nothing is written after the deny-list entry, including an entry that lands between a write's registration and its check; series deletion waits for in-flight writes | Go test with the erasure job and a writer held at each step |
| Q9 | The machine never ships an unfinished minute and never re-sends a confirmed one unless asked (decisions N, O) | Rust tests; on the network drill, the repeats read from the store never exceed the answers the herd lost to cut links plus the re-send requests sent |
| Q10 | During catch-up, 99% of live readings are accepted by the store within 5 s of reaching the server, dropped and refused readings counted as misses; 5,000 machines with a one-hour gap each, reconnecting with the agent's own retry delays, are all filled within 15 minutes; technician sessions held during the run keep the busy-morning profile's limits | A new job in the Performance Stack nightly on an ARM runner, server and store at production's limits and the store carrying production's settings and load, read by its gates (decisions K, T, U) |
| Q11 | Old agent with new server works, and each side ignores a message it does not know | Go and Rust tests, golden fixtures in both directions |
| Q12 | Both kinds of health summary arrive under the floor; a second summary of the same kind inside 10 s is still refused | Go tests |
| Q13 | A failed status write never ends a connection; the sweep reconciles a missed offline write | Go tests with a failing device store |
| Q14 | A missed catch-up answer is followed by the same batch after 1, 2, 4… minutes, up to 30, with no limit and the connection kept | Rust test with an injected clock |
| Q15 | After a store outage, a store crash that loses accepted readings, or a full live queue with the store up, every lost or dropped minute is stored without a reconnect, and a machine on an old build is sent no re-send request | Go test with a writer that fails and recovers, a store whose start time changes, and a simulated fleet |
| Q16 | With 1, 3, 60 and 500 customers waiting, each gets turns whatever its batch size, and a 5-machine customer progresses beside a 2,000-machine one | Go test of admission |
| Q17 | A machine sends its live window at the point in the minute its id names, and 5,000 ids spread evenly across the minute | Rust test |
| Q18 | With a catch-up write held in flight for 10 s, a live reading handed over during it is accepted within 5 s | Go test with a held catch-up sender and an injected clock |

Q10 gates the night from the second commit; between the commits the job
publishes its readings against decision E, and only the go/no-go fails the night
(decisions Y, AA).

### Acceptance outcomes

The acceptance tier proves each product chapter's promises through the HTTP API
and the machine's control path ([ADR-081](../../docs/adr/ADR-081-composition-root-and-test-tiers.md)).
Each outcome below is bound to its chapter in `capabilityOutcomes`
(`capabilities_test.go`) by the package that makes it true. The real-agent gap
fill stays in the nightly network drill (decision AB).

| # | Chapter | Outcome test | Package |
|---|---|---|---|
| Q19 | Device-Health | `TestAMachineBackFromAnOutageFillsItsChartWhereTheReadingsHappened` — a granted batch is answered stored, and the chart shows each reading at the time it was measured | P1 |
| Q20 | Device-Health | `TestCatchUpSentWithoutPermissionIsToldToWaitAndStoresNothing` | P1 |
| Q21 | Device-Health | `TestOneBadReadingCostsNothingElseInItsMinute` | P2 |
| Q22 | Device-Health | `TestHistoryOlderThanTheServerKeepsIsAnsweredAndNotStored` | P2 |
| Q23 | Device-Health | `TestBothKindsOfHealthSummaryInOneBreathAreKept` | P8 |
| Q24 | Device-Health | `TestALiveMinuteReachesTheChartWithoutWaitingForAHeartbeat`, inside the 5 s promise | P3 |
| Q25 | Device-Health | `TestAMachineIsToldEachMinuteItSentWasStored` | P5 |
| Q26 | Data-Erasure | `TestReadingsQueuedWhenAMachineIsErasedNeverReachTheStore` | P3 |
| Q27 | Fleet-and-Devices | `TestAMachineThatDisconnectsShowsOfflineWithTheTimeItLeft`, and online again when it returns | P9 |
| Q28 | Alerts-and-Rules | `TestAMachineIsToldItsAlertWasStoredAndAnOfferAgainLandsOnce` | P11 |
| Q29 | Alerts-and-Rules | `TestAnAlertForARuleTheServerDoesNotShipIsRefusedForGood` | P11 |

The harness's `settle()` sends a heartbeat because the heartbeat writes the
burst today; P3 removes that flush, so P3 replaces `settle()` in its six uses
with a wait that reads the chart until the minute appears, bounded by the 5 s
promise.

### Non-functional requirements

- **Performance:** encode-path allocations are benchmarked and held to the
  committed baseline ([ADR-088](../../docs/adr/ADR-088-benchmarks-measure-the-code.md)).
  Decoded control frames stay inside their allocation size class: the union has
  24 bytes of room (`control_size_test.go`), so the new messages reuse existing
  fields (`Timestamp`, `Tier`, `Cursor`, `AlertID`).
- **Security:** the pen-test gate runs on every diff; scope is the server's;
  catch-up needs a grant; per-machine and per-customer bounds stop one machine or
  one customer from monopolising the writer.
- **Maintainability:** the writer lives in `server/internal/telemetry` behind the
  module lints ([ADR-020](../../docs/adr/ADR-020-module-boundaries.md)); new Go
  files get a mutation shard; files stay inside the PMAT grade; comments follow
  [`code-comments.md`](../rules/code-comments.md).
- **Observability** (no device or tenant label,
  [ADR-076](../../docs/adr/ADR-076-platform-metrics.md)): queue bytes and oldest
  age per class, time from receipt to store acceptance per class (drops counted
  as misses), request duration and size per sender, re-send requests sent by
  cause, store restarts seen, catch-up
  answers by kind, catch-up readings stored or filtered, alert
  answers by kind, presence sweep outcomes. Dashboards may chart them; no
  alert rule reads a product reading (decision AE).

### Definition of done

- Every criterion passes in the gauntlet or on its named nightly.
- ADRs edited in place: [ADR-044](../../docs/adr/ADR-044-telemetry-ingest.md)
  (writer, classes, delivery, admission),
  [ADR-052](../../docs/adr/ADR-052-agent-local-store.md) (confirmed ranges),
  [ADR-124](../../docs/adr/ADR-124-an-alert-reaches-the-queue.md) (alert answers;
  the ninety days corrected),
  [ADR-126](../../docs/adr/ADR-126-advisory-exceptions-lapse.md) (one verdict at
  three points, decision AI). New ADR-130 for presence. Index rows in
  `decisions.md`.
- [`tool-versions.md`](../rules/tool-versions.md) states decision AI, and the
  CI/CD chapter under `docs/infrastructure/` describes the scan before the push.
- The architecture, protocol, metrics, monitoring and fleet pages describe what
  runs; the comments at `conn_backfill.go:13-14` and `backfill/mod.rs:185` are
  corrected.
- The Device Health chapter's "Offline machines lose nothing" states the 5 s and
  15-minute promises and the seconds a hard failure of the server and the store
  together can lose (decision X).
- The chip difference measured once is stated in
  [ADR-107](../../docs/adr/ADR-107-where-a-run-happens.md) (decision Z).
- Q19–Q29 are bound in `capabilityOutcomes`.
- `phases.md` gains its row and this plan is deleted in the commit that lands the
  last package.

## 8. Work packages, in order

The packages land in two commits (decision V), each with one micro-plan holding
a section per package, reviewed before code, and each through tests first,
`/refactor` and the gauntlet. A defect found after a push is fixed inside the
next commit, never in a commit of its own; the one exception is a defect in the
new measurement job itself, which the second commit cannot start without
(decision AC).

| Commit | Packages | Lands when | After it |
|---|---|---|---|
| 1 — Baseline and small fixes | P1, P2, P7a, P8, P10, P12, and the three product rules removed (decision AE) | Its micro-plan is approved and the P7a job has been rehearsed locally | The P7a job is run three times on `dev`; the median decides go or no-go for commit 2 (decisions Q, Y) |
| 2 — Writer, admission, confirmations, presence, alert answers | P3, P4, P5, P6, P7, P9, P11, then P13 last | Commit 1's go reading, and its micro-plan approved; P9 and P11 are written while the go/no-go runs | `phases.md` row, this plan and its micro-plans deleted in the same commit; Ivan reinstalls the production agents |

The plan and commit 1's micro-plan are committed with commit 1 so other
engineers can work from them; commit 2 deletes both.

| Package | Side | What changes | Depends on |
|---|---|---|---|
| P1 Answers and grants | Server, load tests | Every catch-up batch answered once; catch-up needs a grant; a grant holds a place only while the server holds one of the machine's batches, since the server cannot tell a last batch; the load generator waits for its answer and counts each kind (D1 in part, D6, D10) | — |
| P2 Reading-level checks | Server, agent | A bad reading is dropped and counted on its own; the catch-up floor is the store's 30 days, one value held equal across server, agent and chart by a parity test, so the agent's own floor changes here (D4, D5) | — |
| P7a Baseline | Load tests, CI | The generator sends live windows every 60 s and runs the catch-up client; a new Performance Stack job on an ARM runner, server and store at production's limits, its store carrying production's cache setting, scrape configuration and a background writer at production's scrape and CI-trend rate, measures today's code against decision E (published, not gated; decision AA) and the store's sustained import rate (decisions K, T); a store probe writes in P3's request shape — one sender with 256 KiB requests, then two — beside a live-shaped writer of 2,000 readings a second in 1 s requests, and its reading is the go/no-go (decisions Q, Y); a fixed one-minute processor benchmark, run on the runner every night beside the runner's processor model, and once on the production node (decision Z); the job joins `publish`'s needs, its missing-result check and its gate | P1 |
| P3 Shared writer | Server, protocol | Message lengths checked and reserved before reading, and Go's memory limit (D14); scoped readings encoded at hand-off, two classes under one byte budget with one sender each, batching across machines, the deny-list ordering and the wait the erasure job uses; the re-send request, its capability and golden fixtures, sent after a store restart and after a drop (D15, D16); metrics; live and catch-up both routed through it; per-connection coalescing removed (D11) | P1, P2, P7a passed |
| P4 Admission | Server | Per-batch admission from the queue's fill with high and low marks; in throttled mode, turns by customer and then by machine; an equal share per waiting customer, at least one batch a turn with the overshoot charged (D17); the rate floor that overshoots fixed (D1) | P3 |
| P5 Live confirmation | Server, protocol | The live confirmation message and capability, sent once the store accepts the window, reusing existing union fields; golden fixtures both ways; completeness proof; the union-size entry in `techdebt.md` brought to today's figures (1,512 bytes, 107 fields) | P3 |
| P6 Machine side | Agent | Confirmed ranges, migrated from today's per-tier positions; catch-up skips them; no unfinished minute; matching answers; 60 s answer deadline and the retry after it (decision I); the live window sent at the machine's own point in the minute (decision L); the re-send request: confirmations after T forgotten and caught up (D15, D16); how confirmations are saved to disk stated; 30-day floor (D2, D3, D5) | P1, P3, P5 |
| P7 Measurement | Load tests | Generator handles confirmations and re-send requests, sends at each machine's own point in the minute and reconnects with the agent's retry delays; the P7a job's profile with 5,000 machines, a one-hour gap each, live load and technician sessions, gated on decision E and the busy-morning session limits (decision U); the drill's figures read against it | P4, P6 |
| P8 Health-summary floor | Server | A separate floor per kind of summary (D7) | — |
| P9 Presence | Server | Status written on connect and disconnect; 5-minute last-seen sweep and reconciliation, one statement per tenant under row-level security; heartbeat writes nothing; ADR-130 (D8) | — |
| P10 Rollups removed | Monitoring chart | Remove the rollup config and its two flags; update the test that pins them and the monitoring page. The store's one restart costs nothing: a clean stop saves first, and production has no agents during the work (D9, decision W) | — |
| P11 Alert answers | Server, agent, protocol | Alert answer message and capability, reusing existing union fields; the machine keeps an alert until answered and re-offers on "try again" or reconnect (D12) | — |
| P12 One vulnerability verdict | Server, CI, gauntlet | `golang.org/x/net` to 0.62.0 (decision AH). One image scan script — the pinned Trivy, HIGH and CRITICAL, unfixed ignored, one dated exception list — run on the image `make e2e` builds in the gauntlet, in a CI job `merge-to-main` needs, and in the image build on a digest no public tag names yet; signing, attestation and tags follow a pass, and `tag-forward` scans `:latest` before copying it. The dependency check refuses a HIGH or CRITICAL hole in what the server image ships that only a newer line fixes, taking the severity from the advisory's aliases, since the Go database carries none, and reading the same exception list (decision AI); the gauntlet prints each such finding in full. A sweep in `ci-cd-determinism.test.sh`: no tag before the scan, no copy without one (D22, D23) | — |
| P13 Breakpoint sizing | Load tests, CI | The last item of the plan. Against commit 2's server, the full breakpoint walk runs on the workstation with the machine generator and the technician generator each measured in an allowance of its own; from those figures the leg's generator allowance or its ladder is set, as Ivan chooses in a modal. The ceilings the run is judged by (90% of the allowance's memory, 20% of the run refused the processor) stay (D24, decision AK) | P3–P7 |

CI and scheduled jobs these packages touch, checked:

- **Every commit (gauntlet):** Go and Rust tests, golden fixtures, the benchmark
  allocation check, end-to-end tests, SonarCloud, the mutation-shard partition
  check, the comment and documentation gates.
- **`network-drill.yml` (06:00, staging, 20-machine herd, `-backfill-batches=4`):**
  needs P1's generator change the night P1 lands. S1's catch-up-complete and
  gap-fill figures and S2's staleness are the field readings for D1 and D2.
- **`perf-stack.yml` (07:00, throwaway stack):** a new job, added by P7a, runs on
  an ARM runner (free for this public repository) with the server at a quarter
  processor and 384 MiB and the store at half a processor and 512 MiB, as in
  production; the existing jobs keep their caps. Its store takes production's
  cache setting and scrape configuration and a background writer (decision T);
  the runner's local disk stays faster than production's network disk, and its
  processor is a newer chip, both stated (decision Z). It runs without the
  store-side rollups, as production does from commit 1. Its store keeps one
  day, so the profile's gaps stay inside a day; older history is proven by Rust
  tests. The job joins `publish`'s needs, its missing-result check and its gate.
  A scheduled run takes `main`'s code, so the go/no-go runs are dispatched on
  `dev` right after commit 1's push; the workflow's concurrency group queues
  them one after another.
- **`soak.yml` (Sundays 07:00, 5 hours, 500 machines):** its leak slopes cover
  the writer and the presence sweep.
- **`benchmark.yml` (04:00) and `mutation.yml` (03:00):** new benchmarks and new
  files enter their baselines and shards.
- **`cd.yml`:** runs on `main`'s image build, so a commit reaches staging only
  once CI on `dev` passes and the merge to `main` follows; a CI failure on
  commit 1 holds it out of staging and production until commit 2 carries the
  fix. Then staging deploy, staging drills, then production. Production has no
  agents during the work, and Ivan reinstalls them after commit 2 (decision M);
  a machine offline at that moment returns on its old build, which the server
  keeps serving. Production does not wait for the nightly, so a regression can
  reach production up to a day before the nightly reports it (decision K).
- **Monitoring chart (P10, commit 1):** rendered, applied and read back by the
  deploy ([ADR-123](../../docs/adr/ADR-123-alert-delivery.md)); the store
  restarts once, which briefly affects production, staging and the CI trend
  history alike.

### Measured before each push

The gauntlet already runs the Go and Rust benchmarks against their committed
baselines, the conservation slopes and the end-to-end tests. Each commit adds
readings taken on the workstation (24 processors, 15 GiB, x86) at production's
limits, kept with the micro-plan and quoted in the commit message:

- **Commit 1:** the whole P7a job rehearsed through its own scripts and compose
  file — server at a quarter processor and 384 MiB, store at half a processor
  and 512 MiB, background writer on — reading the store probe at one and two
  senders and today's code against decision E; the job's ARM images started once
  under emulation, to catch an image built only for x86; the benchmarks of the
  per-reading checks.
- **Commit 2:** the P7 profile at 5,000 machines with a one-hour gap each, live
  load and technician sessions, against decision E and the busy-morning limits;
  the heap with the queue full and with 5,000 simultaneous senders (Q3); writer
  goroutines at 10, 100 and 1,000 connections (Q4); an hour of the soak profile
  at 500 machines for its leak slopes; the encode-path benchmarks.

What the workstation cannot show is read after the push, with no fix pushed
alone: the dispatched ARM runs, the network drill once staging carries the
build, and the Sunday soak.

### Where each run can happen

- **GitHub's ARM runner** (`ubuntu-24.04-arm`, free for a public repository): 4
  processors and 16 GiB. The job holds the server (a quarter), the store (a
  half), the generator (one, about 3.3 GiB at 5,000 machines), Postgres, the
  background writer and the session driver inside that; a job may run 6 hours.
  GitHub can hand out different hardware from one night to the next, so the
  runner record names the processor each night.
- **Staging** shares the store and the one worker node with production, so its
  drill stays at 20 machines and never carries the storm.
- **Production** is one 2-processor A1 node holding production, staging and
  monitoring. It runs no load test; the once-only processor benchmark runs there
  as a pod in the staging namespace at a quarter processor, by Ivan's command.

## 9. Decisions

All taken by Ivan on 2026-11-06.

- **A. What a machine re-sends: only minutes the server never confirmed.** The
  server confirms each stored live window; catch-up skips any confirmed range at
  any resolution and never sends an unfinished minute. This is how Prometheus
  remote write (the sender tracks what was accepted) and Netdata (the receiver
  asks for the missing range) avoid re-sending. Re-sending only the offline gap,
  and collapsing copies in the store, were weighed and not taken.
- **B. Admission: by the fill of the server's own catch-up queue,** the Zabbix
  model (its server stops taking proxy uploads at 80% of its write cache and takes
  proxies one at a time until 20%). A machine holds a share only while the server
  holds one of its batches; live readings are always served first. The draft's
  response-time formula (the Netflix and Envoy style) and fixed caps alone (the
  Netdata style) were not taken. Staging's nightly drill sharing the store is not
  a deciding factor for now.
- **C. Fairness: by customer** (organization). One customer's catch-up uses at
  most its share while another customer's machines wait.
- **D. Adjacent defects.** The health-summary floor (D7) and alert loss (D12)
  join. The default-tenant fallback (D13) is out — more than one tenant is a low
  product priority — and is not recorded as debt.
- **E. The promise:** live readings reach the store within 5 seconds during any
  catch-up, and 5,000 machines with a one-hour gap each are all filled within 15
  minutes. None of Zabbix, Netdata or Datadog publishes a catch-up speed; they cap
  how far back they accept data (Zabbix 1 hour of offline buffer by default,
  Netdata 1 day, Datadog 1 hour unless historical ingestion is enabled).
  OpenGate accepts 30 days.
- **F. Order: in turns, newest first.** Each waiting machine gets one batch per
  turn, and each machine drains its newest 48 hours before older history. Shortest
  gap first and longest gap first (the draft) were not taken.
- **G. Store-side rollups: removed.** History: added on 2026-07-01 with the first
  telemetry ingest to replace TimescaleDB's rollups (TimescaleDB cannot combine
  them with row-level security), meant for short raw retention and long rollup
  retention. The free single-node store has one retention for everything
  (per-series retention is an Enterprise feature), found on 2026-07-04, when
  min/max/last were cut as unread and the average kept for long-range queries.
  That read path was never wired: charts read the raw series and downsample at
  query time. Since 2026-07-29 raw readings arrive every 60 s, so the 1-minute
  rollup duplicates them. The original plan said catch-up must never pass
  through the rollups; it does. Removing them cuts edge series to a third.
- **H. Presence: written on change, last-seen in bulk.** Connect writes online,
  disconnect writes offline, and a sweep every 5 minutes refreshes last-seen for
  every connected machine and reconciles stale online rows; a failed write is
  retried and never ends a connection. Kubernetes moved node heartbeats to a small
  lease record to spare its store at 5,000 nodes; Nomad keeps heartbeat timers in
  memory and writes status only on a change; Zabbix writes host availability on a
  change. Working status out from last-seen (the Tactical RMM shape) and keeping
  the per-heartbeat write were not taken.

Decisions I–M were taken after the second review (section 2).

- **I. A missed catch-up answer: back off and keep trying.** The machine pauses
  catch-up and sends the same batch again after 1, 2, 4… minutes, up to 30,
  with no limit while the connection lives, and keeps the connection, so a
  technician's remote session is not cut. Prometheus remote write and vmagent
  retry with growing waits for as long as they hold the data. Reconnecting after
  two misses (it cuts remote sessions) and stopping until the next reconnect (a
  desktop's gap could stay open for weeks) were not taken.
- **J. Delivery: at least once.** A repeat comes only when an answer is lost (a
  cut link or a server fault), when a machine crashes before its next save to
  disk (about a minute), or when the server asks again (decisions N, O) — and
  changes no chart, since every chart reads
  averages, lows and highs; Q9 counts repeats against lost answers. Prometheus
  remote write has the same property. Folding copies together in the store, a
  use its documentation does not describe, was not taken.
- **K. The promise is proven on production's shape, and measured first.** A new
  job in the Performance Stack nightly runs on an ARM runner with the server and
  store at production's limits, and its baseline (P7a) lands before the shared
  writer. Today's runner (it compares nights only), the staging load tests
  (staging shares the store with production) and gating every promotion on the
  run were not taken.
- **L. Each machine sends its live window at its own point in the minute,**
  taken from its machine id, as Prometheus offsets each scrape target and
  Telegraf jitters its flush. More live room on the server alone (the 5 s
  promise missed every minute after a mass reconnect) and leaving it to the test
  (a pattern a real restart does not produce) were not taken.
- **M. Agents never run ahead of the server.** Ivan reinstalls every agent by
  hand (two active agents at most); the deploy pipeline pushes none, and an
  over-the-air update refuses an older version. Production's agents are deleted
  before the work starts and reinstalled once commit 2 is deployed (Ivan,
  2026-11-08); staging keeps the pipeline's two throwaway machines. So no
  message announcing what the server supports is needed, and a new agent with
  an old server is not a case to handle.

Decisions N–U were taken on 2026-11-07, after the third review (section 2).

- **N. A store crash is repaired by asking again.** A confirmation still means
  accepted by the store. The server reads the store's start time every few
  seconds; when it changes, every connected machine that supports it is asked to
  send again everything after 30 s before the restart, and a machine offline then
  is asked at its next connect. Netdata's central node asks each machine for what
  it lacks, read from its own disk, so a crash heals itself; Elasticsearch and
  InfluxDB 3 answer only after a disk write by default; Kafka and Mimir after two
  copies hold it; Prometheus remote write, vmagent and Datadog call a reading
  delivered once accepted. Confirming only after the store's save (it slows a
  laptop's two-week catch-up from minutes to about an hour), stating the loss,
  and saving every second were not taken.
- **O. Dropped live minutes are asked for again, machine by machine.** The server
  notes each connected machine's first dropped minute and, once the queue is
  below its low mark, sends only those machines the re-send request, spread over
  a minute. It replaces the restart to every machine, which made an old agent
  re-send its whole session. A timer on the machine, and keeping the outage-only
  restart, were not taken.
- **P. One sender per class.** Live readings have their own sender and never
  wait behind a catch-up request; catch-up keeps large requests. Kubernetes gives
  each priority of API request seats that lower ones cannot take. Short, small
  catch-up requests on one sender were not taken: one sender sending 64 KiB
  requests in turn moves too few readings for the 15-minute promise.
- **Q. P7a is a go/no-go.** If the store cannot take the rate decision E needs,
  P3 does not start, and Ivan chooses from the measured rate: more processor, a
  second catch-up sender, or a slower promise.
- **R. A customer may always send one batch a turn,** whatever its size, and the
  overshoot is charged to its later turns, so no number of waiting customers
  locks every one out.
- **S. Messages are checked before they are read,** in P3: the declared length
  against the largest message a machine sends, a server-wide allowance for
  messages being read, and Go's memory limit below the container's. gRPC refuses
  an oversized message from its length header before reading it. A package of
  its own before P1, and keeping a cap on machines catching up instead, were not
  taken.
- **T. The test store carries production's settings and load:** its cache
  setting, its scrape configuration, and a background writer at production's
  scrape and CI-trend rate. Settings alone, and limits alone, were not taken.
- **U. Technician sessions run during the catch-up test and are gated** on the
  limits the busy-morning profile holds. Recording them without a gate was not
  taken.

Decisions V–AC were taken on 2026-11-08, after the fourth pass (section 2).

- **V. Two commits.** Commit 1 carries what the baseline needs and the small
  fixes (P1, P2, P7a, P8, P10); commit 2 everything after the go reading (P3–P7,
  P9, P11). Each commit costs a full gauntlet run and a hand reinstall of the
  agents, and the nightlies are there to catch regressions. One commit per
  package, and a third commit for presence and alert answers, were not taken.
- **W. Rollups go in commit 1.** History in decision G: they were built so long
  charts could read small averages, which the free store's single keep-time
  ruled out, and no chart ever read them. A clean stop saves first and
  production has no agents during the work, so the restart costs nothing, and
  the test store matches production from its first night. Commit 2, and keeping
  the hourly rollup for a long-chart read path, were not taken.
- **X. A hard failure of the server and the store together is accepted and
  stated.** The few seconds the store had accepted and not saved stay lost; the
  Device Health chapter says so. Prometheus remote write and Datadog accept the
  same loss. A machine re-sending its last confirmed minute on every reconnect
  (MQTT's at-least-once delivery, Netdata's back-fill on connect), and the
  server keeping the store's start time in the database, were not taken.
- **Y. The go/no-go's pass mark:** at production's limits and background load,
  the store takes at least 10,000 readings a second for 15 minutes, never runs
  out of memory, and 99% of the live-shaped writer's requests are accepted
  within 5 s; on a quiet minute it stays under half its processor and memory,
  the spare its own guidance keeps for spikes, which the storm may use. The
  median of three runs decides. Keeping the spare during the storm, and the
  rate alone, were not taken.
- **Z. The chip difference is measured once and then stated.** The ARM runner's
  Neoverse N2 against production's Neoverse N1, read by one fixed benchmark on
  both; the runner's processor is recorded each night, since GitHub's hardware
  can change. Shrinking the limits by the measured or the vendor's ratio was not
  taken; production carries two agents at most.
- **AA. Decision E gates from commit 2,** the commit that makes the promise;
  until then its readings are published. Failing every night from commit 1 was
  not taken.
- **AB. No browser test of the gap fill.** The acceptance tier proves each
  outcome on every commit (Q19–Q29), and the nightly network drill proves the
  real agent's gap fill. A browser test with a stopped agent, about 3–4 minutes
  on every gauntlet run, was not taken.
- **AC. The measurement job is rehearsed before its push, and one fix may
  follow it.** A defect in the P7a job found on the ARM runner is the only fix
  pushed alone, since commit 2 cannot start without the job's reading. Taking
  the go/no-go from the workstation instead was not taken.
- **AD. The store's memory is measured before it is raised.** P7a reads the test
  store at production's 512 MiB with production's 30-day series count loaded,
  and the go/no-go reads memory beside the rate; a failure on memory adds more
  store memory to decision Q's choices. The node has about 6.6 GiB of memory no
  pod requests and 0.19 cores of processor, so more processor is the scarce
  choice. Raising it to 1 GiB now, and shrinking its cache share, were not taken.
- **AE. The monitoring stack may chart product readings, and never alerts on
  them** (Ivan). Commit 1 removes its three product alert rules: the drop ratio above 5% (D19), a
  rule pack raising more than 5 alerts a device a day, and a customer's hourly
  alert ceiling refusing detection. Fixing the drop ratio's formula in place
  was not taken.
- **AF. Staging's machine readings get a store of their own,** on scratch space
  in staging's namespace, inside the free tier: no new volume, the node
  unchanged. Production's stay in the monitoring store. A machine-readings store
  per environment (a paid volume for production), and one store filtered by
  environment on every call, were not taken.
- **AG. A stopped deletion is finished within the hour, and the sweep reads only
  its own environment.** The server re-runs unfinished deletion records every
  hour, not only at start-up; the sweep stays for leftovers. Removing the sweep,
  and scoping it alone, were not taken. Both land with AF in one commit before
  commit 1.
- **AH. `golang.org/x/net` moves to 0.62.0** (2026-11-08), a major move under
  [`tool-versions.md`](../rules/tool-versions.md) for a `0.x` line. The server
  parses no HTML, so neither hole is reachable today, but the image build refuses
  every build until it moves. Keeping 0.61.0 under a dated exception was not
  taken.
- **AI. A serious hole is refused at every stage, whatever line fixes it**
  (2026-11-08). A HIGH or CRITICAL advisory in what the server image ships, fixed
  only in a newer minor or major, is refused locally, in CI and at the image
  build alike, until Ivan answers: take the release, or a dated exception every
  scan reads. Go and the images join the rule `cargo audit` and the npm audit
  already keep. The earlier stages read the Go and OSV databases, which carried
  this CVE ten days before Trivy's, so they refuse at least what the image build
  would. Listing it and passing everywhere, which loosens the image scan, was not
  taken.
- **AJ. The verdict lands in commit 1** (2026-11-08), as P12. D21 reaches staging
  and production with it, so D21's post-deploy checks wait for commit 1's deploy.
  A commit of its own was not taken.
- **AK. The breakpoint leg is fixed last** (2026-11-10), as P13, sized against
  commit 2's server, since commit 2 moves where the server gives out. Until then
  the leg reads invalid each night and its trend line stays blank from
  2026-11-08, so commit 2's break point is compared with a night before #543.
  Measuring and sizing now, and a larger allowance without measuring, were not
  taken. The ceilings are kept: k6 and Gatling both treat a nearly saturated
  generator as a failed run and add generator capacity.

## 10. Starting values

Engineering defaults, each derived rather than picked, and tuned only by P7's
measurement against decision E.

| Value | Start | Why |
|---|---|---|
| Writer senders | 1 per class, live and catch-up | Fixed whatever the fleet size (decision P); a second catch-up sender only by decision Q |
| Flush trigger | 256 KiB encoded or 1 s age | vmagent's default flush is 1 s; 1 s plus a send keeps live under the 5 s promise |
| Live bound | 4 MiB | About 10 s of the 5,000-machine live flow (about 2,200 readings a second at about 160 encoded bytes each), which arrives evenly through the minute (decision L) |
| Catch-up bound | 8 MiB | About 2 s of the 20,000-readings-a-second budget, the scheduler's configured figure until P7a measures the store; more would only lengthen the wait for an answer |
| Byte budget | 12 MiB, the two class bounds together | One ceiling for everything the writer holds, counted on encoded bytes |
| High and low marks | 80% and 20% | Zabbix's throttling marks |
| Customer share | An equal part of the catch-up bound for each waiting customer, at least one batch a turn; all of it with one | The overshoot is charged to later turns, so no waiting customer is locked out, however many wait (decision R) |
| Catch-up answer bound | 15 s (10 s in the queue plus the 5 s write timeout) | A batch waiting longer is answered `DeferBackfill` |
| Defer retry time | Estimated drain time of the queue, 1–30 s, spread by position in throttled mode | The machine adds its own ±25% |
| Machine answer deadline | 60 s | Four times the answer bound, so it fires only on a server fault |
| Missed-answer retry | 1, 2, 4… minutes, up to 30, no limit | Decision I |
| Re-send request spread | Spread over 60 s | A minute of catch-up requests after a restart or a drop, not one second's |
| Re-send margin | 30 s before the server's last read of the old start time | Covers the store's ~6 s of unsaved readings and the 5 s between reads, however long the store takes to come back |
| Store start-time read | Every 5 s | Well inside the 30 s margin |
| Message ceiling | The largest message a machine sends (1 MiB today, the discovery report) | Checked on the declared length before reading (decision S) |
| Reading allowance | Sized in P3 from the heap reading (Q3) | Covers messages read but not yet decoded |
| Go memory limit | 90% of the container's memory limit, taken from the limit itself (345 MiB in production) | Go's guide keeps 5–10% for memory Go does not manage; its cleaner then works to the limit rather than to twice the live heap |
| Presence sweep | Every 5 minutes | Last-seen matters mainly for disconnected machines, which get it at disconnect; bounds staleness after a server crash |
| Catch-up floor | 30 days, the store's retention | One value, held equal by a parity test |

## 11. Proof still owed

Read on 2026-11-08, before production's agents were deleted (erasing a machine
deletes its series from the store,
[ADR-054](../../docs/adr/ADR-054-data-lifecycle.md)). One production machine
was mostly offline and the other connected on and off (Ivan); the store holds
readings only for the current device records, from 2026-10-31 02:11.

| Reading | Result |
|---|---|
| The store's import latency | **Recorded nowhere.** The store times only queries; the server times no write. P3's request duration per sender is the first reading; P7a measures it |
| The 53 `persist_failed` drops | Bursts of exactly 5 (2 from 2026-10-30), at 13 moments over 15 days, all with the machine connected, the store up since 2026-10-07, no server restart, no processor throttling on either side, and 0 import errors at the store. Most likely the server's own encoder refusing one reading (`vm.go:184`), which fails the whole burst (D4); unconfirmed, because the drop is logged only at debug level |
| Duplicates for the production machine | **1 minute stored twice in 1,848**, on 2026-11-08, across about 24 reconnects with 168 grants. The store keeps every copy: checked on a local v1.114.0 store, three copies survive a forced merge and a restart. Two hours later the same export held 80 minutes: staging's hourly sweep deletes production's machine readings (D21), so this reading says nothing about catch-up until D21 is fixed (D18) |
| The "drop ratio > 5%" alert | **Blind at this fleet size.** Its denominator is held at one message a second at least, and production receives 0.12, so it reads 2% where the real ratio is about 17%. Unclamped, the ratio was above 5% in 95% of the last 30 days and peaked at 40%. Never fired. See D19 |
| Memory and processor, 30 days | Server: 25 MiB now, 32 MiB at most, of 384 MiB; processor 0.001 cores typical, 0.012 at most, of 0.25. Store: 430 MiB of unshared memory now, 444 MiB at most, of 512 MiB — 85% of its limit, above the 80% the store's own recommended alert warns at, while almost idle (0.01 cores typical, 0.05 at most, of 0.5) with about 14,500 active series. No memory-limit hits; no restart since 2026-10-07. See D20 |
| Background write rate into the store | Scraping: 959 readings a second on average, 1,073 at most (15 s interval). Imports (machine readings and CI trends): 0.5 a second on average, 94 at most over 5 minutes. Concurrent insert capacity 2 |

Found beside them: the rollup rule matches `opengate_edge_.*`, so it also copies
the server's own counters (`opengate_edge_telemetry_drops_total:1m_avg` and
the like), which P10 removes too.

P7a measures what production cannot show: the store's sustained import rate at
half a processor, against the about 10,000 readings a second the 15-minute
promise needs (7.2 million readings in 15 minutes, plus live load), and the
number of writes the store runs at once. A failed measurement stops P3
(decisions Q, Y).

Once, during P7a: the fixed one-minute processor benchmark as a pod in the
staging namespace on the production node, at a quarter processor, run by Ivan's
command; its ratio to the runner's figure is stated in
[ADR-107](../../docs/adr/ADR-107-where-a-run-happens.md) (decision Z).

Already measured on 2026-11-06 (production and staging, last 30 days): one
production machine connected; drops by reason as quoted; 20,049 store imports
with 0 errors; 470 staging grants against 7,840 deferrals, with places peaking
at 4.

### Commit 1's readings and what follows it

Moved from commit 1's micro-plan, retired in commit 1 (Ivan, 2026-11-09).

On the workstation, kept in this section and quoted in the commit message:

- The `catch-up` job rehearsed through its own compose files and scripts at
  production's limits with the background writer on: the probe at one and two
  senders, the quiet minute, the store's memory, and today's code against
  decision E.
- The job's images started once under arm64 emulation.
- `go test -bench` for `CheckSample` and the filtered `bufferTelemetry`.

#### Readings taken (2026-11-08/09, workstation, 24 x86 processors)

- **Per-reading check:** 8.8 µs per 18-reading window (about 0.5 µs a reading),
  no allocation beyond the window's own; at 5,000 machines a window a minute,
  about 0.07% of one processor. Measured with a throwaway benchmark, deleted
  after.
- **Smoke run** (20 s probe, 100 machines for 2 min): the stack starts at
  production's limits (server 0.25 processor and 384 MiB, store 0.5 and
  512 MiB, the store on production's arguments); one sender wrote about 240,000
  readings a second for 20 s; 44 of 49 watched live minutes were seen, none
  within 5 s, p99 8.9 s — today's server writes a burst at the next heartbeat.
  It found three harness defects, fixed with tests before the full rehearsal:
  a machine catching up a gap never advertised catch-up, a live window stamped
  before the arrival window fell under the ten-second floor, and a message
  pushed before the grant ended the wait for it. The profile's fleet now
  returns within a one-minute phase before the measured drain.
- **ARM images:** every image the job starts or builds from — `postgres:17-alpine`,
  `victoria-metrics:v1.114.0`, `alpine:3.21.9`, and the server's bases
  `node:24-alpine`, `golang:1.27-alpine`, `alpine:3.20` — publishes a
  `linux/arm64` build, read from each image's published platforms; this
  workstation has no ARM emulation registered, and registering it takes a
  privileged container, so none was started under emulation.
- **Full rehearsal** (2026-11-09, store and server at production's limits, background
  writer of 14,400 series every 15 s):

  | Reading | Value |
  |---|---|
  | Processor benchmark, quarter processor | 1,454 rounds a second (x86 workstation) |
  | Quiet minute | store at 0.9% of its processor, 9% of its memory |
  | One sender, 15 min | 321,000 readings a second; 899 of 899 live-shaped requests within 5 s, p99 127 ms; store peak 442 MB (82%); no restart |
  | Two senders, 15 min | 262,000 readings a second; 900 of 900 within 5 s, p99 385 ms; store peak 527 MB (98%); no restart |
  | Go/no-go on these readings | go |
  | Fleet, today's code | **invalid run**: the server's process start time changed mid-run, and 1,150 of 5,000 machines failed to enrol (`enroll … context deadline exceeded`, filing answered 429) at a quarter processor; register p50 26 s, p95 60 s |
  | Fleet catch-up, today's code | 4,872 machines caught up; 9,746 batches stored, 1,779 told to wait (`deferred_busy`), 4 `deferred_write_failed`; last gap stored 2,228 s after the start |
  | Live delay, today's code | 1,674 of 1,697 watched minutes seen, 6 within 5 s; p50 13.4 s, p99 50.4 s, max 89 s |
  | Server during the fleet | 380 of 384 MiB, its whole quarter processor; 877 `persist_failed` drops |

  The three open items, settled 2026-11-09:
  1. **No second start.** The server's start time is the kernel's boot time plus the
     process's age, and the boot time is re-read on every scrape. On this WSL workstation the
     boot time moves forward about 1.2 s every 30 s (sampled from `/proc/stat` over two
     minutes) as the virtual machine's clock is stepped, about 86 s over the 36-minute walk
     against the 74 s read. The compose file starts the server with `restart: "no"`, so a real
     second start leaves it dead. The last eight nightlies on GitHub's runners carry no restart
     finding, so the check holds there.
  2. **Enrolment taken out of the return** (Ivan, modal): the harness's `-enrol-first` installs
     every machine before the walk, eight at a time, each waiting for its answer, at
     production's quarter processor — enrolled, connected once so the server writes its row
     (filing a machine with no row answers 404), then filed; the return minute dials with held
     certificates, as installed agents do. The server's registration timing is read after the
     install and subtracted, so the run's figure is the walk's. Last night's 0.25-processor scaling sweep enrolled about 8–9
     machines a second.
  3. **The probe publishes whatever the fleet's verdict**: `perf-vm-push.sh` pushes the store
     probe and the runner's benchmark before it reads the verdict, and keeps only an invalid
     fleet's own readings out.

- **Fleet re-rehearsal with `-enrol-first`** (2026-11-09): 5,000 of 5,000 machines
  installed and filed in 5 min 20 s, none failed. From minute 3 of the walk the server sat
  at its whole quarter processor and its 384 MiB; its health check answered 503 from
  about minute 33, and it was killed out of memory 35 minutes into the walk (exit 137,
  `OOMKilled`). **Ivan (modal): this is today's baseline** — the leg stays at 5,000 machines
  and production's limits, the fleet's nightly figures read invalid until commit 2, and
  commit 2 is judged by whether the server survives the walk.

- **Server-death check** (2026-11-09, 200 machines, server killed two minutes into a
  four-minute walk): the harness ended 1 min 45 s after the kill and wrote its bundle; the
  merge, the go/no-go and the push of the probe ran. It found two harness defects, fixed with
  tests: a target that stopped answering before the end read as a valid run (the restart
  check needs both readings), now invalid; and the phase probe kept `-backfill-gap` and
  `-live-every`, so each probe trip caught up an hour (215 machines caught up of 200).

- **Monitoring apply** (found by its shell test, 2026-11-09): the read-back handed the
  rendered dashboards ConfigMap to `python3` as one argument, which the new trend panels took
  past Linux's 128 KiB a single argument carries; the desired copy is now a file, and a test
  compares a 300 KB ConfigMap.

#### Added with Ivan's say-so (2026-11-09)

- Two Device Health acceptance tests that could not fail — they looked for a
  refusal reason every server publishes at zero — read the counted value instead.
- The dependency check's core test fails when its driver stops before its last
  check, rather than reading as zero of zero.
- The catch-up fleet enrols before the walk (`-enrol-first`), so the return is a
  reconnect with held certificates (open item 2 above).

#### Where the code differs in detail from the micro-plan

- The background writer is a process the job starts beside the stack
  (`storeprobe -mode=background`), so the override needs no image of its own.
- The probe's catch-up timestamps walk back one minute at a time across 5,000
  machines, so no reading is written twice.
- The rehearsal ran the harness in a container on the stack's network and
  published the store on another port, since this workstation has no
  password-free `sudo` for the hosts entry and the gauntlet's test store holds
  port 8428; the job itself runs as written.

#### After the push

1. Three `perf-stack` runs dispatched on `dev`; the median decides go or no-go
   (decision Y). A defect in the `catch-up` job itself is the one fix pushed
   alone (decision AC).
2. Ivan runs `scripts/cpu-benchmark.sh cluster` once; the ratio goes into
   ADR-107 with commit 2 (decision Z).
3. After CD deploys: D21's section-7 checks (memory
   `numeric-ingest-catchup-spec-agreed`), the network drill's S1/S2 figures
   with P1's generator, and the image build's scan-before-tag run.

#### Read after the push (2026-11-10)

- **Go/no-go run 1** (42736498425): no reading. The two-sender probe read the store's own
  figures with no time limit and held the job to its two-hour limit. Fixed alone under decision
  AC (Ivan, modal) as `0367f92b`: reads bounded at 10 s, a store that stops answering fails the
  go/no-go, and the job prints the store's state and log. Run 2 is 42749117504.
- **Production node's processor benchmark** (decision Z, Ivan, modal): 846 rounds a second at a
  quarter processor, arm64. The runner's figure comes from run 2's log.
- **Commit 2's micro-plan** (`numeric-ingest-commit-2.md`): approved with choices C1–C12 (Ivan,
  modal).
- **Go/no-go: GO** (decision Y, median of three on `dev`). Run A 42772986629: 245,119 readings a
  second with one sender for 900 s, 899 of 899 live requests within 5 s, quiet minute 0%
  processor and 9% memory. Run B 42871003933: 246,021 a second, 899 of 899. Run C 42900982303:
  241,456 a second, 900 of 900, 0% and 9%. The nightly on `main` (42843873057) read 253,950. The
  fleet half reads invalid by design: today's server stops at 384 MiB during the walk.
- **Runner's processor benchmark:** Neoverse-N2, 1,220–1,261 rounds a second at a quarter
  processor against production's 846 (Neoverse-N1), so the runner is about 1.44–1.49 times
  production per processor (decision Z, into ADR-107 with commit 2).

#### The breakpoint leg (2026-11-10)

- Since 2026-11-09 every breakpoint run reads invalid: generator memory 87–96% of its 10 GB
  (ceiling 90%) and the processor refused 20–31% of the run (ceiling 20%). The nights before
  read 73–76% and 15–17%, with one night at 85%.
- **quic-go 0.65.0 is not the cause, on either end.** Workstation, generator in a container on
  one processor and 6 GiB, harness alone (no technician generator, no relay echo), five-minute
  hold, server and generator built at `d6d3275a`:

  | Machines | Server built on | Generator peak | Server memory | Connected |
  |---|---|---|---|---|
  | 2,000 | quic-go 0.64.1 | 349.0 MiB | 283 MiB mean | 2,000 |
  | 2,000 | quic-go 0.65.0 | 332.8 MiB | 284 MiB mean | 2,000 |
  | 8,000 | quic-go 0.64.1 | 1,286.4 MiB | at its 1 GiB limit | 7,225 |
  | 8,000 | quic-go 0.65.0 | 1,279.6 MiB | at its 1 GiB limit | 7,428 |

  The generator itself built on 0.64.1 against 0.65.0 read 344.0 against 349.5 MiB at 2,000.
  At 8,000 the harness used 22–25% of one processor, so most of the CI generator's processor
  goes to the technician generator, the relay echo or the estate build, not yet measured apart.
- **What changed is the server.** From the nights' bundles, the server holds more machines at
  the middle rungs: about 3,450 to about 3,900 at the 4,000 rung, about 4,300 to 5,500–6,500
  at the 8,000 rung. The generator's processor refused at those rungs rose from 21–34% to
  44–65%. The only server changes between the last valid night (`745ddb71`) and the first
  invalid one (`478a676b`) are #543 (pgx 5.12, puddle 2.2.3, quic-go 0.65.0) and D21; quic-go
  accounts for about 200 machines at 8,000, so pgx is the likelier cause, unproven.
- The generator's reading is its whole shared allowance: the harness, the technician generator
  and the page cache charged to it. The runner has 4 processors and 16 GB; the stack's shares
  are server 1, database 1, store 0.5 and generator 1 processor.
- Invalid runs stay out of the trend, so the leg's line is blank from 2026-11-08 and the
  regression gate compares it with nothing. P13 sizes it last (decision AK).

## 12. Fixes

Every fix this work made outside its packages, and every fix still owed, in one
place (Ivan, 2026-11-11).

### Fixed

- **The store probe held its job to the two-hour limit** (go/no-go run 1): its
  reads of the store's own figures had no time limit. Reads are bounded at 10 s
  and a store that stops answering fails the go/no-go (`0367f92b`).
- **A server that died in the fleet walk took the go/no-go reading with it**
  (runs 2 and 3): the probe's readings are written into the bundle directory the
  moment it ends and folded in after the walk (`c582961d`).
- **Found rehearsing commit 1, fixed in it** (`4e7ef5a3`): the monitoring apply
  passed the dashboards past Linux's 128 KiB argument limit; a target that
  stopped answering read as valid; the phase probe kept the catch-up and live
  flags; filing a machine before its first registration answered 404; two
  Device Health acceptance tests could not fail.
- **A replaced connection's late teardown dropped the returning connection's
  catch-up grant and its alert-rule coverage** (found in review, fixed in
  commit 2): only the machine's current connection clears them now.
- **GO-2026-6859** in `net/http`, fixed by Go 1.27.4 in commit 2.
- **Admission checked every grant for expiry on every batch and grant request**
  (found when the 500-customer fairness test took 156 s on four processors,
  Ivan): it now reads the grants only once the soonest deadline has come, with
  the same answers; the package's tests went from 214 s to 105 s (commit 2).
- **The dependency check read a 127-file release commit as two answers**:
  paginated, gh repeats a commit once per hundred files; a commit is read
  unpaginated (commit 2).

### Planned

1. **Catch-up at production's processor.** The 5,000-machine reading on the
   workstation (server 0.25 processor and 384 MiB): live minutes 100% within
   5 s; 4,755 of 5,000 machines' hours filled, the last after 1,730 s against
   960; registration at the timer's 60 s ceiling; a technician's sign-in timed
   out (503). The server sat at its whole quarter processor from the return to
   the end, memory at its ceiling, the catch-up queue at most 2.1 of its 8 MiB,
   so the processor is the limit, not the store. Next: one catch-up run with the
   server's processor profiled at the return, to see where the quarter processor
   goes (handshakes, registration's database round trips, encoding), then the
   fix and the gates read again. Admission's sweep of every grant on each call
   is already fixed (above); encoding still costs 12 allocations a reading. Until it lands, the nightly catch-up job fails
   the night on the two fill gates (Ivan: commit now, fix next).
2. **The read allowance, measured.** It stays 32 MiB (Ivan) until the fix above
   lets the catch-up queue fill; then the heap is read with the queue full and
   5,000 senders (Q3) and the allowance set from it.
3. **Busy-morning registration on the older runner chip.** Every night on an
   AMD EPYC 7763 runner reads a registration median of 340–375 ms, every other
   chip 3–4 ms; the generator had 84% of its processor spare. Each registration
   makes about six database round trips through a 25-connection pool against a
   database at one processor. Next: the bundle carries the pool's waits and the
   database's throttling for the wave, the leg is dispatched until it lands on a
   7763, and the cause is fixed; then the regression gate compares a night only
   with nights on the same processor (Ivan).
4. **The breakpoint allowance, confirmed.** Harness and technicians hold 0.75
   processor each. The confirming walk was stopped at the 1,000 rung, where k6
   was still throttled in 25.8% of its periods; the next nights' breakpoint
   verdicts confirm it or send it back to a modal.
5. **Readings left to the nightlies** (Ivan): the soak's leak slopes at 500
   machines (Sunday) and the breakpoint walk (nightly), read after the deploy.
6. **`perf-stack.yml` split by domain** (Ivan, 2026-11-11: four workflows and
   a shared leg). 15 of the 20 runs to 2026-11-11 were red: breakpoint in 13,
   catch-up in all 9 since it joined, scaling in 6, the regression gate in 8.
   One red leg turned the whole night red and one alert carried every family,
   so the 11-11 busy-morning slowdown read as noise beside expected reds. The
   split: `perf-scaling.yml` (0.25–2 processors), `perf-volume.yml` (500,
   2,000 and 8,000 machines), `perf-shapes.yml` (busy morning, link came back,
   raise it until it gives) and `perf-catchup.yml` (ARM, production's limits),
   each with its own schedule slot, concurrency group, regression gate over its
   own legs and an alert naming its domain; one reusable workflow
   (`workflow_call`) holds the leg every domain runs — build the stack, walk,
   verdict, upload. The venue table (ADR-107), the trend store's workload names
   (ADR-038), `perf-stack.test.sh`, the CI/CD determinism and workflow-input
   sweeps and the Non-Functional Testing chapter follow the split.
