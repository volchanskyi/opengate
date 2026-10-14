#!/usr/bin/env bash
# The summary-page table sourced by scripts/run-summary.sh, scripts/benchmark-summarize.sh and
# scripts/network-drill-regression-check.sh; each heading and metric word explains itself on hover.

# What a heading or metric word means; fails for a word it does not know.
summary_term_text() {
  case "$1" in
    Measurement) printf '%s' 'What was measured: the source, the scenario and the phase, then the reading.' ;;
    Expected) printf '%s' 'The limit this measurement is held to. (reported only) marks a limit that is watched and does not fail the run; no limit marks a measurement nothing holds.' ;;
    Actual) printf '%s' 'What this run measured.' ;;
    Result) printf '%s' 'pass: inside its limit. FAIL: past a limit that fails the run. over: past a limit that is only reported. —: nothing to judge it against.' ;;
    p50) printf '%s' 'Half of the requests took this long or less: the ordinary case.' ;;
    p95) printf '%s' 'Nineteen in twenty took this long or less: the slow end a user notices.' ;;
    p99) printf '%s' 'Ninety-nine in a hundred took this long or less: the worst the run saw, short of outliers.' ;;
    "error rate") printf '%s' 'The share of attempts that failed, as a percentage.' ;;
    throughput) printf '%s' 'Requests completed each second over the phase.' ;;
    "Service Level") printf '%s' 'Avg CPU % and Avg Mem % are how much of its own processor or memory cap the server or the database used, averaged over the phase the profile marks as measured (every phase, where it marks none).' ;;
    "not read") printf '%s' 'The run could not take this reading. It is never shown as 0.' ;;
    ns/op) printf '%s' "The time one call of the benchmark took, held to the median of the nights before and to a ceiling over the baseline, because a runner's speed varies from night to night." ;;
    allocs/op) printf '%s' 'The allocations one call of the benchmark made, held to the committed baseline.' ;;
    B/op) printf '%s' 'The bytes one call of the benchmark allocated.' ;;
    *) return 1 ;;
  esac
}

# A word with its explanation as hover text.
summary_term() {
  local text
  text="$(summary_term_text "$1")" || return 1
  printf '<span title="%s">%s</span>' "$text" "$1"
}

# The words given, each mapped to its explanation, for a jq renderer to build the same hover text.
summary_terms_json() {
  local word json='{}'
  for word in "$@"; do
    json="$(jq -c --arg w "$word" --arg t "$(summary_term_text "$word")" '. + {($w): $t}' <<<"$json")"
  done
  printf '%s' "$json"
}

# The table's two header lines.
summary_table_header() {
  summary_table_header_explaining "$(summary_term_text Measurement)"
}

# The table's two header lines, its Measurement column explained in words of the table's own.
summary_table_header_explaining() {
  printf '| <span title="%s">Measurement</span> | %s | %s | %s |\n' "$1" "$(summary_term Expected)" \
    "$(summary_term Actual)" "$(summary_term Result)"
  printf '|---|---|---|---|\n'
}

summary_table_row() {
  printf '| %s | %s | %s | %s |\n' "$1" "$2" "$3" "$4"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  set -euo pipefail
  echo "summary-table.sh is a sourced library" >&2
  exit 2
fi
