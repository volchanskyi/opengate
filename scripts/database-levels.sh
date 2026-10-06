#!/usr/bin/env bash
# Prints the database's processor and memory use as shares of their caps for run-summary.sh.
# An unreadable value is null; compose reads the cgroup, staging asks the metrics store.
#
# Usage:
#   database-levels.sh cgroup-of <container>          the container's cgroup directory
#   database-levels.sh sample <cgroup-dir> <out>      append a reading every interval until stopped
#   database-levels.sh average <samples> <from> <to>  the levels across a window (RFC 3339 times)
#   database-levels.sh cluster <namespace> <from> <to>
#
# Environment:
#   DB_LEVELS_INTERVAL  seconds between samples (default 5)
#   DB_LEVELS_SAMPLES   stop after this many samples (default: run until stopped)
#   VM_NAMESPACE, VM_SERVICE  where the metrics store runs (cluster mode)
set -euo pipefail

VM_NS="${VM_NAMESPACE:-monitoring}"
VM_SVC="${VM_SERVICE:-monitoring-victoriametrics}"

cgroup_of() {
  local id
  id="$(docker inspect -f '{{.Id}}' "$1")"
  local dir
  for dir in "/sys/fs/cgroup/system.slice/docker-$id.scope" "/sys/fs/cgroup/docker/$id"; do
    if [ -f "$dir/cpu.stat" ]; then
      printf '%s\n' "$dir"
      return 0
    fi
  done
  echo "::error::no cgroup found for container $1" >&2
  return 1
}

# One line a reading: when, processor microseconds used so far, the program's
# own memory, the processor quota and period, and the memory limit.
sample() {
  local dir="$1" out="$2" interval="${DB_LEVELS_INTERVAL:-5}" left="${DB_LEVELS_SAMPLES:-0}"
  local usage anon quota period limit
  read -r quota period <"$dir/cpu.max"
  limit="$(cat "$dir/memory.max")"
  while :; do
    usage="$(awk '$1 == "usage_usec" { print $2 }' "$dir/cpu.stat")"
    anon="$(awk '$1 == "anon" { print $2 }' "$dir/memory.stat")"
    printf '%s\t%s\t%s\t%s\t%s\t%s\n' "$(date -u +%s)" "$usage" "$anon" "$quota" "$period" "$limit" >>"$out"
    if [ "$left" -gt 0 ]; then
      left=$((left - 1))
      [ "$left" -gt 0 ] || return 0
    fi
    [ "$interval" = "0" ] || sleep "$interval"
  done
}

epoch() { date -u -d "$1" +%s; }

average() {
  local samples="$1" from to
  from="$(epoch "$2")"
  to="$(epoch "$3")"
  [ -s "$samples" ] || {
    jq -n '{cpu_percent: null, cpu_cap: null, memory_percent: null, memory_cap: null}'
    return 0
  }
  awk -F'\t' -v from="$from" -v to="$to" '$1 >= from && $1 <= to' "$samples" \
    | jq -Rs '
      split("\n") | map(select(length > 0) | split("\t") | map(tonumber? // .))
      | map({t: .[0], usage: .[1], anon: .[2], quota: .[3], period: .[4], limit: .[5]}) as $s
      | def cap_cores: if ($s[0].quota | type) == "number" then $s[0].quota / $s[0].period else null end;
        def cpus($c): if $c == null then null elif $c == 1 then "1 processor" else "\($c) processors" end;
        def bytes($b): if ($b | type) != "number" then null
                       elif $b >= 1073741824 then "\($b / 1073741824) GiB" else "\($b / 1048576) MiB" end;
      if ($s | length) == 0 then {cpu_percent: null, cpu_cap: null, memory_percent: null, memory_cap: null}
      else
        { cpu_percent: (if ($s | length) < 2 or cap_cores == null then null
                        else (($s[-1].usage - $s[0].usage) / 1000000) / ($s[-1].t - $s[0].t) / cap_cores * 100 end),
          cpu_cap: cpus(cap_cores),
          memory_percent: (if ($s[0].limit | type) != "number" then null
                           else ($s | map(.anon) | add / length) / $s[0].limit * 100 end),
          memory_cap: bytes($s[0].limit) }
      end'
}

# ask QUERY AT — one instant value from the metrics store, or empty.
ask() {
  local reply
  reply="$(kubectl get --raw "/api/v1/namespaces/$VM_NS/services/$VM_SVC:8428/proxy/api/v1/query?$(
    python3 -c 'import sys, urllib.parse; print(urllib.parse.urlencode({"query": sys.argv[1], "time": sys.argv[2]}))' "$1" "$2"
  )" 2>/dev/null)" || return 0
  jq -r '.data.result[0].value[1] // empty' <<<"$reply" 2>/dev/null || true
}

cluster() {
  local namespace="$1" from to seconds scope
  from="$(epoch "$2")"
  to="$(epoch "$3")"
  seconds=$((to - from))
  scope="namespace=\"$namespace\",container=\"postgres\""
  local used quota period rss limit
  used="$(ask "sum(increase(container_cpu_usage_seconds_total{$scope}[${seconds}s]))" "$to")"
  quota="$(ask "max(container_spec_cpu_quota{$scope})" "$to")"
  period="$(ask "max(container_spec_cpu_period{$scope})" "$to")"
  rss="$(ask "avg(avg_over_time(container_memory_rss{$scope}[${seconds}s]))" "$to")"
  limit="$(ask "max(container_spec_memory_limit_bytes{$scope})" "$to")"
  jq -n --arg used "$used" --arg quota "$quota" --arg period "$period" --arg rss "$rss" \
    --arg limit "$limit" --argjson seconds "$seconds" '
    def n($x): if $x == "" then null else ($x | tonumber) end;
    (if n($quota) != null and n($period) != null and n($period) > 0 then n($quota) / n($period) else null end) as $cores
    | { cpu_percent: (if n($used) == null or $cores == null then null else n($used) / $seconds / $cores * 100 end),
        cpu_cap: (if $cores == null then null elif $cores == 1 then "1 processor" else "\($cores) processors" end),
        memory_percent: (if n($rss) == null or n($limit) == null then null else n($rss) / n($limit) * 100 end),
        memory_cap: (if n($limit) == null then null
                     elif n($limit) >= 1073741824 then "\(n($limit) / 1073741824) GiB"
                     else "\(n($limit) / 1048576) MiB" end) }'
}

main() {
  case "${1:-}" in
    cgroup-of) cgroup_of "$2" ;;
    sample) sample "$2" "$3" ;;
    average) average "$2" "$3" "$4" ;;
    cluster) cluster "$2" "$3" "$4" ;;
    *)
      echo "usage: $0 {cgroup-of|sample|average|cluster} ..." >&2
      return 2
      ;;
  esac
}

main "$@"
