#!/usr/bin/env bash
# Offline tests for the nightly QUIC network drill:
#   scripts/fault/network-drill.sh          the scenario runner
#   deploy/scripts/netfault-shaper-pod.sh   the shaper's pod manifest
#   .github/workflows/network-drill.yml     the nightly that drives them
#
# No live cluster: kubectl is stubbed on PATH and records its argv, and the
# stub answers the probe pod's curl calls from canned fixtures. That covers the
# namespace guard, the order the impairments are commanded in, the rule that a
# scenario which measured nothing emits no row at all, the counter agreement
# that proves a scenario ran, and teardown on every path.
#
# Run: ./scripts/tests/network-drill.test.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
RUNNER="$REPO_ROOT/scripts/fault/network-drill.sh"
POD_MANIFEST="$REPO_ROOT/deploy/scripts/netfault-shaper-pod.sh"
WORKFLOW="$REPO_ROOT/.github/workflows/network-drill.yml"
SUMMARIZE="$REPO_ROOT/scripts/network-drill-summarize.sh"
VM_PUSH="$REPO_ROOT/scripts/network-drill-vm-push.sh"
REGRESSION="$REPO_ROOT/scripts/network-drill-regression-check.sh"

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
assert_contains() {
  local name="$1" needle="$2" haystack="$3"
  if printf '%s\n' "$haystack" | grep -qF -- "$needle"; then pass "$name"; else fail "$name (missing [$needle])"; fi
}
assert_lacks() {
  local name="$1" needle="$2" haystack="$3"
  if printf '%s\n' "$haystack" | grep -qF -- "$needle"; then fail "$name (unexpected [$needle])"; else pass "$name"; fi
}

for f in "$RUNNER" "$POD_MANIFEST" "$SUMMARIZE" "$VM_PUSH" "$REGRESSION"; do
  [ -x "$f" ] || {
    echo "FAIL: $f not executable" >&2
    exit 1
  }
done

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
BIN_DIR="$WORK/bin"
mkdir -p "$BIN_DIR"

# The stub answers as the cluster would. Each canned answer is a file the test
# writes before the run, so a case states the world it is putting the runner in
# rather than threading flags through the stub.
cat >"$BIN_DIR/kubectl" <<'SH'
#!/usr/bin/env bash
set -uo pipefail
printf '%s\n' "$*" >>"${KUBECTL_ARGS:-/dev/null}"
# The push transport hands its payload to kubectl on standard input, so a test
# that wants to read what would have been sent asks for it here.
if [ -n "${KUBECTL_STDIN:-}" ]; then
  cat >"$KUBECTL_STDIN"
  exit 0
fi

# Everything the runner asks the cluster goes through `exec <pod> -- curl …`.
# The last argument is the URL, which is what decides the answer.
url=""
for arg in "$@"; do
  case "$arg" in http://*) url="$arg" ;; esac
done

case "$url" in
  *"/impair") cat "${MOCK_COUNTERS_FILE:-/dev/null}"; exit "${MOCK_IMPAIR_RC:-0}" ;;
  *"/rebind") cat "${MOCK_COUNTERS_FILE:-/dev/null}"; exit "${MOCK_REBIND_RC:-0}" ;;
  *"/counters") cat "${MOCK_COUNTERS_FILE:-/dev/null}"; exit "${MOCK_COUNTERS_RC:-0}" ;;
  *"/healthz") exit "${MOCK_HEALTH_RC:-0}" ;;
  # The chart endpoint's own path contains /devices, so it is matched first.
  *"/metrics?"*) cat "${MOCK_METRICS_FILE:-/dev/null}"; exit "${MOCK_METRICS_RC:-0}" ;;
  *"/devices"*) cat "${MOCK_DEVICES_FILE:-/dev/null}"; exit "${MOCK_DEVICES_RC:-0}" ;;
esac

case "${1:-}" in
  delete) exit "${MOCK_DELETE_RC:-0}" ;;
  get) printf 'stub kubectl get: %s\n' "$*" ;;
  *) printf 'stub kubectl: %s\n' "$*" ;;
esac
exit 0
SH
chmod +x "$BIN_DIR/kubectl"
export PATH="$BIN_DIR:$PATH"

