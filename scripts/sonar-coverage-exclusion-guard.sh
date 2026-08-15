#!/usr/bin/env bash
# sonar-coverage-exclusion-guard.sh — keeps sonar.coverage.exclusions true as
# files move.
#
# `sonar.coverage.exclusions` holds the IO/transport modules that integration
# tests cover rather than unit tests. Two ordinary edits silently drop a file out
# of that set, and neither is visible in a local `make sonar`:
#
#   1. Splitting an excluded file. The carved-out half is a NEW path, so it is
#      not excluded, and git blame dates every relocated line to the split — the
#      code becomes brand-new uncovered code in one step. Splitting server.go
#      into server_connection.go put 124 lines of the QUIC accept path into new
#      code at 3% coverage and dropped new_coverage to 31.7% (CI run
#      31904922362). SonarCloud derives "new" from blame, so a pre-commit scan —
#      which runs before the commit exists — cannot see those lines as new: the
#      local gate and the new_coverage margin guard both read green.
#   2. Renaming or deleting an excluded file, which leaves a listed path
#      matching nothing.
#
# This guard is blame-independent: it reads the exclusion list and the diff, not
# SonarCloud, so it fires at the commit that causes the drift rather than in CI.
#
# Checks:
#   inheritance   — a file added since the base that git detects as a rename/copy
#                   of an excluded file must itself be excluded, or be genuinely
#                   unit-testable and deliberately left in. The fix is one line in
#                   sonar-project.properties, or tests for the new file.
#   staleness     — every literal (non-glob) path in the list must exist.
#   justification — every entry is named in the JUSTIFICATIONS comment block above
#                   the property, so no exclusion can be added without writing why.
#   agreement     — the per-language ignore lists in ci.yml and
#                   precommit-gauntlet.sh match each other. A path exempt in every
#                   place coverage is enforced is measured by nothing at all.
#
# Env:
#   SCEG_PROPERTIES     sonar-project.properties path, default repo-root file.
#   SCEG_BASE           git ref the diff is taken against, default the
#                       merge-base with origin/dev.
#   SCEG_COPIES_OVERRIDE  test seam: newline-separated "source<TAB>newpath"
#                       lines (skips git; set-but-empty means "no copies").
#   SCEG_CI_WORKFLOW    ci.yml path, default under SCEG_ROOT.
#   SCEG_GAUNTLET       precommit-gauntlet.sh path, default under SCEG_ROOT.
#   SCEG_ROOT           directory literal paths are resolved against, default
#                       the properties file's directory.
#
# Exit codes: 0 = the exclusion list matches the tree;
#             1 = a carved-out file lost its exclusion, or a listed path is gone;
#             2 = prerequisite missing (no properties file).
set -uo pipefail

SCEG_PROPERTIES="${SCEG_PROPERTIES:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/sonar-project.properties}"
SCEG_ROOT="${SCEG_ROOT:-$(dirname "$SCEG_PROPERTIES")}"
SCEG_CI_WORKFLOW="${SCEG_CI_WORKFLOW:-$SCEG_ROOT/.github/workflows/ci.yml}"
SCEG_GAUNTLET="${SCEG_GAUNTLET:-$SCEG_ROOT/scripts/precommit-gauntlet.sh}"

# sceg_exclusions — print one sonar.coverage.exclusions pattern per line. The
# property spans several physical lines joined by trailing backslashes.
sceg_exclusions() {
  awk '
    /^sonar\.coverage\.exclusions=/ {
      collecting = 1
      sub(/^sonar\.coverage\.exclusions=/, "")
    }
    collecting {
      line = $0
      continues = (line ~ /\\$/)
      sub(/\\$/, "", line)
      count = split(line, parts, ",")
      for (i = 1; i <= count; i++) {
        gsub(/^[ \t]+|[ \t]+$/, "", parts[i])
        if (parts[i] != "") print parts[i]
      }
      if (!continues) exit
    }
  ' "$1"
}

