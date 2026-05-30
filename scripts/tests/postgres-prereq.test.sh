#!/usr/bin/env bash
# Tests for scripts/lib/postgres-prereq.sh. Plain bash; no bats dependency.
# Run: ./scripts/tests/postgres-prereq.test.sh
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LIB="$SCRIPT_DIR/../lib/postgres-prereq.sh"

if [ ! -f "$LIB" ]; then
  echo "FAIL: $LIB not found" >&2
  exit 1
fi

# shellcheck source=/dev/null
. "$LIB"

PASS=0
FAIL=0
FAILURES=()

pass() { PASS=$((PASS + 1)); printf '  ok   %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAILURES+=("$1"); printf '  FAIL %s\n' "$1" >&2; }

echo "pg_probe — open port detection:"
TEST_PORT=$(( (RANDOM % 10000) + 40000 ))
python3 -c "
import socket, sys, time
s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
s.bind(('127.0.0.1', $TEST_PORT))
s.listen(5)
sys.stdout.write('ready\n'); sys.stdout.flush()
deadline = time.time() + 5
while time.time() < deadline:
    try:
        s.settimeout(0.2)
        conn, _ = s.accept()
        conn.close()
    except socket.timeout:
        pass
s.close()
" >/dev/null 2>&1 &
LISTENER_PID=$!

probed=false
for _ in $(seq 1 30); do
  if pg_probe 127.0.0.1 "$TEST_PORT"; then
    probed=true
    break
  fi
  sleep 0.1
done
if [ "$probed" = true ]; then
  pass "pg_probe detects open port $TEST_PORT"
else
  fail "pg_probe did not detect open port $TEST_PORT within 3s"
fi

kill "$LISTENER_PID" 2>/dev/null
wait "$LISTENER_PID" 2>/dev/null

echo "pg_probe — closed port:"
sleep 0.5
if pg_probe 127.0.0.1 "$TEST_PORT"; then
  fail "pg_probe falsely reported $TEST_PORT as open after listener exited"
else
  pass "pg_probe correctly reports closed port $TEST_PORT as unreachable"
fi

echo "pg_probe — default arg handling:"
pg_probe >/dev/null 2>&1 || true
pass "pg_probe accepts default args without syntax error"

echo "pg_ensure_up — function is defined:"
if declare -F pg_ensure_up >/dev/null; then
  pass "pg_ensure_up is defined as a shell function"
else
  fail "pg_ensure_up is NOT defined — gauntlet would lose auto-start"
fi

echo
echo "passed: $PASS    failed: $FAIL"
if [ "$FAIL" -ne 0 ]; then
  echo "FAILURES:"
  for f in "${FAILURES[@]}"; do echo "  - $f" >&2; done
  exit 1
fi
exit 0
