#!/usr/bin/env bash
# Single source of truth for mutation-test shard ids and Go mutation scope.
#
# Every Go shard runs `gremlins unleash .` from server/ so the coverage dry-run
# remains module-wide. The per-shard exclude regexp narrows only the source files
# mutated by that shard. Units are repository-relative to server/:
#   dir:<path>   every non-test Go source below a directory
#   file:<path>  one source file (used to split internal/api)
#
# scripts/tests/mutation-workflow.test.sh proves every non-test server Go source
# is covered by exactly one unit or by the global carve-outs. This prevents
# sources outside internal/ (notably tests/loadtest) from being mutated and
# counted once per shard.

# The Rust leg is split by scope, the same way the Go leg is: each shard names
# the behavior it mutates and owns a fixed set of sources.
#
# Naming the scope is what makes a red leg readable — "rust-core-alerts-retro
# failed" says which code lost coverage, where a slice number says only that one
# sixteenth of an interleaved list did. It is also what keeps a shard cheap:
# cargo-mutants rebuilds between mutants, so consecutive mutants inside one file
# reuse the incremental build, while an interleaved list rebuilds a different
# crate almost every time.
#
# Sizing is measured, not guessed. A shard's wall-clock is its mutant count times
# the per-mutant cost of its package, and that cost differs by an order of
# magnitude between packages: from completed runs, a mesh-agent-core mutant costs
# ~0.59 min (114 mutants in 70 min) against ~0.05 for edge-tsdb and ~0.12 for
# mesh-agent, because every mesh-agent-core mutant relinks the crate's 26 test
# binaries. Against the 75-minute cap, minus ~3 min of toolchain install and
# baseline build and 15 min of headroom, that puts a mesh-agent-core shard's
# ceiling near 120 mutants — which is why its ~1480 are split twenty ways
# while edge-tsdb's ~900 need three and mesh-agent and mesh-protocol one each.
#
# The `mutants` cargo profile (agent/Cargo.toml) drops debug info from that
# relink and takes roughly a fifth off the per-mutant cost; the ceiling above is
# stated without it, so it is headroom rather than budget already spent.
mutation_rust_shards() {
  # Not named `shards`: scripts/mutation-status-build.sh sources this library and
  # keeps a string by that name, and ShellCheck follows the source.
  local ids=(
    rust-core-ml-backfill-drain
    rust-core-ml-backfill-tiers
    rust-core-ml-sampling
    rust-core-ml-host-sources
    rust-core-ml-store-sink
    rust-core-ml-analysis
    rust-core-ml-redaction
    rust-core-alerts-retro-plan
    rust-core-alerts-retro-scan
    rust-core-alerts-conditions
    rust-core-alerts-evaluator
    rust-core-alerts-event
    rust-core-alerts-sink
    rust-core-correlate-divergence
    rust-core-correlate-ranking
    rust-core-session-terminal
    rust-core-session-dispatch
    rust-core-discovery
    rust-core-runtime-lifecycle
    rust-core-runtime
    rust-tsdb-blocks
    rust-tsdb-encoding
    rust-tsdb-substrates
    rust-agent-loops
    rust-protocol-wire
  )
  echo "${ids[*]}"
}

# The cargo package a shard mutates. cargo-mutants runs the tests of the mutated
# package only, so the package is also what the shard's per-mutant test cost is.
mutation_rust_shard_package() {
  case "$1" in
    rust-core-ml-backfill-drain | rust-core-ml-backfill-tiers) echo "mesh-agent-core" ;;
    rust-core-ml-sampling | rust-core-ml-host-sources | rust-core-ml-store-sink) echo "mesh-agent-core" ;;
    rust-core-ml-analysis | rust-core-ml-redaction) echo "mesh-agent-core" ;;
    rust-core-alerts-retro-plan | rust-core-alerts-retro-scan) echo "mesh-agent-core" ;;
    rust-core-alerts-conditions | rust-core-alerts-evaluator) echo "mesh-agent-core" ;;
    rust-core-alerts-event | rust-core-alerts-sink) echo "mesh-agent-core" ;;
    rust-core-correlate-divergence | rust-core-correlate-ranking) echo "mesh-agent-core" ;;
    rust-core-session-terminal | rust-core-session-dispatch) echo "mesh-agent-core" ;;
    rust-core-discovery | rust-core-runtime-lifecycle | rust-core-runtime) echo "mesh-agent-core" ;;
    rust-tsdb-blocks | rust-tsdb-encoding | rust-tsdb-substrates) echo "edge-tsdb" ;;
    rust-agent-loops) echo "mesh-agent" ;;
    rust-protocol-wire) echo "mesh-protocol" ;;
    *)
      echo "unknown mutation shard: $1" >&2
      return 1
      ;;
  esac
}

