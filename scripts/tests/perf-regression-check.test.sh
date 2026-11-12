#!/usr/bin/env bash
# Tests for scripts/perf-regression-check.sh, which holds each leg to its prior nights' median.
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
    --arg cpu "${PROCESSOR:-}" --arg scenario "${SCENARIO:-}" \
    '{metric: ({__name__: $m, env: "ci", leg: $leg, phase: $phase, workload: $workload}
       + (if $cpu == "" then {} else {processor: $cpu} end)
       + (if $scenario == "" then {} else {scenario: $scenario} end)), values: $v, timestamps: $t}' \
    >>"$WORK/store/$metric.jsonl"
}

# flagged_night METRIC LEG PHASE WORKLOAD DAYS_AGO VALUE pushes a reading as a flagged night.
flagged_night() {
  local metric="$1" leg="$2" phase="$3" workload="$4" ago="$5" value="$6"
  jq -nc --arg m "$metric" --arg leg "$leg" --arg phase "$phase" --arg workload "$workload" \
    --argjson t "[$(((MIDNIGHT - ago * 86400 + 49000) * 1000))]" --argjson v "[$value]" \
    '{metric: {__name__: $m, env: "ci", flagged: "1", leg: $leg, phase: $phase, workload: $workload},
      values: $v, timestamps: $t}' >>"$WORK/store/$metric.jsonl"
}

# bundle LEG PROFILE VERDICT P95 P50 — tonight's evidence for one leg.
bundle() {
  local leg="$1" profile="$2" verdict="$3" p95="$4" p50="$5"
  mkdir -p "$WORK/legs/$leg"
  jq -n --arg profile "$profile" --arg verdict "$verdict" --argjson p95 "$p95" --argjson p50 "$p50" '{
    run: {commit: "abc123", profile_name: $profile, profile_version: 1, profile_digest: "d1",
          environment: "runner", finished_at: "2026-09-29T14:00:00Z"},
    verdict: {result: $verdict},
    runner: {processor_model: env.PROCESSOR},
    observations: [
      {series: "aggregate_error_rate", value: 0},
      {series: "connect_p95_ms", value: 2},
      {series: "register_p95_ms", value: $p95},
      {series: "register_p50_ms", value: $p50}
    ]}' >"$WORK/legs/$leg/bundle.json"
}

run_check() {
  : >"$WORK/args"
  : >"$WORK/table.md"
  PATH="$WORK/bin:$PATH" KUBECTL_ARGS="$WORK/args" STORE="$WORK/store" PERF_FLAGGED_FILE="$WORK/flagged.tsv" \
    PERF_NIGHTS_TABLE="$WORK/table.md" \
    VM_RUN_STARTED_AT="${STARTED_OVERRIDE-$TONIGHT}" "$CHECK" "$@"
}

echo "perf-regression-check:"

nights perf_latency_p95_ms volume-500 register volume-500/1/d1 4.99 4.89 5.01 4.93 5.2 4.97 8.62 5.04 4.98
nights perf_latency_p50_ms spike register spike/1/d1 5.32 3.01 4.8 6143 5.1 3.3 4.2 5 5.4

bundle volume-500 volume-500 valid 6.1 3.1
rc=0
out="$(run_check "$WORK/legs/volume-500/bundle.json" 2>&1)" || rc=$?
assert_eq "a night inside its leg's history passes" "0" "$rc"
assert_lacks "and raises nothing" "REGRESSION_ALERT:" "$out"

bundle volume-500 volume-500 valid 20 3.1
rc=0
out="$(run_check "$WORK/legs/volume-500/bundle.json" 2>&1)" || rc=$?
assert_eq "a held leg's first night past its line is flagged, not failed" "0" "$rc"
assert_contains "and the flag names the leg, the measurement and the median" \
  "FLAGGED:volume-500 register latency_p95_ms: 4.99 -> 20" "$out"
assert_eq "the flagged series is written for the push" "volume-500	register	latency_p95_ms" \
  "$(cat "$WORK/flagged.tsv")"

flagged_night perf_latency_p95_ms volume-500 register volume-500/1/d1 2 19
rc=0
out="$(run_check "$WORK/legs/volume-500/bundle.json" 2>&1)" || rc=$?
assert_eq "a held leg past its line on two of its last three nights fails" "1" "$rc"
assert_contains "and names the leg, the measurement and the median" "volume-500 register latency_p95_ms: 4.99 -> 20" "$out"
if grep -qF 'commit' "$WORK/args"; then
  fail "the window is nights by date and asks nothing about commits"
else
  pass "the window is nights by date and asks nothing about commits"
fi

bundle spike spike valid 9650 6140
rc=0
out="$(run_check "$WORK/legs/spike/bundle.json" 2>&1)" || rc=$?
assert_eq "a saturated leg past its median is reported rather than failed" "0" "$rc"
assert_contains "and the report names it" "REPORTED:spike register latency_p50_ms: 5 -> 6140" "$out"

