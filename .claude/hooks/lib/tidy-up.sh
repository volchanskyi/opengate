#!/usr/bin/env bash
# Each step writes a marker naming the content it ran on, and the next step refuses unless the
# content on disk is what that marker names.

TIDY_MARKERS=(gauntlet.pass refactor.start refactor.done refactor.head)

tidy_root() { git rev-parse --show-toplevel 2>/dev/null; }

tidy_marker_path() { printf '%s/.claude/.markers/%s\n' "$(tidy_root)" "$1"; }

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

# Hashes the working tree (tracked and untracked files, from disk) in a throwaway index.
# The markers are left out so writing a proof does not change what it proves.
tidy_fingerprint() {
  local root
  root="$(tidy_root)" || return 1
  (
    cd "$root" || exit 1
    real="$(git rev-parse --git-path index)"
    idx="$(mktemp)"
    trap 'rm -f "$idx"' EXIT
    if [ -f "$real" ]; then cp "$real" "$idx"; else rm -f "$idx"; fi
    # The markers are dropped after the add because git add errors on an excluded ignored path.
    markers=()
    for marker in "${TIDY_MARKERS[@]}"; do
      markers+=(".claude/.markers/$marker")
    done
    GIT_INDEX_FILE="$idx" git add -A -- . >/dev/null 2>&1 || exit 1
    GIT_INDEX_FILE="$idx" git rm -q --cached --ignore-unmatch -- "${markers[@]}" >/dev/null 2>&1 || exit 1
    GIT_INDEX_FILE="$idx" git write-tree
  )
}

# Records a pass only while the content on disk is still the content the checks began on.
tidy_gauntlet_passed() {
  local start="$1" now
  now="$(tidy_fingerprint)" || return 1
  if [ -z "$start" ] || [ "$now" != "$start" ]; then
    return 1
  fi
  tidy_write gauntlet.pass "$now"
}

tidy_done_matches() {
  local finished now
  finished="$(tidy_read refactor.done)"
  [ -n "$finished" ] || return 1
  now="$(tidy_fingerprint)" || return 1
  [ "$finished" = "$now" ]
}
