# Nightly QUIC Network Drills

Closes the **On-demand network drills deferred** entry in
[`techdebt.md`](../../techdebt.md): packet loss, outage and re-addressing on the
machine-facing QUIC path, run nightly as observability alongside the other
scheduled jobs.

Status: **specification — revised after verification; §13 gates implementation.**
Five design questions were settled against measured evidence; §2 records each
answer and the proof behind it.

Every factual claim in this document was re-checked against the repository, the
live cluster and the live OCI tenancy. Most held exactly. Seven did not, and one
check surfaced a product defect that changes what §1.3 can ask. §13 records the
verification, the corrections, and the two items that must be settled before
Phase A starts.

---

## 1. The problem, deconstructed

### 1.1 What is owed

The deployed fault drills cover pod deletion, bad rollout and edge 502/504
([`fault-tolerance.yml`](../../../.github/workflows/fault-tolerance.yml)). Nothing
covers the link between a customer's machine and the server. For a remote
management product that is the single most common failure in the field, and it
is the one the recovery machinery — reconnect backoff, the flap governor,
fast-path resumption, and the whole reconnect-backfill engine — exists to
survive.

### 1.2 Why it was deferred, and why the reason expired

The deferral's stated grounds were two
([`context-driven-fault-injection.md:138-142`](context-driven-fault-injection.md)):

> a privileged CRI-O daemon for one pod is disproportionate today

> Pod-to-pod *partition* is permanently irrelevant (one pod)

Both assumed the victim was the **server** pod. The victim in this spec is a
**machine** pod — the drill's own — so partition becomes the central case rather
than an irrelevant one, and the privileged daemon turns out to be avoidable
entirely (§2.1).

### 1.3 What the drill must answer

Four questions, each phrased the way a support desk would ask it:

1. When a machine goes dark and comes back, does it reconnect on its own?
2. Does the hole its absence left in the customer's charts get filled?
3. Does an alert raised while it was dark still arrive?
4. When a whole site catches up at once over a thin connection, does the site's
   live monitoring stay usable while it happens?

Nothing in the repository answers any of these today. Questions 1, 2 and 4 are
answerable by the drill this plan builds. **Question 3 is not, and cannot be
until a product defect is fixed** — no machine can raise an alert to the server
at all today (§13.2). It stays on this list because it is the right question;
what changes is that the drill cannot ask it in its first release.

---

## 2. Settled decisions and the evidence behind them

| # | Decision | Evidence |
|---|---|---|
| D1 | **Mechanism: an in-path link shaper**, not Chaos Mesh and not traffic-control rules | §2.1 |
| D2 | **Both victims in one run** — one real agent, one simulated fleet | §2.2 |
| D3 | **Report and trend**; alert on regression; never gates a deploy | §2.3 |
| D4 | **Four scenarios**, the outage recovered both wide and narrow | §2.4 |
| D5 | **Record the ADR-055 decision change** — form pending the §13.3 ruling; close the debt with two named residuals | §2.5, §13.3 |

### 2.1 D1 — the mechanism

`sch_netem` **does not exist on the worker node.** Read directly off the node's
root filesystem, which `monitoring-node-exporter` mounts read-only at
`/host/root`:

```
kernel                                5.15.0-320.202.8.2.el8uek.aarch64
CONFIG_NET_SCH_NETEM                  =m        (a module, not built in)
find /lib/modules/$K -name '*netem*'  → nothing
grep -c netem modules.dep             → 0
```

Every sibling scheduler is present — `sch_htb`, `sch_tbf`, `sch_prio`,
`sch_sfq`, `sch_cake`, `sch_codel`, `sch_fq`, `sch_hfsc`, `sch_hhf`, `sch_pie`,
`sch_plug`, `sch_taprio`, `sch_ingress`. `netem` alone is missing: it ships in
`kernel-uek-modules-extra`, which the OKE worker image does not install. It is
absent from `modules.dep`, so it cannot be auto-loaded, and no container can
supply it.

Also present: `xt_statistic.ko` (probabilistic drop by filter rule),
`sch_tbf.ko` / `sch_htb.ko` (bandwidth shaping), `ifb.ko`.

Chaos Mesh implements `delay`, `loss`, `duplicate` and `corrupt` as netem
actions through its `SetTcs()` path; only `partition` (filter rules) and
`bandwidth` (`tbf`) avoid netem. Against the four chosen scenarios:

| Scenario | Shaper | Filter rules in-pod | Chaos Mesh |
|---|---|---|---|
| Total outage | yes | yes | yes |
| Narrow-link recovery | yes | yes (`tbf`) | yes (`tbf`) |
| Heavy one-way loss | yes | yes (`xt_statistic`) | **no** — its loss is netem |
| Slow satellite-style link | yes | **no** — netem absent | **no** — netem absent |
| Machine returns on a new address | yes | **no** — no mechanism | **no** — no such action |

Chaos Mesh would deliver two of four while adding a privileged node agent
holding `SYS_ADMIN`, `SYS_PTRACE`, `SYS_CHROOT`, `MKNOD`, `KILL` and `IPC_LOCK`
plus the runtime socket, on the one node that carries production. The only route
to netem is adding `node_metadata` cloud-init to the node pool
([`main.tf:41-73`](../../../deploy/terraform/modules/oke/main.tf)) and recycling
that node — a permanent node-level dependency, created for a test, that any OKE
image change can silently remove again.

There is **no off-the-shelf alternative**: Toxiproxy is TCP-only (ADR-055
already found this), and `tc`, `comcast`, Pumba and Chaos Mesh are all netem
wrappers. The one in-path precedent is the QUIC working group's own interop
runner, which places a simulator container between client and server and forces
traffic through it — the model this spec follows, reduced from a full network
simulator to a datagram forwarder.

**Three facts make the shaper viable here, all verified:**

- The address plumbing already exists and runs nightly. Both
  [`e2e-machine-pod.sh`](../../../deploy/scripts/e2e-machine-pod.sh) and the load
  test redirect the certificate's hostname to a chosen pod IP with
  `hostAliases`, and the load-test harness takes the QUIC address
  (`-addr`) and the enrolment address (`-enroll-url`) as
  [separate flags](../../../server/tests/loadtest/main.go), so the two addresses can
  be aimed independently. That the QUIC name points at the shaper while enrolment
  takes the fully-qualified service name is a change this plan makes, not a
  property it inherits — both existing call sites pass the short name for both
  (§4.2). TLS is untouched either way: the name on the certificate is the name
  dialled, and no key ever reaches the shaper.
- Nothing on the **QUIC** path depends on the machine's address. It is read
  once, as a log field
  ([`server_connection.go:32`](../../../server/internal/agentapi/server_connection.go)) —
  the sole non-test use of `RemoteAddr()` in the whole `agentapi` package.
