#!/usr/bin/env bash
# Tests the nightly summarizer against fixture JSON that carries a leading banner.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SUMMARIZE="$SCRIPT_DIR/../pmat-summarize.sh"
[ -x "$SUMMARIZE" ] || {
  echo "FAIL: $SUMMARIZE not executable" >&2
  exit 1
}

PASS=0
FAIL=0
FAILURES=()
pass() {
  PASS=$((PASS + 1))
  printf '  ok   %s\n' "$1"
}
fail() {
  FAIL=$((FAIL + 1))
  FAILURES+=("$1")
  printf '  FAIL %s\n' "$1" >&2
}
assert_eq() {
  local n="$1" w="$2" g="$3"
  if [ "$w" = "$g" ]; then pass "$n"; else fail "$n (want=[$w] got=[$g])"; fi
}

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

cat >"$WORK/repo-score.json" <<'JSON'
{
  "total_score": 64.5,
  "grade": "C",
  "categories": {
    "documentation": { "percentage": 66.7 },
    "continuous_integration": { "percentage": 100.0 }
  }
}
JSON

# The fixture has an ANSI banner, then the F-cap object (1 violation) and the min-grade object
# (3 violations); slice_json picks the last, so below_bplus is 3.
{
  printf '\033[1m\033[4m🔍 Checking quality thresholds...\033[0m\n'
  printf '\033[36m✓ Baseline saved to: /tmp/pmat-quality-check.json\033[0m\n'
  cat <<'JSON'
{
  "passed": false,
  "gate_name": "FGradeCapGate",
  "violations": [ { "path": "build.rs", "new_grade": "F" } ],
  "message": "1 F-grade file(s) detected (max allowed: 0)"
}
{
  "passed": false,
  "gate_name": "MinimumGradeGate",
  "violations": [
    { "path": "a.go", "new_grade": "C" },
    { "path": "b.go", "new_grade": "B-" },
    { "path": "c.rs", "new_grade": "F" }
  ],
  "message": "3 file(s) below minimum grade threshold"
}
JSON
} >"$WORK/tdg-check.json"

run() {
  REPO_SCORE_JSON="$WORK/repo-score.json" TDG_CHECK_JSON="$WORK/tdg-check.json" \
    GITHUB_SHA="deadbeef" "$@" "$SUMMARIZE"
}

echo "canonical row + first-run (no PREV_*):"
OUT="$(run env)"
RC=$?
ROW="$(printf '%s\n' "$OUT" | grep -E '^\{' | tail -n1)"
assert_eq "first run exits 0 (no prev → no alert)" "0" "$RC"
assert_eq "repo_score parsed" "64.5" "$(jq -r '.repo_score' <<<"$ROW")"
assert_eq "repo_grade parsed" "C" "$(jq -r '.repo_grade' <<<"$ROW")"
assert_eq "below_bplus = min-grade gate (3), not F-cap (1)" "3" "$(jq -r '.below_bplus' <<<"$ROW")"
assert_eq "commit tagged" "deadbeef" "$(jq -r '.commit' <<<"$ROW")"
assert_eq "category percentage flattened" "66.7" "$(jq -r '.categories.documentation' <<<"$ROW")"

echo
echo "regression: repo-score drop:"
if ! run env PREV_REPO_SCORE=70.0 PREV_BELOW_BPLUS=3 >/dev/null 2>&1; then pass "5.5-pt drop (≥3) flags regression"; else fail "5.5-pt drop should flag"; fi
if run env PREV_REPO_SCORE=66.0 PREV_BELOW_BPLUS=3 >/dev/null 2>&1; then pass "1.5-pt drop (<3) is clean"; else fail "1.5-pt drop should be clean"; fi
if run env PREV_REPO_SCORE=64.5 PREV_BELOW_BPLUS=3 >/dev/null 2>&1; then pass "flat score is clean"; else fail "flat score should be clean"; fi

echo
echo "regression: below-B+ count rise:"
if ! run env PREV_REPO_SCORE=64.5 PREV_BELOW_BPLUS=2 >/dev/null 2>&1; then pass "below-B+ 2 → 3 flags regression"; else fail "below-B+ rise should flag"; fi
if run env PREV_REPO_SCORE=64.5 PREV_BELOW_BPLUS=10 >/dev/null 2>&1; then pass "below-B+ 10 → 3 (improvement) is clean"; else fail "improvement should be clean"; fi

echo
echo "alert text + missing input:"
ALERT="$(run env PREV_REPO_SCORE=70.0 PREV_BELOW_BPLUS=3 2>&1 | grep -c '^REGRESSION_ALERT:')"
if [ "$ALERT" -gt 0 ]; then pass "emits REGRESSION_ALERT lines"; else fail "should emit REGRESSION_ALERT lines"; fi
rc=0
REPO_SCORE_JSON="$WORK/nope.json" TDG_CHECK_JSON="$WORK/tdg-check.json" "$SUMMARIZE" >/dev/null 2>&1 || rc=$?
if [ "$rc" -eq 2 ]; then pass "missing input → exit 2"; else fail "missing input expected exit 2, got $rc"; fi

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
