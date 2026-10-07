#!/usr/bin/env bash
# Tests for scripts/loadtest-bundle-merge.sh, which folds the readings taken beside a run into
# the run's evidence bundle and fails when it finds nothing to merge.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
MERGE="$REPO_ROOT/scripts/loadtest-bundle-merge.sh"
WEIGH="$REPO_ROOT/scripts/perf-weigh-fixture.sh"
CLEANUP="$REPO_ROOT/scripts/loadtest-cleanup.sh"
GOLDEN_WEIGHT="$SCRIPT_DIR/fixtures/fixture-weight.json"
[ -x "$MERGE" ] || {
  echo "FAIL: $MERGE not executable" >&2
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

fresh_bundle() {
  jq -n '{
    schema_version: 11,
    fixture: { size: "lopsided", devices: 500 },
    journeys: null,
    verdict: { result: "valid" }
  }' >"$WORK/bundle.json"
}

# The weighing the merge reads comes from the weighing script, run against a stand-in database
# and metrics store.
STUB="$WORK/stub"
STACK="$WORK/stack"
mkdir -p "$STUB" "$STACK"
cat >"$STUB/docker" <<'STUB_DOCKER'
#!/usr/bin/env bash
# docker exec <container> true  |  docker exec <container> psql ... -tAc <query>
set -euo pipefail
[ "$1" = exec ] || exit 1
[ "$3" = true ] && exit 0
query="${*: -1}"
case "$query" in
  *pg_database_size*) cat "$STUB_STACK/database_bytes" ;;
  *"FROM devices)"*) cat "$STUB_STACK/devices" ;;
  *"FROM sites)"*) cat "$STUB_STACK/sites" ;;
  *"FROM users)"*) cat "$STUB_STACK/users" ;;
  *"FROM device_processes)"*) cat "$STUB_STACK/process_rows" ;;
  *)
    echo "stand-in docker: unexpected query: $query" >&2
    exit 1
    ;;
esac
STUB_DOCKER
cat >"$STUB/curl" <<'STUB_CURL'
#!/usr/bin/env bash
# The stack's metrics store, answering how many series it holds.
set -euo pipefail
case "${*: -1}" in
  */api/v1/series/count) printf '{"status":"success","data":[%s]}\n' "$(cat "$STUB_STACK/series")" ;;
  *)
    echo "curl: (22) The requested URL returned error: 404" >&2
    exit 22
    ;;
esac
STUB_CURL
chmod +x "$STUB/docker" "$STUB/curl"
export STUB_STACK="$STACK"

# stack_holds <database-bytes> <series> <devices> <sites> <users> <process-rows>
stack_holds() {
  printf '%s' "$1" >"$STACK/database_bytes"
  printf '%s' "$2" >"$STACK/series"
  printf '%s' "$3" >"$STACK/devices"
  printf '%s' "$4" >"$STACK/sites"
  printf '%s' "$5" >"$STACK/users"
  printf '%s' "$6" >"$STACK/process_rows"
}

fresh_weight() {
  stack_holds 9901747 12 0 0 0 0
  PATH="$STUB:$PATH" bash "$WEIGH" baseline "$WORK/baseline.json" >/dev/null
  stack_holds 25065139 192012 8000 38 23 23934
  PATH="$STUB:$PATH" bash "$WEIGH" weigh "$WORK/baseline.json" "$WORK/weight.json" >/dev/null
}

fresh_proof() {
  local users="$1" devices="$2" organizations="$3" sites="$4"
  cat >"$WORK/psql" <<PSQL
#!/usr/bin/env bash
query=""
while [ "\$#" -gt 0 ]; do
  case "\$1" in
    -tAc) query="\$2"; shift 2 ;;
    *) shift ;;
  esac
done
[ -n "\$query" ] || { cat >/dev/null; exit 0; }
case "\$query" in
  *"FROM users"*) echo $users ;;
  *"FROM devices"*) echo $devices ;;
  *"FROM organizations"*) echo $organizations ;;
  *"FROM sites"*) echo $sites ;;
esac
PSQL
  chmod +x "$WORK/psql"
  LOADTEST_PSQL="$WORK/psql" "$CLEANUP" "$WORK/cleanup.json" >/dev/null 2>&1 || true
}

