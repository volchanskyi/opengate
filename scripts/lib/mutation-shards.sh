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

mutation_rust_shards() {
  echo "rust-round-robin-1-of-8 rust-round-robin-2-of-8 rust-round-robin-3-of-8 rust-round-robin-4-of-8 rust-round-robin-5-of-8 rust-round-robin-6-of-8 rust-round-robin-7-of-8 rust-round-robin-8-of-8"
}

mutation_web_shards() {
  echo "web"
}

mutation_go_shards() {
  echo "go-api-runtime go-api-identity-admin go-api-device-operations go-api-provisioning-lifecycle go-agentapi-connection-handshake go-agentapi-backfill go-agentapi-edge-telemetry go-domain-persistence go-amt-updates-certificates go-protocol-relay-observability"
}

mutation_all_shards() {
  echo "$(mutation_rust_shards) $(mutation_go_shards) $(mutation_web_shards)"
}

# A CLI -E overrides server/.gremlins.yaml exclude-files, so every sharded run
# must restate the generated code, entry points, and shared test scaffolding.
mutation_go_global_excludes() {
  echo 'openapi_gen\.go|cmd/meshserver/main\.go|tests/loadtest/main\.go|internal/testutil/|internal/faulttest/'
}

mutation_go_shard_units() {
  case "$1" in
    go-api-runtime)
      echo "file:internal/api/api.go file:internal/api/converters.go file:internal/api/middleware.go file:internal/api/wsconn.go file:internal/api/handlers_client_errors.go file:internal/api/handlers_health.go file:internal/api/log_redact.go file:internal/api/metrics_assemble.go file:internal/api/ratelimit.go"
      ;;
    go-api-identity-admin)
      echo "file:internal/api/handlers_auth.go file:internal/api/handlers_users.go file:internal/api/handlers_groups.go file:internal/api/handlers_security_groups.go file:internal/api/handlers_security_group_members.go file:internal/api/handlers_audit.go file:internal/api/handlers_push.go"
      ;;
    go-api-device-operations)
      echo "file:internal/api/handlers_devices.go file:internal/api/handlers_device_actions.go file:internal/api/handlers_device_correlate.go file:internal/api/handlers_device_history.go file:internal/api/handlers_device_inventory.go file:internal/api/handlers_device_metrics.go file:internal/api/handlers_amt.go file:internal/api/handlers_relay.go file:internal/api/handlers_sessions.go"
      ;;
    go-api-provisioning-lifecycle)
      echo "file:internal/api/handlers_enrollment.go file:internal/api/handlers_install.go file:internal/api/handlers_updates.go file:internal/api/handlers_purge.go"
      ;;
    go-agentapi-connection-handshake)
      echo "file:internal/agentapi/conn.go file:internal/agentapi/server.go file:internal/agentapi/errors.go file:internal/agentapi/handshaker.go file:internal/agentapi/deregister.go"
      ;;
    go-agentapi-backfill)
      echo "file:internal/agentapi/backfill_scheduler.go file:internal/agentapi/conn_backfill.go"
      ;;
    go-agentapi-edge-telemetry)
      echo "file:internal/agentapi/conn_discovery.go file:internal/agentapi/conn_telemetry.go file:internal/agentapi/conn_logs.go file:internal/agentapi/conn_history.go file:internal/agentapi/alert_breach.go file:internal/agentapi/alert_rules.go"
      ;;
    go-domain-persistence)
      echo "dir:internal/auth dir:internal/db dir:internal/dbtx dir:internal/device dir:internal/inventory dir:internal/lifecycle dir:internal/session dir:internal/audit dir:internal/usecase"
      ;;
    go-amt-updates-certificates)
      echo "dir:internal/amt dir:internal/updater dir:internal/notifications dir:internal/cert"
      ;;
    go-protocol-relay-observability)
      echo "dir:internal/protocol dir:internal/correlate dir:internal/telemetry dir:internal/relay dir:internal/metrics dir:internal/signaling dir:internal/testpg dir:internal/testvm dir:internal/osutil dir:internal/clientapi dir:tests/loadtest"
      ;;
    *)
      echo "unknown mutation shard: $1" >&2
      return 1
      ;;
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
