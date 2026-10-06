#!/usr/bin/env bash
# Fails when new_coverage, or the coverage of the lines this change touches, falls below a floor
# set above the 80 gate. The diff half reads file content, so uncommitted lines are measured.
#
# Environment:
#   SONAR_TOKEN            required, the token the scan uses
#   SONAR_PROJECT          default volchanskyi_opengate
#   SONAR_BRANCH           default dev
#   SONAR_API              default https://sonarcloud.io
#   NEW_COVERAGE_FLOOR     local floor, default 82
#   NEW_COVERAGE_OVERRIDE  test seam: this value stands in for the API query
#   SCOV_BASE              git ref changed lines are compared against, default HEAD
#   SCOV_SETTLE_RETRIES    settle polls before giving up on the analysis, default 12
#   SCOV_SETTLE_SLEEP      seconds between settle polls, default 5
#   SCOV_CHANGED_OVERRIDE  test seam: newline-separated changed-file list
#   SCOV_LINES_OVERRIDE    test seam: "path:line:hits" rows standing in for the API
#   SCOV_TOUCHED_OVERRIDE  test seam: "path:line" rows standing in for the diff
#   SCOV_PROPERTIES        sonar-project.properties path, default the repo-root file
#   SCOV_REPORT_ROOT       directory the coverage reports are read from, default the repo root
#   CURL_BIN               curl binary (stubbed in tests)
#
# Exit codes:
#   0  both checks clear the floor, or there is nothing to cover
#   1  a check is below the floor
#   2  no SONAR_TOKEN and no override, or the uploaded analysis never became queryable
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

scov_fetch() {
  if [ -n "${NEW_COVERAGE_OVERRIDE:-}" ]; then
    printf '%s' "$NEW_COVERAGE_OVERRIDE"
    return 0
  fi
  "$CURL_BIN" -s -u "$SONAR_TOKEN:" \
    "$SONAR_API/api/measures/component?component=$SONAR_PROJECT&branch=$SONAR_BRANCH&metricKeys=new_coverage" \
    | jq -r '.component.measures[]? | select(.metric=="new_coverage") | (.periods[0].value // .period.value) // empty' 2>/dev/null
}

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

scov_run() {
  local status=0
  scov_main || status=$?
  [ "$status" -eq 0 ] || return "$status"
  scov_check_diff
}

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

# A file outside the sonar.sources roots or under a coverage exclusion has no figure by design.
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
    # A Sonar "**" glob becomes "*", which a case pattern matches across any depth.
    # shellcheck disable=SC2254
    case "$path" in
      ${exclusion//\*\*\//*} | ${exclusion//\*\*/*}) return 1 ;;
    esac
  done <<<"$(scov_property sonar.coverage.exclusions | tr ',' '\n')"
  return 0
}

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

# A single-line hunk has no count, and a pure deletion touches no working-tree line.
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

scov_line_hits() {
  local path="$1"
  if [ -n "${SCOV_LINES_OVERRIDE:-}" ]; then
    printf '%s\n' "$SCOV_LINES_OVERRIDE" \
      | awk -F: -v p="$path" '$1 == p && NF >= 3 { print $2, $3 }'
    return 0
  fi
  # The branch holds the last finished analysis, so its hits count only where its copy of the file
  # equals the working tree's; otherwise nothing prints and the caller reads the local reports.
  local analysed
  analysed="$("$CURL_BIN" -s -u "$SONAR_TOKEN:" \
    "$SONAR_API/api/sources/raw?key=$SONAR_PROJECT:$path&branch=$SONAR_BRANCH" 2>/dev/null)"
  [ -f "$path" ] && [ -n "$analysed" ] && [ "$analysed" = "$(cat "$path")" ] || return 0
  "$CURL_BIN" -s -u "$SONAR_TOKEN:" \
    "$SONAR_API/api/sources/lines?key=$SONAR_PROJECT:$path&branch=$SONAR_BRANCH" \
    | jq -r '.sources[]? | select(has("lineHits")) | "\(.line) \(.lineHits)"' 2>/dev/null
}

# SonarCloud keeps file data for a short-lived branch only for files that branch changed.
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
          # Each line is "<import path>/<file>.go:<start>.<col>,<end>.<col> <stmts> <count>";
          # the name ends at ".go:", since the path is full of dots.
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

# Separates a file with nothing to execute from a missing measurement.
scov_report_was_read() {
  local path="$1" root="${SCOV_REPORT_ROOT:-.}"
  case "$path" in
    server/*) grep -qE '\.go:[0-9]+' "$root/server/coverage.out" 2>/dev/null ;;
    web/*) grep -q '^SF:' "$root/web/coverage/lcov.info" 2>/dev/null ;;
    agent/*) grep -q '^SF:' "$root/agent/lcov.info" 2>/dev/null ;;
    *) return 1 ;;
  esac
}

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

scov_check_diff() {
  local files tally covered to_cover percent i=0
  files="$(scov_changed_files)"
  if [ -z "$files" ]; then
    echo "✓ sonar-coverage-guard: no changed source files — no diff to cover" >&2
    return 0
  fi

  # The sources endpoint can lag indexing by seconds, and an early read returns nothing per file.
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