fresh_export() {
  jq -n '{
    metrics: {
      journey_device_list_ms:   { type: "trend", values: { med: 41.0, "p(95)": 88.5, count: 1200 } },
      journey_device_detail_ms: { type: "trend", values: { med: 60.0, "p(95)": 140.25, count: 600 } },
      http_req_duration:        { type: "trend", values: { med: 7.0, "p(95)": 12.0, count: 7228 } },
      http_reqs:                { type: "counter", values: { count: 7228, rate: 40.1 } },
      requests_refused:         { type: "counter", values: { count: 0, rate: 0 } }
    }
  }' >"$WORK/export.json"
}

run_merge() {
  STATUS=0
  "$MERGE" "$@" >"$WORK/out.txt" 2>"$WORK/err.txt" || STATUS=$?
}

echo "loadtest-bundle-merge:"

fresh_bundle
fresh_weight
run_merge "$WORK/bundle.json" --weight "$WORK/weight.json"
assert_eq "a weighing merges" "0" "$STATUS"
assert_eq "the fixture's own weight lands in the fixture" "15163392" \
  "$(jq -r '.fixture.database_bytes' "$WORK/bundle.json")"
assert_eq "the series the fleet occupies in the metrics store land beside it" "192000" \
  "$(jq -r '.fixture.telemetry_series' "$WORK/bundle.json")"
assert_eq "nothing else in the fixture is disturbed" "500" \
  "$(jq -r '.fixture.devices' "$WORK/bundle.json")"
assert_eq "the process snapshot rows keep a name of their own" "23934" \
  "$(jq -r '.counts.process_rows' "$WORK/weight.json")"

# The golden weighing the harness's reader is tested against has to keep the shape the weighing
# script writes.
producer_shape="$(jq -c '[paths(scalars) | map(tostring) | join(".")] | sort' "$WORK/weight.json")"
golden_shape="$(jq -c '[paths(scalars) | map(tostring) | join(".")] | sort' "$GOLDEN_WEIGHT")"
assert_eq "the golden weighing is the shape the weighing script writes" "$producer_shape" "$golden_shape"

fresh_bundle
run_merge "$WORK/bundle.json" --weight "$GOLDEN_WEIGHT"
assert_eq "the golden weighing's series count reaches the bundle" \
  "$(jq -r '.counts.telemetry_series' "$GOLDEN_WEIGHT")" \
  "$(jq -r '.fixture.telemetry_series' "$WORK/bundle.json")"

fresh_bundle
fresh_export
run_merge "$WORK/bundle.json" --journeys "$WORK/export.json"
assert_eq "an export merges" "0" "$STATUS"
assert_eq "only the named journeys are carried" "2" \
  "$(jq -r '.journeys | length' "$WORK/bundle.json")"
assert_eq "they are carried in name order" "device-detail" \
  "$(jq -r '.journeys[0].name' "$WORK/bundle.json")"
assert_eq "each carries its own tail" "88.5" \
  "$(jq -r '.journeys[] | select(.name == "device-list") | .latency_p95_ms' "$WORK/bundle.json")"
assert_eq "each carries how many requests timed it" "1200" \
  "$(jq -r '.journeys[] | select(.name == "device-list") | .requests' "$WORK/bundle.json")"

# k6 v1.x puts a trend statistic flat on the metric; the fixture above is v0.x, which nests
# them under "values".
fresh_bundle
jq -n '{
  metrics: {
    "journey_device_list_ms":        { "p(50)": 41.0, med: 41.0, "p(95)": 88.5, count: 1200 },
    "journey_device_list_ms{phase:steady}": { "p(50)": 39.0, med: 39.0, "p(95)": 80.0, count: 900 },
    "journey_device_detail_ms":      { "p(50)": 60.0, med: 60.0, "p(95)": 140.25, count: 600 },
    "http_req_duration":             { "p(50)": 7.0, med: 7.0, "p(95)": 12.0, count: 7228 },
    "http_reqs":                     { count: 7228, rate: 40.1 },
    "requests_refused":              { count: 0, rate: 0 }
  }
}' >"$WORK/export.json"
run_merge "$WORK/bundle.json" --journeys "$WORK/export.json"
assert_eq "an export in the pinned exporter shape merges" "0" "$STATUS"
assert_eq "a flat statistic is a reading, not a nought" "140.25" \
  "$(jq -r '.journeys[] | select(.name == "device-detail") | .latency_p95_ms' "$WORK/bundle.json")"
