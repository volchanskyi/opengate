#!/usr/bin/env bash
# Tests the boundary rules in web/eslint.config.js end to end: each probe import is linted from
# standard input under an existing file's path, and the rule must refuse or allow it.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
WEB="$ROOT/web"

PASS=0
FAIL=0
FAILURES=()
pass() {
  PASS=$((PASS + 1))
  printf '  ok   %s\n' "$1"
}
fail() {
  FAIL=$((FAIL + 1))
  FAILURES+=("$1")
  printf '  FAIL %s\n' "$1" >&2
}

if [ ! -x "$WEB/node_modules/.bin/eslint" ]; then
  echo "web/node_modules has no eslint: run 'npm ci' in web/ first." >&2
  exit 2
fi

# Prints refused, allowed, unparsed or unreadable for one import linted as FROM.
verdict() {
  local from="$1" target="$2" probe report
  probe="$(printf "import * as target from '%s';\nexport const probe = target;\n" "$target")"
  report="$(cd "$WEB" && npx eslint --stdin --stdin-filename "$from" --format json <<<"$probe" \
    2>/dev/null || true)"
  jq -r '.[0].messages as $m
    | if ($m | map(select(.fatal)) | length) > 0 then "unparsed"
      elif ($m | map(select(.ruleId == "boundaries/dependencies")) | length) > 0 then "refused"
      else "allowed" end' <<<"$report" 2>/dev/null || echo "unreadable"
}

expect() {
  local from="$1" target="$2" want="$3" got
  if [ ! -f "$WEB/$from" ]; then
    fail "$from exists to lend its path to the probe"
    return
  fi
  got="$(verdict "$from" "$target")"
  if [ "$got" = "$want" ]; then
    pass "$from importing $target is $want"
  else
    fail "$from importing $target is $want (got $got)"
  fi
}

echo "eslint-boundaries:"

expect src/lib/fire-and-forget.ts ../features/devices/AmtBadge refused
expect src/lib/fire-and-forget.ts ../state/auth-store refused
expect src/lib/fire-and-forget.ts ../App refused
expect src/lib/fire-and-forget.ts ./api allowed
expect src/state/auth-store.ts ../features/devices/AmtBadge refused
expect src/state/auth-store.ts ../lib/api allowed
expect src/features/devices/AmtBadge.tsx ../../App refused
expect src/features/devices/AmtBadge.tsx ../../lib/api allowed
expect src/features/devices/AmtBadge.tsx ../../state/auth-store allowed
expect src/features/devices/AmtBadge.tsx ../auth/LoginPage allowed
expect src/router.tsx ./features/devices/AmtBadge allowed
expect src/router.tsx ./state/auth-store allowed

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