- The **HTTP** path is a different answer, and the earlier one here was wrong.
  [`api.go:477`](../../../server/internal/api/api.go) applies `RateLimiter(100, 200)`
  to the entire `/api` subrouter, keyed **per source IP** (`newIPLimiter` /
  `extractIP`); `AuthRateLimiter(10, 20)` is an additional auth-path limiter, not
  the only one. `/api/v1/enroll/{token}` is registered under that subrouter, so
  every enrolment the drill performs is rate-limited, and the whole fleet shares
  one pod IP. Twenty agents against a burst of 200 is comfortable, and the
  nightly load run already enrols a hundred from one pod — so this constrains
  nothing the drill does, but it is a ceiling to respect if the fleet ever grows,
  not an absence of one.
- A root container holding only `NET_ADMIN` is admitted in `opengate-staging`
  today (verified by a server-side dry run; the namespace carries no
  PodSecurity labels and no quota). The shaper needs **none** of that — it is
  recorded here only because it bounds what the rejected alternative would have
  required.

### 2.2 D2 — both victims

- **The real machine** is the shipped Rust agent, built from the commit under
  test and run as a pod exactly as staging end-to-end already does it. It is the
  only thing that exercises the real backoff schedule, the flap governor, the
  fast-path resumption and the real backfill engine.
- **The simulated fleet** is [`server/tests/loadtest`](../../../server/tests/loadtest)
  at **20 agents**. Twenty is not arbitrary: the server admits 4 concurrent
  drains per customer (§4.3), so twenty machines in one tenant produce a real
  queue — four draining, sixteen deferred — which is precisely the herd
  behaviour the scenario is about.

The fleet also gives the drill a **deterministic backlog**: the harness carries
`-backfill-batches` and `-backfill-samples`, so the volume it drains on recovery
is chosen rather than observed. The real agent's backlog is a genuine gap created
by the outage; the fleet's is synthesised. Assertions are split accordingly
(§5.4).

Those two flags drive **telemetry** backfill and nothing else. They were
previously cited here as also giving the drill a deterministic *alert* replay;
they do not, and no other part of the harness does either. The harness's control
vocabulary is fourteen message types and `MsgAgentAlert` is not among them, so
the fleet cannot raise an alert. Neither can the real agent (§13.2). Alert replay
is therefore out of the first release entirely, not merely moved between victims.

### 2.3 D3 — posture

Nightly, non-gating, mirroring [`mutation.yml`](../../../.github/workflows/mutation.yml):
push the numbers to VictoriaMetrics for a trend dashboard, upload the evidence,
and go red with a Telegram alert only when a run regresses against its own
window or breaks an absolute floor. A slow recovery on a shared two-processor
node is a trend to read, not a reason to stop shipping.

The drill therefore lands in **its own workflow**, not in
[`fault-tolerance.yml`](../../../.github/workflows/fault-tolerance.yml). That is
deliberate: [`fault-ci.test.sh:103-107`](../../../scripts/tests/fault-ci.test.sh)
asserts the gating workflow enumerates no network scenario, and that assertion
stays true and untouched. No existing guard is weakened to land this.

### 2.4 D4 — the scenario set

Four scenarios, the first carrying two recovery variants. Corruption and
reordering are deliberately excluded and the reason is recorded rather than
re-proved nightly: this protocol seals each packet, so a damaged one fails its
integrity check and is discarded — indistinguishable from one that never
arrived — and out-of-order delivery is something the protocol resolves by
design.

### 2.5 D5 — the record

[ADR-055](../../../docs/adr/ADR-055-fault-injection-mechanism.md) is amended in
place. The write guard permits this: ADRs are mutable, and only a link to a
**non-archived plan** is blocked
([`pretooluse-write-guard.sh:44-51`](../../../.claude/hooks/pretooluse-write-guard.sh)).

Noted once and then set aside: the project convention reserves in-place edits
for corrections and supersession for a change of mechanism, and this is a change
of mechanism. The instruction to amend in place stands and is what will be
implemented; the amendment will state plainly that the deployed-fault mechanism
changed and why, so the record does not read as though it always said this.

---

## 3. Scope

### 3.1 In scope

- A link shaper: one Go binary under `server/tests/`, plus its pod manifest.
- Four scenarios driven by a staging-only runner script.
- One nightly workflow, non-gating, with evidence upload, trend push and a
  regression check.
- A Grafana trend dashboard.
- ADR-055 amendment, `techdebt.md` closure, `decisions.md` and `phases.md` rows,
  and the [Fault Injection](../../../docs/infrastructure/Fault-Injection.md)
  chapter update.

### 3.2 Out of scope

- **Breaking the link on the server's own side.** The shaper sits between the
  machine and the server, so it can fail the machine's path but not the server's
  own. This is the single residual debt (§10.2).
- **Gating.** The drill never blocks a deploy.
- Corruption and reordering (§2.4).
- Any node-level change: no cloud-init, no module install, no node recycle.
- Any change to shipped agent or server behaviour. If a scenario exposes a
  defect, that defect is filed and fixed on its own plan — this workstream
  builds the instrument, not the repairs.
- Operational tooling beyond the drill itself, per
  [`editing-and-scope.md`](../../rules/editing-and-scope.md).

---

## 4. Domain model

### 4.1 Entities

| Entity | What it is | Where it lives |
|---|---|---|
| **Drill run** | One nightly execution: setup, four scenarios in order, teardown | `network-drill.yml` |
| **Scenario** | One named link failure with parameters and phases | `scripts/fault/network-drill.sh` |
| **Link shaper** | In-path datagram forwarder that impairs on command | `server/tests/netfault/` |
| **Victim** | A machine whose path runs through the shaper: `real` or `fleet` | agent pod / loadtest pod |
| **Phase** | `baseline`, `fault`, `recovery` — a scenario is exactly these three | runner |
| **Measurement** | One named number for one scenario and victim | runner → summary |
| **Evidence bundle** | Everything a human needs to read a run | workflow artifact |
| **Trend row** | Prometheus samples carrying `commit`, `env`, `scenario`, `victim` | VictoriaMetrics |

### 4.2 How they collaborate

```
network-drill.yml
  ├─ takes the staging lease                     (staging-lease.sh)
  ├─ builds: linkshaper, loadtest, mesh-agent    (arch from the node)
  ├─ starts   shaper pod                         ← control endpoint
  ├─ enrols + starts  real machine pod           ─┐ QUIC name → shaper IP
  ├─ enrols + starts  fleet pod (20 agents)      ─┘ enrolment → service FQDN
  ├─ for each scenario:
  │     baseline  → assert healthy, record the control reading
  │     fault     → command the shaper, hold, observe
  │     recovery  → clear or reshape, observe until budget or success
  │     └─ emit measurements, or emit NOTHING if the phase did not measure
  ├─ summarize → evidence bundle + canonical rows
  ├─ push rows to VictoriaMetrics                (lib/vm-push.sh)
  ├─ regression check against a 14-day window    (lib/vm-query.sh)
  ├─ tears down every pod under always()
  └─ releases the lease under always()
```