# The device list is a bare array, which is the shape GET /api/v1/devices
# answers with. A fixture shaped like the endpoint is what makes these tests
# say anything about the runner reading the real one.
online_device() {
  cat >"$WORK/devices-online.json" <<JSON
[{"id":"11111111-1111-1111-1111-111111111111","hostname":"drill-machine","status":"online","last_seen":"$(date -u +%Y-%m-%dT%H:%M:%SZ)"}]
JSON
  printf '%s\n' "$WORK/devices-online.json"
}

offline_device() {
  cat >"$WORK/devices-offline.json" <<JSON
[{"id":"11111111-1111-1111-1111-111111111111","hostname":"drill-machine","status":"offline","last_seen":"$(date -u -d '-5 minutes' +%Y-%m-%dT%H:%M:%SZ)"}]
JSON
  printf '%s\n' "$WORK/devices-offline.json"
}

# A chart window whose buckets are all present is a filled gap; one with nulls
# in it is the hole the outage left. The shape is the chart endpoint's own:
# every bucket of the requested window is there, and one the machine did not
# report for is null.
full_window() {
  printf '{"t":[1,2,3,4,5,6,7,8,9,10],"bucket_s":60,"downsampled":true,"series":[{"name":"cpu.util","min_max_source":"none","avg":[1,2,3,4,5,6,7,8,9,10]}]}\n' >"$WORK/metrics-full.json"
  printf '%s\n' "$WORK/metrics-full.json"
}
holed_window() {
  printf '{"t":[1,2,3,4,5,6,7,8,9,10],"bucket_s":60,"downsampled":true,"series":[{"name":"cpu.util","min_max_source":"none","avg":[1,2,null,null,null,null,null,8,9,10]}]}\n' >"$WORK/metrics-holed.json"
  printf '%s\n' "$WORK/metrics-holed.json"
}

counters_file() {
  local to_server_dropped="$1" blackhole="${2:-false}"
  cat >"$WORK/counters.json" <<JSON
{"to_server":{"in":100,"out":$((100 - to_server_dropped)),"dropped":$to_server_dropped},
 "to_machine":{"in":90,"out":90,"dropped":0},
 "machines":21,"rebinds":0,"seed":7,
 "profile":{"blackhole":$blackhole,"loss_to_server":0,"loss_to_machine":0,
            "delay_each_way_ms":0,"rate_bits_per_sec":0,"max_queue_ms":0}}
JSON
  printf '%s\n' "$WORK/counters.json"
}

# One run of the runner, with every phase collapsed so a scenario that takes
# seven minutes on staging takes no time here. The durations are the scenario's
# own parameters, not a seam a test reaches through: the nightly leaves them at
# the values section 5 of the plan calibrates.
run_drill() {
  local scenario="$1"
  shift
  env \
    NAMESPACE="${NAMESPACE_OVERRIDE:-opengate-staging}" \
    PROBE_POD=drill-probe \
    SHAPER_POD=drill-shaper \
    SHAPER_URL=http://10.244.0.9:9091 \
    SERVER_URL=http://opengate-staging-server:8080 \
    DEVICE_ID=11111111-1111-1111-1111-111111111111 \
    API_TOKEN=stub-token \
    EVIDENCE_DIR="$WORK/evidence" \
    MEASUREMENTS_FILE="$WORK/measurements.jsonl" \
    NETDRILL_BASELINE_SECONDS=0 \
    NETDRILL_FAULT_SECONDS=0 \
    NETDRILL_RECOVERY_SECONDS=1 \
    NETDRILL_POLL_SECONDS=0 \
    KUBECTL_ARGS="$WORK/kubectl-args.txt" \
    "$@" \
    "$RUNNER" "$scenario" 2>&1
}

row_count() {
  [ -f "$WORK/measurements.jsonl" ] && wc -l <"$WORK/measurements.jsonl" || echo 0
}

reset_run() {
  rm -rf "$WORK/evidence" "$WORK/measurements.jsonl" "$WORK/kubectl-args.txt"
  : >"$WORK/kubectl-args.txt"
}

echo "== network-drill.sh: the guards =="

reset_run
out="$(NAMESPACE_OVERRIDE=opengate run_drill s1 || true)"
assert_contains "refuses any namespace but opengate-staging" "opengate-staging" "$out"
assert_eq "a refused namespace leaves no measurement" "0" \
  "$(row_count)"

reset_run
out="$(run_drill not-a-scenario \
  MOCK_DEVICES_FILE="$(online_device)" MOCK_METRICS_FILE="$(full_window)" \
  MOCK_COUNTERS_FILE="$(counters_file 40)" || true)"
