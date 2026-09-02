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
#   LOADTEST_BUNDLE              the QUIC harness's evidence bundle, which
#                                carries what the target was holding either side
#                                of the run (default: the path the workflow
#                                collects it to)
#
# Usage: loadtest-run-completeness.sh <loadtest-summary.json> [completeness.json]
set -euo pipefail

DEFAULT_EXPECTED="api-baseline concurrent-agents relay-throughput quic-agents"

# maxErrorRate is the ceiling past which a scenario's numbers describe the error
# path rather than the system. It is the figure the harness classifies its own
# runs against (server/tests/loadtest/validity.go), held equal here because a run
# has one verdict however many places compute it.
MAX_ERROR_RATE="${LOADTEST_MAX_ERROR_RATE:-0.25}"

# Where the harness's evidence bundle is collected to. It carries the run's own
# verdict about the target it ran against — whether the process was replaced
# underneath it, and whether it gave back what it took — read from the target's
# process families rather than from any count the target maintains about itself.
BUNDLE="${LOADTEST_BUNDLE:-loadtest-bundle/quic-agents.json}"

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

# target_verdict is the harness's own result about its target, or the empty
# string when no bundle was written.
#
# A bundle nobody wrote is silence rather than a pass: the harness may not have
# run at all, and a gate that answers yes when it could not ask is the false
# green this repository already rules against. The scenario checks above have
# their own reasons to fail a night, and they still apply.
target_verdict() {
  [ -s "$BUNDLE" ] || return 0
  jq -r '.verdict.result // empty' "$BUNDLE" 2>/dev/null || true
}

# target_findings is why, in the harness's own words, so the reason travels with
# the night rather than living only in a workflow log.
target_findings() {
  [ -s "$BUNDLE" ] || return 0
  jq -r '.verdict.reasons // [] | .[]' "$BUNDLE" 2>/dev/null || true
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
  local target_result findings
  expected="$(printf '%s\n' "${LOADTEST_EXPECTED_SCENARIOS:-$DEFAULT_EXPECTED}" | tr ' ' '\n' | sed '/^$/d' | sort -u)"
  produced="$(produced_scenarios "$summary")"
  missing="$(comm -23 <(printf '%s\n' "$expected") <(printf '%s\n' "$produced"))"
  unexpected="$(comm -13 <(printf '%s\n' "$expected") <(printf '%s\n' "$produced"))"
  unmeasured="$(unmeasured_scenarios "$summary")"
  breached="$(breached_thresholds)"

  target_result="$(target_verdict)"
  findings="$(target_findings)"

  # The target's own two outcomes fold in on the same doctrine the rest of this
  # file follows: a process that was replaced means the numbers describe two
  # systems, so the night is invalid; a target that kept what it took is a
  # finding about the system, so the night is failed and its rows still enter
  # the trend.
  result="valid"
  if [ -n "$missing" ] || [ -n "$unexpected" ] || [ -n "$unmeasured" ] || [ "$target_result" = "invalid" ]; then
    result="invalid"
  elif [ -n "$breached" ] || [ "$target_result" = "failed" ]; then
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
    --argjson target_findings "$(printf '%s\n' "$findings" | jq -Rn '[inputs | select(length > 0)]')" \
    '{
      result: $result,
      commit: $commit,
      timestamp: $timestamp,
      expected_scenarios: $expected,
      produced_scenarios: $produced,
      missing_scenarios: $missing,
      unexpected_scenarios: $unexpected,
      unmeasured_scenarios: $unmeasured,
      threshold_breaches: $breached,
      target_findings: $target_findings
    }' >"$out"

  cat "$out"

  if [ "$result" = "invalid" ]; then
    [ -z "$missing" ] || echo "::error::scenarios produced no rows: $(printf '%s' "$missing" | tr '\n' ' ')" >&2
    [ -z "$unexpected" ] || echo "::error::rows arrived from scenarios nobody asked for: $(printf '%s' "$unexpected" | tr '\n' ' ')" >&2
    [ -z "$unmeasured" ] || echo "::error::scenarios whose error rate is past ${MAX_ERROR_RATE}, so their rows describe the error path: $(printf '%s' "$unmeasured" | tr '\n' ' ')" >&2
    while IFS= read -r finding; do
      [ -z "$finding" ] || echo "::error::${finding}" >&2
    done <<<"$findings"
    echo "::error::this run is invalid and must not enter the trend." >&2
    return 3
  fi

  if [ "$result" = "failed" ]; then
    while IFS= read -r finding; do
      [ -z "$finding" ] || echo "::warning::${finding}" >&2
    done <<<"$findings"
  fi

  return 0
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