Both machines address the server by the **same certificate name**, and the
`hostAliases` entry points that name at the shaper instead of the server pod.
Enrolment must go to
`opengate-staging-server.opengate-staging.svc.cluster.local:8080` — the
fully-qualified service name, which an `/etc/hosts` entry for the short name does
not intercept, so it resolves through cluster DNS to the Service ClusterIP while
QUIC goes to the shaper. That keeps the shaper single-purpose and carrying UDP
only.

**This is a change, not the current state, and it is required in two places.**
Both existing precedents use the *short* name for both protocols, so redirecting
that one name sends enrolment into a UDP-only forwarder:

| Site | Today | Needed |
|---|---|---|
| [`e2e-machine-pod.sh:36,79`](../../../deploy/scripts/e2e-machine-pod.sh) | `SERVER_NAME="${RELEASE}-server"`, `OPENGATE_ENROLL_URL=http://${SERVER_NAME}:8080` | enrolment URL takes the FQDN; `OPENGATE_SERVER_ADDR` keeps the short name |
| [`load-test.yml:124,254`](../../../.github/workflows/load-test.yml) | `LOADTEST_BASE_URL=http://${RELEASE}-server:8080`, passed as `-enroll-url` | the drill's own invocation passes the FQDN |

The QUIC address stays the short name in both, because it is the name on the
certificate and a pod IP is in none — the load run's own comment makes that
point. Only the enrolment URL moves. `e2e-machine-pod.sh` is added to §8.2 for
this reason; the load-test workflow is not edited, since the drill writes its own
invocation.

### 4.3 The system under observation

Numbers the assertions are calibrated against, all read from the code:

| Quantity | Value | Source |
|---|---|---|
| QUIC idle timeout | 90 s | [`server.go:202`](../../../server/internal/agentapi/server.go), matched by the agent |
| Keepalive period | 30 s | same |
| Reconnect backoff | full jitter, base 1 s, cap 30 s | [`connection.rs:164-171`](../../../agent/crates/mesh-agent-core/src/connection.rs) |
| Flap stability window | 5 s | [`connection.rs`](../../../agent/crates/mesh-agent-core/src/connection.rs) `ReconnectGovernor` |
| Concurrent drains, global | 8 | [`backfill_scheduler.go`](../../../server/internal/agentapi/backfill_scheduler.go) |
| Concurrent drains, per customer | 4 | same |
| Granted drain rate | **2 500 samples/s, fixed** | see below |
| Drain pacing | stop-and-wait; sleep `samples ÷ rate` after each ack | [`pace_delay`](../../../agent/crates/mesh-agent-core/src/ml/backfill/mod.rs) |
| Batch size | 1 000 samples ≈ 45 KB | `max_batch_samples`, 18 dimensions ≈ 45 B/sample |
| Telemetry payload ceiling | 64 KiB | `maxTelemetryPayloadBytes` |

The drain rate is fixed because the scheduler is constructed as
`NewBackfillScheduler(DefaultBackfillSchedulerConfig(), nil, nil)`
([`server.go:117`](../../../server/internal/agentapi/server.go)) and `nil` for the
load signal means full headroom permanently — which the code says outright. So
each grant is `20 000 ÷ 8 = 2 500` samples/s, about **0.9 Mbit/s per catching-up
machine and up to 3.6 Mbit/s per customer**.

Two effects oppose each other on a narrow link, and which one wins is the open
question S2 exists to settle:

- **Pushing toward harm:** four concurrent drains at 0.9 Mbit/s against a
  2 Mbit/s uplink; and heartbeats, live readings and catch-up batches all travel
  the **one control stream** ([single `send_control` path](../../../agent/crates/mesh-agent/src/main.rs)),
  so a 45 KB batch in flight sits ahead of the next heartbeat on an ordered
  stream.
- **Pushing toward safety:** the drain is stop-and-wait, so lengthening
  round-trips throttle each machine automatically; and the scheduler defers with
  aging, so nobody starves.

The scheduler's own comment reasons that backfill "always yields to live
telemetry and control… so admitting a drain cannot delay them." That is true of
**server-side scheduling** and does not carry over to a **shared uplink**. That
gap is the finding S2 is built to measure.

**Which layer is delayed, and which is not.** The blocking above is real but it
is confined to the *application* stream. `AgentConnection` holds one `stream` and
every `send_control` writes to it, so a 45 KB batch does sit ahead of the next
application heartbeat. Connection *liveness* is not on that stream: the 30 s
keepalive is a QUIC PING frame, below the ordered stream, and it is not blocked
by a batch in flight. A machine therefore does not go offline because it is busy
draining — only because packets stopped arriving for 90 s. This is why S2's
staleness measurement is the assertion that bites and its offline assertion is
nearly free (§5.2).

---

## 5. The scenarios

Every scenario runs against both victims simultaneously and follows the same
three phases. Budgets below are **initial and deliberately generous**, in the
manner the existing drills already use, and are tightened toward observed
behaviour as the window fills.

### 5.1 S1 — the site goes dark, and comes back on a healthy link

> Northwind Dental's reception PC loses its connection for three minutes when
> the building's router is power-cycled. The technician wants to know it came
> back by itself, and that the gap in the machine's charts filled in.

| Phase | Duration | Shaper |
|---|---|---|
| baseline | 60 s | pass-through |
| fault | 180 s | drop everything, both directions |
| recovery | 180 s | pass-through |

180 s of darkness exceeds the 90 s idle timeout, so the connection genuinely
dies rather than merely stalling — the distinction matters, because a stalled
connection never exercises reconnect at all.

**Asserted:** the machine reconnects without intervention; it is back online
within **120 s** of restore; the gap in `opengate_edge_metric_avg` is filled to
at least **95 %** of expected points within the recovery window; no telemetry is
recorded as dropped for a reason other than duplicate.

### 5.2 S2 — the same outage, recovered over a thin uplink

> Riverside Clinic's twenty machines share a 2 Mbit/s upload. The site was dark
> for the whole morning. Everything now wants to catch up at once. Does the
> clinic's live monitoring stay usable while it does?

| Phase | Duration | Shaper |
|---|---|---|
| fault | 180 s | drop everything |
| recovery | 240 s | pass-through, shaped to **2 Mbit/s** toward the server, shared by all victims |

The shaping is one token bucket across every machine, which is what a shared
site uplink actually is.

**Asserted:** worst-case staleness of live readings stays under **90 s**; the
catch-up either completes or advances monotonically within the window; no machine
goes offline during the catch-up. All three are recorded as trend numbers whether
or not they pass, because the trend is the point.

