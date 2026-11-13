#!/usr/bin/env bash
# Tests for scripts/loadtest-k6-run.sh — the k6 scenario runner that decides
# whether a run produced a measurement worth trending.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
RUNNER="$REPO_ROOT/scripts/loadtest-k6-run.sh"
[ -x "$RUNNER" ] || {
  echo "FAIL: $RUNNER not executable" >&2
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
  local name="$1" want="$2" got="$3"
  if [ "$want" = "$got" ]; then pass "$name"; else fail "$name (want=[$want] got=[$got])"; fi
}

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

make_fake_k6() {
  local exit_code="$1"
  cat >"$WORK/k6" <<EOF
#!/usr/bin/env bash
printf '%s\n' "\$@" >"$WORK/k6-args.txt"
for arg in "\$@"; do
  case "\$prev" in
    --summary-export) printf '{"metrics":{}}' >"\$arg" ;;
  esac
  prev="\$arg"
done
exit $exit_code
EOF
  chmod +x "$WORK/k6"
}

run_case() {
  local exit_code="$1"
  make_fake_k6 "$exit_code"
  rm -rf "$WORK/summaries"
  mkdir -p "$WORK/summaries"
  STATUS=0
  # Each case writes its own step summary file, so the CI job's summary stays untouched.
  GITHUB_STEP_SUMMARY="${CASE_SUMMARY:-$WORK/case-summary.md}" \
    K6_BIN="$WORK/k6" \
    LOADTEST_K6_SUMMARY_DIR="$WORK/summaries" \
    LOADTEST_BASE_URL="http://127.0.0.1:18080" \
    LOADTEST_RUN_ID="${LOADTEST_RUN_ID:-99-1}" \
    LOADTEST_PROFILE="${LOADTEST_PROFILE:-$REPO_ROOT/load/profiles/normal.yaml}" \
    K6_SUMMARY_TREND_STATS="avg,p(95)" \
    "$RUNNER" api-baseline load/k6/scenarios/api-baseline.js >"$WORK/out.txt" 2>&1 || STATUS=$?
}

echo "loadtest-k6-run:"

run_case 0
if grep -q 'LOADTEST_RUN_ID=99-1' "$WORK/k6-args.txt"; then
  pass "the run id is handed to k6 rather than assumed to be inherited"
else
  fail "the runner must pass LOADTEST_RUN_ID through to k6"
fi

run_case 0
assert_eq "clean run exits 0" "0" "$STATUS"
if [ -f "$WORK/summaries/api-baseline.json" ]; then
  pass "clean run keeps the summary export"
else
  fail "clean run keeps the summary export"
fi

# The profile alone holds the limits, so a failed mark means a script has grown one of its own,
# and the run is refused whole.
: >"$WORK/step-summary.md"
CASE_SUMMARY="$WORK/step-summary.md" run_case 99
assert_eq "a failed k6 mark is an error" "99" "$STATUS"
if [ -f "$WORK/summaries/api-baseline.json" ]; then
  fail "a failed k6 mark discards the summary export"
else
  pass "a failed k6 mark discards the summary export"
fi
if grep -q '::error::' "$WORK/out.txt" && grep -q 'declare none' "$WORK/out.txt"; then
  pass "and says the scripts declare no mark that can fail"
else
  fail "and says the scripts declare no mark that can fail (out=[$(cat "$WORK/out.txt")])"
fi
if [ -s "$WORK/step-summary.md" ]; then
  fail "the run's summary carries no line about k6's own marks (summary=[$(cat "$WORK/step-summary.md")])"
else
  pass "the run's summary carries no line about k6's own marks"
fi

run_case 107
assert_eq "script exception propagates 107" "107" "$STATUS"
if [ -f "$WORK/summaries/api-baseline.json" ]; then
  fail "script exception discards the summary export"
else
  pass "script exception discards the summary export"
fi
if grep -q "api-baseline" "$WORK/out.txt"; then
  pass "aborted run names the scenario it discarded"
else
  fail "aborted run names the scenario it discarded"
fi

# Every mark a scenario declares only names a sub-metric for the export, so none can fail.
failing_marks="$(grep -rnE '"(p\([0-9.]+\)|avg|med|min|max|rate|count)[[:space:]]*(<|<=|==|!=)' \
  "$REPO_ROOT/load/k6" || true)"
if [ -z "$failing_marks" ]; then
  pass "no k6 scenario declares a mark that can fail"
