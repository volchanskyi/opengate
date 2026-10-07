#!/usr/bin/env bash
# A k6 scenario's trends are fed on the path the scenario takes.
# Run: ./scripts/tests/loadtest-scenario-trends.test.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
WSCONN="$REPO_ROOT/server/internal/api/wsconn.go"
SCENARIO="$REPO_ROOT/load/k6/scenarios/relay-throughput.js"
BASELINE="$REPO_ROOT/load/k6/scenarios/api-baseline.js"
SESSION_LIB="$REPO_ROOT/load/k6/lib/session.js"

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

echo "k6 scenario trend reachability:"

for f in "$WSCONN" "$SCENARIO" "$BASELINE" "$SESSION_LIB"; do
  if [ ! -f "$f" ]; then
    fail "missing file: $f"
    printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
    exit 1
  fi
done

# The adapter is the only place in the server that names a WebSocket frame type.
if grep -qE 'websocket\.MessageBinary' "$WSCONN"; then
  pass "the relay writes binary frames"
  RELAY_WRITES_BINARY=1
else
  RELAY_WRITES_BINARY=0
  if grep -qE 'websocket\.MessageText' "$WSCONN"; then
    pass "the relay writes text frames"
  else
    fail "the relay adapter names no frame type — this test cannot tell what the scenario must listen for"
  fi
fi

# k6's k6/ws module dispatches a binary frame to "binaryMessage" and everything
# else to "message". The handler the scenario registers has to match.
handler_registered() {
  grep -qE "socket\.on\(\s*[\"']$1[\"']" "$SCENARIO"
}

if [ "$RELAY_WRITES_BINARY" = "1" ]; then
  if handler_registered binaryMessage; then
    pass "the scenario handles the binary echo the relay sends"
  else
    fail "the relay writes binary but the scenario registers no binaryMessage handler — the echo is never seen and relay_msg_latency_ms stays zero"
  fi
else
  if handler_registered message; then
    pass "the scenario handles the text echo the relay sends"
  else
    fail "the relay writes text but the scenario registers no message handler"
  fi
fi

if grep -qE 'relayMsgLatency\.add' "$SCENARIO"; then
  pass "the scenario records a round trip"
else
  fail "the scenario records no relay_msg_latency_ms sample — the gated series cannot move"
fi

HANDLERS="$(grep -cE "socket\.on\(\s*[\"'](message|binaryMessage)[\"']" "$SCENARIO" || true)"
RECORDERS="$(grep -cE 'relayMsgLatency\.add' "$SCENARIO" || true)"
if [ "${HANDLERS:-0}" -ge 1 ] && [ "${RECORDERS:-0}" -ge 1 ]; then
  pass "every frame handler leads to a recorded round trip ($HANDLERS handler(s), $RECORDERS recorder(s))"
else
  fail "frame handlers and round-trip recorders do not line up ($HANDLERS handler(s), $RECORDERS recorder(s))"
fi

# An empty site leaves both machine journeys untimed, so the site is chosen for holding machines.
if grep -qE 'siteIds\[0\]' "$BASELINE"; then
  fail "api-baseline narrows to an arbitrary site (siteIds[0]) — the journeys it times are recorded only when that site happens to hold machines"
else
  pass "api-baseline does not narrow the fleet to an arbitrary site"
fi

if grep -qE 'siteWithDevices' "$SESSION_LIB" && grep -qE 'siteWithDevices' "$BASELINE"; then
  pass "api-baseline reads the fleet from a site chosen for holding machines"
else
  fail "api-baseline has no way to choose a site that holds machines — the journey trends stay a matter of luck"
fi

for trend in deviceDetailLatency commandAcceptLatency; do
  if grep -qE "$trend\.add" "$BASELINE"; then
    pass "api-baseline records $trend"
  else
    fail "api-baseline declares $trend and never records it"
  fi
done

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
