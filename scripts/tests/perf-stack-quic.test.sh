#!/usr/bin/env bash
# Tests for scripts/perf-stack-quic.sh, using docker and sudo stand-ins that record their arguments.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
QUIC="$REPO_ROOT/scripts/perf-stack-quic.sh"
[ -x "$QUIC" ] || {
  echo "FAIL: $QUIC not executable" >&2
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
STUB="$WORK/stub"
mkdir -p "$STUB"

cat >"$STUB/sudo" <<'STUB_SUDO'
#!/usr/bin/env bash
set -euo pipefail
[ "$1" = -n ] && shift
printf '%s\n' "$*" >>"$STUB_STATE/sudo.log"
case "$1" in
  sysctl)
    shift
    [ "$1" = -w ] && shift
    for setting in "$@"; do printf '%s\n' "$setting" >>"$STUB_STATE/sysctl"; done
    ;;
  tee)
    cat >>"$STUB_STATE/hosts"
    ;;
  *) exit 1 ;;
esac
STUB_SUDO
cat >"$STUB/docker" <<'STUB_DOCKER'
#!/usr/bin/env bash
set -euo pipefail
case "$1" in
  inspect)
    case "$*" in
      *IPAddress*) cat "$STUB_STATE/ip" ;;
      *State.Pid*) cat "$STUB_STATE/pid" ;;
    esac
    ;;
  logs) cat "$STUB_STATE/server.log" ;;
  *) exit 1 ;;
esac
STUB_DOCKER
chmod +x "$STUB/sudo" "$STUB/docker"
export STUB_STATE="$WORK"

run_quic() {
  STATUS=0
  PATH="$STUB:$PATH" GITHUB_ENV="$WORK/github.env" "$QUIC" "$@" >"$WORK/out.txt" 2>&1 || STATUS=$?
}

echo "perf-stack-quic:"

# quic-go asks for a 7 MiB receive buffer per socket and a runner's kernel caps one at 1 MiB.
: >"$WORK/sysctl"
run_quic raise-buffers
assert_eq "raising the buffers succeeds" "0" "$STATUS"
for key in net.core.rmem_max net.core.wmem_max net.core.rmem_default; do
  value="$(sed -n "s/^${key}=//p" "$WORK/sysctl")"
  if [ -n "$value" ] && [ "$value" -ge $((7 * 1024 * 1024)) ]; then
    pass "$key clears the 7 MiB the transport asks for"
  else
    fail "$key must be raised to at least 7 MiB (got=[$value])"
  fi
done

printf '172.18.0.4' >"$WORK/ip"
printf '%s' "$$" >"$WORK/pid"
: >"$WORK/hosts"
: >"$WORK/github.env"
run_quic map-server
assert_eq "mapping the server succeeds" "0" "$STATUS"
assert_eq "the certificate's name resolves to the container" "172.18.0.4 server" "$(cat "$WORK/hosts")"
assert_eq "the harness is told where the target's counters are" \
  "PERF_TARGET_NET_COUNTERS=/proc/$$/net/snmp" "$(cat "$WORK/github.env")"

: >"$WORK/ip"
run_quic map-server
assert_eq "a server with no address is refused" "1" "$STATUS"

printf '172.18.0.4' >"$WORK/ip"
printf '999999999' >"$WORK/pid"
run_quic map-server
assert_eq "a target whose counters cannot be read is refused" "1" "$STATUS"

warning='failed to sufficiently increase receive buffer size (was: 1024 kiB, wanted: 7168 kiB, got: 2048 kiB)'
printf 'Starting QUIC load test\n=== Results ===\n' >"$WORK/harness.txt"
: >"$WORK/server.log"
run_quic check "$WORK/harness.txt"
assert_eq "a run whose ends had the buffers they asked for passes" "0" "$STATUS"

printf '%s\n' "$warning" >>"$WORK/harness.txt"
run_quic check "$WORK/harness.txt"
assert_eq "the generator running short fails the run" "1" "$STATUS"
if grep -qF 'generator' "$WORK/out.txt"; then
  pass "and says which end"
else
  fail "and says which end (got=[$(cat "$WORK/out.txt")])"
fi

printf 'Starting QUIC load test\n=== Results ===\n' >"$WORK/harness.txt"
printf '2026/09/26 12:03:46 %s\n' "$warning" >"$WORK/server.log"
run_quic check "$WORK/harness.txt"
assert_eq "the server running short fails the run" "1" "$STATUS"

: >"$WORK/server.log"
run_quic check "$WORK/no-such-output.txt"
assert_eq "a missing harness output is refused" "1" "$STATUS"

run_quic hold
assert_eq "an unknown verb is refused" "2" "$STATUS"

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf 'Failures:\n' >&2
  for f in "${FAILURES[@]}"; do printf '  - %s\n' "$f" >&2; done
  exit 1
fi