assert_contains "refuses a scenario it does not have" "not-a-scenario" "$out"

reset_run
out="$(env -u DEVICE_ID PROBE_POD=p SHAPER_URL=u SERVER_URL=s NAMESPACE=opengate-staging \
  MEASUREMENTS_FILE="$WORK/measurements.jsonl" EVIDENCE_DIR="$WORK/evidence" \
  "$RUNNER" s1 2>&1 || true)"
assert_contains "refuses to run without the machine it is measuring" "DEVICE_ID" "$out"

echo "== network-drill.sh: a scenario that measured nothing =="

reset_run
out="$(run_drill s1 \
  MOCK_HEALTH_RC=7 \
  MOCK_DEVICES_FILE="$(online_device)" MOCK_METRICS_FILE="$(full_window)" \
  MOCK_COUNTERS_FILE="$(counters_file 40)" || echo "EXIT=$?")"
assert_contains "a shaper that does not answer is inconclusive" "inconclusive" "$out"
assert_contains "the inconclusive run exits 2, as the load harness does" "EXIT=2" "$out"
assert_eq "an inconclusive scenario emits no row at all" "0" \
  "$(row_count)"

reset_run
out="$(run_drill s1 \
  MOCK_IMPAIR_RC=1 \
  MOCK_DEVICES_FILE="$(online_device)" MOCK_METRICS_FILE="$(full_window)" \
  MOCK_COUNTERS_FILE="$(counters_file 40)" || echo "EXIT=$?")"
assert_contains "a refused impairment is inconclusive, not a failed product" "inconclusive" "$out"
assert_eq "a refused impairment emits no row" "0" \
  "$(row_count)"

reset_run
out="$(run_drill s1 \
  MOCK_DEVICES_FILE="$(online_device)" MOCK_METRICS_FILE="$(full_window)" \
  MOCK_COUNTERS_FILE="$(counters_file 0)" || echo "EXIT=$?")"
assert_contains "a blackhole that dropped nothing did not run" "inconclusive" "$out"
assert_eq "a scenario whose counters disagree with its instruction emits no row" "0" \
  "$(row_count)"

echo "== network-drill.sh: S1, the site goes dark and comes back =="

reset_run
out="$(run_drill s1 \
  MOCK_DEVICES_FILE="$(online_device)" MOCK_METRICS_FILE="$(full_window)" \
  MOCK_COUNTERS_FILE="$(counters_file 40)")"
rows="$(cat "$WORK/measurements.jsonl")"
args="$(cat "$WORK/kubectl-args.txt")"

assert_contains "S1 measures how long the machine took to come back" '"netdrill_reconnect_seconds"' "$rows"
assert_contains "S1 measures how much of the hole was filled" '"netdrill_gap_fill_ratio"' "$rows"
assert_contains "S1 measures how long the fill took" '"netdrill_backfill_complete_seconds"' "$rows"
assert_contains "S1 carries what the link discarded toward the server" \
  '"netdrill_shaper_dropped_to_server_total"' "$rows"
assert_contains "S1 carries what the link discarded toward the machine" \
  '"netdrill_shaper_dropped_to_machine_total"' "$rows"
# The shaper's drops belong to the link, not to either machine on it.
assert_contains "the link's own drops are attributed to the link" '"victim":"link"' "$rows"
assert_contains "every row names the scenario" '"scenario":"s1"' "$rows"
assert_contains "every row names the victim it measured" '"victim":"real"' "$rows"

# The phases have to happen in the order the scenario declares. A recovery
# commanded before the outage measures the baseline twice.
instructions() {
  grep -oE -- '--data \{[^}]*\}' "$WORK/kubectl-args.txt" | sed 's/^--data //'
}
order="$(instructions | head -3 | tr '\n' ' ')"
assert_eq "S1 commands pass, then darkness, then pass again" \
  '{} {"blackhole":true} {} ' "$order"

assert_contains "the evidence keeps the counters at every phase boundary" "seed" \
  "$(cat "$WORK/evidence"/s1-counters-*.json 2>/dev/null || echo)"

echo "== network-drill.sh: S1 with a hole nobody filled =="

reset_run
out="$(run_drill s1 \
  MOCK_DEVICES_FILE="$(online_device)" MOCK_METRICS_FILE="$(holed_window)" \
  MOCK_COUNTERS_FILE="$(counters_file 40)")"
