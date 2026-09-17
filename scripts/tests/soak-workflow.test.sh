#!/usr/bin/env bash
# The endurance family offers the technician load its profile declares.
#
# soak.yaml declares three sessions and three technician arrivals a second
# through each of its ten busy phases, and nothing offered either — so those
# numbers described an intention rather than a fact. It cost more here than
# anywhere else the same gap was true, because the leak this family exists for
# stranded two goroutines on a *finished relay session*: a run that opens no
# session never performs the operation the leak attaches to. The first run of
# this family to reach its end finished 2,750 machine-lives and not one
# session, and published a conservation reading divided by the machines alone.
#
# The machine side has to answer as well as the browser side asking. A session
# has two ends: the generator opens the operator's, and the harness joins the
# machine's and echoes — which is also what counts the session into the
# denominator the target's conservation is expressed against. Without that
# flag the browser side times a frame nobody sends back, and the count the
# whole family is judged by does not move.
#
# Both directions of the fold are checked, because either alone is satisfied by
# doing neither: a job that starts a generator and never folds its numbers in
# has produced readings in a temp directory nothing opens, and a job that folds
# an export nothing wrote fails on the night rather than here.
#
# Run: ./scripts/tests/soak-workflow.test.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
WORKFLOW="$REPO_ROOT/.github/workflows/soak.yml"
PROFILE="$REPO_ROOT/load/profiles/soak.yaml"
SCENARIO_DIR="$REPO_ROOT/load/k6/scenarios"
# shellcheck source=scripts/lib/loadtest-profile.sh
. "$REPO_ROOT/scripts/lib/loadtest-profile.sh"

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

echo "soak workflow:"

for f in "$WORKFLOW" "$PROFILE"; do
  if [ ! -f "$f" ]; then
    fail "missing file: $f"
    printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
    exit 1
  fi
done

workflow="$(cat "$WORKFLOW")"

# --- the profile declares a technician load at all ----------------------------
#
# Every check below asks whether what the profile declares is offered. A
# profile declaring nothing satisfies all of them by asking for nothing, which
# is the vacuous pass this sweep would otherwise report forever.
if ! profile_reader_available; then
  fail "this machine cannot read a profile (python3 with PyYAML), so what the endurance run declares could not be asked for"
  printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
  exit 1
fi

walk="$(profile_phases "$PROFILE")"
declared_arrivals="$(jq '[.[] | select(.arrivals_per_second > 0)] | length' <<<"$walk")"
declared_sessions="$(jq '[.[] | select(.sessions > 0)] | length' <<<"$walk")"

if [ "$declared_arrivals" -gt 0 ]; then
  pass "the profile declares technician arrivals in $declared_arrivals phase(s)"
else
  fail "the profile declares no technician arrivals, so the checks below hold it to nothing"
fi
if [ "$declared_sessions" -gt 0 ]; then
  pass "the profile declares held sessions in $declared_sessions phase(s)"
else
  fail "the profile declares no sessions, so the operation this family's leak attaches to is not in its shape"
fi

# --- the job runs a browser-side generator beside the walk --------------------
#
# The scenarios are read off the invocation rather than listed here, so a
# scenario added to or taken off the leg is judged by what it offers rather
# than by a table somebody has to keep level.
alongside="$(grep -oE 'loadtest-k6-alongside\.sh[^&]*' <<<"$workflow" | head -1 || true)"
if [ -n "$alongside" ]; then
  pass "the endurance job runs a browser-side generator beside the walk"
else
  fail "the endurance job offers no technician load, so the numbers its profile declares describe an intention rather than a fact"
fi

# Everything after the script and the harness's output path is a scenario.
scenarios=()
read -r -a alongside_words <<<"$alongside"
for word in "${alongside_words[@]:2}"; do
  case "$word" in
    # A flag, a backgrounding ampersand or a line continuation is not a name.
    -* | '' | '&' | \\) continue ;;
    *) scenarios+=("$word") ;;
  esac
done

if [ "${#scenarios[@]}" -gt 0 ]; then
  pass "the leg names ${#scenarios[@]} scenario(s)"
else
  fail "the leg names no scenario, so it starts a generator with nothing to offer"
fi

# --- what it offers covers what the profile declares --------------------------
#
# A scenario's executor says which of the two technician numbers it offers:
# arrivals are a rate of journeys, sessions are a count held open. One does not
# stand in for the other — a run offering journeys and no session exercises
# every path but the one this family was written for.
offers_arrivals=no
offers_sessions=no
for scenario in "${scenarios[@]}"; do
  script="$SCENARIO_DIR/$scenario.js"
  if [ ! -f "$script" ]; then
    fail "the leg names scenario $scenario and there is no such script"
    continue
  fi
  grep -q 'arrivalScenarios' "$script" && offers_arrivals=yes
  grep -q 'sessionScenarios' "$script" && offers_sessions=yes
done

if [ "$declared_arrivals" -eq 0 ] || [ "$offers_arrivals" = yes ]; then
  pass "the technician arrivals the profile declares are offered"
else
  fail "the profile declares technician arrivals and no scenario on the leg offers a rate of journeys"
fi
if [ "$declared_sessions" -eq 0 ] || [ "$offers_sessions" = yes ]; then
  pass "the sessions the profile declares are held open"
else
  fail "the profile declares sessions and no scenario on the leg holds any open, so the operation this family's leak attaches to never happens"
fi

# --- the machine side answers ------------------------------------------------
if grep -q -- '-relay-sessions' <<<"$workflow"; then
  pass "the harness joins the machine side of a session and echoes"
else
  fail "the harness is not told to answer a session request, so the browser side times a frame nobody sends back and no session enters the conservation denominator"
fi

# --- the generator exists before it is asked for ------------------------------
if grep -q 'grafana/k6/releases/download' <<<"$workflow" \
  && grep -qE '^[[:space:]]*K6_VERSION:' <<<"$workflow"; then
  pass "the job fetches a pinned build of the generator it runs"
else
  fail "the job runs a browser-side generator it never installs"
fi

# --- offered and folded, both directions --------------------------------------
folded=0
for scenario in "${scenarios[@]}"; do
  if grep -q -- "--journeys[^|]*$scenario\.json" <<<"$workflow"; then
    folded=$((folded + 1))
    pass "$scenario's numbers are folded into the run's own evidence"
  else
    fail "$scenario is offered and its numbers are folded nowhere, so they reach a temp directory and stop"
  fi
done

# And back the other way: an export folded by a scenario the leg does not run
# is a fold of a file nothing wrote, which fails on the night rather than here.
while IFS= read -r export_name; do
  [ -n "$export_name" ] || continue
  runs=no
  for scenario in "${scenarios[@]}"; do
    [ "$scenario" = "$export_name" ] && runs=yes
  done
  if [ "$runs" = yes ]; then
    pass "the export folded for $export_name is one the leg produces"
  else
    fail "the job folds $export_name's export and the leg never runs it"
  fi
done < <(grep -oE '\-\-journeys [^ ]+' <<<"$workflow" \
  | sed -nE 's|.*/([a-z0-9-]+)\.json.*|\1|p' | sort -u)

if [ "$folded" -gt 0 ]; then
  pass "read $folded folded scenario(s) off the job"
else
  fail "no folded scenario was read, so this sweep checked nothing"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf 'Failures:\n' >&2
  for f in "${FAILURES[@]}"; do printf '  - %s\n' "$f" >&2; done
  exit 1
fi
