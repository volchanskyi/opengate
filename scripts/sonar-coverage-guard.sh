#!/usr/bin/env bash
# sonar-coverage-guard.sh — local guardrail against the SonarCloud "new_coverage
# sits at the 80.0 boundary" failure.
#
# The quality gate fails new_coverage when it is `LT 80`. A value like
# 79.95% *displays* as "80.0" but fails the gate, and because new-code coverage
# carries sub-line nondeterminism (race/atomic goroutine lines) and shifts with
# the new-code baseline, a run that clears 80 locally can land at 79.95 in CI —
# green locally, red in CI (observed CI run 26929821908: new_coverage 79.95%).
#
# This guard runs in the gauntlet AFTER `make sonar` has uploaded fresh coverage
# and the gate has been evaluated. It makes two checks, and it needs both because
# neither can see what the other does.
#
#   1. The aggregate. It queries the exact (unrounded) new_coverage and fails
#      unless it clears a buffer ABOVE the 80 gate floor, so a borderline local
#      pass can never become a CI failure. SonarCloud derives "new" from git
#      blame, so this figure covers the commits already in the new-code period
#      and is blind to the lines being committed right now — they carry no
#      commit yet and are measured by nothing.
#
#   2. The diff. Every line this change adds or edits, checked against the hit
#      counts SonarCloud computed from the coverage report we just uploaded —
#      file content, not blame, so uncommitted lines are reported like any
#      other. This is the half that sees the commit in front of it.
#
# The gap between them is not hypothetical: a file split out of another arrives
# with every line dated to the split, so CI measures all of it as new. That is
# how a refactor whose extracted file sat at 47% passed a green local gauntlet
# and failed the gate on push. Check 1 saw a period the split was not in yet;
# check 2 would have read the split's own lines and refused.
#
# The gate stays at 80; both checks hold the local result off the cliff edge.
#
# Env:
#   SONAR_TOKEN            required (same token the scan uses).
#   SONAR_PROJECT          default volchanskyi_opengate
#   SONAR_BRANCH           default dev
#   SONAR_API              default https://sonarcloud.io
#   NEW_COVERAGE_FLOOR     local floor, default 82 (= the 80 gate + 2pt buffer).
#   NEW_COVERAGE_OVERRIDE  test seam: use this value instead of querying the API.
#   SCOV_BASE              git ref changed lines are compared against, default HEAD.
#   SCOV_SETTLE_RETRIES    settle polls before giving up on the analysis, default 12.
#   SCOV_SETTLE_SLEEP      seconds between settle polls, default 5.
#   SCOV_CHANGED_OVERRIDE  test seam: newline-separated changed-file list.
#   SCOV_LINES_OVERRIDE    test seam: "path:line:hits" rows, standing in for the
#                          API. A line the coverage report says nothing about —
#                          a comment, a blank, a declaration — simply has no row.
#   SCOV_TOUCHED_OVERRIDE  test seam: "path:line" rows, standing in for the diff.
#   SCOV_PROPERTIES        sonar-project.properties path, default the repo-root
#                          file. It is what says which files the coverage gate
#                          covers at all.
#   SCOV_REPORT_ROOT       directory the coverage reports are read from, default
#                          the repository root. The reports are the fallback for
#                          a file the branch analysis holds no component for.
#   CURL_BIN               curl binary (stubbed in tests).
#
# Exit codes: 0 = both checks clear the floor, or there is nothing to cover;
#             1 = a check is below the floor;
#             2 = prerequisite missing (no SONAR_TOKEN and no override), or the
#                 uploaded analysis never became queryable — a guard that cannot
#                 ask must not answer yes.
set -uo pipefail

SONAR_PROJECT="${SONAR_PROJECT:-volchanskyi_opengate}"
SONAR_BRANCH="${SONAR_BRANCH:-dev}"
SONAR_API="${SONAR_API:-https://sonarcloud.io}"
NEW_COVERAGE_FLOOR="${NEW_COVERAGE_FLOOR:-82}"
SCOV_BASE="${SCOV_BASE:-HEAD}"
SCOV_SETTLE_RETRIES="${SCOV_SETTLE_RETRIES:-12}"
SCOV_SETTLE_SLEEP="${SCOV_SETTLE_SLEEP:-5}"
SCOV_REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CURL_BIN="${CURL_BIN:-curl}"

