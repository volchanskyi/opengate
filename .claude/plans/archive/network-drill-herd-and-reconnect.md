# Network drill: a herd that is there, and a reconnect figure that is a reading

Pays down the two Medium entries in [`techdebt.md`](../../techdebt.md) that the
nightly link drill has carried since its first complete night, and closes the
three gaps the investigation behind them turned up.

Companion decision record: ADR-103 (written by this plan).

---

## 1. What is owed

### 1.1 The herd is gone before the scenario that needs it measures

The drill starts twenty simulated machines so its thin-uplink scenario measures
a site catching up as a herd. They are gone half a minute into the first
blackhole and never come back, so the three scenarios after the first run
against a fleet of one.

Confirmed on two consecutive complete nights. In run `34025636166` the first
blackhole drew 399 dropped datagrams out of the population behind the link and
the second drew 39; the thin-uplink scenario then published a staleness figure
identical to the lossy-link scenario's — 57 seconds against 57 — and the
lossy-link scenario has no herd contention by design.

### 1.2 The reconnect figure is its own phase clock

`netdrill_reconnect_seconds` read 13 on one night and 18 on the next, in both
scenarios that measure it. The machine's own log says why. From
`34025636166`:

```
09:51:58.036  connection lost, will reconnect
09:53:28.039  connection attempt failed  attempt=1  QUIC establish: timed out
09:53:28.353  reconnected successfully   attempt=2
09:53:28.355  registered with server
```

The outage is 180 seconds and the agent's idle timeout and establish timeout are
both 90, so the shape repeats exactly: notice the loss, spend one whole doomed
establish, and the link comes back part way through it. **The reconnect itself
cost 0.31 seconds.** The published 18 is the tail of the attempt it interrupted,
and the floor the figure is gated against is 120 — a number the measurement
cannot reach.

---

## 2. What the investigation added

Three findings that were not in the register, each confirmed against the code,
the cluster or a measurement.

### 2.1 A deferred machine gives up rather than asking again

The simulated machine asks the server for a catch-up slot and honours the answer
— request, wait for permission, then one batch at a time waiting for each
acknowledgement. But when the server says *wait your turn* it stops for good
([`soak_backfill.go:86-88`](../../../server/tests/loadtest/soak_backfill.go)):
`deferred (or unexpected) — shed load, drain nothing`.

The server hands a deferred machine a retry time and
[shortens it the longer it waits](../../../server/internal/agentapi/backfill_scheduler.go).
A real machine asks again. So even with the herd surviving the outage, sixteen
of the twenty would never catch up at all and the site's catch-up would end when
the first four finished — and the catch-up window is exactly what the staleness
figure is measured across.

### 2.2 The herd cannot be told apart from anyone else's

The herd carries the load run's machine names (`soak-t0-a…`), which is what lets
[`loadtest-cleanup.sh`](../../../scripts/loadtest-cleanup.sh) sweep the drill's
twenty the next morning at 05:00 — a workflow that does not know they were the
drill's. The scenario cannot count its own herd while sharing a name with
another run's.

### 2.3 The drill removes its pods and leaves its machines

The plan behind the drill defined cleaning up as pods
([`nightly-quic-network-drills.md`](nightly-quic-network-drills.md) §10.1,
criterion 10), following what the staging browser suite does — remove the pods
and the credential, and let the next deploy's database reset take the rows.

That holds for the browser suite, whose own workflow does the reset. It does not
hold here: the herd is swept a day later by the load run's cleaner, and the real
machine — named `netdrill-machine-<run>-<attempt>` — is swept by nothing. It
matches neither the load run's marker nor `soak-t%`, and no background loop
removes a machine on age (orphan reconcile, incident sweep, retention sweep and
session sweep were each checked). One orphan a night, cleared only when someone
pushes to `main`.

---

## 3. Settled decisions

### D1 — the herd is the simulated fleet, and it is taught to persist

