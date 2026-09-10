#!/usr/bin/env bash
# Read every leg of the scaling sweep together, publish the curve, and fail when
# the sweep did not measure its own variable.
#
# The sweep runs the same profile against a server given a different processor
# share each time. Four bundles a night were produced and nothing had ever
# compared them: the workflow's two jobs read none of each other's output, there
# was no publish step, no trend and no gate, and the uploads were set to warn on
# an empty file set. A sweep nobody reads is four runs that happen to share a
# profile.
#
# What it refuses is the shape the 2026-09-05 sweep actually had. All four legs
# came back with identical phase results — offered equal to achieved, latency
# absent, no errors, no faults — and a target fingerprint of one processor and
# one byte on every rung. A comparison between processor counts whose legs
# report the same processor count, or the same numbers, is not a comparison.
#
# What it deliberately does not refuse is a curve that fails to rise. One night
# is one sample per rung, and the two nights on record disagreed about the shape
# from the same code and the same profile — so a gate asserting the curve moves
# with the variable would have failed one of them and passed the other. The
# shape is published for a reader; only the sweep's ability to measure at all is
# enforced.
#
# Usage: perf-scaling-curve.sh <directory holding the legs>
set -euo pipefail

usage() {
  echo "usage: $0 <directory holding the legs' bundles>" >&2
}

# minimumLegs is what a curve needs to be one. Two points is the fewest that can
# differ; a single leg is a run, and reporting it as a sweep is how a sweep
# comes to be missing three of its rungs without anything saying so.
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

  check_rungs_are_distinct "$rows" || return 1
  check_legs_are_not_identical "$rows" || return 1
  return 0
}

# read_leg turns one bundle into a row of the curve: the processor share the
# server was given, what the machines waited, and what the server did with the
# allowance. A leg that measured nothing is refused here rather than averaged
# into a shape it says nothing about.
read_leg() {
  local path="$1" row

  if ! row="$(jq -er '
      [
        (.target.cpus | tostring),
        (.verdict.result),
        ([.observations[]? | select(.series == "connect_p95_ms") | .value] | first // "absent" | tostring),
        ([.phases[]? | .latency_p95_ms // 0] | max | tostring),
        ([.phases[]? | .target_busy_percent] | if length == 0 or any(. == null) then "absent" else (add / length | . * 10 | round / 10 | tostring) end)
      ] | @tsv' "$path" 2>/dev/null)"; then
    echo "::error::$path is not a readable bundle, so its rung contributes nothing to the curve." >&2
    return 1
  fi

  local result
  result="$(cut -f2 <<<"$row")"
  if [ "$result" = "invalid" ]; then
    echo "::error::the leg at $path did not measure the system, and a sweep missing a rung has no shape." >&2
    return 1
  fi

  local connect
  connect="$(cut -f3 <<<"$row")"
  if [ "$connect" = "absent" ]; then
    echo "::error::the leg at $path carries no wait time, so there is nothing at that rung to compare." >&2
    return 1
  fi

  printf '%s\n' "$row"
}

# publish_curve prints the shape for a reader. It is the whole point of the
# aggregation: the enforcement below only says the sweep could measure, and what
# the sweep is for is the shape.
publish_curve() {
  local rows="$1"
  echo "### Scaling sweep"
  echo
  echo "| Server processors | Verdict | Connect p95 (ms) | Phase p95 (ms) | Target busy (% of allowance) |"
  echo "|---|---|---|---|---|"
  awk -F'\t' 'NF == 5 { printf "| %s | %s | %s | %s | %s |\n", $1, $2, $3, $4, $5 }' <<<"$rows"
  echo
}

# check_rungs_are_distinct refuses a sweep whose legs report the same processor
# share. Every latency figure is a property of the pair that produced it, so
# four bundles naming one processor each are four runs of the same rung however
# the matrix was written — which is exactly what a fingerprint of one processor
# and one byte on every leg produced.
check_rungs_are_distinct() {
  local rows="$1" rungs unique total
  rungs="$(awk -F'\t' 'NF == 5 { print $1 }' <<<"$rows")"
  total="$(grep -c . <<<"$rungs")"
  unique="$(sort -u <<<"$rungs" | grep -c .)"

  if [ "$unique" -lt "$total" ]; then
    echo "::error::the sweep's $total legs name only $unique distinct processor shares, so its rungs are not points on a curve." >&2
    return 1
  fi
}

# check_legs_are_not_identical refuses a sweep whose legs all came back saying
# the same thing. A comparison between rungs that produced one answer measured
# something other than the rungs.
check_legs_are_not_identical() {
  local rows="$1" readings unique
  readings="$(awk -F'\t' 'NF == 5 { print $3 "\t" $4 "\t" $5 }' <<<"$rows")"
  unique="$(sort -u <<<"$readings" | grep -c .)"

  if [ "$unique" -eq 1 ]; then
    echo "::error::every leg of the sweep reported identical readings, so nothing in it is about the processor share." >&2
    return 1
  fi
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
