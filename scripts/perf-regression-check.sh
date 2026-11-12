#!/usr/bin/env bash
# Compares each leg with the median of its unflagged nights on its processor over fourteen dates,
# three at least; each kind of series is judged by its own rule, set beside the constants below.
#
# Environment:
#   VM_RUN_STARTED_AT  the run's start, in seconds since the epoch (required)
#   PERF_FLAGGED_FILE  where each series past its line is written for the push (required)
#   PERF_NIGHTS_TABLE  where the night is written as a table of each series against its nights
#
# Exit codes:
#   0  nothing past its window
#   1  a held leg past its window, a breakpoint below its median, or a rung missing its promise
#   2  the question could not be asked
#
# Usage: perf-regression-check.sh <bundle.json>...   (a leg is named by the bundle's directory)
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/lib/vm-query.sh
. "$HERE/lib/vm-query.sh"
# shellcheck source=scripts/lib/loadtest-profile.sh
. "$HERE/lib/loadtest-profile.sh"
# shellcheck source=scripts/lib/summary-table.sh
. "$HERE/lib/summary-table.sh"

PROFILES="$HERE/../load/profiles"
WINDOW_DATES=14
MIN_WINDOW_DATES=3
# A held leg's tail past three times its median, or its error rate past twice, is flagged, and
# fails on a second night in three.
LATENCY_REL_TOL=2.0
ERROR_RATE_REL_TOL=1.0
# A level past one and a half times its median reads over and fails nothing.
LEVEL_REL_TOL=0.5
MEASUREMENTS=(latency_p95_ms latency_p50_ms error_rate cpu_percent memory_percent
  breakpoint_held_agents signin_promise_kept)

# held_leg reports whether a leg holds its load, so its window decides.
held_leg() {
  case "$1" in
    scaling-1 | scaling-2 | volume-500 | volume-2000 | peak | soak) return 0 ;;
    *) return 1 ;;
  esac
}

num_gt() { awk -v a="$1" -v b="$2" 'BEGIN { exit !(a + 0 > b + 0) }'; }
num_pos() { awk -v a="$1" 'BEGIN { exit !(a + 0 > 0) }'; }
short() { awk -v a="$1" 'BEGIN { printf "%g", a }'; }
times() { awk -v t="$1" 'BEGIN { printf "%g", 1 + t }'; }
line_above() { awk -v m="$1" -v t="$2" 'BEGIN { printf "%.6f", m * (1 + t) }'; }

# line_words is a line drawn a tolerance above the median of its nights, as the table reads it.
line_words() { # threshold tol median count
  printf '≤ %s: %s times the median %s of %s nights on this chip' "$(short "$1")" "$(times "$2")" \
    "$(short "$3")" "$4"
}

ROWS_FILE=""
SERIES='[]'
REGRESSIONS=()
TABLE=()
cleanup() { [ -z "$ROWS_FILE" ] || rm -f "$ROWS_FILE"; }
trap cleanup EXIT

# words names a measurement as the table reads it.
words() {
  case "$1" in
    latency_p95_ms) printf 'latency p95 (ms)' ;;
    latency_p50_ms) printf 'latency p50 (ms)' ;;
    error_rate) printf 'error rate' ;;
    cpu_percent) printf 'processor (%% of allowance)' ;;
    memory_percent) printf 'memory (%% of allowance)' ;;
    *) printf '%s' "$1" ;;
  esac
}

record() { # measurement expected actual result
  TABLE+=("$(summary_table_row "$1" "$2" "$3" "$4")")
}

# nights_of prints measurement, leg/phase[:scenario]/workload/processor, date, value and flagged,
# tab-separated, a night: a night is compared only with nights on its processor.
nights_of() {
  vm_query_nightly "perf_$1" 'env="ci"' "$WINDOW_DATES" | awk -F'\t' -v m="$1" '
    {
      leg = ""; phase = ""; scenario = ""; workload = ""; processor = ""; flagged = 0
      n = split($1, parts, ",")
      for (i = 1; i <= n; i++) {
        split(parts[i], kv, "=")
        if (kv[1] == "leg") leg = kv[2]
        if (kv[1] == "phase") phase = kv[2]
        # The server and the database share the measured phase, so a level names its scenario.
        if (kv[1] == "scenario") scenario = ":" kv[2]
        # A workload is name/version/digest; nights of one load version share a window.
        if (kv[1] == "workload") { split(kv[2], w, "/"); workload = w[1] "/" w[2] }
        if (kv[1] == "processor") processor = kv[2]
        if (kv[1] == "flagged" && kv[2] == "1") flagged = 1
      }
      print m "\t" leg "/" phase scenario "/" workload "/" processor "\t" $2 "\t" $3 "\t" flagged
    }
  '
}

