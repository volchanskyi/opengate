#!/usr/bin/env bash
# Hold the performance stack's and the endurance run's legs to the nights before
# them, beside the fixed limits their profiles declare.
#
# Each leg is compared with the median of the latest reading of each of the
# fourteen dates before tonight's, needing three, read through
# scripts/lib/vm-query.sh — the comparison the load test makes. The fixed limits
# are read in the leg's own job (scripts/perf-bundle-limits.sh) and are not
# repeated here.
#
# Calibrated offline from the legs' bundles of 2026-09-13 to 2026-09-29, the
# nights of the Docker proxy fault on 2026-09-25 to 09-27 set aside:
#
#   * A leg that holds its load moved at most 1.73 times its median from one
#     night to the next (volume-500's registration tail), so it fails past three
#     times. That would have caught the proxy fault's peak leg at 90 times.
#   * A leg driven to or past what the venue holds — a quarter or half of a
#     processor, eight thousand machines, the spike and the breakpoint — swung by
#     up to 1400 times on unchanged code, because a night either saturates the
#     venue or it does not. No band separates a regression from that, so the
#     comparison is reported and decides nothing; the leg's fixed limits still
#     do.
#
# A leg whose bundle says it did not measure the system is compared with
# nothing.
#
# Environment:
#   VM_RUN_STARTED_AT  the run's start, in seconds since the epoch (required)
#
# Exits: 0 nothing past its window, 1 a held leg past its window, 2 the question
# could not be asked.
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

# held_leg LEG — whether the leg holds its load, so its window decides.
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

# window MEASUREMENT — "leg/phase/workload<TAB>median<TAB>count" per series.
window() {
  vm_nightly_window "perf_$1" 'env="ci"' "$WINDOW_DATES" | awk -F'\t' '
    {
      leg = ""; phase = ""; workload = ""
      n = split($1, parts, ",")
      for (i = 1; i <= n; i++) {
        split(parts[i], kv, "=")
        if (kv[1] == "leg") leg = kv[2]
        if (kv[1] == "phase") phase = kv[2]
        if (kv[1] == "workload") workload = kv[2]
      }
      print leg "/" phase "/" workload "\t" $2 "\t" $3
    }
  '
}

main() {
  if [ "$#" -eq 0 ]; then
    echo "usage: $0 <bundle.json>..." >&2
    return 2
  fi
  vm_tonight >/dev/null || return 2

  local windows measurement
  windows=""
  for measurement in latency_p95_ms latency_p50_ms error_rate; do
    windows+="$(window "$measurement" | sed "s|^|$measurement\t|")"$'\n'
  done

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
    workload="$(jq -r '"\(.run.profile_name)/\(.run.profile_version)"' "$bundle")"
    "$HERE/loadtest-bundle-rows.sh" "$bundle" "$ROWS_FILE" >/dev/null

    while IFS=$'\t' read -r measurement phase current; do
      [ -n "$current" ] || continue
      local entry median count tol threshold
      entry="$(awk -F'\t' -v m="$measurement" -v k="$leg/$phase/$workload" '$1 == m && $2 == k { print $3 "\t" $4 }' <<<"$windows")"
      median="${entry%%$'\t'*}"
      count="${entry##*$'\t'}"
      if [ -z "$entry" ] || [ "${count:-0}" -lt "$MIN_WINDOW_DATES" ] || ! num_pos "$median"; then
        continue
      fi
      tol="$LATENCY_REL_TOL"
      [ "$measurement" = "error_rate" ] && tol="$ERROR_RATE_REL_TOL"
      threshold="$(awk -v m="$median" -v t="$tol" 'BEGIN { printf "%.6f", m * (1 + t) }')"
      num_gt "$current" "$threshold" || continue
      line="$leg $phase $measurement: $median -> $current (past $(awk -v t="$tol" 'BEGIN { printf "%g", 1 + t }') times the median of its last ${WINDOW_DATES} nights)"
      if held_leg "$leg"; then
        regressions+=("$line")
      else
        echo "REPORTED:$line — this leg saturates the venue on some nights and not others, so its fixed limits decide"
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
