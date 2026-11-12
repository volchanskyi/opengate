#!/usr/bin/env bash
# Holds each bundle-judged profile's limits level with the rows loadtest-bundle-rows.sh emits.
# Run: ./scripts/tests/loadtest-bundle-rows.test.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
ROWS="$REPO_ROOT/scripts/loadtest-bundle-rows.sh"
WORKFLOW_DIR="$REPO_ROOT/.github/workflows"
PROFILE_DIR="$REPO_ROOT/load/profiles"
# shellcheck source=scripts/lib/loadtest-profile.sh
. "$REPO_ROOT/scripts/lib/loadtest-profile.sh"

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

echo "loadtest bundle rows:"

[ -x "$ROWS" ] || {
  fail "$ROWS is not executable"
  printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
  exit 1
}

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

bundle_observing() {
  local out="$1"
  shift
  local series=()
  while [ "$#" -gt 0 ]; do
    series+=("$(jq -nc --arg s "$1" --argjson v "$2" \
      '{at: "2026-09-13T02:30:00Z", series: $s, value: $v}')")
    shift 2
  done
  jq -n --argjson o "$(printf '%s\n' "${series[@]+"${series[@]}"}" | jq -sc '.')" '{
    schema_version: 11,
    run: {
      id: "quic-agents-1",
      commit: "0b5d1f2c3a4e5d6f7089abcdef0123456789abcd",
      environment: "runner",
      finished_at: "2026-09-13T02:30:00Z"
    },
    observations: $o
  }' >"$out"
}

run_rows() {
  STATUS=0
  OUT="$("$ROWS" "$@" 2>"$WORK/err.txt")" || STATUS=$?
}

bundle_observing "$WORK/full.json" \
  aggregate_error_rate 0.004 connect_p95_ms 41 handshake_p95_ms 12 \
  register_p50_ms 8.1 register_p95_ms 23.45 wave_online_share 1 wave_online_p95_ms 420 \
  target_retained_goroutines_per_operation 0 target_retained_bytes_per_operation 930
# The full bundle carries every section a leg can write, so each kind of leg is cut from it.
jq '.live_acceptance = {received: 360000, stored: 359900, within_five_seconds: 357120}
  | .catch_up = {stored: 10000, deferred: 40, unanswered: 0, complete: 5000, last_stored_seconds: 812.5}
  | .live_delay = {sent: 590, seen: 590, within_five_seconds: 588}
  | .store_probe = {oom_killed: false, quiet: {cpu_share: 0.01, memory_share: 0.09},
      one_sender: {duration_seconds: 900, catch_up: {readings_per_second: 246180}, live: {requests: 899, within_five_seconds: 899},
        store: {peak_memory_bytes: 400000000, memory_limit_bytes: 536870912, restarted: false, stopped_answering: false}},
      two_senders: {duration_seconds: 900, catch_up: {readings_per_second: 219255}, live: {requests: 900, within_five_seconds: 900},
        store: {peak_memory_bytes: 420000000, memory_limit_bytes: 536870912, restarted: false, stopped_answering: false}}}
  | .journeys = [{name: "command-accept", error_rate: 0, latency_p50_ms: 2.6, latency_p95_ms: 18.2},
      {name: "device-detail", error_rate: 0, latency_p50_ms: 11.8, latency_p95_ms: 26.9},
      {name: "device-list", error_rate: 0, latency_p50_ms: 12.7, latency_p95_ms: 26.9},
      {name: "relay-session-echo", error_rate: 0, latency_p50_ms: 3, latency_p95_ms: 7}]
  | .leak_trail = {growth: [{site: "a", intervals_grown: 40, intervals: 57}, {site: "b", intervals_grown: 1, intervals: 57}]}
  | .breaking_point = {held_at: "step-2000", held_agents: 2000, gave_at: "step-4000", gave_agents: 4000}
  | .target = {cpus: 1, memory_bytes: 1073741824}
  | .phases = [{name: "step-1000", measured: true, offered_connected_agents: 1000, error_rate: 0, latency_p95_ms: 6,
                target_busy_percent: 38, target_resident_bytes: 200000000},
               {name: "step-2000", offered_connected_agents: 2000, error_rate: 0.001, latency_p95_ms: 117},
               {name: "step-4000", offered_connected_agents: 4000, error_rate: 0.023, latency_p95_ms: 483}]
  | .database_levels = {cpu_percent: 20, cpu_cap: "1 processor", memory_percent: 30, memory_cap: "1 GiB"}' \
  "$WORK/full.json" >"$WORK/full-catch-up.json"
