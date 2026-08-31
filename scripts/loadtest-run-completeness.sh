#!/usr/bin/env bash
# Record which scenarios produced rows, and classify the night.
#
# A run has three outcomes, not two. Valid and failed are both measurements: one
# of a system that held, one of a system that did not. Invalid is the third and
# it is the one that was missing — the run did not measure the system, because a
# scenario produced nothing at all.
#
# Keeping invalid separate is the whole point. A night where one half ran and
# the other produced nothing, absorbed as data, pulls the window median down;
# the next genuinely slow night is then compared against that lowered median and
# passes. One partial night costs two.
#
# Reads the canonical rows the summarizer produced and the scenarios the run was
# supposed to produce, and writes a completeness record naming both halves — so
# a reader sees what ran rather than inferring it from which rows arrived.
#
# Environment:
#   LOADTEST_EXPECTED_SCENARIOS  space-separated scenario names the run owed
#                                (default: the four the nightly runs)
#   LOADTEST_K6_SUMMARY_DIR      where the k6 exports and any threshold breach
#                                records were written
#
# Usage: loadtest-run-completeness.sh <loadtest-summary.json> [completeness.json]
set -euo pipefail

DEFAULT_EXPECTED="api-baseline concurrent-agents relay-throughput quic-agents"

# maxErrorRate is the ceiling past which a scenario's numbers describe the error
# path rather than the system. It is the figure the harness classifies its own
# runs against (server/tests/loadtest/validity.go), held equal here because a run
# has one verdict however many places compute it.
MAX_ERROR_RATE="${LOADTEST_MAX_ERROR_RATE:-0.25}"

usage() {
  echo "usage: $0 <loadtest-summary.json> [completeness.json]" >&2
}

# produced_scenarios lists every scenario the canonical rows carry.
produced_scenarios() {
  jq -r '[.[].scenario] | unique | .[]' "$1"
}

# unmeasured_scenarios lists the scenarios that emitted rows without measuring
# anything.
#
# A row is not a measurement. A scenario whose every request failed produced
# exactly as many rows as one that worked, and those rows carry zeroes — which
# absorbed as data pull the window median down and let the next genuinely slow
# night compare favourably. A scenario that mostly worked is a different thing
# and stays: a degrading night is what the trend is for.
unmeasured_scenarios() {
  jq -r --argjson ceiling "$MAX_ERROR_RATE" '
    [ .[] | select(.error_rate != null and .error_rate > $ceiling) | .scenario ]
    | unique | .[]
  ' "$1"
}

# breached_thresholds lists the scenarios whose marks were breached. The runner
# writes one file per breach beside the export, because whether a mark is
# blocking is the profile's decision and an exit code cannot carry it this far.
breached_thresholds() {
  local dir="${LOADTEST_K6_SUMMARY_DIR:-loadtest-k6}"
  [ -d "$dir" ] || return 0
  find "$dir" -maxdepth 1 -type f -name '*.thresholds' -printf '%f\n' 2>/dev/null \
    | sed 's/\.thresholds$//' \
    | sort
}

main() {
  if [ "$#" -lt 1 ] || [ "$#" -gt 2 ]; then
    usage
    return 2
  fi

  local summary="$1"
  local out="${2:-loadtest-completeness.json}"

  if [ ! -s "$summary" ]; then
    echo "::error::missing or empty canonical summary: $summary" >&2
    return 2
  fi

  local expected produced missing unexpected unmeasured breached result
  expected="$(printf '%s\n' "${LOADTEST_EXPECTED_SCENARIOS:-$DEFAULT_EXPECTED}" | tr ' ' '\n' | sed '/^$/d' | sort -u)"
  produced="$(produced_scenarios "$summary")"
  missing="$(comm -23 <(printf '%s\n' "$expected") <(printf '%s\n' "$produced"))"
  unexpected="$(comm -13 <(printf '%s\n' "$expected") <(printf '%s\n' "$produced"))"
  unmeasured="$(unmeasured_scenarios "$summary")"
  breached="$(breached_thresholds)"

  result="valid"
  if [ -n "$missing" ] || [ -n "$unexpected" ] || [ -n "$unmeasured" ]; then
    result="invalid"
  elif [ -n "$breached" ]; then
    result="failed"
  fi

  jq -n \
    --arg result "$result" \
    --arg commit "${GITHUB_SHA:-$(git rev-parse HEAD 2>/dev/null || echo unknown)}" \
    --arg timestamp "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    --argjson expected "$(printf '%s\n' "$expected" | jq -Rn '[inputs | select(length > 0)]')" \
    --argjson produced "$(printf '%s\n' "$produced" | jq -Rn '[inputs | select(length > 0)]')" \
    --argjson missing "$(printf '%s\n' "$missing" | jq -Rn '[inputs | select(length > 0)]')" \
    --argjson unexpected "$(printf '%s\n' "$unexpected" | jq -Rn '[inputs | select(length > 0)]')" \
    --argjson unmeasured "$(printf '%s\n' "$unmeasured" | jq -Rn '[inputs | select(length > 0)]')" \
    --argjson breached "$(printf '%s\n' "$breached" | jq -Rn '[inputs | select(length > 0)]')" \
    '{
      result: $result,
      commit: $commit,
      timestamp: $timestamp,
      expected_scenarios: $expected,
      produced_scenarios: $produced,
      missing_scenarios: $missing,
      unexpected_scenarios: $unexpected,
      unmeasured_scenarios: $unmeasured,
      threshold_breaches: $breached
    }' >"$out"

  cat "$out"

  if [ "$result" = "invalid" ]; then
    [ -z "$missing" ] || echo "::error::scenarios produced no rows: $(printf '%s' "$missing" | tr '\n' ' ')" >&2
    [ -z "$unexpected" ] || echo "::error::rows arrived from scenarios nobody asked for: $(printf '%s' "$unexpected" | tr '\n' ' ')" >&2
    [ -z "$unmeasured" ] || echo "::error::scenarios whose error rate is past ${MAX_ERROR_RATE}, so their rows describe the error path: $(printf '%s' "$unmeasured" | tr '\n' ' ')" >&2
    echo "::error::this run is invalid and must not enter the trend." >&2
    return 3
  fi

  return 0
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
