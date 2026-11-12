#!/usr/bin/env bash
# Reads every leg of the scaling sweep, publishes the curve with its sizing figure, and fails when
# the legs are identical.
# Usage: perf-scaling-curve.sh <directory holding the legs>
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/lib/loadtest-profile.sh
. "$HERE/lib/loadtest-profile.sh"

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

  local rows="" count=0 path promise=""
  while IFS= read -r path; do
    local row profile
    if [ -z "$promise" ]; then
      profile="$HERE/../load/profiles/$(jq -r '.run.profile_name // "scaling"' "$path").yaml"
      promise="$(signin_promise "$profile")" || return 1
    fi
    if ! row="$(read_leg "$path" "$promise")"; then
      return 1
    fi
    rows+="$row"$'\n'
    count=$((count + 1))
  done <<<"$bundles"

  publish_curve "$rows" "$promise"

  if [ "$count" -lt "$MINIMUM_LEGS" ]; then
    echo "::error::the sweep produced $count leg(s); a curve needs at least $MINIMUM_LEGS points to differ at all." >&2
    return 1
  fi

  check_rungs_are_distinct "$rows" || return 1
  check_legs_are_not_identical "$rows" || return 1
  return 0
}

# read_leg turns one bundle into a tab-separated curve row, its sign-in held to PROMISE, and refuses
# a leg that measured nothing.
read_leg() {
  local path="$1" promise="$2" row kept

  if ! row="$(jq -er '
      [
        (.target.cpus | tostring),
        (.verdict.result),
        ([.observations[]? | select(.series == "connect_p95_ms") | .value] | first // "absent" | tostring),
        ([.phases[]? | .latency_p95_ms // 0] | max | tostring),
        ([.phases[]? | .target_busy_percent] | if length == 0 or any(. == null) then "absent" else (add / length | . * 10 | round / 10 | tostring) end),
        ([.journeys[]? | select(.name == "device-list") | .latency_p95_ms] | first // "absent" | tostring),
        ((reduce (.observations // [])[] as $o ({}; .[$o.series] = $o.value)) as $value
          | def ms: . * 10 | round / 10 | tostring;
          if $value.register_p50_ms == null or $value.register_p95_ms == null then "absent"
          else "\($value.register_p50_ms | ms) / \($value.register_p95_ms | ms)" end)
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

  # The sweep holds technician load constant, so a rung without a technician reading is refused.
  local journey
  journey="$(cut -f6 <<<"$row")"
  if [ "$journey" = "absent" ]; then
    echo "::error::the leg at $path carries no technician reading, so what the sweep varied processors against at that rung was machines arriving and nothing else." >&2
    return 1
  fi

  kept="—"
  if [ -n "$promise" ]; then
    case "$(signin_promise_kept "$path" "$promise")" in
      1) kept="kept" ;;
      0) kept="missed" ;;
      *) kept="—" ;;
    esac
  fi
  printf '%s\t%s\n' "$row" "$kept"
}

# publish_curve prints the curve as a Markdown table, then the smallest processor share whose rung
# keeps the ordinary sign-in promise: the figure a fleet of this size is sized by.
publish_curve() {
  local rows="$1" promise="$2" smallest marks
  echo "### Scaling sweep"
  echo
  echo "| Server processors | Verdict | Connect p95 (ms) | Phase p95 (ms) | Target busy (% of allowance) | Fleet list p95 (ms) | Sign-in p50 / p95 (ms) | Sign-in promise |"
  echo "|---|---|---|---|---|---|---|---|"
  awk -F'\t' 'NF == 8 { printf "| %s | %s | %s | %s | %s | %s | %s | %s |\n", $1, $2, $3, $4, $5, $6, $7, $8 }' <<<"$rows"
  echo
  if [ -z "$promise" ]; then
    echo "Sizing figure: the profile holds the machines' sign-in to no ordinary promise."
    return 0
  fi
  marks="median within $(cut -f1 <<<"$promise") ms, slowest 5% within $(cut -f2 <<<"$promise") ms"
  smallest="$(awk -F'\t' 'NF == 8 && $8 == "kept" && (low == "" || $1 + 0 < low + 0) { low = $1 }
    END { print low }' <<<"$rows")"
  if [ -n "$smallest" ]; then
    echo "Sizing figure: $smallest processors keep the ordinary sign-in promise ($marks)."
  else
    echo "Sizing figure: no rung keeps the ordinary sign-in promise ($marks)."
  fi
}

# check_rungs_are_distinct refuses a sweep in which two legs report the same processor share.
check_rungs_are_distinct() {
  local rows="$1" rungs unique total
  rungs="$(awk -F'\t' 'NF == 8 { print $1 }' <<<"$rows")"
  total="$(grep -c . <<<"$rungs")"
  unique="$(sort -u <<<"$rungs" | grep -c .)"

  if [ "$unique" -lt "$total" ]; then
    echo "::error::the sweep's $total legs name only $unique distinct processor shares, so its rungs are not points on a curve." >&2
    return 1
  fi
}

# check_legs_are_not_identical refuses a sweep whose legs all report the same readings.
check_legs_are_not_identical() {
  local rows="$1" readings unique
  readings="$(awk -F'\t' 'NF == 8 { print $3 "\t" $4 "\t" $5 "\t" $6 "\t" $7 }' <<<"$rows")"
  unique="$(sort -u <<<"$readings" | grep -c .)"

  if [ "$unique" -eq 1 ]; then
    echo "::error::every leg of the sweep reported identical readings, so nothing in it is about the processor share." >&2
    return 1
  fi
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
