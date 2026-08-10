#!/usr/bin/env bash
# Tests for scripts/lib/victoriametrics-prereq.sh. Plain bash; no bats dependency.
# Run: ./scripts/tests/victoriametrics-prereq.test.sh
#
# testvm memoizes its container per test BINARY, and `go test ./tests/...` builds
# one binary per package, so an unset VICTORIAMETRICS_TEST_URL makes every
# VictoriaMetrics-touching package start its own. The gauntlet closes that the
# same way it closes the Postgres one — start a single instance and export its
# URL — and these tests pin the library behind it without starting a container.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
LIB="$SCRIPT_DIR/../lib/victoriametrics-prereq.sh"

if [ ! -f "$LIB" ]; then
  echo "FAIL: $LIB not found" >&2
  exit 1
fi

# shellcheck source=/dev/null
. "$LIB"

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

echo "vm_probe — open port detection:"
TEST_PORT=$(((RANDOM % 10000) + 40000))
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
  if vm_probe 127.0.0.1 "$TEST_PORT"; then
    probed=true
    break
  fi
  sleep 0.1
done
if [ "$probed" = true ]; then
  pass "vm_probe detects open port $TEST_PORT"
else
  fail "vm_probe did not detect open port $TEST_PORT within 3s"
fi

kill "$LISTENER_PID" 2>/dev/null || true
wait "$LISTENER_PID" 2>/dev/null || true

echo "vm_probe — closed port:"
sleep 0.5
if vm_probe 127.0.0.1 "$TEST_PORT"; then
  fail "vm_probe falsely reported $TEST_PORT as open after listener exited"
else
  pass "vm_probe correctly reports closed port $TEST_PORT as unreachable"
fi

echo "vm_probe — default arg handling:"
vm_probe >/dev/null 2>&1 || true
pass "vm_probe accepts default args without syntax error"

echo "vm_ensure_up — function is defined:"
if declare -F vm_ensure_up >/dev/null; then
  pass "vm_ensure_up is defined as a shell function"
else
  fail "vm_ensure_up is NOT defined — the gauntlet would lose auto-start"
fi

echo "vm_ensure_up — an unreachable instance is reported, never passed over:"
OUT="$(VM_PREREQ_START_CMD=false vm_ensure_up 127.0.0.1 "$TEST_PORT" 1 2>&1 || true)"
if printf '%s' "$OUT" | grep -q "unreachable"; then
  pass "vm_ensure_up says an unreachable instance is unreachable"
else
  fail "vm_ensure_up said nothing about an unreachable instance: [$OUT]"
fi
if VM_PREREQ_START_CMD=false vm_ensure_up 127.0.0.1 "$TEST_PORT" 1 >/dev/null 2>&1; then
  fail "vm_ensure_up returned success when it could not start VictoriaMetrics"
else
  pass "vm_ensure_up fails loudly when it cannot start VictoriaMetrics"
fi

echo "vm_test_url — the URL the gauntlet exports:"
assert_eq "vm_test_url defaults to the make target's published port" \
  "http://127.0.0.1:8428" "$(vm_test_url)"
assert_eq "vm_test_url honours an override port" \
  "http://127.0.0.1:9999" "$(VICTORIAMETRICS_TEST_PORT=9999 vm_test_url)"

echo "make target exists and pins the same image as the Go harness:"
GO_VM_IMAGE="$(grep -oE 'image = "victoriametrics/[^"]+"' "$ROOT/server/internal/testvm/testvm.go" | cut -d'"' -f2 || true)"
MK_VM_IMAGE="$(grep -oE 'victoriametrics/victoria-metrics:[^ \\]+' "$ROOT/Makefile" | head -1 || true)"
assert_eq "Makefile VictoriaMetrics image matches testvm's pin" "$GO_VM_IMAGE" "$MK_VM_IMAGE"
if grep -qE '^victoriametrics-test-up:' "$ROOT/Makefile"; then
  pass "make victoriametrics-test-up exists for vm_ensure_up to call"
else
  fail "make victoriametrics-test-up is missing — vm_ensure_up cannot auto-start"
fi

echo "gauntlet wires the gate in:"
GAUNTLET="$ROOT/scripts/precommit-gauntlet.sh"
if grep -q "victoriametrics-prereq.sh" "$GAUNTLET" && grep -q "vm_ensure_up" "$GAUNTLET"; then
  pass "gauntlet sources the library and calls vm_ensure_up"
else
  fail "gauntlet does not gate on VictoriaMetrics — ./tests/... would start one container per package"
fi
if grep -q "export VICTORIAMETRICS_TEST_URL" "$GAUNTLET"; then
  pass "gauntlet exports VICTORIAMETRICS_TEST_URL so every package shares one instance"
else
  fail "gauntlet does not export VICTORIAMETRICS_TEST_URL — packages would each provision their own"
fi

echo
echo "passed: $PASS    failed: $FAIL"
if [ "$FAIL" -ne 0 ]; then
  echo "FAILURES:"
  for f in "${FAILURES[@]}"; do echo "  - $f" >&2; done
  exit 1
fi
exit 0
