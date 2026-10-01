#!/usr/bin/env bash
# The one shape every run's summary page takes: a table of Measurement,
# Expected, Actual and Result, and a legend under it saying in plain words what
# each column and each kind of reading means.
#
# Result is one of four words, and nothing else: pass — inside its limit; FAIL —
# past a limit that fails the run; over — past a limit that is watched and does
# not fail the run; — — nothing to judge it against. A reading the run could not
# take is "not read", never 0.
#
# Sourced by scripts/run-summary.sh, scripts/network-drill-regression-check.sh
# and scripts/benchmark-summarize.sh.

summary_table_header() {
  printf '| Measurement | Expected | Actual | Result |\n'
  printf '|---|---|---|---|\n'
}

# summary_table_row MEASUREMENT EXPECTED ACTUAL RESULT
summary_table_row() {
  printf '| %s | %s | %s | %s |\n' "$1" "$2" "$3" "$4"
}

# summary_legend prints the legend's lines for the three columns and for a
# reading that could not be taken.
summary_legend() {
  printf '\n'
  printf -- '- **Expected** — the limit this measurement is held to. "(reported only)" marks a limit that is watched and does not fail the run; "no limit" marks a measurement nothing holds.\n'
  printf -- '- **Actual** — what this run measured.\n'
  printf -- '- **Result** — pass: inside its limit. FAIL: past a limit that fails the run. over: past a limit that is only reported. —: nothing to judge it against.\n'
  printf -- '- **not read** — the run could not take this reading. It is never shown as 0.\n'
}

# summary_legend_terms TERM... prints a line for each further term a table
# uses: p50, p95, p99, error rate, Service Level.
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
