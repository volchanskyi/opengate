#!/usr/bin/env bash
# Holds the gauntlet's step timers to a clock that only moves forward, whatever the wall clock does.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
LIB="$REPO_ROOT/scripts/lib/step-clock.sh"
GAUNTLET="$REPO_ROOT/scripts/precommit-gauntlet.sh"

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

[ -f "$LIB" ] || {
  echo "FAIL: $LIB missing" >&2
  exit 1
}

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
mkdir -p "$WORK/bin"

# The workstation's wall clock steps back a second or two a few times an hour; this date answers
# two seconds earlier on every call, as `date +%s` does across such a step.
cat >"$WORK/bin/date" <<'FAKE_DATE'
#!/usr/bin/env bash
set -uo pipefail
n=$(($(cat "$FAKE_DATE_STATE" 2>/dev/null || echo 2000000000) - 2))
printf '%s' "$n" >"$FAKE_DATE_STATE"
printf '%s\n' "$n"
FAKE_DATE
chmod +x "$WORK/bin/date"

elapsed="$(PATH="$WORK/bin:$PATH" FAKE_DATE_STATE="$WORK/date-state" \
  bash -c 'set -euo pipefail; . "$1"; s="$(step_clock)"; sleep 1; e="$(step_clock)"; echo $((e - s))' _ "$LIB")"
if [ "$elapsed" -ge 1 ] && [ "$elapsed" -le 3 ]; then
  pass "a second's step reads 1-3 s while the wall clock steps back"
else
  fail "a second's step read ${elapsed}s while the wall clock stepped back"
fi

reading="$(bash -c 'set -euo pipefail; . "$1"; step_clock' _ "$LIB")"
read -r uptime _ </proc/uptime
uptime="${uptime%.*}"
if [ "$((uptime - reading))" -ge 0 ] && [ "$((uptime - reading))" -le 1 ]; then
  pass "a reading is the kernel's whole seconds since boot"
else
  fail "a reading of $reading is not the kernel's $uptime seconds since boot"
fi

gauntlet="$(cat "$GAUNTLET")"
if grep -qE 'date \+%s|EPOCHSECONDS|EPOCHREALTIME|[$]SECONDS' <<<"$gauntlet"; then
  fail "the gauntlet reads the wall clock for a timer"
else
  pass "the gauntlet reads no wall clock for a timer"
fi
if grep -qF 'scripts/lib/step-clock.sh' <<<"$gauntlet" && grep -qE '[$][(]step_clock[)]' <<<"$gauntlet"; then
  pass "the gauntlet times its steps through step_clock"
else
  fail "the gauntlet does not time its steps through step_clock"
fi

printf '\n%d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