rows="$(cat "$WORK/measurements.jsonl")"
assert_contains "an unfilled gap is still measured rather than withheld" '"netdrill_gap_fill_ratio"' "$rows"
assert_contains "the fill ratio is the share of buckets that came back" '"value":0.5' "$rows"

echo "== network-drill.sh: S3, the connection is up but bad =="

reset_run
out="$(run_drill s3 \
  MOCK_DEVICES_FILE="$(online_device)" MOCK_METRICS_FILE="$(full_window)" \
  MOCK_COUNTERS_FILE="$(counters_file 20)")"
rows="$(cat "$WORK/measurements.jsonl")"
assert_contains "S3 counts the times the machine crossed the offline line" \
  '"netdrill_offline_transitions"' "$rows"
assert_contains "S3 impairs one direction only" '"loss_to_server":0.2' \
  "$(cat "$WORK/kubectl-args.txt")"
assert_lacks "S3 leaves the direction the machine receives in alone" '"loss_to_machine":0.2' \
  "$(cat "$WORK/kubectl-args.txt")"

echo "== network-drill.sh: S4, a slow link and a new address =="

reset_run
out="$(run_drill s4 \
  MOCK_DEVICES_FILE="$(online_device)" MOCK_METRICS_FILE="$(full_window)" \
  MOCK_COUNTERS_FILE="$(counters_file 0)")"
rows="$(cat "$WORK/measurements.jsonl")"
args="$(cat "$WORK/kubectl-args.txt")"

assert_contains "S4 holds each datagram for a third of a second each way" '"delay_each_way_ms":300' "$args"
assert_contains "S4 moves the shaper to a new server-facing port" "/rebind" "$args"
assert_contains "S4 records whether the session survived the new address" \
  '"netdrill_session_survived"' "$rows"
# A failed migration and a broken link look identical from the outside: both
# end at the idle timeout. Recording the reconnect beside the survival is what
# tells them apart.
assert_contains "S4 records whether the machine reconnected instead" \
  '"netdrill_reconnected_after_rebind"' "$rows"

echo "== network-drill.sh: S2, the thin uplink =="

reset_run
out="$(run_drill s2 \
  MOCK_DEVICES_FILE="$(online_device)" MOCK_METRICS_FILE="$(full_window)" \
  MOCK_COUNTERS_FILE="$(counters_file 40)")"
rows="$(cat "$WORK/measurements.jsonl")"
args="$(cat "$WORK/kubectl-args.txt")"

assert_contains "S2 recovers over a 2 Mbit/s uplink" '"rate_bits_per_sec":2000000' "$args"
assert_contains "S2 states the depth the link buffers to" '"max_queue_ms"' "$args"
assert_contains "S2 measures the worst staleness of the live readings" \
  '"netdrill_live_staleness_max_seconds"' "$rows"
# Staleness measured from the restore would be the outage's own three minutes
# every night. It is measured from when the machine is back, so the scenario
# records that moment too.
assert_contains "S2 measures when the machine came back before watching it" \
  '"netdrill_reconnect_seconds"' "$rows"
assert_contains "S2 measures whether a machine went offline while catching up" \
  '"netdrill_offline_transitions"' "$rows"

echo "== network-drill.sh: the link is left clear =="

reset_run
out="$(run_drill s1 \
  MOCK_DEVICES_FILE="$(online_device)" MOCK_METRICS_FILE="$(full_window)" \
  MOCK_COUNTERS_FILE="$(counters_file 40)")"
last_impairment="$(instructions | tail -1)"
assert_eq "a scenario hands the link back clear" '{}' "$last_impairment"

# A run that ended badly must not leave the link impaired for the scenario after
# it, or for whatever else holds the namespace next.
reset_run
out="$(run_drill s1 \
  MOCK_DEVICES_RC=1 \
  MOCK_DEVICES_FILE="$(online_device)" MOCK_METRICS_FILE="$(full_window)" \
  MOCK_COUNTERS_FILE="$(counters_file 40)" || true)"
last_impairment="$(instructions | tail -1)"
assert_eq "a scenario that ended badly still hands the link back clear" \
  '{}' "$last_impairment"

echo "== netfault-shaper-pod.sh: the manifest =="

