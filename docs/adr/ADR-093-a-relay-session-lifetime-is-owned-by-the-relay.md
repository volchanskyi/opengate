---
number: 93
title: A relay session's lifetime is owned by the relay, not by a hijacked request context
status: Accepted
date: 2026-09-02
---

# ADR-093 — A relay session's lifetime is owned by the relay, not by a hijacked request context

## Context

A staging server walked from 29 MiB to an out-of-memory kill across four nightly
load runs — 29 → 134 → 230 → 334 MiB against a 384Mi limit — and took a load run
down with it. It sat at 7,148 goroutines and 344 MiB for three hours with zero
agents connected and no CPU load, and every liveness number it published said it
was idle.

The relay's WebSocket handler ended by blocking on `<-r.Context().Done()`. That
context belongs to a connection `websocket.Accept` has already hijacked, and
`net/http` stops managing a hijacked connection in three separate ways, each of
which was read in the toolchain source rather than inferred:

- `w.cancelCtx()` runs **after** `ServeHTTP` returns, which is circular for a
  handler blocked inside it.
- The hijack calls `abortPendingRead`, which stops the background read that is
  the only thing turning a client hangup into a cancellation.
- `setState(rwc, StateHijacked)` calls `trackConn(c, false)`. The connection is
  untracked, so `Server.Shutdown` neither waits for it nor closes it, and
  `Server.Close` cannot reach it either.

So the context was a select branch that could never fire. Measured against
twenty clients connecting and hanging up: twenty handlers entered, **zero**
returned, 2.05 goroutines and 15,983 bytes retained per connection. Every relay
session opens two of those handlers — the operator's and the machine's — so a
completed session stranded two goroutines forever. In production that measured
1,183 sessions to +2,232 goroutines and +76 MiB.

Two properties of the defect are worth stating because they are the reverse of
where anyone looks. **Only the happy path leaked**: a side whose peer never
arrives hits the peer-wait timeout, returns, and is clean; only a successful,
fully piped session leaked. And **the shutdown path reported a drain it never
performed** — measured with five hijacked handlers still blocked, `Shutdown`
returned in zero seconds with a nil error and none of the five contexts fired.
On every rolling deploy each live relay session was severed by process exit with
no close frame. That is the failure class
[`ci-cd-determinism.md`](../../.claude/rules/ci-cd-determinism.md) already rules
against — a step whose work was refused must not report success — living in the
product rather than in CI.

Nothing caught it. A copy of the tree with the wait deleted outright passed every
test in all three tiers whose name mentions the relay, in the same time or
faster. Not one assertion in 1,519 Go test functions held that line.

## Decision

**The relay owns its sessions' lifetime, and says when one has ended.** A session
carries a `done` channel closed exactly once, by whichever of its two teardown
owners runs: the pipe's defer for a pair that reached the pipe, and `Unregister`
for a side whose peer never arrived.

**`Register` hands that channel back to the caller that registered.** Not a
`SessionDone(token)` lookup: teardown deletes the token, so a lookup races the
very event the caller is asking to be told about.

**The handler parks on that channel and on a server-lifetime context — never on
the request context.** The lifetime context is the one `app.Build` already
receives; it simply was not carried into the API server before. Keeping the
request context as a third branch would encode a belief that a client hangup or
a shutdown is handled there, and the Context section is the proof that neither
is.

**The handler closes its own WebSocket on the way out**, with `CloseNow`.
`net/http` will not close a hijacked connection for a handler that returns, so
nothing else can; `CloseNow` rather than `Close` because a graceful close waits
five seconds for an acknowledgement that, on this path, has either already been
sent or never will be. It is deferred first so it runs last, after the relay's
own teardown has had its graceful close.

**Shutdown waits for the relay to drain.** `Relay.WaitForDrain` reads the relay's
own active count under the shutdown budget, and the process waits on it *before*
`httpSrv.Shutdown`, which cannot see a hijacked connection at all.

**A session proves its peer is alive rather than assuming it.** The parked
handler pings its own connection on an interval and requires the pong back
inside it. That catches both the peer that vanished and the peer that is present
and no longer consuming — the frame carries its own deadline, and the answer has
to arrive. One unanswered ping ends the session: a peer that cannot answer
inside a whole interval is not answering. TCP keep-alive does not reach this
case, because the socket is alive.

**A forwarded frame carries a deadline; a read does not.** A peer that stops
draining lets the buffers fill, and the forwarding write then blocks with no
error for as long as that peer stays connected — measured still blocked after
six seconds. A read stays undeadlined on purpose: a quiet session is legitimate,
and a read deadline would end a technician's session watching a static screen.
Liveness is the ping's job, not the read's.

**Teardown books out before it waits on the network.** `closeBoth` performs
graceful closes that each wait up to five seconds for an acknowledgement an
absent peer never sends — measured at 5.005 s for one and 10.005 s for the
sequential pair. The pipe did that work *first*, so for up to ten seconds after
a session was over the gauge still counted it, `ActiveTokens` still reported it
live — which the stale-session sweep reads as "in use", so the row could not be
collected — and the registry still held it. `Unregister` already did the
opposite and stated the principle in a comment; the pipe is now held to the same
order, and the two sides are closed **concurrently**, which bounds the bad case
at one handshake instead of two.

**Both pipe directions are waited for.** Only one of the two copiers ends a
session; the other is unblocked by the close. That was not a leak, but the
asymmetry was one edit away from becoming one.

### Alternatives considered

**Delete the wait outright.** A handler that returns immediately after pairing is
provably leak-free: `net/http` will not close the hijacked connection, so the
relay keeps piping through the still-open socket. This is exactly why the mutant
with the line deleted stayed green. Rejected because the handler's own
`relay session disconnected` log line would then fire at pairing time rather than
at session end, and because nothing would be left holding the socket to close it.

**A hard cap on relay session duration.** A remote session a technician
legitimately holds open for an hour is not a defect, and a cap would make it one.
The ping is the liveness answer; a cap is a product regression wearing a fix's
clothes.

**`CloseNow` everywhere in teardown.** It would be zero seconds always, and it
was rejected: it discards the `1000 Normal Closure` that tells an operator's
browser the session *ended* rather than *broke*. Booking out first and closing
the two sides concurrently gets the books honest immediately and halves the
remaining wait, which is what actually mattered. `CloseNow` is used only where
there is no acknowledgement left to wait for — the handler's own backstop close
on a connection `net/http` will not close.

## Consequences

A completed relay session gives back what it took: the slope of retained
goroutines against completed sessions is zero across three load points, where it
was exactly 2.000 before. The handler's lifetime now equals the session's, so its
disconnect log line means what it says, and a rolling deploy ends live sessions
rather than severing them.

`Register` returns two values where it returned one, which every caller and test
had to be adapted to — a deliberate cost, taken because the alternative shape
races teardown.

The ping is only meaningful because the relay is already reading each side: a
pong is delivered through the connection's own read loop, so a side nothing
reads could send a ping and never see the answer. The pipe reads both sides for
the life of the session, which is what makes the question askable at all.

The test that holds this is a **slope** rather than a `NumGoroutine` baseline.
Every `NewServer` starts immortal sweeper goroutines that take no context, and
the store and its pool add more; those are a constant, which a slope removes and
a baseline cannot. That the suite had no such assertion anywhere — no
`runtime.NumGoroutine`, no `goleak`, no `ReadMemStats`, in any language at any
tier — is the wider finding, and it is
[ADR-096](ADR-096-a-counter-of-a-resource-is-not-a-measurement-of-it.md)'s
subject.
