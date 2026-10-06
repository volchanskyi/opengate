#!/usr/bin/env bash
# Classifies the night as valid, failed or invalid and records which scenarios produced rows.
# Invalid keeps a partial night out of the trend, whose window median it would otherwise lower.
#
# Environment:
#   LOADTEST_EXPECTED_SCENARIOS  space-separated scenario names the run owed
#   LOADTEST_K6_SUMMARY_DIR      where the k6 exports and threshold breach records were written
#   LOADTEST_BUNDLE              the QUIC harness's evidence bundle
#   LOADTEST_GATE_BREACHES       the profile limits and marks the night crossed
#
# Exit codes:
#   0  the night held
#   2  the night could not be asked
#   3  the night did not measure the system; its rows stay out of the trend
#   4  the system crossed a limit; the night is red and its rows enter the trend
#
# Usage: loadtest-run-completeness.sh <loadtest-summary.json> [completeness.json]
set -euo pipefail

DEFAULT_EXPECTED="api-baseline concurrent-agents relay-throughput quic-agents"

# The error-rate ceiling past which a scenario's numbers describe the error path; it equals the
# harness's own figure in server/tests/loadtest/validity.go.
MAX_ERROR_RATE="${LOADTEST_MAX_ERROR_RATE:-0.25}"

# The bundle carries the harness's verdict on whether the target process was replaced and whether
# it gave back what it took.
BUNDLE="${LOADTEST_BUNDLE:-loadtest-bundle/quic-agents.json}"

# The profile limits read against tonight's rows; a breach fails the night while its rows still
# enter the trend.
GATE_BREACHES="${LOADTEST_GATE_BREACHES:-loadtest-gate-breaches.json}"

usage() {
  echo "usage: $0 <loadtest-summary.json> [completeness.json]" >&2
}

produced_scenarios() {
  jq -r '[.[].scenario] | unique | .[]' "$1"
}

# unmeasured_scenarios lists scenarios whose error rate passes the ceiling; their rows carry
# zeroes that would pull the window median down.
unmeasured_scenarios() {
  jq -r --argjson ceiling "$MAX_ERROR_RATE" '
    [ .[] | select(.error_rate != null and .error_rate > $ceiling) | .scenario ]
    | unique | .[]
  ' "$1"
}

# breached_thresholds lists scenarios with a breach file, which the runner writes because an exit
# code cannot carry whether a mark is blocking.
breached_thresholds() {
  local dir="${LOADTEST_K6_SUMMARY_DIR:-loadtest-k6}"
  [ -d "$dir" ] || return 0
  find "$dir" -maxdepth 1 -type f -name '*.thresholds' -printf '%f\n' 2>/dev/null \
    | sed 's/\.thresholds$//' \
    | sort
}

# target_verdict is the harness's result about its target; a missing bundle yields the empty
# string, never a pass.
target_verdict() {
  [ -s "$BUNDLE" ] || return 0
  jq -r '.verdict.result // empty' "$BUNDLE" 2>/dev/null || true
}

# gate_breaches lists the enforced limits the night crossed; a missing file yields nothing.
gate_breaches() {
  [ -s "$GATE_BREACHES" ] || return 0
  jq -r '.[]? | select(.enforced == true) | .message' "$GATE_BREACHES" 2>/dev/null || true
}

# reported_marks are the marks the profile watches without enforcing; they print as notices only.
reported_marks() {
  [ -s "$GATE_BREACHES" ] || return 0
  jq -r '.[]? | select(.enforced == false) | .message' "$GATE_BREACHES" 2>/dev/null || true
}

# target_findings lists the reasons for the verdict in the harness's own words.
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
  local target_result findings gates reported
  expected="$(printf '%s\n' "${LOADTEST_EXPECTED_SCENARIOS:-$DEFAULT_EXPECTED}" | tr ' ' '\n' | sed '/^$/d' | sort -u)"
  produced="$(produced_scenarios "$summary")"
  missing="$(comm -23 <(printf '%s\n' "$expected") <(printf '%s\n' "$produced"))"
  unexpected="$(comm -13 <(printf '%s\n' "$expected") <(printf '%s\n' "$produced"))"
  unmeasured="$(unmeasured_scenarios "$summary")"
  breached="$(breached_thresholds)"

  target_result="$(target_verdict)"
  findings="$(target_findings)"
  gates="$(gate_breaches)"
  reported="$(reported_marks)"

  # A replaced target process makes the numbers describe two systems, so the night is invalid; a
  # target that kept what it took is a finding about the system, so the night is failed.
  result="valid"
  if [ -n "$missing" ] || [ -n "$unexpected" ] || [ -n "$unmeasured" ] || [ "$target_result" = "invalid" ]; then
    result="invalid"
  elif [ -n "$breached" ] || [ -n "$gates" ] || [ "$target_result" = "failed" ]; then
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
    --argjson gate_breaches "$(printf '%s\n' "$gates" | jq -Rn '[inputs | select(length > 0)]')" \
    --argjson reported_marks "$(printf '%s\n' "$reported" | jq -Rn '[inputs | select(length > 0)]')" \
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
      gate_breaches: $gate_breaches,
      reported_marks: $reported_marks,
      target_findings: $target_findings
    }' >"$out"

  cat "$out"

  while IFS= read -r mark; do
    [ -z "$mark" ] || echo "::notice::reported, not enforced: ${mark}"
  done <<<"$reported"

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

  # A night that crossed a limit measured the system, so its rows enter the trend and the night
  # goes red.
  if [ "$result" = "failed" ]; then
    while IFS= read -r finding; do
      [ -z "$finding" ] || echo "::error::${finding}" >&2
    done <<<"$findings"
    while IFS= read -r breach; do
      [ -z "$breach" ] || echo "::error::${breach}" >&2
    done <<<"$gates"
    [ -z "$breached" ] || echo "::error::marks breached: $(printf '%s' "$breached" | tr '\n' ' ')" >&2
    echo "::error::this night crossed a limit it is held to." >&2
    return 4
  fi

  return 0
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
