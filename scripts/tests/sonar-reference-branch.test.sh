#!/usr/bin/env bash
# Each case builds a throwaway repository to catch a reference branch behind its remote.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LIB="$SCRIPT_DIR/../lib/sonar-reference-branch.sh"

if [ ! -f "$LIB" ]; then
  echo "FAIL: $LIB not found" >&2
  exit 1
fi

# shellcheck source=/dev/null
. "$LIB"

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

expect_rc() {
  local name="$1" want="$2"
  shift 2
  local got=0
  "$@" >/dev/null 2>&1 || got=$?
  if [ "$got" -eq "$want" ]; then
    pass "$name"
  else
    fail "$name (wanted exit $want, got $got)"
  fi
}

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

record() {
  local repo="$1" message="$2"
  printf '%s\n' "$message" >>"$repo/log.txt"
  git -C "$repo" add log.txt
  git -C "$repo" commit -q -m "$message"
}

# The repository starts with `main` and `origin/main` in agreement, and work happens on `dev`.
a_repo() {
  local repo="$WORK/$1"
  mkdir -p "$repo"
  git -C "$repo" init -q -b main
  git -C "$repo" config user.email fixture@example.invalid
  git -C "$repo" config user.name Fixture
  record "$repo" "first"
  # The check reads refs only, so the remote-tracking ref needs no remote behind it.
  git -C "$repo" update-ref refs/remotes/origin/main "$(git -C "$repo" rev-parse main)"
  git -C "$repo" checkout -q -b dev
  printf '%s\n' "$repo"
}

# Moves the remote-tracking ref one commit past the local branch, as a fetch does.
put_remote_ahead() {
  local repo="$1" tip
  tip="$(git -C "$repo" rev-parse main)"
  git -C "$repo" update-ref refs/remotes/origin/main \
    "$(git -C "$repo" commit-tree -m ahead -p "$tip" "$tip^{tree}")"
}

echo "sonar reference branch:"

LEVEL="$(a_repo level)"
expect_rc "a level reference branch passes" 0 sonar_reference_branch_check "$LEVEL" main

# The scanner takes its merge base from the local reference branch; a stale one inflates new code.
BEHIND="$(a_repo behind)"
put_remote_ahead "$BEHIND"
expect_rc "a reference branch behind its remote fails" 1 sonar_reference_branch_check "$BEHIND" main

REMEDY="$(sonar_reference_branch_check "$BEHIND" main 2>&1)"
if grep -qF -- "git fetch origin main:main" <<<"$REMEDY"; then
  pass "the refusal names the command that fixes it"
else
  fail "the refusal names the command that fixes it (got: $REMEDY)"
fi

# A reference branch ahead of its remote is a local commit awaiting its push.
AHEAD="$(a_repo ahead)"
git -C "$AHEAD" checkout -q main
record "$AHEAD" "second"
git -C "$AHEAD" checkout -q dev
expect_rc "a reference branch ahead of its remote passes" 0 sonar_reference_branch_check "$AHEAD" main

# With no local reference branch the scanner resolves the remote-tracking ref.
NOLOCAL="$(a_repo nolocal)"
git -C "$NOLOCAL" branch -q -D main
expect_rc "no local reference branch passes" 0 sonar_reference_branch_check "$NOLOCAL" main

expect_rc "a check that cannot read the refs fails" 1 sonar_reference_branch_check "$WORK/absent" main

echo
if [ "$FAIL" -gt 0 ]; then
  echo "Summary: $PASS passed, $FAIL failed" >&2
  for f in "${FAILURES[@]}"; do echo "  - $f" >&2; done
  exit 1
fi
echo "Summary: $PASS passed, 0 failed"
