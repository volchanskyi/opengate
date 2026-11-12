#!/usr/bin/env bash
# Tests for scripts/perf-vm-push.sh, which pushes each leg's bundle rows into the trend.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
PUSH="$REPO_ROOT/scripts/perf-vm-push.sh"

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

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
mkdir -p "$WORK/bin"
cat >"$WORK/bin/kubectl" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$KUBECTL_ARGS"
cat >>"$KUBECTL_STDIN"
STUB
chmod +x "$WORK/bin/kubectl"

# bundle DIR PROFILE VERDICT P95 P50 CONNECT ERRORS — a leg's evidence, in the
# shape the harness writes it.
bundle() {
  local dir="$1" profile="$2" verdict="$3" p95="$4" p50="$5" connect="$6" errors="$7"
  mkdir -p "$dir"
  jq -n --arg profile "$profile" --arg verdict "$verdict" \
    --argjson p95 "$p95" --argjson p50 "$p50" --argjson connect "$connect" --argjson errors "$errors" '{
      run: {commit: "abc123", profile_name: $profile, profile_version: 1, profile_digest: "d1",
            environment: "runner",
            started_at: "2026-09-29T13:44:35Z", finished_at: "2026-09-29T13:51:35Z"},
      verdict: {result: $verdict},
      observations: [
        {series: "aggregate_error_rate", value: $errors},
        {series: "connect_p95_ms", value: $connect},
        {series: "register_p95_ms", value: $p95},
        {series: "register_p50_ms", value: $p50}
      ]}' >"$dir/bundle.json"
}

bundle "$WORK/perf-bundles/scaling-0.25" scaling valid 10000 595 188 0
bundle "$WORK/perf-bundles/volume-500" volume-500 valid 4.99 3.1 2 0
bundle "$WORK/perf-bundles/spike" spike invalid 9650 6140 45 0.2
bundle "$WORK/soak-bundle/soak" soak valid 9.11 3.25 2 0

run_push() {
  : >"$WORK/args"
  : >"$WORK/pushed"
  PATH="$WORK/bin:$PATH" KUBECTL_ARGS="$WORK/args" KUBECTL_STDIN="$WORK/pushed" \
    VM_RUN_STARTED_AT=1790689000 GITHUB_WORKFLOW="Performance Stack" GITHUB_SHA=abc123 GITHUB_RUN_ID=77 \
    "$PUSH" "$@"
}

echo "perf-vm-push:"

if out="$(run_push "$WORK/perf-bundles/scaling-0.25/bundle.json" "$WORK/perf-bundles/volume-500/bundle.json" \
  "$WORK/perf-bundles/spike/bundle.json" "$WORK/soak-bundle/soak/bundle.json" 2>&1)"; then
  pass "a night's legs are pushed"
else
  fail "a night's legs are pushed (out=[$out])"
fi

pushed="$(cat "$WORK/pushed")"
check() { # description, line
  if grep -qxF -- "$2" <<<"$pushed"; then pass "$1"; else fail "$1 (missing [$2] in [$pushed])"; fi
}
check "a leg's registration tail is named by the leg and the workload it measured" \
  'perf_latency_p95_ms{env="ci",leg="scaling-0.25",phase="register",workload="scaling/1/d1"} 10000 1790689000000'
check "and its middle case beside it" \
  'perf_latency_p50_ms{env="ci",leg="volume-500",phase="register",workload="volume-500/1/d1"} 3.1 1790689000000'
check "and the connect tail" \
  'perf_latency_p95_ms{env="ci",leg="volume-500",phase="connect",workload="volume-500/1/d1"} 2 1790689000000'
check "and the error rate" \
  'perf_error_rate{env="ci",leg="scaling-0.25",phase="aggregate",workload="scaling/1/d1"} 0 1790689000000'
check "the endurance run is a leg of its own" \
  'perf_latency_p95_ms{env="ci",leg="soak",phase="register",workload="soak/1/d1"} 9.11 1790689000000'

