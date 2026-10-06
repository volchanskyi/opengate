#!/usr/bin/env bash
# Mutation-test shard ids and Go scope: a shard runs gremlins from server/ over the narrowest path
# holding its units, each dir:<path> or file:<path> relative to server/.

# The directory of this library, from which the gremlins config sets the per-mutant leash.
MUTATION_SHARDS_LIB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Rust shards are named by scope so a red leg says which code lost coverage, and each owns a fixed
# set of sources so consecutive mutants reuse cargo-mutants' incremental build.
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
    rust-tsdb-surface
    rust-agent-loops
    rust-protocol-wire
  )
  echo "${ids[*]}"
}

# The cargo package a shard mutates; cargo-mutants runs only that package's tests.
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
    rust-tsdb-blocks | rust-tsdb-encoding | rust-tsdb-surface) echo "edge-tsdb" ;;
    rust-agent-loops) echo "mesh-agent" ;;
    rust-protocol-wire) echo "mesh-protocol" ;;
    *)
      echo "unknown mutation shard: $1" >&2
      return 1
      ;;
  esac
}

# Sources a shard owns, as dir:<path> or file:<path> units relative to agent/.
# The literal `rest` marks a package's catch-all shard, which owns every source its siblings leave.
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
      echo "file:crates/edge-tsdb/src/gorilla.rs file:crates/edge-tsdb/src/bitio.rs file:crates/edge-tsdb/src/tier.rs"
      ;;
    # The crate's catch-all; the bake-off substrates are carved out in agent/.cargo/mutants.toml
    # because they sit behind a feature the shipped agent turns off.
    rust-tsdb-surface) echo "rest" ;;
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

# A unit as a cargo-mutants path glob; a glob containing a slash matches the whole workspace path.
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

# The cargo-mutants arguments selecting one shard's mutants, one per line for reading into an array.
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

# What one Rust shard may project to, in minutes: the 90-minute job cap less about 3 for the
# toolchain and baseline and 15 of headroom for a slow runner.
mutation_rust_shard_budget_minutes() {
  echo 72
}

# The cost of one mutant by package, in thousandths of a minute, measured on nightly shards and
# scaled by the 0.78 the `mutants` cargo profile takes off a rebuild; each mutant relinks the tests.
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
  echo "go-api-runtime go-api-intake go-api-status go-api-converters go-api-identity go-api-tenancy-admin go-api-device-control go-api-device-sessions go-api-device-reads go-api-incidents go-api-rules go-api-enrollment go-api-updates-purge go-agentapi-connection go-agentapi-handshake go-agentapi-backfill go-agentapi-edge-telemetry go-domain-rules go-domain-alerts-room go-domain-alerts-record go-domain-persistence go-amt go-updates-certificates go-protocol-wire go-relay-signaling go-observability-harness go-composition-root"
}

mutation_all_shards() {
  echo "$(mutation_rust_shards) $(mutation_go_shards) $(mutation_web_shards)"
}

