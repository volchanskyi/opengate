# PR-1 — Remove log-rate, orphaned host collectors, `AgentSelf`; repoint the AgentMetricWindow golden + loadtest to host-metric dims

Micro-plan of [system-logs-and-central-host-metrics.md](system-logs-and-central-host-metrics.md).
Self-contained; implementable without the master plan open.

## Objective

Delete the unused host **log-rate** feature and everything left dead by its
removal, and **repoint** the `AgentMetricWindow` golden + loadtest telemetry from
`log.rate.*` to the host-metric dims PR-2 will emit. This is a removal +
fixture-repoint PR: no new runtime behavior. After it, the agent no longer emits
any `AgentMetricWindow` (the field is dormant until PR-2).

## Decisions carried in (locked)

- Host log-rate analysis is **not wanted** — delete it entirely.
- `LogSource::AgentSelf` and the journald/Windows collectors are **only** used by
  log-rate; after removal they are orphaned and fail `clippy -D warnings`
  (dead code), so **delete them here** — keep only `redact_entries` (still used by
  the Agent Logs path). PR-3 rebuilds host collection purpose-built.
- **Repoint** the golden + loadtest to host-metric dims now (not delete-only), so
  `AgentMetricWindow` encoding coverage is continuous.

## Preconditions

`git checkout dev && git pull --rebase origin dev`. Confirm the deletion surface
is still current: `grep -rnE "log_rate|LogRate|LOG_RATE|spawn_log_readers" agent/crates --include=*.rs`.

## Proof the deletion is safe

