---
number: 127
title: A session moves to a direct link when the networks allow, and loses nothing moving back
---

# ADR-127 — A session moves to a direct link when the networks allow, and loses nothing moving back

## Context

Every remote session ran through the server's relay: browser to server to host,
over two WebSockets. A technician in the same building as a host paid two trips
to the cluster for every keystroke — about 86 ms from the maintainer's
workstation to San Jose — and a 2 GB log bundle crossed the cluster and sat whole
in both the host's memory and the tab's. The pieces of a direct WebRTC link
existed on all three sides and had never been joined: nothing started an
attempt, neither side sent on the link, the hand-off could not finish, and the
documentation described it as working.

Three defects sat beside it. Test hosts never joined sessions on the compose
stack or on staging, because the server built the host's relay address from the
browser's request. Two technicians could type into one host at once, and the
device page listed every session's token to every member
([ADR-059](./ADR-059-session-row-lifecycle.md) holds the fix). A directory
listing was one frame with no limit, so a folder of 51 000–84 000 entries ended
the session at the relay's 4 MiB read limit; a download read its whole file
into memory before its first piece; and one queue at the host put a terminal
echo behind up to 16 MiB of file data — 13 s at 10 Mbit/s.

## Decision

**A session tries once to move onto a direct link, and the relay stays open for
its whole life.** Once its relay connects, the session tab offers a WebRTC peer
connection carrying one ordered data channel. The offer, the answer and the
candidates travel on the relay, the session is usable on the relay from the
first moment, and closing the relay ends the session. A session changes path at
most twice — relay to Direct, Direct back to the relay — so a spotty network
cannot bounce it; a technician who wants Direct back opens a new session. An
attempt that has not switched within
[`ATTEMPT_BUDGET_MS`](../../agent/crates/mesh-protocol/src/direct/handoff.rs)
closes the link on both sides. On the test stack the switch takes 219–227 ms on
one network and 417–423 ms through its routers, and an attempt with no route ends
on its own after 5.6–7.4 s, so the budget sits above both.

**The server is trusted for who is on the link, and stays out of what it
carries.** The link is encrypted between the peers, and each side's certificate
fingerprint arrives through the signed-in relay. The server already reads every
relayed frame and issues the agent's identity, so no binding independent of it
exists: an honest server never sees a direct session's bytes, and a compromised
one could substitute fingerprints, as it can already read the relay.

**What each side learns is stated, not switched.** The browser learns the
host's local addresses, already shown in its hardware inventory, and its public
address. The host learns the technician's public address — often a home address,
and through a split-tunnel VPN or an HTTP proxy one their web traffic would not
show (RFC 8828). Every session attempts Direct; no setting keeps one off it. Logs
name an address's kind — local, public, learned from the peer — and never the
address.

**The host checks a bounded number of addresses, not a chosen set.** It accepts
at most [`MAX_REMOTE_CANDIDATES`](../../agent/crates/mesh-protocol/src/direct/candidates.rs)
per link and ignores any candidate naming a port below `LOWEST_CANDIDATE_PORT`,
in trickled candidates and in the offer alike. A session holder can make the host
send connection checks to any address on its network and look up any `.local`
name: the host's library resolves Chromium's random `.local` names, which is what
lets two machines on one network meet. A session holder already reaches that
network from a terminal, since a session is granted every permission it asks
for; the rule is revisited when a role without terminal access exists.

**The helper is ours, on the port hosts already use.**
[`stunhelper`](../../server/internal/stunhelper/helper.go) answers well-formed
STUN Binding requests of at most 548 bytes on UDP 9090, beside the agents' QUIC
([ADR-037](./ADR-037-quic-transport.md) holds the sharing condition). It answers
a set number per second from one address and a set number in all, its
per-address counts living in a fixed table, so a flood from forged addresses
cannot grow its memory; everything else is dropped unanswered. The helper list
is a server setting, `OPENGATE_ICE_SERVERS`, sent to the browser in the
create-session response and to the host in `SessionRequest`; the host takes its
helpers and its relay address from the server only. Production names the
public QUIC host. Staging names its in-cluster service, so the load runs there
carry the helper's traffic beside the fleet's; browsers on staging stay on the
relay. A production deploy asks the helper which address the runner comes from
and compares it with what the edge saw
([`helper-probe.py`](../../deploy/scripts/helper-probe.py)).

