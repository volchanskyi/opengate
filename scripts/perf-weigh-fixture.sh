#!/usr/bin/env bash
# Weigh a fixture before spending storage on it.
#
# Nobody has measured what a two-thousand-machine fleet weighs. Production's
# database holds 66 MB after seven weeks with four real machines, and the
# cluster node has roughly nine gigabytes of margin before the kubelet starts
# evicting pods. Whether a fixture fits inside that margin is a fact one
# measurement settles, and until the measurement exists no storage decision
# should be made in either direction — the block-storage grant is exactly full,
# so "give staging its own disk" costs another volume, and the only candidate is
# the one holding every log the fleet has.
#
# So: read the database's own size before a fleet is built and again after, and
# report the difference. Nothing here decides anything; it produces the figure
# the decision needs.
#
# It is two commands rather than one because the two readings have to straddle
# the build. Taken back to back they always differ by nothing, which is a
# measurement that cannot fail and cannot inform.
#
#   perf-weigh-fixture.sh baseline                 → prints the empty size
#   perf-weigh-fixture.sh weigh <baseline> <out>   → measures against it
#
# Environment:
#   PERF_DB_CONTAINER  the database container (default opengate-perf-postgres)
#   PERF_EVICTION_MARGIN_BYTES  the margin the result is compared against
set -euo pipefail

DB_CONTAINER="${PERF_DB_CONTAINER:-opengate-perf-postgres}"
DB_USER="${PERF_DB_USER:-opengate}"
DB_NAME="${PERF_DB_NAME:-opengate}"

# The node root's free space at the time the strategy was written, in bytes.
# It is the figure the answer is compared against, and it is stated here so a
# reader can see what "fits" was measured against rather than inferring it.
DEFAULT_EVICTION_MARGIN_BYTES=$((9 * 1024 * 1024 * 1024))

psql_scalar() {
  docker exec "$DB_CONTAINER" psql -U "$DB_USER" -d "$DB_NAME" -tAc "$1" | tr -d '[:space:]'
}

# database_bytes is the database's own account of its size, which is the number
# a volume has to hold — not the sum of what was inserted.
database_bytes() {
  psql_scalar "SELECT pg_database_size(current_database())"
}

table_rows() {
  psql_scalar "SELECT COALESCE((SELECT COUNT(*) FROM $1), 0)"
}

usage() {
  echo "usage: $0 baseline | $0 weigh <baseline-bytes> [output.json]" >&2
}

require_stack() {
  if ! docker exec "$DB_CONTAINER" true >/dev/null 2>&1; then
    echo "::error::database container $DB_CONTAINER is not running; bring the performance stack up first" >&2
    return 2
  fi
}

main() {
  case "${1:-}" in
    baseline)
      require_stack || return 2
      # An empty database is not zero — the schema, the indexes and the rows the
      # migrations ship all weigh something — so this is what the fixture's own
      # weight is measured against.
      database_bytes
      return 0
      ;;
    weigh) ;;
    *)
      usage
      return 2
      ;;
  esac

  local baseline="${2:-}"
  local out="${3:-fixture-weight.json}"
  local margin="${PERF_EVICTION_MARGIN_BYTES:-$DEFAULT_EVICTION_MARGIN_BYTES}"

  case "$baseline" in
    '' | *[!0-9]*)
      echo "::error::weigh needs the baseline byte count the empty stack reported" >&2
      return 2
      ;;
  esac

  require_stack || return 2

  local devices sites users telemetry_series
  devices="$(table_rows devices)"
  sites="$(table_rows sites)"
  users="$(table_rows users)"
  telemetry_series="$(table_rows device_processes)"

  local total fixture_bytes fits
  total="$(database_bytes)"
  fixture_bytes=$((total - baseline))
  fits=true
  if [ "$total" -gt "$margin" ]; then
    fits=false
  fi

  jq -n \
    --argjson baseline_bytes "$baseline" \
    --argjson database_bytes "$total" \
    --argjson fixture_bytes "$fixture_bytes" \
    --argjson eviction_margin_bytes "$margin" \
    --argjson devices "${devices:-0}" \
    --argjson sites "${sites:-0}" \
    --argjson users "${users:-0}" \
    --argjson telemetry_series "${telemetry_series:-0}" \
    --arg fits "$fits" \
    --arg timestamp "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    '{
      timestamp: $timestamp,
      baseline_bytes: $baseline_bytes,
      database_bytes: $database_bytes,
      fixture_bytes: $fixture_bytes,
      eviction_margin_bytes: $eviction_margin_bytes,
      fits_inside_margin: ($fits == "true"),
      counts: {
        devices: $devices,
        sites: $sites,
        users: $users,
        telemetry_series: $telemetry_series
      }
    }' >"$out"

  if [ "$fits" = "true" ]; then
    echo "Fixture weighs ${total} bytes, inside the ${margin}-byte margin: staging needs no volume and the storage question closes."
  else
    echo "::warning::Fixture weighs ${total} bytes, past the ${margin}-byte margin: the storage question reopens with a measured number attached."
  fi
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
