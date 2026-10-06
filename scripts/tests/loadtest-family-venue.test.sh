#!/usr/bin/env bash
# Holds every load profile to a workflow that runs it, and the load-test page's table to both.
# Run: ./scripts/tests/loadtest-family-venue.test.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
WORKFLOW_DIR="$REPO_ROOT/.github/workflows"
PROFILE_DIR="$REPO_ROOT/load/profiles"
TESTING_DOC="$REPO_ROOT/docs/infrastructure/Non-Functional-Testing.md"

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

profiles=()
while IFS= read -r path; do
  profiles+=("$(basename "$path" .yaml)")
done < <(find "$PROFILE_DIR" -name '*.yaml' | sort)

if [ "${#profiles[@]}" -gt 0 ]; then
  pass "there are ${#profiles[@]} profiles to check"
else
  fail "no profiles were found, so this sweep checked nothing"
fi

# A path a workflow assembles at run time from a matrix value is invisible here, so the matrix
# spells its profile paths out.
workflows="$(cat "$WORKFLOW_DIR"/*.yml)"

for profile in "${profiles[@]}"; do
  if grep -qF "load/profiles/$profile.yaml" <<<"$workflows"; then
    pass "$profile is run by a workflow"
  else
    fail "$profile is named by no workflow — schedule it, or delete it"
  fi
done

doc="$(cat "$TESTING_DOC")"

# A row's label may carry a digit and comma, as in "Volume 8,000".
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

  linked_profile="$(sed -n 's|.*(\.\./\.\./load/profiles/\([a-z0-9-]*\)\.yaml).*|\1|p' <<<"$row")"
  if [ -n "$linked_profile" ] && [ -f "$PROFILE_DIR/$linked_profile.yaml" ]; then
    pass "$family links a profile that exists"
  else
    fail "$family links load/profiles/${linked_profile:-?}.yaml, which is not there"
  fi

  linked_workflow="$(sed -n 's|.*(\.\./\.\./\.github/workflows/\([a-z-]*\.yml\)).*|\1|p' <<<"$row")"
  if [ -n "$linked_workflow" ] && [ -f "$WORKFLOW_DIR/$linked_workflow" ]; then
    pass "$family names $linked_workflow, which exists"
  else
    fail "$family names .github/workflows/${linked_workflow:-?}, which is not there"
  fi

  if [ -n "$linked_workflow" ] && [ -f "$WORKFLOW_DIR/$linked_workflow" ] \
    && grep -qF "load/profiles/$linked_profile.yaml" "$WORKFLOW_DIR/$linked_workflow"; then
    pass "$linked_workflow really runs $linked_profile"
  else
    fail "$linked_workflow does not name load/profiles/$linked_profile.yaml"
  fi
done <<<"$table_rows"

declares_sessions() {
  python3 "$SCRIPT_DIR/fixtures/profile-declares-sessions.py" "$PROFILE_DIR/$1.yaml"
}

# venue_of prints the text of the workflow job that names the profile's path.
venue_of() {
  python3 "$SCRIPT_DIR/fixtures/profile-venue.py" "$WORKFLOW_DIR" "load/profiles/$1.yaml"
}

# `breakpoint` raises the fleet to sixteen thousand machines on a shared runner, so its venue
# offers no technician sessions yet.
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