# Sources a shard owns, as units repository-relative to agent/:
#   dir:<path>   every source below a directory
#   file:<path>  one source file
# The literal `rest` marks a package's catch-all shard, which owns everything its
# siblings do not claim. Every package has exactly one, so a source added
# tomorrow is mutated the day it lands instead of falling through the map.
mutation_rust_shard_units() {
  case "$1" in
    rust-core-ml-backfill-drain)
      echo "file:crates/mesh-agent-core/src/ml/backfill/drain.rs"
      ;;
    rust-core-ml-backfill-tiers)
      echo "file:crates/mesh-agent-core/src/ml/backfill/mod.rs"
      ;;
    rust-core-ml-sampling)
      echo "file:crates/mesh-agent-core/src/ml/sampler.rs file:crates/mesh-agent-core/src/ml/host_metric_stream.rs file:crates/mesh-agent-core/src/ml/window.rs"
      ;;
    rust-core-ml-host-sources)
      echo "file:crates/mesh-agent-core/src/ml/diskperf.rs file:crates/mesh-agent-core/src/ml/pressure.rs file:crates/mesh-agent-core/src/ml/primary_iface.rs file:crates/mesh-agent-core/src/ml/cgroup.rs"
      ;;
    rust-core-ml-store-sink)
      echo "file:crates/mesh-agent-core/src/ml/store_sink.rs"
      ;;
    rust-core-ml-analysis)
      echo "file:crates/mesh-agent-core/src/ml/kmeans.rs file:crates/mesh-agent-core/src/ml/ensemble.rs file:crates/mesh-agent-core/src/ml/mod.rs"
      ;;
    rust-core-ml-redaction)
      echo "file:crates/mesh-agent-core/src/ml/redact.rs"
      ;;
    rust-core-alerts-retro-plan)
      echo "file:crates/mesh-agent-core/src/alerts/retro/mod.rs"
      ;;
    rust-core-alerts-retro-scan)
      echo "file:crates/mesh-agent-core/src/alerts/retro/scan.rs"
      ;;
    rust-core-alerts-conditions)
      echo "file:crates/mesh-agent-core/src/alerts/evaluator/condition.rs"
      ;;
    rust-core-alerts-evaluator)
      echo "file:crates/mesh-agent-core/src/alerts/evaluator/mod.rs"
      ;;
    rust-core-alerts-event)
      echo "file:crates/mesh-agent-core/src/alerts/event.rs file:crates/mesh-agent-core/src/alerts/mod.rs"
      ;;
    rust-core-alerts-sink)
      echo "file:crates/mesh-agent-core/src/alerts/sink.rs file:crates/mesh-agent-core/src/alerts/evidence.rs"
      ;;
    rust-core-correlate-divergence) echo "file:crates/mesh-agent-core/src/correlate/ks.rs" ;;
    rust-core-correlate-ranking)
      echo "file:crates/mesh-agent-core/src/correlate/rank.rs file:crates/mesh-agent-core/src/correlate/window.rs file:crates/mesh-agent-core/src/correlate/mod.rs"
      ;;
    rust-core-session-terminal)
      echo "file:crates/mesh-agent-core/src/session/terminal_handle.rs"
      ;;
    rust-core-session-dispatch)
      echo "dir:crates/mesh-agent-core/src/session/handlers file:crates/mesh-agent-core/src/session/handler.rs file:crates/mesh-agent-core/src/session/relay.rs file:crates/mesh-agent-core/src/session/mod.rs"
      ;;
    rust-core-discovery) echo "dir:crates/mesh-agent-core/src/discovery" ;;
    rust-core-runtime-lifecycle)
      echo "file:crates/mesh-agent-core/src/update.rs file:crates/mesh-agent-core/src/maintenance.rs file:crates/mesh-agent-core/src/identity.rs file:crates/mesh-agent-core/src/platform.rs file:crates/mesh-agent-core/src/terminal.rs"
      ;;
    rust-core-runtime) echo "rest" ;;
    rust-tsdb-blocks)
      echo "dir:crates/edge-tsdb/src/store file:crates/edge-tsdb/src/compact.rs file:crates/edge-tsdb/src/deflate.rs"
      ;;
    rust-tsdb-encoding)
      echo "file:crates/edge-tsdb/src/gorilla.rs file:crates/edge-tsdb/src/bitio.rs file:crates/edge-tsdb/src/crc.rs file:crates/edge-tsdb/src/frame.rs file:crates/edge-tsdb/src/tier.rs"
      ;;
    rust-tsdb-substrates) echo "rest" ;;
    rust-agent-loops) echo "rest" ;;
    rust-protocol-wire) echo "rest" ;;
    *)
      echo "unknown mutation shard: $1" >&2
      return 1
      ;;
  esac
}

