#!/usr/bin/env bash
# sonar-reference-branch.sh — keeps the workstation's copy of the reference
# branch level with the remote, so a local scan measures this change rather than
# a season of history.
#
# Sourced by scripts/precommit-gauntlet.sh (and by
# scripts/tests/sonar-reference-branch.test.sh). NOT executable on its own —
# this is a library of bash functions.
#
# Why it exists: on SonarCloud `dev` is a short-lived branch, so its new code is
# everything since it left the long-lived branch above it, and the boundary is
# the merge base between the two. The scanner computes that merge base from the
# repository it is handed, and it resolves the reference branch by name — which
# finds the local `refs/heads/main`, not the remote-tracking ref. Nothing on a
# workstation ever needs that local branch, so nothing updates it, and nothing
# reads it either.
#
# What that costs: the merge base falls back to wherever the two last agreed. On
# this repository the local `main` had not moved since April, so a scan run in
# September measured five months as new code and failed the gate on thirteen
# findings and eighty-five smells from files the change had never opened. The
# three blame-independent guards beside it all passed, correctly, because they
# judge the files the change touched. And CI could not reproduce any of it: a
# fresh checkout has a current reference branch by construction, so the workstation
# is the only place the question is ever asked wrongly.
#
# .claude/rules/git.md already says to pull `main` before starting work, for the
# separate reason that Dependabot's security updates land there and never reach
# `dev`. This is the same command, checked rather than remembered.
#
# Functions exported:
#   sonar_reference_branch_check REPO_ROOT BRANCH   — the gate; logs to stderr

# sonar_reference_branch_check REPO_ROOT BRANCH — 0 when the local reference
# branch is level with or ahead of its remote-tracking ref, 1 when it is behind
# or the refs could not be read.
#
# Ahead is not drift: between a commit and its push every workstation is ahead,
# and the merge base is correct throughout. Only behind moves the boundary.
sonar_reference_branch_check() {
  local root="$1" branch="$2"
  local local_ref="refs/heads/$branch" remote_ref="refs/remotes/origin/$branch"
  local local_tip remote_tip

  if ! git -C "$root" rev-parse --git-dir >/dev/null 2>&1; then
    printf '✗ %s is not a git repository, so whether its reference branch is current cannot be read.\n' \
      "$root" >&2
    return 1
  fi

  # No local branch is no problem: the scanner then resolves the
  # remote-tracking ref, which a fetch keeps current.
  if ! local_tip="$(git -C "$root" rev-parse --verify --quiet "$local_ref")"; then
    return 0
  fi
  if ! remote_tip="$(git -C "$root" rev-parse --verify --quiet "$remote_ref")"; then
    return 0
  fi

  if [ "$local_tip" = "$remote_tip" ]; then
    return 0
  fi
  if git -C "$root" merge-base --is-ancestor "$remote_tip" "$local_tip" 2>/dev/null; then
    return 0
  fi

  local behind
  behind="$(git -C "$root" rev-list --count "$local_ref..$remote_ref" 2>/dev/null || echo '?')"
  printf '✗ the local %s branch is %s commit(s) behind origin/%s.\n' "$branch" "$behind" "$branch" >&2
  printf '  SonarCloud measures new code from the merge base with %s, and the scanner reads that\n' "$branch" >&2
  printf '  branch from this repository — so the scan would report months of history as this\n' >&2
  printf "  change's, and fail the gate on findings in files it never opened.\n" >&2
  printf '  Fix with:  git fetch origin %s:%s\n' "$branch" "$branch" >&2
  return 1
}
