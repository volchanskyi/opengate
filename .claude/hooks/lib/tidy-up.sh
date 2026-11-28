#!/usr/bin/env bash
# Each step writes a marker naming the content it ran on, and the next step refuses unless the
# content on disk is what that marker names.

TIDY_MARKERS=(refactor.start refactor.done refactor.head gauntlet.pass)

# Seconds a gauntlet pass covers its content, counted from the run's start, when the advisory
# lookups happen.
GAUNTLET_PASS_SECONDS=3600

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

tidy_done_matches() {
  local finished now
  finished="$(tidy_read refactor.done)"
  [ -n "$finished" ] || return 1
  now="$(tidy_fingerprint)" || return 1
  [ "$finished" = "$now" ]
}

# Clears the last pass and prints what a run begins on: the content fingerprint and the second.
gauntlet_pass_begin() {
  local now
  tidy_clear gauntlet.pass
  now="$(tidy_fingerprint)" || return 1
  printf '%s %s\n' "$now" "$(date +%s)"
}

# Writes the pass for a run that began as BEGUN, only while the content is still what it began on.
gauntlet_pass_record() {
  local begun="$1" now
  now="$(tidy_fingerprint)" || return 1
  [ -n "$begun" ] && [ "${begun%% *}" = "$now" ] || return 1
  tidy_write gauntlet.pass "$begun"
}

gauntlet_pass_matches() {
  local pass started now
  pass="$(tidy_read gauntlet.pass)"
  [ -n "$pass" ] || return 1
  started="${pass#* }"
  [[ "$started" =~ ^[0-9]+$ ]] || return 1
  now="$(tidy_fingerprint)" || return 1
  [ "${pass%% *}" = "$now" ] && [ $(($(date +%s) - started)) -le "$GAUNTLET_PASS_SECONDS" ]
}

# Both records a commit needs: the finished tidy-up and a gauntlet pass, each naming this content.
commit_proofs_match() { tidy_done_matches && gauntlet_pass_matches; }