**Link-control travels apart from session data.** Six relay messages set up,
switch and drop the link — `DirectOffer`, `DirectAnswer`, `IceCandidate`,
`DirectRefused`, `DirectSwitch`, `DirectFallback` — and are never numbered or
held. Every `IceCandidate` names the media section of the browser's offer:
Firefox refuses a candidate naming none, and behind two routers a browser
reaches the host only through the host's candidates. Every other frame is session data: numbered on the link, kept by its
sender until confirmed, and re-sent through the relay after a fallback, so in
each direction every frame arrives exactly once and in order across any change
of path. A side moves to the link by sending `DirectSwitch` as its last
session-data frame on the relay; its peer delivers link frames only after that
notice. After a fallback, a side sends its kept copies above the count the peer
reported in `DirectFallback`, then everything new. A peer whose fallback notice
does not arrive within `PEER_FALLBACK_WAIT_MS` ends the session with "Connection
lost — reconnect": re-sending without knowing what arrived could repeat an Enter.

**One hand-off, written twice, held equal by one script.** The hand-off is a
pure state machine in each language — events in, instructions out, no I/O —
[`handoff.rs`](../../agent/crates/mesh-protocol/src/direct/handoff.rs) on the host
and [`handoff.ts`](../../web/src/lib/direct/handoff.ts) in the browser. Both
replay every scenario in [`testdata/direct-link/`](../../testdata/direct-link/)
and 10 000 random drop points each. A single Rust copy compiled for the browser
was weighed and rejected: every web job, the image build, the version manifest
and the daily dependency updates would change with it.

**Confirmations count bytes, inside a 4 MiB window.** A receiver confirms with
`DirectAck` once a quarter of the window has been delivered, or `ACK_DELAY_MS`
after the first unconfirmed frame, whichever comes first, so throughput never
waits on a timer and a background tab downloads at full speed. A sender numbers
nothing new while more than
[`WINDOW_BYTES`](../../agent/crates/mesh-protocol/src/direct/handoff.rs) is
unconfirmed. At 100 Mbit/s and 40 ms the window never binds: the link ran at the
full shaped rate with under 2 MiB unconfirmed.

**Silence ends a link; slowness does not.** The host checks its peer every
second and calls the link silent after three unanswered checks
([`peer.rs`](../../agent/crates/mesh-agent-core/src/link/peer.rs)), so it notices
a dead link first and its `DirectFallback` moves the browser too. The browser
treats its own `disconnected` as passing and falls back only if the link is not
connected again `DISCONNECTED_LIMIT_MS` later, or at once on `failed`. A shorter
silence changes nothing — the link's own retransmission delivers every frame late
and in order — and a slow or frozen page keeps answering checks, so the window
holds its sender instead. Measured in all three browsers: an idle link for 90 s,
a page busy for 30 s and 1 s cuts all stayed Direct, and so did a tab Chromium
froze for 30 s; a cut moved the session to the relay 2.2–2.9 s after it, once in
about twenty runs 4.7 s in Firefox, the page 5–15 ms behind the host. Falling back on slow data,
trying Direct again after a delay, and a "try direct again" button were weighed
and rejected: the first fires on a frozen receiver and on the lossy hop both
paths share, and the others bounce a session on a bad network or reopen a second
link with its numbering.

