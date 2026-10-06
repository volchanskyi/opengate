#!/usr/bin/env bash
# The scanner takes its merge base from the local refs/heads/<branch>, so a stale one reads old
# history as new code. Sourced by scripts/precommit-gauntlet.sh; the check logs to stderr.

# Returns 0 when the local branch is level with or ahead of its remote ref, 1 when behind or unread.
# Ahead leaves the merge base correct; only behind moves the boundary.
sonar_reference_branch_check() {
  local root="$1" branch="$2"
  local local_ref="refs/heads/$branch" remote_ref="refs/remotes/origin/$branch"
  local local_tip remote_tip

  if ! git -C "$root" rev-parse --git-dir >/dev/null 2>&1; then
    printf '✗ %s is not a git repository, so whether its reference branch is current cannot be read.\n' \
      "$root" >&2
    return 1
  fi

  # Without a local branch the scanner uses the remote-tracking ref, which a fetch keeps current.
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
