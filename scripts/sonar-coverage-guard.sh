#!/usr/bin/env bash
# sonar-coverage-guard.sh — local guardrail against the SonarCloud "new_coverage
# sits at the 80.0 boundary" failure.
#
# The quality gate fails new_coverage when it is `LT 80`. A value like
# 79.95% *displays* as "80.0" but fails the gate, and because new-code coverage
# carries sub-line nondeterminism (race/atomic goroutine lines) and shifts with
# the new-code baseline, a run that clears 80 locally can land at 79.95 in CI —
# green locally, red in CI (observed CI run 26929821908: new_coverage 79.95%).
#
# This guard runs in the gauntlet AFTER `make sonar` has uploaded fresh coverage
# and the gate has been evaluated. It queries the exact (unrounded) new_coverage
# and fails unless it clears a buffer ABOVE the 80 gate floor, so a borderline
# local pass can never become a CI failure. The gate stays at 80; this only
# keeps the local result off the cliff edge.
#
# Env:
#   SONAR_TOKEN            required (same token the scan uses).
#   SONAR_PROJECT          default volchanskyi_opengate
#   SONAR_BRANCH           default dev
#   SONAR_API              default https://sonarcloud.io
#   NEW_COVERAGE_FLOOR     local floor, default 82 (= the 80 gate + 2pt buffer).
#   NEW_COVERAGE_OVERRIDE  test seam: use this value instead of querying the API.
#   CURL_BIN               curl binary (stubbed in tests).
#
# Exit codes: 0 = new_coverage clears the floor, or there is no new code to
#                 cover (metric absent);
#             1 = new_coverage is below the floor;
#             2 = prerequisite missing (no SONAR_TOKEN and no override).
set -uo pipefail

SONAR_PROJECT="${SONAR_PROJECT:-volchanskyi_opengate}"
SONAR_BRANCH="${SONAR_BRANCH:-dev}"
SONAR_API="${SONAR_API:-https://sonarcloud.io}"
NEW_COVERAGE_FLOOR="${NEW_COVERAGE_FLOOR:-82}"
CURL_BIN="${CURL_BIN:-curl}"

# scov_fetch — print the raw new_coverage value (empty when the metric is absent,
# i.e. the analysis introduced no new lines to cover).
scov_fetch() {
  if [ -n "${NEW_COVERAGE_OVERRIDE:-}" ]; then
    printf '%s' "$NEW_COVERAGE_OVERRIDE"
    return 0
  fi
  "$CURL_BIN" -s -u "$SONAR_TOKEN:" \
    "$SONAR_API/api/measures/component?component=$SONAR_PROJECT&branch=$SONAR_BRANCH&metricKeys=new_coverage" \
    | jq -r '.component.measures[]? | select(.metric=="new_coverage") | (.periods[0].value // .period.value) // empty' 2>/dev/null
}

# scov_below_floor <value> <floor> — exit 0 (true) when value < floor. Float-safe
# via awk so "79.95" < "82" compares numerically, not lexically.
scov_below_floor() {
  awk -v v="$1" -v f="$2" 'BEGIN { exit !((v + 0) < (f + 0)) }'
}

scov_main() {
  if [ -z "${NEW_COVERAGE_OVERRIDE:-}" ] && [ -z "${SONAR_TOKEN:-}" ]; then
    echo "✗ sonar-coverage-guard: SONAR_TOKEN unset (and no NEW_COVERAGE_OVERRIDE)." >&2
    return 2
  fi
  local cov; cov="$(scov_fetch)"
  if [ -z "$cov" ]; then
    echo "✓ sonar-coverage-guard: no new_coverage metric (no new lines to cover) — nothing to guard" >&2
    return 0
  fi
  if scov_below_floor "$cov" "$NEW_COVERAGE_FLOOR"; then
    {
      echo "✗ new_coverage ${cov}% is below the local floor ${NEW_COVERAGE_FLOOR}%."
      echo "  The SonarCloud gate fails new_coverage < 80; this buffer keeps the local"
      echo "  result off the 80.0 boundary so a borderline pass cannot flip to red in CI."
      echo "  Fix: add tests for new/changed lines until new_coverage ≥ ${NEW_COVERAGE_FLOOR}%."
      echo "  Inspect: https://sonarcloud.io/component_measures?id=${SONAR_PROJECT}&branch=${SONAR_BRANCH}&metric=new_coverage&view=list"
    } >&2
    return 1
  fi
  echo "✓ new_coverage ${cov}% ≥ local floor ${NEW_COVERAGE_FLOOR}% (gate floor 80 + buffer)" >&2
  return 0
}

# Run only when executed directly; sourcing exposes the functions for unit tests.
if [ "${BASH_SOURCE[0]}" = "${0}" ]; then
  scov_main
fi