The order is deliberate. **The staleness number is the one that bites** — it is
the only one that measures the head-of-line blocking §4.3 describes. The offline
assertion is nearly free, and the plan should not pretend otherwise: there is no
staleness-based offline threshold anywhere in the server. A device is marked
offline at exactly one place, the connection-teardown path
([`server_connection.go:156`](../../../server/internal/agentapi/server_connection.go));
the four background sweeps reclaim telemetry orphans, incidents, retention and
stale relay sessions, and none of them looks at `last_seen`. So "offline" means
"the QUIC connection died", which at 2 Mbit/s takes losing keepalives for 90 s
straight. It is worth asserting — that is the failure a saturated uplink would
actually produce — but it will pass on most nights without proving anything, and
a green S2 is not evidence that live monitoring stayed usable. The staleness
number is that evidence.

### 5.3 S3 — the connection is up but bad

> A machine on saturated rural broadband keeps its connection but loses a fifth
> of what it sends.

| Phase | Duration | Shaper |
|---|---|---|
| baseline | 30 s | pass-through |
| fault | 180 s | drop **20 %** of datagrams toward the server; clean toward the machine |
| recovery | 90 s | pass-through |

Asymmetry is deliberate: a customer's upload is the direction that degrades, and
a symmetric fault would hide which side the recovery machinery is coping with.

**Asserted:** the machine stays online throughout — no offline transition, no
flap; telemetry continues to arrive, at reduced rate; the connection is not
re-established more than once.

### 5.4 S4 — a slow link, and a machine that returns on a new address

Two impairments in one window, because both are cheap and neither needs a fresh
outage.

| Phase | Duration | Shaper |
|---|---|---|
| baseline | 30 s | pass-through |
| fault A | 120 s | 300 ms each way |
| fault B | 60 s | re-address: the shaper moves to a new server-facing port mid-connection |
| recovery | 90 s | pass-through |

> A survey office works over satellite: a third of a second each way, all day.
> And a customer's router reboots at 3 a.m. every night, handing every machine a
> new public address — does that show up as a nightly gap in every chart?

**Asserted for A:** the machine stays online at 300 ms; the keepalive continues
to hold the connection open. **Asserted for B:** the session survives the change
of address — the server keeps it rather than forcing a reconnect.

**How B actually works, because the earlier account here was backwards.** The
claim was that the server keeps a session across a client address change *when
the connection identifier is unchanged*. quic-go v0.62.0 does the opposite, and
the distinction decides whether the scenario can pass at all:

- The transport demultiplexes by **connection ID**, not by 4-tuple
  (`Transport.handlePacket` → `packetHandlerMap.Get(connID)`), so a packet from
  the shaper's new port still reaches the right connection. Good.
- `Conn.handleShortHeaderPacket` then sees `p.remoteAddr != c.RemoteAddr()` and
  hands the packet to the **path manager**, which probes the new path with a
  `PATH_CHALLENGE` — and to do that it calls `connIDManager.GetConnIDForPath`,
  which pops a **spare, previously-unused connection ID** off the queue the peer
  issued. With none available it logs *"skipping validation of new path… since no
  connection ID is available"* and returns, and the path is never validated.
  Migration therefore requires a **new** CID, not an unchanged one. The unchanged
  case is the zero-length-CID shortcut, and quinn's default `cid_len` is 8, so it
  does not apply here.
- The switch then needs the path validated **and** a non-probing packet that is
  the highest-numbered one received (`shouldSwitch && pn == largestRcvdAppData`)
  before `ChangeRemoteAddr` is called.

It should hold: quic-go advertises `MaxActiveConnectionIDs = 4`, quinn issues
spares up to that limit and answers `PATH_CHALLENGE`. But it is a three-condition
path, not a property of the identifier staying put, and **its failure mode is
silent** — no switch means the server keeps writing to a port the shaper has
closed, and the connection dies at the idle timeout looking exactly like an
ordinary outage. So the drill must distinguish the two: B asserts session
survival **and** records `netdrill_session_survived` alongside whether a
reconnect occurred, so a failure reads as "migration did not happen" rather than
"the link broke". If it fails, the finding is that every customer with a
rebooting router carries a nightly gap.

**Alert replay is not in this release.** It was specified here as riding on S1's
recovery, asserted on the fleet. Nothing can produce the alert: the harness has
no `MsgAgentAlert` in its vocabulary, and the shipped agent never sends one
either (§13.2). The assertion, its series and its acceptance criterion are
withdrawn rather than left to fail on night one.

---

## 6. The link shaper

### 6.1 What it is

A single Go binary, `server/tests/netfault/`, built for the node's architecture
and copied into a pod exactly as the load-test harness already is. It listens on
UDP 9090, forwards each datagram to the real server, forwards replies back, and
applies whatever impairment it has been told to apply.

It holds one server-facing socket per machine-facing address, so the server sees
a distinct source port per machine and return traffic routes correctly — the
same shape any network address translator has.

Control is a small HTTP endpoint on a second port, reachable only inside the
cluster, which accepts a scenario command and reports counters.

### 6.2 Impairments

| Command | Effect |
|---|---|
| `pass` | forward everything |
| `blackhole` | drop everything, both directions |
| `loss(p, direction)` | drop a seeded fraction, one direction |
| `delay(d)` | hold each datagram for `d` before forwarding |
| `rate(bps)` | one token bucket shared by every machine |
| `rebind` | move every server-facing socket to a new local port |

Every impairment draws from a **seeded** generator, and the seed is recorded in
the evidence. Two nights with the same seed drop the same datagrams, which is
what makes a trend comparison mean anything — and is something the kernel's own
emulator could not have given us.

### 6.3 Non-negotiable implementation details

These are failure modes that would corrupt results silently, so they are
requirements, not advice:

1. **A 64 KiB read buffer, and a truncation assertion.** Go's `ReadFromUDP`
   silently truncates an oversized datagram. A truncated path-probing packet
   would look like corruption the drill never asked for. A read returning
   exactly the buffer size is a fatal error, not a warning.
2. **Address mappings outlive the outage.** Idle expiry is **600 s**, comfortably
   past the longest dark window. A mapping that expired mid-blackhole would turn
   S1 into a different experiment without saying so.
3. **The shaper accounts for itself.** It counts datagrams in, out and dropped,
   per direction, and the drill reads those counters at every phase boundary. A
   scenario whose drop count does not match its instruction did not run.
4. **A dead shaper is inconclusive, never a failure.** If the shaper stops
   answering, the scenario is abandoned and **emits no measurement at all**.
   This is the rule [`loadtest-quic-run.sh`](../../../scripts/loadtest-quic-run.sh)
   already writes down for its own half: a harness that measured nothing
   produces rows of zeroes, zeroes pull the window median down, and one bad
   night quietly costs two.