Twenty copies of the shipped agent were measured rather than estimated: the
binary from CI, a real server, real enrolment.

| | Twenty simulated | Twenty real agents |
|---|---|---|
| Processor | 1.2 millicores, all twenty | ~406 millicores (20.3 each) |
| Memory | 17 MiB, all twenty | ~700 MiB (35–38 MiB each) |
| Names in the product | twenty distinct | twenty identical |

The worker has **350 millicores unclaimed** (1480m of 1830m committed), so
twenty real agents do not fit — and the figure is not an artifact of the
measuring machine's core count: pinned to two processors the agent drops from 30
threads to 8 and its processor use stays at 20.3 millicores, because the cost is
the sampler scoring host readings every second.

They would also all carry the same name. The agent takes its name from the
operating system and accepts no setting to change it; five real agents on one
host produced five machines in the product, every one named `DESKTOP-HV91KGN`.
One pod each would fix the name and not the processor.

So the herd stays simulated and gains three behaviours, each behind its own
switch, each off by default so the 05:00 load run is unchanged.

### D2 — what a reconnect figure is

Two numbers, published together, because the run tonight shows they answer
different questions:

- **`netdrill_reconnect_seconds`** — the link comes back, and the machine is
  registered this many seconds later, on the machine's own clock. What a site
  waits through. Gated at the existing 120-second floor, which is the worst
  healthy behaviour can produce (a 90-second establish plus a 30-second backoff
  cap).
- **`netdrill_reconnect_attempt_seconds`** — from the machine's last failed
  attempt to being back. 0.31 seconds tonight. It carries no luck about where in
  its cycle the machine happened to be when the link returned, so it is floored
  tightly at 35 seconds — the backoff cap plus an establish margin.

Both are taken from the machine's own log rather than from a five-second poll of
a status the server writes.

**What the second number includes, stated plainly:** the wait before the final
try as well as the try itself. The backoff delay is logged at debug level and
the machine runs at info, so the two cannot be separated without changing what
the pod logs — which is a change to a spec the staging browser suite shares. The
number is bounded by backoff cap plus establish time either way, which is what
makes the floor meaningful.

### D3 — the outage length varies

Drawn from the run's own seed so a night is reproducible and recorded, and
published as a row so a reader can interpret the figure beside it. Varied in the
first scenario only: the thin-uplink scenario's staleness figure is gated
against a rolling window, and an outage that changed length would move the size
of the backlog it is measured across.

### D4 — a machine that never came back is a reading, not an absence

The register's trigger says to guard the first scenario's empty reconnect
reading the way the second one is guarded — by going inconclusive. **This plan
does not do that, and the reason is the rule at the top of the runner:** a
scenario that could not observe the system emits nothing. A machine that never
returned was observed perfectly; what was observed is a failure, and it is the
failure that scenario exists to find. Calling it inconclusive would hide it.

So the scenario publishes `netdrill_reconnected` as one or nought, floored at
one. `netdrill_reconnect_seconds` stays absent, because a machine that did not
reconnect has no reconnect duration.

### D5 — the drill removes its own machines

Through `DELETE /api/v1/devices/{id}`, the door a technician uses, with the
administrator credential the run already holds. Before the probe pod is removed,
since every request goes through it.

---

## 4. Scope

### In scope

- The simulated fleet harness: reconnect, retry-when-deferred, its own names.
- The scenario runner: read the herd, the two reconnect figures, the varied
  outage, the reconnected reading.
- The workflow: the herd's stay, the harness's verdict in the evidence, the
  machines removed in teardown.
- The regression check: floors for the new series.
- Docs, ADR-103, and the register entries this pays down.

### Out of scope

- Any change to shipped agent or server behaviour. The drill builds the
  instrument, not the repairs — the boundary the original plan set.
- Alert delivery. It has its own plan, written after this one lands.
- Breaking the link on the server's own side. Still blocked on the free-tier
  storage cap and the shared node; its register entry stays.

