#!/usr/bin/env bash
# Every family runs somewhere, and the page that says so is checked against it.
#
# Five of the seven profiles were named by nothing at all — not by a workflow,
# not by a script, not by the harness. `soak.yaml` declared eight hours and ran
# never; `breakpoint.yaml` was the only thing that would establish where the
# system gives out and it ran never; `spike.yaml` was the burst-recovery case
# and it ran never. Meanwhile docs/infrastructure/Testing.md placed them on
# "staging at night" and "staging overnight" in the present tense.
#
# scripts/tests/docs-live-state.test.sh structurally cannot catch that. Its
# phrase list looks for past-state narration — a thing described after it was
# removed — and this is the opposite: a thing described before it ever existed.
# So the check is here, and it reads both directions, because either one alone
# is satisfied by an empty table.
#
# Run: ./scripts/tests/loadtest-family-venue.test.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
WORKFLOW_DIR="$REPO_ROOT/.github/workflows"
PROFILE_DIR="$REPO_ROOT/load/profiles"
TESTING_DOC="$REPO_ROOT/docs/infrastructure/Testing.md"

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

echo "loadtest family venue:"

# --- Every profile is run by some workflow ------------------------------------
#
# A profile nothing names is a shape nobody has ever applied. It costs nothing
# to keep and it reads, to everyone downstream, as coverage.

profiles=()
while IFS= read -r path; do
  profiles+=("$(basename "$path" .yaml)")
done < <(find "$PROFILE_DIR" -name '*.yaml' | sort)

if [ "${#profiles[@]}" -gt 0 ]; then
  pass "there are ${#profiles[@]} profiles to check"
else
  fail "no profiles were found, so this sweep checked nothing"
fi

# The whole workflow directory read once. A path a workflow assembles at run
# time from a matrix value is invisible here, which is why the matrix spells its
# profile paths out.
workflows="$(cat "$WORKFLOW_DIR"/*.yml)"

for profile in "${profiles[@]}"; do
  if grep -qF "load/profiles/$profile.yaml" <<<"$workflows"; then
    pass "$profile is run by a workflow"
  else
    fail "$profile is named by no workflow — schedule it, or delete it"
  fi
done

# --- Testing.md's table names workflows that exist ----------------------------
#
# The table is what a reader is told. A row pointing at a file that is not there
# is the same claim as a row pointing at nothing.

doc="$(cat "$TESTING_DOC")"

# A row's label is a name a reader recognises, and a sweep's points carry the
# number that distinguishes them — "Volume 8,000" is one row, not a malformed
# one.
table_rows="$(grep -E '^\| \[[A-Z][A-Za-z0-9,. ]*\]\(\.\./\.\./load/profiles/' <<<"$doc" || true)"
row_count="$(grep -c . <<<"${table_rows:-}" || true)"

if [ "${row_count:-0}" -eq "${#profiles[@]}" ]; then
  pass "the family table has one row per profile ($row_count)"
else
  fail "the family table has $row_count rows against ${#profiles[@]} profiles"
fi

while IFS= read -r row; do
  [ -n "$row" ] || continue
  family="$(sed -n 's/^| \[\([A-Za-z0-9,. ]*\)\].*/\1/p' <<<"$row")"

  # Every profile the row links must be a file that exists.
  linked_profile="$(sed -n 's|.*(\.\./\.\./load/profiles/\([a-z0-9-]*\)\.yaml).*|\1|p' <<<"$row")"
  if [ -n "$linked_profile" ] && [ -f "$PROFILE_DIR/$linked_profile.yaml" ]; then
    pass "$family links a profile that exists"
  else
    fail "$family links load/profiles/${linked_profile:-?}.yaml, which is not there"
  fi

  # And every workflow it names must be a file that exists, or the venue column
  # is describing a run that happens nowhere.
  linked_workflow="$(sed -n 's|.*(\.\./\.\./\.github/workflows/\([a-z-]*\.yml\)).*|\1|p' <<<"$row")"
  if [ -n "$linked_workflow" ] && [ -f "$WORKFLOW_DIR/$linked_workflow" ]; then
    pass "$family names $linked_workflow, which exists"
  else
    fail "$family names .github/workflows/${linked_workflow:-?}, which is not there"
  fi

  # The workflow it names must be one that actually runs that profile, or the
  # table is right about two facts and wrong about the pair.
  if [ -n "$linked_workflow" ] && [ -f "$WORKFLOW_DIR/$linked_workflow" ] \
    && grep -qF "load/profiles/$linked_profile.yaml" "$WORKFLOW_DIR/$linked_workflow"; then
    pass "$linked_workflow really runs $linked_profile"
  else
    fail "$linked_workflow does not name load/profiles/$linked_profile.yaml"
  fi