5. **No fault behaviour in any shipped path.** Enforced structurally, not by
   convention: a `go list -deps` test in the pattern of
   [`noship_test.go`](../../../server/internal/faulttest/noship_test.go) asserts
   the server binary never depends on the shaper package.

### 6.4 Fidelity: what the shaper changes

Stated plainly rather than buried, because an instrument's distortions belong in
its documentation:

- **One extra hop**, roughly a fifth of a millisecond inside the cluster.
  Irrelevant beside a 300 ms scenario and a 90 s timeout.
- **The network-layer congestion hint is not carried through.** The shaper
  forwards payloads, not the marking some paths apply to signal congestion
  early. Most real customer paths do not carry it either, so the drill's path
  resembles a customer's more than the unshaped in-cluster path does — but it is
  a difference, and it is why the shaper is an instrument for recovery
  behaviour, not for congestion-control research.
- **The server records the shaper's address**, not the machine's. Verified
  harmless: that value is used once, as a log field.

---

## 7. Non-functional requirements

### 7.1 Performance and node budget

The worker has 1830m of allocatable processor and 9.2 GiB of allocatable memory,
and carries production. Allocatable is not the number that matters: **1480m of
CPU requests — 80% — is already committed** at steady state, leaving about 350m,
and limits already sit at 347% overcommit. Memory is the roomy side, at 21%
requested.

The drill's pods request **less than 200m and 320 MiB in total**, matching what
the load test and the staging machine pods already ask for, so they fit inside
that 350m with roughly half of it to spare. Their *limits*, inherited from the
same precedents, total nearer 800m — well under the node ceiling, but adding to
an already-overcommitted node.

The consequence is on the measurements, not on scheduling: reconnect and
staleness readings taken on a two-processor node at this level of overcommit will
be noisy, and the trend, not any single night, is what carries the finding. This
is the same reasoning §2.3 gives for the drill never gating a deploy.

Total runtime is about 25 minutes of scenarios plus setup and teardown; the job
timeout is 60 minutes, which also absorbs a wait on the namespace lease.

Scheduled at **06:00 UTC** — after the load test at 05:00, which holds the same
lease, and clear of the 03:00 and 04:00 batches that occupy the account's job
pool.

### 7.2 Security

- **No privilege of any kind.** No node agent, no runtime socket, no added
  capability, no root. The shaper is an ordinary unprivileged pod.
- **No key leaves the cluster.** The real machine enrols through the public
  enrolment endpoint exactly as the load test does, keeping its own private key;
  no certificate authority key is ever copied out.
- **Staging only.** Every runner refuses any namespace but `opengate-staging`,
  in the manner of the existing runners.
- **The credential is short-lived.** The enrolment token is minted per run and
  expires within the hour.
- **The shaper is unreachable from outside.** No Ingress route, no Service
  publishing it; the control endpoint is cluster-internal.

### 7.3 Long-term maintainability

- **No dependency on anything outside the repository.** No node module, no
  cluster component, no third-party controller. A node image change cannot break
  it, which is precisely the failure the rejected alternatives were exposed to.
- **The shaper is unit-testable in process.** Impairments are pure functions
  over datagrams with an injected generator and clock, so loss fractions, token
  buckets and delays are asserted by ordinary Go tests with no cluster and no
  sleeping. This satisfies the test-determinism rule directly: no test skips
  itself for want of a cluster.
- **It runs identically under Docker locally.** The same binary, the same
  commands.
- **New surface is registered where the gates expect it.** §8 lists each
  registration; none is optional.

---

## 8. Where the change fits

### 8.1 New files

| Path | What |
|---|---|
| `server/tests/netfault/` | the shaper: forwarder, impairments, control endpoint, `main.go` |
| `server/tests/netfault/*_test.go` | in-process tests for every impairment |
| `server/tests/netfault/noship_test.go` | `go list -deps` assertion |
| `deploy/scripts/netfault-shaper-pod.sh` | shaper pod manifest, in the style of `e2e-machine-pod.sh` |
| `scripts/fault/network-drill.sh` | staging-only scenario runner |
| `scripts/network-drill-summarize.sh` | measurements → canonical rows |
| `scripts/network-drill-vm-push.sh` | canonical rows → Prometheus text → VictoriaMetrics |
| `scripts/network-drill-regression-check.sh` | window comparison and floors |
| `scripts/tests/network-drill.test.sh` | offline policy and behaviour tests |
| `.github/workflows/network-drill.yml` | the nightly |
| `deploy/grafana/provisioning/dashboards/network-drill-trend.json` | the trend |

### 8.2 Existing files touched

| Path | Change |
|---|---|
| [`scripts/lib/mutation-shards.sh`](../../../scripts/lib/mutation-shards.sh) | add `dir:tests/netfault` to `go-observability-harness`; add `tests/netfault/main\.go` to the global excludes beside `tests/loadtest/main\.go` |
| [`deploy/scripts/e2e-machine-pod.sh`](../../../deploy/scripts/e2e-machine-pod.sh) | `OPENGATE_ENROLL_URL` takes the fully-qualified service name so redirecting the short name at the shaper does not send enrolment into a UDP-only forwarder (§4.2). `OPENGATE_SERVER_ADDR` keeps the short name — it is the certificate's. The staging browser suite keeps working unchanged: the FQDN resolves to the same server through the Service |
| [`.claude/shell-policy.exceptions`](../../shell-policy.exceptions) | only if a new script needs a non-default execution class |
| [`docs/infrastructure/Fault-Injection.md`](../../../docs/infrastructure/Fault-Injection.md) | **delete** the Chaos Mesh surfaces and guardrail sections; describe the shaper and the four scenarios with their budgets. See the note below — this is not the swap it was written as |
| [`docs/Home.md`](../../../docs/Home.md) | the Fault Injection row currently says "on-demand Chaos Mesh drills" |
| [`docs/adr/ADR-055-…`](../../../docs/adr/ADR-055-fault-injection-mechanism.md) | amend the deployed-fault mechanism in place |
| [`.claude/techdebt.md`](../../techdebt.md) | close the entry; open the one residual |
| [`.claude/decisions.md`](../../decisions.md) | ADR-055 row updated |
| [`.claude/phases.md`](../../phases.md) | Completed row linking `plans/archive/nightly-quic-network-drills.md` |

`fault-tolerance.yml` and `fault-ci.test.sh` are **not** touched. The gating path
keeps its "no network scenario" assertion, and it stays true.

**Chaos Mesh was never built, and the doc edit is a deletion.** This row was
written as replacing one live mechanism with another. It is not: `chaos-mesh`
appears nowhere in the repository except
[`fault-ci.test.sh`](../../../scripts/tests/fault-ci.test.sh), where it is asserted
*absent*. Nothing installs it, `scripts/fault/` holds only `pod-delete.sh`,
`bad-rollout.sh` and the two ingress scripts, and `ingress-504` is an nginx
proxy-timeout annotation patch rather than a `NetworkChaos`. So
[`Fault-Injection.md`](../../../docs/infrastructure/Fault-Injection.md) describes an
uninstalled mechanism in the present tense — "deployed faults run in Chaos Mesh",
guardrails that "are implemented by the scenario runner", and a claim that
"agent-side reconnect is proven by the Chaos Mesh drills" — which is a
live-state violation standing today, independent of this workstream.

