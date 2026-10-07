#!/usr/bin/env bash
# Weighs a fixture by the database size and metrics series count before and after a fleet is built.
#
# Usage:
#   perf-weigh-fixture.sh baseline <baseline.json>          the empty stack
#   perf-weigh-fixture.sh weigh <baseline.json> [out.json]  measures against it
#
# Environment:
#   PERF_DB_CONTAINER  the database container (default opengate-perf-postgres)
#   PERF_METRICS_URL   the stack's metrics store (default http://127.0.0.1:8428)
#   PERF_EVICTION_MARGIN_BYTES  the margin the result is compared against
set -euo pipefail

DB_CONTAINER="${PERF_DB_CONTAINER:-opengate-perf-postgres}"
DB_USER="${PERF_DB_USER:-opengate}"
DB_NAME="${PERF_DB_NAME:-opengate}"
METRICS_URL="${PERF_METRICS_URL:-http://127.0.0.1:8428}"

# The node root's free space in bytes, the margin a fixture's weight is compared against.
DEFAULT_EVICTION_MARGIN_BYTES=$((9 * 1024 * 1024 * 1024))

psql_scalar() {
  docker exec "$DB_CONTAINER" psql -U "$DB_USER" -d "$DB_NAME" -tAc "$1" | tr -d '[:space:]'
}

# database_bytes is the database's own account of its size, the number a volume has to hold.
database_bytes() {
  psql_scalar "SELECT pg_database_size(current_database())"
}

table_rows() {
  psql_scalar "SELECT COALESCE((SELECT COUNT(*) FROM $1), 0)"
}

# telemetry_series is how many series the metrics store holds, by its own count.
telemetry_series() {
  local answer count
  answer="$(curl -fsS "${METRICS_URL}/api/v1/series/count")"
  count="$(jq -r '.data[0] // empty' <<<"$answer")"
  case "$count" in
    '' | *[!0-9]*)
      echo "::error::the metrics store at ${METRICS_URL} did not say how many series it holds: $answer" >&2
      return 1
      ;;
  esac
  printf '%s\n' "$count"
}

usage() {
  echo "usage: $0 baseline <baseline.json> | $0 weigh <baseline.json> [output.json]" >&2
}

require_stack() {
  if ! docker exec "$DB_CONTAINER" true >/dev/null 2>&1; then
    echo "::error::database container $DB_CONTAINER is not running; bring the performance stack up first" >&2
    return 2
  fi
}

# baseline records the empty stack: the schema, indexes and migration rows weigh something already.
baseline() {
  local out="${1:-}"
  if [ -z "$out" ]; then
    usage
    return 2
  fi
  require_stack || return 2
  local bytes series
  bytes="$(database_bytes)"
  series="$(telemetry_series)" || return 1
  jq -n --argjson database_bytes "$bytes" --argjson telemetry_series "$series" \
    '{database_bytes: $database_bytes, telemetry_series: $telemetry_series}' >"$out"
  cat "$out"
}

weigh() {
  local baseline_file="${1:-}"
  local out="${2:-fixture-weight.json}"
  local margin="${PERF_EVICTION_MARGIN_BYTES:-$DEFAULT_EVICTION_MARGIN_BYTES}"

  local baseline_bytes baseline_series
  baseline_bytes="$(jq -r '.database_bytes // empty' "$baseline_file" 2>/dev/null || true)"
  baseline_series="$(jq -r '.telemetry_series // empty' "$baseline_file" 2>/dev/null || true)"
  case "${baseline_bytes}:${baseline_series}" in
    :* | *: | *[!0-9:]*)
      echo "::error::weigh needs the baseline the empty stack recorded, and ${baseline_file:-nothing} holds none" >&2
      return 2
      ;;
  esac

  require_stack || return 2

  local devices sites users process_rows total series
  devices="$(table_rows devices)"
  sites="$(table_rows sites)"
  users="$(table_rows users)"
  process_rows="$(table_rows device_processes)"
  total="$(database_bytes)"
  series="$(telemetry_series)" || return 1

  local fits=true
  if [ "$total" -gt "$margin" ]; then
    fits=false
  fi

  jq -n \
    --argjson baseline_bytes "$baseline_bytes" \
    --argjson database_bytes "$total" \
    --argjson fixture_bytes "$((total - baseline_bytes))" \
    --argjson baseline_series "$baseline_series" \
    --argjson eviction_margin_bytes "$margin" \
    --argjson devices "${devices:-0}" \
    --argjson sites "${sites:-0}" \
    --argjson users "${users:-0}" \
    --argjson process_rows "${process_rows:-0}" \
    --argjson telemetry_series "$((series - baseline_series))" \
    --arg fits "$fits" \
    --arg timestamp "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    '{
      timestamp: $timestamp,
      baseline_bytes: $baseline_bytes,
      database_bytes: $database_bytes,
      fixture_bytes: $fixture_bytes,
      baseline_series: $baseline_series,
      eviction_margin_bytes: $eviction_margin_bytes,
      fits_inside_margin: ($fits == "true"),
      counts: {
        devices: $devices,
        sites: $sites,
        users: $users,
        process_rows: $process_rows,
        telemetry_series: $telemetry_series
      }
    }' >"$out"

  if [ "$fits" = "true" ]; then
    echo "Fixture weighs ${total} bytes, inside the ${margin}-byte margin: staging needs no volume and the storage question closes."
  else
    echo "::warning::Fixture weighs ${total} bytes, past the ${margin}-byte margin: the storage question reopens with a measured number attached."
  fi
}

main() {
  local action="${1:-}"
  case "$action" in
    baseline) baseline "${2:-}" ;;
    weigh) weigh "${2:-}" "${3:-}" ;;
    *)
      usage
      return 2
      ;;
  esac
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
