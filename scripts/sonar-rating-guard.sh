#!/usr/bin/env bash
# sonar-rating-guard.sh — local guardrail against the SonarCloud
# "new_reliability_rating / new_security_rating below A" gate failing only in CI.
#
# The gate fails those two conditions on any bug or vulnerability among *new*
# code. SonarCloud derives "new" from git blame, so during a pre-commit
# `make sonar` — which runs BEFORE the commit exists — freshly added and changed
# lines carry no commit and are not counted as new. A bug written this morning
# therefore sits outside the new-code period locally, both ratings read A, and
# the same analysis re-run in CI once the lines are committed reads D and C:
# green locally, red in CI (observed: commit 2acbdbdc, new_reliability_rating 4
# and new_security_rating 3, from two `.sort()` calls and one assembled SQL
# statement that the local scan had reported as clean).
#
# This guard sidesteps the blame gap the same way the duplication guard does, by
# reading an ABSOLUTE measure instead of a new-code one: the issues API reports
# every unresolved finding with its file, computed from file content rather than
# from blame, so a finding on an uncommitted line is reported like any other. The
# guard runs AFTER `make sonar` has uploaded the working tree, keeps only the
# findings that sit on files this change touched, and fails on the ones that can
# drop a rating below A.
#
# What blocks and what only warns follows what the gate itself measures:
#
#   bug / vulnerability on changed MAIN code   → fails (this is the gate)
#   unreviewed hotspot on changed MAIN code    → fails (new_security_hotspots_reviewed)
#   anything else on a changed analyzed file   → reported, does not fail
#
# The warning half matters on its own: nine of the twelve findings on 2acbdbdc
# were code smells, and the local scan showed none of them either.
#
# Env:
#   SONAR_TOKEN              required (same token the scan uses).
#   SONAR_PROJECT            default volchanskyi_opengate
#   SONAR_BRANCH             default dev
#   SONAR_API                default https://sonarcloud.io
#   RATING_BASE              git ref changed files are compared against, default HEAD.
#   RATING_SETTLE_RETRIES    polls to wait for the upload to finish, default 12.
#   RATING_SETTLE_SLEEP      seconds between polls, default 5.
#   RATING_CHANGED_OVERRIDE  test seam: newline-separated file list (skips git).
#   RATING_ISSUES_OVERRIDE   test seam: TSV issue lines (skips the API).
#   RATING_HOTSPOTS_OVERRIDE test seam: TSV hotspot lines (skips the API).
#   RATING_PENDING_OVERRIDE  test seam: queued-task count (skips the API).
#   CURL_BIN                 curl binary (stubbed in tests).
#
# Exit codes: 0 = no blocking finding on a changed file (or nothing changed);
#             1 = a blocking finding, or an analysis still being processed;
#             2 = prerequisite missing (no SONAR_TOKEN and no override).
set -uo pipefail

SONAR_PROJECT="${SONAR_PROJECT:-volchanskyi_opengate}"
SONAR_BRANCH="${SONAR_BRANCH:-dev}"
SONAR_API="${SONAR_API:-https://sonarcloud.io}"
RATING_BASE="${RATING_BASE:-HEAD}"
RATING_SETTLE_RETRIES="${RATING_SETTLE_RETRIES:-12}"
RATING_SETTLE_SLEEP="${RATING_SETTLE_SLEEP:-5}"
CURL_BIN="${CURL_BIN:-curl}"