# The path gremlins walks for a shard, relative to server/; gremlins also runs `go test` over it.
# A shard owning a unit outside internal/ walks the module root, which a narrower walk would miss.
mutation_go_shard_scan_path() {
  local shard="${1:?mutation shard required}" unit
  for unit in $(mutation_go_shard_units "$shard"); do
    case "${unit#*:}" in
      internal/*) ;;
      *)
        echo "."
        return 0
        ;;
    esac
  done
  echo "./internal"
}

# A CLI -E overrides .gremlins.yaml exclude-files, so each run restates them in walk coordinates;
# cmd/meshserver is excluded whole because every source in it is the process's own wiring.
mutation_go_global_excludes() {
  case "${1:-.}" in
    ./internal)
      echo 'openapi_gen\.go|^testutil/|^faulttest/'
      ;;
    .)
      echo 'openapi_gen\.go|^cmd/meshserver/|^tests/loadtest/main\.go$|^tests/netfault/main\.go$|^internal/testutil/|^internal/faulttest/'
      ;;
    *)
      echo "unknown mutation scan path: $1" >&2
      return 1
      ;;
  esac
}

mutation_go_shard_units() {
  case "$1" in
    go-api-runtime)
      echo "file:internal/api/api.go file:internal/api/middleware.go file:internal/api/wsconn.go file:internal/api/ratelimit.go file:internal/api/proxytrust.go"
      ;;
    go-api-intake)
      echo "file:internal/api/validate.go file:internal/api/log_redact.go"
      ;;
    go-api-status)
      echo "file:internal/api/handlers_client_errors.go file:internal/api/handlers_health.go file:internal/api/metrics_assemble.go"
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
    # Every mutant here re-pays the Postgres-backed API suite, the most expensive in the module.
    go-api-device-control)
      echo "file:internal/api/handlers_devices.go file:internal/api/handlers_device_actions.go file:internal/api/handlers_maintenance.go"
      ;;
    go-api-device-sessions)
      echo "file:internal/api/handlers_amt.go file:internal/api/handlers_sessions.go file:internal/api/handlers_relay.go"
      ;;
    go-api-device-reads)
      echo "file:internal/api/handlers_device_summary.go file:internal/api/handlers_device_inventory.go file:internal/api/handlers_device_metrics.go file:internal/api/handlers_device_history.go"
      ;;
    go-api-incidents)
      echo "file:internal/api/handlers_incidents.go file:internal/api/handlers_incident_moves.go file:internal/api/handlers_incident_assign.go file:internal/api/handlers_incident_comment.go file:internal/api/handlers_incident_evidence.go"
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
      echo "file:internal/agentapi/conn.go file:internal/agentapi/conn_register.go file:internal/agentapi/conn_guard.go file:internal/agentapi/conn_maintenance.go file:internal/agentapi/server.go file:internal/agentapi/server_connection.go file:internal/agentapi/deregister.go"
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
    go-domain-rules)
      echo "dir:internal/rules"
      ;;
    go-domain-alerts-room)
      echo "file:internal/alerts/room.go file:internal/alerts/queue.go file:internal/alerts/incident.go file:internal/alerts/aggregate.go"
      ;;
    go-domain-alerts-record)
      echo "file:internal/alerts/types.go file:internal/alerts/limits.go file:internal/alerts/noise.go file:internal/alerts/postgres.go file:internal/alerts/postgres_fold.go file:internal/alerts/postgres_lifecycle.go file:internal/alerts/postgres_limits.go file:internal/alerts/postgres_noise.go file:internal/alerts/postgres_retention.go"
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
    go-protocol-wire)
      echo "dir:internal/protocol dir:internal/osutil"
      ;;
    go-relay-signaling)
      echo "dir:internal/relay dir:internal/signaling dir:internal/clientapi"
      ;;
    go-observability-harness)
      echo "dir:internal/telemetry dir:internal/metrics dir:internal/testpg dir:internal/testvm dir:internal/testreaper dir:tests/loadtest dir:tests/netfault"
      ;;
    # The composition root is mutated: the acceptance suite stands the assembly up, so a mutant
    # that unwires a port has a test to be killed by.
    go-composition-root)
      echo "dir:internal/app"
      ;;
    *)
      echo "unknown mutation shard: $1" >&2
      return 1
      ;;
  esac
}

# Seconds one Go mutant costs its shard, rounded up from nightly runs and measured over the mutants
# that finish, since a blocked one holds a worker for its whole leash and is counted separately.
mutation_go_shard_seconds_per_mutant() {
  case "$1" in
    go-api-runtime) echo 46 ;;
    go-api-intake) echo 42 ;;
    go-api-status) echo 46 ;;
    go-api-converters) echo 68 ;;
    go-api-incidents) echo 63 ;;
    go-api-rules) echo 67 ;;
    go-api-identity) echo 57 ;;
    go-api-tenancy-admin) echo 72 ;;
    go-api-enrollment) echo 53 ;;
    go-api-updates-purge) echo 64 ;;
    go-api-device-control) echo 69 ;;
    go-api-device-sessions) echo 66 ;;
    go-api-device-reads) echo 54 ;;
    go-agentapi-connection) echo 10 ;;
    go-agentapi-handshake) echo 8 ;;
    go-agentapi-edge-telemetry) echo 7 ;;
    go-agentapi-backfill) echo 3 ;;
    go-domain-rules) echo 12 ;;
    go-domain-alerts-room) echo 39 ;;
    go-domain-alerts-record) echo 25 ;;
    go-domain-persistence) echo 5 ;;
    # These two spend almost their whole run inside one blocked mutant's leash, declared below.
    go-amt) echo 2 ;;
    go-updates-certificates) echo 1 ;;
    # Both run against in-memory fixtures and never leave the process.
    go-protocol-wire | go-relay-signaling) echo 1 ;;
    go-observability-harness) echo 1 ;;
    # Postgres-backed like the API shards: each mutant re-pays a schema migration and full assembly.
    go-composition-root) echo 5 ;;
    *)
      echo "unknown mutation shard: $1" >&2
      return 1
      ;;
  esac
}

# How many of a shard's mutants never terminate: CONDITIONALS_NEGATION removes a loop's exit, and
# gremlins records TIMED OUT, which leaves the score alone and costs wall clock.
mutation_go_shard_blocking_mutants() {
  case "$1" in
    # server.go's listener guard and server_connection.go's read guard: a mutant that stops the
    # server publishing its address leaves every dialling test waiting.
    go-agentapi-connection) echo 2 ;;
    # transport/mps.go's accept loop returns on a cancelled context; negated, it accepts forever.
    go-amt) echo 1 ;;
    # notifications/vapid.go pads a private key to 32 bytes; negated, it prepends zeros forever.
    go-updates-certificates) echo 1 ;;
    # postgres_retention.go's drain repeats a batched delete until a pass reclaims less than a
    # full batch; negated, a pass that reclaims nothing repeats forever.
    go-domain-alerts-record) echo 1 ;;
    *)
      mutation_go_shard_seconds_per_mutant "$1" >/dev/null || return 1
      echo 0
      ;;
  esac
}

# The longest a Go shard's coverage run takes before the first mutant, in seconds (298 measured);
# gremlins derives every mutant's leash from this figure.
mutation_go_coverage_elapsed_ceiling_seconds() {
  echo 300
}

# What a Go shard pays beyond the coverage run before the first mutant: checkout, image pulls,
# Postgres, VictoriaMetrics, the toolchain and gremlins; 26s measured.
mutation_go_setup_ceiling_seconds() {
  echo 30
}

# The most wall clock one non-terminating mutant can cost a shard: gremlins gives every mutant the
# coverage run's time times the timeout coefficient, which this reads from server/.gremlins.yaml.
mutation_go_leash_ceiling_seconds() {
  local coefficient
  coefficient="$(sed -nE 's/^[[:space:]]*timeout-coefficient:[[:space:]]*([0-9]+).*/\1/p' \
    "$MUTATION_SHARDS_LIB_DIR/../../server/.gremlins.yaml")"
  case "$coefficient" in
    '' | *[!0-9]*)
      echo "could not read timeout-coefficient from server/.gremlins.yaml" >&2
      return 1
      ;;
  esac
  echo $((coefficient * $(mutation_go_coverage_elapsed_ceiling_seconds)))
}