---

## 5. Steps

Each step is test-first. A step's tests are written and seen to fail before its
source is touched.

### Step 1 — the harness persists (Go, `server/tests/loadtest/`)

1. **Tests first** in `soak_test.go` / a new `persist_test.go`:
   - a machine whose connection is severed during the hold dials again when the
     switch is on, and reports the severance when it is off;
   - a machine told to wait for a catch-up slot asks again when the switch is
     on, and sheds load when it is off;
   - the machine names carry the given prefix, and the default is unchanged;
   - the reconnect count reaches the run's verdict.
2. Three flags, defaulting to today's behaviour: `-reconnect`,
   `-retry-deferred`, `-hostname-prefix`.
3. `runAgentWithContext` re-dials, re-registers and re-sends its backlog after a
   severance, until the context ends.
4. `drainBackfill` honours the server's retry time when told to wait.
5. `planAgents` takes the prefix.
6. New non-test `.go` files under `server/` are assigned in
   [`mutation-shards.sh`](../../../scripts/lib/mutation-shards.sh) — the partition
   check fails the gauntlet otherwise.

### Step 2 — the scenario reads its herd (`scripts/fault/network-drill.sh`)

1. **Tests first** in `scripts/tests/network-drill.test.sh`: a herd present, a
   herd absent (inconclusive, no rows), a herd partly back.
2. `fleet_online()` counts the drill's own machines, by its own prefix, online.
3. The thin-uplink scenario refuses to publish a staleness figure without them.
4. `netdrill_fleet_online` is published so the trend records the herd each night.

### Step 3 — the reconnect figures (`scripts/fault/network-drill.sh`)

1. **Tests first**: the two figures derived from a canned machine log; a log with
   no reconnect line; a machine that never came back; a varied outage recorded.
2. `MACHINE_POD` reaches the runner.
3. The first scenario's outage length is drawn from the seed and published.
4. Both figures are read from the machine's log; `netdrill_reconnected` is
   published either way.

### Step 4 — the workflow (`\.github/workflows/network-drill.yml`)

1. **Tests first** in `network-drill.test.sh`: the herd's stay covers the
   scenarios; the harness's verdict is collected; the machines are removed
   before the probe pod.
2. The herd's stay is extended past the last scenario.
3. `loadtest-quic-incluster.sh collect` puts the harness's verdict in the
   evidence bundle.
4. Teardown removes every machine the drill enrolled and proves none remains.

### Step 5 — the floors (`scripts/network-drill-regression-check.sh`)

1. **Tests first**: each new floor fires and each holds.
2. `netdrill_reconnected` below one; `netdrill_reconnect_attempt_seconds` above
   35; `netdrill_fleet_online` below its minimum.

### Step 6 — the record

1. `docs/infrastructure/Fault-Injection.md` describes the drill as it now is.
2. ADR-103 with the decisions in §3, and its row in
   [`decisions.md`](../../decisions.md).
3. **Both register entries removed from [`techdebt.md`](../../techdebt.md)** — the
   herd entry and the reconnect entry. A paid-down entry is deleted, not
   annotated. The two entries this plan does not pay stay untouched: network
   faults on the server's own side, and the alert a machine raises.
4. A **Completed** row in [`phases.md`](../../phases.md), and this plan moved to
   `plans/archive/` with its links bumped one level deeper — in the same commit
   that lands the implementation.

---

## 6. Definition of done

1. A severed simulated machine comes back, and one that is told to wait asks
   again — both only when the drill asks for it, with the 05:00 load run's
   behaviour unchanged and asserted so.
2. The thin-uplink scenario publishes a staleness figure only when its herd is
   there, and the herd's size is in the trend.
3. The reconnect figures are the machine's own account, the outage varies, and a
   machine that never came back publishes a nought rather than nothing.
4. The drill removes every machine it enrolled and proves none remains.
5. The gauntlet passes, the two register entries are gone, and this plan is
   archived.