mutation_rust_unit_matches() {
  local unit="${1:?mutation unit required}"
  local source="${2:?source path required}"

  case "$unit" in
    dir:*)
      local dir="${unit#dir:}"
      [[ "$source" == "$dir/"* ]]
      ;;
    file:*) [[ "$source" == "${unit#file:}" ]] ;;
    *) return 1 ;;
  esac
}

# A unit as a cargo-mutants path glob. Globs containing a slash match the whole
# workspace-relative path, which every unit here does.
mutation_rust_shard_glob() {
  local unit="${1:?mutation unit required}"

  case "$unit" in
    dir:*) printf '%s/**' "${unit#dir:}" ;;
    file:*) printf '%s' "${unit#file:}" ;;
    *)
      echo "unknown mutation unit: $unit" >&2
      return 1
      ;;
  esac
}

# The cargo-mutants arguments selecting one shard's mutants, one per line so a
# caller can read them into an array without re-splitting paths.
mutation_rust_shard_args() {
  local shard="${1:?mutation shard required}"
  local pkg units other other_units unit

  pkg="$(mutation_rust_shard_package "$shard")" || return 1
  units="$(mutation_rust_shard_units "$shard")" || return 1
  printf -- '--package\n%s\n' "$pkg"

  if [ "$units" = "rest" ]; then
    for other in $(mutation_rust_shards); do
      [ "$other" = "$shard" ] && continue
      [ "$(mutation_rust_shard_package "$other")" = "$pkg" ] || continue
      other_units="$(mutation_rust_shard_units "$other")"
      [ "$other_units" = "rest" ] && continue
      for unit in $other_units; do
        printf -- '--exclude\n%s\n' "$(mutation_rust_shard_glob "$unit")"
      done
    done
    return 0
  fi

  for unit in $units; do
    printf -- '--file\n%s\n' "$(mutation_rust_shard_glob "$unit")"
  done
}

# What one shard is allowed to project to, in minutes. The job cap is 75; a run
# pays about 3 of those for the toolchain install and the unmutated baseline, and
# 15 are held back as headroom, because a shard sized to finish at 74 minutes is
# a shard that fails the first time a runner is slow.
mutation_rust_shard_budget_minutes() {
  echo 57
}

