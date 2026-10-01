#!/usr/bin/env bash
# The proof that a commit's content was checked and then tidied, in that order.
#
# Sourced by scripts/precommit-gauntlet.sh, scripts/refactor-gate.sh, the commit
# guard and the post-commit hook. Each step writes one marker naming the content
# it ran on, and the next step refuses unless the content in front of it is the
# content that marker names:
#
#   gauntlet.pass   every check passed on this content
#   refactor.start  /refactor began on content the checks had passed
#   refactor.done   /refactor finished, and left this content
#   refactor.head   the commit carrying that content, which the push guard reads
#
# Content is named by tidy_fingerprint, so a marker is about what is on disk —
# not about a commit, and not about what happens to be staged.

TIDY_MARKERS=(gauntlet.pass refactor.start refactor.done refactor.head)

tidy_root() { git rev-parse --show-toplevel 2>/dev/null; }

tidy_marker_path() { printf '%s/.claude/.markers/%s\n' "$(tidy_root)" "$1"; }

# tidy_read NAME — the content a marker names, or nothing when there is none.
tidy_read() {
  local path
  path="$(tidy_marker_path "$1")"
  if [ -f "$path" ]; then cat "$path"; fi
}

tidy_write() {
  local path
  path="$(tidy_marker_path "$1")"
  mkdir -p "$(dirname "$path")"
  printf '%s\n' "$2" >"$path"
}

tidy_clear() { rm -f "$(tidy_marker_path "$1")"; }

# tidy_fingerprint — the tree object of the working tree as it stands: every
# tracked file and every untracked file not ignored, read from disk, whatever is
# staged. Built in a throwaway index seeded from the real one, so only files
# whose stat changed are hashed again and the real index is never touched. The
# markers themselves are left out, or writing a proof would change what it
# proves.
tidy_fingerprint() {
  local root
  root="$(tidy_root)" || return 1
  (
    cd "$root" || exit 1
    real="$(git rev-parse --git-path index)"
    idx="$(mktemp)"
    trap 'rm -f "$idx"' EXIT
    if [ -f "$real" ]; then cp "$real" "$idx"; else rm -f "$idx"; fi
    # The markers are dropped after the add rather than excluded from it: an
    # exclusion naming an ignored path is an error to `git add`.
    markers=()
    for marker in "${TIDY_MARKERS[@]}"; do
      markers+=(".claude/.markers/$marker")
    done
    GIT_INDEX_FILE="$idx" git add -A -- . >/dev/null 2>&1 || exit 1
    GIT_INDEX_FILE="$idx" git rm -q --cached --ignore-unmatch -- "${markers[@]}" >/dev/null 2>&1 || exit 1
    GIT_INDEX_FILE="$idx" git write-tree
  )
}

# tidy_gauntlet_passed START — record a pass for the content the checks began
# on, provided it is still the content on disk. A tree edited while the checks
# ran is not the tree they passed, so it records nothing and says so.
tidy_gauntlet_passed() {
  local start="$1" now
  now="$(tidy_fingerprint)" || return 1
  if [ -z "$start" ] || [ "$now" != "$start" ]; then
    return 1
  fi
  tidy_write gauntlet.pass "$now"
}

# tidy_done_matches — did /refactor finish on exactly the content on disk now?
tidy_done_matches() {
  local finished now
  finished="$(tidy_read refactor.done)"
  [ -n "$finished" ] || return 1
  now="$(tidy_fingerprint)" || return 1
  [ "$finished" = "$now" ]
}