# windows prints one object a series: the median, count and sum of its unflagged nights, and how
# many of its two latest nights were flagged.
windows() {
  jq -Rnc '
    [ inputs | split("\t") | select(length >= 5)
      | {m: .[0], key: .[1], date: .[2], value: (.[3] | tonumber), flagged: (.[4] == "1")} ]
    | group_by([.m, .key])
    | map((map(select(.flagged | not) | .value) | sort) as $w
        | { m: .[0].m, key: .[0].key, count: ($w | length), sum: ($w | add),
            median: ($w | if length == 0 then null elif length % 2 == 1 then .[(length - 1) / 2]
                          else (.[length / 2 - 1] + .[length / 2]) / 2 end),
            prior_flagged: (group_by(.date) | map(any(.[]; .flagged)) | .[-2:] | map(select(.)) | length) })
  ' <<<"$1"
}

# window_of prints a series' median, count, prior flags and sum, or nothing with too few nights.
window_of() { # measurement key
  jq -r --arg m "$1" --arg k "$2" --argjson least "$MIN_WINDOW_DATES" '.[]
    | select(.m == $m and .key == $k and .count >= $least and .median != null)
    | "\(.median)\t\(.count)\t\(.prior_flagged)\t\(.sum)"' <<<"$SERIES"
}

# judge_tail holds a tail or an error rate to its tolerance; a held leg fails on a second night in
# three past it, and a leg that saturates its venue is reported.
judge_tail() { # leg workload measurement phase current flagged_file
  local leg="$1" workload="$2" measurement="$3" phase="$4" current="$5" flagged_file="$6"
  local name entry median count prior tol threshold expected line
  name="$leg · $phase · $(words "$measurement")"
  entry="$(window_of "$measurement" "$leg/$phase/$workload")"
  IFS=$'\t' read -r median count prior _ <<<"$entry"
  if [ -z "$entry" ] || ! num_pos "$median"; then
    record "$name" "fewer than $MIN_WINDOW_DATES nights on this chip" "$current" "—"
    return 0
  fi
  tol="$LATENCY_REL_TOL"
  [ "$measurement" = "error_rate" ] && tol="$ERROR_RATE_REL_TOL"
  threshold="$(line_above "$median" "$tol")"
  expected="$(line_words "$threshold" "$tol" "$median" "$count")"
  if ! num_gt "$current" "$threshold"; then
    record "$name" "$expected" "$current" "pass"
    return 0
  fi
  printf '%s\t%s\t%s\n' "$leg" "$phase" "$measurement" >>"$flagged_file"
  line="$leg $phase $measurement: $median -> $current (past $(times "$tol") times the median of its last ${WINDOW_DATES} nights)"
  if ! held_leg "$leg"; then
    echo "REPORTED:$line — this leg saturates the venue on some nights and not others, so its fixed limits decide"
    record "$name" "$expected" "$current" "over"
  elif [ "$prior" -gt 0 ]; then
    REGRESSIONS+=("$line, and on one of its two nights before")
    record "$name" "$expected" "$current" "FAIL"
  else
    echo "FLAGGED:$line — flagged and kept out of later windows; the leg fails if it is past its line again on either of the next two nights"
    record "$name" "$expected" "$current" "FLAGGED"
  fi
}

# judge_level reports the server's or the database's level past its tolerance.
judge_level() { # leg workload measurement scenario current
  local leg="$1" workload="$2" measurement="$3" scenario="$4" current="$5"
  local name entry median count threshold expected
  name="$leg · $scenario · $(words "$measurement")"
  entry="$(window_of "$measurement" "$leg/measured:$scenario/$workload")"
  IFS=$'\t' read -r median count _ _ <<<"$entry"
  if [ -z "$entry" ] || ! num_pos "$median"; then
    record "$name" "fewer than $MIN_WINDOW_DATES nights on this chip" "$(short "$current")" "—"
    return 0
  fi
  threshold="$(line_above "$median" "$LEVEL_REL_TOL")"
  expected="$(line_words "$threshold" "$LEVEL_REL_TOL" "$median" "$count")"
  if num_gt "$current" "$threshold"; then
    echo "REPORTED:$leg $scenario $measurement: $(short "$median") -> $(short "$current") (past $(times "$LEVEL_REL_TOL") times the median of its last ${WINDOW_DATES} nights)"
    record "$name" "$expected" "$(short "$current")" "over"
  else
    record "$name" "$expected" "$(short "$current")" "pass"
  fi
}

# judge_capacity fails a ladder that held fewer machines than the median of its nights.
judge_capacity() { # bundle leg workload
  local bundle="$1" leg="$2" workload="$3" held entry median count expected
  held="$(jq -r '.breaking_point.held_agents // empty' "$bundle")"
  [ -n "$held" ] || return 0
  entry="$(window_of breakpoint_held_agents "$leg/ladder/$workload")"
  IFS=$'\t' read -r median count _ _ <<<"$entry"
  if [ -z "$entry" ]; then
    record "$leg · machines held" "fewer than $MIN_WINDOW_DATES nights on this chip" "$held" "—"
    return 0
  fi
  expected="≥ $(short "$median"): the median of $count nights on this chip"
  if awk -v a="$held" -v b="$median" 'BEGIN { exit !(a + 0 < b + 0) }'; then
    REGRESSIONS+=("$leg held_agents: $(short "$median") -> $held (below the median of its last ${WINDOW_DATES} nights on this chip)")
    record "$leg · machines held" "$expected" "$held" "FAIL"
  else
    record "$leg · machines held" "$expected" "$held" "pass"
  fi
}

