---
number: 83
title: A nightly that repairs itself rather than reporting
status: Accepted
date: 2026-08-26
---

# ADR-083 — A nightly that repairs itself rather than reporting

## Context

Four scheduled workflows were red at once, and each had been red long enough
that nobody was reading them.

**The load test** failed every night from 2026-08-22 — the night after
[ADR-082](ADR-082-load-runs-measure-the-system-or-say-they-did-not.md) landed.
Four faults in one chain. The step that deletes the run's enrollment token wrote
the statement as `psql -c "… token = :'token'"`, and `psql` substitutes a
variable in a file and on standard input and never inside `-c`, so the server
received the colon literally and answered with a syntax error. Cleanup then ran
a plain `DELETE FROM users`; three tables reference `users` with no cascade — the
tokens it minted, the sessions it opened, the sites it owns — so the delete was
refused, the script stopped on the refusal, and nothing was removed at all. The
run identifier every load identity is named after was set nowhere, so each night
asked for the same three addresses and the second night was refused as a
duplicate. And the step that classifies a night as valid read a file the
following step writes.

Measured against the live database on 2026-08-26: 94 accounts, 94 of them load
residue, 24 administrators, all 24 residue.

**The benchmark gate** failed from 2026-08-21. `BenchmarkHandshaker_PerformHandshake`
moved 65 → 69 allocations and 5472 → 5902 bytes against the committed reference.
The cause was a deadline added to the test's own in-memory pipe inside the
measured loop: the test's plumbing was being billed to the server's handshake.

**Mutation testing** produced no complete score after 2026-08-10. Two causes.
The pre-flight refused `go-observability-harness`, projecting 450 mutants at ten
seconds each against a fifty-five-minute budget — while the same shard's own
report shows it finished in three minutes. And two shards were shot at the
seventy-five-minute cap without writing a report, so the artifact set came back
short and the whole night's score was lost. The per-mutant costs the projection
uses had drifted in both directions at once.

**The performance stack** failed on the only night it has ever run. All four legs
of the sweep called a repository-root script from inside `server/`; the volume
job counted rows in a table that has never existed, and took its two size
readings back to back with nothing built in between, so the fixture's weight was
zero by construction.

## Decision

**Every fault is repaired where it lived, and each one gains the test that would
have caught it.** A workflow contract test now asserts that no `psql -c` string
names a variable `psql` will not expand there, that every path a step names
exists from where that step runs, that the weighing counts only tables the schema
creates, and that a file is written before the step that reads it.

**Registration is measured where the device row lands.** The harness reads the
server's own registration histogram and the connection pool beside it, and
publishes no registration figure at all when it has not been given a server to
ask. A number the harness cannot stand behind is worse than an absent one: two
gate ceilings sat on one for months.

**A run walks its profile.** The phases are sequenced — climbing to each declared
level, holding, winding down — and the machine the run shares is read against the
profile's own limits between phases. A run that pushes it past them stops there.

**A fleet is built through the same interface a technician uses.** Customers,
sites and operator accounts are ordinary requests; the machines arrive by
enrolling with a credential the run mints and spends. A loader writing rows
straight into the database would be faster and would describe a fleet shaped by
what the loader believes the schema means.

**Staging gains one administrator that no run creates and no cleanup removes.**
Every account there is load residue, so "the oldest administrator" found one the
same run was about to delete. The deployment seeds it, its password is generated
by the chart into a secret and written down nowhere, and it is re-seeded on every
upgrade because that database rides an `emptyDir`.

**Production is last in the eviction order.** Its server and database now request
exactly what they are capped at. The figures are measured — seven days put the
server's busiest five minutes at 105 millicores and its largest resident size at
30 MB, and the database's peak at 99 MB — and as large as the node can give: 1180
of its 1830 millicores were already spoken for and a run's generators need what
is left.

**One number for the whole interface becomes four.** Opening a fleet list, 300 ms.
Opening one machine's page, 500 ms. An instruction accepted, one second. A
keystroke coming back, 150 ms and advisory until fresh nights show the relay path
holds it.

**The mutation costs are re-based from the reports rather than adjusted to fit,**
the two shards that overran are split, and the job ceiling rises to ninety
minutes. The ceiling sits above the widest measured shard rather than on top of
it: a shard shot at the ceiling writes no report and the whole night's score is
lost, so a ceiling set too low is paid for by every other shard.

**The performance stack moves from weekly to nightly, at 07:00.** The repository
is public, so the runners carry no minute budget; what binds is the twenty-job
pool, which the fifty-job mutation matrix holds from 03:00 to about 05:30 with
the load test following at 05:00.

## Consequences

The soak holds for eight hours rather than six — long enough for a slow leak to
become a line, and inside the life of the session it signs in with, so nothing
has to be renewed mid-run.

Production's processor ceiling falls from a full core to 250 millicores. That is
roughly twice its busiest measured five minutes, and the guarantee it gains is
worth more than the burst it gives up: nothing on that node protected it before.

Building the two larger fleets on staging stays a deliberate act rather than a
nightly one. The nightly builds the committed reference fleet; the larger two are
a `workflow_dispatch` choice, because staging's database writes into the same
node root production's does and a fleet four times the size has not been weighed
there yet. The performance stack weighs it on a runner first, which is the
measurement that decision was always waiting on.

Two things stay open and are recorded as debt rather than closed by assumption:
load identities still live in the default tenant, because no interface asks for a
second one; and whether the Always-Free processor grant is two or four still
needs the Oracle console, with both guards held at the stricter pair meanwhile.