manifest="$(MACHINE=drill-shaper RELEASE=opengate-staging NODE_ARCH=arm64 \
  SERVER_POD_IP=10.244.0.3 SHAPER_SEED=7 "$POD_MANIFEST")"

assert_contains "the shaper is a pod in the namespace" "kind: Pod" "$manifest"
assert_contains "the shaper forwards to the server pod, not to itself" "10.244.0.3" "$manifest"
assert_contains "the shaper carries the run's seed" "-seed=7" "$manifest"
assert_contains "a shaper that dies stays dead and is visible" "restartPolicy: Never" "$manifest"
assert_contains "the shaper is pinned to the node's architecture" "kubernetes.io/arch: arm64" "$manifest"
assert_contains "the shaper runs as no one in particular" "runAsNonRoot: true" "$manifest"
assert_contains "the shaper holds no capability of any kind" "- ALL" "$manifest"
assert_lacks "the shaper is not privileged" "privileged: true" "$manifest"
assert_lacks "the shaper adds no capability" "add:" "$manifest"
assert_contains "the shaper cannot gain privilege" "allowPrivilegeEscalation: false" "$manifest"
assert_contains "the shaper asks the shared node for very little" "cpu: 50m" "$manifest"

echo "== network-drill.yml: what the nightly may and may not declare =="

if [ -f "$WORKFLOW" ]; then
  wf="$(cat "$WORKFLOW")"
  # The drill's own job must not declare the staging environment: its required
  # reviewers would leave a scheduled run waiting for a human who never comes.
  drill_job="$(awk '/^  network-drill:/,/^  [a-z-]+:$/' "$WORKFLOW")"
  assert_lacks "the drill job declares no staging environment" "environment: staging" "$drill_job"
  assert_contains "only the publishing job takes the observability environment" \
    "environment: observability" "$wf"
  assert_contains "the nightly runs after the load test, on the same lease" "cron: '0 6 * * *'" "$wf"
  assert_contains "the drill takes the staging namespace before touching it" \
    "staging-lease.sh acquire" "$wf"
  assert_contains "the drill gives the namespace back on every path" \
    "staging-lease.sh release" "$wf"
  # The workflow names the release and the namespace through its own variables,
  # so what is matched is the literal text in the file rather than the host it
  # expands to. The dollars are escaped so this shell leaves them alone.
  assert_contains "the machines enrol through the fully-qualified service name" \
    "\${RELEASE}-server.\${NAMESPACE}.svc.cluster.local:8080" "$wf"
  assert_contains "the drill resolves an agent binary rather than building one" \
    "build-image.yml" "$wf"
  assert_contains "every pod the drill created is removed whatever happened" "if: always()" "$wf"
else
  fail "missing workflow: $WORKFLOW"
fi

echo "== the reporting scripts =="

cat >"$WORK/rows.jsonl" <<'JSONL'
{"metric":"netdrill_reconnect_seconds","scenario":"s1","victim":"real","commit":"abc123","env":"ci","value":18}
{"metric":"netdrill_gap_fill_ratio","scenario":"s1","victim":"real","commit":"abc123","env":"ci","value":0.99}
{"metric":"netdrill_offline_transitions","scenario":"s3","victim":"real","commit":"abc123","env":"ci","value":0}
JSONL

summary="$("$SUMMARIZE" "$WORK/rows.jsonl")"
assert_contains "the summary carries every measurement it was given" "netdrill_reconnect_seconds" "$summary"
assert_eq "the summary carries no measurement it was not given" "3" "$(jq -r 'length' <<<"$summary")"
assert_contains "every row is stamped with when the run summarised it" '"timestamp"' "$summary"

# A night whose scenarios were all inconclusive has an empty file. That is a
# real outcome, and reporting it as an array of nothing would push a night that
# measured zero into a trend that cannot tell the two apart.
: >"$WORK/empty.jsonl"
if "$SUMMARIZE" "$WORK/empty.jsonl" >/dev/null 2>&1; then
  fail "a run that measured nothing was summarised as a run that measured zero"
else
  pass "a run that measured nothing is refused rather than summarised"
fi

if "$SUMMARIZE" "$WORK/no-such-file.jsonl" >/dev/null 2>&1; then
  fail "a missing measurements file was summarised anyway"
else
  pass "a missing measurements file is refused"
fi

printf '%s\n' "$summary" >"$WORK/summary.json"

