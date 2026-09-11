#!/usr/bin/env bash
# Tests for scripts/loadtest-bundle-merge.sh — the readings taken beside a run
# reaching the run's own evidence.
#
# Two numbers a bundle declares are measured by steps other than the harness:
# the fleet's weight on disk, read from the database after the fleet exists, and
# the technician journeys, timed by a generator in another pod. Each wrote its
# figure into a file of its own that no later reader opened, so the one artifact
# that outlives the metrics store carried a null where the family's whole
# finding belongs.
#
# A merge that found nothing to merge must fail. Reporting success for work that
# did not happen is the shape this exists to close.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
MERGE="$REPO_ROOT/scripts/loadtest-bundle-merge.sh"
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
    schema_version: 8,
    fixture: { size: "lopsided", devices: 500 },
    journeys: null,
    verdict: { result: "valid" }
  }' >"$WORK/bundle.json"
}

fresh_weight() {
  jq -n '{ fixture_bytes: 1398101, telemetry_series: 4096 }' >"$WORK/weight.json"
}

fresh_export() {
  jq -n '{
    metrics: {
      journey_device_list_ms:   { type: "trend", values: { med: 41.0, "p(95)": 88.5, count: 1200 } },
      journey_device_detail_ms: { type: "trend", values: { med: 60.0, "p(95)": 140.25, count: 600 } },
      http_req_duration:        { type: "trend", values: { med: 7.0, "p(95)": 12.0, count: 7228 } }
    }
  }' >"$WORK/export.json"
}

run_merge() {
  STATUS=0
  "$MERGE" "$@" >"$WORK/out.txt" 2>"$WORK/err.txt" || STATUS=$?
}

echo "loadtest-bundle-merge:"

# The fleet's weight reaches the evidence.
fresh_bundle
fresh_weight
run_merge "$WORK/bundle.json" --weight "$WORK/weight.json"
assert_eq "a weighing merges" "0" "$STATUS"
assert_eq "the database size lands in the fixture" "1398101" \
  "$(jq -r '.fixture.database_bytes' "$WORK/bundle.json")"
assert_eq "the series count lands beside it" "4096" \
  "$(jq -r '.fixture.telemetry_series' "$WORK/bundle.json")"
assert_eq "nothing else in the fixture is disturbed" "500" \
  "$(jq -r '.fixture.devices' "$WORK/bundle.json")"

# The journeys reach the evidence, and only the journeys.
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

# The shape the pinned exporter actually writes.
#
# k6 v1.x puts a trend statistic flat on the metric; the fixture above is v0.x,
# which nests them under "values". Reading only the nested shape is not a wrong
# number, it is three zeros — every field falls back to nought — so every bundle
# the nightly produced declared that opening a fleet list, opening a machine and
# sending an instruction each took no time at all. The fixture that should have
# caught it was written in the shape the reader reads rather than the shape the
# exporter writes.
fresh_bundle
jq -n '{
  metrics: {
    "journey_device_list_ms":        { "p(50)": 41.0, med: 41.0, "p(95)": 88.5, count: 1200 },
    "journey_device_list_ms{phase:steady}": { "p(50)": 39.0, med: 39.0, "p(95)": 80.0, count: 900 },
    "journey_device_detail_ms":      { "p(50)": 60.0, med: 60.0, "p(95)": 140.25, count: 600 },
    "http_req_duration":             { "p(50)": 7.0, med: 7.0, "p(95)": 12.0, count: 7228 }
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

# Both at once, which is what the volume family's job does.
fresh_bundle
fresh_weight
fresh_export
run_merge "$WORK/bundle.json" --weight "$WORK/weight.json" --journeys "$WORK/export.json"
assert_eq "both merge together" "0" "$STATUS"
assert_eq "the weighing survives the journeys" "1398101" \
  "$(jq -r '.fixture.database_bytes' "$WORK/bundle.json")"
assert_eq "the journeys survive the weighing" "2" \
  "$(jq -r '.journeys | length' "$WORK/bundle.json")"

# A merge with nothing to merge is a step reporting success for no work.
fresh_bundle
run_merge "$WORK/bundle.json"
assert_eq "naming nothing to merge is refused" "2" "$STATUS"

# An absent bundle is not a bundle with nothing to add.
rm -f "$WORK/bundle.json"
fresh_weight
run_merge "$WORK/bundle.json" --weight "$WORK/weight.json"
assert_eq "an absent bundle fails" "1" "$STATUS"

# A weighing that never happened must not reach the evidence as a zero — zero
# bytes is the emptiest fixture ever built.
fresh_bundle
: >"$WORK/weight.json"
run_merge "$WORK/bundle.json" --weight "$WORK/weight.json"
assert_eq "an empty weighing fails" "1" "$STATUS"
assert_eq "and the bundle is left as it was" "null" \
  "$(jq -r '.fixture.database_bytes // "null"' "$WORK/bundle.json")"

# An export carrying no journeys is a generator that timed no screens, which is
# the absence this merge exists to make visible.
fresh_bundle
jq -n '{ metrics: { http_req_duration: { type: "trend", values: { med: 7.0 } } } }' >"$WORK/export.json"
run_merge "$WORK/bundle.json" --journeys "$WORK/export.json"
assert_eq "an export naming no journeys fails" "1" "$STATUS"

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf 'Failures:\n' >&2
  for f in "${FAILURES[@]}"; do printf '  - %s\n' "$f" >&2; done
  exit 1
fi
