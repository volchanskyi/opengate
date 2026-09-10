#!/usr/bin/env bash
# Tests for scripts/lib/sonar-reference-branch.sh. Plain bash; no bats.
# Run: ./scripts/tests/sonar-reference-branch.test.sh
#
# The library's job is to catch a local reference branch that has fallen behind
# the remote it names. Every case here builds a throwaway repository, so the
# suite says nothing about the machine it runs on and cannot pass by accident on
# a workstation that happens to be level.
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

# record REPO MESSAGE — lands one commit so two branches can differ.
record() {
  local repo="$1" message="$2"
  printf '%s\n' "$message" >>"$repo/log.txt"
  git -C "$repo" add log.txt
  git -C "$repo" commit -q -m "$message"
}

# a_repo NAME — a repository whose `main` branch and `origin/main` remote ref
# both exist and agree, which is the shape a fresh clone has. Work happens on
# `dev`, as it does here.
a_repo() {
  local repo="$WORK/$1"
  mkdir -p "$repo"
  git -C "$repo" init -q -b main
  git -C "$repo" config user.email fixture@example.invalid
  git -C "$repo" config user.name Fixture
  record "$repo" "first"
  # A remote-tracking ref with no remote behind it: the check reads refs and
  # never the network, so a fixture that reaches nothing is the honest one.
  git -C "$repo" update-ref refs/remotes/origin/main "$(git -C "$repo" rev-parse main)"
  git -C "$repo" checkout -q -b dev
  printf '%s\n' "$repo"
}

# put_remote_ahead REPO — moves the remote-tracking ref one commit past the
# local branch without touching the working tree, which is what a fetch does
# when somebody else has pushed.
put_remote_ahead() {
  local repo="$1" tip
  tip="$(git -C "$repo" rev-parse main)"
  git -C "$repo" update-ref refs/remotes/origin/main \
    "$(git -C "$repo" commit-tree -m ahead -p "$tip" "$tip^{tree}")"
}

echo "sonar reference branch:"

# The everyday case: the local ref names the commit the remote does.
LEVEL="$(a_repo level)"
expect_rc "a level reference branch passes" 0 sonar_reference_branch_check "$LEVEL" main

# The defect. The scanner resolves its merge base against the local reference
# branch, so one left behind turns new code into every commit since the two last
# agreed — five months of it here, reported as this change's.
BEHIND="$(a_repo behind)"
put_remote_ahead "$BEHIND"
expect_rc "a reference branch behind its remote fails" 1 sonar_reference_branch_check "$BEHIND" main

# A refusal a reader cannot act on costs the same gauntlet twice, so it carries
# the command that fixes it.
REMEDY="$(sonar_reference_branch_check "$BEHIND" main 2>&1)"
if grep -qF -- "git fetch origin main:main" <<<"$REMEDY"; then
  pass "the refusal names the command that fixes it"
else
  fail "the refusal names the command that fixes it (got: $REMEDY)"
fi

# A reference branch ahead of its remote is an ordinary local commit, not drift.
# Refusing it would refuse every workstation between a commit and its push.
AHEAD="$(a_repo ahead)"
git -C "$AHEAD" checkout -q main
record "$AHEAD" "second"
git -C "$AHEAD" checkout -q dev
expect_rc "a reference branch ahead of its remote passes" 0 sonar_reference_branch_check "$AHEAD" main

# Nothing to compare is not a failure. With no local reference branch the
# scanner resolves the remote-tracking ref, which is current by construction, so
# the question this asks does not arise.
NOLOCAL="$(a_repo nolocal)"
git -C "$NOLOCAL" branch -q -D main
expect_rc "no local reference branch passes" 0 sonar_reference_branch_check "$NOLOCAL" main

# A check that cannot ask must not answer yes: a directory that is not a
# repository is a broken invocation, not a level workstation.
expect_rc "a check that cannot read the refs fails" 1 sonar_reference_branch_check "$WORK/absent" main

echo
if [ "$FAIL" -gt 0 ]; then
  echo "Summary: $PASS passed, $FAIL failed" >&2
  for f in "${FAILURES[@]}"; do echo "  - $f" >&2; done
  exit 1
fi
echo "Summary: $PASS passed, 0 failed"