Two consequences for the edit. First, it is larger than one section: the mechanism
paragraph, the surfaces table, the guardrails section and the scenario-catalog
rows all name Chaos Mesh. Second, **the shaper does not replace all of it.**
StressChaos CPU/memory pressure and server↔Postgres / server↔relay latency are
explicitly out of this plan's scope (§3.2), so those rows are deleted as
never-built rather than reassigned to the shaper — writing the shaper into them
would trade one false doc for another. The `Home.md` row loses "on-demand Chaos
Mesh drills" for the same reason.

### 8.3 Dependencies

Only what the repository already has: the load-test harness, the machine pod
script, [`scripts/staging-lease.sh`](../../../scripts/staging-lease.sh),
`lib/vm-push.sh`, `lib/vm-query.sh`, the `oci-kube-setup` action, and the
`observability` environment that scopes the Telegram alert secrets.

Two constraints on how the workflow is written, both learned from the nightly
that already does this:

- **The drill job must not declare `environment: staging`.** Its required
  reviewers would leave a scheduled run waiting for a human that never comes —
  [`load-test.yml:62`](../../../.github/workflows/load-test.yml) avoids it
  deliberately and says so, reaching the cluster with repository-level OCI
  credentials instead. The drill follows that shape. Only the trend-publishing
  job takes `environment: observability`, as the load run's does. This is also
  why the drill cannot live in
  [`fault-tolerance.yml`](../../../.github/workflows/fault-tolerance.yml), which is
  `environment: staging` and is dispatched by hand — a second reason beside the
  one §2.3 already gives.
- **The agent binary is fetched, not built.** §4.2's sketch says the run builds
  `mesh-agent`; staging end-to-end does not, and copying it is the wrong lesson.
  [`cd.yml:296`](../../../.github/workflows/cd.yml) downloads
  `mesh-agent-${AGENT_TARGET}` as an artifact from
  [`build-image.yml`](../../../.github/workflows/build-image.yml) and `kubectl cp`s
  it into an alpine pod. A standalone nightly has no upstream run of its own to
  download from, so it resolves the most recent successful `build-image` run on
  `main` and takes the `aarch64` artifact from there, recording the run id and
  commit in the evidence bundle. An aarch64 Rust release build inside the job
  would not fit the 60-minute budget beside 24 minutes of scenarios, and the
  commit under test is the one that produced the image staging is running
  anyway. If no such artifact is available the run is **inconclusive** and emits
  no rows, by the same rule §6.3 applies to a dead shaper.

---

## 9. Measurements, trend and the gate

### 9.1 Series

Every sample carries `commit` and `env="ci"` (required by `vm_push`), plus
`scenario` and `victim`.

| Series | Meaning |
|---|---|
| `netdrill_reconnect_seconds` | restore → machine online again |
| `netdrill_backfill_complete_seconds` | restore → gap filled |
| `netdrill_gap_fill_ratio` | share of expected points present after recovery |
| `netdrill_offline_transitions` | times the machine crossed the offline line |
| `netdrill_live_staleness_max_seconds` | worst staleness of live readings during recovery |
| `netdrill_session_survived` | 1 if the session survived re-addressing |
| `netdrill_reconnected_after_rebind` | 1 if the machine reconnected instead — separates a failed migration from a broken link (§5.4) |
| `netdrill_shaper_dropped_to_server_total` / `netdrill_shaper_dropped_to_machine_total` | the shaper's own count, per direction — attributed to the link rather than to either machine on it |

`netdrill_alerts_replayed` is withdrawn with the assertion it served (§5.4). It
returns in the same change that gives the agent an alert delivery path (§13.2).

### 9.2 The regression check

The pattern in
[`loadtest-regression-check.sh`](../../../scripts/loadtest-regression-check.sh):
read a 14-day window back out of VictoriaMetrics, require at least three
samples, and compare against frozen relative bands plus absolute floors.

Bands are calibrated from the first two weeks of runs and written down beside
the two readings that bracket them, not guessed. Until that window exists the
check enforces the **absolute floors only** — reconnect within 120 s, gap fill
at or above 95 %, zero unexplained offline transitions in S3 — and says so in
its output rather than silently passing.

A scenario that measured nothing contributes **no row**, so an aborted night
cannot flatter the next one.

---

## 10. Definition of done

### 10.1 Acceptance criteria

**Core behaviour**

1. The shaper forwards a QUIC session unmodified, and a real agent enrols,
   registers and reports through it exactly as it does without it.
2. Each of the six impairments is asserted by an in-process Go test with an
   injected generator and clock — no cluster, no sleeping, no skipping.
3. Two runs with the same seed drop the same datagrams.
4. An oversized datagram is a fatal error, never a silent truncation.
5. `go list -deps` proves the server binary does not depend on the shaper.

**The drill**

6. All four scenarios run against both victims in one nightly, inside 60 minutes.
7. Every scenario emits its measurements, or emits nothing and says why.
8. A dead or unreachable shaper yields an inconclusive scenario, never a
   measurement and never a false pass.
9. The runner refuses any namespace but `opengate-staging`.
10. Every pod is removed under `always()`, and the run asserts none remains.
11. The staging lease is taken before and released after, under `always()`.
11a. Both victims enrol successfully with the QUIC name pointed at the shaper —
    the check that the §4.2 enrolment split actually holds, and the one that
    fails loudly if either invocation site was missed.
11b. The drill job declares no `environment: staging`, and a policy assertion in
    `network-drill.test.sh` keeps it that way.
11c. A run that cannot resolve an `aarch64` `mesh-agent` artifact is inconclusive
    and emits no rows.

**Reporting**

12. Rows reach VictoriaMetrics and render on the trend dashboard.
13. The evidence bundle carries the seed, the shaper's counters, pod events, and
    per-phase readings for every scenario.
14. A regression alerts by Telegram and turns the workflow red; a normal night
    is green; **no outcome ever blocks a deploy**.

**The record**

15. The ADR-055 decision change is recorded — in place or by supersession, per
    the ruling §13.3 asks for.
16. `techdebt.md` entry closed; the residuals opened with their reopening
    conditions — the server-side link (§10.2) and the withdrawn alert replay
    (§13.2).
17. `Fault-Injection.md` and the `Home.md` row describe the shaper, in live-state
    terms only, with every never-built Chaos Mesh claim deleted rather than
    reassigned (§8.2).
18. `decisions.md` and `phases.md` rows added; this plan archived in the same
    commit that lands the final implementation.