# The measured cost of one mutant, in thousandths of a minute, by package.
#
# These come from completed nightly shards — mesh-agent-core's 114-mutant alerts
# leg in 70 min, edge-tsdb's 350 in 17.8, mesh-agent's 264 in 32, mesh-protocol's
# 83 in 6 — each then scaled by the 0.78 the `mutants` cargo profile takes off a
# rebuild (agent/Cargo.toml). The spread is not noise: cargo-mutants relinks the
# mutated package's test binaries once per mutant, and mesh-agent-core has 26 of
# them, so a mutant there costs an order of magnitude more than one in edge-tsdb.
#
# Re-measure from a nightly run rather than adjusting these to make a shard fit.
mutation_rust_package_milliminutes_per_mutant() {
  case "$1" in
    mesh-agent-core) echo 460 ;;
    edge-tsdb) echo 40 ;;
    mesh-agent) echo 94 ;;
    mesh-protocol) echo 56 ;;
    *)
      echo "unknown mutation package: $1" >&2
      return 1
      ;;
  esac
}

mutation_web_shards() {
  echo "web"
}

mutation_go_shards() {
  echo "go-api-runtime go-api-intake go-api-converters go-api-identity go-api-tenancy-admin go-api-device-control go-api-device-reads go-api-incidents go-api-rules go-api-enrollment go-api-updates-purge go-agentapi-connection go-agentapi-handshake go-agentapi-backfill go-agentapi-edge-telemetry go-domain-detection go-domain-persistence go-amt go-updates-certificates go-protocol-relay go-observability-harness go-composition-root"
}

mutation_all_shards() {
  echo "$(mutation_rust_shards) $(mutation_go_shards) $(mutation_web_shards)"
}

# A CLI -E overrides server/.gremlins.yaml exclude-files, so every sharded run
# must restate the generated code, entry points, and shared test scaffolding.
#
# cmd/meshserver is excluded as a package rather than as one file: every source
# in it is the process's own wiring — flag parsing, construction order, and the
# goroutines that start the periodic workers — and a mutant there changes how the
# process is assembled rather than what any behavior does. Splitting that wiring
# across files for readability must not quietly enrol it in mutation testing.
mutation_go_global_excludes() {
  echo 'openapi_gen\.go|cmd/meshserver/|tests/loadtest/main\.go|internal/testutil/|internal/faulttest/'
}