# Four flagged nights at 60 ms predate three ordinary nights at 5 ms; the window reads those three.
nights perf_latency_p95_ms volume-200 register volume-200/1/d1 5 5 5
for ago in 7 6 5 4; do flagged_night perf_latency_p95_ms volume-200 register volume-200/1/d1 "$ago" 60; done
bundle volume-200 volume-200 valid 20 3.1
out="$(run_check "$WORK/legs/volume-200/bundle.json" 2>&1)" || true
assert_contains "a flagged night stays out of the window" "volume-200 register latency_p95_ms: 5 -> 20" "$out"

# A profile edited without a change of load keeps its nights; a new load version starts its own.
bundle volume-500 volume-500 valid 20 3.1
jq '.run.profile_digest = "d2"' "$WORK/legs/volume-500/bundle.json" >"$WORK/edited.json"
mv "$WORK/edited.json" "$WORK/legs/volume-500/bundle.json"
out="$(run_check "$WORK/legs/volume-500/bundle.json" 2>&1)" || true
assert_contains "a profile whose limits changed is still compared with its nights" \
  "volume-500 register latency_p95_ms: 4.99 -> 20" "$out"
jq '.run.profile_version = 2' "$WORK/legs/volume-500/bundle.json" >"$WORK/edited.json"
mv "$WORK/edited.json" "$WORK/legs/volume-500/bundle.json"
rc=0
out="$(run_check "$WORK/legs/volume-500/bundle.json" 2>&1)" || rc=$?
assert_eq "a new load version is compared with no night of the old one" "0" "$rc"
assert_lacks "and flags nothing" "FLAGGED:" "$out"

bundle volume-2000 volume-2000 valid 900 3.1
rc=0
out="$(run_check "$WORK/legs/volume-2000/bundle.json" 2>&1)" || rc=$?
assert_eq "a leg with no window is judged by its fixed limits alone" "0" "$rc"

bundle scaling-1 scaling invalid 900 3.1
nights perf_latency_p95_ms scaling-1 register scaling/1/d1 7.6 7.8 7.5 7.7
rc=0
out="$(run_check "$WORK/legs/scaling-1/bundle.json" 2>&1)" || rc=$?
assert_eq "an invalid leg is not compared" "0" "$rc"

# A night is compared only with nights on the same processor: the busy morning reads hundreds of
# milliseconds on every 7763 night and a few on every other chip.
PROCESSOR="AMD EPYC 7763 64-Core Processor" nights perf_latency_p50_ms peak register peak/1/d1 350 337 362 375
PROCESSOR="AMD EPYC 9V74 80-Core Processor" nights perf_latency_p50_ms peak register peak/1/d1 3.5 3.4 3.6 3.3
PROCESSOR="AMD EPYC 7763 64-Core Processor" bundle peak peak valid 900 352
rc=0
out="$(PROCESSOR="AMD EPYC 7763 64-Core Processor" run_check "$WORK/legs/peak/bundle.json" 2>&1)" || rc=$?
assert_lacks "a 7763 night reading like the 7763 nights before it is not flagged" "peak register latency_p50_ms" "$out"
PROCESSOR="AMD EPYC 9V74 80-Core Processor" bundle peak peak valid 900 352
out="$(PROCESSOR="AMD EPYC 9V74 80-Core Processor" run_check "$WORK/legs/peak/bundle.json" 2>&1)" || true
assert_contains "the same reading on a 9V74 is past the 9V74 nights" "peak register latency_p50_ms: 3.45 -> 352" "$out"

table="$(cat "$WORK/table.md")"
assert_contains "the night is written as a table of each series against its nights" \
  "| peak · register · latency p50 (ms) | ≤ 10.35: 3 times the median 3.45 of 4 nights on this chip | 352 | FLAGGED |" "$table"

# The server's and database's levels read over past 1.5 times their median on the chip, and are
# reported, never failed.
SCENARIO=server nights perf_cpu_percent volume-500 measured volume-500/1/d1 20 21 19 20
SCENARIO=database nights perf_cpu_percent volume-500 measured volume-500/1/d1 30 31 29 30
bundle volume-500 volume-500 valid 6.1 3.1
jq '.target = {cpus: 1, memory_bytes: 402653184}
  | .phases = [{name: "steady", measured: true, target_busy_percent: 45, target_resident_bytes: 100663296}]
  | .database_levels = {cpu_percent: 32}' "$WORK/legs/volume-500/bundle.json" >"$WORK/levels.json"
mv "$WORK/levels.json" "$WORK/legs/volume-500/bundle.json"
rc=0
out="$(run_check "$WORK/legs/volume-500/bundle.json" 2>&1)" || rc=$?
assert_eq "a level past 1.5 times its nights does not fail the night" "0" "$rc"
assert_contains "and is reported against its scenario's nights" \
  "REPORTED:volume-500 server cpu_percent: 20 -> 45 (past 1.5 times the median of its last 14 nights)" "$out"
assert_lacks "while a level inside its nights is not" "volume-500 database cpu_percent" "$out"
table="$(cat "$WORK/table.md")"
assert_contains "the table marks the level over" \
  "| volume-500 · server · processor (% of allowance) | ≤ 30: 1.5 times the median 20 of 4 nights on this chip | 45 | over |" "$table"
