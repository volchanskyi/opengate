# Micro-Plan: The relay's completed sessions, and a load run that can see its target die

**Status:** complete — see §0. **Branch:** `dev`. **Owner:** agent (Go, Helm, CI).
**Origin:** Load Tests run
[33565000569](https://github.com/volchanskyi/opengate/actions/runs/33565000569)
failed while run
[33590867089](https://github.com/volchanskyi/opengate/actions/runs/33590867089)
passed **on the same commit `a47b7bb`**, with the same inputs and the same
fleet result. Nothing in the code differed. What differed was how much memory
the staging server had already leaked before each run started.

**Evidence gathered 2026-09-01/02 against the live cluster and VictoriaMetrics
(30d retention — re-read before the window closes on 2026-10-01), then extended
2026-09-02 by a seven-layer sweep of the whole repository whose measurements are
reproduced in §2.5–§2.7.**

**Nothing in this plan is deferred.** Every defect the sweep found is fixed by a
workstream below, and the missing gate class is closed by WS8.

---

## 0. Where this stands

**Every workstream landed.** Four as separate commits on 2026-09-02; the
remaining five as one commit, which is a change from the original
one-PR-per-workstream sequencing in §5.

| WS | What it fixes | Status | Commit |
|---|---|---|---|
| **WS5.1** | D4 (restart half) | **landed** | `086592bc` |
| **WS1** | D1, D5 — *the bug* | **landed** | `2c30734b` |
| **WS8** | D10 | **landed** | `04bbe777` |
| **WS2** | D6, D7 | **landed** | `54baf891` |
| **WS6** | N1, N2 | **landed** | the final commit |
| **WS4** | D3 | **landed** | the final commit |
| **WS3** | D2 | **landed** | the final commit |
| **WS5.2–5** | D4 (cAdvisor half) | **landed** | the final commit |
| **WS7** | D8, D9 | **landed** | the final commit |

### What the landed half is measured to have done

The slope the whole plan turns on, taken by the tests that now hold it:

| | Retained goroutines per completed session | Retained heap per completed session |
|---|---|---|
| Before WS1 | **2.000** | **34 KiB** |
| After WS1 | **0.000** | 1.8 KiB |

Both figures were taken twice: once by
[`relay_handler_test.go`](../../../server/internal/api/relay_handler_test.go)'s
slope test and once by
[`conservation_test.go`](../../../server/tests/integration/conservation_test.go),
and both were confirmed to **fail on the pre-fix code** by temporarily reverting
the handler's park and re-running. The pre-fix figures above are that run.

Also landed: `Shutdown` now ends live relay sessions
(`Relay.WaitForDrain`); a stalled peer ends its session inside the ping budget;
teardown books out before it waits on the network and closes the two sides
concurrently; `alert-rules.yml` has its first gate and a rule on
`process_start_time_seconds`; and the conservation rule, its semgrep guard and
both fixtures are in place.

### One deviation from §4, decided during WS4

WS4 step 5 asked for both goroutines and resident memory to be gated per
completed operation. Only goroutines are. Resident memory is recorded beside
them and not gated, because the Go runtime does not return freed arena to the
operating system promptly: inside a single run, resident growth cannot be told
apart from a working set that simply got bigger, and a band wide enough not to
fire on that is wider than the leak this gate exists for — the incident's own
figure was 66 KiB per session, well inside any such band. The instruments for
resident memory are WS5's container-against-limit alert, which watches
continuously rather than twice a night, and the per-operation figure the run
records into the trend. ADR-094 states the boundary as part of the decision.

### Notes for whoever resumes

Things learned in the landed half that are not in the analysis above and would
cost an hour each to rediscover:

- **`Conn.Ping` needs the pinging side to be read.** A pong arrives through the
  connection's own read loop, so a side nothing reads can send a ping and never
  see the answer. This was measured both ways before the ping was written. It
  works in the relay only because the pipe reads both sides for the life of the
  session — any future ping outside that shape has to arrange its own reader.
- **A write that hits its deadline does not surface `context.DeadlineExceeded`
  reliably.** The library tears the connection down when the write context
  expires, so the error that comes back often names the closed socket instead.
  Assert that the write *ends*, and prove it is the deadline with a contrast arm
  against a peer that drains.
- **An echo peer is not a draining peer.** An echo whose replies nobody reads
  fills its own buffers, stops reading, and stalls exactly like a silent peer.
  The write-budget test needs a real drain-and-discard server.
- **`Register` returns two values now.** Every call site and test was adapted;
  the relay tests use a `mustRegister` helper because Go will not let a
  two-result call be spread into a three-parameter helper.
- **The token-leak semgrep rule wants `protocol.RedactToken(...)` inline at each
  log call**, not a redacted local. The relay package already followed that
  convention and says why; `handlers_relay.go` now does too. Touching a line that
  logs a redacted local will fail the pen-test gate even though the value is
  safe.
- **PMAT grades by file size.** Adding the lifetime tests pushed
  `relay_test.go` to grade B and blocked the commit; the fix was splitting the
  teardown-and-lifetime tests into `lifetime_test.go`. Expect the same on any
  file the remaining workstreams grow. Test files under `server/tests/` are
  skipped by the grader; files under `server/internal/` are not.
- **`git add` before `make shell-fmt` stages the unformatted file.** The gauntlet
  reads the work tree, so it passes while the commit carries unformatted
  content, and CI then fails on shfmt drift. Format first, stage second.
- **SonarCloud went down mid-run on 2026-09-02** with *"Failed to load the
  quality profiles of project 'volchanskyi\_opengate'"* after three attempts,
  which also fails the new-coverage guard downstream because there are no
  figures to read. There is no skip path; it is a retry, not a fix.

---

## 1. What happened, in order

| Time (2026-09-01) | Signal | Source |
|---|---|---|
| 18:10 | staging server clean: 29 goroutines, 29 MiB | `go_goroutines`, `process_resident_memory_bytes` |
| 18:20 / 18:40 / 19:00 | three load runs ratchet it to 2407 → 4777 → **7148 goroutines, 334 MiB** and it never comes back down | same |
| 19:00 → 22:10 | flat at 7147 goroutines / **344 MiB against a 384Mi limit**, zero agents connected, ~0 CPU, for three hours | same |
| 22:12:39 | run 33565000569 starts its QUIC fleet; 100 agents dial pod IP `10.244.0.8` directly (workflow `hostAliases`) | [load-test.yml:232](../../../.github/workflows/load-test.yml#L232) |
| 22:13:30 | `opengate_agents_connected` = 100; RSS 344.15 MiB; goroutines 7455 | VictoriaMetrics |
| **22:13:58** | **the server process is replaced** — `process_start_time_seconds` jumps to exactly this second | `process_start_time_seconds` |
| 22:13:58–22:14:10 | k6 gets `connection refused` on the ClusterIP: the Service has no ready endpoint | job log |
| 22:14–22:16 | `node_vmstat_oom_kill` on the node goes **2 → 4** — the only OOM kills on that node in three days | `node_vmstat_oom_kill` |
| 22:14:30 | RSS 344 → 28 MiB, goroutines 7455 → 32, DB pool 5 → 1; new process resets every device to offline ([app.go:214](../../../server/internal/app/app.go#L214)) | VictoriaMetrics |
| 22:14:30 → end | `opengate_agents_connected` = **0 for the rest of the run**; nothing reconnects | VictoriaMetrics |
| 22:17:30 | `relay-throughput` `setup()` finds no online machine, exit 107 → run recorded invalid | job log |

The passing run survived on timing, not on merit: CD's *Deploy staging* ran
04:25:45–04:29:09 and handed it a fresh pod ~90 seconds before it took the
namespace lease. Its own pod then leaked the same way — 31 goroutines / 29 MiB
at 04:30, **2695 goroutines / 130 MiB by 04:38** — and was replaced by the
fault drill before anything noticed.

The ratchet is deterministic, not flaky. **Four runs to death:**

```
29 MiB → 134 → 230 → 334 → OOM      [container limit 384Mi]
```

---

## 2. The defect, isolated and reproduced

### 2.1 What leaks

[`handlers_relay.go:138`](../../../server/internal/api/handlers_relay.go#L138) —
`registerAndWait` ends with `<-ctx.Done()`, where `ctx` is `r.Context()`, the
**HTTP request context of a connection `websocket.Accept` has already
hijacked**. Once net/http hands the connection away it stops managing it, so
nothing cancels that context when the client goes away. The handler blocks
forever.

Reproduced standalone against `nhooyr.io/websocket v1.8.17`, 20 clients
connecting and hanging up:

| Arm | Handlers entered | Handlers returned | Goroutines leaked per connection | Heap retained per connection |
|---|---|---|---|---|
| A — `<-r.Context().Done()` (today) | 20 | **0** | **2.05** | **15,983 B** |
| B — `<-sessionDone` (proposed) | 20 | **20** | 0.05 | 2,148 B |

Every relay session opens **two** of these handlers — the operator's
(`side=browser`) and the machine's (`side=agent`, [loadtest
relay.go:99-103](../../../server/tests/loadtest/relay.go#L99)) — so a completed
session strands two goroutines. Measured in production: 1183 relay sessions →
**+2232 goroutines, +76 MiB**, or **65.8 KiB per session**. Arm B is the fix,
and it is proven, not assumed.

**What that costs when remote sessions are in real use.** Production runs the
same 384Mi limit with `requests` equal to `limits`
([values-production.yaml](../../../deploy/helm/opengate/values-production.yaml)) on
a ~29 MiB idle baseline — roughly **5,400 completed sessions of headroom**. An
estate whose technicians open a few thousand sessions a day reaches that inside
a day, and every kill runs `ResetAllStatuses`, so the whole fleet goes offline
at once and re-registers.

### 2.2 What does *not* leak — do not "fix" these

- **The pipe goroutines.** [`relay.go:322-332`](../../../server/internal/relay/relay.go#L322)
  both exit: `closeBoth` closes each `Conn`, which errors the blocked read.
  The arithmetic says so — the two handlers account for it, not four things.
- **Sockets and file descriptors.** `process_open_fds` sat flat at 16–17
  through the entire 344 MiB climb. The websocket `Close` reaches
  `c.rwc.Close()`; only the goroutine and its retained heap survive.
- **The relay's own bookkeeping.** `opengate_relay_active_sessions` and
  `r.count` return to zero — [`pipe`'s defer](../../../server/internal/relay/relay.go#L307)
  runs. §2.7 explains why that is the problem rather than the reassurance, and
  §2.6 shows they return to zero up to ten seconds late.

### 2.3 Why the happy path is the leaking one

A side whose peer never arrives hits `peerWaitTimeout`, returns, and does not
leak. Only a **successful, fully piped session** leaks. The failure path is
clean and the success path is not, which is the reverse of where anyone looks.

### 2.4 Production exposure

The real agent ([`session/relay.rs`](../../../agent/crates/mesh-agent-core/src/session/relay.rs))
and the web client both drive the same `/ws/relay/{token}` handler.
Production is flat only because it has no real remote-session traffic yet.
Every future customer session leaks two goroutines. The nightly load test is
the canary that found it.

### 2.5 Nothing cancels that context — including shutdown

Three separate mechanisms in `net/http` say why, and all three were read in the
toolchain source rather than inferred:

- `w.cancelCtx()` runs **after** `ServeHTTP` returns, which is circular for a
  handler blocked inside it.
- The hijack calls `abortPendingRead`, which stops the background read that is
  the only thing turning a client hangup into a cancellation.
- `setState(rwc, StateHijacked)` calls `trackConn(c, false)`. The connection is
  **untracked**, so `Server.Shutdown` neither waits for it nor closes it, and
  `Server.Close` cannot reach it either.

Measured directly, with five hijacked handlers still blocked:

```
C  Shutdown() vs a hijacked handler   entered=5  returned=0  ctxFired=0   Shutdown took 0s, err=<nil>
```

So `r.Context()` is not a shutdown signal for this handler — it is a select
branch that can never fire. And
[`main.go`](../../../server/cmd/meshserver/main.go)'s `httpSrv.Shutdown` reports a
graceful drain it did not perform: on a rolling deploy every live relay session
is severed by process exit with no close frame. That is the failure class
[`ci-cd-determinism.md`](../../rules/ci-cd-determinism.md) already rules against —
a step whose work was refused must not report success — living in the product
rather than in CI.

### 2.6 The same pair of sockets has no liveness check and a slow teardown

- **A write with no deadline, and no liveness probe of any kind.**
  `WSConn.WriteMessage` and `ReadMessage` both pass `context.Background()`
  ([`wsconn.go`](../../../server/internal/api/wsconn.go)); nothing pings;
  `RelayPeerTimeout`'s own comment records that paired sessions are not
  time-limited. Against a peer that stops reading, the write was **still
  blocked after 6 s with no deadline and no error**. TCP keep-alive bounds a
  peer that *vanishes* (Go's listener sets 15 s idle, 15 s interval, 9 probes)
  but not a peer that is present and not draining.
- **Teardown waits on the network before it updates its own books.**
  `closeBoth` performs two graceful closes sequentially, each waiting up to 5 s
  for a close frame an absent peer never sends:

  ```
  E1 Close(StatusNormalClosure) against a peer that never answers:  5.005s
  E2 closeBoth() shape — two sequential graceful closes:           10.005s
  E3 the same pair with CloseNow():                                     0s
  ```

  And [`pipe`'s defer](../../../server/internal/relay/relay.go#L305) runs
  `closeBoth()` **first**, before `r.count.Add(-1)`, the map delete, the
  registry delete and `OnSessionEnd`. So for up to ten seconds after a session
  is over the gauge still counts it, `ActiveTokens` still reports it live — so
  the session sweeper will not reclaim its row — and the registry still holds
  it. [`Unregister`](../../../server/internal/relay/relay.go#L235) does the same
  work in the opposite order and states the principle in a comment: *"External
  cleanup must already be complete before that network wait begins."* The two
  teardown paths disagree, and `Unregister` is the one that is right.

### 2.7 Why no gate could see it, and what that generalises to

The decisive experiment: a copy of the server tree with `<-ctx.Done()` deleted
from `registerAndWait`, re-running every test in all three tiers whose name
contains "relay".

```
############ BASELINE ############        ############ MUTANT: the wait deleted ############
ok  .../internal/relay        0.126s      ok  .../internal/relay        0.119s
ok  .../internal/api          9.975s      ok  .../internal/api          8.348s
ok  .../tests/integration     4.144s      ok  .../tests/integration     3.518s
```

Not one assertion in 1,519 Go test functions holds that line. It is
behaviourally inert to everything the suite checks, and marginally *faster*
without it because the handlers return instead of being abandoned. Every gate
class is blind for its own structural reason: coverage counts the line as
covered because it executes; the benchmark trend measures `allocs/op`, which is
identical for a leak because only *retention* differs; mutation testing mutates
conditionals and arithmetic, and any statement-level mutant here is an
equivalent mutant by every existing assertion.

`runtime.NumGoroutine`, `go.uber.org/goleak` and `runtime.ReadMemStats` return
**zero matches across the whole repository**. Across 43 gauntlet checks, 88
shell tests and 25 CI jobs there is no conservation assertion in any language at
any tier.

Underneath that sits the general shape. Every liveness number this system
publishes is **bookkeeping the teardown path maintains**, not a measurement of
the resource. `opengate_relay_active_sessions` returned to zero *because the
code that decrements it ran*. WS4 closes it nightly; WS8 closes it at commit
time and writes the rule: a counter of a resource must be paired with a reading
of the resource, and the pair asserted.

---

## 3. What let a three-hour condition end as a CI failure

| # | Defect | Evidence | Fixed by |
|---|---|---|---|
| **D1** | The relay leaks two goroutines per completed session, forever. | §2.1 | WS1 |
| **D2** | The QUIC harness reported `Agents: 100/100 succeeded, Failures: 0` and a `valid` bundle for an 8m30s hold whose connections were severed at the 4-minute mark. [`holdOpen`](../../../server/tests/loadtest/soak.go#L179) only ever **reads**, and [`isTimeout`](../../../server/tests/loadtest/soak.go#L306) sends every timeout back around the loop as "a quiet server has nothing to say". A dead peer and a quiet one are indistinguishable to it. | job log vs `opengate_agents_connected` | WS3 |
| **D3** | Nothing invalidates a run whose **target** restarted mid-run. `api-baseline` recorded a 0.74% error rate and 45 failed checks against a server that was dying, and those rows counted as produced. The run only failed because the restart happened to break the *last* scenario. | job log; `validity.go` has the right three-outcome model but no target-health input | WS4 |
| **D4** | Nothing watches a container against its own limit. The Grafana rule at [alert-rules.yml:203](../../../deploy/grafana/provisioning/alerting/alert-rules.yml#L203) reads `node_memory_MemAvailable_bytes` — node-wide — so a pod at 90% of its cgroup limit for three hours fired nothing. No cAdvisor scrape exists ([vmagent-scrape.yaml](../../../deploy/helm/monitoring/files/vmagent-scrape.yaml) has three jobs, none of them the kubelet). `up{job="opengate-server"}` stayed 1 across the restart. | live `ClusterRole monitoring-victoriametrics`; VictoriaMetrics label values | WS5 |
| **D5** | `httpSrv.Shutdown` reports a graceful drain it did not perform: a hijacked connection is untracked, so live relay sessions are neither waited for nor closed. | §2.5 | WS1 |
| **D6** | Relay reads and writes carry no deadline and there is no liveness probe. A present-but-stalled peer holds two goroutines and two sockets indefinitely. | §2.6 | WS2 |
| **D7** | Teardown performs two sequential graceful closes, and does them *before* its own bookkeeping — so a finished session counts as active, and blocks its own row from being swept, for up to ten seconds. | §2.6 | WS2 |
| **D8** | `signaling.Tracker` (the type this workstream removed) holds a `sync.Map` whose `Remove` nothing calls; it does not leak only because nothing calls `StartSignaling` either. Production touches the tracker solely for `Config().ICEServers`, so `opengate_signaling_upgrades_total` is a documented metric that is structurally always zero — the upgrade is negotiated *through* the relay pipe, which the server copies without decoding. Its only exercise is an integration test calling the tracker directly. | grep of production call sites | WS7 |
| **D9** | `auditLog` ([api.go:567](../../../server/internal/api/api.go#L567)) fans out one goroutine per audited action with a timeout but **no concurrency bound**, while its structural sibling `persistTelemetry` holds a slot semaphore. Not a leak; a burst amplifier competing for the same connection pool. | §2.7 sweep | WS7 |
| **D10** | No gate at any tier asserts that a completed operation returns what it took, so this class of defect is invisible until a nightly run dies of it. | §2.7 | WS8 |
| **N1** | `/metrics` is reachable through the public ingress. The chart's only rule is `path: /`, `pathType: Prefix`; there is no NetworkPolicy and no proxy in front. The endpoint also sits **outside** the rate-limited subrouter, so it is unauthenticated, unthrottled, and renders the whole registry per request. | §4.6 | WS6 |
| **N2** | Two comments assert that boundary, and one of them is an executable check that has been switched off since the Caddy era. | §4.6 | WS6 |

D1 is the bug. Everything else is why nobody saw it, or what the sweep found
beside it.

---

## 4. Workstreams

Eight, independent, landing as separate PRs. **WS1 is the only one that fixes
the original bug**; WS8 is the only one that stops the class. TDD is mandatory
throughout — the failing test lands before the source change in every step.

### WS1 — The relay owns its sessions' lifetime *(fixes D1, D5)*
**LANDED — `2c30734b`.** Kept below as the record of what was decided and why.


The handler must stop waiting on a context that will never be cancelled and
start waiting on the signal the relay already knows: the session ended.

**Files:** [`server/internal/relay/relay.go`](../../../server/internal/relay/relay.go),
[`server/internal/api/handlers_relay.go`](../../../server/internal/api/handlers_relay.go),
[`server/internal/api/api.go`](../../../server/internal/api/api.go) (`ServerConfig`),
[`server/internal/app/app.go`](../../../server/internal/app/app.go) (carry the
lifetime context in), [`server/cmd/meshserver/main.go`](../../../server/cmd/meshserver/main.go),
[`server/internal/relay/relay_test.go`](../../../server/internal/relay/relay_test.go),
[`server/internal/api/relay_handler_test.go`](../../../server/internal/api/relay_handler_test.go).

1. **Test first** — in `relay_handler_test.go`, a test that pairs both sides,
   drives one message, closes the client, and requires the handler goroutine to
   return within a bound. It must fail on today's code.
2. **Test first** — a leak-class regression test, **in the slope form**, not as
   a fixed baseline. Measure retained goroutines at several completed-session
   counts and assert the slope is ~0. A `runtime.NumGoroutine()` ±5 assertion
   would be brittle: every `NewServer` starts **three immortal goroutines** —
   two rate-limiter sweepers and the login-failure sweeper — none of which takes
   a context or can be stopped, and the test store and pool add more. The slope
   form is immune to any fixed baseline and is the shape this repository already
   uses and already tests in
   [`server/tests/vmramseries/`](../../../server/tests/vmramseries/). Prefer it over
   adding `go.uber.org/goleak`: one dependency fewer through `govulncheck`, and
   the assertion is the one that actually failed here. WS8 generalises this test;
   this one is its first instance.
3. Add a per-session `done chan struct{}` to `session`, closed exactly once
   (`sync.Once` on the session) by **both** teardown owners — `pipe`'s defer
   ([relay.go:307](../../../server/internal/relay/relay.go#L307)) and
   `Unregister` ([relay.go:243](../../../server/internal/relay/relay.go#L243)).
4. Change `Register` to return `(<-chan struct{}, error)`. Returning the
   channel from the registering call is deliberate: a separate
   `SessionDone(token)` lookup races the map delete that teardown performs.
5. `registerAndWait` selects on `{sessionDone, lifetimeCtx.Done()}` — a
   **server-lifetime context**, not `r.Context()`. §2.5 proves the request
   context is never cancelled by anything, so keeping it in the select would
   encode a belief that shutdown is handled. `app.Build` already receives a
   lifetime context; it simply is not carried into `ServerConfig` today. Adding
   it is what closes D5: `main.go`'s shutdown path can then end live sessions
   instead of reporting a drain it never performed.
6. `registerAndWait` must `CloseNow()` the websocket on its way out. net/http
   will not close a hijacked connection for a handler that returns, and today
   that path is unreachable so nothing covers it. `CloseNow` rather than `Close`
   for the reason E1–E3 measures.
7. Both `pipe` goroutines get a done channel and `pipe` waits for both before
   returning. Not a leak today (§2.2) — but the asymmetry at
   [relay.go:337-341](../../../server/internal/relay/relay.go#L337) is one edit
   away from becoming one.

**Considered and rejected: deleting the wait outright.** A handler that returns
immediately after pairing is provably leak-free — net/http will not close a
hijacked connection, so the relay keeps piping through the still-open socket,
which is exactly why the §2.7 mutant stayed green. It is rejected because the
handler's own `relay session disconnected` log line would then fire at pairing
time rather than at session end, and because nothing would be left holding the
socket to close it. Record this in ADR-093 as the alternative considered.

**Acceptance:** both new tests green; the slope of retained goroutines against
completed sessions is ~0 across at least three load points; `Shutdown` ends live
relay sessions rather than returning immediately.

### WS2 — A session that knows its peer is alive, and books itself out before it waits on the network *(fixes D6, D7)*
**LANDED — `54baf891`.** Kept below as the record of what was decided and why.


Lands after WS1, because the keep-alive belongs in the handler WS1 keeps parked
until session end.

**Files:** [`server/internal/api/wsconn.go`](../../../server/internal/api/wsconn.go),
[`server/internal/api/handlers_relay.go`](../../../server/internal/api/handlers_relay.go),
[`server/internal/relay/relay.go`](../../../server/internal/relay/relay.go),
[`server/internal/api/wsconn_test.go`](../../../server/internal/api/wsconn_test.go),
[`server/internal/api/relay_handler_test.go`](../../../server/internal/api/relay_handler_test.go),
[`server/internal/relay/relay_test.go`](../../../server/internal/relay/relay_test.go).

1. **Test first** — a peer that accepts the connection and never reads. The
   forwarding write must fail with a deadline error rather than blocking. Today
   it blocks past any bound the test can set.
2. **Test first** — a peer whose socket is alive but whose process is gone. The
   session must end within a stated number of missed pings. Today it never ends.
3. **Test first** — a session whose peers never answer a close. The relay's
   active-session count must reach zero *before* the close completes, and
   `ActiveTokens` must stop naming the token.
4. `WSConn.WriteMessage` takes a bounded context instead of
   `context.Background()`. Name the constant and state what it is a bound on: a
   relay frame that a peer cannot accept within it is a peer that is not
   consuming, not a slow link. `ReadMessage` keeps `context.Background()` — a
   quiet session is legitimate and a bare read deadline would kill a technician
   watching a static screen. Liveness is step 5's job, not the read's.
5. `registerAndWait` runs a ping ticker on its own `*websocket.Conn` while it
   waits for the session to end. `Conn.Ping(ctx)` with a bounded context writes
   a control frame and waits for the pong, so it detects **both** the peer that
   vanished and the peer that is present and not draining — the write itself
   carries the deadline. On failure: `CloseNow()` and return, which errors the
   pipe's read and ends the session for both sides. This needs no change to the
   `relay.Conn` interface and so no change to the loadtest fake.
6. **Rejected: a hard cap on session duration.** A remote session that a
   technician legitimately holds open for an hour is not a defect, and a
   duration cap would make it one. The ping is the liveness answer; a cap is a
   product regression wearing a fix's clothes. Record this in ADR-093.
7. `pipe`'s defer is reordered to match `Unregister`, which already states the
   principle: books first, network wait last. Decrement the count, delete the
   token, delete the registry entry and fire `OnSessionEnd`, **then** close.
8. `closeBoth` closes the two sides **concurrently** rather than in sequence,
   bounding the bad case at one handshake instead of two.
9. **Rejected: `CloseNow` everywhere in teardown.** It would be 0 s always, but
   it discards the `1000 Normal Closure` that tells an operator's browser the
   session ended rather than broke. Steps 7 and 8 get the books honest
   immediately and halve the remaining wait, which is what actually mattered.

**Acceptance:** a stalled peer ends the session within the stated ping budget; a
finished session's count and token are gone before any close handshake
completes; the existing relay data-path tests are unchanged.

### WS3 — A hold that can tell a quiet server from a dead one *(fixes D2)*
**NOT STARTED.** Lands in the single remaining commit.


**Files:** [`server/tests/loadtest/soak.go`](../../../server/tests/loadtest/soak.go),
[`server/tests/loadtest/hold_test.go`](../../../server/tests/loadtest/hold_test.go).

The heartbeat is agent→server ([conn.go:424](../../../server/internal/agentapi/conn.go#L424)),
so a held agent that only reads receives nothing from a *healthy* server
either. Reading harder cannot close this; the hold has to **write**.

1. **Test first** — a fake stream whose peer dies mid-hold. `holdOpen` must
   return an error. Today it returns `nil` after burning the full duration.
2. `holdOpen` sends `MsgAgentHeartbeat` on an interval well inside the hold.
   A write to a dead QUIC peer fails, which turns an undetectable severance
   into a named error. It is protocol-legal — the agent already sends these
   during its traffic phase — and it keeps the device's `online` status true,
   which the relay scenario depends on.
3. A hold that received and sent nothing for its whole duration is a failure,
   not a success. Fold that into the harness's own reasons so the bundle says
   which agents lost their connection and when.

**Acceptance:** the fleet step goes red on a target that dies mid-hold; the
existing green path is unchanged.

### WS4 — The run's conservation record *(fixes D3)*
**NOT STARTED.** Lands in the single remaining commit. Owes ADR-094.


This is the sibling of [ADR-090](../../../docs/adr/ADR-090-a-run-is-gated-on-its-own-verdict-not-on-its-failure-count.md):
a run is gated on its own verdict, and both "my target was replaced underneath
me" and "my target did not give back what it took" belong in that verdict.

**The seam already exists.** [`server_registration.go`](../../../server/tests/loadtest/server_registration.go)
already fetches the target's `/metrics`, already parses Prometheus exposition
text, and already writes `db_pool_in_use` and `db_pool_open` into the bundle's
`Observations` — a section [`bundle.go`](../../../server/tests/loadtest/bundle.go)'s
`Validate()` already makes mandatory. The registry registers `NewGoCollector`
and `NewProcessCollector`, so the four families this needs are on the very page
the harness already reads. **No kubeconfig, no `restartCount`, no pod UID, no
Prometheus round trip** — and unlike a cluster-side check it also works for the
volume and scaling families, which run on a GitHub runner where there is no
cluster to ask.

**Files:** [`server/tests/loadtest/server_registration.go`](../../../server/tests/loadtest/server_registration.go)
(or a sibling `target_health.go` — `tests/loadtest` is already a `dir:` mutation
shard and `main.go` is already in the mutation exclusion regex, so a new
non-test file there needs no
[`mutation-shards.sh`](../../../scripts/lib/mutation-shards.sh) edit),
[`bundle.go`](../../../server/tests/loadtest/bundle.go),
[`validity.go`](../../../server/tests/loadtest/validity.go),
[`run_bundle.go`](../../../server/tests/loadtest/run_bundle.go),
[`main.go`](../../../server/tests/loadtest/main.go),
[`scripts/loadtest-run-completeness.sh`](../../../scripts/loadtest-run-completeness.sh),
[`scripts/tests/loadtest-run-completeness.test.sh`](../../../scripts/tests/loadtest-run-completeness.test.sh),
plus the matching `_test.go` files.

1. **Test first** — a parsed page fixture at run start and one at run end, and a
   `Classify` case for each of the two new outcomes. Extend the shell
   completeness gate the same way.
2. Read `go_goroutines`, `process_resident_memory_bytes`, `process_open_fds` and
   `process_start_time_seconds` off the page the harness already fetches. Take
   the **start** reading as the first thing `main` does after flags, before
   `runWorkload` — the harness starts before k6 and finishes after all three
   scenarios, so its own brackets cover the whole night. Take the **end**
   reading after the fleet is wound down, for the reason ADR-090 gives.
3. Both readings enter `Observations`. `process_open_fds` travels because it is
   what distinguishes a goroutine leak from a socket leak, and its flatness is
   what ruled sockets out in §2.2.
4. `process_start_time_seconds` changed between the two readings →
   **`ResultInvalid`**, reason `target restarted mid-run`. **Invalid, not
   failed** — the numbers were measured against two different processes, and
   `validity.go`'s own comment explains why absorbing that as data costs two
   nights.
5. Goroutines or resident memory not back within a band → **`GateBreaches`** →
   `ResultFailed`. A finding about the system, not an invalidation, which is
   what `validity.go`'s own doctrine says. Express the band **per completed
   session** rather than absolutely — the harness knows how many relay sessions
   it joined, so the check is a slope and does not have to guess at a fixed
   floor.
6. `scripts/loadtest-run-completeness.sh` gains the same two branches, because a
   run has one verdict however many places compute it — the file already says so
   about `MAX_ERROR_RATE`.

**Why this and not a cluster-side check.** The bundle is authoritative past the
30-day metrics retention, it enters the trend, and the reason travels with the
run. This incident would have been a red run on the first night with the cause
named in the bundle.

**Acceptance:** a run with a mid-run restart is recorded invalid and does not
enter the trend, with the reason naming the restart; a run whose target did not
give its goroutines back is recorded failed, with the per-session figure named.

### WS5 — See a container against its own limit *(fixes D4)*

**PARTLY LANDED.** Step 1 (the `process_start_time_seconds` alert) and the new
`alert-rules.test.sh` gate shipped in `086592bc`. Steps 2–5 — the cAdvisor scrape
job, the `ClusterRole` grant, the working-set-against-limit and OOM-event alerts,
and their gate assertions — are **not started** and land in the single remaining
commit. Note that `monitoring-scrape.test.sh` currently asserts the scrape config
has **no** `role: node` job; adding cAdvisor means changing that assertion, not
working around it.


A bundle cannot page anyone at 03:00, so this stays in full.

**Files:** [`deploy/helm/monitoring/files/vmagent-scrape.yaml`](../../../deploy/helm/monitoring/files/vmagent-scrape.yaml),
[`deploy/helm/monitoring/templates/victoriametrics.yaml`](../../../deploy/helm/monitoring/templates/victoriametrics.yaml)
(the `ClusterRole`), [`deploy/grafana/provisioning/alerting/alert-rules.yml`](../../../deploy/grafana/provisioning/alerting/alert-rules.yml),
[`scripts/tests/monitoring-scrape.test.sh`](../../../scripts/tests/monitoring-scrape.test.sh),
plus a new alert-rules gate.

1. **Restart detection needs no new infrastructure.** `process_start_time_seconds`
   is already collected and dated this restart to the exact second. Alert on
   it changing. Do this first — it is one rule and it would have caught the
   event on its own.
2. **Memory-against-limit needs cAdvisor.** Add a `kubernetes-cadvisor` scrape
   job (`role: node`, kubelet `/metrics/cadvisor`) for
   `container_memory_working_set_bytes`, `container_spec_memory_limit_bytes`
   and `container_oom_events_total` — the last names the container that was
   killed, which is the answer this incident cost six hours to reconstruct.
3. The scraper's `ClusterRole` currently grants `pods`, `endpoints`, `services`
   only. It needs `nodes`, `nodes/metrics`, `nodes/proxy` and the
   `/metrics/cadvisor` non-resource URL. Verified against the live cluster.
4. Alert on **working set / limit > 80% for 10m** per container. That
   condition was true for three hours and said nothing.
5. **Test first, and in the same commit** — `monitoring-scrape.test.sh` gains
   the cAdvisor job assertions, and a new `alert-rules.test.sh` (chmod +x,
   `100755`, or the gauntlet's shell-tests step fails) pins the three new
   rules. There is no gate on `alert-rules.yml` today; adding the rule without
   the guard is the pattern this repo has already rejected twice.

**Acceptance:** `container_oom_events_total` and
`container_memory_working_set_bytes` resolve in VictoriaMetrics for the
`opengate-staging` namespace; all three alerts evaluate; both gates green.

### WS6 — An internal listener: pprof and `/metrics` off the public edge *(fixes N1, N2)*
**NOT STARTED.** Lands in the single remaining commit. Owes ADR-095.


**Why an internal listener has to be built rather than reused.** Everything the
server serves — API, `/ws`, `/metrics`, `/healthz` and the SPA — is on one chi
router behind one catch-all ingress rule, because SPA serving moved into the Go
binary (`-web-dir /srv/web`,
[values.yaml:27](../../../deploy/helm/opengate/values.yaml#L27)). Mounting
`net/http/pprof` on a path would publish it. The ingress-level carve-out is also
closed by an existing deliberate decision: the chart runs the controller with
`allowSnippetAnnotations=false`
([NOTES.txt](../../../deploy/helm/opengate/templates/NOTES.txt)), which is why
security headers live in the add-headers ConfigMap.

**The comment was true when it was written.** Its ancestor read *"not proxied by
Caddy — internal only"*, and the retired Caddyfile routed only `handle /api/*`
and `handle /ws/*` to the server; everything else fell to a static handler with
an `index.html` fallback. Commit `17e34309` — the commit that *introduced*
[`docs-live-state.md`](../../rules/docs-live-state.md) — swapped "not proxied by
Caddy" for "not exposed through the ingress" as a live-state edit, substituting
the new edge's name for the old one without re-verifying the claim.

**What reads `/metrics` today, and how.** Five consumers, none of which uses the
public route — every one talks to the Service, the pod, the compose port, or the
process itself:

| Consumer | Reaches it by | Runs |
|---|---|---|
| vmagent | in-cluster endpoints scrape, job `opengate-server`, port name `http` | every 15 s |
| CD smoke test | `kubectl port-forward svc/…-server 18080:8080` ([cd.yml:357](../../../.github/workflows/cd.yml#L357), [cd.yml:616](../../../.github/workflows/cd.yml#L616)) | every staging and production deploy |
| `make e2e` smoke test | compose stack, `--host 127.0.0.1 --port 8080` ([Makefile:304](../../../Makefile#L304)) | every gauntlet run |
| Load-test QUIC harness | `-metrics-url=http://${RELEASE}-server:8080` ([load-test.yml:124](../../../.github/workflows/load-test.yml#L124)) | every nightly run |
| Acceptance tier | `a.Get("/metrics").Text()` ([device_health_test.go:200](../../../server/tests/acceptance/device_health_test.go#L200)) | every `go test` |

**Files:** [`server/internal/app/app.go`](../../../server/internal/app/app.go) and
its test, [`server/cmd/meshserver/main.go`](../../../server/cmd/meshserver/main.go),
[`server/internal/api/api.go`](../../../server/internal/api/api.go),
[`deploy/helm/opengate/templates/server-service.yaml`](../../../deploy/helm/opengate/templates/server-service.yaml),
[`deploy/helm/opengate/templates/server-deployment.yaml`](../../../deploy/helm/opengate/templates/server-deployment.yaml),
[`deploy/helm/opengate/values.yaml`](../../../deploy/helm/opengate/values.yaml),
[`deploy/helm/monitoring/files/vmagent-scrape.yaml`](../../../deploy/helm/monitoring/files/vmagent-scrape.yaml),
[`.github/workflows/cd.yml`](../../../.github/workflows/cd.yml),
[`.github/workflows/load-test.yml`](../../../.github/workflows/load-test.yml),
[`deploy/scripts/smoke-test.sh`](../../../deploy/scripts/smoke-test.sh),
[`deploy/docker-compose.test.yml`](../../../deploy/docker-compose.test.yml),
[`scripts/tests/monitoring-scrape.test.sh`](../../../scripts/tests/monitoring-scrape.test.sh),
[`scripts/tests/cd-workflow.test.sh`](../../../scripts/tests/cd-workflow.test.sh),
[`scripts/tests/loadtest-workflow.test.sh`](../../../scripts/tests/loadtest-workflow.test.sh),
[`docs/architecture/Metrics-Reference.md`](../../../docs/architecture/Metrics-Reference.md),
[`docs/infrastructure/Monitoring.md`](../../../docs/infrastructure/Monitoring.md).

1. **Test first** — the profiling routes and `/metrics` are reachable on the
   internal listener and **absent from the public one**, asserted as two
   handlers rather than one.
2. Build the second `http.Server` **in `internal/app`**, returning it for
   `main.go` to start the way `serveBackground` starts the public one. Not in
   `main.go` itself: the Go coverage gate filters only `/testutil/` and
   `api/openapi_gen.go`, so `cmd/meshserver` counts toward the ≥80% total and
   untestable wiring there costs coverage.
3. Move `/metrics` and mount `net/http/pprof` on it. Leave `/healthz` on the
   public listener — the kubelet probe needs it.
4. **The four call sites move in the same commit, and three of them fail
   silently if missed** — this is precisely
   [`ci-cd-determinism.md`](../../rules/ci-cd-determinism.md)'s subject, so each
   gets a read-back:
   - `server-service.yaml` gains a `metrics` port; `monitoring-scrape.test.sh`
     asserts the `opengate-server` job's `keep` regex moved from `http` to
     `metrics` (**already guarded** — extend the existing assertions).
   - Both `cd.yml` port-forwards. The smoke test's `/metrics` check must reach
     the new port; either forward both or split the check's base URL. **New
     gate:** extend `cd-workflow.test.sh` to assert the forwarded port matches
     the chart's metrics port.
   - The harness's `-metrics-url`. **New gate:** extend
     `loadtest-workflow.test.sh` the same way.
   - The compose port mapping for `make e2e`.
5. **Invert the smoke test's skip, do not delete it.**
   [`smoke-test.sh:105-119`](../../../deploy/scripts/smoke-test.sh#L105) already
   carries the boundary as an executable claim and wraps it in
   `if [[ -z "$DOMAIN" ]]`. That skip dates to v0.13.2 (2026-03-23), when the
   Caddy edge served `/metrics` as `try_files {path} /index.html` — 200 with the
   SPA page, so the body grep failed and the check was skipped rather than
   inverted. Neither CD invocation passes `--domain`, so it has never run in CI.
   Make a `--domain` run assert `/metrics` is **not** the exposition, and add one
   `--domain` smoke run to CD so the boundary is proven on every deploy rather
   than assumed.
6. Fix both stale comments (N2): `api.go`'s *"internal only — not exposed
   through the ingress"* and `smoke-test.sh`'s *"A `--domain` run … cannot see
   it"*. Both become true statements about the new listener. Say what the code
   does now, per [`docs-live-state.md`](../../rules/docs-live-state.md).
7. Document the Go and process collector families in
   `Metrics-Reference.md`, which claims to list *every* series the server
   publishes and omits the four that diagnosed this incident.
8. Confirm the pen-test gate agrees pprof is unreachable from outside.

**Acceptance:** `/metrics` and `/debug/pprof` answer on the internal port and
404 on the public one; a `--domain` smoke run proves it; all five consumers
still read metrics, each proved by its own gate; `make e2e` and the nightly load
run both green.

### WS7 — Two bounded structures *(fixes D8, D9)*
**NOT STARTED.** Lands in the single remaining commit.


**Files:** `server/internal/signaling/tracker.go` (removed),
[`server/internal/api/api.go`](../../../server/internal/api/api.go),
[`server/internal/metrics/metrics.go`](../../../server/internal/metrics/metrics.go),
[`server/internal/app/background.go`](../../../server/internal/app/background.go),
[`server/tests/integration/signaling_relay_test.go`](../../../server/tests/integration/signaling_relay_test.go),
[`docs/architecture/Metrics-Reference.md`](../../../docs/architecture/Metrics-Reference.md),
plus tests.

1. **Test first** — for D9, a burst of audited actions must not exceed the
   concurrency bound; a shed write must be counted, not lost.
2. **D8 — decide, then make the code say it.** The WebRTC upgrade is negotiated
   inside the relay pipe, which the server copies without decoding, so the
   server cannot observe it as built. Either wire a real caller — which means
   the relay inspecting control frames, a decision rather than a cleanup — or
   remove the unreachable state machine, the map with no eviction caller, and
   the metric that can never move, taking its row out of `Metrics-Reference.md`
   and its source out of `GaugeSource`. Removing is the smaller, honest change.
   Either way the outcome must not be a map with a `Remove` nobody calls waiting
   for its first caller. If the tracker stays, `signaling_relay_test.go` must
   drive it **through the product** rather than calling it directly.
3. **D9 — give `auditLog` the bound its sibling has.** `persistTelemetry`'s slot
   semaphore is the reference pattern; copy its shape, including the counted
   drop so a shed audit write is reported rather than silently lost.

**Acceptance:** no map in the process has an eviction path with no caller; a
burst of audited actions holds the bound and reports what it shed; the metrics
reference and the code agree about what the server publishes.

### WS8 — Conservation at commit time, and the rule that holds it *(fixes D10)*
**LANDED — `04bbe777`.** Kept below as the record of what was decided and why.


WS4 catches this class nightly. This catches it before the commit lands, and
writes the rule so the next long-lived connection path inherits it.

**Files:** a new `server/tests/integration/conservation_test.go`, a new
`policy/semgrep/resources/hijacked-request-context.yaml`,
[`scripts/tests/pentest-review.test.sh`](../../../scripts/tests/pentest-review.test.sh),
a new `.claude/rules/resource-conservation.md`,
[`CLAUDE.md`](../../../CLAUDE.md) (the rules index row),
[`docs/infrastructure/Testing.md`](../../../docs/infrastructure/Testing.md).

1. **Test first, and it is the deliverable.** `conservation_test.go` drives N
   complete relay sessions against one assembled server at several values of N,
   fits a line through retained goroutines and retained heap against completed
   sessions, and asserts both slopes are ~0. The **slope**, not a fixed
   baseline, for the reason WS1 step 2 gives — the three immortal per-server
   goroutines are a constant that a slope removes and a baseline cannot.
   The method is [`vmramseries`](../../../server/tests/vmramseries/)'s, whose own
   comment explains why a single reading divided by what is present answers a
   different question.
2. It belongs in the **integration tier**: that tier's stated seam is "what
   needs a transport", and
   [`test-tier-placement.test.sh`](../../../scripts/tests/test-tier-placement.test.sh)
   requires every test file there to reach one — `websocket.Dial` and
   `setupRelayPair` are both on its list. Keep the tier free of non-test Go
   files, as it is today.
3. Size N so the test is a few seconds, and state the chosen points in a comment
   with the slope tolerance and why it is what it is. A tolerance nobody can
   justify is a flake waiting for a slow machine.
4. **The static guard.** `policy/semgrep/resources/hijacked-request-context.yaml`
   — a handler that calls `websocket.Accept` and then blocks on
   `r.Context().Done()`. This is a genuine
   [ADR-027](../../../docs/adr/ADR-027-adversarial-pentest-precommit-gate.md) subject rather
   than a stretch of one: an attacker who opens and abandons sessions consumes
   the server without bound, which is CWE-400. Follow the house shape of
   [`unchecked-path-new.yaml`](../../../policy/semgrep/paths/unchecked-path-new.yaml):
   an `og-` id, a header comment naming the gap class and stating where the
   pattern is approximate, and `metadata` carrying category, CWE and confidence.
5. **Both fixtures, in the same commit.** `pentest-review.test.sh` materialises
   a vulnerable and a safe fixture per rule and asserts the rule fires on one
   and stays silent on the other. A rule without both is a rule nobody can trust
   to be silent.
6. **The rule file.** `.claude/rules/resource-conservation.md`, stating what
   §2.7 found: a counter of a resource is not a measurement of it. Wherever the
   product publishes a count it maintains — sessions, connections, slots,
   grants — something must read the resource itself and an invariant must bind
   the two; and a path that acquires a per-connection or per-session resource
   states where it is released, with a gate that proves it. Name what it is
   enforced by, as every other rule file does, and add its row to the
   [`CLAUDE.md`](../../../CLAUDE.md) index table.
7. Add the conservation tier to
   [`Testing.md`](../../../docs/infrastructure/Testing.md) beside the existing
   layers, so the next person looking for "where does a resource test go" finds
   an answer.

**Acceptance:** the conservation test fails on the pre-WS1 code and passes
after; the semgrep rule fires on its vulnerable fixture and is silent on its
safe one; `make shell-test` and the pen-test gate are green; the rule file is
indexed in `CLAUDE.md`.

---

## 5. Sequencing

```
WS5.1 (process_start_time alert)   ── LANDED 086592bc
WS1  (the fix, + D5)               ── LANDED 2c30734b
WS8  (the gate and the rule)       ── LANDED 04bbe777
WS2  (liveness + teardown order)   ── LANDED 54baf891
─────────────────────────────────────────────────────────────────────────────
WS4, WS3, WS5.2-5, WS6, WS7        ── ONE commit, one push, plus the records
                                      and this plan's archival
```

**The remaining five land together, not one at a time.** That supersedes the
one-PR-per-workstream shape the rest of this section describes. They are still
independent of each other, so the order they are *written* in is free; what
changed is that they are committed once. Two consequences worth planning for:

- The gauntlet runs once instead of five times, which is the point — it is
  roughly twenty minutes a commit, and every failure costs a whole run. Do the
  per-language checks (`go test ./...`, `make shell-test`, the pen-test gate,
  `pmat tdg` on every changed file under `server/internal/`) **before** the
  commit attempt rather than discovering them inside it.
- One commit means one failure surface. WS6 is the one that can break other
  workstreams' gates — it moves `/metrics` to a second listener and four call
  sites move with it — so write it first among the five and let the others
  settle on top of it.

The four already-landed workstreams were sequenced deliberately, and the reason
is worth keeping: WS8 landed third because its test is WS1's test generalised,
so writing it while that reasoning was still in hand cost a fraction of what it
would have cost later — and until it existed, nothing stopped the same shape
appearing in the next long-lived connection path.

**Interim mitigation: not needed, not taken.** Restarting the staging server at
the start of each load run was held in reserve as a stopgap. WS1 landed before
any load run needed it, so no stopgap was ever added and there is none to
remove. The other half of that note still stands for anyone tempted: do **not**
raise the 384Mi limit — an unbounded leak defeats any ceiling, and a larger one
only lengthens the fuse.

---

## 6. Records to write

| Artefact | Content |
|---|---|
| **ADR-093** — **WRITTEN** (`2c30734b`, extended by `54baf891`) | A relay session's lifetime is owned by the relay, not by a hijacked request context, and its peers are proved alive rather than assumed. Context: `websocket.Accept` hijacks, so the request context is never cancelled — not by a client hangup and not by `Server.Shutdown`, because a hijacked connection is untracked. The happy path leaked and the failure path did not. Record both rejected alternatives: deleting the wait (WS1) and a hard session-duration cap (WS2). |
| **ADR-094** — **owed** | A load run records what its target was holding: a run whose target was replaced mid-run is invalid, not failed, and a run whose target did not give back what it took is failed. The sibling of ADR-090, and the process-side counterpart of `CleanupProof`. |
| **ADR-095** — **owed** | The server has two listeners: what the world may reach, and what only the cluster may. Context: SPA serving moved into the binary and collapsed three edge routes into one catch-all, so the boundary two comments asserted had stopped existing. |
| **ADR-096** — **WRITTEN** (`04bbe777`) | A counter of a resource is not a measurement of it. Context: every liveness number the server published returned to zero correctly while the process held 7,455 goroutines, because each is maintained by the teardown path rather than read from the resource. The sibling of ADR-091's read-back doctrine and ADR-088's *a gate measures the system, not its own harness*. |
| [`decisions.md`](../../decisions.md) | one row each, ≤200 characters of prose. Rows 093 and 096 are **in**; 094 and 095 are owed. |
| [`phases.md`](../../phases.md) | Completed row on landing, linking `plans/archive/…`. **Not yet written** — it lands with the final commit, because the consistency gate refuses a Completed row whose plan is not archived. |
| [`techdebt.md`](../../techdebt.md) | **No entry. Nothing in this plan is deferred** — D1 through D10, N1 and N2 are each fixed by a workstream above. If a workstream is dropped during implementation, that is the moment an entry is owed, with its pay-down trigger. |

**Archive this plan in the same commit that lands the final workstream** —
`git mv` into `plans/archive/`, bump every internal link one `../` deeper,
re-stage the new path, and repoint the `phases.md` row. The consistency gate
refuses a Completed row pointing at a non-archived plan. Since the remaining
five workstreams are now one commit, that is the commit: records, archival and
`phases.md` row all go in with them.

Note the re-staging trap the project has been bitten by before: `git mv` stages
the file's **pre-edit** content, so after bumping the links you must `git add`
the **new** path or the commit carries the old links. `GO111MODULE=off go run
./scripts/check-doc-links` does not catch it, because the link checker
deliberately does not scan `.claude/plans/**` as a source.

---

## 7. Reviewer checklist

Boxes are ticked where the landed half satisfied them. An unticked box is either
owed by the remaining commit or has not been re-verified since.

- [x] A failing test preceded every source change (branch history shows it).
- [x] `registerAndWait` waits on the session's own done channel and a
      **server-lifetime** context — never on `r.Context()` — and closes its
      websocket with `CloseNow` on the way out.
- [x] The session done channel is closed exactly once on **both** teardown
      paths — `pipe`'s defer and `Unregister`.
- [x] The leak test is a **slope** across several session counts, not a fixed
      `NumGoroutine()` baseline, and it fails on the pre-fix code.
- [x] `Shutdown` ends live relay sessions instead of returning immediately.
- [x] A relay write carries a deadline; a stalled peer ends the session within a
      stated ping budget; no hard session-duration cap was added.
- [x] `pipe`'s teardown updates its books **before** it waits on the network,
      matching `Unregister`, and closes the two sides concurrently.
- [x] `holdOpen` fails on a peer that dies mid-hold.
- [x] A mid-run target restart classifies **invalid**; a target that did not
      give its goroutines back classifies **failed**; both reasons name the
      figure. The Go verdict and the shell completeness gate agree.
- [x] The four `/metrics` call sites moved together, and the three previously
      unguarded ones each gained a gate in the same commit.
- [x] The smoke test's `--domain` branch **asserts the boundary** instead of
      skipping, and CD runs it.
- [x] pprof is absent from the public listener; the pen-test gate agrees.
- [x] The cAdvisor job, the ClusterRole grant, and all three alerts each have a
      gate landing in the same commit.
- [x] No map in the process has an eviction path with no caller; `auditLog`
      holds a bound and counts what it sheds.
- [x] The conservation test lives in the integration tier, fails on the pre-fix
      code, and states its tolerance and why.
- [x] The semgrep rule ships with **both** fixtures — one it fires on, one it is
      silent on.
- [x] `.claude/rules/resource-conservation.md` exists, names what enforces it,
      and has its row in the `CLAUDE.md` index.
- [x] No new non-test `.go` file under `server/` without its
      [mutation-shards.sh](../../../scripts/lib/mutation-shards.sh) entry
      (`tests/loadtest` is already a `dir:` shard; `internal/app` is not — check).
- [x] Any new `scripts/tests/*.test.sh` is `100755`.
- [x] The stopgap restart, if it was added, is gone. *(None was added.)*
- [x] No doc or comment narrates the removed behaviour — say what the code does
      now ([`docs-live-state.md`](../../rules/docs-live-state.md)).

---

## 8. Out of scope

- Raising the staging memory limit as a fix (§5).
- A hard cap on relay session duration (WS2 step 6) and `CloseNow` everywhere in
  teardown (WS2 step 9) — both rejected on their merits, with the reasons
  recorded in ADR-093 rather than left as omissions.
- Reconnect logic in the QUIC harness. A harness that silently re-dials would
  have hidden this run's severance just as thoroughly as the false green did;
  WS3 makes the severance loud instead.
- Migrating `nhooyr.io/websocket` to its `coder/websocket` successor. Every
  behaviour measured in §2.5–§2.6 — the 5-second handshake, the per-connection
  timeout goroutine, the deadline-free write — is documented, correct library
  behaviour being used without a deadline by us. The bug is ours, so the
  migration is its own decision on its own evidence.
- Extending the conservation test beyond the relay in this plan. WS8 builds the
  method and the rule; pointing it at the agent QUIC path and the MPS path is
  the rule's job on its own schedule, not a defect this incident found.
- kube-state-metrics. cAdvisor plus `process_start_time_seconds` covers
  everything this incident needed; a second exporter is not yet earned.
