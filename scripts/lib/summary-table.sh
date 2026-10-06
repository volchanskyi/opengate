#!/usr/bin/env bash
# The summary-page table and legend sourced by scripts/run-summary.sh and
# scripts/benchmark-summarize.sh; Result is pass, FAIL, over, a dash or "not read".

summary_table_header() {
  printf '| Measurement | Expected | Actual | Result |\n'
  printf '|---|---|---|---|\n'
}

summary_table_row() {
  printf '| %s | %s | %s | %s |\n' "$1" "$2" "$3" "$4"
}

summary_legend() {
  printf '\n'
  printf -- '- **Expected** — the limit this measurement is held to. "(reported only)" marks a limit that is watched and does not fail the run; "no limit" marks a measurement nothing holds.\n'
  printf -- '- **Actual** — what this run measured.\n'
  printf -- '- **Result** — pass: inside its limit. FAIL: past a limit that fails the run. over: past a limit that is only reported. —: nothing to judge it against.\n'
  printf -- '- **not read** — the run could not take this reading. It is never shown as 0.\n'
}

summary_legend_terms() {
  local term
  for term in "$@"; do
    case "$term" in
      p50) printf -- '- **p50** — half of the requests took this long or less: the ordinary case.\n' ;;
      p95) printf -- '- **p95** — nineteen in twenty took this long or less: the slow end a user notices.\n' ;;
      p99) printf -- '- **p99** — ninety-nine in a hundred took this long or less: the worst the run saw, short of outliers.\n' ;;
      "error rate") printf -- '- **error rate** — the share of attempts that failed, as a percentage.\n' ;;
      "Service Level") printf -- '- **Service Level** — Avg CPU %% and Avg Mem %% are how much of its own processor or memory cap the server or the database used, averaged over the phase the profile marks as measured (every phase, where it marks none).\n' ;;
    esac
  done
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  set -euo pipefail
  echo "summary-table.sh is a sourced library" >&2
  exit 2
fi
