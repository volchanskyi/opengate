#!/usr/bin/env bash
# Tests for scripts/fault/drill-summary.sh: a staging drill's job summary names the drill, its
# result, its recovery time beside the limit, and whether a fault was left on the edge.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
SUMMARY="$REPO_ROOT/scripts/fault/drill-summary.sh"
[ -x "$SUMMARY" ] || {
  echo "FAIL: $SUMMARY not executable" >&2
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
# has NAME LINE OUTPUT passes when LINE is a whole line of OUTPUT.
has() {
  if grep -qxF -- "$2" <<<"$3"; then pass "$1"; else fail "$1 (missing [$2] in [$3])"; fi
}

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

echo "fault drill summary:"

echo "34 120" >"$WORK/pod-delete-recovery.txt"
out="$("$SUMMARY" pod-delete "$WORK" success success)"
has "the drill is named" "## Staging fault drill: pod-delete" "$out"
has "a recovery inside its limit passes" \
  "**Result:** pass: the server came back in 34 s, inside its 120 s limit." "$out"
has "the row reads the drill, its time and the edge" \
  "| pod-delete | pass | 34 s (limit 120 s) | none |" "$out"

echo "121 120" >"$WORK/pod-delete-recovery.txt"
out="$("$SUMMARY" pod-delete "$WORK" failure success)"
has "a recovery past its limit fails" \
  "**Result:** FAIL: the server took 121 s, past its 120 s limit." "$out"
has "the failing row reads FAIL" "| pod-delete | FAIL | 121 s (limit 120 s) | none |" "$out"

out="$("$SUMMARY" ingress-504 "$WORK" success failure)"
has "a fault left on the edge fails the drill" \
  "**Result:** FAIL: a fault was left on the staging edge." "$out"
has "a drill that measures no recovery says so" "| ingress-504 | pass | not measured | FOUND |" "$out"

rm -f "$WORK/pod-delete-recovery.txt"
out="$("$SUMMARY" pod-delete "$WORK" failure skipped)"
has "a drill that stopped before recovering says so" \
  "**Result:** FAIL: the drill stopped before the server came back." "$out"
has "an unread recovery and an unchecked edge are named" \
  "| pod-delete | FAIL | not measured | not checked |" "$out"

out="$("$SUMMARY" pod-delete "$WORK" cancelled cancelled)"
has "a cancelled drill reads cancelled" "| pod-delete | cancelled | not measured | not checked |" "$out"

rc=0
"$SUMMARY" pod-delete "$WORK" >/dev/null 2>&1 || rc=$?
assert_eq "too few arguments are a usage error" "2" "$rc"

echo
echo "fault drill summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
