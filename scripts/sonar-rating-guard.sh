#!/usr/bin/env bash
# Fails on a bug, vulnerability or unreviewed hotspot on a changed main-code file and reports other
# findings there. It reads absolute issue lists, which carry no git-blame gap for uncommitted lines.
#
# Environment:
#   SONAR_TOKEN              required, the token the scan uses
#   SONAR_PROJECT            default volchanskyi_opengate
#   SONAR_BRANCH             default dev
#   SONAR_API                default https://sonarcloud.io
#   RATING_BASE              git ref changed files are compared against, default HEAD
#   RATING_SETTLE_RETRIES    polls to wait for the upload to finish, default 12
#   RATING_SETTLE_SLEEP      seconds between polls, default 5
#   RATING_CHANGED_OVERRIDE  test seam: newline-separated file list, skips git
#   RATING_ISSUES_OVERRIDE   test seam: TSV issue lines, skips the API
#   RATING_HOTSPOTS_OVERRIDE test seam: TSV hotspot lines, skips the API
#   RATING_PENDING_OVERRIDE  test seam: queued-task count, skips the API
#   CURL_BIN                 curl binary (stubbed in tests)
#
# Exit codes:
#   0  no blocking finding on a changed file, or nothing changed
#   1  a blocking finding, or an analysis still being processed
#   2  no SONAR_TOKEN and no override
set -uo pipefail

SONAR_PROJECT="${SONAR_PROJECT:-volchanskyi_opengate}"
SONAR_BRANCH="${SONAR_BRANCH:-dev}"
SONAR_API="${SONAR_API:-https://sonarcloud.io}"
RATING_BASE="${RATING_BASE:-HEAD}"
RATING_SETTLE_RETRIES="${RATING_SETTLE_RETRIES:-12}"
RATING_SETTLE_SLEEP="${RATING_SETTLE_SLEEP:-5}"
CURL_BIN="${CURL_BIN:-curl}"

# server/tests holds Go the analysis reads, so a finding can land there.
srat_is_analyzed() {
  local p="$1"
  case "$p" in
    server/internal/* | server/tests/* | agent/crates/* | web/src/*) ;;
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

# server/tests holds Go outside *_test.go, which the exclusions keep, so it counts as main code.
srat_is_source() {
  srat_is_analyzed "$1" || return 1
  case "$1" in
    *_test.go | *.test.ts | *.test.tsx | *.spec.ts | *.spec.tsx) return 1 ;;
    agent/crates/*/tests/*) return 1 ;;
    */testutil/* | */testpg/* | */testdata/*) return 1 ;;
  esac
  return 0
}

srat_blocks() {
  case "$1" in
    BUG | VULNERABILITY) return 0 ;;
    *) return 1 ;;
  esac
}

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

# A non-zero queued or running count means the answers below describe an earlier upload.
srat_pending() {
  if [ -n "${RATING_PENDING_OVERRIDE:-}" ]; then
    printf '%s' "$RATING_PENDING_OVERRIDE"
    return 0
  fi
  "$CURL_BIN" -s -u "$SONAR_TOKEN:" \
    "$SONAR_API/api/ce/activity_status?componentKey=$SONAR_PROJECT" \
    | jq -r '((.pending // 0) + (.inProgress // 0))' 2>/dev/null
}

# Zero findings and unfinished counting are the same list, so the queue has to be empty first.
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

# Prints one TSV line per unresolved issue: path, type, severity, rule, line, message.
srat_fetch_issues() {
  if [ -n "${RATING_ISSUES_OVERRIDE+x}" ]; then
    printf '%s\n' "$RATING_ISSUES_OVERRIDE"
    return 0
  fi
  "$CURL_BIN" -s -u "$SONAR_TOKEN:" \
    "$SONAR_API/api/issues/search?componentKeys=$SONAR_PROJECT&branch=$SONAR_BRANCH&resolved=false&ps=500" \
    | jq -r '.issues[]? | [(.component | sub("^[^:]*:"; "")), .type, .severity, .rule, ((.line // 0) | tostring), .message] | @tsv' 2>/dev/null
}

# Prints one TSV line per hotspot awaiting review: path, line, rule.
srat_fetch_hotspots() {
  if [ -n "${RATING_HOTSPOTS_OVERRIDE+x}" ]; then
    printf '%s\n' "$RATING_HOTSPOTS_OVERRIDE"
    return 0
  fi
  "$CURL_BIN" -s -u "$SONAR_TOKEN:" \
    "$SONAR_API/api/hotspots/search?projectKey=$SONAR_PROJECT&branch=$SONAR_BRANCH&status=TO_REVIEW&ps=500" \
    | jq -r '.hotspots[]? | [(.component | sub("^[^:]*:"; "")), ((.line // 0) | tostring), .ruleKey] | @tsv' 2>/dev/null
}

srat_touched() {
  [ -n "$2" ] && grep -qxF "$1" <<<"$2"
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