**Two lanes at every sending point.** Each side keeps an interactive lane —
terminal, control messages, notices — and a bulk lane for file data, always takes
the next interactive frame first, and numbers a frame only once chosen, so the
numbered sequence stays the one ordered stream. Each lane has a cap enforced
where a frame is produced
([`outbox.rs`](../../agent/crates/mesh-agent-core/src/session/outbox.rs),
[`session-channel.ts`](../../web/src/lib/transport/session-channel.ts)): an
interactive frame fits one 16 KiB piece and the lane queues at most 256 KiB; a
bulk frame is one 256 KiB file piece and the lane holds two. A frame over its
lane's cap is refused with an error and never sent. A directory listing goes out
in pages of at most 16 KiB, each a `FileListResponse` with `more` set until the
last, and the session tab draws only the rows in view: a 120 000-entry folder had
stalled the tab for 73 s and cut a running download to 18% of the line, and now
lists in 2.5 s at 100 Mbit/s with no stall over 110 ms. On the link, a frame is
cut into pieces of at most 16 KiB (RFC 8831 §6.6) sent back to back, and its own
length header marks its end. Moving listings to the bulk lane was weighed and
rejected — browsing would wait behind every download — and so were pausing
technician input for a transfer, smaller file frames, which multiply the relay's
messages per byte, and two ordered channels, which split the numbering.

**The link's send limit is set per link at run time.** The host library counts
bytes in flight in its send buffer, and no fixed limit serves both rates: one
sized for 100 Mbit/s gave a 95th-percentile echo of 695 ms at 10 Mbit/s, and one
sized for 10 Mbit/s held a 100 Mbit/s path to 22%. The host sets the limit from
[`pacing.rs`](../../agent/crates/mesh-agent-core/src/link/pacing.rs): the delivery
rate its confirmations show over the last second, times the least round trip
sampled while the sender was not held at its limit, plus `UNSENT_CUSHION`; raised
by a quarter while it stays full, between `FLOOR` and the window. A round trip
sampled while the sender is held carries its own queue, and a rate read per tick
from 1 MiB confirmation steps comes out in steps, so both estimates are taken
this way. The browser sends while the channel's `bufferedAmount` is under
`UNSENT_LIMIT`.

**The relay socket is held the same way.** The host feeds its relay socket only
while the bytes the peer has not acknowledged are under a limit computed from
the delivered bytes and least round trip the socket reports, and keeps at most
`RELAY_UNSENT_LIMIT` unsent
([`relay.rs`](../../agent/crates/mesh-agent-core/src/session/relay.rs)). It waits
after a write, so the next bulk frame stays in the lanes where typing can
overtake it. Unheld, the operating system sent until the router's queue was full:
0.47 s, 1.1 s and 3.3 s of echo at 10 Mbit/s for routers queuing 100, 300 and
1 000 packets. The hold covers the host's side; the technician's side of the
relay is the server's sending.

**The echo promise.** During a download over Direct at a 40 ms round trip, a
terminal echo comes back within 350 ms at 10 Mbit/s and within 100 ms at
100 Mbit/s, and through the server to the same figures. Idle echo is
already 50–58 ms on the stack, since the page draws the character after the trip,
and one 256 KiB piece ahead adds 23 ms at 100 Mbit/s. Smaller pieces over Direct
alone, to promise 80 ms, were weighed and rejected. Measured over three runs of
120 samples each, with a 120 000-entry listing paging in beside the download:

| Path | 10 Mbit/s echo, 95th percentile | 100 Mbit/s echo, 95th percentile | Download, share of the line |
|---|---|---|---|
| Direct | 275–286 ms | 78–89 ms | 86–92% |
| Through server | 254–276 ms | 82–83 ms | 96–97% |

**The speed tests take enough samples to hold the promise as written.** At
100 Mbit/s the readings sit close to their limits, and over twenty runs of
thirty samples two landed past them: 102 ms through the server, and a Direct
download at 77% of the line. The tests keep the limits and take 120 echo samples
a run, so the 95th percentile passes its limit only when seven echoes are slow
where two did among thirty, and read the download's rate over at least 30 s,
measured in the page as the last piece
arrives ([`performance.spec.ts`](../../web/e2e/performance.spec.ts)). A test
margin wider than the promise, and accepting the odd red night, were weighed and
rejected.

