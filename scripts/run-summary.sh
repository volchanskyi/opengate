#!/usr/bin/env bash
# Renders a run's summary: each measurement beside its profile limit, the error rate, and the
# share of its ceiling the server and database used in the measured phase.
#
# Usage:
#   run-summary.sh window <profile.yaml> <bundle.json>  prints "<from> <to>", the measured span
#   run-summary.sh render --title T --profile P --rows R [--bundle B] [--database D]
#   rows R         the run's canonical rows
#   bundle B       the bundle holding the server's readings
#   database D     {cpu_percent, cpu_cap, memory_percent, memory_cap} from database-levels.sh
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/lib/loadtest-profile.sh
. "$HERE/lib/loadtest-profile.sh"
# shellcheck source=scripts/lib/summary-table.sh
. "$HERE/lib/summary-table.sh"

# measured_phases prints the measured phase names as a JSON array, or all phases if none is marked.
measured_phases() {
  profile_phases "$1" | jq -c 'if any(.[]; .measured) then [.[] | select(.measured) | .name] else [.[].name] end'
}

window() {
  local profile="$1" bundle="$2" names
  names="$(measured_phases "$profile")"
  jq -r --argjson names "$names" '
    [.phases[] | select(.name as $n | $names | index($n))]
    | if length == 0 then empty else "\(map(.started_at) | min) \(map(.finished_at) | max)" end
  ' "$bundle"
}

render() {
  local title="" profile="" rows="" bundle="" database=""
  while [ "$#" -gt 0 ]; do
    case "$1" in
      --title) title="$2" ;;
      --profile) profile="$2" ;;
      --rows) rows="$2" ;;
      --bundle) bundle="$2" ;;
      --database) database="$2" ;;
      *)
        echo "run-summary: unknown argument $1" >&2
        return 2
        ;;
    esac
    shift 2
  done
  [ -n "$title" ] && [ -n "$profile" ] && [ -s "$rows" ] || {
    echo "usage: $0 render --title T --profile P --rows R [--bundle B] [--database D]" >&2
    return 2
  }

  local gates names server db
  gates="$(profile_gates "$profile")"
  names="$(measured_phases "$profile")"
  server='{}'
  [ -z "$bundle" ] || server="$(jq -c --argjson names "$names" '
    [.phases[] | select(.name as $n | $names | index($n))] as $measured
    | def mean(f): [$measured[] | f | select(. != null)] | if length == 0 then null else add / length end;
    { cpu_percent: mean(.target_busy_percent),
      cpu_cap: .target.cpus,
      memory_percent: (mean(.target_resident_bytes) as $r
        | if $r == null or (.target.memory_bytes // 0) == 0 then null else $r / .target.memory_bytes * 100 end),
      memory_bytes: .target.memory_bytes }' "$bundle")"
  db='{}'
  [ -z "$database" ] || db="$(jq -c '.' "$database")"

  printf '### %s\n\n' "$title"
  summary_table_header
  jq -r --argjson gates "$gates" --argjson server "$server" --argjson db "$db" '
    def num: (. * 10 | round) / 10 | tostring;
    def words($m): {latency_p50_ms: "p50", latency_p95_ms: "p95", latency_p99_ms: "p99",
                    error_rate: "error rate", rps: "throughput"}[$m];
    def shown($m; $v): if $m == "error_rate" then "\($v * 100 | num) %"
                       elif $m == "rps" then "\($v | num) per second"
                       else "\($v | num) ms" end;
    def cpus($c): if $c == null then "" elif $c == 1 then " of 1 processor" else " of \($c) processors" end;
    def bytes($b): if $b == null then ""
                   elif $b >= 1073741824 then " of \($b / 1073741824 | num) GiB"
                   else " of \($b / 1048576 | num) MiB" end;
    def level($p; $cap): if $p == null then "not read" else "\($p | num) %\($cap)" end;
    (.[] as $row
     | ("latency_p50_ms", "latency_p95_ms", "latency_p99_ms", "error_rate", "rps") as $m
     | $row[$m] as $v
     | select($v != null)
     | "\($row.source)/\($row.scenario)/\($row.phase)" as $series
     | ([$gates[] | select(.series == $series and .metric == $m)] | first) as $g
     | (if $g == null then ["no limit", "—"]
        else
          (if $g.max != null then "≤ \(shown($m; $g.max))" else "≥ \(shown($m; $g.min))" end
            + (if $g.blocking then "" else " (reported only)" end)) as $expected
          | (($g.max != null and $v > $g.max) or ($g.min != null and $v < $g.min)) as $crossed
          | [$expected, (if $crossed | not then "pass" elif $g.blocking then "FAIL" else "over" end)]
        end) as $judged
     | [ "\($series) \(words($m))", $judged[0], shown($m; $v), $judged[1] ]),
    ["Service Level Avg CPU % (server)", "—", level($server.cpu_percent; cpus($server.cpu_cap)), "—"],
    ["Service Level Avg Mem % (server)", "—", level($server.memory_percent; bytes($server.memory_bytes)), "—"],
    ["Service Level Avg CPU % (database)", "—",
      level($db.cpu_percent; (if $db.cpu_cap then " of \($db.cpu_cap)" else "" end)), "—"],
    ["Service Level Avg Mem % (database)", "—",
      level($db.memory_percent; (if $db.memory_cap then " of \($db.memory_cap)" else "" end)), "—"]
    | "| \(.[0]) | \(.[1]) | \(.[2]) | \(.[3]) |"
  ' "$rows"
  summary_legend
  summary_legend_terms p50 p95 p99 "error rate" "Service Level"
}

main() {
  case "${1:-}" in
    window)
      [ "$#" -eq 3 ] || {
        echo "usage: $0 window <profile.yaml> <bundle.json>" >&2
        return 2
      }
      window "$2" "$3"
      ;;
    render)
      shift
      render "$@"
      ;;
    *)
      echo "usage: $0 {window|render} ..." >&2
      return 2
      ;;
  esac
}

main "$@"
