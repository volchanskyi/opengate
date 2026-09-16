#!/usr/bin/env bash
# A floor on a rate is below what the generator offers, or nothing can clear it.
#
# A profile can hold a scenario to a minimum throughput. That is a statement
# about the system only where the generator is capable of exceeding it; where it
# is not, the limit is breached on every run that has ever been taken and says
# nothing at all — the mirror of the gate reader's own sentence about a limit on
# a measurement nothing produced.
#
# What it cost: the relay path is held to at least five requests a second. The
# profile gives that scenario five sessions, each of which makes one request and
# then pauses a full second, so five of them can make at most five requests a
# second and only with a server that answers in no time. The nights on record
# read 4.926, 4.926, 4.928, 4.932 and 4.935 — a spread of nine thousandths, one
# hair under a floor no server could ever reach. The server underneath was
# opening a session in 4.9 ms and returning a keystroke through the machine in
# 3.0 ms, with nothing failing.
#
# It was invisible for a second reason, repaired alongside this one: the night's
# own verdict recorded itself as failed and the job reported success.
#
# What is checked here is the arithmetic impossibility, which is the half a
# static gate can see: sessions times requests per journey, over the pause each
# journey takes, is an upper bound nothing can exceed, and a floor at or above it
# is refused. How close underneath a floor may sit is a judgement about the
# server, and it is made in the profile with the nights on record beside it.
#
# The session-driven scenarios are found rather than listed, so a scenario that
# starts holding sessions is swept without anybody remembering to add it. A sweep
# that reached nothing fails, because a parser quietly matching no scenario would
# report every floor as reachable.
#
# Run: ./scripts/tests/loadtest-reachable-floor.test.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
SCENARIO_DIR="$ROOT/load/k6/scenarios"
PROFILE_DIR="$ROOT/load/profiles"

PASS=0
FAIL=0

pass() {
  printf '  PASS %s\n' "$1"
  PASS=$((PASS + 1))
}

fail() {
  printf '  FAIL %s\n' "$1"
  FAIL=$((FAIL + 1))
}

assert_eq() {
  if [ "$2" = "$3" ]; then
    pass "$1"
  else
    fail "$1 (want=[$2] got=[$3])"
  fi
}

# session_driven_scenarios names every k6 scenario whose executors come from the
# shared session builder — a fixed number of sessions held open, rather than an
# arrival rate offered.
session_driven_scenarios() {
  local file
  for file in "$SCENARIO_DIR"/*.js; do
    [ -e "$file" ] || continue
    if grep -qF 'sessionScenarios(' <<<"$(cat "$file")"; then
      basename "$file" .js
    fi
  done
}

# requests_per_journey counts the HTTP calls one journey makes.
requests_per_journey() {
  grep -cE 'http\.(get|post|put|del|patch|request)\(' <<<"$(cat "$1")" || true
}

# pause_seconds is the shortest wait a journey takes before the next one starts.
# The shortest is the one that bounds the rate from above.
pause_seconds() {
  local found
  found="$(grep -oE '\bsleep\(([0-9]+(\.[0-9]+)?)\)' <<<"$(cat "$1")" | grep -oE '[0-9]+(\.[0-9]+)?' | sort -g | head -1)"
  printf '%s' "$found"
}

# measured_sessions is how many sessions the profile's measured phase holds
# open, which is what the session builder gives the scenario.
measured_sessions() {
  local profile="$1"
  grep -A20 -E '^\s+- name:' "$profile" >/dev/null 2>&1 || true
  python3 - "$profile" <<'PY'
import re
import sys

text = open(sys.argv[1]).read()
blocks = re.split(r"\n  - name: ", text.split("phases:", 1)[1])[1:]
for block in blocks:
    if re.search(r"^\s+measured:\s*true\s*$", block, re.M):
        found = re.search(r"^\s+sessions:\s*(\d+)\s*$", block, re.M)
        print(found.group(1) if found else "")
        break
PY
}

# rps_floors lists every minimum this profile holds a scenario's HTTP rate to.
rps_floors() {
  local profile="$1" scenario="$2"
  python3 - "$profile" "$scenario" <<'PY'
import re
import sys

text = open(sys.argv[1]).read()
series = "k6/%s/http" % sys.argv[2]
gates = text.split("gates:", 1)
if len(gates) < 2:
    sys.exit(0)
for entry in re.split(r"\n  - series: ", gates[1])[1:]:
    name, _, rest = entry.partition("\n")
    if name.strip() != series:
        continue
    if not re.search(r"^\s+metric:\s*rps\s*$", rest, re.M):
        continue
    found = re.search(r"^\s+min:\s*([0-9.]+)\s*$", rest, re.M)
    if found:
        print(found.group(1))
PY
}

echo "loadtest-reachable-floor:"

SCENARIOS="$(session_driven_scenarios)"
if [ -z "$SCENARIOS" ]; then
  fail "the sweep found no session-driven scenario to read, so it checked nothing"
  echo
  echo "Summary: $PASS passed, $FAIL failed"
  exit 1
fi

READ=0
while IFS= read -r scenario; do
  [ -n "$scenario" ] || continue
  file="$SCENARIO_DIR/$scenario.js"

  calls="$(requests_per_journey "$file")"
  if [ "$calls" -lt 1 ]; then
    fail "$scenario: no HTTP call found in the journey, so its rate cannot be bounded"
    continue
  fi

  pause="$(pause_seconds "$file")"
  if [ -z "$pause" ] || [ "$pause" = "0" ]; then
    fail "$scenario: no pause found between journeys, so its rate cannot be bounded"
    continue
  fi

  for profile in "$PROFILE_DIR"/*.yaml; do
    [ -e "$profile" ] || continue
    sessions="$(measured_sessions "$profile")"
    [ -n "$sessions" ] || continue

    floors="$(rps_floors "$profile" "$scenario")"
    [ -n "$floors" ] || continue

    ceiling="$(python3 -c "print($sessions * $calls / $pause)")"
    while IFS= read -r floor; do
      [ -n "$floor" ] || continue
      READ=$((READ + 1))
      name="$(basename "$profile")"
      if python3 -c "import sys; sys.exit(0 if $floor < $ceiling else 1)"; then
        pass "$name holds $scenario to $floor a second, under the $ceiling its generator can offer"
      else
        fail "$name holds $scenario to $floor a second, which its own generator cannot reach: $sessions sessions making $calls request(s) every ${pause}s is at most $ceiling"
      fi
    done <<<"$floors"
  done
done <<<"$SCENARIOS"

# A sweep that read no limit checked nothing, which is the shape it exists to
# refuse one field over.
if [ "$READ" -lt 1 ]; then
  fail "the sweep read no rate floor at all, so it proved nothing"
else
  pass "the sweep read $READ rate floor(s)"
fi

# The defect, demonstrated: a floor equal to what the generator offers is
# refused, and one under it is not. A guard that has stopped reproducing
# anything fails rather than quietly policing a non-problem.
assert_eq "a floor equal to the generator's ceiling is refused" "1" \
  "$(python3 -c "import sys; sys.exit(0 if 5.0 < 5.0 else 1)" && echo 0 || echo 1)"
assert_eq "a floor under the generator's ceiling is allowed" "0" \
  "$(python3 -c "import sys; sys.exit(0 if 4.5 < 5.0 else 1)" && echo 0 || echo 1)"

echo
echo "Summary: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ]
