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

# scov_changed_files — print changed + untracked source files, one per line.
scov_changed_files() {
  if [ -n "${SCOV_CHANGED_OVERRIDE:-}" ]; then
    printf '%s\n' "$SCOV_CHANGED_OVERRIDE"
    return 0
  fi
  {
    git diff --name-only "$SCOV_BASE" 2>/dev/null
    git ls-files --others --exclude-standard 2>/dev/null
  } | sort -u | while IFS= read -r f; do
    [ -n "$f" ] && scov_is_source "$f" && printf '%s\n' "$f"
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

# scov_file_tally <path> — print "covered to_cover" over the lines this change
# touched, or nothing when SonarCloud has no coverage figures for the file.
scov_file_tally() {
  local path="$1" hits changed
  hits="$(scov_line_hits "$path")"
  [ -n "$hits" ] || return 1
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
