#!/usr/bin/env bash
# A report that comes out still absolute, or naming no source, fails so it is never uploaded.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
CHECK="$REPO_ROOT/scripts/rust-lcov-relativize.sh"
[ -x "$CHECK" ] || {
  echo "FAIL: $CHECK not executable" >&2
  exit 1
}

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
assert_eq() {
  local name="$1" want="$2" got="$3"
  if [ "$want" = "$got" ]; then pass "$name"; else fail "$name (want=[$want] got=[$got])"; fi
}

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

run_check() {
  STATUS=0
  "$CHECK" "$WORK/lcov.info" "$1" >"$WORK/out.txt" 2>"$WORK/err.txt" || STATUS=$?
}

echo "rust-lcov-relativize:"

cat >"$WORK/lcov.info" <<'LCOV'
TN:
SF:/home/ivan/opengate/agent/crates/mesh-agent-core/src/update.rs
DA:1,3
DA:2,0
LF:2
LH:1
end_of_record
TN:
SF:/home/ivan/opengate/agent/crates/edge-tsdb/src/bitio.rs
DA:1,7
LF:1
LH:1
end_of_record
LCOV
run_check /home/ivan/opengate
assert_eq "a report of absolute paths is rewritten" "0" "$STATUS"
assert_eq "every path is now repository-relative" "2" \
  "$(grep -c '^SF:agent/crates/' "$WORK/lcov.info")"
assert_eq "no absolute path survives" "0" "$(grep -c '^SF:/' "$WORK/lcov.info" || true)"
assert_eq "the coverage counts are untouched" "DA:1,3 DA:2,0 DA:1,7" \
  "$(grep '^DA:' "$WORK/lcov.info" | tr '\n' ' ' | sed 's/ $//')"

cat >"$WORK/lcov.info" <<'LCOV'
SF:/build/repo/agent/crates/a/src/lib.rs
DA:1,1
end_of_record
LCOV
run_check /build/repo/
assert_eq "a root given with a trailing slash is the same root" "0" "$STATUS"
assert_eq "and the path is still relativized" "SF:agent/crates/a/src/lib.rs" \
  "$(grep '^SF:' "$WORK/lcov.info")"

run_check /build/repo
assert_eq "a second run leaves an already-relative report alone" "0" "$STATUS"
assert_eq "and the path is unchanged" "SF:agent/crates/a/src/lib.rs" \
  "$(grep '^SF:' "$WORK/lcov.info")"

cat >"$WORK/lcov.info" <<'LCOV'
SF:/somewhere/else/agent/crates/a/src/lib.rs
DA:1,1
end_of_record
LCOV
run_check /build/repo
assert_eq "a path outside the root fails rather than uploading" "1" "$STATUS"
if grep -q "still absolute" "$WORK/err.txt"; then
  pass "and says which paths would have been dropped"
else
  fail "and says which paths would have been dropped"
fi

printf 'TN:\n' >"$WORK/lcov.info"
run_check /build/repo
assert_eq "a report naming no source fails" "1" "$STATUS"

rm -f "$WORK/lcov.info"
run_check /build/repo
assert_eq "an absent report fails" "1" "$STATUS"

STATUS=0
"$CHECK" >/dev/null 2>&1 || STATUS=$?
assert_eq "no arguments is a usage error" "2" "$STATUS"

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
