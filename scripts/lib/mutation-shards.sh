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
# Sizing. Two costs decide whether a shard fits the 75-minute cap: the rebuild,
# which the grouping above keeps small, and the mutated package's test suite,
# which cargo-mutants runs once per mutant. mesh-agent-core carries ~1400 of the
# workspace's ~2700 mutants and gets ten shards; edge-tsdb's ~900 cheap ones get
# three; mesh-agent and mesh-protocol get one each.
mutation_rust_shards() {
  # Not named `shards`: scripts/mutation-status-build.sh sources this library and
  # keeps a string by that name, and ShellCheck follows the source.
  local ids=(
    rust-core-ml-backfill
    rust-core-ml-sampling
    rust-core-ml-analysis
    rust-core-alerts-retro
    rust-core-alerts-evaluator
    rust-core-alerts-dispatch
    rust-core-correlate
    rust-core-session
    rust-core-discovery
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
    rust-core-ml-backfill | rust-core-ml-sampling | rust-core-ml-analysis) echo "mesh-agent-core" ;;
    rust-core-alerts-retro | rust-core-alerts-evaluator | rust-core-alerts-dispatch) echo "mesh-agent-core" ;;
    rust-core-correlate | rust-core-session | rust-core-discovery | rust-core-runtime) echo "mesh-agent-core" ;;
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
    rust-core-ml-backfill)
      echo "file:crates/mesh-agent-core/src/ml/backfill.rs"
      ;;
    rust-core-ml-sampling)
      echo "file:crates/mesh-agent-core/src/ml/sampler.rs file:crates/mesh-agent-core/src/ml/store_sink.rs file:crates/mesh-agent-core/src/ml/host_metric_stream.rs file:crates/mesh-agent-core/src/ml/window.rs file:crates/mesh-agent-core/src/ml/primary_iface.rs file:crates/mesh-agent-core/src/ml/pressure.rs file:crates/mesh-agent-core/src/ml/cgroup.rs"
      ;;
    rust-core-ml-analysis)
      echo "file:crates/mesh-agent-core/src/ml/kmeans.rs file:crates/mesh-agent-core/src/ml/ensemble.rs file:crates/mesh-agent-core/src/ml/diskperf.rs file:crates/mesh-agent-core/src/ml/redact.rs file:crates/mesh-agent-core/src/ml/mod.rs"
      ;;
    rust-core-alerts-retro)
      echo "file:crates/mesh-agent-core/src/alerts/retro.rs"
      ;;
    rust-core-alerts-evaluator)
      echo "file:crates/mesh-agent-core/src/alerts/evaluator.rs"
      ;;
    rust-core-alerts-dispatch)
      echo "file:crates/mesh-agent-core/src/alerts/event.rs file:crates/mesh-agent-core/src/alerts/sink.rs file:crates/mesh-agent-core/src/alerts/evidence.rs file:crates/mesh-agent-core/src/alerts/mod.rs"
      ;;
    rust-core-correlate) echo "dir:crates/mesh-agent-core/src/correlate" ;;
    rust-core-session) echo "dir:crates/mesh-agent-core/src/session" ;;
    rust-core-discovery) echo "dir:crates/mesh-agent-core/src/discovery" ;;
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

mutation_web_shards() {
  echo "web"
}