# Minutes of mutant execution a Go shard may spend: 90:00 cap - 0:30 setup - 5:00 coverage - 15:00
# leash = 69:30, the leash being headroom for a mutant that starts blocking undeclared.
mutation_go_shard_budget_minutes() {
  echo 69
}

# Per-shard timeout-coefficient override; empty output adds no CLI flag and inherits the baseline.
# A coefficient above the baseline puts a shard's blocked mutants outside the bound the budget uses.
mutation_go_shard_timeout_coefficient() {
  case "$1" in
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

# The unit as gremlins sees it, anchored at the front as api/x.go is a substring of agentapi/x.go.
# A unit the walk cannot reach prints nothing; its shard's own walk is the module root.
mutation_go_unit_regex() {
  local unit="${1:?mutation unit required}"
  local scan="${2:-.}"
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

  case "$scan" in
    ./internal)
      case "$path" in
        internal/*) path="${path#internal/}" ;;
        *) return 0 ;;
      esac
      ;;
    .) ;;
    *)
      echo "unknown mutation scan path: $scan" >&2
      return 1
      ;;
  esac

  path="${path//./\\.}"
  if [[ "$unit" == file:* ]]; then
    path="$path\$"
  fi
  printf '^%s' "$path"
}

mutation_go_shard_exclude_regex() {
  local shard="${1:?mutation shard required}"
  local other unit part scan
  local regex

  mutation_go_shard_units "$shard" >/dev/null || return 1
  scan="$(mutation_go_shard_scan_path "$shard")"
  regex="$(mutation_go_global_excludes "$scan")" || return 1
  for other in $(mutation_go_shards); do
    [[ "$other" == "$shard" ]] && continue
    for unit in $(mutation_go_shard_units "$other"); do
      part="$(mutation_go_unit_regex "$unit" "$scan")" || return 1
      [ -n "$part" ] || continue
      regex="$regex|$part"
    done
  done
  printf '%s\n' "$regex"
}
