#!/usr/bin/env bash
# Tests for scripts/perf-regression-check.sh — the performance stack's and the
# endurance run's legs, against the nights before them.
#
# The legs were held to their profiles' fixed limits and to nothing else, so a
# leg that doubled its registration tail inside its limit said nothing. This
# holds each leg to the median of its own previous nights, as the load test is
# held — where the history says that comparison means something. A leg that
# holds its load moved at most 1.73 times its median from one night to the next
# over the bundles on record; a leg driven to or past what the venue holds swung
# by up to 1400 times on unchanged code. The first is failed past three times
# its median; the second is reported and left to its fixed limits.
#
# Run: ./scripts/tests/perf-regression-check.test.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
CHECK="$REPO_ROOT/scripts/perf-regression-check.sh"

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
assert_contains() {
  if grep -qF -- "$2" <<<"$3"; then pass "$1"; else fail "$1 (missing [$2] in [$3])"; fi
}
assert_lacks() {
  if grep -qF -- "$2" <<<"$3"; then fail "$1 (unexpected [$2])"; else pass "$1"; fi
}

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
mkdir -p "$WORK/bin" "$WORK/store"

# The stand-in store: every reading of the metric asked for, one JSON object per
# series, as the export API writes them.
cat >"$WORK/bin/kubectl" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$KUBECTL_ARGS"
metric="$(grep -oE 'match\[\]=[a-z_0-9]+' <<<"$*" | head -n 1)"
metric="${metric#match[]=}"
[ -f "$STORE/$metric.jsonl" ] && cat "$STORE/$metric.jsonl"
exit 0
STUB
chmod +x "$WORK/bin/kubectl"

TONIGHT="$(date -u -d '2026-09-29 13:42' +%s)"
MIDNIGHT="$(date -u -d '2026-09-29 00:00' +%s)"

# nights METRIC LEG PHASE WORKLOAD VALUE... — one series, one reading a night,
# the last value being last night's.
nights() {
  local metric="$1" leg="$2" phase="$3" workload="$4" n i=0 v stamps=() values=()
  shift 4
  n=$#
  for v in "$@"; do
    stamps+=("$(((MIDNIGHT - (n - i) * 86400 + 49000) * 1000))")
    values+=("$v")
    i=$((i + 1))
  done
  jq -nc --arg m "$metric" --arg leg "$leg" --arg phase "$phase" --arg workload "$workload" \
    --argjson t "[$(printf '%s,' "${stamps[@]}" | sed 's/,$//')]" \
    --argjson v "[$(printf '%s,' "${values[@]}" | sed 's/,$//')]" \
    '{metric: {__name__: $m, env: "ci", leg: $leg, phase: $phase, workload: $workload}, values: $v, timestamps: $t}' \
    >>"$WORK/store/$metric.jsonl"
}

# bundle LEG PROFILE VERDICT P95 P50 — tonight's evidence for one leg.
bundle() {
  local leg="$1" profile="$2" verdict="$3" p95="$4" p50="$5"
  mkdir -p "$WORK/legs/$leg"
  jq -n --arg profile "$profile" --arg verdict "$verdict" --argjson p95 "$p95" --argjson p50 "$p50" '{
    run: {commit: "abc123", profile_name: $profile, profile_version: 1, environment: "runner",
          finished_at: "2026-09-29T14:00:00Z"},
    verdict: {result: $verdict},
    observations: [
      {series: "aggregate_error_rate", value: 0},
      {series: "connect_p95_ms", value: 2},
      {series: "register_p95_ms", value: $p95},
      {series: "register_p50_ms", value: $p50}
    ]}' >"$WORK/legs/$leg/bundle.json"
}

run_check() {
  : >"$WORK/args"
  PATH="$WORK/bin:$PATH" KUBECTL_ARGS="$WORK/args" STORE="$WORK/store" \
    VM_RUN_STARTED_AT="${STARTED_OVERRIDE-$TONIGHT}" "$CHECK" "$@"
}

echo "perf-regression-check:"

# The history the store is back-filled with: the volume-500 leg's own
# registration tail over nine nights on record, and the spike leg's middle case,
# which is either a write or a queue depending on whether the night saturated.
nights perf_latency_p95_ms volume-500 register volume-500/1 4.99 4.89 5.01 4.93 5.2 4.97 8.62 5.04 4.98
nights perf_latency_p50_ms spike register spike/1 5.32 3.01 4.8 6143 5.1 3.3 4.2 5 5.4

# A night inside what the back-filled history holds passes.
bundle volume-500 volume-500 valid 6.1 3.1
rc=0
out="$(run_check "$WORK/legs/volume-500/bundle.json" 2>&1)" || rc=$?
assert_eq "a night inside its leg's history passes" "0" "$rc"
assert_lacks "and raises nothing" "REGRESSION_ALERT:" "$out"

# A leg that holds its load, past three times the median of its nights.
bundle volume-500 volume-500 valid 20 3.1
rc=0
out="$(run_check "$WORK/legs/volume-500/bundle.json" 2>&1)" || rc=$?
assert_eq "a leg that holds its load fails past three times its nights' median" "1" "$rc"
assert_contains "and names the leg, the measurement and the median" "volume-500 register latency_p95_ms: 4.99 -> 20" "$out"
if grep -qF 'commit' "$WORK/args"; then
  fail "the window is nights by date and asks nothing about commits"
else
  pass "the window is nights by date and asks nothing about commits"
fi

# A leg driven to or past what the venue holds swings by three orders of
# magnitude on unchanged code, so the same comparison is reported and decides
# nothing; the leg's fixed limits still do.
bundle spike spike valid 9650 6140
rc=0
out="$(run_check "$WORK/legs/spike/bundle.json" 2>&1)" || rc=$?
assert_eq "a saturated leg past its median is reported rather than failed" "0" "$rc"
assert_contains "and the report names it" "REPORTED:spike register latency_p50_ms: 5 -> 6140" "$out"

# Fewer than three nights is no window at all.
bundle volume-2000 volume-2000 valid 900 3.1
rc=0
out="$(run_check "$WORK/legs/volume-2000/bundle.json" 2>&1)" || rc=$?
assert_eq "a leg with no window is judged by its fixed limits alone" "0" "$rc"

# A leg that did not measure the system is judged by nothing here.
bundle scaling-1 scaling invalid 900 3.1
nights perf_latency_p95_ms scaling-1 register scaling/1 7.6 7.8 7.5 7.7
rc=0
out="$(run_check "$WORK/legs/scaling-1/bundle.json" 2>&1)" || rc=$?
assert_eq "an invalid leg is not compared" "0" "$rc"

# Without tonight's date the window would read tonight into itself.
rc=0
STARTED_OVERRIDE="" run_check "$WORK/legs/volume-500/bundle.json" >/dev/null 2>&1 || rc=$?
assert_eq "a check that does not know tonight's date refuses" "2" "$rc"

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