# srat_is_analyzed <path> — exit 0 when SonarCloud reads the file at all: under a
# sonar.sources root, a Rust/Go/TS extension, and not generated output. Test
# files are included; they are analyzed, they raise findings, and those findings
# are worth naming even though they move no rating.
srat_is_analyzed() {
  local p="$1"
  case "$p" in
    server/internal/* | agent/crates/* | web/src/*) ;;
    *) return 1 ;;
  esac
  case "$p" in
    *_gen.go | *.pb.go) return 1 ;;
    */testdata/*) return 1 ;;
  esac
  case "$p" in
    *.rs | *.go | *.ts | *.tsx) return 0 ;;
    *) return 1 ;;
  esac
}

# srat_is_source <path> — exit 0 when the file is main production code, which is
# what the two ratings are computed over. A finding in a test file cannot move
# them, so the guard must not fail a commit the gate would pass.
srat_is_source() {
  srat_is_analyzed "$1" || return 1
  case "$1" in
    *_test.go | *.test.ts | *.test.tsx | *.spec.ts | *.spec.tsx) return 1 ;;
    */tests/* | */testutil/* | */testpg/*) return 1 ;;
  esac
  return 0
}

# srat_blocks <type> — exit 0 for the finding types that drop a rating below A.
# Code smells move new_maintainability_rating, which the gate holds at A with
# smells present, so they are reported rather than blocking.
srat_blocks() {
  case "$1" in
    BUG | VULNERABILITY) return 0 ;;
    *) return 1 ;;
  esac
}

# srat_changed_files — print changed + untracked analyzed files, one per line.
srat_changed_files() {
  if [ -n "${RATING_CHANGED_OVERRIDE+x}" ]; then
    printf '%s\n' "$RATING_CHANGED_OVERRIDE"
    return 0
  fi
  {
    git diff --name-only "$RATING_BASE" 2>/dev/null
    git ls-files --others --exclude-standard 2>/dev/null
  } | sort -u | while IFS= read -r f; do
    [ -n "$f" ] && srat_is_analyzed "$f" && printf '%s\n' "$f"
  done
}

# srat_pending — print how many analysis tasks are queued or running. A non-zero
# count means the answers below are about an earlier upload, not this one.
srat_pending() {
  if [ -n "${RATING_PENDING_OVERRIDE:-}" ]; then
    printf '%s' "$RATING_PENDING_OVERRIDE"
    return 0
  fi
  "$CURL_BIN" -s -u "$SONAR_TOKEN:" \
    "$SONAR_API/api/ce/activity_status?componentKey=$SONAR_PROJECT" \
    | jq -r '((.pending // 0) + (.inProgress // 0))' 2>/dev/null
}

# srat_settled — exit 0 once nothing is queued. Zero findings and "not finished
# counting" are the same empty list, and one of them is a false green, so the
# caller refuses the answer rather than reporting it.
srat_settled() {
  local i=0 n
  while :; do
    n="$(srat_pending)"
    [ -z "$n" ] && return 0 # cannot tell — the fetch below reports its own trouble
    [ "$n" = "0" ] && return 0
    [ "$i" -ge "$RATING_SETTLE_RETRIES" ] && return 1
    i=$((i + 1))
    sleep "$RATING_SETTLE_SLEEP"
  done
}

# srat_fetch_issues — print one TSV line per unresolved issue:
# path, type, severity, rule, line, message. Absolute, not new-code scoped.
srat_fetch_issues() {
  if [ -n "${RATING_ISSUES_OVERRIDE+x}" ]; then
    printf '%s\n' "$RATING_ISSUES_OVERRIDE"
    return 0
  fi
  "$CURL_BIN" -s -u "$SONAR_TOKEN:" \
    "$SONAR_API/api/issues/search?componentKeys=$SONAR_PROJECT&branch=$SONAR_BRANCH&resolved=false&ps=500" \
    | jq -r '.issues[]? | [(.component | sub("^[^:]*:"; "")), .type, .severity, .rule, ((.line // 0) | tostring), .message] | @tsv' 2>/dev/null
}

# srat_fetch_hotspots — print one TSV line per hotspot awaiting review:
# path, line, rule.
srat_fetch_hotspots() {
  if [ -n "${RATING_HOTSPOTS_OVERRIDE+x}" ]; then
    printf '%s\n' "$RATING_HOTSPOTS_OVERRIDE"
    return 0
  fi
  "$CURL_BIN" -s -u "$SONAR_TOKEN:" \
    "$SONAR_API/api/hotspots/search?projectKey=$SONAR_PROJECT&branch=$SONAR_BRANCH&status=TO_REVIEW&ps=500" \
    | jq -r '.hotspots[]? | [(.component | sub("^[^:]*:"; "")), ((.line // 0) | tostring), .ruleKey] | @tsv' 2>/dev/null
}

# srat_touched <path> <changed-list> — exit 0 when the finding sits on a file
# this change touched. Scoping is what keeps somebody else's finding from
# failing a commit that did not cause it.
srat_touched() {
  [ -n "$2" ] && printf '%s\n' "$2" | grep -qxF "$1"
}

srat_main() {
  local changed
  changed="$(srat_changed_files)"
  changed="$(printf '%s\n' "$changed" | grep -v '^$')"
  if [ -z "$changed" ]; then
    echo "✓ sonar-rating-guard: no analyzed source files changed — nothing to guard" >&2
    return 0
  fi

  if [ -z "${SONAR_TOKEN:-}" ] \
    && [ -z "${RATING_ISSUES_OVERRIDE+x}" ] && [ -z "${RATING_HOTSPOTS_OVERRIDE+x}" ]; then
    echo "✗ sonar-rating-guard: SONAR_TOKEN unset (and no issue override)." >&2
    return 2
  fi

  if ! srat_settled; then
    {
      echo "✗ sonar-rating-guard: the uploaded analysis is still being processed."
      echo "  An empty finding list would be indistinguishable from a clean one, so"
      echo "  this is refused rather than reported as a pass. Re-run the guard."
    } >&2
    return 1
  fi

  local blocking=() warning=() path type sev rule line msg
  while IFS=$'\t' read -r path type sev rule line msg; do
    [ -n "$path" ] || continue
    srat_touched "$path" "$changed" || continue
    if srat_blocks "$type" && srat_is_source "$path"; then
      blocking+=("$path:$line [$type/$sev] $rule — $msg")
    else
      warning+=("$path:$line [$type/$sev] $rule — $msg")
    fi
  done <<<"$(srat_fetch_issues)"

  while IFS=$'\t' read -r path line rule; do
    [ -n "$path" ] || continue
    srat_touched "$path" "$changed" || continue
    if srat_is_source "$path"; then
      blocking+=("$path:$line [HOTSPOT] $rule — awaiting review")
    else
      warning+=("$path:$line [HOTSPOT] $rule — awaiting review")
    fi
  done <<<"$(srat_fetch_hotspots)"

  if [ "${#warning[@]}" -gt 0 ]; then
    {
      echo "ℹ sonar-rating-guard: ${#warning[@]} finding(s) on changed files that move no gate condition:"
      printf '    %s\n' "${warning[@]}"
    } >&2
  fi

  if [ "${#blocking[@]}" -gt 0 ]; then
    {
      echo "✗ ${#blocking[@]} finding(s) on changed main code would fail the gate in CI:"
      printf '    %s\n' "${blocking[@]}"
      echo "  A bug fails new_reliability_rating and a vulnerability fails"
      echo "  new_security_rating, both of which the gate holds at A. They are invisible"
      echo "  to the local scan because SonarCloud reads new code from git blame and"
      echo "  these lines are not committed yet."
      echo "  Fix: resolve each finding, then re-run. Do not suppress without approval."
      echo "  Inspect: https://sonarcloud.io/project/issues?id=${SONAR_PROJECT}&branch=${SONAR_BRANCH}&resolved=false"
    } >&2
    return 1
  fi

  echo "✓ sonar-rating-guard: no bug, vulnerability or unreviewed hotspot on the $(printf '%s\n' "$changed" | wc -l) changed file(s)" >&2
  return 0
}

# Run only when executed directly; sourcing exposes the functions for unit tests.
if [ "${BASH_SOURCE[0]}" = "${0}" ]; then
  srat_main
fi
