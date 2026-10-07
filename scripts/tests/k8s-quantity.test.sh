#!/usr/bin/env bash
# Tests for scripts/k8s-quantity.sh, which converts cluster quantities such as 250m and 384Mi.
# Binary and decimal suffixes stay distinct: 384Mi is 402653184 bytes and 384M is 384000000.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
CONVERT="$REPO_ROOT/scripts/k8s-quantity.sh"
[ -x "$CONVERT" ] || {
  echo "FAIL: $CONVERT not executable" >&2
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

echo "k8s-quantity:"

# Processors, which the cluster writes in thousandths.
assert_eq "a milli share becomes a fraction" "0.25" "$("$CONVERT" cpu 250m)"
assert_eq "half a processor" "0.5" "$("$CONVERT" cpu 500m)"
assert_eq "a whole processor written plainly" "1" "$("$CONVERT" cpu 1)"
assert_eq "a whole processor written in thousandths" "2" "$("$CONVERT" cpu 2000m)"
assert_eq "a fraction written plainly" "0.5" "$("$CONVERT" cpu 0.5)"

# Memory, where the binary and decimal suffixes are different numbers.
assert_eq "a binary mebibyte multiple" "402653184" "$("$CONVERT" memory 384Mi)"
assert_eq "a decimal megabyte multiple is not the same number" "384000000" "$("$CONVERT" memory 384M)"
assert_eq "a binary gibibyte" "1073741824" "$("$CONVERT" memory 1Gi)"
assert_eq "a kibibyte multiple" "393216" "$("$CONVERT" memory 384Ki)"
assert_eq "bytes with no suffix at all" "512" "$("$CONVERT" memory 512)"

# A quantity nobody could read must not become a zero: zero bytes is the
# smallest machine ever measured, and the run would be judged against it.
STATUS=0
"$CONVERT" memory "" >/dev/null 2>&1 || STATUS=$?
assert_eq "an empty quantity fails" "1" "$STATUS"

STATUS=0
"$CONVERT" memory "lots" >/dev/null 2>&1 || STATUS=$?
assert_eq "a quantity that is not a number fails" "1" "$STATUS"

STATUS=0
"$CONVERT" disk 10Gi >/dev/null 2>&1 || STATUS=$?
assert_eq "a kind it does not convert is refused" "2" "$STATUS"

STATUS=0
"$CONVERT" cpu >/dev/null 2>&1 || STATUS=$?
assert_eq "a call with no quantity is refused" "2" "$STATUS"

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf 'Failures:\n' >&2
  for f in "${FAILURES[@]}"; do printf '  - %s\n' "$f" >&2; done
  exit 1
fi
