#!/usr/bin/env bash
# Every tracked npm lockfile is audited, locally and in CI.
# `npm audit` reads only the lockfile in the directory it runs from.

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

summarize() {
  echo
  echo "Summary: $PASS passed, $FAIL failed"
  if [ "$FAIL" -gt 0 ]; then
    printf '  - %s\n' "${FAILURES[@]}" >&2
    exit 1
  fi
  exit 0
}

echo "npm-audit-scope:"

for f in "$GAUNTLET" "$CI"; do
  if [ ! -f "$f" ]; then
    fail "$(basename "$f") is readable"
    summarize
  fi
done

# Tracked lockfiles only; an untracked scratch tree ships no dependencies.
mapfile -t LOCKFILES < <(cd "$ROOT" && git ls-files '*package-lock.json' | sort)

if [ "${#LOCKFILES[@]}" -eq 0 ]; then
  fail "the repository tracks at least one package-lock.json"
else
  pass "found ${#LOCKFILES[@]} tracked lockfile(s): ${LOCKFILES[*]}"
fi

# Only the audit step's own text counts, so a directory named elsewhere cannot satisfy it.
CI_AUDIT_STEP="$(awk '
  /^      - name: npm vulnerability check/ { on = 1; next }
  on && /^      - name: / { exit }
  on { print }
' "$CI")"

if [ -z "$CI_AUDIT_STEP" ]; then
  fail "ci.yml has an 'npm vulnerability check' step the audited directories can be read from"
elif grep -q 'npm ci' <<<"$CI_AUDIT_STEP"; then
  # An audit reads the lockfile an install produced.
  pass "ci.yml installs before auditing"
else
  fail "ci.yml's npm vulnerability check runs no npm ci — the audit would read an uninstalled tree"
fi

if grep -qF 'scripts/npm-audit.sh' <<<"$CI_AUDIT_STEP" \
  && ! grep -qE '(^|[;&|(]|then|do)[[:space:]]*npm audit' <<<"$CI_AUDIT_STEP"; then
  pass "ci.yml audits through scripts/npm-audit.sh, with no audit of its own"
else
  fail "ci.yml's npm vulnerability check must call scripts/npm-audit.sh and run no npm audit itself"
fi
if grep -qE '(^|[;&|(]|then|do)[[:space:]]*npm audit' "$GAUNTLET"; then
  fail "the gauntlet runs an npm audit of its own beside scripts/npm-audit.sh"
else
  pass "the gauntlet audits only through scripts/npm-audit.sh"
fi

# The script reads the report with jq, so the job installs the pinned one first.
SECURITY_JOB="$(awk '/^  security-audit:/ { on = 1; next } on && /^  [A-Za-z0-9_-]+:[[:space:]]*$/ { on = 0 } on' "$CI")"
if grep -qF 'setup-pinned-tools' <<<"$SECURITY_JOB"; then
  pass "ci.yml's security-audit job sets up the pinned jq"
else
  fail "ci.yml's security-audit job runs scripts/npm-audit.sh without the pinned jq"
fi

for lock in "${LOCKFILES[@]}"; do
  dir="$(dirname "$lock")"

  if grep -qF "scripts/npm-audit.sh $dir" "$GAUNTLET"; then
    pass "gauntlet audits $dir"
  else
    fail "gauntlet does not audit $dir — add scripts/npm-audit.sh $dir to scripts/precommit-gauntlet.sh"
  fi

  # The step may name directories inline or from a list, so any mention in the step counts.
  if grep -qF "$dir" <<<"$CI_AUDIT_STEP"; then
    pass "ci.yml audits $dir"
  else
    fail "ci.yml does not audit $dir — add it to the npm vulnerability check in .github/workflows/ci.yml"
  fi
done

summarize
