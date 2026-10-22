#!/usr/bin/env bash
# Compares each leg with the median of its unflagged nights of the previous fourteen dates (three
# needed); a held leg past three times that median fails when one of its two nights before was too.
#
# Environment:
#   VM_RUN_STARTED_AT  the run's start, in seconds since the epoch (required)
#   PERF_FLAGGED_FILE  where each series past its line is written for the push (required)
#
# Exit codes:
#   0  nothing past its window
#   1  a held leg past its window
#   2  the question could not be asked
#
# Usage: perf-regression-check.sh <bundle.json>...   (a leg is named by the bundle's directory)
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/lib/vm-query.sh
. "$HERE/lib/vm-query.sh"

WINDOW_DATES=14
MIN_WINDOW_DATES=3
LATENCY_REL_TOL=2.0
ERROR_RATE_REL_TOL=1.0

# held_leg reports whether a leg holds its load, so its window decides.
held_leg() {
  case "$1" in
    scaling-1 | scaling-2 | volume-500 | volume-2000 | peak | soak) return 0 ;;
    *) return 1 ;;
  esac
}

num_gt() { awk -v a="$1" -v b="$2" 'BEGIN { exit !(a + 0 > b + 0) }'; }
num_pos() { awk -v a="$1" 'BEGIN { exit !(a + 0 > 0) }'; }

ROWS_FILE=""
cleanup() { [ -z "$ROWS_FILE" ] || rm -f "$ROWS_FILE"; }
trap cleanup EXIT

# nights_of prints "measurement<TAB>leg/phase/workload<TAB>date<TAB>value<TAB>flagged" a night.
nights_of() {
  vm_query_nightly "perf_$1" 'env="ci"' "$WINDOW_DATES" | awk -F'\t' -v m="$1" '
    {
      leg = ""; phase = ""; workload = ""; flagged = 0
      n = split($1, parts, ",")
      for (i = 1; i <= n; i++) {
        split(parts[i], kv, "=")
        if (kv[1] == "leg") leg = kv[2]
        if (kv[1] == "phase") phase = kv[2]
        if (kv[1] == "workload") workload = kv[2]
        if (kv[1] == "flagged" && kv[2] == "1") flagged = 1
      }
      print m "\t" leg "/" phase "/" workload "\t" $2 "\t" $3 "\t" flagged
    }
  '
}

# windows prints one object a series: the median and count of its unflagged nights, and how many
# of its two latest nights were flagged.
windows() {
  jq -Rnc '
    [ inputs | split("\t") | select(length >= 5)
      | {m: .[0], key: .[1], date: .[2], value: (.[3] | tonumber), flagged: (.[4] == "1")} ]
    | group_by([.m, .key])
    | map((map(select(.flagged | not) | .value) | sort) as $w
        | { m: .[0].m, key: .[0].key, count: ($w | length),
            median: ($w | if length == 0 then null elif length % 2 == 1 then .[(length - 1) / 2]
                          else (.[length / 2 - 1] + .[length / 2]) / 2 end),
            prior_flagged: (group_by(.date) | map(any(.[]; .flagged)) | .[-2:] | map(select(.)) | length) })
  ' <<<"$1"
}

main() {
  if [ "$#" -eq 0 ]; then
    echo "usage: $0 <bundle.json>..." >&2
    return 2
  fi
  vm_tonight >/dev/null || return 2
  local flagged_file="${PERF_FLAGGED_FILE:?PERF_FLAGGED_FILE names where flagged series are written}"
  : >"$flagged_file"

  local nights="" measurement series
  for measurement in latency_p95_ms latency_p50_ms error_rate; do
    nights+="$(nights_of "$measurement")"$'\n'
  done
  series="$(windows "$nights")"

  ROWS_FILE="$(mktemp)"
  local bundle leg workload verdict regressions=() line
  for bundle in "$@"; do
    [ -s "$bundle" ] || {
      echo "::error::no evidence bundle at $bundle" >&2
      return 2
    }
    leg="$(basename "$(dirname "$bundle")")"
    verdict="$(jq -r '.verdict.result // empty' "$bundle")"
    if [ "$verdict" = "invalid" ] || [ -z "$verdict" ]; then
      echo "leg $leg did not measure the system, so it is compared with nothing"
      continue
    fi
    workload="$(jq -r '"\(.run.profile_name)/\(.run.profile_version)/\(.run.profile_digest)"' "$bundle")"
    "$HERE/loadtest-bundle-rows.sh" "$bundle" "$ROWS_FILE" >/dev/null

    while IFS=$'\t' read -r measurement phase current; do
      [ -n "$current" ] || continue
      local entry median count prior tol threshold
      entry="$(jq -r --arg m "$measurement" --arg k "$leg/$phase/$workload" \
        '.[] | select(.m == $m and .key == $k) | "\(.median)\t\(.count)\t\(.prior_flagged)"' <<<"$series")"
      IFS=$'\t' read -r median count prior <<<"$entry"
      if [ -z "$entry" ] || [ "${count:-0}" -lt "$MIN_WINDOW_DATES" ] || ! num_pos "$median"; then
        continue
      fi
      tol="$LATENCY_REL_TOL"
      [ "$measurement" = "error_rate" ] && tol="$ERROR_RATE_REL_TOL"
      threshold="$(awk -v m="$median" -v t="$tol" 'BEGIN { printf "%.6f", m * (1 + t) }')"
      num_gt "$current" "$threshold" || continue
      printf '%s\t%s\t%s\n' "$leg" "$phase" "$measurement" >>"$flagged_file"
      line="$leg $phase $measurement: $median -> $current (past $(awk -v t="$tol" 'BEGIN { printf "%g", 1 + t }') times the median of its last ${WINDOW_DATES} nights)"
      if ! held_leg "$leg"; then
        echo "REPORTED:$line — this leg saturates the venue on some nights and not others, so its fixed limits decide"
      elif [ "$prior" -gt 0 ]; then
        regressions+=("$line, and on one of its two nights before")
      else
        echo "FLAGGED:$line — flagged and kept out of later windows; the leg fails if it is past its line again on either of the next two nights"
      fi
    done < <(jq -r '.[] | ("latency_p95_ms", "latency_p50_ms", "error_rate") as $m
      | select(.[$m] != null) | [$m, .phase, .[$m]] | @tsv' "$ROWS_FILE")
  done

  if [ "${#regressions[@]}" -eq 0 ]; then
    echo "no leg is past the nights before it"
    return 0
  fi
  echo "REGRESSION_ALERT:Performance regression on ${GITHUB_REF_NAME:-dev}"
  echo "REGRESSION_ALERT:"
  for line in "${regressions[@]}"; do
    echo "REGRESSION_ALERT:  - $line"
  done
  return 1
}

main "$@"
