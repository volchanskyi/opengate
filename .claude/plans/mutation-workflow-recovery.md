# Mutation Workflow Recovery

**Objective:** Restore mutation testing as a complete, reliable nightly signal,
establish a fresh post-Edge-Sentinel score baseline, and then repair every language
that is below the existing floor.

## Re-review findings (2026-07-13)

The original plan's direction was sound, but its evidence and shard model are now
stale.

### 1. Liveness has regressed beyond `go-api`

Runs #62 through #73 contain only one complete run (#66). Eleven of twelve runs
were incomplete:

- `go-api` repeatedly reached the 75-minute cap, but run #73 completed in 74m27s;
  it has effectively zero headroom rather than being the only current blocker.
- `rust-2` timed out in runs #70 through #73.
- `go-db` timed out in runs #72 and #73 after the server-side Edge-Sentinel and
  lifecycle growth.
- The older one-off failures (`rust` in #62 and `go-pure` in #63) predate the
  current shard layout but remain part of the incomplete-run history.

The recovery must therefore split Rust, API, and the database-backed Go shard. An
API-only split cannot restore complete nightly runs.

### 2. The Go partition is not actually disjoint

[`scripts/lib/mutation-shards.sh`](../../scripts/lib/mutation-shards.sh) partitions
only `server/internal/*`. The non-test load-driver sources under
`server/tests/loadtest/` are not assigned to a shard and are not globally excluded
except for `main.go`. As a result, `soak.go`, `soak_backfill.go`, and
`soak_telemetry.go` are mutated in **every** Go shard and their counts are merged
multiple times.

The replacement unit model must inventory every non-test Go source under `server/`,
not just `server/internal/*`, and assign the load-driver directory to exactly one
shard.

### 3. Run #66 is no longer a current score baseline

Run #66 (commit `2b9953f`) remains the last complete canonical row:

- Rust: `84.0%` (`361` caught, `69` missed, `3` timeout)
- Go: `82.4%` (`956` killed, `133` survived, `71` not covered)
- Web: `85.2%` (`1641` killed, `226` survived, `57` no coverage)

However, commit `a3fdf36` landed mutation-targeted Go and Rust tests immediately
after that run, including the correlate, metric-assembly, telemetry, and ML tests
the old plan proposed adding again. Many subsequent Edge-Sentinel workstreams also
added new mutation surface. Do not treat #66 as the score of current `dev`, and do
not recreate tests that already landed.

### 4. The newest shard artifacts show a broader current regression

Run #73 (commit `fdb2d12`) is incomplete, so it must never produce a canonical
Rust or Go row. Its per-shard artifacts are still useful as explicitly partial
diagnostic evidence:

- `rust-1` completed at `77.1%` (`644` caught, `192` missed, `5` timeout).
- `rust-2` was cancelled with `end_time: null`; its partial counts also happen to
  compute to `77.1%`, but that number is **not** a publishable language score.
- `go-api` completed at `81.5%`, `go-pure-1` at `89.8%`, and `go-pure-2` at
  `84.1%`; `go-db` produced no report, so none of these may be merged into a Go
  score.
- Web is a single complete shard and therefore provides valid language evidence
  even though the overall run was incomplete: `80.4%` (`1874` killed, `380`
  survived, `2` timeout, `77` no coverage).

Current `dev` also contains the later WS-20 lifecycle implementation, including
`internal/lifecycle`, `handlers_purge.go`, and `DataLifecycle.tsx`; no complete
mutation run has measured that code yet.

### 5. The first recovered dispatch exposed two remaining tail shards

Workflow run
[`29300355420`](https://github.com/volchanskyi/opengate/actions/runs/29300355420)
validated the completeness contract and most of the new partition, but it was
not complete:

- All four API shards finished in 22–32 minutes, `go-data` in 7 minutes, and
  `go-pure-1`/`go-pure-2` in 34/41 minutes.
- `rust-1` and `rust-2` finished in 21/29 minutes, while the consecutive
  `rust-3` and `rust-4` slices timed out. Cargo-mutants' default `slice`
  sharding concentrated the expensive discovery/connection modules.
- The isolated `go-agentapi` shard completed in 74m48s with 183 file-level
  mutations, leaving no usable headroom.
- The publish job uploaded and pushed `complete=false`, classified Rust 3/4 as
  invalid, and skipped baseline restore, summarization, canonical artifact, and
  score push. The no-partial-score contract therefore worked in a real timeout.

The measured follow-up is eight Rust shards with explicit `round-robin`
distribution and three balanced file-unit agent API shards. The implementation
steps below incorporate that corrected map.

## Success criteria

- Every expected Rust, Go, and Web shard completes within the existing 75-minute
  job cap, with at least 10 minutes of observed headroom during the confirmation
  window.
- Three consecutive scheduled mutation runs finish with no cancelled or incomplete
  mutation shards.
- Every complete run pushes `mutation_score{language="rust|go|web",env="ci"}` to
  VictoriaMetrics.
- Incomplete runs upload a status artifact and push run/shard completion metrics,
  but never upload or push partial Rust, Go, or Web score rows as canonical data.
- A fresh complete run after the shard recovery becomes the score-repair baseline.
- Rust, Go, and Web are all at or above the existing `85.0%` floor before this
  recovery closes.
- Existing gate semantics stay unchanged: absolute floor `85.0%`; previous-run
  drop alert `>2.0pp`; mutation testing remains observability rather than a merge
  gate.

## File inventory

### Workflow and shell infrastructure

- Modify [`.github/workflows/mutation.yml`](../../.github/workflows/mutation.yml)
  - Expand Rust to eight round-robin shards.
  - Use the ten-shard Go unit map below.
  - Derive publish expectations from the shard library where shell can do so.
  - Build and upload an always-present mutation status artifact.
  - Push incomplete-run/shard status metrics before summarization.
- Modify [`scripts/lib/mutation-shards.sh`](../../scripts/lib/mutation-shards.sh)
  - Replace the internal-package-only model with directory and file mutation units.
  - Inventory all non-test Go source under `server/` exactly once or explicitly
    exclude it.
  - Expose the expected Rust, Go, Web, and all-shard IDs used by status generation.
- Modify [`scripts/mutation-merge-rust.sh`](../../scripts/mutation-merge-rust.sh)
  - Reject missing, malformed, incomplete, or non-numeric cargo-mutants outcomes.
  - Write merged output atomically.
- Modify [`scripts/mutation-merge-go.sh`](../../scripts/mutation-merge-go.sh)
  - Reject missing, malformed, or non-numeric gremlins reports.
  - Write merged output atomically.
- Create `scripts/mutation-status-build.sh`
  - Validate every expected artifact and emit one deterministic status JSON file.
- Create `scripts/mutation-status-vm-push.sh`
  - Convert the status JSON to Prometheus text and push it through
    [`scripts/lib/vm-push.sh`](../../scripts/lib/vm-push.sh).
- Modify [`scripts/tests/mutation-workflow.test.sh`](../../scripts/tests/mutation-workflow.test.sh)
  - Pin the new shard map, whole-server Go source partition, merge validation,
    status generation, and no-partial-score behavior.
- Modify [`scripts/tests/ci-trend-vm-push.test.sh`](../../scripts/tests/ci-trend-vm-push.test.sh)
  - Pin the status metric names, mandatory labels, shard label escaping, and
    complete/incomplete values.
- Modify [`Makefile`](../../Makefile) only if the unit model requires compatibility
  changes in `make mutate-go`; local and CI shard IDs must remain identical.

### Score repair and project state

- Add focused language tests only after the first complete recovered run identifies
  the current survivors and no-coverage clusters.
- Update [`docs/Testing.md`](../../docs/Testing.md) and, if status series are charted
  or queried there, [`docs/Monitoring.md`](../../docs/Monitoring.md) to describe the
  complete-vs-incomplete publication contract by linking to its executable sources.
- Update [`.claude/techdebt.md`](../techdebt.md) after implementation lands so the
  existing Go-only nightly-confirmation item covers the whole mutation workflow.

No ADR is required: this preserves the existing mutation-as-observability and
VictoriaMetrics decisions rather than changing them.

## Implementation plan

### 1. Pin the corrected contracts with failing shell tests

Extend `scripts/tests/mutation-workflow.test.sh` before changing workflow or shell
implementation:

1. Require exactly eight Rust matrix legs using `0/8` through `7/8`, with
   `--sharding round-robin`.
2. Require workflow Go shard IDs to match `mutation_go_shards` exactly.
3. Inventory every `server/**/*.go` non-test source:
   - each file is covered by exactly one `dir:` or `file:` mutation unit, or
   - it matches an explicit global exclusion;
   - no source is assigned twice;
   - test files do not become mutation targets.
4. Prove `server/tests/loadtest/soak*.go` belongs to one shard only and
   `tests/loadtest/main.go` remains globally excluded.
5. Prove every non-generated `server/internal/api/*.go` source is assigned once,
   including `handlers_device_history.go` and `handlers_purge.go`.
6. Add Rust merge fixtures for missing files, malformed JSON, `end_time: null`, and
   missing/non-numeric count fields; every failure must exit 2 and leave no output.
7. Add equivalent Go merge fixtures for missing files, malformed JSON, and
   missing/non-numeric count fields; every failure must exit 2 and leave no output.
8. Add status-builder fixtures for a complete run, a missing Go shard, an invalid
   Rust outcome, and an invalid Web report.
9. Add VM-push fixtures for complete and incomplete status payloads.
10. Assert the workflow uploads status before any summarize step can exit 2 and never
   runs score publication when status says the run is incomplete.

All tests remain deterministic and always run; add no skip/focus markers.

### 2. Re-shard Rust and Go with real headroom

#### Rust

Change the Rust workflow matrix to:

```yaml
- { language: rust, shard: rust-1, rust_shard: "0/8" }
- { language: rust, shard: rust-2, rust_shard: "1/8" }
- { language: rust, shard: rust-3, rust_shard: "2/8" }
- { language: rust, shard: rust-4, rust_shard: "3/8" }
- { language: rust, shard: rust-5, rust_shard: "4/8" }
- { language: rust, shard: rust-6, rust_shard: "5/8" }
- { language: rust, shard: rust-7, rust_shard: "6/8" }
- { language: rust, shard: rust-8, rust_shard: "7/8" }
```

Run cargo-mutants with `--sharding round-robin`. Publish must expect all eight
outcome files. Do not raise the 75-minute cap or remove `edge-tsdb`; horizontal
split and balanced distribution are the recovery mechanisms.

#### Go

Use these eight shards:

| Shard | Mutation units |
|---|---|
| `go-api-core` | `api.go`, `converters.go`, `middleware.go`, `wsconn.go`, `handlers_client_errors.go`, `handlers_health.go`, `log_redact.go`, `metrics_assemble.go`, `ratelimit.go` |
| `go-api-auth-admin` | `handlers_auth.go`, `handlers_users.go`, `handlers_groups.go`, `handlers_security_groups.go`, `handlers_security_group_members.go`, `handlers_audit.go`, `handlers_push.go` |
| `go-api-devices` | `handlers_devices.go`, `handlers_device_actions.go`, `handlers_device_correlate.go`, `handlers_device_history.go`, `handlers_device_inventory.go`, `handlers_device_metrics.go`, `handlers_amt.go`, `handlers_relay.go`, `handlers_sessions.go` |
| `go-api-lifecycle` | `handlers_enrollment.go`, `handlers_install.go`, `handlers_updates.go`, `handlers_purge.go` |
| `go-agentapi-core` | `conn.go`, `server.go`, `errors.go` |
| `go-agentapi-control` | `backfill_scheduler.go`, `conn_backfill.go`, `conn_discovery.go`, `handshaker.go`, `deregister.go` |
| `go-agentapi-telemetry` | `conn_telemetry.go`, `conn_logs.go`, `conn_history.go`, `alert_breach.go`, `alert_rules.go` |
| `go-data` | `internal/auth/`, `internal/db/`, `internal/dbtx/`, `internal/device/`, `internal/inventory/`, `internal/lifecycle/`, `internal/session/`, `internal/audit/`, `internal/usecase/` |
| `go-pure-1` | `internal/amt/`, `internal/updater/`, `internal/notifications/`, `internal/cert/` |
| `go-pure-2` | `internal/protocol/`, `internal/correlate/`, `internal/telemetry/`, `internal/relay/`, `internal/metrics/`, `internal/signaling/`, `internal/testpg/`, `internal/testvm/`, `internal/osutil/`, `internal/clientapi/`, plus `tests/loadtest/` |

Represent units as repository-relative paths:

- `file:internal/agentapi/conn.go`
- `dir:tests/loadtest`
- `file:internal/api/handlers_auth.go`

`mutation_go_shard_exclude_regex` must combine the global exclusions with every
unit not assigned to the requested shard. Keep module-wide coverage by continuing
to run `gremlins unleash .` from `server/`; only mutation targets are narrowed.

Global exclusions remain:

- `internal/api/openapi_gen.go`
- `cmd/meshserver/main.go`
- `tests/loadtest/main.go`
- `internal/testutil/`

Do not tune `server/.gremlins.yaml`'s timeout coefficient in this recovery. The
runtime problem is duplicated and oversized mutation scope, not evidence that the
per-mutant budget is wrong.

### 3. Make completeness explicit and impossible to confuse with score

Harden both merge scripts:

- validate every input before creating the output path;
- require every count field consumed by `mutation-summarize.sh` to be numeric;
- additionally require Rust `.end_time` to be a non-null string;
- write to `mktemp` in the output directory and `mv` only after successful `jq`;
- remove any temporary file on failure and never leave a stale canonical output.

Create a status file shaped like:

```json
{
  "commit": "<github.sha>",
  "run_id": "<github.run_id>",
  "complete": false,
  "shards": {
    "rust-1": { "complete": true, "reason": "ok" },
    "rust-2": { "complete": false, "reason": "invalid" },
    "go-api-core": { "complete": false, "reason": "missing" },
    "web": { "complete": true, "reason": "ok" }
  }
}
```

Reasons are a bounded vocabulary: `ok`, `missing`, or `invalid`. `complete` is true
only when all expected shard artifacts validate and both language merges can be
formed. Web validates as JSON with a reporter-shaped `files` object; file existence
alone is not enough.

Workflow order in `publish`:

1. Download artifacts.
2. Validate/place artifacts and build `mutation-status.json`.
3. Upload `mutation-run-status` with `if: always()` and 90-day retention.
4. Set up OCI/kube access.
5. Push status metrics; warn and continue if only the observability transport fails.
6. Restore the previous complete baseline.
7. Run summarize/regression logic only when the status is complete; otherwise exit 2
   with the existing incomplete-run diagnostic.
8. Upload/push the canonical score row only for a complete run.

Status metrics:

- `mutation_run_complete{commit,env="ci"} 0|1`
- `mutation_shard_complete{commit,env="ci",shard="<id>"} 0|1`

The status JSON carries the diagnostic reason. Do not add `reason` to the metric
labels unless a dashboard/query needs it; the boolean series is the stable contract.

### 4. Land liveness recovery before score-repair changes

Keep the first implementation change limited to workflow/scripts/tests/docs. Verify
locally with:

```bash
./scripts/tests/mutation-workflow.test.sh
./scripts/tests/ci-trend-vm-push.test.sh
make shell-quality
```

After it lands on `dev`, dispatch `mutation.yml` against `dev` and inspect:

- every expected artifact exists and validates;
- all shard jobs have at least 10 minutes of headroom;
- `mutation-run-status` reports complete;
- exactly one canonical row is produced;
- VictoriaMetrics receives all three language score rows and all completion series.

A red workflow caused by a real score below the floor is expected at this stage and
is evidence that liveness has been restored. It must not be misclassified as an
incomplete run.

### 5. Repair scores from the first complete recovered artifacts

Use the fresh complete artifact set as the authoritative work queue. For every
language below 85%:

1. Group `Survived`/`NoCoverage`/`missed` outcomes by file and function.
2. Remove equivalent/unviable cases from the actionable queue with an explicit
   rationale; do not lower the floor or exclude a real source file to improve score.
3. Add failing, observable tests for the largest clusters first.
4. Run the focused language suite locally.
5. Re-dispatch mutation testing and repeat until all three canonical scores clear the
   floor.

The run #73 artifacts provide a provisional priority queue, not a substitute for the
fresh complete baseline:

#### Rust provisional queue

- Completed `rust-1`: `edge-tsdb` corpus, frame, store, append-only, compact/redb,
  fault, and block logic account for most missed mutants. The old plan over-weighted
  `baseline.rs` and `bitio.rs`, which had only a few misses in #73.
- Partial `rust-2`: discovery ports/packages, connection/backoff, host-log parsing,
  session state, file operations, update, and terminal branches are the largest
  observed clusters. The old plan omitted the new discovery modules entirely.
- Preserve the existing equivalent-mutant note for the unimplemented file-upload arm
  unless production adds an observable upload side effect.

#### Go provisional queue

- Treat the post-#66 correlate, metric-assembly, telemetry, and ML mutation tests as
  existing groundwork. Add only assertions that kill survivors still present in the
  fresh report.
- Run #73 points first to remaining API metric assembly, update handlers, log
  redaction, rate limiting, auth, and relay/session branches.
- The current `go-pure-2` report still has actionable metrics/correlate/telemetry and
  test-harness gaps; removing duplicated loadtest mutation will make the real package
  score legible.
- `agentapi`, `inventory`, `lifecycle`, and purge handling have grown since the last
  complete Go row and must be assessed from the recovered run rather than inferred
  from #66.

#### Web provisional queue

Web already has valid #73 evidence at 80.4%, so it is part of the recovery rather
than optional maintenance. The largest actionable file clusters are:

1. `DeviceInventory.tsx`
2. `DeviceList.tsx`
3. `DeviceMetrics.tsx`
4. `charts/aligned-data.ts`
5. `state/device-store.ts`
6. `DeviceLogs.tsx`
7. `charts/TimeSeriesChart.tsx`
8. `report-error.ts`
9. `FleetHealth.tsx`
10. `Permissions.tsx`

Also inspect the newer `DataLifecycle.tsx`, which was not present in run #73. Reuse
the extensive existing `DeviceList`, `report-error`, and `Permissions` suites; add
tests for specific surviving mutations rather than duplicating broad render cases.

Focused verification commands are selected from the files actually changed. At
minimum, the final score-repair change runs:

```bash
cd server && go test ./internal/...
cd agent && cargo test --workspace
cd web && npm test
```

### 6. Confirm nightly reliability and close project state

After all language scores clear the floor:

1. Observe three consecutive scheduled runs with no incomplete shard and at least
   10 minutes of per-shard headroom.
2. Confirm each run pushed status plus Rust/Go/Web score series.
3. Update `.claude/techdebt.md` to remove or narrow the mutation-confirmation debt
   only after that evidence exists.
4. Record the significant completed work in `.claude/phases.md` and archive this plan
   in the same completing commit, fixing links per the plan-archive rule.

## Gotchas and constraints

- Do not lower the `85.0%` floor or `2.0pp` drop threshold.
- Do not raise the 75-minute job cap as a substitute for partitioning.
- Do not remove `edge-tsdb`, discovery, lifecycle, load-driver helpers, or other real
  source from mutation testing merely to reduce runtime or raise score.
- Do not merge whatever artifacts happen to exist. Every expected shard must validate.
- `end_time: null` is incomplete even when cargo-mutants wrote counts and uploaded an
  artifact.
- Keep `fail-fast: false`; one failed shard must not hide the others' diagnostic
  artifacts.
- Preserve summarize exit semantics:
  - exit `2` = incomplete or malformed input
  - exit `1` = complete run with a real score regression
  - exit `0` = complete clean run
- A VM transport failure must not change score/completeness classification.
- Do not add silent test skips or focus markers.

## Reviewer checklist

- [ ] Rust matrix and publish expectations contain exactly `0/8..7/8` and use
      round-robin distribution.
- [ ] Workflow Go shard IDs match `mutation_go_shards`.
- [ ] Every non-test Go source under `server/` is assigned once or explicitly excluded.
- [ ] `tests/loadtest/soak*.go` is mutated once, not once per shard.
- [ ] API units include both device history and purge handlers.
- [ ] `agentapi` is isolated into three non-empty file-unit shards.
- [ ] Rust and Go merge scripts reject malformed/non-numeric inputs and write atomically.
- [ ] Rust merge rejects `end_time: null`.
- [ ] Incomplete runs upload and push status but never publish a canonical score row.
- [ ] Complete runs still restore a baseline, publish one canonical row, and preserve
      regression semantics.
- [ ] Score-repair tests are derived from the first complete recovered artifact set.
- [ ] Web is restored above floor; it is not deferred as optional maintenance.
- [ ] Tech debt, phases, docs, and plan archival are updated only when their evidence
      and completion conditions are satisfied.