# judge_promise fails a scaling rung that missed the ordinary sign-in promise on a chip where most
# of its nights kept it, and reports one that missed it where most of them did too.
judge_promise() { # bundle leg workload
  local bundle="$1" leg="$2" workload="$3" promise kept tonight entry count sum expected
  [ "$(jq -r '.run.profile_name // empty' "$bundle")" = scaling ] || return 0
  promise="$(signin_promise "$PROFILES/scaling.yaml")" || return 2
  [ -n "$promise" ] || return 0
  kept="$(signin_promise_kept "$bundle" "$promise")"
  [ -n "$kept" ] || return 0
  tonight="missed"
  [ "$kept" = 1 ] && tonight="kept"
  entry="$(window_of signin_promise_kept "$leg/register/$workload")"
  IFS=$'\t' read -r _ count _ sum <<<"$entry"
  if [ -z "$entry" ]; then
    record "$leg · sign-in promise" "fewer than $MIN_WINDOW_DATES nights on this chip" "$tonight" "—"
    return 0
  fi
  expected="kept on $(short "$sum") of its $count nights on this chip"
  if [ "$kept" = 1 ]; then
    record "$leg · sign-in promise" "$expected" "$tonight" "pass"
  elif awk -v s="$sum" -v n="$count" 'BEGIN { exit !(s * 2 > n) }'; then
    REGRESSIONS+=("$leg sign-in promise: missed on a chip where most of its last ${WINDOW_DATES} nights kept it")
    record "$leg · sign-in promise" "$expected" "$tonight" "FAIL"
  else
    echo "REPORTED:$leg sign-in promise: missed, as on most of its last ${WINDOW_DATES} nights on this chip"
    record "$leg · sign-in promise" "$expected" "$tonight" "missed"
  fi
}

write_table() {
  [ -n "${PERF_NIGHTS_TABLE:-}" ] && [ "${#TABLE[@]}" -gt 0 ] || return 0
  {
    echo "### Each leg against its own nights"
    echo
    summary_table_header_explaining "The leg, where in the night it was read, and what; Expected is the line its own nights on this chip draw."
    printf '%s\n' "${TABLE[@]}"
    echo
  } >>"$PERF_NIGHTS_TABLE"
}

main() {
  if [ "$#" -eq 0 ]; then
    echo "usage: $0 <bundle.json>..." >&2
    return 2
  fi
  vm_tonight >/dev/null || return 2
  local flagged_file="${PERF_FLAGGED_FILE:?PERF_FLAGGED_FILE names where flagged series are written}"
  : >"$flagged_file"

  local nights="" measurement
  for measurement in "${MEASUREMENTS[@]}"; do
    nights+="$(nights_of "$measurement")"$'\n'
  done
  SERIES="$(windows "$nights")"

  ROWS_FILE="$(mktemp)"
  local bundle leg workload verdict line phase current scenario
  for bundle in "$@"; do
    [ -s "$bundle" ] || {
      echo "::error::no evidence bundle at $bundle" >&2
      return 2
    }
    leg="$(basename "$(dirname "$bundle")")"
    verdict="$(jq -r '.verdict.result // empty' "$bundle")"
    if [ "$verdict" = "invalid" ] || [ -z "$verdict" ]; then
      echo "leg $leg did not measure the system, so it is compared with nothing"
      record "$leg" "—" "did not measure the system" "—"
      continue
    fi
    workload="$(jq -r '"\(.run.profile_name)/\(.run.profile_version)/\(.runner.processor_model // "")"' "$bundle")"
    "$HERE/loadtest-bundle-rows.sh" "$bundle" "$ROWS_FILE" >/dev/null

    while IFS=$'\t' read -r measurement phase current; do
      [ -n "$current" ] || continue
      judge_tail "$leg" "$workload" "$measurement" "$phase" "$current" "$flagged_file"
    done < <(jq -r '.[] | select(.source != "service") | ("latency_p95_ms", "latency_p50_ms", "error_rate") as $m
      | select(.[$m] != null) | [$m, .phase, .[$m]] | @tsv' "$ROWS_FILE")
    while IFS=$'\t' read -r measurement scenario current; do
      [ -n "$current" ] || continue
      judge_level "$leg" "$workload" "$measurement" "$scenario" "$current"
    done < <(jq -r '.[] | select(.source == "service") | ("cpu_percent", "memory_percent") as $m
      | select(.[$m] != null) | [$m, .scenario, .[$m]] | @tsv' "$ROWS_FILE")
    judge_capacity "$bundle" "$leg" "$workload"
    judge_promise "$bundle" "$leg" "$workload" || return 2
  done
  write_table

  if [ "${#REGRESSIONS[@]}" -eq 0 ]; then
    echo "no leg is past the nights before it"
    return 0
  fi
  echo "REGRESSION_ALERT:Performance regression on ${GITHUB_REF_NAME:-dev}"
  echo "REGRESSION_ALERT:"
  for line in "${REGRESSIONS[@]}"; do
    echo "REGRESSION_ALERT:  - $line"
  done
  return 1
}

main "$@"