mutation_go_shard_units() {
  case "$1" in
    go-api-runtime)
      echo "file:internal/api/api.go file:internal/api/middleware.go file:internal/api/wsconn.go file:internal/api/ratelimit.go"
      ;;
    go-api-intake)
      echo "file:internal/api/validate.go file:internal/api/log_redact.go file:internal/api/handlers_client_errors.go file:internal/api/handlers_health.go file:internal/api/metrics_assemble.go"
      ;;
    go-api-converters)
      echo "file:internal/api/converters.go file:internal/api/converters_incidents.go file:internal/api/converters_rules.go"
      ;;
    go-api-identity)
      echo "file:internal/api/handlers_auth.go file:internal/api/handlers_users.go file:internal/api/handlers_security_groups.go file:internal/api/handlers_security_group_members.go file:internal/api/handlers_audit.go"
      ;;
    go-api-tenancy-admin)
      echo "file:internal/api/handlers_organizations.go file:internal/api/handlers_sites.go file:internal/api/handlers_device_tags.go file:internal/api/handlers_alert_limits.go file:internal/api/handlers_push.go"
      ;;
    go-api-device-control)
      echo "file:internal/api/handlers_devices.go file:internal/api/handlers_device_actions.go file:internal/api/handlers_maintenance.go file:internal/api/handlers_amt.go file:internal/api/handlers_sessions.go file:internal/api/handlers_relay.go"
      ;;
    go-api-device-reads)
      echo "file:internal/api/handlers_device_summary.go file:internal/api/handlers_device_inventory.go file:internal/api/handlers_device_metrics.go file:internal/api/handlers_device_history.go"
      ;;
    go-api-incidents)
      echo "file:internal/api/handlers_incidents.go file:internal/api/handlers_incident_moves.go file:internal/api/handlers_incident_evidence.go"
      ;;
    go-api-rules)
      echo "file:internal/api/handlers_rules.go file:internal/api/handlers_rules_admin.go file:internal/api/handlers_rules_read.go file:internal/api/handlers_rules_tuning.go file:internal/api/handlers_rules_rollout.go"
      ;;
    go-api-enrollment)
      echo "file:internal/api/handlers_enrollment.go file:internal/api/handlers_install.go"
      ;;
    go-api-updates-purge)
      echo "file:internal/api/handlers_updates.go file:internal/api/handlers_purge.go"
      ;;
    go-agentapi-connection)
      echo "file:internal/agentapi/conn.go file:internal/agentapi/conn_guard.go file:internal/agentapi/conn_maintenance.go file:internal/agentapi/server.go file:internal/agentapi/server_connection.go file:internal/agentapi/deregister.go"
      ;;
    go-agentapi-handshake)
      echo "file:internal/agentapi/handshaker.go file:internal/agentapi/errors.go"
      ;;
    go-agentapi-backfill)
      echo "file:internal/agentapi/backfill_scheduler.go file:internal/agentapi/conn_backfill.go"
      ;;
    go-agentapi-edge-telemetry)
      echo "file:internal/agentapi/conn_discovery.go file:internal/agentapi/conn_telemetry.go file:internal/agentapi/conn_accounting.go file:internal/agentapi/conn_coverage.go file:internal/agentapi/conn_logs.go file:internal/agentapi/conn_history.go file:internal/agentapi/conn_hardware.go file:internal/agentapi/alert_breach.go file:internal/agentapi/alert_rules.go file:internal/agentapi/conn_alerts.go file:internal/agentapi/alert_rules_catalogue.go file:internal/agentapi/vitals.go"
      ;;
    go-domain-detection)
      echo "dir:internal/rules dir:internal/alerts"
      ;;
    go-domain-persistence)
      echo "dir:internal/auth dir:internal/db dir:internal/dbtx dir:internal/device dir:internal/inventory dir:internal/lifecycle dir:internal/organization dir:internal/settings dir:internal/session dir:internal/audit dir:internal/usecase"
      ;;
    go-amt)
      echo "dir:internal/amt"
      ;;
    go-updates-certificates)
      echo "dir:internal/updater dir:internal/cert dir:internal/notifications"
      ;;
    go-protocol-relay)
      echo "dir:internal/protocol dir:internal/relay dir:internal/signaling dir:internal/clientapi dir:internal/osutil"
      ;;
    go-observability-harness)
      echo "dir:internal/telemetry dir:internal/metrics dir:internal/testpg dir:internal/testvm dir:tests/loadtest"
      ;;
    # The composition root. It is mutated rather than carved out: a dry run
    # measured 23 mutants there, and what they land on is behavior a test can
    # state — the refusals that name a missing dependency, the all-or-nothing
    # wiring of the metrics store's four faces, and the fallback that leaves
    # device deletion as a plain Postgres delete when there are no series to
    # purge. The acceptance suite stands the assembly up, so a mutant that
    # unwires a port has somewhere to be killed.
    go-composition-root)
      echo "dir:internal/app"
      ;;
    *)
      echo "unknown mutation shard: $1" >&2
      return 1
      ;;
  esac
}

# What one Go mutant costs the shard that owns it, in seconds.
#
# gremlins re-runs the test packages that cover the mutated line, so the cost is
# a property of those tests rather than of the source: an internal/api handler
# mutant re-pays the Postgres-backed API suite and the integration tests that
# reach it (~41-51 s), while an internal/agentapi mutant re-runs an in-process
# harness (~4 s). Ignoring that spread is what makes a shard count meaningless as
# a size — 95 API mutants and 593 domain mutants are the same 70 minutes.
#
# These come from completed nightly shards (elapsed_time / mutants_total in each
# shard's mutation-report JSON), rounded up so a shard is never projected cheaper
# than it ran. A shard carved out of another inherits its parent's rate until a
# nightly measures it on its own. Re-measure from a run rather than lowering a
# number to make a shard fit.
mutation_go_shard_seconds_per_mutant() {
  case "$1" in
    go-api-runtime | go-api-enrollment | go-api-updates-purge) echo 51 ;;
    go-api-intake) echo 27 ;;
    go-api-converters | go-api-incidents | go-api-rules) echo 44 ;;
    go-api-identity | go-api-tenancy-admin) echo 42 ;;
    go-api-device-control | go-api-device-reads) echo 41 ;;
    go-agentapi-connection | go-agentapi-handshake | go-agentapi-edge-telemetry) echo 5 ;;
    go-agentapi-backfill) echo 3 ;;
    go-domain-detection | go-domain-persistence) echo 8 ;;
    go-amt | go-updates-certificates) echo 11 ;;
    go-protocol-relay | go-observability-harness) echo 10 ;;
    # Postgres-backed like the API shards: every mutant re-pays a schema
    # migration and a full assembly. Rated with them until a nightly measures it.
    go-composition-root) echo 51 ;;
    *)
      echo "unknown mutation shard: $1" >&2
      return 1
      ;;
  esac
}

