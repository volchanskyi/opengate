#!/usr/bin/env bash
# Every npm lockfile in the tree is audited, locally and in CI.
#
# `npm audit` reads one lockfile: the one in the directory it runs from. The
# gauntlet and the CI security job each named `web/`, so a second lockfile added
# elsewhere was covered by neither, and the packages under it aged out of the
# advisory database with no gate anywhere reporting it. That is how
# tools/mermaid-validate sat on a flagged dompurify and mermaid while both
# audits passed: nothing was excluded, the directory was simply never named.
#
# So the list is derived rather than maintained. This finds every lockfile the
# repository tracks and requires both audit sites to run against each one, which
# means a new lockfile fails this gate on the commit that adds it rather than
# going unwatched.
#
# Run: ./scripts/tests/npm-audit-scope.test.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
GAUNTLET="$ROOT/scripts/precommit-gauntlet.sh"
CI="$ROOT/.github/workflows/ci.yml"

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

echo "npm-audit-scope:"

for f in "$GAUNTLET" "$CI"; do
  if [ ! -f "$f" ]; then
    fail "$(basename "$f") is readable"
    echo
    echo "Summary: $PASS passed, $FAIL failed"
    printf '  - %s\n' "${FAILURES[@]}" >&2
    exit 1
  fi
done

# Tracked lockfiles only: a scratch tree a mutation run leaves behind is not a
# dependency set anybody ships.
mapfile -t LOCKFILES < <(cd "$ROOT" && git ls-files '*package-lock.json' | sort)

if [ "${#LOCKFILES[@]}" -eq 0 ]; then
  fail "the repository tracks at least one package-lock.json"
else
  pass "found ${#LOCKFILES[@]} tracked lockfile(s): ${LOCKFILES[*]}"
fi

# The npm vulnerability check step, isolated so a directory named anywhere else
# in the workflow cannot stand in for one this step actually audits.
CI_AUDIT_STEP="$(awk '
  /^      - name: npm vulnerability check/ { on = 1; next }
  on && /^      - name: / { exit }
  on { print }
' "$CI")"

if [ -z "$CI_AUDIT_STEP" ]; then
  fail "ci.yml has an 'npm vulnerability check' step the audited directories can be read from"
elif grep -q 'npm ci' <<<"$CI_AUDIT_STEP"; then
  # An audit reads the lockfile an install produced, so the step has to install
  # what it is about to report on.
  pass "ci.yml installs before auditing"
else
  fail "ci.yml's npm vulnerability check runs no npm ci — the audit would read an uninstalled tree"
fi

for lock in "${LOCKFILES[@]}"; do
  dir="$(dirname "$lock")"

  if grep -qF "cd $dir && npm audit" "$GAUNTLET"; then
    pass "gauntlet audits $dir"
  else
    fail "gauntlet does not audit $dir — add it to the npm audit step in scripts/precommit-gauntlet.sh"
  fi

  # The step may name its directories inline or drive them from a list, so the
  # check is that the directory is named in the step at all — not the shape of
  # the shell around it.
  if grep -qF "$dir" <<<"$CI_AUDIT_STEP"; then
    pass "ci.yml audits $dir"
  else
    fail "ci.yml does not audit $dir — add it to the npm vulnerability check in .github/workflows/ci.yml"
  fi
done

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