# sceg_justified — print every pattern named in the JUSTIFICATIONS comment block
# that precedes the property. A line looks like "#   <pattern> — <reason>".
sceg_justified() {
  awk '
    /^# JUSTIFICATIONS/ { collecting = 1; next }
    collecting && /^sonar\.coverage\.exclusions=/ { exit }
    collecting && /^#[ \t]+[^ \t]/ {
      line = $0
      sub(/^#[ \t]+/, "", line)
      # The reason follows an em dash; everything before it is the pattern.
      idx = index(line, " — ")
      if (idx > 0) {
        pattern = substr(line, 1, idx - 1)
        gsub(/^[ \t]+|[ \t]+$/, "", pattern)
        if (pattern != "") print pattern
      }
    }
  ' "$1"
}

# sceg_rust_ignore <file> — print the cargo-llvm-cov --ignore-filename-regex value.
sceg_rust_ignore() {
  grep -oE -- "--ignore-filename-regex[= ]+[\"'][^\"']+[\"']" "$1" 2>/dev/null \
    | sed -E "s/.*[\"']([^\"']+)[\"']/\1/" | head -1
}

# sceg_go_ignore <file> — print the grep -v -E pattern applied to coverage.out.
sceg_go_ignore() {
  grep -oE -- "grep -v -E [\"'][^\"']+[\"'] coverage\.out" "$1" 2>/dev/null \
    | sed -E "s/grep -v -E [\"']([^\"']+)[\"'] coverage\.out/\1/" | head -1
}

# sceg_base — the committed ref the working tree is compared against.
sceg_base() {
  if [ -n "${SCEG_BASE:-}" ]; then
    printf '%s' "$SCEG_BASE"
    return 0
  fi
  git merge-base HEAD origin/dev 2>/dev/null || git rev-parse HEAD 2>/dev/null
}

# sceg_snapshot_worktree — build a throwaway index holding staged, unstaged and
# untracked changes, so the diff sees the split as it exists at /precommit time
# (before `git add`). Echoes the index path, or "" outside a work tree.
sceg_snapshot_worktree() {
  git rev-parse --is-inside-work-tree >/dev/null 2>&1 || {
    echo ""
    return 0
  }
  local index real
  index="$(mktemp)"
  real="$(git rev-parse --git-path index 2>/dev/null)"
  if [ -f "$real" ]; then cp "$real" "$index" 2>/dev/null || :; fi
  if GIT_INDEX_FILE="$index" git add -A >/dev/null 2>&1; then
    echo "$index"
  else
    rm -f "$index"
    echo ""
  fi
}

# sceg_copies — print "source<TAB>newpath" for every file the diff reports as a
# rename or copy of another file.
sceg_copies() {
  if [ -n "${SCEG_COPIES_OVERRIDE+x}" ]; then
    printf '%s' "$SCEG_COPIES_OVERRIDE" | grep -v '^$' || :
    return 0
  fi
  local base index
  base="$(sceg_base)"
  [ -n "$base" ] || return 0
  index="$(sceg_snapshot_worktree)"
  [ -n "$index" ] || return 0
  GIT_INDEX_FILE="$index" git diff --cached -M -C --find-copies-harder \
    --diff-filter=CR --name-status "$base" 2>/dev/null \
    | awk -F'\t' '/^[CR]/ { print $2 "\t" $3 }'
  rm -f "$index"
}

# sceg_is_excluded <path> <patterns...> — true when path is covered by a literal
# entry or a glob in the exclusion list.
sceg_is_excluded() {
  local path="$1"
  shift
  local pattern
  for pattern in "$@"; do
    [ "$pattern" = "$path" ] && return 0
    # Sonar's ** spans directory separators, which is what bash globbing does
    # under globstar; a single * does not, but treating both the same way only
    # ever makes this guard quieter about a file that is already excluded.
    # shellcheck disable=SC2053 # intentional glob match, not a literal compare
    [[ "$path" == $pattern ]] && return 0
  done
  return 1
}