if grep -qF 'leg="spike"' <<<"$pushed"; then
  fail "a leg that did not measure the system stays out of the trend"
else
  pass "a leg that did not measure the system stays out of the trend"
fi
if grep -qF 'spike' <<<"$out" && grep -qi 'did not measure' <<<"$out"; then
  pass "and the push says which leg it left out"
else
  fail "and the push says which leg it left out (out=[$out])"
fi
samples="$(grep -vE '^ci_run_info' <<<"$pushed" || true)"
if grep -qE '[{,](commit|run_id)=' <<<"$samples"; then
  fail "a leg's sample names the measurement only"
else
  pass "a leg's sample names the measurement only"
fi

printf 'volume-500\tregister\tlatency_p95_ms\n' >"$WORK/flagged.tsv"
PERF_FLAGGED_FILE="$WORK/flagged.tsv" run_push "$WORK/perf-bundles/volume-500/bundle.json" >/dev/null 2>&1
pushed="$(cat "$WORK/pushed")"
check "a series the check flagged is pushed as flagged" \
  'perf_latency_p95_ms{env="ci",flagged="1",leg="volume-500",phase="register",workload="volume-500/1/d1"} 4.99 1790689000000'
check "and the leg's other series as they are" \
  'perf_latency_p50_ms{env="ci",leg="volume-500",phase="register",workload="volume-500/1/d1"} 3.1 1790689000000'

bundle "$WORK/perf-bundles/scaling-0.5" scaling valid 900 400 20 0
jq '.runner = {processor_model: "AMD EPYC 7763 64-Core Processor", steal_percent: 0.4}' \
  "$WORK/perf-bundles/scaling-0.5/bundle.json" >"$WORK/runner.json"
mv "$WORK/runner.json" "$WORK/perf-bundles/scaling-0.5/bundle.json"
run_push "$WORK/perf-bundles/scaling-0.5/bundle.json" >/dev/null 2>&1
pushed="$(cat "$WORK/pushed")"
check "a leg that recorded its runner names the processor it ran on, so nights compare like with like" \
  'perf_latency_p95_ms{env="ci",leg="scaling-0.5",phase="register",processor="AMD EPYC 7763 64-Core Processor",workload="scaling/1/d1"} 900 1790689000000'

# The catch-up leg carries the store probe, the fleet's catch-up and live share, and the runner's
# fixed benchmark; each is pushed under its own name.
bundle "$WORK/perf-bundles/catch-up" catch-up valid 300 120 40 0
jq '.runner = {processor_model: "Neoverse-N2", cpu_benchmark_rounds_per_second: 812.5}
  | .catch_up = {stored: 10000, deferred: 40, unanswered: 0, complete: 5000, last_stored_seconds: 1312.5}
  | .live_acceptance = {received: 360000, stored: 359900, within_five_seconds: 357120}
  | .store_probe = {oom_killed: false, quiet: {seconds: 60, cpu_share: 0.12, memory_share: 0.31},
      one_sender: {senders: 1, duration_seconds: 900,
        catch_up: {readings_per_second: 10500}, live: {requests: 900, within_five_seconds: 891, p99_ms: 420},
        store: {peak_memory_bytes: 400000000}},
      two_senders: {senders: 2, duration_seconds: 900,
        catch_up: {readings_per_second: 14000}, live: {requests: 900, within_five_seconds: 855, p99_ms: 6100},
        store: {peak_memory_bytes: 500000000}}}' \
  "$WORK/perf-bundles/catch-up/bundle.json" >"$WORK/catch-up.json"
mv "$WORK/catch-up.json" "$WORK/perf-bundles/catch-up/bundle.json"
run_push "$WORK/perf-bundles/catch-up/bundle.json" >/dev/null 2>&1
pushed="$(cat "$WORK/pushed")"
check "the store's catch-up rate is pushed for each sender count" \
  'perf_catchup_readings_per_second{env="ci",leg="catch-up",processor="Neoverse-N2",senders="1",workload="catch-up/1/d1"} 10500 1790689000000'