**The speed tests run nightly, in a workflow of their own.** Taking enough
samples makes them slow: the seven speed tests took about nine of the fourteen
minutes of the end-to-end run every push waited on, and a reading near its limit
turned a commit red on a shared runner's noise. They run nightly and on dispatch
in [`e2e-performance.yml`](../../.github/workflows/e2e-performance.yml), in
Chromium, where their budgets were measured, and alert when the run does not
pass. Each records its readings beside their limits, which the run's summary
lays out ([`e2e-summarize.sh`](../../scripts/e2e-summarize.sh)). Every other run
leaves them out, held by
[`e2e-speed-nightly.test.sh`](../../scripts/tests/e2e-speed-nightly.test.sh).

**Downloads stream into the browser's Downloads, one at a time.** The host reads
a file one 256 KiB piece at a time as the bulk lane drains, and a failed read
ends the download with `FileDownloadError`. The page passes pieces as they arrive
to the service worker ([`sw.js`](../../web/public/sw.js)), which hands the browser
an ordinary download; the page holds a few pieces, never the file. The worker
answers the page before the download's frame opens, encodes every character a
file name parameter reserves — Chromium refuses a raw quote or parenthesis and
names the file after the download — and forbids sniffing, since WebKit otherwise
read the stream as a page and renamed it `.html`. The page messages the worker
every 10 s while a download runs: without it Firefox saved a truncated file and
reported success. All three browsers saved a 160 MiB download running six and a
half minutes, and one held still for 20, 45 and 90 s, whole and matching its
source. A page with no controlling worker
([`download-saver.ts`](../../web/src/features/file-manager/download-saver.ts))
holds up to `BUFFERED_LIMIT` in memory and refuses a larger file with a message.
A session runs one download at a time: the session tab queues the next request,
View included, and shows it waiting, and the host refuses a second with
`FileDownloadError`. A transfer number on every file frame, for parallel
downloads, was weighed and rejected — a protocol change in three languages for
no gain in speed, since parallel downloads share one link.

**A download resumes within its session, not after it.** A download survives any
change of path at the exact piece, and a session that ends ends its download. A
browser's own download manager resumes only a response carrying a size and a
fingerprint and honouring byte ranges (RFC 9110 §13.1.5, §14), and a
service-worker download carries neither. TeamViewer continues a half-sent file
when it is sent again; RustDesk keeps a partial file beside a digest of its
source and continues at the block where it stopped; ScreenConnect has no resume;
MeshCentral streams through its server with no byte ranges. Weighed and
rejected: keeping the partial file in the browser's private storage (a resume of
our own beside the standard one, twice the disk, hidden progress, storage the
browser may evict); a session waiting minutes for a side to return; and every
download served by the server with byte ranges, which resumes after anything but
sends every byte across the cluster and loses local-network speed.

**The fleet counts how attempts end.** The session tab reports each outcome as it
happens — Direct, no direct route, this browser can't, the link dropped — and the
server counts them in `opengate_direct_link_outcomes_total`, labelled by outcome
alone, with no address, host, customer or technician. Only the session's holder
may report, and a session counts each outcome once. The overview dashboard's
"Direct links" panel shows the Direct share and the drops, so a browser update
that stops Direct forming anywhere shows by the next morning.

**The session bar names the path.** It reads Direct or Through server, and
hovering gives the reason in plain words: this browser can't, no direct route on
this network, the direct link dropped.

**The session tab owns the session.** Open session opens a tab at once at
`/devices/{id}/session`, named for its host; the tab creates the session, reads
the relay address and helper list from the response, needs nothing from the page
that opened it, and keeps the token out of the address bar. A reload starts a new
session, which replaces the old. Go to session asks the named tab forward and
then reads whether the page gave up focus within half a second
([`session-tab.ts`](../../web/src/features/session/session-tab.ts)): Chromium
and Firefox bring the tab forward, WebKit finds it and leaves it behind, and a
desktop can refuse to switch windows, so where focus stays the page says "Open in
another tab of this browser". Naming the browser from its user agent was weighed
and rejected: it misses the desktop case and needs a change per browser.

