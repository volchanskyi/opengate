#!/usr/bin/env bash
# Tests scripts/e2e-summarize.sh over recorded Playwright JSON reports and edits of them.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
SUMMARIZE="$REPO_ROOT/scripts/e2e-summarize.sh"
SPEED="$SCRIPT_DIR/fixtures/e2e-performance-report.json"
CROSS="$SCRIPT_DIR/fixtures/e2e-cross-browser-report.json"
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
  if [ "$2" = "$3" ]; then pass "$1"; else fail "$1 (want=[$2] got=[$3])"; fi
}
contains() {
  if grep -qF -- "$2" <<<"$3"; then pass "$1"; else fail "$1 (missing [$2] in [$3])"; fi
}
lacks() {
  if grep -qF -- "$2" <<<"$3"; then fail "$1 (found [$2] in [$3])"; else pass "$1"; fi
}

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

SPEED_PURPOSE="Checks typing and downloads over a slowed office line. Checks the page's memory during a long download. Each result is held to the product's promised limit."
CROSS_PURPOSE='Runs the browser suite in Chromium, Firefox and WebKit, each also from behind a home router, and retries a failed test once.'
STAGING_PURPOSE='Runs the browser suite against the staging release before production is promoted, and retries a failed test once.'
SECOND_PARAGRAPH=3
MEMORY="the page's memory stays flat through a 512 MiB download"
FROZEN='a tab frozen for 30 s mid-download carries on without loss'
ECHO='typing echoes within 100 ms at 100 Mbit/s over Direct, and the download keeps 80% of the line'
DIRECT='the session moves to Direct within a second of the relay opening, through the router'
LOGIN='auth.spec.ts › Auth flows › login with valid credentials redirects to /devices'

summary() {
  rc=0
  out="$("$SUMMARIZE" "$2" "$3")" || rc=$?
  assert_eq "the summary exits 0 ($1)" 0 "$rc"
  local purpose="$CROSS_PURPOSE"
  case "$2" in
    performance) purpose="$SPEED_PURPOSE" ;;
    staging) purpose="$STAGING_PURPOSE" ;;
  esac
  assert_eq "the purpose comes first ($1)" "$purpose" "$(sed -n "${SECOND_PARAGRAPH}p" <<<"$out")"
}

echo "▶ a speed night with a missed limit and a timeout"
summary "speed, mixed" performance "$SPEED"
contains "the speed summary is titled" '## E2E Performance' "$out"
contains "the failures fail the night, each named" \
  "**Result:** FAIL: 2 of 4 speed specs failed: $FROZEN; $ECHO." "$out"
contains "the table explains its headings" '<span title=' "$out"
contains "a held limit reads pass beside its reading" "| $ECHO — echo, 95th percentile | ≤ 100 ms | 95 ms | pass |" "$out"
contains "a missed limit reads FAIL beside its reading" \
  "| $ECHO — download, share of the line | ≥ 80% | 78.3% (78.3 Mbit/s) | FAIL |" "$out"
contains "every reading of a spec has its row" \
  "| $ECHO — download, time the rate is read over | ≥ 30 s | 70.2 s | pass |" "$out"
contains "a failure no reading explains has a row of its own" \
  "| $FROZEN | passes | timed out: Test timeout of 150000ms exceeded. | FAIL |" "$out"
lacks "a terminal colour never reaches the page" $'\e[' "$out"
contains "a bar inside a cell is escaped" \
  "| $DIRECT — time to Direct \\| after the relay opens | < 1000 ms | 412 ms | pass |" "$out"
lacks "a passing spec whose readings speak needs no outcome row" "| $MEMORY | passes |" "$out"
lacks "a spec whose missed limit explains the failure needs no outcome row" "| $ECHO | passes |" "$out"

echo "▶ a clean speed night"
jq '(.. | objects | select(has("results")) | .status) = "expected"
  | (.. | objects | select(has("annotations")) | .status) = "passed"
  | (.. | objects | select(has("annotations")) | .annotations) |= map(.description |= (fromjson | .held = true | tojson))
  | del(.. | objects | .error?)' "$SPEED" >"$WORK/clean.json"