assert_eq "and so is how many times the screen was opened" "600" \
  "$(jq -r '.journeys[] | select(.name == "device-detail") | .requests' "$WORK/bundle.json")"
assert_eq "the measured phase wins over the whole run" "80.0" \
  "$(jq -r '.journeys[] | select(.name == "device-list") | .latency_p95_ms' "$WORK/bundle.json")"
assert_eq "a phase-tagged copy is not a second journey" "2" \
  "$(jq -r '.journeys | length' "$WORK/bundle.json")"

fresh_bundle
fresh_weight
fresh_export
run_merge "$WORK/bundle.json" --weight "$WORK/weight.json" --journeys "$WORK/export.json"
assert_eq "both merge together" "0" "$STATUS"
assert_eq "the weighing survives the journeys" "15163392" \
  "$(jq -r '.fixture.database_bytes' "$WORK/bundle.json")"
assert_eq "the journeys survive the weighing" "2" \
  "$(jq -r '.journeys | length' "$WORK/bundle.json")"

fresh_bundle
jq -n '{
  metrics: {
    "relay_msg_latency_ms": { "p(50)": 12.0, med: 12.0, "p(95)": 44.5, count: 54000 },
    "http_req_duration":    { "p(50)": 4.9, med: 4.9, "p(95)": 9.0, count: 108000 },
    "http_reqs":            { count: 108000, rate: 6.0 },
    "requests_refused":     { count: 12, rate: 0.0007 }
  }
}' >"$WORK/relay.json"
run_merge "$WORK/bundle.json" --journeys "$WORK/relay.json"
assert_eq "a session export merges" "0" "$STATUS"
assert_eq "the session round trip is carried" "relay-session-echo" \
  "$(jq -r '.journeys[0].name' "$WORK/bundle.json")"
assert_eq "it carries its own tail" "44.5" \
  "$(jq -r '.journeys[0].latency_p95_ms' "$WORK/bundle.json")"
assert_eq "and how many sessions timed it" "54000" \
  "$(jq -r '.journeys[0].requests' "$WORK/bundle.json")"

fresh_bundle
fresh_export
run_merge "$WORK/bundle.json" --journeys "$WORK/export.json" --journeys "$WORK/relay.json"
assert_eq "two exports merge together" "0" "$STATUS"
assert_eq "both scenarios' journeys are carried" "3" \
  "$(jq -r '.journeys | length' "$WORK/bundle.json")"
assert_eq "they are carried in name order" "device-detail device-list relay-session-echo" \
  "$(jq -r '[.journeys[].name] | join(" ")' "$WORK/bundle.json")"
assert_eq "the screens survive the sessions" "88.5" \
  "$(jq -r '.journeys[] | select(.name == "device-list") | .latency_p95_ms' "$WORK/bundle.json")"

fresh_bundle
fresh_export
: >"$WORK/relay.json"
run_merge "$WORK/bundle.json" --journeys "$WORK/export.json" --journeys "$WORK/relay.json"
assert_eq "one empty export of two fails" "1" "$STATUS"
assert_eq "and the bundle is left as it was" "null" \
  "$(jq -r '.journeys // "null" | if type == "array" then "array" else . end' "$WORK/bundle.json")"

fresh_bundle
run_merge "$WORK/bundle.json"
assert_eq "naming nothing to merge is refused" "2" "$STATUS"

rm -f "$WORK/bundle.json"
fresh_weight
run_merge "$WORK/bundle.json" --weight "$WORK/weight.json"
assert_eq "an absent bundle fails" "1" "$STATUS"

fresh_bundle
: >"$WORK/weight.json"
run_merge "$WORK/bundle.json" --weight "$WORK/weight.json"
assert_eq "an empty weighing fails" "1" "$STATUS"
assert_eq "and the bundle is left as it was" "null" \
  "$(jq -r '.fixture.database_bytes // "null"' "$WORK/bundle.json")"