check "beside the share of live-shaped requests it took within 5 s" \
  'perf_catchup_live_within_5s_share{env="ci",leg="catch-up",processor="Neoverse-N2",senders="2",workload="catch-up/1/d1"} 0.95 1790689000000'
check "and the store's largest memory" \
  'perf_catchup_store_peak_memory_bytes{env="ci",leg="catch-up",processor="Neoverse-N2",senders="1",workload="catch-up/1/d1"} 400000000 1790689000000'
check "the quiet minute's processor share" \
  'perf_catchup_quiet_cpu_share{env="ci",leg="catch-up",processor="Neoverse-N2",workload="catch-up/1/d1"} 0.12 1790689000000'
check "the time the fleet's last gap was filled" \
  'perf_catchup_fill_seconds{env="ci",leg="catch-up",processor="Neoverse-N2",workload="catch-up/1/d1"} 1312.5 1790689000000'
check "the share of live readings the server's store accepted within 5 s" \
  'perf_catchup_live_accepted_within_5s_share{env="ci",leg="catch-up",processor="Neoverse-N2",workload="catch-up/1/d1"} 0.992 1790689000000'
check "and the runner's fixed benchmark beside its processor" \
  'perf_runner_benchmark_rounds_per_second{env="ci",leg="catch-up",processor="Neoverse-N2",workload="catch-up/1/d1"} 812.5 1790689000000'

# The probe and the benchmark measure the store and the runner, so a fleet run that measured
# nothing still publishes them.
jq '.verdict.result = "invalid"' "$WORK/perf-bundles/catch-up/bundle.json" >"$WORK/catch-up.json"
mkdir -p "$WORK/invalid/catch-up"
mv "$WORK/catch-up.json" "$WORK/invalid/catch-up/bundle.json"
if out="$(run_push "$WORK/invalid/catch-up/bundle.json" 2>&1)"; then
  pass "an invalid fleet run with a store probe is pushed"
else
  fail "an invalid fleet run with a store probe is pushed (out=[$out])"
fi
pushed="$(cat "$WORK/pushed")"
check "its store probe is pushed whatever the fleet's verdict" \
  'perf_catchup_readings_per_second{env="ci",leg="catch-up",processor="Neoverse-N2",senders="1",workload="catch-up/1/d1"} 10500 1790689000000'
check "with the quiet minute" \
  'perf_catchup_quiet_cpu_share{env="ci",leg="catch-up",processor="Neoverse-N2",workload="catch-up/1/d1"} 0.12 1790689000000'
check "and the runner's benchmark" \
  'perf_runner_benchmark_rounds_per_second{env="ci",leg="catch-up",processor="Neoverse-N2",workload="catch-up/1/d1"} 812.5 1790689000000'
if grep -qE '^perf_(catchup_fill_seconds|catchup_machines_caught_up|catchup_live_accepted|latency_|error_rate)' <<<"$pushed"; then
  fail "the invalid fleet's own readings stay out of the trend (pushed=[$pushed])"
else
  pass "the invalid fleet's own readings stay out of the trend"
fi
if grep -qi 'did not measure' <<<"$out"; then
  pass "and the push says the fleet was left out"
else
  fail "and the push says the fleet was left out (out=[$out])"
fi

# The server's and database's levels, each a share of its own allowance over the measured phases.
bundle "$WORK/levels/peak" peak valid 9.9 3.3 2 0
jq '.target = {cpus: 1, memory_bytes: 402653184}
  | .phases = [{name: "wave", measured: true, target_busy_percent: 42.5, target_resident_bytes: 201326592}]
  | .database_levels = {cpu_percent: 31.25, memory_percent: 12.5}' \
  "$WORK/levels/peak/bundle.json" >"$WORK/levels.json"
