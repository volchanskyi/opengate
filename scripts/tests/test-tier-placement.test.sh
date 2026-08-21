#!/usr/bin/env bash
# Gives each Go test tier a stated seam, so where a test lives says what it needs.
#
# server/tests/integration/ exists for what needs a transport: a real QUIC peer,
# a real socket, a WebSocket. Forty-seven of its eighty-two tests once asserted
# nothing an in-process test could not assert — pgx type semantics, a signaling
# tracker constructed in memory, plain HTTP refusals — because nothing said where
# a test belonged and a new test simply followed the last one.
#
# server/tests/acceptance/ exists for outcomes, stated through the two doors a
# real installation has: the HTTP API and the machine's control stream. A test
# that reaches into a repository is asserting on a row rather than on anything a
# customer can see, so the repositories are reachable only through the harness's
# own arrangement helpers.
#
# Run: ./scripts/tests/test-tier-placement.test.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
INTEGRATION="$ROOT/server/tests/integration"
ACCEPTANCE="$ROOT/server/tests/acceptance"

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

echo "test-tier-placement:"

for dir in "$INTEGRATION" "$ACCEPTANCE"; do
  if [ ! -d "$dir" ]; then
    fail "missing tier: $dir"
    printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
    exit 1
  fi
done

# The transport entry points. A test in the integration tier must reach one of
# them: the QUIC dialer or the harness that stands a listener up, the WebSocket
# client, or the relay pair that joins a browser side to a machine side.
transport_entry_points='quic\.DialAddr|quic\.Config|newAgentTestEnv|dialAgentStream|connectAgent|setupRelayPair|websocket\.Dial|nhooyr\.io/websocket'

misplaced=""
while IFS= read -r file; do
  # A file declaring no test is harness: it carries helpers for the tests
  # beside it and states nothing on its own.
  grep -q '^func Test' "$file" || continue
  grep -qE "$transport_entry_points" "$file" && continue
  misplaced="$misplaced ${file#"$ROOT/"}"
done < <(find "$INTEGRATION" -name '*_test.go' | sort)

if [ -z "$misplaced" ]; then
  pass "every test in the integration tier needs a transport"
else
  fail "these tests need no transport and belong beside the code they exercise:$misplaced"
fi

# The acceptance tier speaks through two doors. Repository packages are the
# inside of the product, so the only file allowed to name one is the harness,
# which uses them to arrange a precondition the product offers no door for.
repository_packages='internal/(device|session|alerts|rules|updater|organization|audit|inventory|notifications|lifecycle|settings|cert|relay|agentapi|signaling)"'
arrangement_files='harness_test.go|tenancy_and_access_test.go|intel_amt_test.go'

reaching=""
while IFS= read -r file; do
  base="$(basename "$file")"
  printf '%s\n' "$base" | grep -qE "^($arrangement_files)$" && continue
  grep -qE "$repository_packages" "$file" && reaching="$reaching ${file#"$ROOT/"}"
done < <(find "$ACCEPTANCE" -name '*_test.go' | sort)

if [ -z "$reaching" ]; then
  pass "no acceptance test reaches past the two doors"
else
  fail "these acceptance tests import a repository package outside the arrangement helpers:$reaching"
fi

# The acceptance tier holds no non-test Go file. That keeps it out of the
# mutation partition check, out of .gremlins.yaml and out of every coverage
# list — the same shape the integration tier already has.
production_go="$(find "$ACCEPTANCE" -name '*.go' ! -name '*_test.go' | wc -l | tr -d ' ')"
if [ "$production_go" -eq 0 ]; then
  pass "the acceptance tier holds no production Go file"
else
  fail "the acceptance tier holds $production_go non-test Go file(s)"
fi

# Every acceptance test runs in parallel. Per-test schema isolation makes that
# safe, and it is what keeps the tier inside the integration tier's budget.
serial=""
while IFS= read -r file; do
  # awk over each test function's first line of body.
  while IFS= read -r name; do
    grep -A 2 "^func $name(t \*testing.T) {" "$file" | grep -q 't.Parallel()' \
      || serial="$serial ${file#"$ROOT/"}:$name"
  done < <(grep -oE '^func (Test[A-Za-z0-9_]+)\(t \*testing\.T\)' "$file" | awk '{print $2}' | sed 's/(t.*//')
done < <(find "$ACCEPTANCE" -name '*_test.go' | sort)

if [ -z "$serial" ]; then
  pass "every acceptance test runs in parallel"
else
  fail "these acceptance tests do not call t.Parallel():$serial"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf 'Failures:\n' >&2
  for f in "${FAILURES[@]}"; do printf '  - %s\n' "$f" >&2; done
  exit 1
fi
