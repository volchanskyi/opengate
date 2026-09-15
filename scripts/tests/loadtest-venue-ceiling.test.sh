#!/usr/bin/env bash
# A profile does not ask for more machines than its venue has been shown to hold.
#
# Every number a night is judged by lives in the profile, and until now the
# largest of them was the one number nothing checked: how many machines the
# profile asks to connect. A profile could name a fleet the venue has never come
# close to holding, and the first thing anybody would learn about it is a red
# night — or worse, a green one, because a fleet that half arrived still produces
# percentiles and still clears a limit written against the half that did.
#
# That is not hypothetical. The staging ladder's first rung asked for 250 and
# came back invalid; the throwaway ladder reached 8,000 with no errors at all and
# gave at 16,000. Both are facts a run had to be spent to learn, and both are
# knowable from the text the moment they have been recorded once.
#
# So they are recorded once, in
# scripts/lib/loadtest-venue-ceilings.sh, and this holds every profile to the
# row for the venue it names.
#
# The one shape allowed past a ceiling is a ladder, and a ladder says so itself:
# a profile carrying `gave_out:` has written down what counts as giving out
# before going to look for it, which is the whole of what a capacity ladder is.
# Its exemption is re-earned rather than kept — a ladder that no longer reaches
# past the ceiling has stopped being one, and a ladder with no rung at or below
# it cannot say which rung was the last that held.
#
# Run: ./scripts/tests/loadtest-venue-ceiling.test.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
PROFILE_DIR="$REPO_ROOT/load/profiles"
# shellcheck source=scripts/lib/loadtest-profile.sh
. "$REPO_ROOT/scripts/lib/loadtest-profile.sh"
# shellcheck source=scripts/lib/loadtest-venue-ceilings.sh
. "$REPO_ROOT/scripts/lib/loadtest-venue-ceilings.sh"

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

echo "loadtest venue ceiling:"

# profile_breach prints why a profile is refused, or nothing.
#
# It is a function rather than inline so the sweep below and the demonstration
# at the end ask the same question of the same code. A guard whose sweep and
# whose proof are two implementations is a guard with two behaviours.
profile_breach() {
  local profile="$1" name venue ceiling largest ladder below above
  name="$(basename "$profile" .yaml)"

  venue="$(profile_venue "$profile")" || {
    printf '%s names no venue\n' "$name"
    return 0
  }
  ceiling="$(loadtest_venue_ceiling_agents "$venue" 2>/dev/null)" || {
    printf '%s names venue %s, which no ceiling has been recorded for\n' "$name" "$venue"
    return 0
  }

  largest="$(profile_phases "$profile" | jq -r '[.[].agents] | max')"
  ladder="$(profile_is_ladder "$profile")"

  if [ "$ladder" != "true" ]; then
    if [ "$largest" -gt "$ceiling" ]; then
      printf '%s asks for %s machines on %s, which has been shown to hold %s\n' \
        "$name" "$largest" "$venue" "$ceiling"
    fi
    return 0
  fi

  # A ladder, which is allowed past the ceiling and owes the bracket instead.
  below="$(profile_phases "$profile" | jq -r --argjson c "$ceiling" '[.[].agents | select(. > 0 and . <= $c)] | length')"
  above="$(profile_phases "$profile" | jq -r --argjson c "$ceiling" '[.[].agents | select(. > $c)] | length')"
  if [ "$above" -eq 0 ]; then
    printf '%s declares gave_out: but never reaches past %s on %s, so it is not a ladder any more\n' \
      "$name" "$ceiling" "$venue"
  elif [ "$below" -eq 0 ]; then
    printf '%s starts above %s on %s, so no rung of it can be the last that held\n' \
      "$name" "$ceiling" "$venue"
  fi
}

# --- Every profile is inside its venue, or is a ladder that brackets it -------

checked=0
while IFS= read -r profile; do
  name="$(basename "$profile" .yaml)"
  breach="$(profile_breach "$profile")"
  checked=$((checked + 1))
  if [ -z "$breach" ]; then
    pass "$name fits the venue it names"
  else
    fail "$breach"
  fi
done < <(find "$PROFILE_DIR" -name '*.yaml' | sort)

# A sweep that reached nothing passes every assertion it never made.
if [ "$checked" -ge 5 ]; then
  pass "reached $checked profiles"
else
  fail "reached only $checked profiles — the profile directory moved, or the reader stopped answering"
fi

# --- Every recorded ceiling is a ceiling something is held to ------------------
#
# A row nothing reads is where the next drift begins: it goes stale silently,
# because nothing ever asks it a question. Same rule the tool manifest keeps.

for venue in $(loadtest_venues); do
  if grep -qlr "^environment: $venue\$" "$PROFILE_DIR" >/dev/null 2>&1; then
    pass "the $venue ceiling is a ceiling some profile is held to"
  else
    fail "$venue has a recorded ceiling and no profile names it — delete the row or schedule a profile there"
  fi
  evidence="$(loadtest_venue_ceiling_evidence "$venue")"
  if [ -n "$evidence" ]; then
    pass "the $venue ceiling names the run that measured it"
  else
    fail "$venue's ceiling is a number with no run behind it"
  fi
done

# --- The guard refuses a profile that is over its venue -----------------------
#
# A sweep that has stopped being able to refuse anything reports the same thing
# as a repository with nothing to refuse. So the refusal itself is exercised,
# against a profile built to be over the ceiling by one machine.

fixture="$(mktemp -d)"
trap 'rm -rf "$fixture"' EXIT
over="$(($(loadtest_venue_ceiling_agents staging) + 1))"
cat >"$fixture/over.yaml" <<EOF
schema_version: 1
name: over
family: normal
environment: staging
fixture: small
phases:
  - name: steady
    duration: 1m
    operator_arrivals_per_second: 1
    connected_agents: $over
    sessions: 0
    measured: true
EOF

if [ -n "$(profile_breach "$fixture/over.yaml")" ]; then
  pass "a profile one machine over its venue's ceiling is refused"
else
  fail "a profile over its venue's ceiling was accepted — this guard no longer guards anything"
fi

# And a ladder that has stopped reaching past the ceiling is refused too, so the
# exemption cannot outlive the reason for it.
cat >"$fixture/stunted.yaml" <<EOF
schema_version: 1
name: stunted
family: breakpoint
environment: runner
fixture: small
phases:
  - name: step-1
    duration: 1m
    operator_arrivals_per_second: 1
    connected_agents: 100
    sessions: 0
gave_out:
  error_rate_above: 0.05
EOF

if [ -n "$(profile_breach "$fixture/stunted.yaml")" ]; then
  pass "a ladder that no longer reaches past the ceiling is refused"
else
  fail "a ladder that reaches nothing kept its exemption"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf 'Failures:\n' >&2
  for f in "${FAILURES[@]}"; do printf '  - %s\n' "$f" >&2; done
  exit 1
fi