fresh_bundle
jq -n '{ metrics: {
  http_req_duration: { type: "trend", values: { med: 7.0 } },
  http_reqs:         { type: "counter", values: { count: 10 } },
  requests_refused:  { type: "counter", values: { count: 0 } }
} }' >"$WORK/export.json"
run_merge "$WORK/bundle.json" --journeys "$WORK/export.json"
assert_eq "an export naming no journeys fails" "1" "$STATUS"

fresh_bundle
fresh_export
run_merge "$WORK/bundle.json" --journeys "$WORK/export.json"
assert_eq "a night nobody was refused says so" "0" \
  "$(jq -r '.refusals.refused' "$WORK/bundle.json")"
assert_eq "beside how many requests it made" "7228" \
  "$(jq -r '.refusals.requests' "$WORK/bundle.json")"

fresh_bundle
fresh_export
jq -n '{
  metrics: {
    "relay_msg_latency_ms": { "p(50)": 12.0, med: 12.0, "p(95)": 44.5, count: 54000 },
    "http_reqs":            { count: 108000, rate: 6.0 },
    "requests_refused":     { count: 12, rate: 0.0007 }
  }
}' >"$WORK/relay.json"
run_merge "$WORK/bundle.json" --journeys "$WORK/export.json" --journeys "$WORK/relay.json"
assert_eq "two exports merge together" "0" "$STATUS"
assert_eq "every scenario's refusals are counted" "12" \
  "$(jq -r '.refusals.refused' "$WORK/bundle.json")"
assert_eq "against every scenario's requests" "115228" \
  "$(jq -r '.refusals.requests' "$WORK/bundle.json")"

# A counter nobody incremented is absent from the export, so the scenarios add zero on every
# answered request to keep the series present.
fresh_bundle
jq -n '{
  metrics: {
    "journey_device_list_ms": { med: 41.0, "p(95)": 88.5, count: 1200 },
    "http_reqs":              { count: 7228, rate: 40.1 }
  }
}' >"$WORK/uncounted.json"
run_merge "$WORK/bundle.json" --journeys "$WORK/uncounted.json"
assert_eq "an export that counted no refusals at all fails" "1" "$STATUS"
assert_eq "and the bundle is left as it was" "null" \
  "$(jq -r '.refusals // "null" | if type == "object" then "object" else . end' "$WORK/bundle.json")"

fresh_bundle
jq -n '{
  metrics: {
    "journey_device_list_ms": { med: 41.0, "p(95)": 88.5, count: 1200 },
    "http_reqs":              { count: 0, rate: 0 },
    "requests_refused":       { count: 0, rate: 0 }
  }
}' >"$WORK/silent.json"
run_merge "$WORK/bundle.json" --journeys "$WORK/silent.json"
assert_eq "an export naming no request at all fails" "1" "$STATUS"

fresh_bundle
fresh_proof 0 0 0 0
run_merge "$WORK/bundle.json" --cleanup "$WORK/cleanup.json"
assert_eq "a cleanup proof merges" "0" "$STATUS"
assert_eq "the bundle says the cleanup was counted" "true" \
  "$(jq -r '.cleanup.verified' "$WORK/bundle.json")"
assert_eq "with the four kinds the cleanup counts" \
  "orphan_devices orphan_organizations orphan_sites orphan_users verified" \
  "$(jq -r '.cleanup | keys | join(" ")' "$WORK/bundle.json")"

fresh_bundle
fresh_proof 3 2 1 4
run_merge "$WORK/bundle.json" --cleanup "$WORK/cleanup.json"
assert_eq "a proof of residue merges" "0" "$STATUS"
assert_eq "and the bundle comes out unclean" "3 2 1 4" \
  "$(jq -r '.cleanup | "\(.orphan_users) \(.orphan_devices) \(.orphan_organizations) \(.orphan_sites)"' "$WORK/bundle.json")"

fresh_bundle
: >"$WORK/cleanup.json"
run_merge "$WORK/bundle.json" --cleanup "$WORK/cleanup.json"
assert_eq "an empty proof fails" "1" "$STATUS"
assert_eq "and the bundle is left as it was" "null" \
  "$(jq -r '.cleanup // "null" | if type == "object" then "object" else . end' "$WORK/bundle.json")"

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf 'Failures:\n' >&2
  for f in "${FAILURES[@]}"; do printf '  - %s\n' "$f" >&2; done
  exit 1
fi
