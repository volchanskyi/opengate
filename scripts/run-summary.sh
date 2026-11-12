#!/usr/bin/env bash
# Renders a run's summary: each measurement beside its profile limit, the error rate, and the
# share of its ceiling the server and database used in the measured phase.
#
# Usage:
#   run-summary.sh window <profile.yaml> <bundle.json>  prints "<from> <to>", the measured span
#   run-summary.sh render --title T --profile P --rows R [--bundle B]
#   rows R         the run's canonical rows, the server's and database's levels among them
#   bundle B       the bundle holding the run's verdict
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
  local title="" profile="" rows="" bundle=""
  while [ "$#" -gt 0 ]; do
    case "$1" in
      --title) title="$2" ;;
      --profile) profile="$2" ;;
      --rows) rows="$2" ;;
      --bundle) bundle="$2" ;;
      *)
        echo "run-summary: unknown argument $1" >&2
        return 2
        ;;
    esac
    shift 2
  done
  [ -n "$title" ] && [ -n "$profile" ] && [ -s "$rows" ] || {
    echo "usage: $0 render --title T --profile P --rows R [--bundle B]" >&2
    return 2
  }

  local gates
  gates="$(profile_gates "$profile")"

  local verdict='null'
  [ -z "$bundle" ] || verdict="$(jq -c '.verdict // null' "$bundle")"

  printf '### %s\n\n' "$title"
  jq -r --argjson verdict "$verdict" '
    if $verdict == null then empty
    elif $verdict.result == "invalid" then
      ($verdict.reasons // []) as $r
      | "**Did not count:** \($r[0] // "the run gave no reason").\(if ($r | length) > 1
          then " And \($r | length - 1) more reason(s), in the verdict step." else "" end)\n"
    else "**Counted:** the run measured the system.\n" end' <<<'null'
  jq -r --argjson gates "$gates" \
    --arg header "$(summary_table_header)" \
    --argjson terms "$(summary_terms_json p50 p95 p99 "error rate" throughput "Service Level" "not read")" '
    . as $all |
    def term($w): "<span title=\"\($terms[$w])\">\($w)</span>";
    def rows_value($series; $m): [$all[] | select("\(.source)/\(.scenario)/\(.phase)" == $series) | .[$m]] | first;
    def num: (. * 10 | round) / 10 | tostring;
    # Each metric a row can carry: the words it reads as, and how its value is written.
    def vocabulary: {
      latency_p50_ms: [term("p50"), "ms"], latency_p95_ms: [term("p95"), "ms"],
      latency_p99_ms: [term("p99"), "ms"], error_rate: [term("error rate"), "%"],
      rps: [term("throughput"), "per second"], rps_share_of_plan: ["throughput, share of plan", "%"],
      arrival_share: ["arrived", "%"], within_5s_share: ["within 5 s", "%"],
      machines_caught_up: ["machines caught up", ""], fill_seconds: ["time to fill", "s"],
      readings_per_second: ["readings a second", ""], live_within_5s_share: ["live within 5 s", "%"],
      cpu_share: ["processor", "%"], memory_share: ["memory", "%"],
      peak_memory_share: ["peak memory", "%"], unanswered: ["never answered", ""],
      deferred: ["told to wait", ""], dropped_iterations: ["dropped iterations", ""],
      retained_goroutines_per_operation: ["goroutines kept per operation", ""],
      retained_bytes_per_operation: ["bytes kept per operation", "B"],
      growing_sites: ["heap sites growing", ""], held_agents: ["machines held", ""]};
    def shown($unit; $v): if $unit == "%" then "\($v * 100 | num) %"
                          elif $unit == "" then ($v | num)
                          else "\($v | num) \($unit)" end;
    def limit($unit; $v): if $unit == "%" then "\($v * 100 | num) %"
                          elif $unit == "percent" then "\($v | num) %"
                          elif $unit == "" then ($v | tostring)
                          else "\($v | tostring) \($unit)" end;
    def cpus($c): if $c == null then "" elif $c == 1 then " of 1 processor" else " of \($c) processors" end;
    def bytes($b): if $b == null then ""
                   elif $b >= 1073741824 then " of \($b / 1073741824 | num) GiB"
                   else " of \($b / 1048576 | num) MiB" end;
    def gate($series; $m): [$gates[] | select(.series == $series and .metric == $m)] | first;
    # A rung starved of processor on purpose reads a failing limit as a watched mark.
    def enforced($g; $cpus): $g.blocking and ($g.blocking_from_cpus == null or ($cpus // $g.blocking_from_cpus) >= $g.blocking_from_cpus);
    def crossed($g; $v): $v == null or ($g.max != null and $v > $g.max) or ($g.min != null and $v < $g.min);
    # A measurement with no limit fails, since every measurement is held to one; a measurement that
    # follows another limit reads pass or over with that limit.
    def judged($series; $m; $v; $unit; $cpus):
      gate($series; $m) as $g
      | if $g == null then ["no limit", "FAIL"]
        elif $g.follows != null then
          ($g.follows | split("|")) as [$fs, $fm]
          | gate($fs; $fm) as $f
          | (rows_value($fs; $fm)) as $fv
          | ["with \($fs) \($fm) (pass/over)", (if $f == null or crossed($f; $fv) then "over" else "pass" end)]
        else
          enforced($g; $cpus) as $enforced
          | (if $g.max != null then "≤ \(limit($unit; $g.max))" else "≥ \(limit($unit; $g.min))" end
            + (if $enforced then "" else " (pass/over)" end)) as $expected
          | [$expected, (if crossed($g; $v) | not then "pass" elif $enforced then "FAIL" else "over" end)]
        end;
    def level($name; $series; $m; $p; $cap):
      judged($series; $m; $p; "percent"; null) as $j
      | ["\(term("Service Level")) \($name)", $j[0],
         (if $p == null then term("not read") else "\($p | num) %\($cap)" end), $j[1]];
    def levels($part): [$all[] | select(.source == "service" and .scenario == $part)] | first // {};
    def section($r):
      if $r.source == "catchup" and ($r.scenario | startswith("probe")) then "probe"
      elif $r.source == "service" then "levels" else "main" end;
    def lines($rows):
      $rows[] as $row
      | "\($row.source)/\($row.scenario)/\($row.phase)" as $series
      # The pace delivered reads beside its share of the pace planned, which carries the limit.
      | ($row | to_entries[] | select(vocabulary[.key] != null and .value != null)
         | select(.key != "rps" or $row.rps_share_of_plan == null)) as $e
      | vocabulary[$e.key] as [$words, $unit]
      | judged($series; $e.key; $e.value; $unit; $row.target_cpus) as $j
      | (if $e.key == "rps_share_of_plan" and $row.rps != null
         then "\($row.rps | num) per second, \(shown($unit; $e.value)) of plan" else shown($unit; $e.value) end) as $actual
      | [ "\($series) \($words)", $j[0], $actual, $j[1] ];
    def table($rows): ($header, (lines($rows) | "| \(.[0]) | \(.[1]) | \(.[2]) | \(.[3]) |"));
    (map(select(section(.) == "main")) as $main | if ($main | length) > 0 then table($main) else empty end),
    (map(select(section(.) == "probe")) as $probe
      | if ($probe | length) > 0 then ("", "#### Store probe", "", table($probe)) else empty end),
    (levels("server") as $server | levels("database") as $db
     | ("", "#### Server and database", "", $header,
        ([level("Avg CPU % (server)"; "service/server/measured"; "cpu_percent"; $server.cpu_percent; cpus($server.cpu_cap)),
          level("Avg Mem % (server)"; "service/server/measured"; "memory_percent"; $server.memory_percent;
            bytes($server.memory_bytes)),
          level("Avg CPU % (database)"; "service/database/measured"; "cpu_percent"; $db.cpu_percent;
            (if $db.cpu_cap then " of \($db.cpu_cap)" else "" end)),
          level("Avg Mem % (database)"; "service/database/measured"; "memory_percent"; $db.memory_percent;
            (if $db.memory_cap then " of \($db.memory_cap)" else "" end))][]
         | "| \(.[0]) | \(.[1]) | \(.[2]) | \(.[3]) |")))
  ' "$rows"
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