# scov_fetch — print the raw new_coverage value (empty when the metric is absent,
# i.e. the analysis introduced no new lines to cover).
scov_fetch() {
  if [ -n "${NEW_COVERAGE_OVERRIDE:-}" ]; then
    printf '%s' "$NEW_COVERAGE_OVERRIDE"
    return 0
  fi
  "$CURL_BIN" -s -u "$SONAR_TOKEN:" \
    "$SONAR_API/api/measures/component?component=$SONAR_PROJECT&branch=$SONAR_BRANCH&metricKeys=new_coverage" \
    | jq -r '.component.measures[]? | select(.metric=="new_coverage") | (.periods[0].value // .period.value) // empty' 2>/dev/null
}

# scov_below_floor <value> <floor> — exit 0 (true) when value < floor. Float-safe
# via awk so "79.95" < "82" compares numerically, not lexically.
scov_below_floor() {
  awk -v v="$1" -v f="$2" 'BEGIN { exit !((v + 0) < (f + 0)) }'
}

scov_main() {
  if [ -z "${NEW_COVERAGE_OVERRIDE:-}" ] && [ -z "${SONAR_TOKEN:-}" ]; then
    echo "✗ sonar-coverage-guard: SONAR_TOKEN unset (and no NEW_COVERAGE_OVERRIDE)." >&2
    return 2
  fi
  local cov
  cov="$(scov_fetch)"
  if [ -z "$cov" ]; then
    echo "✓ sonar-coverage-guard: no new_coverage metric (no new lines to cover) — nothing to guard" >&2
    return 0
  fi
  if scov_below_floor "$cov" "$NEW_COVERAGE_FLOOR"; then
    {
      echo "✗ new_coverage ${cov}% is below the local floor ${NEW_COVERAGE_FLOOR}%."
      echo "  The SonarCloud gate fails new_coverage < 80; this buffer keeps the local"
      echo "  result off the 80.0 boundary so a borderline pass cannot flip to red in CI."
      echo "  Fix: add tests for new/changed lines until new_coverage ≥ ${NEW_COVERAGE_FLOOR}%."
      echo "  Inspect: https://sonarcloud.io/component_measures?id=${SONAR_PROJECT}&branch=${SONAR_BRANCH}&metric=new_coverage&view=list"
    } >&2
    return 1
  fi
  echo "✓ new_coverage ${cov}% ≥ local floor ${NEW_COVERAGE_FLOOR}% (gate floor 80 + buffer)" >&2
  return 0
}

# scov_run — both checks. The aggregate covers the commits already in the
# new-code period; the diff covers the one being made now. Neither sees what the
# other does, so both have to pass.
scov_run() {
  local status=0
  scov_main || status=$?
  [ "$status" -eq 0 ] || return "$status"
  scov_check_diff
}

# --- The diff half ------------------------------------------------------------

# scov_is_source <path> — exit 0 when path is a SonarCloud-analyzed production
# source file: under a sonar.sources root, a Rust/Go/TS extension, and neither a
# test nor generated file (mirrors sonar.exclusions / sonar.test.inclusions).
scov_is_source() {
  local p="$1"
  case "$p" in
    server/internal/* | agent/crates/* | web/src/*) ;;
    *) return 1 ;;
  esac
  case "$p" in
    *_test.go | *.test.ts | *.test.tsx | *.spec.ts | *.spec.tsx) return 1 ;;
    *_gen.go | *.pb.go) return 1 ;;
    */tests/* | */testdata/* | */testutil/* | */testpg/*) return 1 ;;
  esac
  case "$p" in
    *.rs | *.go | *.ts | *.tsx) return 0 ;;
    *) return 1 ;;
  esac
}

# scov_property <name> — one setting's value from sonar-project.properties, with
# the backslash continuations joined and the whitespace taken out.
#
# The file is read rather than copied, because a second copy of the analysis's
# scope is a second thing to keep true and the first one to go stale.
scov_property() {
  local name="$1" file="${SCOV_PROPERTIES:-$SCOV_REPO_ROOT/sonar-project.properties}"
  [ -f "$file" ] || return 0
  awk -v key="$name" '
    joining { line = line $0 }
    !joining && index($0, key "=") == 1 { line = substr($0, length(key) + 2); joining = 1 }
    joining {
      if (line ~ /\\$/) { line = substr(line, 1, length(line) - 1); next }
      gsub(/[ \t]/, "", line)
      print line
      exit
    }' "$file"
}

# scov_gate_covers <path> — whether the coverage gate measures this file at all.
#
# A file the analysis never indexes, and a file the coverage exclusions name,
# carry no figure anywhere by design. That is the same silence as a production
# file whose coverage the analysis dropped, and only the second is the defect
# the refusal below exists for — so the two are told apart here, from the
# analysis's own configuration rather than from a list kept beside it.
#
# A commit confined to the load harness and a documentation tool was refused on
# every attempt for touching nothing this gate measures.
scov_gate_covers() {
  local path="$1" root exclusion
  scov_is_source "$path" || return 1

  local covered=1
  while IFS= read -r root; do
    [ -n "$root" ] || continue
    case "$path" in "$root"/*) covered=0 ;; esac
  done <<<"$(scov_property sonar.sources | tr ',' '\n')"
  [ "$covered" -eq 0 ] || return 1

  while IFS= read -r exclusion; do
    [ -n "$exclusion" ] || continue
    # A Sonar glob is shell-glob syntax with "**" meaning any depth, which a
    # case pattern already reads as "anything" — the separators either side are
    # what the two spellings differ over.
    # shellcheck disable=SC2254
    case "$path" in
      ${exclusion//\*\*\//*} | ${exclusion//\*\*/*}) return 1 ;;
    esac
  done <<<"$(scov_property sonar.coverage.exclusions | tr ',' '\n')"
  return 0
}