# Minutes of mutant execution a Go shard may spend. The job cap is 75 minutes;
# a measured Go leg pays about 4.5 of them before the first mutant runs (image
# pull, Postgres, toolchain, gremlins, and the module-wide coverage run), and the
# workflow holds 15 minutes of headroom. What is left is the budget.
mutation_go_shard_budget_minutes() {
  echo 55
}

# Per-shard gremlins timeout-coefficient override. Most shards inherit the
# baseline in server/.gremlins.yaml (empty output => no CLI flag).
#
# The isolated go-agentapi-backfill shard is the exception: its runtime is
# dominated by a handful of conn_backfill.go guard-clause mutants that block
# under the Postgres-backed harness and TIME OUT. gremlins already counts
# TIMED_OUT as caught, so those mutants were never going to be reported as
# survivors — the baseline's multi-minute per-mutant budget only burns wall-clock
# on them and leaves the shard with no headroom under the 75-minute cap. A
# coefficient of 5 still gives a genuine slow Postgres mutant a comfortable
# budget (well above the ~40 s schema re-setup a real test pays) so no would-be
# survivor is falsely credited as caught, while cutting the blocking mutants'
# budget by two-thirds. The baseline stays high globally because the wide
# Postgres domain shards need it to avoid false timeouts inflating their score.
#
# A coefficient is a bound, not a cure: where a mutant blocks because the test
# harness has no deadline of its own, the deadline is what to fix — a bounded
# harness kills the same mutant in seconds instead of minutes.
mutation_go_shard_timeout_coefficient() {
  case "$1" in
    go-agentapi-backfill) echo "5" ;;
    *) echo "" ;;
  esac
}

mutation_go_unit_matches() {
  local unit="${1:?mutation unit required}"
  local source="${2:?source path required}"

  case "$unit" in
    dir:*)
      local dir="${unit#dir:}"
      [[ "$source" == "$dir/"* ]]
      ;;
    file:*) [[ "$source" == "${unit#file:}" ]] ;;
    *) return 1 ;;
  esac
}

mutation_go_unit_regex() {
  local unit="${1:?mutation unit required}"
  local path

  case "$unit" in
    dir:*) path="${unit#dir:}/" ;;
    file:*) path="${unit#file:}" ;;
    *)
      echo "unknown mutation unit: $unit" >&2
      return 1
      ;;
  esac

  case "$path" in
    *[!a-zA-Z0-9_./-]*)
      echo "unsupported character in mutation unit: $unit" >&2
      return 1
      ;;
  esac
  path="${path//./\\.}"
  if [[ "$unit" == file:* ]]; then
    path="$path\$"
  fi
  printf '%s' "$path"
}

mutation_go_shard_exclude_regex() {
  local shard="${1:?mutation shard required}"
  local other unit part
  local regex

  mutation_go_shard_units "$shard" >/dev/null || return 1
  regex="$(mutation_go_global_excludes)"
  for other in $(mutation_go_shards); do
    [[ "$other" == "$shard" ]] && continue
    for unit in $(mutation_go_shard_units "$other"); do
      part="$(mutation_go_unit_regex "$unit")" || return 1
      regex="$regex|$part"
    done
  done
  printf '%s\n' "$regex"
}
