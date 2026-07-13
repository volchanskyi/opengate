# Mutation Workflow Recovery

**Objective:** Restore mutation testing as a reliable nightly signal, then repair
the real Rust and Go score regressions found in the last 9 mutation runs.

## Findings this plan implements

Last 9 scheduled mutation runs (#62 through #70) show two separate failures:

1. **Workflow liveness regression.** Eight of nine runs had at least one cancelled
   shard and therefore did not produce a complete, trustworthy canonical score row.
   The recurring blocker is `go-api`; the newest run also cancelled `rust-2`.
2. **Score regression.** The one complete recent run (#66, commit `2b9953f`) pushed
   a valid VictoriaMetrics row and failed correctly:
   - Rust: `84.0%` (`361` killed, `69` survived, `3` timeout, `433` total)
   - Go: `82.4%` (`956` killed, `133` survived, `71` not covered, `1160` total)
   - Web: `85.2%` (`1641` killed, `226` survived, `57` no coverage, `1925` total)

The July 2 VM row for Rust (`73.8%`) came from a cancelled legacy Rust run and is
partial. Future partial scores must be prevented rather than corrected manually.

## Success criteria

- Three consecutive scheduled mutation runs finish with no cancelled mutation shards.
- Every complete run pushes `mutation_score{language="rust|go|web",env="ci"}` to
  VictoriaMetrics.
- Rust and Go are back at or above the existing `85.0%` floor.
- Incomplete runs are visible in VictoriaMetrics as incomplete, but never publish
  partial Rust/Go/Web score rows.
- Existing gate semantics stay unchanged: absolute score floor `85.0%`; previous-run
  drop alert `>2.0pp`.

## File inventory

- Modify `.github/workflows/mutation.yml`
  - Expand Rust matrix from 2 to 4 shards.
  - Replace `go-api` with file-level API shards.
  - Update publish expected artifact lists.
  - Push incomplete-run/shard status metrics before failing incomplete runs.
- Modify `scripts/lib/mutation-shards.sh`
  - Replace package-only Go shard model with mutation units that can be packages or
    individual `server/internal/api/*.go` files.
  - Keep a single source of truth for workflow matrix shard IDs and shard scoping.
- Modify `scripts/mutation-merge-rust.sh`
  - Reject missing, malformed, or incomplete cargo-mutants outcome files.
  - Write merged output atomically.
- Create `scripts/mutation-status-vm-push.sh`
  - Push run/shard completion metrics to VM from a status JSON file.
- Modify `scripts/tests/mutation-workflow.test.sh`
  - Pin the new shard model, Rust completeness rules, and incomplete-run metric path.
- Add focused tests under existing language test suites:
  - Go: `server/internal/correlate`, `server/internal/api`, `server/internal/agentapi`
  - Rust: `agent/crates/edge-tsdb`, `agent/crates/mesh-agent-core`
  - Web: focused tests for `DeviceList`, `report-error`, and `Permissions`
- Update `.claude/techdebt.md` after implementation lands
  - Replace the existing Go-only nightly confirmation debt with Rust+Go confirmation.

## Implementation plan

### 1. Make workflow liveness measurable first

Add a publish-step status file with this shape:

```json
{
  "commit": "<github.sha>",
  "run_id": "<github.run_id>",
  "complete": false,
  "shards": {
    "rust-1": true,
    "rust-2": false,
    "go-api-core": true
  }
}
```

Rules:

- `complete` is true only when every expected Rust, Go, and Web artifact is present
  and mergeable.
- A Rust artifact counts as complete only if its `outcomes.json` passes the new
  `mutation-merge-rust.sh` validation.
- Web remains one shard; it counts complete when `web/reports/mutation/mutation.json`
  exists after artifact placement.
- The status artifact is uploaded with every publish job, including incomplete runs.

Create `scripts/mutation-status-vm-push.sh`:

- Input: the status JSON file above.
- Output: Prometheus text pushed via `scripts/lib/vm-push.sh`.
- Metrics:
  - `mutation_run_complete{commit,env="ci"} 0|1`
  - `mutation_shard_complete{commit,env="ci",shard="<id>"} 0|1`
- Run this before `mutation-summarize.sh`, after OCI/kube setup, so cancelled-shard
  runs leave VM evidence even when summarize exits 2.
- If VM push fails, keep current policy: observability transport failure must not
  mislabel the score result. Annotate a warning and continue to the summarize step.

### 2. Expand Rust sharding and prevent partial Rust rows

Change the Rust workflow matrix from:

```yaml
- { language: rust, shard: rust-1, rust_shard: "0/2" }
- { language: rust, shard: rust-2, rust_shard: "1/2" }
```

to:

```yaml
- { language: rust, shard: rust-1, rust_shard: "0/4" }
- { language: rust, shard: rust-2, rust_shard: "1/4" }
- { language: rust, shard: rust-3, rust_shard: "2/4" }
- { language: rust, shard: rust-4, rust_shard: "3/4" }
```

Update the publish job's `rust_expected` list to include all four outcome files.

Harden `scripts/mutation-merge-rust.sh`:

- For each input file:
  - file exists
  - valid JSON
  - `.end_time != null`
  - `.caught`, `.missed`, `.timeout`, `.unviable` are numeric
- If validation fails:
  - print a specific diagnostic naming the bad shard file
  - exit 2
  - leave no output file behind
- On success, write to `mktemp` in the output directory, then `mv` to the requested
  output path.

Update `scripts/tests/mutation-workflow.test.sh`:

- Require exactly four Rust matrix entries using `0/4..3/4`.
- Require publish to expect all four Rust artifact paths.
- Add a fixture where one `outcomes.json` has `"end_time": null`; assert merge fails
  and no output is written.
- Add malformed JSON and missing numeric field fixtures.

### 3. Split Go API by file-level mutation units

Keep module-wide coverage: each Go shard still runs `gremlins unleash .` from `server/`.
Only mutation targets are narrowed by `--exclude-files`.

Replace `go-api` with these file-level shards:

| Shard | Mutation targets |
|---|---|
| `go-api-core` | `internal/api/api.go`, `converters.go`, `middleware.go`, `wsconn.go`, `handlers_client_errors.go`, `handlers_health.go`, `log_redact.go`, `metrics_assemble.go`, `ratelimit.go` |
| `go-api-auth-admin` | `handlers_auth.go`, `handlers_users.go`, `handlers_groups.go`, `handlers_security_groups.go`, `handlers_security_group_members.go`, `handlers_audit.go`, `handlers_push.go` |
| `go-api-devices` | `handlers_devices.go`, `handlers_device_actions.go`, `handlers_device_correlate.go`, `handlers_device_inventory.go`, `handlers_device_metrics.go`, `handlers_amt.go`, `handlers_relay.go`, `handlers_sessions.go` |
| `go-api-lifecycle` | `handlers_enrollment.go`, `handlers_install.go`, `handlers_updates.go` |

Keep existing non-API shards:

- `go-db`
- `go-pure-1`
- `go-pure-2`

Update `scripts/lib/mutation-shards.sh`:

- `mutation_go_shards` returns:
  `go-api-core go-api-auth-admin go-api-devices go-api-lifecycle go-db go-pure-1 go-pure-2`
- Introduce `mutation_go_shard_units <shard>` returning units, where a unit is one of:
  - `pkg:<name>` for `server/internal/<name>/`
  - `file:internal/api/<file>.go` for API files
- Build each shard's exclude regex by excluding every unit not assigned to that shard.
- Continue excluding global carve-outs:
  - `openapi_gen.go`
  - `cmd/meshserver/main.go`
  - `tests/loadtest/main.go`
  - `internal/testutil/`
- Do not mutate API test files directly. The shard target list is source files only.

Update `scripts/tests/mutation-workflow.test.sh`:

- Assert every non-excluded `server/internal/<pkg>` is assigned exactly once, except
  `api`, which is represented by file units.
- Assert every non-generated source file in `server/internal/api/*.go` is assigned to
  exactly one API shard or explicitly excluded.
- Assert generated `internal/api/openapi_gen.go` remains globally excluded.
- Assert each shard's regex excludes all other units and does not exclude its own units.
- Assert workflow matrix Go shard IDs match the shard library.

Update `Makefile` only if `make mutate-go` assumes package-only shards. It should keep
using `mutation_go_shards` and `mutation_go_shard_exclude_regex`; no local behavior
change is needed beyond compatibility with the new unit model.

### 4. Repair Go score regressions

Prioritize tests that kill the largest surviving/not-covered clusters from run #66.

Go order:

1. `server/internal/correlate`
   - Add tests for default baseline window start/end.
   - Add tests for explicit baseline window validation.
   - Add tests for min sample cutoff boundaries.
   - Add tests for max-points truncation.
   - Add tests for top-N clamping and ranking ties.
   - Add KS/anomaly/shift math tests for equal distributions, one-point shifts,
     zero-variance baseline, all-zero baseline, and saturation at 1.0.
2. `server/internal/api/metrics_assemble.go`
   - Add boundary tests for point limit, empty series, single-point series, provenance
     fields, and min/max source selection.
   - Keep expected JSON/API behavior unchanged; tests should observe existing behavior.
3. `server/internal/agentapi`
   - Add tests for registration/handshake/capability branches currently not covered.
   - Add telemetry connection tests for payload cap, drop/accept branches, and server-owned
     org/device scoping.
4. Re-run Go mutation manually if feasible, otherwise rely on the next workflow run after
   liveness restoration.

Do not tune `server/.gremlins.yaml` timeout coefficient in this plan. The current coefficient
is already documented as a stability fix; this recovery should come from sharding and tests.

### 5. Repair Rust score regressions

Prioritize `edge-tsdb` first because it caused the most recent Rust runtime growth and has many
missed mutants in the July 10 partial artifact.

Rust order:

1. `agent/crates/edge-tsdb`
   - Append-only tests: byte cap enforcement, segment rotation, range filtering, total sample
     accounting, and size-on-disk reporting.
   - Baseline store tests: open default state, range inclusivity/exclusivity, empty range.
   - Bit I/O tests: cross-byte writes/reads, partial-byte flush, multi-bit round trips.
   - Corpus tests: deterministic seed output, counter monotonicity, sticky gauge behavior.
2. `agent/crates/mesh-agent-core`
   - Connection/backoff tests: jitter bounds, reconnect elapsed math, max-attempt comparison
     boundaries.
   - File ops tests: `NotFound` handling for directory and file metadata paths.
   - Session tests: ping, close, cleanup, and receive-loop observable effects.
   - Terminal tests: default shell and resize/read loop observable branches.
   - ML tests: sampler arithmetic, k-means farthest-pair and nearest-center boundaries,
     anomaly-window empty/full behavior, and redaction edge cases.
3. Preserve the existing equivalent-mutant note for file-upload behavior unless implementation
   adds an observable upload side effect.

### 6. Maintain Web above floor

Web is stable but close to the floor. Do this after Rust/Go liveness and score fixes:

- `DeviceList.tsx`: add tests for empty/grouped device lists, action button visibility,
  selection/filter branches, and disabled states.
- `report-error.ts`: add tests for error classification, optional fields, ignored branches,
  and fallback message behavior.
- `Permissions.tsx`: add tests for role/permission rendering branches and empty permission
  arrays.

Do not make Web changes part of the first recovery PR if Rust/Go are still unstable; keep it
as the second PR if necessary.

## TDD and execution order

1. Add failing shell tests for Rust merge completeness (`end_time == null`), 4 Rust shards,
   API file-level Go sharding, and incomplete-run status metrics.
2. Implement workflow/script changes until `./scripts/tests/mutation-workflow.test.sh` passes.
3. Add Go mutation-targeted tests in the order above.
4. Add Rust mutation-targeted tests in the order above.
5. Add Web maintenance tests if Rust/Go are stable or in a follow-up PR.
6. Run local verification.
7. Trigger `mutation.yml` manually and inspect:
   - all shards complete
   - canonical row artifact exists
   - VM has `mutation_score` rows for all three languages
   - VM has `mutation_run_complete=1`
   - VM has `mutation_shard_complete=1` for every shard

## Verification commands

Run before committing:

```bash
./scripts/tests/mutation-workflow.test.sh
cd server && go test ./internal/correlate ./internal/api ./internal/agentapi
cd agent && cargo test -p edge-tsdb -p mesh-agent-core
cd web && npm test -- DeviceList report-error Permissions
```

If only workflow/scripts changed in the first PR, the minimum local gate is:

```bash
./scripts/tests/mutation-workflow.test.sh
make shell-quality
```

After merge, manually dispatch Mutation Testing and verify in GitHub Actions plus VM. The
tech-debt item is not paid down until three consecutive scheduled runs complete and push all
language scores.

## Gotchas and constraints

- Do not lower the `85.0%` floor or `2.0pp` drop threshold.
- Do not remove `edge-tsdb` from mutation testing to reduce runtime. Shard it and test it.
- Do not let an incomplete Rust shard publish a partial score; `end_time == null` means
  incomplete even when `outcomes.json` exists.
- Do not turn VM transport failures into score regressions. Status metric push is
  observability; summarize exit code remains the score/incomplete source of truth.
- Keep `fail-fast: false`; one failed shard should not cancel visibility for other shards.
- Preserve the current incomplete-vs-regression split:
  - summarize exit `2` = incomplete/malformed input
  - summarize exit `1` = real score regression
  - summarize exit `0` = clean

## Reviewer checklist

- [ ] Rust workflow matrix has exactly four `cargo mutants --shard k/4` legs.
- [ ] Publish expects exactly those four Rust outcome files.
- [ ] `mutation-merge-rust.sh` rejects missing, malformed, and `end_time == null` inputs.
- [ ] Go shard IDs in workflow match `scripts/lib/mutation-shards.sh`.
- [ ] API source files are partitioned exactly once; `openapi_gen.go` is excluded.
- [ ] Incomplete runs push `mutation_run_complete=0` and per-shard status metrics.
- [ ] Complete runs still upload `mutation-canonical-row` and push score/count metrics.
- [ ] Go and Rust targeted tests address the highest non-killed clusters from run #66.
- [ ] `.claude/techdebt.md` is updated only after implementation, not before evidence.