sceg_main() {
  if [ ! -f "$SCEG_PROPERTIES" ]; then
    echo "✗ sonar-coverage-exclusion-guard: $SCEG_PROPERTIES not found." >&2
    return 2
  fi

  local patterns=()
  while IFS= read -r pattern; do
    patterns+=("$pattern")
  done < <(sceg_exclusions "$SCEG_PROPERTIES")

  local failed=0

  # Check 1 — a file carved out of an excluded file inherits its exclusion.
  local source new
  while IFS=$'\t' read -r source new; do
    [ -n "$new" ] || continue
    sceg_is_excluded "$source" "${patterns[@]}" || continue
    if ! sceg_is_excluded "$new" "${patterns[@]}"; then
      {
        echo "✗ $new is a split of $source, which is coverage-excluded, but is not excluded itself."
        echo "  Every line it carries is newly blamed, so it lands in SonarCloud's new code"
        echo "  at whatever unit coverage it has — the pre-commit scan cannot see this,"
        echo "  because blame has no commit for those lines yet."
        echo "  Fix: add $new to sonar.coverage.exclusions in sonar-project.properties,"
        echo "  or unit-test it to the ≥80% the gate holds new code to."
      } >&2
      failed=1
    fi
  done < <(sceg_copies)

  # Check 2 — a listed literal path still names a file.
  local pattern
  for pattern in "${patterns[@]}"; do
    case "$pattern" in
      *'*'*) continue ;;
    esac
    if [ ! -e "$SCEG_ROOT/$pattern" ]; then
      {
        echo "✗ sonar.coverage.exclusions lists $pattern, which does not exist."
        echo "  A moved or deleted file leaves an entry matching nothing, and the path"
        echo "  that replaced it is scanned for coverage without anyone deciding that."
        echo "  Fix: repoint the entry in sonar-project.properties, or drop it."
      } >&2
      failed=1
    fi
  done

  # Check 3 — every entry states why no in-process test can execute it.
  local justified=()
  while IFS= read -r entry; do
    justified+=("$entry")
  done < <(sceg_justified "$SCEG_PROPERTIES")
  for pattern in "${patterns[@]}"; do
    local found=1 j
    for j in ${justified[@]+"${justified[@]}"}; do
      [ "$j" = "$pattern" ] && found=0 && break
    done
    if [ "$found" -ne 0 ]; then
      {
        echo "✗ sonar.coverage.exclusions lists $pattern with no justification."
        echo "  An exclusion whose reason is not written down cannot be reviewed and will"
        echo "  never be removed. Add a line to the JUSTIFICATIONS block above the property:"
        echo "    #   $pattern — <why no in-process test can execute it>"
        echo "  .claude/rules/coverage-exclusions.md — an exclusion is a last resort."
      } >&2
      failed=1
    fi
  done

  # Check 4 — the per-language ignore lists agree wherever coverage is enforced.
  local ci="$SCEG_CI_WORKFLOW" gauntlet="$SCEG_GAUNTLET"
  if [ -f "$ci" ] && [ -f "$gauntlet" ]; then
    local lang
    for lang in rust go; do
      local a b
      a="$(sceg_"${lang}"_ignore "$ci")"
      b="$(sceg_"${lang}"_ignore "$gauntlet")"
      if [ "$a" != "$b" ]; then
        {
          echo "✗ the $lang coverage ignore list differs between CI and the gauntlet."
          echo "    ci.yml:              $a"
          echo "    precommit-gauntlet:  $b"
          echo "  A path ignored in both, and excluded in Sonar, is measured by nothing."
          echo "  Fix: make them identical."
        } >&2
        failed=1
      fi
    done
  fi

  if [ "$failed" -ne 0 ]; then
    return 1
  fi
  echo "✓ sonar-coverage-exclusion-guard: ${#patterns[@]} exclusions, each justified and resolving; ignore lists agree" >&2
  return 0
}

# Run only when executed directly; sourcing exposes the functions for unit tests.
if [ "${BASH_SOURCE[0]}" = "${0}" ]; then
  sceg_main
fi
