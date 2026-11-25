#!/usr/bin/env bash
# Reads what the runner and the server's allowance did over one leg, and how a stopped server
# ended: processor model, host steal, throttling, exit code, memory kill and stop time.
#
# Usage:
#   perf-runner-record.sh snapshot <server-cgroup-dir> <out.json>
#   perf-runner-record.sh snapshot --server-stopped <container-state.json> <out.json>
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
  sed -n '5,8p' "${BASH_SOURCE[0]}" | sed 's/^# *//' >&2
  exit 2
}

# host_reading prints the processor model and the kernel's steal and total ticks as JSON.
host_reading() {
  local model ticks
  model="$(lscpu | sed -n 's/^Model name:[[:space:]]*//p')"
  # The first line sums every processor: user nice system idle iowait irq softirq steal.
  ticks="$(awk '$1 == "cpu" { print $9, $2 + $3 + $4 + $5 + $6 + $7 + $8 + $9; exit }' "$PROC_STAT")"
  [ -n "$ticks" ] || {
    echo "::error::no processor line in $PROC_STAT" >&2
    return 1
  }
  jq -n --arg model "$model" --arg ticks "$ticks" '
    ($ticks | split(" ") | map(tonumber)) as $t
    | {processor_model: $model, steal_ticks: $t[0], total_ticks: $t[1]}'
}

snapshot() {
  local dir="$1" out="$2" host
  if [ ! -r "$dir/cpu.stat" ]; then
    echo "::error::no processor counters at $dir/cpu.stat" >&2
    return 1
  fi
  host="$(host_reading)"
  jq --rawfile stat "$dir/cpu.stat" '
    ($stat | split("\n") | map(select(length > 0) | split(" ") | {key: .[0], value: (.[1] | tonumber)})
     | from_entries) as $s
    | . + {nr_periods: ($s.nr_periods // 0), nr_throttled: ($s.nr_throttled // 0),
       throttled_usec: ($s.throttled_usec // 0)}' <<<"$host" >"$out"
}

# snapshot_stopped reads the runner and the state Docker kept: a stopped container's cgroup goes
# with it, and `docker inspect -f '{{json .State}}'` says how it ended.
snapshot_stopped() {
  local state="$1" out="$2" host
  if [ ! -r "$state" ] || ! jq -e 'type == "object"' "$state" >/dev/null 2>&1; then
    echo "::error::no container state at $state" >&2
    return 1
  fi
  host="$(host_reading)"
  jq --slurpfile state "$state" '. + {server_stopped: true,
    server_exit_code: $state[0].ExitCode, server_oom_killed: $state[0].OOMKilled,
    server_finished_at: $state[0].FinishedAt}' <<<"$host" >"$out"
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
    | {
        processor_model: $a.processor_model,
        steal_percent: (if $total > 0 then (($a.steal_ticks - $b.steal_ticks) * 100 / $total | round2) else null end),
        server_stopped: ($a.server_stopped // false)
      }
      + if $a.server_stopped then
          {server_periods: null, server_throttled_periods: null, server_throttled_share: null,
           server_throttled_ms: null, server_exit_code: $a.server_exit_code,
           server_oom_killed: $a.server_oom_killed, server_finished_at: $a.server_finished_at}
        else
          ($a.nr_periods - $b.nr_periods) as $periods
          | ($a.nr_throttled - $b.nr_throttled) as $throttled
          | {server_exit_code: null, server_oom_killed: null, server_finished_at: null,
             server_periods: $periods,
             server_throttled_periods: $throttled,
             server_throttled_share: (if $periods > 0 then ($throttled / $periods | round2) else null end),
             server_throttled_ms: (($a.throttled_usec - $b.throttled_usec) / 1000 | round)}
        end'
}

main() {
  case "${1:-}" in
    snapshot)
      if [ "${2:-}" = --server-stopped ]; then
        [ "$#" -eq 4 ] || usage
        snapshot_stopped "$3" "$4"
      else
        [ "$#" -eq 3 ] || usage
        snapshot "$2" "$3"
      fi
      ;;
    delta)
      [ "$#" -eq 3 ] || usage
      delta "$2" "$3"
      ;;
    *) usage ;;
  esac
}

main "$@"