19. The full gauntlet passes.

### 10.2 The residual debt

**Breaking the link on the server's own side.** The shaper sits between machine
and server, so it fails the machine's path only. Reopen if a failure is ever
observed that is specific to the server's own network interface and is not
already covered by the pod-deletion and gateway drills. Doing so would require
either a privileged node agent or a second cluster, both of which the free-tier
storage cap and the shared production node currently rule out.

---

## 11. Edge and error cases

### 11.1 Drill mechanics

| Case | Handling |
|---|---|
| Staging deploy runs concurrently | the namespace lease, taken before anything is touched |
| Shaper pod evicted or killed | scenario inconclusive; no row; run reports it |
| Real machine pod dies | `restartPolicy: Never`, so it is a visible failed pod; that victim's rows are omitted, the fleet's are kept |
| Node under pressure | small requests, capped limits, scenarios run one at a time |
| Enrolment token expires | minted immediately before use; run is well inside the hour |
| A machine was deleted server-side while dark | reconnect is refused by identifier; recorded as evidence, not asserted — it is the lifecycle path's own concern |
| Mapping expires mid-outage | 600 s idle expiry, past every dark window |
| A batch exceeds the payload ceiling | the run asserts no `payload_too_large` drop appears; 1 000 samples ≈ 45 KB against a 64 KiB ceiling leaves little margin, and this assertion is what would notice it shrinking |
| Clock drift | gaps are minutes; the server's clamp window is far wider |
| First runs have no window | absolute floors only, stated in the output |

### 11.2 What a technician would see

| Situation | What the drill asks |
|---|---|
| Northwind Dental's reception PC drops for three minutes | Does it return unaided, and does the hole in its charts fill? |
| Riverside Clinic's twenty machines catch up over a 2 Mbit/s uplink | Does live monitoring stay usable for the site while it happens, or does the site go quiet exactly when someone is watching it? |
| A machine on saturated rural broadband loses a fifth of its uploads | Does it hold its connection, or churn? |
| A survey office on satellite, a third of a second each way | Does the connection stay open at all? |
| A customer's router reboots nightly with a new address | Does that show up as a nightly gap in every chart on that site? |
| A machine raises an alert while it is dark | Does the alert arrive when it returns — once, not twice? |

---

## 12. Implementation steps

Test-first throughout; each step's tests precede its source.

**Phase A — the shaper**

1. `netfault` package tests: forwarding, per-machine mapping, mapping expiry,
   truncation refusal.
2. The forwarder.
3. Impairment tests: blackhole, seeded loss by direction, delay, token bucket,
   re-addressing.
4. The impairments and the control endpoint.
5. `noship_test.go`.
6. Register `dir:tests/netfault` in the mutation shard library and exclude its
   `main.go`; confirm the partition check passes.

**Phase B — the pod and the runner**

7. `scripts/tests/network-drill.test.sh` with stubbed `kubectl`, in the pattern
   of [`fault-k8s.test.sh`](../../../scripts/tests/fault-k8s.test.sh): namespace
   refusal, phase ordering, inconclusive handling, cleanup on every path,
   idempotent teardown. Executable bit set.
8. `deploy/scripts/netfault-shaper-pod.sh`.
9. `scripts/fault/network-drill.sh`.

**Phase C — reporting**

10. Tests for summarize, push and regression check, including the
    measured-nothing path.
11. The three scripts.

**Phase D — the nightly**

12. Policy assertions for the new workflow, extending the same test file —
    including that it declares no `environment: staging` (§8.3) and that the
    enrolment URL it passes is fully qualified (§4.2).
13. `deploy/scripts/e2e-machine-pod.sh`: enrolment URL to the FQDN, QUIC address
    unchanged. `scripts/tests/e2e-stack-machines.test.sh` covers this script
    already, so its assertion moves with it — and the staging browser suite is
    re-run to prove the change is invisible to it.
14. `.github/workflows/network-drill.yml`, resolving the agent artifact rather
    than building it.
15. The Grafana dashboard.

**Phase E — the record**

16. The ADR-055 decision change, in the form §13.3 settles; `Fault-Injection.md`;
    the `Home.md` row.
17. `techdebt.md` closure and both residuals; `decisions.md`; `phases.md`.
18. Archive this plan, bumping its links one level deeper, in the same commit as
    the final implementation.

**Phase F — proving**

19. Run the workflow by hand against staging; confirm all four scenarios
    measure, evidence uploads, rows land, and the dashboard renders.
20. Deliberately break the shaper mid-run and confirm the result is inconclusive
    with no row — the false-green case, tested rather than assumed.
21. Prove S4-B's failure mode is legible: run the rebind against a connection
    with no spare connection IDs available and confirm the run records a failed
    migration rather than a broken link (§5.4).
22. Full gauntlet.

---

## 13. Verification record

Every claim in this document was re-checked against the repository at `dev`, the
live OKE cluster, the live OCI tenancy, and the vendored sources of both QUIC
implementations. This section records what the check found. It is part of the
spec because a reviewer's first question about a number is where it came from.

### 13.1 What held

The arithmetic and the citations are sound, and the ones the assertions lean on
hardest were checked to the line:

- **The drain rate is genuinely fixed at 2 500 samples/s.**
  `DefaultBackfillSchedulerConfig` is `MaxConcurrent: 8`, `PerTenantMax: 4`,
  `BaseBudgetSamplesPerSec: 20_000`, bounds 500–5 000; `NewBackfillScheduler`
  substitutes `func() float64 { return 1.0 }` for a nil headroom signal, and
  `rate()` computes `20000 × 1.0 / 8 = 2500`, inside the bounds. The 0.9 Mbit/s
  and 3.6 Mbit/s figures follow.
- **Eighteen dimensions is exact**, not rounded: thirteen `series_dim_name`
  entries and five `series_max_dim_name` entries in `store_sink.rs`.
- **`server_connection.go:32` is the only non-test use of `RemoteAddr()`** in the
  whole `agentapi` package. The shaper's address really does reach one log field
  and nothing else.
- **`payload_too_large` is a real drop reason on the backfill path**, and the
  ceiling really is `64 * 1024`.
- **`alert_duplicate` is a real telemetry-drop reason.** An earlier reading of
  this plan called §5.1's "dropped for a reason other than duplicate" carve-out
  spurious; that was wrong. Ten alert drop reasons are reported through the same
  `dropTelemetry` counter as the fifteen literal ones, `alert_duplicate` among
  them, so the carve-out is correct and stays.
- **The single-control-stream argument is structurally right.**
  `AgentConnection` holds one `stream`, and every `send_control` writes to it.
  What §4.3 now adds is which layer that does and does not delay.
