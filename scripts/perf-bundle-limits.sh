#!/usr/bin/env bash
# Read a profile's limits against a run's own evidence, where the run measured
# something.
#
# Every profile but the staging night's runs where that night's join does not
# happen, so the rows their limits are read against come out of the evidence bundle
# (scripts/loadtest-bundle-rows.sh) and go to the one evaluator every limit is
# read by (scripts/loadtest-gate-check.sh). This is the two of them in one place,
# because four workflow steps were calling them as a pair and a pair spelled out
# four times is four places to change.
#
# The bundle also carries the run's verdict about itself, and that is what
# decides whether these numbers mean anything. A run classified invalid did not
# measure the system, which the run says in its own words two steps earlier, so
# every figure beside that verdict is a reading of something else. A leg whose
# fleet count had been refused reported a registration tail of 4.6 seconds
# against a limit of 500 ms as its headline error; the night before, on the same
# profile, the same leg read 396 ms.
#
# So an invalid run's numbers are printed and decide nothing — the job is red on
# the verdict's account, and a reader who can see the figures should say what
# they were. A run that did measure the system is judged exactly as before.
#
# A bundle it cannot read, or one carrying no verdict, fails loudly: whether the
# run measured anything is then unknown, and a reader that answers yes when it
# cannot ask is the false green this repository rules against.
#
# Exits: 0 nothing blocking to report, 1 a run that measured the system crossed a
# limit it is held to, 2 or more the question could not be asked.
#
# Usage: perf-bundle-limits.sh <profile.yaml> <bundle.json> [rows.json]
set -euo pipefail

usage() {
  echo "usage: $0 <profile.yaml> <bundle.json> [rows.json]" >&2
}

main() {
  if [ "$#" -lt 2 ] || [ "$#" -gt 3 ]; then
    usage
    return 2
  fi

  local profile="$1" bundle="$2"
  local rows="${3:-/tmp/perf-bundle-rows.json}"

  if [ ! -s "$bundle" ]; then
    echo "::error::no evidence bundle at $bundle, so whether this run measured anything is unknown." >&2
    return 2
  fi

  local verdict
  verdict="$(jq -r '.verdict.result // empty' "$bundle" 2>/dev/null || true)"
  if [ -z "$verdict" ]; then
    echo "::error::the bundle at $bundle carries no verdict, so whether this run measured anything is unknown." >&2
    return 2
  fi

  local here
  here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

  "$here/loadtest-bundle-rows.sh" "$bundle" "$rows"

  # Taken on the command's own line, so errexit tests it rather than ending the
  # script at it: a status read after an unguarded command is a branch nothing
  # reaches.
  local status=0
  "$here/loadtest-gate-check.sh" "$profile" "$rows" || status=$?

  # The question could not be asked at all, which is a defect in the setup
  # rather than a verdict about the system, and it says so on its own account.
  if [ "$status" -ge 2 ]; then
    return "$status"
  fi

  if [ "$verdict" = "invalid" ]; then
    if [ "$status" -ne 0 ]; then
      echo "::warning::the limits above were read against a run that did not measure the system, so they decide nothing; the run's own verdict is what this leg is red for." >&2
    fi
    return 0
  fi

  return "$status"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
