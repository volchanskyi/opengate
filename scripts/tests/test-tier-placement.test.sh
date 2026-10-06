#!/usr/bin/env bash
# Holds integration tests to those needing a transport, and acceptance tests to the HTTP API
# and the control stream, parallel and free of repository imports outside the harness.

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

transport_entry_points='quic\.DialAddr|quic\.Config|newAgentTestEnv|dialAgentStream|connectAgent|setupRelayPair|websocket\.Dial|nhooyr\.io/websocket'

misplaced=""
while IFS= read -r file; do
  grep -q '^func Test' "$file" || continue
  grep -qE "$transport_entry_points" "$file" && continue
  misplaced="$misplaced ${file#"$ROOT/"}"
done < <(find "$INTEGRATION" -name '*_test.go' | sort)

if [ -z "$misplaced" ]; then
  pass "every test in the integration tier needs a transport"
else
  fail "these tests need no transport and belong beside the code they exercise:$misplaced"
fi

repository_packages='internal/(device|session|alerts|rules|updater|organization|audit|inventory|notifications|lifecycle|settings|cert|relay|agentapi|signaling)"'
arrangement_files='harness_test.go|tenancy_and_access_test.go|intel_amt_test.go'

reaching=""
while IFS= read -r file; do
  base="$(basename "$file")"
  grep -qE "^($arrangement_files)$" <<<"$base" && continue
  grep -qE "$repository_packages" "$file" && reaching="$reaching ${file#"$ROOT/"}"
done < <(find "$ACCEPTANCE" -name '*_test.go' | sort)

if [ -z "$reaching" ]; then
  pass "no acceptance test reaches past the two doors"
else
  fail "these acceptance tests import a repository package outside the arrangement helpers:$reaching"
fi

production_go="$(find "$ACCEPTANCE" -name '*.go' ! -name '*_test.go' | wc -l | tr -d ' ')"
if [ "$production_go" -eq 0 ]; then
  pass "the acceptance tier holds no production Go file"
else
  fail "the acceptance tier holds $production_go non-test Go file(s)"
fi

serial=""
while IFS= read -r file; do
  while IFS= read -r name; do
    # The opening lines sit in a variable because a pipe into grep -q fails under pipefail.
    opening="$(grep -A 2 "^func $name(t \*testing.T) {" "$file" || true)"
    grep -qF 't.Parallel()' <<<"$opening" \
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