# scov_changed_files — print the changed + untracked files the coverage gate
# covers, one per line.
scov_changed_files() {
  if [ -n "${SCOV_CHANGED_OVERRIDE:-}" ]; then
    printf '%s\n' "$SCOV_CHANGED_OVERRIDE" | while IFS= read -r f; do
      [ -n "$f" ] && scov_gate_covers "$f" && printf '%s\n' "$f"
    done
    return 0
  fi
  {
    git diff --name-only "$SCOV_BASE" 2>/dev/null
    git ls-files --others --exclude-standard 2>/dev/null
  } | sort -u | while IFS= read -r f; do
    [ -n "$f" ] && scov_gate_covers "$f" && printf '%s\n' "$f"
  done
}

# scov_added_lines — read a unified diff on standard input and print the working
# tree's line numbers it added or edited, one per line.
#
# It is separate from the command that produces the diff so it can be held to
# the hunk shapes that matter — a single-line hunk with no count, and a pure
# deletion, which touches no line of the working tree at all — without standing
# up a repository to produce each one.
scov_added_lines() {
  awk '
    /^@@/ {
      # @@ -old,count +new,count @@ — the "+" side is the working tree, and a
      # count of zero is a pure deletion, which adds no line to cover.
      match($0, /\+[0-9]+(,[0-9]+)?/)
      spec = substr($0, RSTART + 1, RLENGTH - 1)
      split(spec, parts, ",")
      start = parts[1] + 0
      count = (2 in parts) ? parts[2] + 0 : 1
      for (i = 0; i < count; i++) print start + i
    }'
}

# scov_changed_lines <path> — print the working tree's line numbers this change
# added or edited. A file git does not know yet is new in its entirety, and
# reporting nothing for one is how a brand-new uncovered file walks past the
# check written to catch it.
scov_changed_lines() {
  local path="$1"
  if [ -n "${SCOV_TOUCHED_OVERRIDE:-}" ]; then
    printf '%s\n' "$SCOV_TOUCHED_OVERRIDE" \
      | awk -F: -v p="$path" '$1 == p && NF >= 2 { print $2 }'
    return 0
  fi
  if ! git cat-file -e "$SCOV_BASE:$path" 2>/dev/null; then
    [ -f "$path" ] || return 0
    awk '{ print NR }' "$path"
    return 0
  fi
  git diff -U0 --no-color "$SCOV_BASE" -- "$path" 2>/dev/null | scov_added_lines
}

# scov_line_hits <path> — print "line hits" for every line SonarCloud has a
# coverage figure for, from the analysis just uploaded. The hit counts come from
# the coverage report and the file's own content, so a line with no commit
# behind it is reported exactly like one that has had a commit for years.
scov_line_hits() {
  local path="$1"
  if [ -n "${SCOV_LINES_OVERRIDE:-}" ]; then
    printf '%s\n' "$SCOV_LINES_OVERRIDE" \
      | awk -F: -v p="$path" '$1 == p && NF >= 3 { print $2, $3 }'
    return 0
  fi
  "$CURL_BIN" -s -u "$SONAR_TOKEN:" \
    "$SONAR_API/api/sources/lines?key=$SONAR_PROJECT:$path&branch=$SONAR_BRANCH" \
    | jq -r '.sources[]? | select(has("lineHits")) | "\(.line) \(.lineHits)"' 2>/dev/null
}

