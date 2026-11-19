#!/usr/bin/env bash
# Tests for scripts/loadtest-k6-alongside.sh — the technicians sign in before the walk, and the
# scenarios join the walk with the accounts signed in for them.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
ALONGSIDE="$REPO_ROOT/scripts/loadtest-k6-alongside.sh"

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

# The fake k6 signs a scenario's technicians in when asked for their accounts, and otherwise
# records what a scenario run was handed and whether the walk could have started.
cat >"$WORK/k6" <<EOF
#!/usr/bin/env bash
out="" accounts="" scenario="" prev=""
for arg in "\$@"; do
  case "\$arg" in
    LOADTEST_TECHNICIANS_OUT=*) out="\${arg#*=}" ;;
    LOADTEST_TECHNICIANS=*) accounts="\${arg#*=}" ;;
    LOADTEST_SCENARIO=*) scenario="\${arg#*=}" ;;
  esac
done
if [ -n "\$out" ]; then
  if [ -f "$WORK/refuse-sign-in" ]; then
    echo "setup: register returned 503: request timeout" >&2
    exit 107
  fi
  printf '[{"token":"t-%s","email":"%s@opengate-loadtest.invalid"}]' "\$scenario" "\$scenario" >"\$out"
  echo "signed-in \$scenario" >>"$WORK/calls"
  exit 0
fi
echo "run \$scenario accounts=\$(cat "\$accounts" 2>/dev/null) ready=\$(cat "$WORK/ready" 2>/dev/null)" >>"$WORK/calls"
for arg in "\$@"; do
  [ "\$prev" = --summary-export ] && printf '{"metrics":{}}' >"\$arg"
  prev="\$arg"
done
exit 0
EOF
chmod +x "$WORK/k6"

# harness writes the run's announcements as the harness does: sign-in leave at once, and the
# estate and the walk only once the ready file says the technicians signed in.
harness() {
  local log="$WORK/harness.txt"
  printf 'Technicians may sign in\n' >"$log"
  (
    for _ in $(seq 1 100); do
      if [ "$(cat "$WORK/ready" 2>/dev/null)" = signed-in ]; then
        printf 'Estate filed\nWalk started at %s\n' "$(date +%s)" >>"$log"
        exit 0
      fi
      sleep 0.05
    done
  ) &
  HARNESS=$!
}

run_alongside() {
  rm -f "$WORK/calls" "$WORK/ready"
  mkdir -p "$WORK/summaries"
  harness
  STATUS=0
  K6_BIN="$WORK/k6" \
    LOADTEST_K6_SUMMARY_DIR="$WORK/summaries" \
    LOADTEST_BASE_URL="http://127.0.0.1:18080" \
    LOADTEST_RUN_ID="99-1" \
    LOADTEST_PROFILE="$REPO_ROOT/load/profiles/catch-up.yaml" \
    LOADTEST_TECHNICIANS_READY="$WORK/ready" \
    LOADTEST_K6_ALONGSIDE_POLL_SECONDS=0.05 \
    LOADTEST_K6_ALONGSIDE_TIMEOUT_SECONDS="${TIMEOUT:-20}" \
    "$ALONGSIDE" "$WORK/harness.txt" api-baseline relay-throughput >"$WORK/out.txt" 2>&1 || STATUS=$?
  wait "$HARNESS" || true
}

echo "loadtest-k6-alongside:"

run_alongside
assert_eq "a leg whose technicians signed in exits 0" "0" "$STATUS"
assert_eq "the ready file says the technicians signed in" "signed-in" "$(cat "$WORK/ready")"
assert_eq "every scenario's technicians sign in first" "2" "$(grep -c '^signed-in ' "$WORK/calls")"
if grep -qxF 'run api-baseline accounts=[{"token":"t-api-baseline","email":"api-baseline@opengate-loadtest.invalid"}] ready=signed-in' "$WORK/calls" \
  && grep -qxF 'run relay-throughput accounts=[{"token":"t-relay-throughput","email":"relay-throughput@opengate-loadtest.invalid"}] ready=signed-in' "$WORK/calls"; then
  pass "each scenario runs after the sign-in with its own accounts"
else
  fail "each scenario runs after the sign-in with its own accounts: $(cat "$WORK/calls")"
fi
last_sign_in="$(grep -n '^signed-in ' "$WORK/calls" | tail -1 | cut -d: -f1)"
first_run="$(grep -n '^run ' "$WORK/calls" | head -1 | cut -d: -f1)"
if [ "$last_sign_in" -lt "$first_run" ]; then
  pass "no scenario runs before every technician signed in"
else
  fail "a scenario ran before every technician signed in: $(cat "$WORK/calls")"
fi

touch "$WORK/refuse-sign-in"
run_alongside
rm -f "$WORK/refuse-sign-in"
assert_eq "a refused sign-in exits 1" "1" "$STATUS"
if grep -q '^refused: ' "$WORK/ready"; then
  pass "the ready file tells the harness the sign-in was refused, so the walk stops at once"
else
  fail "the ready file must name a refused sign-in: $(cat "$WORK/ready" 2>/dev/null)"
fi
if ! grep -q '^run ' "$WORK/calls" 2>/dev/null; then
  pass "no scenario runs once the sign-in was refused"
else
  fail "a scenario ran after the sign-in was refused: $(cat "$WORK/calls")"
fi

rm -f "$WORK/calls" "$WORK/ready"
printf 'Starting QUIC load test\n' >"$WORK/harness.txt"
STATUS=0
K6_BIN="$WORK/k6" LOADTEST_TECHNICIANS_READY="$WORK/ready" \
  LOADTEST_K6_ALONGSIDE_POLL_SECONDS=0.05 LOADTEST_K6_ALONGSIDE_TIMEOUT_SECONDS=1 \
  "$ALONGSIDE" "$WORK/harness.txt" api-baseline >"$WORK/out.txt" 2>&1 || STATUS=$?
assert_eq "a harness that never lets the technicians sign in exits 1" "1" "$STATUS"
if grep -qF 'did not say the technicians may sign in within 1s' "$WORK/out.txt"; then
  pass "the wait that ran out is named"
else
  fail "the wait that ran out is named: $(cat "$WORK/out.txt")"
fi

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}"
  exit 1
fi
