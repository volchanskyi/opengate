#!/usr/bin/env bash
# Fails on a high or critical advisory in DIR's lockfile unless a live exception names it.
# Exceptions live in scripts/lib/npm-audit-exceptions.json and lapse when stale or past review.
#
# Usage:
#   scripts/npm-audit.sh DIR   (a directory relative to the repository root, such as web)
#
# Exit codes:
#   0  no high or critical advisory outside a live exception
#   1  an advisory without an exception, or an exception that lapsed
#   2  the audit or the registry stayed unreadable, or the exception list is malformed
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
EXCEPTIONS="$ROOT/scripts/lib/npm-audit-exceptions.json"
ATTEMPTS=3

if [ "$#" -ne 1 ]; then
  echo "usage: scripts/npm-audit.sh DIR" >&2
  exit 2
fi
DIR="$1"

refuse() {
  echo "::error::npm audit ($DIR): $1" >&2
  exit 2
}

failed=0
finding() {
  echo "::error::npm audit ($DIR): $*" >&2
  failed=1
}

[ -d "$ROOT/$DIR" ] || refuse "$ROOT/$DIR is not a directory"
jq -e 'type == "array"' "$EXCEPTIONS" >/dev/null 2>&1 || refuse "$EXCEPTIONS is not a JSON array"
incomplete="$(jq -r '.[] | . as $e | ["dir", "advisory", "package", "newest", "review_by", "reason"][]
  | select(($e[.] // "") == "") | "\($e.advisory // "an entry") has no \(.)"' "$EXCEPTIONS")"
[ -z "$incomplete" ] || refuse "the exception list is incomplete: $(tr '\n' ';' <<<"$incomplete")"
undated="$(jq -r '.[] | select(.review_by | test("^[0-9]{4}-[0-9]{2}-[0-9]{2}$") | not)
  | "\(.advisory) review_by \(.review_by)"' "$EXCEPTIONS")"
[ -z "$undated" ] || refuse "a review_by is not YYYY-MM-DD: $(tr '\n' ';' <<<"$undated")"

REPORT="$(mktemp)"
trap 'rm -f "$REPORT"' EXIT

# A report with no vulnerabilities object, or with an error object, is a request that failed.
readable=""
for attempt in $(seq 1 "$ATTEMPTS"); do
  (cd "$ROOT/$DIR" && npm audit --json) >"$REPORT" 2>/dev/null || true
  if jq -e '(.vulnerabilities | type == "object") and (has("error") | not)' "$REPORT" >/dev/null 2>&1; then
    readable=yes
    break
  fi
  cause="$(jq -r '"\(.error.code // "no code"): \(.error.summary // "no summary")"' "$REPORT" 2>/dev/null \
    || head -c 200 "$REPORT")"
  echo "::warning::npm audit ($DIR) attempt $attempt/$ATTEMPTS was unreadable: $cause" >&2
  if [ "$attempt" -lt "$ATTEMPTS" ]; then
    sleep $((attempt * 15))
  fi
done
[ -n "$readable" ] || refuse "the audit stayed unreadable after $ATTEMPTS attempts"

registry_latest() {
  local pkg="$1" attempt version
  for attempt in $(seq 1 "$ATTEMPTS"); do
    if version="$(cd "$ROOT/$DIR" && npm view "$pkg" version 2>/dev/null)" && [ -n "$version" ]; then
      echo "$version"
      return 0
    fi
    if [ "$attempt" -lt "$ATTEMPTS" ]; then
      sleep $((attempt * 15))
    fi
  done
  echo "::error::npm audit ($DIR): the registry gave no version of $pkg after $ATTEMPTS attempts" >&2
  return 2
}

declare -A reported=()
while IFS=$'\t' read -r id name severity title; do
  reported[$id]=1
  if ! jq -e --arg dir "$DIR" --arg id "$id" 'any(.[]; .dir == $dir and .advisory == $id)' \
    "$EXCEPTIONS" >/dev/null; then
    finding "$id ($name, $severity) has no exception: $title"
  fi
done < <(jq -r '[.vulnerabilities[].via[] | objects
  | select(.severity == "high" or .severity == "critical")
  | {id: ((.url // "") | split("/") | last), name, severity, title}]
  | unique_by(.id)[] | [.id, .name, .severity, .title] | @tsv' "$REPORT")

today="$(date -u +%F)"
while IFS=$'\t' read -r id pkg newest review_by reason; do
  if [ -z "${reported[$id]:-}" ]; then
    finding "the audit no longer reports $id ($pkg); delete its exception"
    continue
  fi
  if [[ "$today" > "$review_by" ]]; then
    finding "the exception for $id ($pkg) passed its review date $review_by; renew or remove it"
    continue
  fi
  latest="$(registry_latest "$pkg")" || exit 2
  if [ "$latest" != "$newest" ]; then
    finding "$pkg $latest is published, after the $newest the exception for $id recorded;" \
      "check whether it fixes the advisory"
    continue
  fi
  echo "npm audit ($DIR): $id ($pkg) excepted until $review_by: $reason"
done < <(jq -r --arg dir "$DIR" '.[] | select(.dir == $dir)
  | [.advisory, .package, .newest, .review_by, .reason] | @tsv' "$EXCEPTIONS")

if [ "$failed" -eq 0 ]; then
  echo "npm audit ($DIR): no high or critical advisory outside a live exception"
fi
exit "$failed"
