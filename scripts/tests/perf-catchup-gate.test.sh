#!/usr/bin/env bash
# Tests scripts/perf-catchup-gate.sh: the store probe's readings in a bundle reported against the
# go/no-go's marks, each missed mark named, and a bundle with no probe never read as a reading.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
GATE="$ROOT/scripts/perf-catchup-gate.sh"

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

# bundle_with <jq-edit> writes a bundle whose probe passes every mark, edited by the jq program.
bundle_with() {
  jq -n '{
    store_probe: {
      oom_killed: false,
      quiet: {seconds: 60, cpu_share: 0.12, memory_share: 0.31},
      one_sender: {senders: 1, duration_seconds: 900,
        catch_up: {requests: 5000, readings: 9450000, failed: 0, readings_per_second: 10500},
        live: {requests: 900, readings: 1800000, within_five_seconds: 899, p99_ms: 420},
        store: {peak_memory_bytes: 400000000, memory_limit_bytes: 536870912, restarted: false}},
      two_senders: {senders: 2, duration_seconds: 900,
        catch_up: {requests: 7000, readings: 12600000, failed: 0, readings_per_second: 14000},
        live: {requests: 900, readings: 1800000, within_five_seconds: 860, p99_ms: 6100},
        store: {peak_memory_bytes: 500000000, memory_limit_bytes: 536870912, restarted: false}}
    }} | '"$1" >"$WORK/bundle.json"
}

judge() {
  status=0
  "$GATE" "$WORK/bundle.json" >"$WORK/out" 2>&1 || status=$?
}
out_has() { grep -qF -- "$1" "$WORK/out"; }

echo "catch-up go/no-go:"

bundle_with '.'
judge
if [ "$status" -eq 0 ] && out_has 'store probe: the store took 10500 readings a second' && ! out_has 'below its mark'; then
  pass "a store past every mark is reported with its rate named"
else
  fail "a passing probe must be reported clean (status=$status out=[$(cat "$WORK/out")])"
fi
if out_has 'two senders: 14000 readings a second'; then
  pass "the two-sender figure is published beside it for the choice it informs"
else
  fail "the two-sender figure must be published (out=[$(cat "$WORK/out")])"
fi

for case in \
  '.store_probe.one_sender.catch_up.readings_per_second = 9999|10000 readings a second' \
  '.store_probe.one_sender.duration_seconds = 600|15 minutes' \
  '.store_probe.oom_killed = true|ran out of memory' \
  '.store_probe.one_sender.store.restarted = true|started again' \
  '.store_probe.one_sender.store.stopped_answering = true|stopped answering with one sender' \
  '.store_probe.two_senders.store.stopped_answering = true|stopped answering with two senders' \
  '.store_probe.one_sender = {senders: 1, duration_seconds: 0, request_bytes: 262144, catch_up: {requests: 0, readings: 0, failed: 0, readings_per_second: 0}, live: {requests: 0, readings: 0, within_five_seconds: 0, p99_ms: 0}, store: {peak_memory_bytes: 0, memory_limit_bytes: 536870912, restarted: false, stopped_answering: true}}|stopped answering with one sender' \
  '.store_probe.one_sender.live.within_five_seconds = 890|99%' \
  '.store_probe.quiet.cpu_share = 0.5|half its processor' \
  '.store_probe.quiet.memory_share = 0.51|half its memory'; do
  edit="${case%%|*}"
  named="${case#*|}"
  bundle_with "$edit"
  judge
  if [ "$status" -eq 0 ] && out_has "$named" && out_has 'below its mark'; then
    pass "a missed mark is named and fails nothing: $named"
  else
    fail "$edit must be reported naming '$named' and exit 0 (status=$status out=[$(cat "$WORK/out")])"
  fi
done

bundle_with '.store_probe.one_sender = {}'
judge
if [ "$status" -eq 2 ] && out_has 'holds no store probe reading'; then
  pass "a probe run that wrote nothing is unreadable, never a go"
else
  fail "a probe run that wrote nothing must exit 2 (status=$status out=[$(cat "$WORK/out")])"
fi

bundle_with 'del(.store_probe)'
judge
if [ "$status" -eq 2 ]; then
  pass "a bundle with no probe reading is unreadable, never a go"
else
  fail "a bundle with no probe must exit 2 (status=$status)"
fi

status=0
"$GATE" >"$WORK/out" 2>&1 || status=$?
if [ "$status" -eq 2 ] && out_has 'Usage'; then
  pass "a gate named no bundle prints its usage"
else
  fail "a gate with no bundle must exit 2 (status=$status)"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