mutation_go_shards() {
  echo "go-api-runtime go-api-identity-admin go-api-device-operations go-api-investigations go-api-provisioning-lifecycle go-agentapi-connection-handshake go-agentapi-backfill go-agentapi-edge-telemetry go-domain-persistence go-amt-updates-certificates go-protocol-relay-observability"
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
      echo "file:internal/api/api.go file:internal/api/converters.go file:internal/api/middleware.go file:internal/api/wsconn.go file:internal/api/handlers_client_errors.go file:internal/api/handlers_health.go file:internal/api/log_redact.go file:internal/api/metrics_assemble.go file:internal/api/ratelimit.go file:internal/api/validate.go"
      ;;
    go-api-identity-admin)
      echo "file:internal/api/handlers_auth.go file:internal/api/handlers_users.go file:internal/api/handlers_sites.go file:internal/api/handlers_organizations.go file:internal/api/handlers_security_groups.go file:internal/api/handlers_security_group_members.go file:internal/api/handlers_audit.go file:internal/api/handlers_push.go"
      ;;
    go-api-device-operations)
      echo "file:internal/api/handlers_devices.go file:internal/api/handlers_device_summary.go file:internal/api/handlers_device_actions.go file:internal/api/handlers_maintenance.go file:internal/api/handlers_device_history.go file:internal/api/handlers_device_inventory.go file:internal/api/handlers_device_metrics.go file:internal/api/handlers_amt.go file:internal/api/handlers_relay.go file:internal/api/handlers_sessions.go"
      ;;
    go-api-investigations)
      echo "file:internal/api/handlers_incidents.go file:internal/api/handlers_incident_moves.go file:internal/api/handlers_incident_evidence.go file:internal/api/handlers_rules.go file:internal/api/converters_incidents.go"
      ;;
    go-api-provisioning-lifecycle)
      echo "file:internal/api/handlers_enrollment.go file:internal/api/handlers_install.go file:internal/api/handlers_updates.go file:internal/api/handlers_purge.go"
      ;;
    go-agentapi-connection-handshake)
      echo "file:internal/agentapi/conn.go file:internal/agentapi/conn_guard.go file:internal/agentapi/conn_maintenance.go file:internal/agentapi/server.go file:internal/agentapi/server_connection.go file:internal/agentapi/errors.go file:internal/agentapi/handshaker.go file:internal/agentapi/deregister.go"
      ;;
    go-agentapi-backfill)
      echo "file:internal/agentapi/backfill_scheduler.go file:internal/agentapi/conn_backfill.go"
      ;;
    go-agentapi-edge-telemetry)
      echo "file:internal/agentapi/conn_discovery.go file:internal/agentapi/conn_telemetry.go file:internal/agentapi/conn_accounting.go file:internal/agentapi/conn_coverage.go file:internal/agentapi/conn_logs.go file:internal/agentapi/conn_history.go file:internal/agentapi/conn_hardware.go file:internal/agentapi/alert_breach.go file:internal/agentapi/alert_rules.go file:internal/agentapi/conn_alerts.go file:internal/agentapi/alert_rules_catalogue.go file:internal/agentapi/vitals.go"
      ;;
    go-domain-persistence)
      echo "dir:internal/alerts dir:internal/auth dir:internal/db dir:internal/dbtx dir:internal/device dir:internal/inventory dir:internal/lifecycle dir:internal/organization dir:internal/rules dir:internal/settings dir:internal/session dir:internal/audit dir:internal/usecase"
      ;;
    go-amt-updates-certificates)
      echo "dir:internal/amt dir:internal/updater dir:internal/notifications dir:internal/cert"
      ;;
    go-protocol-relay-observability)
      echo "dir:internal/protocol dir:internal/telemetry dir:internal/relay dir:internal/metrics dir:internal/signaling dir:internal/testpg dir:internal/testvm dir:internal/osutil dir:internal/clientapi dir:tests/loadtest"
      ;;
    *)
      echo "unknown mutation shard: $1" >&2
      return 1
      ;;
  esac
}

# Per-shard gremlins timeout-coefficient override. Most shards inherit the
# baseline in server/.gremlins.yaml (empty output => no CLI flag). The isolated
# go-agentapi-backfill shard is the exception: its runtime is dominated by a
# handful of conn_backfill.go guard-clause mutants that block under the
# Postgres-backed harness and TIME OUT. gremlins already counts TIMED_OUT as
# caught, so those mutants were never going to be reported as survivors — the
# baseline's multi-minute per-mutant budget only burns wall-clock on them and
# leaves the shard with no headroom under the 75-minute cap. A coefficient of 5
# still gives a genuine slow Postgres mutant a comfortable budget (well above
# the ~40s schema re-setup a real test pays) so no would-be survivor is falsely
# credited as caught, while cutting the blocking mutants' budget by two-thirds
# and restoring headroom. The baseline stays high globally because the wide
# Postgres domain shards need it to avoid false timeouts inflating their score.
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