# The push has to produce samples VictoriaMetrics will accept, carrying the two
# labels every trend series in this project is required to have.
KUBECTL_STDIN="$WORK/pushed.txt" "$VM_PUSH" "$WORK/summary.json" >/dev/null 2>&1 || true
push_out="$(cat "$WORK/pushed.txt" 2>/dev/null || echo)"
assert_contains "the push names the scenario each sample came from" 'scenario="s1"' "$push_out"
assert_contains "the push names the victim each sample measured" 'victim="real"' "$push_out"
assert_contains "every sample carries the commit label the transport requires" 'commit="abc123"' "$push_out"
assert_contains "every sample carries the env label the transport requires" 'env="ci"' "$push_out"

printf '[]\n' >"$WORK/summary-empty.json"
if "$VM_PUSH" "$WORK/summary-empty.json" >/dev/null 2>&1; then
  fail "an empty summary was pushed as a night's trend"
else
  pass "an empty summary is refused rather than pushed"
fi

# The floors hold from night one, before any window exists to compare against.
cat >"$WORK/regression-clean.json" <<'JSON'
[{"metric":"netdrill_reconnect_seconds","scenario":"s1","victim":"real","commit":"abc123","env":"ci","value":18},
 {"metric":"netdrill_gap_fill_ratio","scenario":"s1","victim":"real","commit":"abc123","env":"ci","value":0.99},
 {"metric":"netdrill_offline_transitions","scenario":"s3","victim":"real","commit":"abc123","env":"ci","value":0},
 {"metric":"netdrill_session_survived","scenario":"s4","victim":"real","commit":"abc123","env":"ci","value":1}]
JSON
out="$("$REGRESSION" "$WORK/regression-clean.json" 2>&1)" && rc=0 || rc=$?
assert_eq "a normal night is not a regression" "0" "${rc:-0}"
assert_contains "the check says which comparison it actually made" "absolute floors" "$out"

cat >"$WORK/regression-slow.json" <<'JSON'
[{"metric":"netdrill_reconnect_seconds","scenario":"s1","victim":"real","commit":"abc123","env":"ci","value":400}]
JSON
out="$("$REGRESSION" "$WORK/regression-slow.json" 2>&1)" && rc=0 || rc=$?
assert_eq "a machine that took too long to come back is a regression" "1" "${rc:-0}"
assert_contains "the regression says what a customer would have seen" "come back on its own" "$out"

cat >"$WORK/regression-hole.json" <<'JSON'
[{"metric":"netdrill_gap_fill_ratio","scenario":"s1","victim":"real","commit":"abc123","env":"ci","value":0.4}]
JSON
out="$("$REGRESSION" "$WORK/regression-hole.json" 2>&1)" && rc=0 || rc=$?
assert_eq "a hole that did not fill is a regression" "1" "${rc:-0}"

cat >"$WORK/regression-churn.json" <<'JSON'
[{"metric":"netdrill_offline_transitions","scenario":"s3","victim":"real","commit":"abc123","env":"ci","value":2}]
JSON
out="$("$REGRESSION" "$WORK/regression-churn.json" 2>&1)" && rc=0 || rc=$?
assert_eq "a machine that churned on a lossy link is a regression" "1" "${rc:-0}"

# S1's outage is meant to take the machine offline, so crossing the line there
# is the scenario working rather than the product failing.
cat >"$WORK/regression-s1-offline.json" <<'JSON'
[{"metric":"netdrill_offline_transitions","scenario":"s1","victim":"real","commit":"abc123","env":"ci","value":1}]
JSON
out="$("$REGRESSION" "$WORK/regression-s1-offline.json" 2>&1)" && rc=0 || rc=$?
assert_eq "going offline during the outage scenario is not a regression" "0" "${rc:-0}"

cat >"$WORK/regression-migration.json" <<'JSON'
[{"metric":"netdrill_session_survived","scenario":"s4","victim":"real","commit":"abc123","env":"ci","value":0}]
JSON
out="$("$REGRESSION" "$WORK/regression-migration.json" 2>&1)" && rc=0 || rc=$?
assert_eq "a session that did not survive a new address is a regression" "1" "${rc:-0}"
assert_contains "the migration regression says what it costs a customer" "rebooting router" "$out"

if "$REGRESSION" "$WORK/no-such-summary.json" >/dev/null 2>&1; then
  fail "a missing summary was checked anyway"
else
  pass "a missing summary is refused rather than passed"
fi

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