mv "$WORK/levels.json" "$WORK/levels/peak/bundle.json"
run_push "$WORK/levels/peak/bundle.json" >/dev/null 2>&1
pushed="$(cat "$WORK/pushed")"
check "the server's processor level is pushed under its scenario" \
  'perf_cpu_percent{env="ci",leg="peak",phase="measured",scenario="server",workload="peak/1/d1"} 42.5 1790689000000'
check "and its memory level" \
  'perf_memory_percent{env="ci",leg="peak",phase="measured",scenario="server",workload="peak/1/d1"} 50 1790689000000'
check "beside the database's" \
  'perf_cpu_percent{env="ci",leg="peak",phase="measured",scenario="database",workload="peak/1/d1"} 31.25 1790689000000'

# The breakpoint's capacity is the machines it held, pushed so a night is read against its own.
bundle "$WORK/ladder/breakpoint" breakpoint valid 120 20 30 0
jq '.run.profile_version = 2 | .runner = {processor_model: "AMD EPYC 9V74 80-Core Processor"}
  | .phases = [] | .breaking_point = {held_agents: 4000, gave_at: "step-8000"}' \
  "$WORK/ladder/breakpoint/bundle.json" >"$WORK/ladder.json"
mv "$WORK/ladder.json" "$WORK/ladder/breakpoint/bundle.json"
run_push "$WORK/ladder/breakpoint/bundle.json" >/dev/null 2>&1
pushed="$(cat "$WORK/pushed")"
check "the breakpoint's held machines are pushed beside its chip" \
  'perf_breakpoint_held_agents{env="ci",leg="breakpoint",phase="ladder",processor="AMD EPYC 9V74 80-Core Processor",workload="breakpoint/2/d1"} 4000 1790689000000'

# A scaling rung pushes whether it kept the profile's ordinary sign-in promise, and a sweep whose
# every rung measured pushes the smallest share that kept it.
sweep() { # cpus p50 p95 [verdict]
  local cpus="$1" p50="$2" p95="$3" verdict="${4:-valid}"
  bundle "$WORK/sweep/scaling-$cpus" scaling "$verdict" "$p95" "$p50" 20 0
  jq --argjson cpus "$cpus" '.target = {cpus: $cpus, memory_bytes: 402653184}' \
    "$WORK/sweep/scaling-$cpus/bundle.json" >"$WORK/rung.json"
  mv "$WORK/rung.json" "$WORK/sweep/scaling-$cpus/bundle.json"
}
sweep 0.25 15387 51049
sweep 0.5 4 415
sweep 1 3 5
sweep 2 3 5
run_push "$WORK"/sweep/scaling-*/bundle.json >/dev/null 2>&1
pushed="$(cat "$WORK/pushed")"
check "a rung that missed the sign-in promise is pushed as missed" \
  'perf_signin_promise_kept{env="ci",leg="scaling-0.25",phase="register",workload="scaling/1/d1"} 0 1790689000000'
check "and one that kept it as kept" \
  'perf_signin_promise_kept{env="ci",leg="scaling-0.5",phase="register",workload="scaling/1/d1"} 1 1790689000000'
check "the sweep's sizing figure is the smallest share that kept it" \
  'perf_scaling_sizing_cpus{env="ci",workload="scaling/1"} 0.5 1790689000000'
sweep 0.5 4 415 invalid
run_push "$WORK"/sweep/scaling-*/bundle.json >/dev/null 2>&1
pushed="$(cat "$WORK/pushed")"
if grep -qF 'perf_scaling_sizing_cpus' <<<"$pushed"; then
  fail "a sweep missing a rung pushes no sizing figure (pushed=[$pushed])"
else
  pass "a sweep missing a rung pushes no sizing figure, since a hole would read as a larger one"
fi

if out="$(run_push "$WORK/perf-bundles/spike/bundle.json" 2>&1)"; then
  fail "a night with nothing measured is refused (out=[$out])"
else
  pass "a night with nothing measured is refused"
fi

if run_push "$WORK/no-such/bundle.json" >/dev/null 2>&1; then
  fail "a missing bundle is refused"
else
  pass "a missing bundle is refused"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
