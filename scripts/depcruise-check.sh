#!/usr/bin/env bash
# Fails on any dependency-cruiser error in web/src, and on warnings past the snapshot count,
# or on any warning once the gate's flip marker exists.
#
# Usage:
#   scripts/depcruise-check.sh
#
# Exit codes:
#   0  no error, and warnings within what the gate allows
#   1  an error, or warnings past what the gate allows
#   2  the report or the snapshot could not be read
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SNAPSHOT="$ROOT/web/dependency-cruiser.snapshot.json"
MARKER="$ROOT/.claude/.markers/arch-lint-flipped/depcruise"

refuse() {
  echo "::error::depcruise: $1" >&2
  exit 2
}

# depcruise exits non-zero whenever it reports an error, so the report is judged on its summary.
report="$(cd "$ROOT/web" && npx --no-install depcruise src --output-type json --no-progress \
  2>/dev/null || true)"
errors="$(jq -er '.summary.error' <<<"$report" 2>/dev/null)" || refuse "the report is not readable JSON"
warnings="$(jq -er '.summary.warn' <<<"$report" 2>/dev/null)" || refuse "the report has no warn count"

failed=0
if [ "$errors" -gt 0 ]; then
  echo "::error::depcruise reports $errors error(s):" >&2
  jq -r '.summary.violations[] | select(.rule.severity == "error")
    | "  \(.rule.name): \(.from) -> \(.to)"' <<<"$report" >&2
  failed=1
fi

if [ -f "$MARKER" ]; then
  if [ "$warnings" -gt 0 ]; then
    echo "::error::depcruise (flipped to error mode) violations: current=$warnings." >&2
    failed=1
  fi
else
  baseline="$(jq -er '.warn' "$SNAPSHOT" 2>/dev/null)" || refuse "$SNAPSHOT has no warn count"
  if [ "$warnings" -gt "$baseline" ]; then
    echo "::error::depcruise warning count grew: current=$warnings baseline=$baseline." >&2
    echo "Fix the new violation, or raise .warn in web/dependency-cruiser.snapshot.json." >&2
    failed=1
  fi
fi

if [ "$failed" -eq 0 ]; then
  echo "depcruise: no error, $warnings warning(s) within what the gate allows"
fi
exit "$failed"