# scov_local_line_hits <path> — print "line hits" for a file, read from the
# coverage reports the scan uploaded.
#
# What the analysis holds per file is not a fact about the coverage report; it
# is a fact about the branch. SonarCloud keeps file-level data for a short-lived
# branch only where that branch changed the file, and `dev` is short-lived — so
# a file the previous commit did not touch has no component on it, whatever its
# coverage. The lines being committed right now are never in that set, which is
# exactly the blame gap this check exists to close, arrived at from the other
# side.
#
# It surfaced on a commit whose predecessor touched only test-harness files:
# every guarded source file came back "not found" for a change that had just
# added a well-covered one, and the guard refused permanently rather than once.
#
# The reports describe the working tree, so they carry no blame gap at all, and
# they are the same numbers SonarCloud was given — sonar-project.properties
# names all three, and this reads them where that file points.
scov_local_line_hits() {
  local path="$1" root="${SCOV_REPORT_ROOT:-.}" want_go

  case "$path" in
    server/*)
      # A Go cover profile names files by import path.
      want_go="github.com/volchanskyi/opengate/$path"
      # A Go cover profile names a block by its first and last line, so every
      # line the block spans carries the block's count.
      awk -v want="$want_go" '
        function lineOf(spec,   dot) {
          dot = index(spec, ".")
          return (dot == 0 ? spec : substr(spec, 1, dot - 1)) + 0
        }
        NR == 1 { next }
        NF == 3 {
          # "<import path>/<file>.go:<start>.<col>,<end>.<col> <stmts> <count>".
          # The name is everything before the last colon, which is where a
          # separator-based split goes wrong: the path is full of dots.
          cut = index($1, ".go:")
          if (cut == 0) next
          if (substr($1, 1, cut + 2) != want) next
          # "<start>.<col>,<end>.<col>" — a column is not a line, and awk reads
          # "10.20" as 10.2, so each half is cut at its own dot.
          span = substr($1, cut + 4)
          comma = index(span, ",")
          if (comma == 0) next
          start = lineOf(substr(span, 1, comma - 1))
          end = lineOf(substr(span, comma + 1))
          for (line = start; line <= end; line++) print line, $3
        }' "$root/server/coverage.out" 2>/dev/null
      ;;
    web/*)
      # The web report names files relative to web/.
      scov_lcov_line_hits "$root/web/coverage/lcov.info" "${path#web/}"
      ;;
    agent/*)
      # The Rust report is rewritten into repository-relative paths before the
      # scan reads it, so its names are already the ones used here.
      scov_lcov_line_hits "$root/agent/lcov.info" "$path"
      ;;
  esac
}

# scov_lcov_line_hits <report> <path> — print "line hits" for one file in an
# LCOV report.
scov_lcov_line_hits() {
  local report="$1" want="$2"
  [ -f "$report" ] || return 0
  awk -v want="$want" '
    /^SF:/ { inside = (substr($0, 4) == want); next }
    /^end_of_record/ { inside = 0; next }
    inside && /^DA:/ {
      split(substr($0, 4), parts, ",")
      print parts[1] + 0, parts[2] + 0
    }' "$report" 2>/dev/null
}

# scov_report_was_read <path> — whether the coverage report covering this file's
# tree exists and names at least one source.
#
# It is what separates a file with nothing to execute from a measurement that
# went missing, and the two are otherwise the same silence. A Rust module that
# is doc comments and `pub mod` lines has no executable line, so llvm-cov writes
# no record for it while naming every other file in its crate — and a guard that
# reads that as coverage nobody took refuses a change that has nothing to cover.
#
# A report that names nothing is the case this guard exists for, and it still
# refuses. The read-back is the signal rather than the absence, for the reason
# rust-lcov-relativize.sh gives about the report it rewrites.
scov_report_was_read() {
  local path="$1" root="${SCOV_REPORT_ROOT:-.}"
  case "$path" in
    server/*) grep -qE '\.go:[0-9]+' "$root/server/coverage.out" 2>/dev/null ;;
    web/*) grep -q '^SF:' "$root/web/coverage/lcov.info" 2>/dev/null ;;
    agent/*) grep -q '^SF:' "$root/agent/lcov.info" 2>/dev/null ;;
    *) return 1 ;;
  esac
}

# scov_file_tally <path> — print "covered to_cover" over the lines this change
# touched, or nothing when the file's own coverage report was never read.
scov_file_tally() {
  local path="$1" hits changed
  hits="$(scov_line_hits "$path")"
  [ -n "$hits" ] || hits="$(scov_local_line_hits "$path")"
  if [ -z "$hits" ]; then
    # A file neither source says anything about, in a tree whose report was
    # read, holds no executable line — so there is nothing here to cover.
    scov_report_was_read "$path" || return 1
    printf '0 0\n'
    return 0
  fi
  changed="$(scov_changed_lines "$path")"
  [ -n "$changed" ] || {
    printf '0 0\n'
    return 0
  }
  awk -v changed="$changed" '
    BEGIN { split(changed, list, "\n"); for (i in list) touched[list[i] + 0] = 1 }
    ($1 + 0) in touched { to_cover++; if (($2 + 0) > 0) covered++ }
    END { printf "%d %d\n", covered + 0, to_cover + 0 }
  ' <<<"$hits"
}

# scov_diff_coverage <files> — print "covered to_cover" summed over every changed
# file, and exit non-zero when the analysis reported figures for none of them.
scov_diff_coverage() {
  local f tally covered=0 to_cover=0 measured=0
  while IFS= read -r f; do
    [ -n "$f" ] || continue
    tally="$(scov_file_tally "$f")" || continue
    measured=1
    covered=$((covered + $(cut -d' ' -f1 <<<"$tally")))
    to_cover=$((to_cover + $(cut -d' ' -f2 <<<"$tally")))
  done <<<"$1"
  [ "$measured" -eq 1 ] || return 1
  printf '%d %d\n' "$covered" "$to_cover"
}

# scov_check_diff — the second check. Fails when the lines this change touched
# are covered below the floor.
scov_check_diff() {
  local files tally covered to_cover percent i=0
  files="$(scov_changed_files)"
  if [ -z "$files" ]; then
    echo "✓ sonar-coverage-guard: no changed source files — no diff to cover" >&2
    return 0
  fi

  # `make sonar` waits on the compute engine, but the sources endpoint can lag a
  # few seconds behind indexing. Reading it too early returns nothing for every
  # file, and a guard that passes because it could not ask is the false green it
  # was written to close.
  until tally="$(scov_diff_coverage "$files")"; do
    i=$((i + 1))
    if [ "$i" -gt "$SCOV_SETTLE_RETRIES" ]; then
      {
        echo "✗ sonar-coverage-guard: SonarCloud reported no coverage figures for any changed file"
        echo "  after ${SCOV_SETTLE_RETRIES} polls. The lines this change touched were measured by"
        echo "  nothing, which is not the same as being covered."
      } >&2
      return 2
    fi
    sleep "$SCOV_SETTLE_SLEEP"
  done

  covered="$(cut -d' ' -f1 <<<"$tally")"
  to_cover="$(cut -d' ' -f2 <<<"$tally")"
  if [ "$to_cover" -eq 0 ]; then
    echo "✓ sonar-coverage-guard: the changed lines hold nothing to cover" >&2
    return 0
  fi

  percent="$(awk -v c="$covered" -v t="$to_cover" 'BEGIN { printf "%.2f", c / t * 100 }')"
  if scov_below_floor "$percent" "$NEW_COVERAGE_FLOOR"; then
    {
      echo "✗ the lines this change touches are ${percent}% covered, below the local floor ${NEW_COVERAGE_FLOOR}%."
      echo "  ${covered} of ${to_cover} changed lines that need covering are hit by a test."
      echo "  SonarCloud dates every line of a moved or split file to the move, so CI measures"
      echo "  all of them as new code against the 80 gate. Fix: cover the changed lines."
      echo "  Inspect: https://sonarcloud.io/component_measures?id=${SONAR_PROJECT}&branch=${SONAR_BRANCH}&metric=new_coverage&view=list"
    } >&2
    return 1
  fi
  echo "✓ sonar-coverage-guard: ${covered}/${to_cover} changed lines covered (${percent}% ≥ ${NEW_COVERAGE_FLOOR}%)" >&2
  return 0
}

# Run only when executed directly; sourcing exposes the functions for unit tests.
if [ "${BASH_SOURCE[0]}" = "${0}" ]; then
  scov_run
fi