mv "$WORK/full-catch-up.json" "$WORK/full.json"
# Every leg writes machines, technicians, live readings and levels; catch-up adds its catch-up and
# probe and no wave, the soak its leak trail, the breakpoint its ladder in place of a wave.
jq 'del(.catch_up, .live_delay, .store_probe, .leak_trail, .breaking_point)' "$WORK/full.json" >"$WORK/kind-plain.json"
jq 'del(.leak_trail, .breaking_point) | .observations |= map(select(.series | startswith("wave") | not))' \
  "$WORK/full.json" >"$WORK/kind-catch-up.json"
jq 'del(.catch_up, .live_delay, .store_probe, .breaking_point)' "$WORK/full.json" >"$WORK/kind-soak.json"
jq 'del(.catch_up, .live_delay, .store_probe, .leak_trail) | .observations |= map(select(.series | startswith("wave") | not))' \
  "$WORK/full.json" >"$WORK/kind-breakpoint.json"
run_rows "$WORK/full.json"
assert_eq "a bundle carrying every series is read" "0" "$STATUS"

# measurements_of prints each series|metric a set of rows carries a reading for.
measurements_of() {
  jq -r '
    .[] | . as $row
    | to_entries[]
    | select((.key | IN("source", "scenario", "phase", "commit", "env", "timestamp", "workload", "target_cpus",
        "cpu_cap", "memory_cap", "memory_bytes") | not) and .value != null)
    | "\($row.source)/\($row.scenario)/\($row.phase)|\(.key)"' <<<"$1" | sort -u
}

assert_eq "the aggregate error rate is read off the bundle" "0.004" \
  "$(jq -r '.[] | select(.phase == "aggregate") | .error_rate' <<<"$OUT")"
assert_eq "registration is read off the bundle" "23.45" \
  "$(jq -r '.[] | select(.phase == "register") | .latency_p95_ms' <<<"$OUT")"
assert_eq "so is its middle case" "8.1" \
  "$(jq -r '.[] | select(.phase == "register") | .latency_p50_ms' <<<"$OUT")"
assert_eq "the measured wave's share is read off the bundle" "1" \
  "$(jq -r '.[] | select(.phase == "wave") | .arrival_share' <<<"$OUT")"
assert_eq "so is its time from dial to online" "420" \
  "$(jq -r '.[] | select(.phase == "wave") | .latency_p95_ms' <<<"$OUT")"
assert_eq "every machine-side row names the machine side" "quic quic-agents" \
  "$(jq -r '[.[] | select(.source == "quic")] | [.[] | .source] + [.[] | .scenario] | unique | join(" ")' <<<"$OUT")"
assert_eq "the technicians are read under the in-cluster names" "command_accept device_detail device_list relay" \
  "$(jq -r '[.[] | select(.source == "k6") | .phase] | sort | join(" ")' <<<"$OUT")"
assert_eq "the batches never answered are read off the catch-up" "0" \
  "$(jq -r '.[] | select(.scenario == "batches") | .unanswered' <<<"$OUT")"
assert_eq "the watched machines' live minutes are a share of those sent" "0.9966" \
  "$(jq -r '.[] | select(.scenario == "watched") | .within_5s_share * 10000 | round / 10000' <<<"$OUT")"
assert_eq "the store's peak memory is a share of its limit" "0.7823" \
  "$(jq -r '.[] | select(.scenario == "probe-store") | .peak_memory_share * 10000 | round / 10000' <<<"$OUT")"
assert_eq "each probe's run length is read off it, so a probe cut short fails the night" "900 900" \
  "$(jq -r '[.[] | select(.scenario == "probe-one" or .scenario == "probe-two") | .duration_seconds] | join(" ")' <<<"$OUT")"
assert_eq "a heap site grown in more than half the intervals counts" "1" \
  "$(jq -r '.[] | select(.source == "leak") | .growing_sites' <<<"$OUT")"
assert_eq "the worst rung that held is read beside the one that gave" "0.001 117 0.023 483" \
  "$(jq -r '[.[] | select(.phase == "rungs-held", .phase == "rung-gave") | "\(.error_rate) \(.latency_p95_ms)"] | join(" ")' <<<"$OUT")"
jq '.phases[0] += {register_p50_ms: 3.2, register_p95_ms: 9.9}' "$WORK/full.json" >"$WORK/phase-signins.json"
assert_eq "a phase's own sign-in times are read off it" "3.2 9.9" \
  "$(jq -r '.[] | select(.scenario == "register" and .phase == "step-1000") | "\(.latency_p50_ms) \(.latency_p95_ms)"' \
    <<<"$("$ROWS" "$WORK/phase-signins.json")")"
