#!/usr/bin/env bash
# Tests for deploy/scripts/helper-probe.py against a responder on loopback that answers a Binding
# request with the address it saw, one that answers for another address, and a port nobody holds.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
PROBE="$REPO_ROOT/deploy/scripts/helper-probe.py"
CD="$REPO_ROOT/.github/workflows/cd.yml"

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

echo "helper-probe:"

[ -f "$PROBE" ] || {
  echo "FAIL: $PROBE missing" >&2
  exit 1
}

WORK="$(mktemp -d)"
RESPONDERS=()
trap 'kill "${RESPONDERS[@]}" 2>/dev/null; rm -rf "$WORK"' EXIT

# The responder answers each Binding request with XOR-MAPPED-ADDRESS naming the address given, or
# the sender's own when none is, and writes its port once it is listening.
cat >"$WORK/responder.py" <<'RESPONDER'
import socket, struct, sys

claimed = sys.argv[2] if len(sys.argv) > 2 else None
sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
sock.bind(("127.0.0.1", 0))
with open(sys.argv[1], "w", encoding="utf-8") as handle:
    handle.write(str(sock.getsockname()[1]))
COOKIE = 0x2112A442
while True:
    data, (host, port) = sock.recvfrom(2048)
    kind, _, cookie = struct.unpack("!HHI", data[:8])
    if kind != 0x0001 or cookie != COOKIE:
        continue
    address = socket.inet_aton(claimed or host)
    xaddr = struct.unpack("!I", address)[0] ^ COOKIE
    value = struct.pack("!BBHI", 0, 1, port ^ (COOKIE >> 16), xaddr)
    attribute = struct.pack("!HH", 0x0020, len(value)) + value
    reply = struct.pack("!HHI", 0x0101, len(attribute), COOKIE) + data[8:20] + attribute
    sock.sendto(reply, (host, port))
RESPONDER

start_responder() { # port file, [claimed address]
  python3 "$WORK/responder.py" "$@" &
  RESPONDERS+=("$!")
  for _ in $(seq 1 50); do
    [ -s "$1" ] && return 0
    sleep 0.1
  done
  return 1
}

start_responder "$WORK/honest.port"
honest="$(cat "$WORK/honest.port")"
start_responder "$WORK/rewritten.port" 10.0.0.7
rewritten="$(cat "$WORK/rewritten.port")"

rc=0
out="$(python3 "$PROBE" "stun:127.0.0.1:$honest" 127.0.0.1 2>&1)" || rc=$?
if [ "$rc" -eq 0 ] && grep -qF "127.0.0.1" <<<"$out"; then
  pass "a helper naming the caller's own address passes and says which"
else
  fail "a helper naming the caller's own address must pass (rc=$rc): $out"
fi

rc=0
out="$(python3 "$PROBE" "stun:127.0.0.1:$rewritten" 127.0.0.1 2>&1)" || rc=$?
if [ "$rc" -ne 0 ] && grep -qF "10.0.0.7" <<<"$out" && grep -qF "127.0.0.1" <<<"$out"; then
  pass "a helper naming another address fails, naming both"
else
  fail "a helper behind an address-rewriting hop must fail and name both addresses (rc=$rc): $out"
fi

# A port nobody holds answers with an unreachable notice, which a connected socket reads at once.
closed="$(python3 -c 'import socket; s=socket.socket(socket.AF_INET, socket.SOCK_DGRAM); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1])')"
rc=0
out="$(timeout 60 python3 "$PROBE" "stun:127.0.0.1:$closed" 127.0.0.1 2>&1)" || rc=$?
if [ "$rc" -ne 0 ] && [ "$rc" -ne 124 ] && grep -qi "no answer" <<<"$out"; then
  pass "a helper that never answers fails and says so"
else
  fail "a helper that never answers must fail with 'no answer' (rc=$rc): $out"
fi

rc=0
out="$(python3 "$PROBE" "turn:127.0.0.1:$honest" 127.0.0.1 2>&1)" || rc=$?
if [ "$rc" -ne 0 ]; then
  pass "a helper that is not a stun: URL is refused"
else
  fail "a turn: URL must be refused: $out"
fi

# The production deploy asks the helper the running server names, from the runner's address.
step="$(awk '
  $0 == "      - name: Ask the production helper where this runner comes from" { on = 1; next }
  on && /^      - / { exit }
  on { print }
' "$CD")"
if grep -qF 'deploy/scripts/helper-probe.py' <<<"$step" \
  && grep -qF 'name=="OPENGATE_ICE_SERVERS"' <<<"$step" \
  && grep -qF '/cdn-cgi/trace' <<<"$step"; then
  pass "the production deploy probes the helper the running server names against the runner's address"
else
  fail "cd.yml has no production step probing the running server's helper against the runner's address"
fi

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