else
  fail "the k6 scripts hold limits that belong in the profile: $failing_marks"
fi
# k6 v1.6.1 leaves dropped_iterations out of the export when none dropped unless a mark names it,
# and the profile fails a night whose reading never arrived.
missing_dropped=""
mapfile -t arrival < <(grep -l 'arrivalScenarios(' "$REPO_ROOT"/load/k6/scenarios/*.js || true)
for scenario in "${arrival[@]}"; do
  grep -q 'dropped_iterations: exportOnly("counter")' "$scenario" \
    || missing_dropped="$missing_dropped $(basename "$scenario")"
done
if [ "${#arrival[@]}" -eq 0 ]; then
  fail "no arrival scenario was found to read"
elif [ -z "$missing_dropped" ]; then
  pass "every arrival scenario exports dropped_iterations even when none dropped"
else
  fail "these arrival scenarios must name dropped_iterations with exportOnly(\"counter\"):$missing_dropped"
fi
# The merge reads the measured phase's completed arrivals and their time from these two.
missing_timed=""
for scenario in "${arrival[@]}"; do
  grep -qF 'iterations: "counter"' "$scenario" && grep -qF 'iteration_duration: "trend"' "$scenario" \
    || missing_timed="$missing_timed $(basename "$scenario")"
done
if [ "${#arrival[@]}" -gt 0 ] && [ -z "$missing_timed" ]; then
  pass "every arrival scenario exports its measured phase's completed arrivals and their time"
else
  fail "these arrival scenarios must name iterations and iteration_duration in measuredExports:$missing_timed"
fi
if grep -q 'export function exportOnly' "$REPO_ROOT/load/k6/lib/profile.js"; then
  pass "the k6 library names the export-only mark the scenarios use"
else
  fail "load/k6/lib/profile.js must export exportOnly"
fi

cat >"$WORK/k6" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
chmod +x "$WORK/k6"
rm -rf "$WORK/summaries"
mkdir -p "$WORK/summaries"
STATUS=0
K6_BIN="$WORK/k6" \
  LOADTEST_K6_SUMMARY_DIR="$WORK/summaries" \
  LOADTEST_BASE_URL="http://127.0.0.1:18080" \
  K6_SUMMARY_TREND_STATS="avg,p(95)" \
  "$RUNNER" api-baseline load/k6/scenarios/api-baseline.js >"$WORK/out.txt" 2>&1 || STATUS=$?
if [ "$STATUS" -ne 0 ]; then
  pass "a clean run that wrote no export fails"
else
  fail "a clean run that wrote no export fails"
fi

WORKFLOW="$REPO_ROOT/.github/workflows/load-test.yml"
direct="$(grep -cE '^\s+/tmp/k6 run' "$WORKFLOW" || true)"
assert_eq "load-test.yml invokes k6 only through the runner" "0" "$direct"
wrapped="$(grep -cE 'scripts/loadtest-k6-run\.sh' "$WORKFLOW" || true)"
assert_eq "load-test.yml invokes the runner" "1" "$wrapped"

named="$(grep -oE 'for scenario in api-baseline concurrent-agents relay-throughput' "$WORKFLOW" || true)"
if [ -n "$named" ]; then
  pass "load-test.yml drives all three scenarios through the runner, together"
else
  fail "load-test.yml drives all three scenarios through the runner, together"
fi

# The scenarios load under stand-in k6 modules whose Trend records each name a scenario times, so
# the export marks are read off the scenarios as k6 builds them.
if ! command -v node >/dev/null 2>&1; then
  fail "node is on PATH, so the scenarios' export marks can be built rather than described"
fi
K6_STAND_IN="$WORK/k6-stand-in"
mkdir -p "$K6_STAND_IN/node_modules/k6"
cp -r "$REPO_ROOT/load/k6/lib" "$REPO_ROOT/load/k6/scenarios" "$K6_STAND_IN/"
printf '{"type":"module"}\n' >"$K6_STAND_IN/package.json"
cat >"$K6_STAND_IN/node_modules/k6/package.json" <<'EOF'
{"name": "k6", "type": "module",
 "exports": {".": "./index.js", "./metrics": "./metrics.js", "./http": "./http.js", "./ws": "./ws.js"}}
EOF
cat >"$K6_STAND_IN/node_modules/k6/index.js" <<'EOF'
export function check() { return true; }
export function fail(message) { throw new Error(message); }
export function sleep() {}
EOF
cat >"$K6_STAND_IN/node_modules/k6/metrics.js" <<'EOF'
export const trends = [];
export class Trend { constructor(name) { trends.push(name); } add() {} }
export class Counter { add() {} }
EOF
printf 'export default {};\n' >"$K6_STAND_IN/node_modules/k6/http.js"
printf 'export default {};\n' >"$K6_STAND_IN/node_modules/k6/ws.js"

# scenario_marks prints a scenario's timed trends and its threshold names, loaded over a walk.
scenario_marks() { # scenario walk
  (cd "$K6_STAND_IN" && LOADTEST_PHASES="$2" node --input-type=module -e "
    globalThis.__ENV = process.env;
    const { trends } = await import('k6/metrics');
    const { options } = await import('./scenarios/$1.js');
    process.stdout.write(JSON.stringify({ trends, thresholds: Object.keys(options.thresholds || {}) }));
  ")
}

STEADY_WALK='[{"name":"ramp","seconds":60,"arrivals_per_second":2,"sessions":1,"agents":10,"measured":false},
  {"name":"steady","seconds":120,"arrivals_per_second":2,"sessions":1,"agents":10,"measured":true}]'
timed=0
for scenario in "$K6_STAND_IN"/scenarios/*.js; do
  name="$(basename "$scenario" .js)"
  if ! marks="$(scenario_marks "$name" "$STEADY_WALK")"; then
    fail "$name loads under the stand-in k6"
    continue
  fi
  timed=$((timed + $(jq '.trends | length' <<<"$marks")))
  assert_eq "$name exports every time it takes over the measured phase" "" \
    "$(jq -r '(.trends - [.thresholds[] | select(endswith("{phase:steady}")) | rtrimstr("{phase:steady}")])
      | join(" ")' <<<"$marks")"
done
if [ "$timed" -gt 0 ]; then
  pass "the scenarios time something, so the check above read their marks"
else
  fail "no scenario declared a time it takes, so the check above read nothing"
fi

# Breakpoint's technicians are judged over its recovery: the walk its profile projects marks the
# phase, the scenarios export it, and the rows its limits read carry that phase alone.
# shellcheck source=scripts/lib/loadtest-profile.sh
. "$REPO_ROOT/scripts/lib/loadtest-profile.sh"
BREAKPOINT_WALK="$(profile_phases "$REPO_ROOT/load/profiles/breakpoint.yaml")"
assert_eq "breakpoint measures its recovery" "recovery" \
  "$(jq -r '[.[] | select(.measured) | .name] | join(" ")' <<<"$BREAKPOINT_WALK")"
jq -n '{schema_version: 11, verdict: {result: "valid"}, run: {commit: "deadbeef"},
  observations: [{series: "aggregate_error_rate", value: 0}, {series: "connect_p95_ms", value: 5}]}' \
  >"$WORK/breakpoint-bundle.json"
merged=()
for scenario in api-baseline relay-throughput; do
  marks="$(scenario_marks "$scenario" "$BREAKPOINT_WALK")"
  # Each time reads 9000 ms over the whole run and 40 ms over each phase the scenario exports.
  jq -n --argjson marks "$marks" '
    def stat($ms): {"p(50)": $ms, med: $ms, "p(95)": $ms, count: 100};
    {metrics: (
      ([$marks.trends[] | {key: ., value: stat(9000)}]
        + [$marks.thresholds[] | select((split("{")[0]) as $base | $marks.trends | index($base))
            | {key: ., value: stat(40)}])
      | from_entries
      + {http_reqs: {count: 200, rate: 1}, requests_refused: {count: 0, rate: 0}})}' \
    >"$WORK/breakpoint-$scenario.json"
  merged+=(--journeys "$WORK/breakpoint-$scenario.json")
done
"$REPO_ROOT/scripts/loadtest-bundle-merge.sh" "$WORK/breakpoint-bundle.json" "${merged[@]}" >/dev/null
assert_eq "breakpoint's technician rows read its recovery alone" \
  "k6/api-baseline/command_accept 40|k6/api-baseline/device_detail 40|k6/api-baseline/device_list 40|k6/relay-throughput/relay 40" \
  "$("$REPO_ROOT/scripts/loadtest-bundle-rows.sh" "$WORK/breakpoint-bundle.json" \
    | jq -r '[.[] | select(.source == "k6") | "\(.source)/\(.scenario)/\(.phase) \(.latency_p95_ms)"]
      | sort | join("|")')"

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