jq '.phases[1] += {fleet_online_p95_seconds: 130.5, fleet_online_all_seconds: 198}' "$WORK/full.json" >"$WORK/fleet-online.json"
assert_eq "a timed phase's fleet-online times are read off it" "130.5 198" \
  "$(jq -r '.[] | select(.scenario == "online" and .phase == "step-2000") | "\(.online_p95_seconds) \(.online_all_seconds)"' \
    <<<"$("$ROWS" "$WORK/fleet-online.json")")"
assert_eq "an untimed phase writes no fleet-online row" "0" \
  "$(jq '[.[] | select(.scenario == "online")] | length' <<<"$OUT")"
assert_eq "the server's level is read over the measured phase" "38" \
  "$(jq -r '.[] | select(.source == "service" and .scenario == "server") | .cpu_percent' <<<"$OUT")"
assert_eq "the share of live readings stored within 5 s is read off the server's count" "0.992" \
  "$(jq -r '.[] | select(.scenario == "live") | .within_5s_share' <<<"$OUT")"
assert_eq "so are the machines caught up" "5000" \
  "$(jq -r '.[] | select(.scenario == "gaps") | .machines_caught_up' <<<"$OUT")"
assert_eq "and when the last gap was filled" "812.5" \
  "$(jq -r '.[] | select(.scenario == "gaps") | .fill_seconds' <<<"$OUT")"

# An absent registration row lets its limits fail the run; a row carrying zero would pass them.
bundle_observing "$WORK/no-register.json" aggregate_error_rate 0 connect_p95_ms 41
run_rows "$WORK/no-register.json"
assert_eq "a run the server did not answer still reads" "0" "$STATUS"
assert_eq "and publishes no registration row" "0" \
  "$(jq '[.[] | select(.phase == "register")] | length' <<<"$OUT")"
assert_eq "a run that judged no wave publishes no wave row" "0" \
  "$(jq '[.[] | select(.phase == "wave")] | length' <<<"$OUT")"
assert_eq "a leg with no catch-up publishes no catch-up row" "0" \
  "$(jq '[.[] | select(.source == "catchup")] | length' <<<"$OUT")"

run_rows "$WORK/absent.json"
assert_eq "a bundle that is not there refuses" "2" "$STATUS"

bundle_observing "$WORK/no-aggregate.json" connect_p95_ms 41 register_p95_ms 23
run_rows "$WORK/no-aggregate.json"
assert_eq "a bundle stating no aggregate error rate refuses" "2" "$STATUS"
if grep -q 'aggregate' "$WORK/err.txt"; then
  pass "and says which reading it wanted"
else
  fail "and says which reading it wanted"
fi

bundle_observing "$WORK/empty.json"
run_rows "$WORK/empty.json"
assert_eq "a bundle observing nothing refuses" "2" "$STATUS"

run_rows
assert_eq "a call naming no bundle refuses" "2" "$STATUS"

jq --argjson phases "$(profile_phases "$PROFILE_DIR/peak.yaml")" \
  '.phases += [$phases[] | {name, register_p50_ms: 3, register_p95_ms: 9}]' "$WORK/kind-plain.json" >"$WORK/peak-leg.json"
run_rows "$WORK/peak-leg.json" "$WORK/rows.json"
assert_eq "rows can be written for the evaluator" "0" "$STATUS"
GATE_STATUS=0
"$REPO_ROOT/scripts/loadtest-gate-check.sh" "$PROFILE_DIR/peak.yaml" "$WORK/rows.json" \
  >"$WORK/gate.txt" 2>&1 || GATE_STATUS=$?
assert_eq "the evaluator reads them without complaint" "0" "$GATE_STATUS"
if grep -qE 'gates: [0-9]+ read, 0 breached' "$WORK/gate.txt"; then
  pass "and reports how many limits it read"
else
  fail "and reports how many limits it read: $(cat "$WORK/gate.txt")"
fi

