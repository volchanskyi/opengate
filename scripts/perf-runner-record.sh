#!/usr/bin/env bash
# Reads what the runner and the server's processor allowance did over one leg: the processor
# model, the share of time the host took back, and how often the server's container was throttled.
#
# Usage:
#   perf-runner-record.sh snapshot <server-cgroup-dir> <out.json>
#   perf-runner-record.sh delta <before.json> <after.json>
#
# Environment:
#   PERF_PROC_STAT  the kernel's processor counters (default /proc/stat)
#
# Exit codes:
#   0  read
#   1  a counter could not be read
#   2  a usage error or a missing snapshot
set -euo pipefail

PROC_STAT="${PERF_PROC_STAT:-/proc/stat}"

usage() {
  sed -n '5,7p' "${BASH_SOURCE[0]}" | sed 's/^# *//' >&2
  exit 2
}

snapshot() {
  local dir="$1" out="$2" model ticks
  if [ ! -r "$dir/cpu.stat" ]; then
    echo "::error::no processor counters at $dir/cpu.stat" >&2
    return 1
  fi
  model="$(lscpu | sed -n 's/^Model name:[[:space:]]*//p')"
  # The first line sums every processor: user nice system idle iowait irq softirq steal.
  ticks="$(awk '$1 == "cpu" { print $9, $2 + $3 + $4 + $5 + $6 + $7 + $8 + $9; exit }' "$PROC_STAT")"
  [ -n "$ticks" ] || {
    echo "::error::no processor line in $PROC_STAT" >&2
    return 1
  }
  jq -n --arg model "$model" --arg ticks "$ticks" --rawfile stat "$dir/cpu.stat" '
    ($stat | split("\n") | map(select(length > 0) | split(" ") | {key: .[0], value: (.[1] | tonumber)})
     | from_entries) as $s
    | ($ticks | split(" ") | map(tonumber)) as $t
    | {processor_model: $model, steal_ticks: $t[0], total_ticks: $t[1],
       nr_periods: ($s.nr_periods // 0), nr_throttled: ($s.nr_throttled // 0),
       throttled_usec: ($s.throttled_usec // 0)}' >"$out"
}

delta() {
  local before="$1" after="$2"
  if [ ! -s "$before" ] || [ ! -s "$after" ]; then
    echo "::error::a snapshot is missing: $before, $after" >&2
    return 2
  fi
  jq -n --slurpfile b "$before" --slurpfile a "$after" '
    def round2: . * 100 | round / 100;
    $b[0] as $b | $a[0] as $a
    | ($a.total_ticks - $b.total_ticks) as $total
    | ($a.nr_periods - $b.nr_periods) as $periods
    | ($a.nr_throttled - $b.nr_throttled) as $throttled
    | {
        processor_model: $a.processor_model,
        steal_percent: (if $total > 0 then (($a.steal_ticks - $b.steal_ticks) * 100 / $total | round2) else null end),
        server_periods: $periods,
        server_throttled_periods: $throttled,
        server_throttled_share: (if $periods > 0 then ($throttled / $periods | round2) else null end),
        server_throttled_ms: (($a.throttled_usec - $b.throttled_usec) / 1000 | round)
      }'
}

main() {
  case "${1:-}" in
    snapshot)
      [ "$#" -eq 3 ] || usage
      snapshot "$2" "$3"
      ;;
    delta)
      [ "$#" -eq 3 ] || usage
      delta "$2" "$3"
      ;;
    *) usage ;;
  esac
}

main "$@"
