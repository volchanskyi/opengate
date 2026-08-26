# Repair the four performance nightlies, and finish what the strategy scaffolded

Follow-on to [`performance-test-strategy-revision.md`](performance-test-strategy-revision.md).
Everything below was read off a system on 2026-08-26 — the workflow logs, the
live staging database, and the code — not inferred.

## 1. Confirmed state

### 1.1 Load Tests — red every night since 2026-08-22

The night after the strategy commit landed. Four faults in one chain.

**L1 — the spent token is never deleted.** `load-test.yml` runs
`psql -q -c "DELETE FROM enrollment_tokens WHERE token = :'token'"`. `psql`
substitutes `-v` variables in files and on standard input, not inside `-c`,
so the server receives the text literally and answers
`syntax error at or near ":"`. Every night since the 22nd left a live token.

**L2 — cleanup cannot delete a user.** `loadtest-cleanup.sh` runs a plain
`DELETE FROM users`. Three tables reference `users` with no cascade —
`enrollment_tokens.created_by`, `agent_sessions.user_id`, `sites.owner_id` —
so the delete is refused, `set -e` aborts the script, and nothing is removed.

**L3 — every run uses the same three identities.** `LOADTEST_RUN_ID` is set
nowhere in the repository, so `runId()` in `load/k6/lib/session.js` returns its
fallback `local` and each scenario registers the same address every night. With
L2 in place, night two gets `400 registration failed` — the duplicate-email
path — and all three k6 scenarios abort in `setup`.

**L4 — completeness reads a file that does not exist yet.** *Record run
completeness* is ordered before *Build canonical load-test summary* but reads
`loadtest-summary.json`, which the later step writes. It fails with
`missing or empty canonical summary` on every run, whatever else happened.

**L5 — staging has no administrator that survives cleanup.** Measured:
94 users, 94 of them load-test residue, 24 admins, all 24 residue. The mint step
selects `FROM users WHERE is_admin ORDER BY created_at LIMIT 1`; once cleanup
works, that matches nothing, the `INSERT … SELECT` writes zero rows without
erroring, and the machine half of the run spends a token that does not exist.

**L6 — registration is still timed at the send buffer.** `metrics.go` publishes
`AgentRegistrationDuration`, recorded where the device row lands, but nothing in
`server/tests/loadtest/` reads it: `main.go` still stops its clock after
`codec.WriteFrame`. The two `register` ceilings still sit on a number that
cannot move.

### 1.2 Benchmark Trends — red since 2026-08-21

`BenchmarkHandshaker_PerformHandshake` moved 65 → 69 allocations and
5472 → 5902 bytes against the committed reference. Commit `16a66aeb` added
`clientConn.SetDeadline(...)` inside the measured loop. The test's own plumbing
is being billed to the server's handshake.

### 1.3 Mutation Testing — no complete run since 2026-08-10

Two independent causes.

**M1 — the pre-flight refuses a shard.** `go-observability-harness` projects
450 mutants × 10 s = 75 min against a 55-minute budget. It grew when
`server/tests/loadtest/` gained the fixture, profile, simulator, validity and
bundle files, all of which land in that shard through `dir:tests/loadtest`.

**M2 — two shards silently hit the job ceiling.** `go-domain-detection`
(353 mutants, rated 8 s) and `go-api-device-control` (59 mutants, rated 41 s)
project 48 and 40 minutes and are shot at exactly 75. Their per-mutant ratings
are stale. Device-control took 64.4 minutes on 2026-08-21 and passed, so it has
been marginal for a while.

Consequence: the artifact set comes back incomplete, the publish job goes red,
and the score gate reds behind it.

### 1.4 Performance Stack — failed on its only run, 2026-08-23

**P1 — the sweep cannot find its own scripts.** All four legs run
`scripts/loadtest-quic-run.sh` and `-profile=load/profiles/scaling.yaml` with
`working-directory: server`. Exit 127, "No such file or directory".

**P2 — the weighing queries a table that has never existed.**
`perf-weigh-fixture.sh` counts `device_metric_samples`. Numeric readings live in
VictoriaMetrics; the Postgres telemetry table is `device_processes`.

**P3 — the measurement is structurally zero.** `baseline` and `total` are both
read from `pg_database_size` with nothing created in between, so
`fixture_bytes` is always `0`.

**P4 — the two jobs disagree about where a bundle goes.** The sweep writes to
`server/perf-bundles` and uploads `server/perf-bundles`; the volume job writes
to `../perf-bundles` and uploads `perf-bundles`, having also created a
`perf-bundles` at the root. The run uploaded nothing.

**Not a fault:** the workflow is weekly by design, `cron: '0 4 * * 0'`. Its one
run was Sunday 2026-08-23.

### 1.5 The Actions envelope, measured

