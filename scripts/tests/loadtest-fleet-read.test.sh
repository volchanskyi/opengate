#!/usr/bin/env bash
# An empty fleet read says which of two things happened.
#
# The relay scenario opens sessions against machines the machine-side harness is
# holding connected, so a read that comes back with no online machine stops the
# run. It stopped it with one message for two situations that need entirely
# different work: a fleet that never arrived at all, and a fleet that arrived and
# that the server is no longer holding — which is what a restart under the run's
# own load produces, because the server sets every online device offline when it
# starts.
#
# Run 33565000569 is what that cost. The harness reported 100 of 100 machines
# connected and held them for eight and a half minutes; the read taken four
# minutes fifty into that hold returned no online machine at all; and nobody
# could say which of the two it was. The answer was sitting in the list the read
# had just fetched — an empty list is nothing ever enrolled, a list full of
# offline machines is the server having forgotten them.
#
# The decision is in a module of its own so it can be driven here with real
# fleet shapes rather than swept for as text. Everything it needs is the list;
# it makes no request and imports nothing from the generator.
#
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

# The module is a generator module, and the generator's modules are ECMAScript
# whatever the directory around them says. A package of its own is what lets
# node load it the way k6 does.
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
printf '{"type":"module"}\n' >"$WORK/package.json"
cp "$FLEET" "$WORK/fleet.js"

# A machine the k6 scenario would be handed, in the shape the API answers with.
machine() {
  printf '{"id":"%s","status":"%s"}' "$1" "$2"
}

# ask runs one case and prints what the module answered, so a case that could not
# be driven at all fails rather than matching an empty string.
ask() {
  local call="$1" fleet="$2"
  node --input-type=module -e "
    import { emptyFleetReason, onlineIds } from '$WORK/fleet.js';
    const fleet = $fleet;
    const answer = $call;
    process.stdout.write(answer === null || answer === undefined ? 'null' : JSON.stringify(answer));
  "
}

# --- the two empty answers are different answers ------------------------------
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

# --- a read that found something is not an empty one --------------------------
held="$(ask 'emptyFleetReason(fleet)' "[$(machine a online),$(machine b offline)]")"
if [ "$held" = "null" ]; then
  pass "a read holding one online machine names no reason to stop"
else
  fail "a read holding an online machine must name no reason to stop, got: $held"
fi

# --- and the ids it hands back are the connected ones alone -------------------
ids="$(ask 'onlineIds(fleet)' "[$(machine a online),$(machine b offline),$(machine c online)]")"
if [ "$ids" = '["a","c"]' ]; then
  pass "only the connected machines are handed to the scenario"
else
  fail "only the connected machines are handed to the scenario, got: $ids"
fi

# A read the server answered with nothing at all is neither list — and a reader
# that treats it as an empty fleet reports "nothing ever enrolled" about a
# request that never resolved.
absent="$(ask 'onlineIds(fleet)' 'null')"
if [ "$absent" = '[]' ]; then
  pass "an absent body yields no machine rather than throwing part-way through setup"
else
  fail "an absent body must yield no machine, got: $absent"
fi

summarize
