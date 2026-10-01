# Non-Functional Testing

- [Load and soak tests](#load-and-soak-tests)
  - [Profiles and evidence](#profiles-and-evidence)
  - [The seven families](#the-seven-families)
    - [Finding what leaked](#finding-what-leaked)
    - [Opening a soak dump](#opening-a-soak-dump)
  - [k6 scenarios](#k6-scenarios)
    - [Load comes from the profile](#load-comes-from-the-profile)
    - [One address per technician](#one-address-per-technician)
  - [QUIC harness](#quic-harness)
  - [In CI](#in-ci)
- [Leak checks](#leak-checks)
- [Speed checks](#speed-checks)
  - [Benchmarks](#benchmarks)
    - [Running them locally](#running-them-locally)
    - [Regressions](#regressions)
  - [Bundle size](#bundle-size)
  - [Lighthouse](#lighthouse)
- [Fuzzing](#fuzzing)

How the system behaves under load, over a night, against a clock and given
input it was never meant to receive. Whether it gives the right answer is
[Testing](./Testing.md); whether it survives its own parts failing is
[Fault Injection](./Fault-Injection.md).

## Load and soak tests

A run is valid, failed, or **invalid**. Valid and failed are both measurements —
one of a system that held, one of a system that did not — and both belong in the
trend. Invalid is the third: the run did not measure the system, because a
scenario produced no rows, the generator ran out of room, or a safety ceiling
stopped it. An invalid run never enters the trend, because a partial night
absorbed as data lowers the window median and the next genuinely slow night then
compares favourably against it and passes.
[ADR-082](../adr/ADR-082-load-run-validity.md) is
the decision behind that and everything below it.

### Profiles and evidence

A run reads one profile and writes one evidence bundle.

**Profiles** live in [`load/profiles/`](../../load/profiles) and are versioned.
Each declares its family, the environment class it runs in, the fixture it needs,
an ordered phase list, the safety limits that stop it, and the numbers its
results are read against.

Those numbers live in the profile and nowhere else, which
[ADR-101](../adr/ADR-101-load-profiles-and-limits.md) settles. A
measurement carries at most one limit that fails a night, per direction, and any
number of marks that only report — a floor under a collapse and a target
somebody is watching before enforcing are different statements, and a profile
that collapsed them would lose whichever it dropped. Every measurement the
extraction produces is either limited or named in the profile's `ungated:` list
with a stated reason, because the limits the profile took over had a catch-all
and a list without one can lose protection while looking like consolidation.
[`loadtest-gate-check.sh`](../../scripts/loadtest-gate-check.sh) reads them in
the publish step, where the browser-side and machine-side rows have been joined;
[`loadtest-regression-check.sh`](../../scripts/loadtest-regression-check.sh)
keeps the fortnight comparison and holds no numbers of its own. The environment vocabulary has no production member, which is
what makes "production is never a target" a property of the schema rather than of
a reviewer's attention.

The environment also decides which safety limits mean anything. The processor
ceiling is a promise made to whatever else sits on the node, so a staging profile
declares one and a disposable runner stack — created by the job and thrown away
with it — may not: driving the processor is the scaling sweep's whole experiment,
and a ceiling nothing consults reads as protection that is not there. The memory
and disk ceilings hold everywhere, because past them the node has nowhere to put
what the run produces. The schema enforces both halves.

The venue also decides *how* the machine is read, which
[ADR-107](../adr/ADR-107-where-a-run-happens.md) settles.
A run that owns its box is read at the instant it looks, because the minute
before that look is the job's own image build. A run that is a guest on a cluster
node is read over the last minute, because that minute is production going about
its business and is exactly what a ceiling protecting a neighbour asks about — and
because the instant quantises into steps as coarse as the node is small. Which of
the two a run is comes from whether the kernel gives it a processor allowance of
its own, the same question that decides whose room the generator's reading
describes.

**Bundles** are versioned JSON, one per run, uploaded as a workflow artifact. A
bundle carries what produced the numbers, on what hardware, against how much
data, what load was offered, what load arrived, what the run observed, and what
state it left the system in. The metrics store retains 30 days, so the bundle is
authoritative and the dashboard is a view of it; a bundle missing a mandatory
section fails the run rather than entering the trend as a thinner version of a
real one.

Every field in it is a reading or is absent, which is what
[ADR-082](../adr/ADR-082-load-run-validity.md) settles
and what the validation enforces. Both sides of the measurement are recorded
because a latency figure is a property of the pair: the target's own limits are
handed in by whoever started it — the sweep's matrix value, or the cluster's own
container spec through
[`k8s-quantity.sh`](../../scripts/k8s-quantity.sh) — and the generator reads
itself, including how much room it had left. A generator nobody measured
invalidates the run.

That room is bracketed around the load rather than sampled after it, and it names
whose room it is. A generator with an allowance of its own — a cgroup quota, which
the staging load-test pod has — is measured against that allowance, and three
rules fall on the figure: too little processor left, too much memory held, or too
much of the run spent runnable and refused the processor. A generator that shares
a box with the system it measures, which is what the throwaway stack is, has no
allowance to be measured against; its figure is carried as evidence and the
question of whether it offered the load is answered by attainment, which is a
reading of the fleet.

The arrival rate is two pairs, not one, because two processes offer arrivals. The
machine side is the harness's own and is measured from the fleet; the technician
side is the profile's declaration, and its achieved half stays absent until a
browser-side generator fills it. A phase's latency is a live round trip taken
during the phase — a machine that connects, handshakes, registers and hangs up —
because the control stream has no reply to a heartbeat.

Beside a phase's wait times sits how hard the target worked over that phase: its
own processor counter, read off the same page the harness reads registration
timing and goroutine counts from, bracketed around the phase and divided by the
phase's clock and by the processor allowance the target was declared with. What
it states is *the target used this share of what it was given*, which compares
across a rung with half a processor and a rung with four. Without it, a target
that has run out of processor and one that is idle but slow produce the same
wait times, and those are different problems with different fixes. A run that
read the target's own account of itself is held to producing the figure for
every phase, because both come off the one page.

A phase is also an offer, and the fleet is what makes the offer true. A step
going from eight thousand machines to sixteen thousand over five minutes is
offering fifty-three arrivals a second, so the fleet spreads the machines it is
adding across the window it has to add them in rather than dialling them all at
once. The server refuses enrolments past about a hundred a second on purpose, and
a burst past that is turned away and counted as machines that could not arrive —
which reads as a system that could not absorb the load rather than as load that
was never offered. [ADR-082](../adr/ADR-082-load-run-validity.md)
is the decision.

A machine that arrived and a machine that ended cleanly are two facts, and every
count in a bundle says which one it is. The fleet that exists is the machines
that reached registered; a severance is recorded beside that and never inside
it. A summary counting the survivors reported a fleet of 439 in a run that had
filed 10,520 machines under a customer, and took the connect, handshake and
registration series from those 439 alone — which drops the slowest arrivals
first, so a run reads faster the more of its fleet it loses. On a system that
holds the two counts are the same number, which is why only a night that severed
its fleet can tell them apart.

A phase that winds down offers nothing, and that decides what its error rate is
a reading of. An outcome is known when a machine's life ends rather than when it
began, so a dial that started under the level before this one can end under this
one, and a phase reaching for nobody has the tail of the phase before it as
every outcome in its window. The error ceiling therefore falls on a phase that
offered arrivals. Where the question is whether a crushed system came back, the
phase asking it offers them: the generator never replaces a machine it lost —
the gap between the level asked for and the level connected is the finding — so
the ladder empties its fleet and dials the opening load again rather than
holding what the top rung left of it.

The breakpoint family carries one thing the others do not: what counts as giving
out. `gave_out` names an error rate, a wait time, or a share of the target's
processor allowance, and the run reports the last rung that stayed inside all of
them, the first that did not, and the reading that decided it — together with how
many rungs it looked at, so "nothing gave out" cannot be reported by a ladder
that walked nothing. Without a declared definition the family's answer is
whatever the run happened to survive
([ADR-101](../adr/ADR-101-load-profiles-and-limits.md)).

Two of a bundle's readings are taken by steps outside the harness: the fleet's
weight on disk, read from the database once the fleet exists, and the technician
journeys, timed in another pod.
[`loadtest-bundle-merge.sh`](../../scripts/loadtest-bundle-merge.sh) folds those
into the bundle and fails when there is nothing to fold.

It folds one more thing from the same export: how many of the run's requests the
server refused at the door, beside how many it made. The server counts requests
per address, so a night whose presented addresses were not believed spends one
allowance between every virtual user — it fills with refusals, reds the
error-rate gate, and is shaped exactly like a night against a slow server. The
count is what tells a broken test setup from a finding about the product. It is
not free in the export: what k6 publishes about failures is a single pass/fail
rate with no breakdown by status, so the scenarios count it themselves, through
the one request client they all share, and a nought is recorded on every answered
request so a clean night's series exists rather than being absent. The merge
refuses an export that made requests and carries no count of them, and
[`perf-bundle-verdict.sh`](../../scripts/perf-bundle-verdict.sh) prints the share
beside the verdict — with a note, not a gate, past one per cent
([ADR-116](../adr/ADR-116-forwarded-addresses.md)).

Both schemas, their validation and the verdict rules live in
[`server/tests/loadtest/`](../../server/tests/loadtest) and are exercised by that
package's tests.

### The seven families

| Family | Venue | Driven by | Why there |
|---|---|---|---|
| [Normal](../../load/profiles/normal.yaml) | Staging, nightly | [`load-test.yml`](../../.github/workflows/load-test.yml) | the everyday shape on the hardware production runs on, against a server reserving and capped at exactly what production is |
| [Peak](../../load/profiles/peak.yaml) | GitHub-hosted runner, nightly | [`perf-stack.yml`](../../.github/workflows/perf-stack.yml) | the busiest ordinary morning is two thousand machines, and the cluster node has 150 millicores left to reserve |
| [Spike](../../load/profiles/spike.yaml) | GitHub-hosted runner, nightly | [`perf-stack.yml`](../../.github/workflows/perf-stack.yml) | a site whose link came back is the same two thousand, arriving at once |
| [Breakpoint](../../load/profiles/breakpoint.yaml) | GitHub-hosted runner, nightly | [`perf-stack.yml`](../../.github/workflows/perf-stack.yml) | a capacity test that stops short of the capacity measures nothing, and saturating the cluster node throttles production's own probes |
| [Volume 500](../../load/profiles/volume-500.yaml) | GitHub-hosted runner, nightly | [`perf-stack.yml`](../../.github/workflows/perf-stack.yml) | staging's database shares the node root with production; a runner brings its own disk |
| [Volume 2,000](../../load/profiles/volume-2000.yaml) | GitHub-hosted runner, nightly | [`perf-stack.yml`](../../.github/workflows/perf-stack.yml) | the middle point of the sweep: the legs differ in machines enrolled, because the fixture names do not differ in how much data they hold |
| [Volume 8,000](../../load/profiles/volume-8000.yaml) | GitHub-hosted runner, nightly | [`perf-stack.yml`](../../.github/workflows/perf-stack.yml) | the far point, four times the largest fixture any name plans |
| [Scaling](../../load/profiles/scaling.yaml) | GitHub-hosted runner, nightly | [`perf-stack.yml`](../../.github/workflows/perf-stack.yml) | the sweep needs four or five processor points and the cluster offers one |
| [Soak](../../load/profiles/soak.yaml) | GitHub-hosted runner, weekly | [`soak.yml`](../../.github/workflows/soak.yml) | the run owns the machine and destroys it, which is what makes following a held object with a debugger possible at all |

Every row names a workflow that runs it, and
[`scripts/tests/loadtest-family-venue.test.sh`](../../scripts/tests/loadtest-family-venue.test.sh)
holds the table to that in both directions: a profile no workflow names is a
family this page would otherwise describe in the present tense while it never
ran, which the live-state gate structurally cannot see — it looks for past-state
narration, and this is a present-tense claim about something that does not
happen.

Both sweeps are read back, each by a job of its own that downloads every leg and
publishes the curve —
[`perf-scaling-curve.sh`](../../scripts/perf-scaling-curve.sh) over the processor
rungs and [`perf-volume-curve.sh`](../../scripts/perf-volume-curve.sh) over the
estates. Each refuses a sweep that could not measure at all: a leg that measured
nothing, legs holding the same point, fewer legs than a curve needs, or legs that
all came back saying the same thing. Neither refuses a curve that fails to rise,
because one night is one sample per leg and two nights from the same code have
disagreed about the shape.

Each leg also runs a browser-side generator from its own profile beside the
fleet, folds what it timed into its bundle, and its curve refuses a leg that
carries no technician reading. Whatever a family holds constant has to be
something its variable can move, and machines arriving is not: that cost barely
changes with the second processor or with the size of the estate already in the
database, which is how a scaling curve came to be flat from one processor upwards
while every leg looked fine.

Both halves of the profile's technician load, because both are declared and they
are different loads: a rate of journeys is screens opened, and `sessions` is a
count held open. What each sweep publishes is a capacity claim — how many
machines a processor holds, what an estate of a given size costs — and a
technician remoted into a machine is the expensive thing the product does, so a
figure read with nobody watching a screen describes a load that never happens.
The harness answers each session's machine side on these venues for the same
reason it does on the endurance one, and
[`perf-stack.test.sh`](../../scripts/tests/perf-stack.test.sh) holds each family
to offering what its own profiles declare and to folding what it offers, in both
directions.
[ADR-101](../adr/ADR-101-load-profiles-and-limits.md) is
the decision behind all of that, and behind the two things that made the sweeps
readable in the first place: the rungs sit below what the rest of the stack
leaves, so the generator's share no longer shrinks as the server's grows, and the
fleet sits where the rungs can differ at all.

How large a fleet a profile may ask for is a property of its venue, and the
figure is a reading rather than a target:
[`loadtest-venue-ceilings.sh`](../../scripts/lib/loadtest-venue-ceilings.sh)
carries one row per venue with the run that established it, and
[`loadtest-venue-ceiling.test.sh`](../../scripts/tests/loadtest-venue-ceiling.test.sh)
refuses a profile asking past its row. A capacity ladder is the one shape allowed
through, and it identifies itself by declaring `gave_out:`; in exchange it owes a
rung at or below the ceiling and one above, or nothing in it can be named as the
last that held. See [ADR-107](../adr/ADR-107-where-a-run-happens.md).

On the sweep legs the generator runs inside a declared processor and memory
allowance of its own
([`loadtest-generator-share.sh`](../../scripts/loadtest-generator-share.sh)), so
the stack's four consumers are four declarations rather than three and a
remainder. A machine that cannot grant one says so and the run goes ahead
unbounded, and the bundle's headroom scope is what says which of the two
happened. The endurance run takes the remainder instead: its stack's three
services declare well under half the runner's processors between them and the
load offered beside its fleet is small, so what is left over is far more than
the harness asks for — and the bundle's headroom is the reading that would say
otherwise rather than an assumption that it cannot happen.

A runner is x86_64 and production is ARM64, so every family but the first
produces comparisons — between fixture sizes, between processor counts, between
one night and the next — and never an absolute capacity claim about production
hardware. Staging is the one venue on the hardware production actually runs on,
which is what its row is for. The runner families' stack is
[`deploy/docker-compose.perf.yml`](../../deploy/docker-compose.perf.yml), and the
volume family also weighs the fixture it built with
[`scripts/perf-weigh-fixture.sh`](../../scripts/perf-weigh-fixture.sh): the
database's size and the series the fleet occupies in the stack's metrics store,
each against the empty stack. The harness's own reader and the merge are both
tested against a weighing that script wrote on the stack, held to the script's
current shape.

On every runner leg the machine-facing path is production's
([`perf-stack-quic.sh`](../../scripts/perf-stack-quic.sh)): the runner is given
the receive-buffer ceilings production's node has before the stack starts, and
the generator dials the name on the server's certificate, mapped to the
container's own address, rather than the published port Docker relays. A run
either end of which still got less receive buffer than the transport asked for
fails. Each phase carries the generator's own room over that phase and the
datagrams each end's kernel dropped, and a ladder's breaking point sets the rung
that held beside the rung that gave for every one of those that moved.

The endurance run is five hours with the fleet coming and going rather than
eight holding still. Holding a connection is not work: an unchanging fleet
finishes one operation per machine for the whole run, which leaves a leak
detector almost nothing to divide by, and five hours also fits inside the six a
scheduled job is killed at. Its ten cycles end and restart 250 machines each.

It offers the technician load its profile declares alongside that, from the same
browser-side generator the sweep legs run: a rate of journeys, and one to three
sessions held open through every phase. The sessions are the point of it. The
leak this family was written for stranded two goroutines on a session that had
*finished*, so a run opening none never performs the operation it exists to
watch — and they count twice over, because the harness answers each session's
machine side and those answers join the machine-lives in the denominator the
conservation reading divides by. Both generators' numbers are folded into the
run's own evidence bundle beside the machine side.

#### Finding what leaked

The conservation reading says whether a completed operation gave back what it
took. It does not say where, and the distance between those two answers is a
week: the run that produced the evidence is over, the machine is destroyed, and
the next endurance run is seven days away. Two readings close it, and they answer
different halves.

**What grew.** The soak takes the target's goroutine and heap profiles on an
interval and keeps every one beside the bundle, in the symbolised text form —
because the binary that would resolve a protocol buffer's addresses does not
outlive the job. The bundle's `leak_trail` reports the difference between the
readings as growth at a named line, ranked heaviest first, with the number of
intervals each stack grew in beside it: a leak grows in nearly every interval,
a cache that filled once grows in one. For a stuck-goroutine leak that is the
whole finding — the profile carries a count and the stack it is parked on.
A profile the target would not answer with is counted rather than skipped, and
a trail of fewer than two readings fails validation, because a single reading
cannot have found that nothing grew.
[ADR-119](../adr/ADR-119-finding-a-leak.md) is the decision.

**What holds it.** A heap profile records where an object was born, and a leak is
about what is still pointing at it. So
[`loadtest-reference-walk.sh`](../../scripts/loadtest-reference-walk.sh) takes a
core off the running server without stopping it and walks the heaviest live types
back to the root that keeps them alive — a global, or a named variable in a live
goroutine's frame. It runs here and nowhere else: it needs to attach to a process
and it needs a binary that still carries its debugging information, and this is
the venue where the run owns the machine and destroys it. Every way the walk
cannot happen is a refusal rather than an empty report, which
[`loadtest-reference-walk.test.sh`](../../scripts/tests/loadtest-reference-walk.test.sh)
holds it to. The core is compressed and encrypted before the walk reads it, the
plain copy is removed on every exit, and the encrypted file is uploaded as the
`soak-dump` artifact beside the plain `soak-bundle` that carries the reports.
[ADR-119](../adr/ADR-119-finding-a-leak.md)
is the decision, including why this target is built with its symbol table kept
and why the dump is encrypted to a key no workflow holds.

#### Opening a soak dump

The dump is encrypted to the maintainer's own `age` key, whose private half is
on the maintainer's machine and nowhere else. `age` and `zstd` are installed at
the versions [`tool-versions.sh`](../../scripts/lib/tool-versions.sh) pins by
[`install-dump-tools.sh`](../../scripts/install-dump-tools.sh); the reader is
built by [`install-viewcore.sh`](../../scripts/install-viewcore.sh). Download the
`soak-dump` artifact of the run, then:

```bash
age -d -i ~/.config/age/opengate-soak-dump.key core.<pid>.zst.age | zstd -d -o core.<pid>
```

and read it with the program copy from the same run's `soak-bundle`.

### k6 scenarios

Three k6 scenarios in [`load/k6/scenarios/`](../../load/k6/scenarios), each
building its executors from the profile the night walks:

| Scenario | Exercises |
|----------|-----------|
| [`api-baseline.js`](../../load/k6/scenarios/api-baseline.js) | A technician's round: health, current user, sites and the device list, then one machine's page and one instruction sent to it, each timed on its own |
| [`relay-throughput.js`](../../load/k6/scenarios/relay-throughput.js) | A real remote session: the operator's side of the relay, timing its own frame coming back from the machine |
| [`concurrent-agents.js`](../../load/k6/scenarios/concurrent-agents.js) | Agent-shaped device and session reads spread across the fleet's sites |

`setup()` registers a throwaway member of the staging organization through
[`load/k6/lib/session.js`](../../load/k6/lib/session.js) and reads the sites that
member can see. Where a scenario times a journey against one machine, it reads
the fleet from a site chosen for holding machines: an estate spreads its fleet
over its sites, so a site picked for sorting first is empty as often as not, and
a journey with nothing to open publishes a zero indistinguishable from a fast
night. The scenarios act on the fleet the estate already holds rather than
standing up their own: organization is the visibility boundary, so a member
reads the whole fleet, while creating a site is administrator work the server
refuses, and a scenario that built its own fixtures would measure the 403 path
instead. What they do send a machine is idempotent and stops at the server
accepting it, so a run times the acceptance path without a fleet-wide side
effect. `setup()` throws on an unexpected status, so a broken precondition names
itself rather than turning every request in the run red.

#### Load comes from the profile

How many journeys a second arrive and how many sessions are open are
technician-side numbers, and the profile is where both are written down
([ADR-101](../adr/ADR-101-load-profiles-and-limits.md)). The projection
in [`scripts/lib/loadtest-profile.sh`](../../scripts/lib/loadtest-profile.sh)
hands the walk to the generator, which turns each phase into one arrival-rate
scenario tagged with that phase's name.

Arrival rate rather than a fixed count of virtual users: a fixed count is a
closed loop, where each user waits for its own reply before asking again, so a
server that has slowed is offered *less* work and the latency it reports
understates the damage. What the open loop costs is a second way to be wrong —
a generator that cannot keep the rate offers less and the night reports a
healthy system nobody finished asking — so `dropped_iterations` is a number the
profiles hold to a limit.

Tagging by phase is also what lets a percentile be taken over the load rather
than over the climb to it. A profile marks one phase `measured: true`, the
generator names that phase's sub-metric in a threshold, and both the canonical
row and the bundle read that sub-metric where it exists. A generator joins the
walk where the walk is, from the start time the harness announces — one that
started the shape again from its beginning would hold its steady window open
past the drain.

#### One address per technician

The server counts requests per address, at about a hundred a second
([API router](../../server/internal/api/api.go)). A run driving the server from
one pod used to spend one allowance between all of it, which made the rate
limiter the run's throughput: the latency figures described an idle system and
no throughput regression could show.

Each simulated technician now presents an address of its own, and so does each
arriving machine — from 198.18.0.0/15, which RFC 2544 reserves for benchmark
traffic, so a synthetic address can never be somebody real. The limit is still
enforced at full strength; the budget is simply per technician.

A presented address is believed only from a peer the deployment has named as a
proxy, which is narrower than the rule it replaced — see
[ADR-116](../adr/ADR-116-forwarded-addresses.md)
and [Security](Security-and-Dependencies.md#rate-limiting).
[`scripts/tests/loadtest-rate-budget.test.sh`](../../scripts/tests/loadtest-rate-budget.test.sh)
sizes one technician's own share against the router's limit and holds the whole
chain that makes the address believed — the label the pods carry, the service
that selects it, the trusted list that names the service — because a single
broken link puts every technician back behind one allowance and the night
reports it as the server failing.

Every name a run creates carries the run's marker and its own seed, so two nights
never ask the server for the same customer.
[`scripts/loadtest-cleanup.sh`](../../scripts/loadtest-cleanup.sh) removes what
matches — accounts, customers, sites and machines — and counts each kind, and
[`loadtest-bundle-merge.sh`](../../scripts/loadtest-bundle-merge.sh) folds that
count into the bundle after the run, residue and all: a run that says it left
nothing has to have looked, and a kind that is removed but never counted is a
kind whose residue nobody can see. The harness writes its bundle before anything
is removed, so its own cleanup section is uncounted and says why; on the
disposable stack nothing outlives the job and the bundle says that rather than
counting nought. The statements are held to the live schema by a test in
[`server/tests/loadtest/`](../../server/tests/loadtest) that runs the script
against a database the migrations built, so a column that moves fails the day it
moves rather than the next night.

The relay scenario needs a machine on the other end of every session it opens, so
the QUIC harness holds its fleet connected for the whole k6 window rather than
running after it. What the metric records is the operator's own frame going out
through the server, through the machine, and back. The relay is a byte pipe —
the agent protocol is MessagePack, so the server forwards every frame as binary
— and k6 delivers a binary frame to a different handler than a text one, so the
operator's side listens on both. Listening on one leaves the echo arriving at a
handler that does not exist: the frame lands and is counted, the round trip is
never recorded, and each iteration spends its whole echo timeout waiting for an
answer it already had.

Each scenario declares the workload it performs, and that name travels with every
sample into the trend store. A window baseline is only a baseline for the work
that produced it, so a scenario rewritten to measure something else takes a new
name and is compared against itself rather than against what it replaced. See
[ADR-038](../adr/ADR-038-ci-trend-store.md).

The scenarios spell their URLs by hand, so
[`scripts/tests/api-endpoint-drift.test.sh`](../../scripts/tests/api-endpoint-drift.test.sh)
checks every path and query parameter they send — and every one
[`deploy/scripts/smoke-test.sh`](../../deploy/scripts/smoke-test.sh) probes — against
[`api/openapi.yaml`](../../api/openapi.yaml), which is what makes a route rename fail
in the gauntlet rather than in the nightly.

[`scripts/loadtest-k6-run.sh`](../../scripts/loadtest-k6-run.sh) runs each scenario and
keeps its summary export only when the run produced a measurement. A failed
threshold counts as one; a script exception does not, and its export is discarded so
the handful of requests `setup()` managed never enters the trend the regression
check compares against. A breached threshold is recorded beside the export rather
than failing the scenario, because whether a mark is blocking is the profile's
decision — a mark set tighter than the measurement's own spread would otherwise
fail every night from the day it was tightened.

Against staging, k6 itself runs in a short-lived cluster pod through
[`scripts/loadtest-k6-incluster.sh`](../../scripts/loadtest-k6-incluster.sh), which
executes the same k6 argument list beside the server and copies the summary export
back to the runner for the decision above. Generating load one hop from the server
is what keeps the trend a measurement of the server rather than of the path to it.
The generator pod holds its own processor and memory allocation, separate from the
server's: sharing them is why the API mark had to be set wider than any regression
worth finding.

### QUIC harness

[`server/tests/loadtest/`](../../server/tests/loadtest) connects N machines, each
performing the full mTLS QUIC handshake and registration, then holds them
connected and behaving — heartbeats and telemetry on a jittered cadence,
reconnects with a backlog to drain, duplicate connections, and the machine side
of any relay session the server hands it. It reports p50/p95/p99 for connect,
handshake and register, and writes an evidence bundle.

It also reports the window the fleet arrived in — from the run's start to the
moment the last machine finished registering — and that is what the run's arrival
rate divides by. The run's own wall clock is nearly all hold: `-hold` keeps every
machine connected so the k6 relay scenario has something to open sessions
against, so a rate taken from it describes the hold rather than the arrival, and
a hundred machines that all arrived report a fraction of one per second.

Registration is the one figure it does not time itself. The harness's own clock
would stop when the frame reaches a local send buffer, which reports microseconds
however slow the write behind it becomes, and the device row is written later and
elsewhere — so `-metrics-url` points it at the server's own account of how long
registration took, published where that row lands, with the connection pool
beside it. A registration queued behind a connection and one executing slowly are
the same latency until the pool says which. That figure is the one the run
publishes: a run the server did not answer publishes no registration line at all,
because an absent figure is honest where a local write under registration's name
is not.

```bash
# Default: 100 machines against a local stack that owns its own authority
cd server && go run ./tests/loadtest/ -agents=100 -addr=127.0.0.1:9090

# Against staging: build the fleet through the API as the seeded service
# account, enrol the way an installer does, hold the fleet connected, and answer
# session requests so a generator can measure the relay
cd server && go run ./tests/loadtest/ \
  -agents=500 -addr=10.0.0.42:9090 \
  -enroll-url=http://opengate-staging-server:8080 \
  -metrics-url=http://opengate-staging-server:8081 \
  -fixture-account="$SERVICE_ACCOUNT" -fixture-password="$SERVICE_PASSWORD" \
  -relay-sessions -hold=8m \
  -profile=../load/profiles/normal.yaml -bundle=/tmp/loadtest-bundle
```

A run given a profile walks its phases — climbing to each declared level, holding
there, and winding down at the end — rather than offering the whole fleet at once
and waiting. The machine it shares is read between phases against the profile's
own limits, and a run that has pushed it past them stops there.

The fleet itself is built through the same interface a technician uses:
customers, sites and operator accounts are ordinary requests, and the machines
arrive by enrolling with a credential the run mints and spends. `-fixture-size`
picks which of the three committed fleets to build and `-fixture-seed` decides
it, so the same seed reproduces the same fleet exactly.

The certificate authority's private key never leaves the cluster. Against
anything shared the harness keeps its own private keys and sends signing
requests, spending a token minted for the run and deleted after it; only a local
stack, whose authority is as disposable as the stack around it, is signed for
directly.

Every address the harness dials goes through one allowlist — the configured
target and the relay URL that arrives inside a session request alike — so a field
on the wire cannot send a generator somewhere the run is forbidden to go.

Against staging the harness is launched detached inside its own cluster pod
through [`scripts/loadtest-quic-incluster.sh`](../../scripts/loadtest-quic-incluster.sh),
so the fleet is held by the pod rather than by a stream reaching it from the
runner. The launch is a call that returns in a moment and every later question —
is it offering a fleet, what did it decide, what did it print — is a fresh call
that can be asked again. The start returns only once the harness has announced
its fleet, which is why a fleet that never came up is reported by the step that
launched it rather than by a scenario four minutes later; a launch the API server
never got to the kubelet is made again, and one that reached the pod never is,
because a second harness would build a second fixture over the first one's names.
See [ADR-082](../adr/ADR-082-load-run-validity.md).

[`scripts/loadtest-quic-run.sh`](../../scripts/loadtest-quic-run.sh) applies the
same keep-or-discard rule the k6 half has to what that read-back returns: a fleet
that half connected is a measurement and its error rate is the finding, while a
harness that could not start describes its own failure and its output is
discarded.

[`scripts/loadtest-run-completeness.sh`](../../scripts/loadtest-run-completeness.sh)
then names which scenarios produced rows and which did not, and returns the
verdict that decides whether the night enters the trend at all.

### In CI

- **E2E** runs on every push and gates `merge-to-main` (includes Lighthouse CI audits)
- **Bundle size** runs on every push and gates `merge-to-main` (size-limit gzip check)
- **Load tests** walk the everyday profile against staging nightly, and on
  `workflow_dispatch` (not on every push)
- **The performance stack** — the volume, scaling and three shape families, on a
  throwaway runner — runs nightly, last in the order, after the twenty-job pool
  the earlier batches hold has drained
- **The endurance soak** runs weekly, on a throwaway runner of its own

The hour a cron names is not the hour a run starts: every scheduled run begins
four and a half to six and a half hours later, and the spacing compresses as it
slips. What the hour buys is a place in the order. The two runs that need the
staging namespace are kept apart by the claim they take on it rather than by the
gap between their crons — see
[`scripts/staging-lease.sh`](../../scripts/staging-lease.sh), which renews that
claim for as long as its holder is working and fails the run that lost it.
- **Browser performance evidence** comes from Lighthouse CI artifacts/summaries
  and the bundle-size gate; PageSpeed Insights is not part of the current CD
  workflow.

## Leak checks

**What they ask:** *did the operation give back what it took?*

Every functional test asks whether an operation produced the right answer. That
is a different question, and a leak passes it. A relay session that stranded two
goroutines every time it completed returned the right answer on every request,
for the life of the project, while the server walked up to its memory limit.

**Why no other gate saw it.** Each was blind for its own structural reason, and
the reasons are worth knowing because they are not specific to this defect:

| Gate | Why it stayed green |
|---|---|
| Coverage | The leaking line *executed*, so it counted as covered. |
| Benchmark trend | It measures allocations per operation. A leak allocates the same amount; only *retention* differs. |
| Mutation testing | Deleting the line changed no assertion in the suite, so the mutant was equivalent — a tree with the line removed passed every test, slightly faster. |

**What the test does.**
[`conservation_test.go`](../../server/tests/integration/conservation_test.go)
runs against **one** assembled server and, at several different session counts:

1. drives N relay sessions from start to finish;
2. reads the two resources the process actually holds — live goroutines, and
   retained heap;
3. plots those readings against the number of sessions completed, and fits a
   line through them.

**The assertion is that both lines are flat** — zero retained per completed
session, within a stated tolerance.

**Why a slope and not a single reading.** A server starts goroutines that take no
context and never stop, and its store and connection pool add more. Any fixed
"goroutines should be ≤ N" figure has to guess at that constant and goes stale
the moment anything else in the process changes. A slope cancels the constant
out, and it states the property directly: *one more completed session should
cost nothing*.

**Where the tolerances come from.** Each is written in the file next to the two
measurements that bracket it — what the defect read, and what the fixed code
reads. For the relay that is 2.0 goroutines and 34 KiB per session with the bug,
0.0 and 1.8 KiB without it, so the tolerances sit at 0.5 and 8 KiB. A tolerance
nobody can justify is a flake waiting for a slow machine.

**Why the integration tier.** The test drives real WebSocket connections, and
that tier's seam is "what needs a transport".

**Two things run beside it.** A static guard in the pen-test gate refuses the
code shape that caused this before the test has to run, and the nightly load run
asks the same question of the deployed server under real load
([ADR-082](../adr/ADR-082-load-run-validity.md)). The
rule all three serve is
[`resource-conservation.md`](../../.claude/rules/resource-conservation.md).

## Speed checks

### Benchmarks

The standalone [benchmark workflow](../../.github/workflows/benchmark.yml) tracks hot-path
performance trends in VictoriaMetrics. Allocation metrics (`allocs/op`, `B/op`) are
deterministic and gate against the committed
[baseline](../../benchmarks/baseline.json); wall-clock `ns/op` gates against a
VictoriaMetrics window baseline plus an absolute ceiling, sized from the live
series' measured variance because shared GitHub runners are noisy. See
[CI Pipeline](./CI-Pipeline.md) for the gate semantics.

| Language | What's Benchmarked | Tool |
|----------|--------------------|------|
| Go | Protocol codec, cert signing, DB operations, handshake | `testing.B` + `-benchmem` |
| Rust | Frame/handshake encode/decode; Edge Sentinel detection, sampler, RSS probe | Criterion 0.8 |

#### Running them locally

```bash
# Go
cd server && go test -bench=. -benchmem -run='^$' ./internal/...

# Rust
cd agent && cargo bench -p mesh-protocol
cd agent && cargo bench -p mesh-agent-core --bench edge_sentinel_bench
```

#### Regressions

The committed [`benchmarks/baseline.json`](../../benchmarks/baseline.json) is the reviewed
baseline. Allocation regressions above the baseline tolerance fail the workflow; `ns/op`
outliers are emitted as advisory lines and graphed on the Grafana **Benchmark Trends**
dashboard.

### Bundle size

`size-limit` with `@size-limit/file` enforces gzip size budgets on the production build output. Configuration: `web/.size-limit.json`.

```bash
# Check bundle size locally
cd web && npm run build && npm run size
```

### Lighthouse

After E2E tests, Lighthouse CI audits `/login` with 3 runs (desktop, no throttling). Accessibility and best-practices failures are hard errors; performance is warn-only due to CI runner variance. Configuration: `web/.lighthouserc.json`.

```bash
# Run locally (requires server at localhost:8080)
npm install -g @lhci/cli
cd web && lhci autorun
```

## Fuzzing

The wire decoder is the agent's primary untrusted-input surface, so
[`Frame::decode`](../../agent/crates/mesh-protocol/src/codec.rs) has a coverage-guided
cargo-fuzz / libFuzzer target ([`agent/fuzz/fuzz_targets/decode.rs`](../../agent/fuzz/fuzz_targets/decode.rs))
that asserts arbitrary bytes never panic. libFuzzer needs a nightly toolchain, so
the bounded session runs as observability — `make fuzz-rust` locally, and the
nightly [fuzz.yml workflow](../../.github/workflows/fuzz.yml) in CI — never as a
merge gate.

The always-run guard on stable is
[`decode_corpus_test.rs`](../../agent/crates/mesh-protocol/tests/decode_corpus_test.rs):
it replays every seed in [`agent/fuzz/corpus/decode/`](../../agent/fuzz/corpus/decode)
(crafted edge cases per decode branch, a real encoded frame, plus any minimized
crash) through the decoder under plain `cargo test`. A crash found by the nightly
fuzzer is minimized and committed back into that corpus, so the stable replay
re-runs it forever.

The server side of the same surface uses Go's native fuzzing in
[`codec_fuzz_test.go`](../../server/internal/protocol/codec_fuzz_test.go):
`FuzzReadFrame` holds the envelope parser to "no panic, and no allocation past
`MaxFrameSize`", and `FuzzDecodeControl` holds the msgpack decoder to "decode or
error, never panic". Both seed from the committed goldens — `FuzzDecodeControl`
peels the envelope first so the fuzzer starts on msgpack-shaped input — plus
hand-written edge cases for empty, truncated and unknown-type frames. Under
plain `go test` the seed corpus runs as a normal test; each doc comment carries
the `-fuzz` invocation for an extended local session.