The repository is public, so GitHub-hosted runners carry no minute budget and no
storage charge. The ceiling that binds is concurrency: on the 2026-08-26
mutation run exactly 20 jobs started within four seconds and the remaining 31
queued, the last starting 25 minutes late. That is the Free plan's 20-job pool,
and the 51-job mutation matrix saturates it from 03:00 to roughly 05:30 nightly.

| Slot (UTC) | Workflows |
|---|---|
| 03:00 | mutation (51 jobs), cross-browser E2E, Terraform drift |
| 04:00 | benchmarks, fuzzing, code-quality trend |
| 05:00 | load tests |

## 2. Decisions locked

| # | Decision | Source |
|---|---|---|
| 1 | Staging gets a permanent seeded service administrator, created by the deployment and never removed by cleanup | user |
| 2 | Every fixture row is created through the public API | user |
| 3 | The benchmark stops its clock around its own setup; the reference figures are re-measured | user |
| 4 | Mutation shards are split, their costs re-based from measured runs, and the job ceiling raised | user |
| 5 | All three fleet sizes are built in both venues — staging and the runner | user |
| 6 | Speed marks are graded by what the technician is doing, not one API-wide number | user |
| 7 | Both free-tier processor guards are held at the stricter 2 processors / 12 GB | user |
| 8 | Production gets a guaranteed share before staging carries a large fleet | user |
| 9 | The soak holds for eight hours, inside the login token's life | user |
| 10 | The performance stack moves to nightly, at 07:00 UTC where the job pool is clear | user |
| 11 | One commit; the scheduled runs are the verdict | user |

### On decision 5 — the risk, stated once

Staging's database has no volume of its own: it writes into the same node root
production's database and log store sit on, 12.5 GiB of it, and every pod on
that node is evictable. Two thousand machines and their history on staging can
reach production. Decision 8 is the mitigation and is not optional: production's
server and database get requests equal to limits, which puts them last in the
eviction order, before any large fleet is built on staging.

### On decision 6 — the marks

| Journey | Mark | Why that number |
|---|---|---|
| Machine list | 300 ms | A glance. A technician scanning a fleet does not wait. |
| One machine's page | 500 ms | Fans out to inventory, history and readings. |
| Command accepted | 1 s | A deliberate act; the person expects a moment. |
| Session round trip | 150 ms | A conversation. Past this, typing feels detached. |

## 3. Scope

### In scope

- L1–L6, the benchmark repair, M1–M2, P1–P4.
- A fixture builder that creates rows, through the public API.
- Phase sequencing in the harness: a run walks its profile's phases.
- Safety limits that are evaluated rather than declared.
- Production quality-of-service, ahead of decision 5.
- The graded marks, and the soak's length.
- Holding the two free-tier processor guards equal at 2 / 12.

### Out of scope

- A tenant-creation API. Load identities stay in the default tenant; the debt
  entry and its trigger stand.
- Token renewal in the generator (the eight-hour soak needs none).
- Any change to the log store or the storage layout.
- Production as a load target.

## 4. Steps

1. **Production quality of service.** Requests equal limits for the production
   server and database. Prerequisite for everything that builds a fleet on the
   shared node.
2. **A durable staging administrator.** Seeded by the deployment, excluded from
   cleanup by name, and the account the enrollment token is minted against.
3. **Load-test repairs.** L1 (token deletion over standard input), L2 (dependent
   rows removed before the users that own them, in one transaction, never
   touching the service account), L3 (`LOADTEST_RUN_ID` from the run id, carried
   into the in-cluster generator), L4 (summary before completeness).
4. **Registration measured where it lands (L6).** The harness reads the server's
   own registration histogram and records it beside its own figure.
5. **The benchmark.** Stop the clock around the pipe and its deadline;
   re-measure the reference.
6. **Mutation.** A shard of its own for the load-test harness, the detection
   shard split, costs re-based from completed nights, the ceiling raised, the
   partition tests updated.
7. **Performance stack.** P1–P4, nightly at 07:00.
8. **Fixture builder.** Walks a `FixturePlan` through the public API — operator
   accounts, customers, sites, then machines by enrolling them — and removes
   what it made through the same surface.
9. **Phase sequencing.** The harness walks its profile's phases at the declared
   rates and reports one result per phase.
10. **Safety limits evaluated.** Node processor, memory and free space are read
    during a run; a breach ends the run and the night is invalid.
11. **The marks and the soak.** Graded gates per journey; soak at eight hours.
12. **Free-tier guards** held equal at 2 / 12.
13. **Docs, ADR, ledger, register.**

## 5. Verification

- `/precommit` green.
- The scheduled runs are the verdict (decision 11): mutation 03:00, benchmarks
  04:00, load tests 05:00, performance stack 07:00.
