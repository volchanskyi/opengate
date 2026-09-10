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

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf 'Failures:\n' >&2
  for f in "${FAILURES[@]}"; do printf '  - %s\n' "$f" >&2; done
  exit 1
fi