**The relay speaks through `github.com/coder/websocket`.** A relay close carries
its reason — "Opened elsewhere", "Ended by <name>" — to the session tab, and the
tab shows "Connection lost — reconnect" for a close that carries none.
`nhooyr.io/websocket`, its predecessor, answered the peer's close echo with a
second close frame, and a browser fails a connection on a second close, so no
reason ever reached a tab. `TestWSConn_EndWritesOneCloseFrame` counts the close
frames on the wire.

**Tested behind two routers on the compose stack.** The stack carries an
"internet" network and isolated office and home networks, each behind a router
container that translates addresses, limits the rate and drops UDP on command
([`router.sh`](../../deploy/router/router.sh)); `agent-a` sits behind the office
router, and a browser server behind the home router. Only the routers hold
network-admin rights ([ADR-055](./ADR-055-fault-injection.md)). The browser
server's image takes its Playwright version from the web lockfile, so the remote
browser always matches the suite's own Playwright. Each machine is held to one
CPU: a sender moving between CPUs put its packets in different per-CPU queues on
the test host, which delivered them up to 19 places out of order, and the link's
congestion control read three reports of a gap as loss and halved its window
six to ten times a run, holding Direct at 47–70% of a 100 Mbit/s line. Field
paths reorder too — Wi-Fi, an office on two internet lines — and Direct's rate
falls when one does.

**Out-of-order packets are measured, and the promise holds at one in a hundred.**
The office router reorders a set share of packets on command, and a speed case
reads Direct's share of the line and the bytes it sent beyond the file at 0%, 1%
and 5% reordering, at 10 and 100 Mbit/s and a 40 ms round trip, held to the 80%
download promise at 1% and read at 5%
([`performance.spec.ts`](../../web/e2e/performance.spec.ts)). Over two runs at
10 Mbit/s and one at 100:

| Line | 0% reordered | 1% reordered | 5% reordered |
|---|---|---|---|
| 10 Mbit/s | 92% of the line | 87–88% | 52–54% |
| 100 Mbit/s | 88% | 17% | past the five-minute limit |

The 100 Mbit/s case at 1% fails the promise, and the 5% case there runs past
the test's time limit; the pass mark stays where it is, so the speed nightly
reports both until the cause is fixed. The host's link library declares a packet lost once three reports show a gap
behind it, with no allowance for reordering, no setting for one and no counters
it publishes, and `rtc-sctp` 0.21.1 is its newest release; its pacing takes no
resend input of its own, so the cut is the library's. Closing it needs a change
inside that library — a reorder allowance, or loss detection by time as RACK
does — so the test machines stay held to one processor each until it lands. The stack publishes
the console on IPv4 alone: Firefox, with no camera or microphone permission,
gathers addresses only on the interface it takes as the page's default, and for a
page reached at `localhost` over IPv6 loopback that is the loopback interface,
whose addresses it ignores, so it offered none and never formed a link. A
technician never opens the console over loopback. A nightly
production canary was weighed and rejected: the two routers and the fleet's
outcome counts are the evidence.

## Consequences

- Typing over Direct waits one trip between technician and host; a technician in
  the host's building waits about a millisecond.
- A direct session's bytes never cross the server, and the relay-only case is
  the server's worst: every session whose routers block Direct lands there, so
  capacity is sized as if none went Direct. Load, performance and soak runs carry
  what Direct adds to the server — the helper's answers on the fleet's port — and
  hold every helper request answered.
- In the largest public measurement about 70% of attempts across two routers
  form a direct route; the rest stay on the relay with "no direct route on this
  network".
- Memory per session on each side stays under 13 MiB: the window of kept copies,
  both lanes, one frame in reassembly and the window held before a switch notice.
  A download adds nothing that grows with the file.
- A server rollback stops every attempt, since the browser starts each one and
  the server serves the browser.
- Staging proves the relay only: it has no route to break and no permission to
  break one.