summary "speed, clean" performance "$WORK/clean.json"
contains "a clean night passes" '**Result:** pass: all 4 speed specs held their limits.' "$out"
contains "a spec with no reading still shows it passed" "| $FROZEN | passes | passed | pass |" "$out"
lacks "a clean night shows no FAIL" '| FAIL |' "$out"

echo "▶ a cross-browser night with a failure, a retry and a skip"
summary "cross, mixed" cross-browser "$CROSS"
contains "the cross-browser summary is titled" '## E2E Cross-Browser' "$out"
contains "a failure fails the night, and a retry is counted" \
  '**Result:** FAIL: 1 of 6 tests failed. 1 passed only on retry.' "$out"
contains "the counts have their headings" '| Browser | Passed | Failed | Passed on retry | Skipped |' "$out"
contains "each browser has its row" '| chromium | 1 | 0 | 0 | 0 |' "$out"
contains "a retried test counts apart from a passed one" '| firefox | 0 | 0 | 1 | 0 |' "$out"
contains "a failed test counts against its browser" '| webkit | 0 | 1 | 0 | 0 |' "$out"
contains "a skipped test counts against its browser" '| webkit-behind-home-router | 0 | 0 | 0 | 1 |' "$out"
contains "the failure is named with its browser, path and error" \
  "- webkit › $LOGIN: Error: expect(locator).toBeVisible() | timed out" "$out"
contains "the retry is named with the error it first ended on" \
  "- firefox › $LOGIN: Error: page.goto: NS_ERROR_NET_RESET" "$out"
lacks "a terminal colour never reaches the list" $'\e[' "$out"

echo "▶ a clean cross-browser night"
jq '(.. | objects | select(has("results")) | .status) = "expected"
  | (.. | objects | select(has("duration")) | .status) = "passed"
  | del(.. | objects | .error?)' "$CROSS" >"$WORK/cross-clean.json"
summary "cross, clean" cross-browser "$WORK/cross-clean.json"
contains "a clean night passes" '**Result:** pass: none of the 6 tests failed.' "$out"
lacks "a clean night names no failure" '### Failed' "$out"
lacks "a clean night names no retry" '### Passed only on retry' "$out"

echo "▶ a staging run with a failure, a retry and a skip"
summary "staging, mixed" staging "$CROSS"
contains "the staging summary is titled" '## Staging browser tests' "$out"
contains "a failure fails the staging run, and a retry is counted" \
  '**Result:** FAIL: 1 of 6 tests failed. 1 passed only on retry.' "$out"
contains "each staging browser has its row" '| webkit | 0 | 1 | 0 | 0 |' "$out"
contains "the staging failure is named with its browser, path and error" \
  "- webkit › $LOGIN: Error: expect(locator).toBeVisible() | timed out" "$out"

echo "▶ a run that produced nothing"
for run in performance cross-browser staging; do
  summary "$run, no report" "$run" "$WORK/absent.json"
  contains "a missing report is nothing to read ($run)" \
    '**Result:** the run produced nothing to read: Playwright wrote no report.' "$out"
done
printf 'not json\n' >"$WORK/garbled.json"
summary "garbled" performance "$WORK/garbled.json"
contains "a report that does not parse is nothing to read" \
  '**Result:** the run produced nothing to read: the report does not parse.' "$out"
jq '.suites = []' "$SPEED" >"$WORK/empty.json"
summary "empty" performance "$WORK/empty.json"
contains "a report with no test is nothing to read" \
  '**Result:** the run produced nothing to read: the report holds no test.' "$out"

echo "▶ usage"
for args in "" "performance" "nightly $SPEED"; do
  rc=0
  read -ra argv <<<"$args"
  "$SUMMARIZE" "${argv[@]}" >/dev/null 2>&1 || rc=$?
  assert_eq "arguments [$args] are a usage error" 2 "$rc"
done

echo
echo "e2e-summarize: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  for f in "${FAILURES[@]}"; do printf '  - %s\n' "$f" >&2; done
  exit 1
fi
