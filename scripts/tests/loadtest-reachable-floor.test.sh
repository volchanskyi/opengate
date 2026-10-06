#!/usr/bin/env bash
# A floor on a rate is below what the generator offers, or nothing can clear it.

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

session_driven_scenarios() {
  local file
  for file in "$SCENARIO_DIR"/*.js; do
    [ -e "$file" ] || continue
    if grep -qF 'sessionScenarios(' <<<"$(cat "$file")"; then
      basename "$file" .js
    fi
  done
}

requests_per_journey() {
  grep -cE 'http\.(get|post|put|del|patch|request)\(' <<<"$(cat "$1")" || true
}

# The shortest pause bounds the request rate from above.
pause_seconds() {
  local found
  found="$(grep -oE '\bsleep\(([0-9]+(\.[0-9]+)?)\)' <<<"$(cat "$1")" | grep -oE '[0-9]+(\.[0-9]+)?' | sort -g | head -1)"
  printf '%s' "$found"
}

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

if [ "$READ" -lt 1 ]; then
  fail "the sweep read no rate floor at all, so it proved nothing"
else
  pass "the sweep read $READ rate floor(s)"
fi

assert_eq "a floor equal to the generator's ceiling is refused" "1" \
  "$(python3 -c "import sys; sys.exit(0 if 5.0 < 5.0 else 1)" && echo 0 || echo 1)"
assert_eq "a floor under the generator's ceiling is allowed" "0" \
  "$(python3 -c "import sys; sys.exit(0 if 4.5 < 5.0 else 1)" && echo 0 || echo 1)"

echo
echo "Summary: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ]