# Profiles named by a workflow that reads its limits through scripts/perf-bundle-limits.sh, or
# that calls a workflow of this repository which does.
bundle_judged_profiles() {
  local workflow judging called
  judging="$(grep -rlE 'perf-bundle-limits\.sh' "$WORKFLOW_DIR" 2>/dev/null || true)"
  while IFS= read -r workflow; do
    [ -n "$workflow" ] || continue
    if grep -qxF "$workflow" <<<"$judging"; then
      grep -oE 'load/profiles/[a-z0-9-]+\.yaml' "$workflow" || true
      continue
    fi
    while IFS= read -r called; do
      [ -n "$called" ] || continue
      if grep -qxF "$REPO_ROOT/$called" <<<"$judging"; then
        grep -oE 'load/profiles/[a-z0-9-]+\.yaml' "$workflow" || true
      fi
    done < <(sed -n 's|^[[:space:]]*uses:[[:space:]]*\./\(\.github/workflows/[A-Za-z0-9_.-]*\)[[:space:]]*$|\1|p' "$workflow")
  done < <(find "$WORKFLOW_DIR" -name '*.yml' | sort) | sort -u
}

# Workflows that write a bundle and have no browser-side export to join it to.
machine_side_workflows() {
  local workflow
  while IFS= read -r workflow; do
    grep -qE 'loadtest-summarize\.sh' "$workflow" && continue
    echo "$workflow"
  done < <(grep -rlE -- '-bundle=' "$WORKFLOW_DIR" 2>/dev/null || true)
}

swept=0
while IFS= read -r workflow; do
  [ -n "$workflow" ] || continue
  swept=$((swept + 1))
  if grep -qE 'perf-bundle-limits\.sh' "$workflow"; then
    pass "$(basename "$workflow") reads its limits off the run's own evidence"
  else
    fail "$(basename "$workflow") writes a bundle and reads no limit at all"
  fi
done < <(machine_side_workflows)

if [ "$swept" -ge 2 ]; then
  pass "reached $swept workflow(s) whose only output is a bundle"
else
  fail "reached only $swept such workflow(s) — the naming changed shape"
fi

# Each profile and the rows its own kind of leg writes agree both ways: every limit names a
# measurement the leg writes, and every measurement it writes carries a limit.
profiles=0
limits=0
while IFS= read -r relative; do
  [ -n "$relative" ] || continue
  profile="$REPO_ROOT/$relative"
  if [ ! -s "$profile" ]; then
    fail "a workflow names $relative and there is no such profile"
    continue
  fi
  profiles=$((profiles + 1))
  name="$(basename "$relative" .yaml)"
  case "$name" in
    catch-up | soak | breakpoint) kind="$WORK/kind-$name.json" ;;
    *) kind="$WORK/kind-plain.json" ;;
  esac
  # Every phase of the profile signed machines in, so each phase writes its sign-in row; the
  # harness times a phase's fleet online where a limit names it.
  timed="$(profile_gates "$profile" | jq -c '[.[] | .series | select(startswith("quic/online/")) | ltrimstr("quic/online/")] | unique')"
  jq --argjson phases "$(profile_phases "$profile")" --argjson timed "$timed" \
    '.phases += [$phases[] | {name, register_p50_ms: 3, register_p95_ms: 9}
      + (if (.name | IN($timed[])) then {fleet_online_p95_seconds: 120, fleet_online_all_seconds: 190} else {} end)]' \
    "$kind" >"$WORK/leg.json"
  written="$(measurements_of "$("$ROWS" "$WORK/leg.json")")"
  limited="$(profile_gates "$profile" | jq -r '.[] | "\(.series)|\(.metric)"' | sort -u)"
  limits=$((limits + $(grep -c . <<<"$limited")))

  unread="" unlimited=""
  while IFS= read -r measurement; do
    [ -n "$measurement" ] || continue
    grep -qxF "$measurement" <<<"$written" || unread="$unread $measurement"
  done <<<"$limited"
  while IFS= read -r measurement; do
    [ -n "$measurement" ] || continue
    grep -qxF "$measurement" <<<"$limited" || unlimited="$unlimited $measurement"
  done <<<"$written"
  if [ -z "$unread" ]; then
    pass "$name.yaml limits only what its leg writes"
  else
    fail "$name.yaml limits measurements its leg never writes:$unread"
  fi
  if [ -z "$unlimited" ]; then
    pass "every measurement the $name leg writes carries a limit"
  else
    fail "measurements the $name leg writes and its profile holds to nothing:$unlimited"
  fi
done < <(bundle_judged_profiles)

if [ "$profiles" -ge 5 ] && [ "$limits" -ge 8 ]; then
  pass "read $limits limit(s) across $profiles profile(s)"
else
  fail "read only $limits limit(s) across $profiles profile(s) — did the workflows stop naming profiles?"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf 'Failures:\n' >&2
  for f in "${FAILURES[@]}"; do printf '  - %s\n' "$f" >&2; done
  exit 1
fi