- Log-rate is telemetry-only; no detector consumes it (the ensemble is
  `EdgeMlEnsemble<3>` over `[cpu,mem,disk]`) — [edge_sentinel.rs:340-366](../../../agent/crates/mesh-agent/src/edge_sentinel.rs#L340-L366).
- `collect_host_logs` / `LogSource::AgentSelf` have **no** non-test caller besides
  `spawn_log_readers`; Agent Logs reads its own files via `LogCollector` directly
  — [main.rs:829](../../../agent/crates/mesh-agent/src/main.rs#L829).
- `redact_entries` **is** used by the Agent Logs answer path — [main.rs:842](../../../agent/crates/mesh-agent/src/main.rs#L842) — so it (and its `redact_log_line` helper) stays.

## File inventory

| File | Change |
|---|---|
| `agent/crates/mesh-agent-core/src/ml/log_rate.rs` | **Delete file.** |
| `agent/crates/mesh-agent-core/src/ml/mod.rs` | Remove `pub mod log_rate;`. |
| `agent/crates/mesh-agent/src/host_logs.rs` | Reduce to `redact_entries` (+ its helpers/tests) **only**. Delete: `log_rate_vector`, `log_rate_dims`, `build_log_rate_window`, `LOG_RATE_FIELD_LABELS`, `source_label`, `MAX_HOST_LINES`, `LogSource`, `collect_host_logs`, `collect_journald`, `collect_windows_events`, `parse_journald_json`, `parse_windows_event_json`, `parse_windows_events`, `read_journald_lines`, `journald_priority_to_level`, `windows_level_to_label`, `realtime_micros_to_iso`, `json_str`, and their tests. Prune now-unused imports (`LogRateExtractor`, `LOG_RATE_DIMS`, `MetricDim`, `LogCollector`/`LogFilter`, `serde_json`, `BufRead`/`io`, `Path`, `chrono`); keep `mesh_protocol::LogEntry` + `redact_log_line`. |
| `agent/crates/mesh-agent/src/edge_sentinel.rs` | Remove the `use crate::host_logs::{build_log_rate_window, collect_host_logs, LogSource}` import, `LOG_READER_INTERVAL`, `LOG_SOURCES`, and `spawn_log_readers`. |
| `agent/crates/mesh-agent/src/main.rs` | Remove `LOG_RATE_TELEMETRY_CAP` (l.269), the `log_rate_tx/log_rate_rx` channel (l.491-492), the `spawn_log_readers` call (l.493-494), and the `log_rate_rx` drain line inside the heartbeat (l.701). Keep the discovery + health drains. |
| `agent/crates/mesh-agent-core/benches/edge_sentinel_bench.rs` | Remove `bench_log_rate_window_fold`, the `log_rate::LogRateExtractor` import, and its `criterion_group!` entries (l.125, l.133). |
| `agent/crates/mesh-agent/tests/cli_flags.rs` | Rewrite the l.3 comment to current state (drop "host log-rate readers"); confirm the test still passes (it asserts startup, not log-rate). |
| `agent/crates/mesh-agent-core/src/maintenance.rs` | Rewrite the l.5-6 doc comment to current state (drop "log-rate collectors"/"log-rate"). Describe what is suppressed now: sampler, discovery. |
| `agent/crates/mesh-protocol/tests/golden_test.rs` | In the `AgentMetricWindow` golden builder, replace the `log.rate.*` dims with host-metric dims (`cpu.total`, `mem.used_percent`, `disk.used_percent`, `net.rx_bytes`, `net.tx_bytes`) with representative avgs; rename the variant → `control_agent_metric_window_host_metrics`. |
| `testdata/golden/control_agent_metric_window_log_rates.{bin,meta.json}` | Rename → `control_agent_metric_window_host_metrics.{bin,meta.json}`; regenerate the `.bin` (below). |
| `server/internal/protocol/golden_part2_test.go` (l.21) | Update the registration row: name `AgentMetricWindowHostMetrics`, file `control_agent_metric_window_host_metrics.bin`. |
| `server/internal/protocol/golden_part7_test.go` (l.93-114) | Rename → `TestGoldenControlAgentMetricWindowHostMetrics`; assert the host-metric dims + values. |
| `server/tests/loadtest/soak_telemetry.go` (+ `soak.go`/`main.go` if they name `log.rate`) | Replace emitted `log.rate.*` dims with host-metric dims; keep the loadtest's own tests (`soak_test.go`, `main_test.go`) green. |

## TDD-ordered steps

1. **RED (test first):** rewrite `golden_part7_test.go` to expect the host-metric
   dims. Fails against the still-log-rate fixture — satisfies the TDD gate.
2. Update the Rust generator `golden_test.rs` (host-metric dims + variant rename).
3. **Regenerate fixtures**, then rename to `..._host_metrics.*`:
   - `cd agent && GENERATE_GOLDEN=1 cargo test -p mesh-protocol --test golden_test`
   - `cd server && GENERATE_GOLDEN=1 go test ./internal/protocol -run TestGenerate`
4. Update the `golden_part2_test.go` registration row.
5. Delete/reduce source: `log_rate.rs`, `mod.rs`, `host_logs.rs`, `edge_sentinel.rs`,
   `main.rs`, the bench, and the two comments.
6. Repoint the loadtest dims.
7. Verify: `make golden` (cross-language agreement), `make lint` (**dead-code via
   `-D warnings`** — the real gate for the orphan removal), `make test` (Rust + Go
   + web), `cd agent && cargo bench --no-run` (benches compile).
8. `/precommit` → commit → `/refactor` → `/precommit` → commit → push.
9. In the final commit: `git mv` this micro-plan to `plans/archive/`, bump its
   internal links one `../` deeper, validate with
   `GO111MODULE=off go run ./scripts/check-doc-links` (per [plans-and-adrs.md](../../rules/plans-and-adrs.md)).

## Edge / error cases

- **Dead-code sweep is the acceptance gate.** After the deletion, `make lint`
  must be clean with no `#[allow(dead_code)]` parking. If anything is still
  flagged, it means a helper is orphaned — delete it too (or it's genuinely used,
  in which case keep it).
- **`redact_entries` must survive** — grep confirms Agent Logs uses it; its test
  stays.
- **Golden drift:** `make golden` fails if the Rust generator and Go decoder
  disagree — regenerate both sides, never hand-edit the `.bin`.
- **No VM migration:** stale `log.rate.*` series in VM age out via retention (out
  of scope; no operational script).

## Out of scope

- The live host-metric emitter (PR-2) — this PR only repoints the *fixture/loadtest*
  dims; no agent code emits `AgentMetricWindow` after this PR.
- Rebuilding host collectors (PR-3).

## Reviewer checklist

- [ ] `make lint` clean — **zero** dead-code warnings, **zero** new `#[allow(dead_code)]`.
- [ ] `make golden` green; `.bin`/`.meta.json` regenerated (not hand-edited); both variants renamed.
- [ ] `host_logs.rs` contains only `redact_entries` (+ helpers/tests); Agent Logs path still compiles/works.
- [ ] No `log_rate|LogRate|LOG_RATE|spawn_log_readers|LogSource|collect_host_logs` remain (grep).
- [ ] Comments (`cli_flags.rs`, `maintenance.rs`) describe **current** state only (no removed-feature narration — [docs-live-state.md](../../rules/docs-live-state.md)).
- [ ] Loadtest emits host-metric dims; its tests pass.
- [ ] TDD honored (golden test edited before source); this micro-plan archived in the final commit.

## DoD

`/precommit` green (incl. sonar, dead-code, golden, shell, mutation floors held on
changed Rust/Go), `/refactor`, pushed to `dev`, micro-plan archived. No phases.md
row here — the workstream's Completed row + master-plan archive land in PR-3.