- **Live cluster matches every environment claim**: kernel
  `5.15.0-320.202.8.2.el8uek.aarch64`, `cpu-alloc: 1830m`, one
  `VM.Standard.A1.Flex` node of 2 OCPU carrying production, and
  `opengate-staging` with no PodSecurity labels and no quota.
- **Live OCI confirms the netem cost.** The node pool reports
  `node-metadata: None`, so the only route to `netem` really would mean
  introducing that block and recycling the single worker.
- **The precedents are as described**: `-addr` and `-enroll-url` are separate
  flags; `WINDOW_DAYS=14` and `MIN_WINDOW_SAMPLES=3`; `vm-push.sh` mandates
  `commit` and `env="ci"`; the `go-observability-harness` shard and the
  `tests/loadtest/main.go` exclusion are where §8.2 says; `loadtest-quic-run.sh`
  states the measured-nothing rule this plan borrows; the load run holds the same
  namespace lease, at 05:00, clear of the 03:00 and 04:00 batches.
- **The scenario budget adds up**: 420 + 420 + 300 + 300 s = 24 minutes.

**The `netem` finding was re-run before Phase A, and it holds.** §2.1 rests the
whole of D1 on it, so it was read again off the node's root filesystem through
`monitoring-node-exporter`'s `/host/root` mount:

```
kernel                                5.15.0-320.202.8.2.el8uek.aarch64
CONFIG_NET_SCH_NETEM                  =m
find /lib/modules/$K -name '*netem*'  → nothing
grep -c netem modules.dep             → 0
sch_htb sch_tbf sch_prio sch_sfq sch_cake sch_codel sch_fq sch_hfsc
sch_ingress xt_statistic ifb          → all present as .ko.xz
```

The drill re-reads it at setup and writes the output into the evidence bundle, so
a node image that starts shipping `netem` is visible in the record rather than
only in this document.

### 13.2 The defect this review surfaced

**No machine can raise an alert to the server. The agent has no alert delivery
path at all.**

The agent's alert *production* side is complete and wired into `main.rs`: an
`AlertSink` with a bounded queue and a rolling hourly ceiling, an event watch, a
rule evaluator, and a retroactive scanner all clone the sink and write to it. The
server's *ingestion* side is equally complete — `conn_alerts.go` carries ten drop
reasons, duplicate suppression and reconnect-replay handling.

Nothing connects them. `AlertSink::drain()` has **zero production call sites**:
its only caller in `mesh-agent` is inside `#[cfg(test)] mod tests`.
`ControlMessage::AgentAlert` is constructed **only in golden tests**. `EdgeAlert`
has no consumer outside the alerts module. So every alert every machine raises
goes into a 256-entry ring buffer, ages out under the oldest-first eviction the
sink documents, and is discarded — and the server's alert machinery has never had
a producer.

For this plan, that settles question 3 of §1.3: an alert raised while a machine
is dark does not arrive, but not for any reason a network drill would find. The
assertion, the `netdrill_alerts_replayed` series and their acceptance criterion
are withdrawn (§5.4, §9.1).

**This is not this workstream's to fix.** Per §3.2, a defect a scenario exposes
is filed and fixed on its own plan; this one was exposed by reading rather than
running, which changes nothing about where it belongs. It is filed in
`techdebt.md` as its own entry, at a severity that reflects a silently
non-functional alerting pipeline rather than a gap in a test. The alert-replay
assertion returns to this drill in the same change that gives the sink a drain.

### 13.3 The two rulings needed before Phase A

**1. ADR-055: amend in place, or supersede?** §2.5 chose in-place and noted the
tension. The verification makes the tension sharper rather than softer:
[`plans-and-adrs.md`](../../rules/plans-and-adrs.md) reserves in-place edits for
keeping an ADR *true* and requires a new ADR with `supersedes:` for "a reversal
or replacement, not a correction". ADR-055's Decision section names Chaos Mesh as
the deployed-fault mechanism and carries "Accepted risk: a privileged
chaos-daemon on the shared production worker" — both of which this plan reverses.
The write guard does permit the in-place edit (it blocks only non-archived plan
links, confirmed at `pretooluse-write-guard.sh:44-51`), so the hook will not stop
it; the rule is what says otherwise, and §2.5 overrides it citing an instruction
recorded nowhere in the repository.

There is a third reading worth putting on the table, because §8.2 changes the
facts: ADR-055's deployed-fault mechanism was **never implemented**. An ADR
recording a decision that was never carried out, being replaced by one that will
be, is closer to a genuine supersession than either of the first two options
assumed. Recommendation: **supersede**, with the new ADR stating plainly that the
Chaos Mesh mechanism was never built and why the shaper replaces it. If the
in-place instruction is reaffirmed, it is followed — but the reaffirmation is
recorded here rather than left implicit.

**Ruled: amend in place.** The in-place instruction of §2.5 was put to the owner
alongside the supersession recommendation above and reaffirmed. ADR-055 is
edited so its Decision section names the shaper as the deployed-fault mechanism
and states that the Chaos Mesh mechanism it previously named was never built,
so the record does not read as though it always said this.

**2. Does closing the techdebt entry match its own trigger?** The entry's
pay-down trigger reads "the network-drill item closes only if/when a lossy-network
scenario is actually needed." This plan closes it because the drill is being
built, which is a different reason. Either the trigger is restated when the entry
is closed, or the closure is explained against the trigger as written. Silently
closing an entry against an unmet trigger is the kind of drift the register
exists to prevent.

**Ruled: the entry is cleaned up on completion.** The register row is removed
when the drill lands, and the two residuals of §10.2 and §13.2 are opened in its
place with their own reopening conditions.

### 13.4 Corrections applied to this document

| § | Was | Now |
|---|---|---|
| 1.3 | four questions, all answerable | question 3 marked unanswerable pending §13.2 |
| 2.1 | "the HTTP rate limiter is auth-path only" | per-IP limiter on the whole `/api` subrouter, enrolment included |
| 2.2 | backfill flags give a deterministic alert replay | they give a deterministic telemetry backlog; alert replay withdrawn |
| 4.2 | enrolment already uses the FQDN | it does not; two invocation sites use the short name, one is edited |
| 4.3 | a batch ahead of the next heartbeat | true of the application stream; keepalive is below it |
| 5.2 | "no machine crosses the offline threshold" | no such threshold exists; staleness is the assertion that bites |
| 5.4 | migration works "when the connection identifier is unchanged" | it requires a *new* CID plus path validation; failure is silent |
| 5.4 | alert replay asserted on the fleet | withdrawn (§13.2) |
| 7.1 | 1830m allocatable, "about 9.4 GiB" | 9.2 GiB, and 1480m of CPU requests already committed |
| 8.2 | replace the Chaos Mesh sections with the shaper | delete them; Chaos Mesh was never built and the shaper replaces only part |
| 8.2 | — | `e2e-machine-pod.sh` added |
| 8.3 | — | no `environment: staging`; agent binary fetched, not built |