done <<<"$table_rows"

# --- A profile that declares sessions is run somewhere that opens them --------
#
# Every profile declares `operator_arrivals_per_second` and `sessions`, and both
# are technician-side numbers a machine-side harness cannot offer on its own.
# They are offered by a browser-side generator running beside the fleet, and the
# leg has to install one and fold what it timed back into the bundle.
#
# Three profiles declared them at a venue that did neither, so twenty held
# technician sessions apiece were a number in a file. A capacity ladder that
# opens no session finds the load a server gives out under for a load nobody
# runs — a technician remoted into a machine is the expensive thing the product
# does, and the rung it would give out at is not the rung the run reports.
#
# So a profile declares sessions and its venue offers them, or it declares none.

# declares_sessions PROFILE — does this profile ask for technician load in any
# phase? Read off the file rather than assumed, because the answer is the whole
# question.
declares_sessions() {
  python3 "$SCRIPT_DIR/fixtures/profile-declares-sessions.py" "$PROFILE_DIR/$1.yaml"
}

# venue_of PROFILE — the workflow job that names this profile's path, as the
# text of that job. A path a matrix assembles at run time is invisible here,
# which is why the matrix spells its profile paths out.
venue_of() {
  python3 "$SCRIPT_DIR/fixtures/profile-venue.py" "$WORKFLOW_DIR" "load/profiles/$1.yaml"
}

# The one venue deliberately still short, and the reason, beside the name.
# `breakpoint` raises the fleet until the server gives out: sixteen thousand
# machines on a shared runner, with a hundred and sixty held sessions on top.
# What that generator would need of the runner is a reading to take before the
# leg offers it, and the reading now exists on every other leg — so this entry
# comes out when it has been taken, rather than being kept.
SESSIONS_NOT_YET_OFFERED=(breakpoint)

exempt_from_sessions() {
  local candidate="$1" entry
  for entry in "${SESSIONS_NOT_YET_OFFERED[@]}"; do
    [ "$entry" = "$candidate" ] && return 0
  done
  return 1
}

checked_venues=0
for profile in "${profiles[@]}"; do
  wants="$(declares_sessions "$profile")"
  [ "$wants" = "yes" ] || continue
  if exempt_from_sessions "$profile"; then
    pass "$profile is the one venue whose generator cost is still to be read"
    continue
  fi
  checked_venues=$((checked_venues + 1))
  venue="$(venue_of "$profile")"
  if [ -z "$venue" ]; then
    fail "$profile declares technician load and no job names it"
    continue
  fi
  # The generator that opens the sessions, and the step that gives what it timed
  # a reader. A leg that runs the generator and never folds its numbers in has
  # applied the load and thrown away the measurement.
  if grep -qF "Install k6" <<<"$venue"; then
    pass "$profile runs somewhere that installs the browser-side generator"
  else
    fail "$profile declares technician load at a venue that installs no generator"
  fi
  if grep -qF "loadtest-bundle-merge.sh" <<<"$venue"; then
    pass "$profile's venue folds what the generator timed into the leg"
  else
    fail "$profile's venue times technician load and folds none of it in"
  fi
done

if [ "$checked_venues" -gt 0 ]; then
  pass "the sweep reached $checked_venues profiles declaring technician load"
else
  fail "the sweep reached no profile declaring technician load, so it checked nothing"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf 'Failures:\n' >&2
  for f in "${FAILURES[@]}"; do printf '  - %s\n' "$f" >&2; done
  exit 1
fi
