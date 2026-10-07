#!/usr/bin/env bash
# Drives emptyFleetReason and onlineIds from load/k6/lib/fleet.js with real fleet shapes.
# Run: ./scripts/tests/loadtest-fleet-read.test.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
FLEET="$ROOT/load/k6/lib/fleet.js"

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

summarize() {
  echo
  echo "Summary: $PASS passed, $FAIL failed"
  if [ "$FAIL" -gt 0 ]; then
    printf '  - %s\n' "${FAILURES[@]}" >&2
    exit 1
  fi
  exit 0
}

echo "loadtest-fleet-read:"

if [ ! -f "$FLEET" ]; then
  fail "load/k6/lib/fleet.js is readable"
  summarize
fi

if ! command -v node >/dev/null 2>&1; then
  fail "node is on PATH, so what the fleet read decides can be driven rather than described"
  summarize
fi

# A package.json of type module lets node load the k6 module as ECMAScript.
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
printf '{"type":"module"}\n' >"$WORK/package.json"
cp "$FLEET" "$WORK/fleet.js"

machine() {
  printf '{"id":"%s","status":"%s"}' "$1" "$2"
}

ask() {
  local call="$1" fleet="$2"
  node --input-type=module -e "
    import { emptyFleetReason, onlineIds } from '$WORK/fleet.js';
    const fleet = $fleet;
    const answer = $call;
    process.stdout.write(answer === null || answer === undefined ? 'null' : JSON.stringify(answer));
  "
}

nothing_enrolled="$(ask 'emptyFleetReason(fleet)' '[]')"
if grep -qi 'enrol' <<<"$nothing_enrolled" && ! grep -qi 'forgot\|offline' <<<"$nothing_enrolled"; then
  pass "an empty list reads as a fleet that never arrived"
else
  fail "an empty list must read as a fleet that never arrived, got: $nothing_enrolled"
fi

all_offline="$(ask 'emptyFleetReason(fleet)' "[$(machine a offline),$(machine b offline)]")"
if grep -qi 'offline' <<<"$all_offline" && grep -q '2' <<<"$all_offline"; then
  pass "a list of machines none of which is online reads as the server no longer holding them, and says how many"
else
  fail "a fleet the server forgot must read as such and name the count, got: $all_offline"
fi

if [ "$nothing_enrolled" = "$all_offline" ]; then
  fail "the two empty reads answer identically, which is the defect this exists to close"
else
  pass "the two empty reads answer differently"
fi

held="$(ask 'emptyFleetReason(fleet)' "[$(machine a online),$(machine b offline)]")"
if [ "$held" = "null" ]; then
  pass "a read holding one online machine names no reason to stop"
else
  fail "a read holding an online machine must name no reason to stop, got: $held"
fi

ids="$(ask 'onlineIds(fleet)' "[$(machine a online),$(machine b offline),$(machine c online)]")"
if [ "$ids" = '["a","c"]' ]; then
  pass "only the connected machines are handed to the scenario"
else
  fail "only the connected machines are handed to the scenario, got: $ids"
fi

absent="$(ask 'onlineIds(fleet)' 'null')"
if [ "$absent" = '[]' ]; then
  pass "an absent body yields no machine rather than throwing part-way through setup"
else
  fail "an absent body must yield no machine, got: $absent"
fi

summarize
