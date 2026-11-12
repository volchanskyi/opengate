#!/usr/bin/env bash
# Holds the technician generator's memory to its visits, not the fleet: a device list fetched inside
# a journey is timed and its body is never kept, since the list grows with every machine filed.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
SCENARIO_DIR="$ROOT/load/k6/scenarios"

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

# journey prints a scenario's per-visit function, from its default export to the next export.
journey() {
  awk '/^export default function/ {on = 1; print; next} on && /^export / {exit} on' "$1"
}

echo "loadtest-k6-bodies:"

scenarios=("$SCENARIO_DIR"/*.js)
if [ "${#scenarios[@]}" -eq 0 ]; then
  fail "load/k6/scenarios contains at least one scenario"
  summarize
fi

listing=0
for f in "${scenarios[@]}"; do
  name="$(basename "$f" .js)"
  body="$(journey "$f")"
  if [ -z "$body" ]; then
    fail "$name declares a default journey this check can read"
    continue
  fi
  lists="$(grep -F 'devicesUrl(' <<<"$body" || true)"
  [ -n "$lists" ] || continue
  listing=$((listing + 1))
  kept="$(grep -vF 'responseType: "none"' <<<"$lists" || true)"
  if [ -z "$kept" ]; then
    pass "$name times its device list and keeps none of the body"
  else
    fail "$name keeps a device list's body on every visit, so its memory grows with the fleet: $kept"
  fi
  if grep -qE '\bdevices\.json\(' <<<"$body"; then
    fail "$name decodes the device list inside its journey"
  else
    pass "$name decodes no device list inside its journey"
  fi
done

if [ "$listing" -gt 0 ]; then
  pass "$listing scenario(s) fetch a device list inside a journey and were checked"
else
  fail "no scenario fetches a device list inside a journey, so this check reads nothing"
fi

summarize