assert_contains "and the database's level as passing" \
  "| volume-500 · database · processor (% of allowance) | ≤ 45: 1.5 times the median 30 of 4 nights on this chip | 32 | pass |" "$table"

# The breakpoint fails the night below the median of the machines its nights on the chip held.
ladder() { # held
  local held="$1"
  bundle breakpoint breakpoint valid 120 20
  jq --argjson held "$held" '.run.profile_version = 2 | .phases = []
    | .breaking_point = {held_agents: $held, gave_at: "step-8000"}' \
    "$WORK/legs/breakpoint/bundle.json" >"$WORK/ladder.json"
  mv "$WORK/ladder.json" "$WORK/legs/breakpoint/bundle.json"
}
PROCESSOR="AMD EPYC 9V74 80-Core Processor" nights perf_breakpoint_held_agents breakpoint ladder breakpoint/2/d1 \
  8000 8000 4000 8000
PROCESSOR="AMD EPYC 9V74 80-Core Processor" ladder 8000
rc=0
out="$(PROCESSOR="AMD EPYC 9V74 80-Core Processor" run_check "$WORK/legs/breakpoint/bundle.json" 2>&1)" || rc=$?
assert_eq "a breakpoint holding its nights' median passes" "0" "$rc"
PROCESSOR="AMD EPYC 9V74 80-Core Processor" ladder 4000
rc=0
out="$(PROCESSOR="AMD EPYC 9V74 80-Core Processor" run_check "$WORK/legs/breakpoint/bundle.json" 2>&1)" || rc=$?
assert_eq "a breakpoint holding fewer machines than its nights' median fails the night" "1" "$rc"
assert_contains "and the alert names what it held against the median" \
  "breakpoint held_agents: 8000 -> 4000 (below the median of its last 14 nights on this chip)" "$out"
PROCESSOR="AMD EPYC 7763 64-Core Processor" ladder 4000
rc=0
out="$(PROCESSOR="AMD EPYC 7763 64-Core Processor" run_check "$WORK/legs/breakpoint/bundle.json" 2>&1)" || rc=$?
assert_eq "a breakpoint on a chip with no nights of its own is not compared" "0" "$rc"

# A scaling rung fails the night when it misses the sign-in promise on a chip where most of its
# nights kept it; on a chip where most missed it, the miss is reported.
rung() { # cpus p95 p50
  local cpus="$1" p95="$2" p50="$3"
  bundle "scaling-$cpus" scaling valid "$p95" "$p50"
  jq --argjson cpus "$cpus" '.target = {cpus: $cpus, memory_bytes: 402653184}' \
    "$WORK/legs/scaling-$cpus/bundle.json" >"$WORK/rung.json"
  mv "$WORK/rung.json" "$WORK/legs/scaling-$cpus/bundle.json"
}
PROCESSOR="AMD EPYC 9V74 80-Core Processor" nights perf_signin_promise_kept scaling-0.5 register scaling/1/d1 1 1 0 1
PROCESSOR="AMD EPYC 7763 64-Core Processor" nights perf_signin_promise_kept scaling-0.5 register scaling/1/d1 0 0 1 0
PROCESSOR="AMD EPYC 9V74 80-Core Processor" rung 0.5 2438 453
rc=0
out="$(PROCESSOR="AMD EPYC 9V74 80-Core Processor" run_check "$WORK/legs/scaling-0.5/bundle.json" 2>&1)" || rc=$?
assert_eq "a rung missing the sign-in promise on a chip where it usually keeps it fails the night" "1" "$rc"
assert_contains "and the alert says so" \
  "scaling-0.5 sign-in promise: missed on a chip where most of its last 14 nights kept it" "$out"
PROCESSOR="AMD EPYC 7763 64-Core Processor" rung 0.5 2438 453
rc=0
out="$(PROCESSOR="AMD EPYC 7763 64-Core Processor" run_check "$WORK/legs/scaling-0.5/bundle.json" 2>&1)" || rc=$?
assert_eq "a rung missing it on a chip where it usually misses it does not fail" "0" "$rc"
table="$(cat "$WORK/table.md")"
assert_contains "and the table shows the miss against its nights" \
  "| scaling-0.5 · sign-in promise | kept on 1 of its 4 nights on this chip | missed | missed |" "$table"
PROCESSOR="AMD EPYC 9V74 80-Core Processor" rung 0.5 5 3
rc=0
out="$(PROCESSOR="AMD EPYC 9V74 80-Core Processor" run_check "$WORK/legs/scaling-0.5/bundle.json" 2>&1)" || rc=$?
assert_eq "a rung that keeps the promise passes" "0" "$rc"

rc=0
STARTED_OVERRIDE="" run_check "$WORK/legs/volume-500/bundle.json" >/dev/null 2>&1 || rc=$?
assert_eq "a check that does not know tonight's date refuses" "2" "$rc"

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
