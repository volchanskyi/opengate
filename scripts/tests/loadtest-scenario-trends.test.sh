#!/usr/bin/env bash
# A k6 scenario's trends must be fed on the path the scenario actually takes.
#
# The load-test gate reads its series out of these trends, so a trend fed from a
# branch the scenario rarely enters, or from a callback that never fires, gates
# nothing while reading as a passing gate forever. The two invariants below are
# each a case of that, and each spans files that do not reference each other, so
# nothing but a test can hold them.
#
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

# --- The relay echo must arrive at a handler that exists ---
#
# Three gate ceilings are named after k6/relay-throughput/relay, and that series
# is filled from the relay_msg_latency_ms trend the scenario records when its own
# frame comes back through the machine. k6 dispatches a frame by type, and the
# server's relay writes every frame it pipes as binary — the agent protocol is
# MessagePack, so a byte pipe is what the relay is. Register only the text
# handler and nothing errors: the frame arrives, ws_msgs_received counts it, and
# the trend stays empty while three ceilings sit on the zero it reports. A night
# measured 200 sessions and 200 frames back, recorded no round trip at all, and
# reported a throughput collapse — every iteration having burned its whole echo
# timeout waiting for an answer that had already arrived.
#
# What the relay writes to each side. The adapter is the only place in the
# server that names a WebSocket frame type, so it is the whole answer.
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

# The round trip is only recorded from inside a frame handler. A trend fed from
# anywhere else is measuring something other than the relay, which is the defect
# the scenario's own ceilings exist to catch.
if grep -qE 'relayMsgLatency\.add' "$SCENARIO"; then
  pass "the scenario records a round trip"
else
  fail "the scenario records no relay_msg_latency_ms sample — the gated series cannot move"
fi

# Every handler that can receive the echo must feed the trend. One that closes
# the socket without recording turns a healthy round trip into a timeout.
HANDLERS="$(grep -cE "socket\.on\(\s*[\"'](message|binaryMessage)[\"']" "$SCENARIO" || true)"
RECORDERS="$(grep -cE 'relayMsgLatency\.add' "$SCENARIO" || true)"
if [ "${HANDLERS:-0}" -ge 1 ] && [ "${RECORDERS:-0}" -ge 1 ]; then
  pass "every frame handler leads to a recorded round trip ($HANDLERS handler(s), $RECORDERS recorder(s))"
else
  fail "frame handlers and round-trip recorders do not line up ($HANDLERS handler(s), $RECORDERS recorder(s))"
fi

# --- A journey timed against a machine must be given a site that has one ---
#
# api-baseline reads the fleet narrowed to a site, and times a machine's page and
# an instruction to it from whatever that read returned. Narrowing to whichever
# site happens to be first asks an arbitrary one of seven for its machines, so on
# a night when that site holds none, the read is legitimately empty and both
# journeys are silently not timed. device_detail and command_accept published a
# flat zero on five of six nights and a real figure on the sixth — which is not a
# fast night, it is the one night the arbitrary choice landed.
#
# The site the fleet is read from must therefore be chosen for holding machines.
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

# Both journeys are timed inside the branch that needs a machine, so a scenario
# that reaches that branch must record both. A branch entered without recording
# is the zero these trends have been publishing.
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
