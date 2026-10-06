#!/usr/bin/env bash
# Reads every leg of the volume family, publishes the curve, and refuses legs that held the same
# estate, went unweighed, lacked a technician reading or reported identical readings.
# Usage: perf-volume-curve.sh <directory holding the legs>
set -euo pipefail

usage() {
  echo "usage: $0 <directory holding the legs' bundles>" >&2
}

# Two points is the fewest a curve needs to differ.
MINIMUM_LEGS=2

main() {
  if [ "$#" -ne 1 ]; then
    usage
    return 2
  fi

  local root="$1"
  if [ ! -d "$root" ]; then
    echo "::error::there is no directory at $root, so no leg of the sweep can be read." >&2
    return 1
  fi

  local bundles
  bundles="$(find "$root" -name bundle.json -type f | sort)"
  if [ -z "$bundles" ]; then
    echo "::error::no leg of the sweep left a bundle under $root, so nothing was measured." >&2
    return 1
  fi

  local rows="" count=0 path
  while IFS= read -r path; do
    local row
    if ! row="$(read_leg "$path")"; then
      return 1
    fi
    rows+="$row"$'\n'
    count=$((count + 1))
  done <<<"$bundles"

  publish_curve "$rows"

  if [ "$count" -lt "$MINIMUM_LEGS" ]; then
    echo "::error::the sweep produced $count leg(s); a curve needs at least $MINIMUM_LEGS points to differ at all." >&2
    return 1
  fi

  check_estates_are_distinct "$rows" || return 1
  check_legs_are_not_identical "$rows" || return 1
  return 0
}

# read_leg turns one bundle into a tab-separated curve row and refuses a leg that measured nothing.
read_leg() {
  local path="$1" row

  if ! row="$(jq -er '
      [
        (.fixture.devices // 0 | tostring),
        (.verdict.result),
        (.fixture.database_bytes // 0 | tostring),
        ([.observations[]? | select(.series == "register_p95_ms") | .value] | first // "absent" | tostring),
        ([.journeys[]? | select(.name == "device-list") | .latency_p95_ms] | first // "absent" | tostring)
      ] | @tsv' "$path" 2>/dev/null)"; then
    echo "::error::$path is not a readable bundle, so its leg contributes nothing to the curve." >&2
    return 1
  fi

  local result
  result="$(cut -f2 <<<"$row")"
  if [ "$result" = "invalid" ]; then
    echo "::error::the leg at $path did not measure the system, and a sweep missing a leg has no shape." >&2
    return 1
  fi

  # An unweighed leg has no data size to place on the curve's axis.
  local bytes
  bytes="$(cut -f3 <<<"$row")"
  if [ "$bytes" = "0" ]; then
    echo "::error::the leg at $path was never weighed, so there is no amount of data to draw its reading against." >&2
    return 1
  fi

  local register
  register="$(cut -f4 <<<"$row")"
  if [ "$register" = "absent" ]; then
    echo "::error::the leg at $path carries no registration reading, so the write path this family slows has nothing recorded at that size." >&2
    return 1
  fi

  # The family holds technician load constant, so a leg without a technician reading is refused.
  local journey
  journey="$(cut -f5 <<<"$row")"
  if [ "$journey" = "absent" ]; then
    echo "::error::the leg at $path carries no technician reading, so what this leg varied the estate size against was machines arriving and nothing else." >&2
    return 1
  fi

  printf '%s\n' "$row"
}

# publish_curve prints the curve as a Markdown table.
publish_curve() {
  local rows="$1"
  echo "### Volume sweep"
  echo
  echo "| Machines enrolled | Verdict | Database (MB) | Register p95 (ms) | Fleet list p95 (ms) |"
  echo "|---|---|---|---|---|"
  awk -F'\t' 'NF == 5 { printf "| %s | %s | %.1f | %s | %s |\n", $1, $2, $3 / 1048576, $4, $5 }' <<<"$rows"
  echo
}

# check_estates_are_distinct refuses a sweep whose legs held the same estate.
check_estates_are_distinct() {
  local rows="$1" estates unique total
  estates="$(awk -F'\t' 'NF == 5 { print $1 }' <<<"$rows")"
  total="$(grep -c . <<<"$estates")"
  unique="$(sort -u <<<"$estates" | grep -c .)"

  if [ "$unique" -lt "$total" ]; then
    echo "::error::the sweep's $total legs hold only $unique distinct estates, so they are not distinct points on a curve." >&2
    return 1
  fi
}

# check_legs_are_not_identical refuses a sweep whose legs all report the same readings.
check_legs_are_not_identical() {
  local rows="$1" readings unique
  readings="$(awk -F'\t' 'NF == 5 { print $4 "\t" $5 }' <<<"$rows")"
  unique="$(sort -u <<<"$readings" | grep -c .)"

  if [ "$unique" -eq 1 ]; then
    echo "::error::every leg of the sweep reported identical readings, so nothing in it is about how much data was there." >&2
    return 1
  fi
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
