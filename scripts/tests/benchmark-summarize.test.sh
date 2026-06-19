#!/usr/bin/env bash
# Tests for scripts/benchmark-summarize.sh and the deterministic benchmark gate.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SUMMARIZE="$SCRIPT_DIR/../benchmark-summarize.sh"
[ -x "$SUMMARIZE" ] || {
  echo "FAIL: $SUMMARIZE not executable" >&2
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
assert_num_eq() {
  local name="$1" want="$2" got="$3"
  if awk -v w="$want" -v g="$got" 'BEGIN { exit !(g == w) }'; then pass "$name"; else fail "$name (want=[$want] got=[$got])"; fi
}

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

cat >"$WORK/go.txt" <<'GO'
goos: linux
goarch: amd64
pkg: github.com/volchanskyi/opengate/server/internal/protocol
cpu: AMD EPYC
BenchmarkEncodeFrame-8        1000000      123.4 ns/op      64 B/op      2 allocs/op
BenchmarkDecodeFrame-8         500000      245.0 ns/op     128 B/op      4 allocs/op
PASS
ok  github.com/volchanskyi/opengate/server/internal/protocol  1.234s
GO

mkdir -p "$WORK/criterion/encode_frame/new"
cat >"$WORK/criterion/encode_frame/new/estimates.json" <<'JSON'
{
  "mean": { "point_estimate": 987.6 },
  "median": { "point_estimate": 970.0 }
}
JSON

cat >"$WORK/baseline.json" <<'JSON'
{
  "version": 1,
  "default_tolerances": {
    "allocs_op": 0.02,
    "bytes_op": 0.02,
    "ns_op": 1.50
  },
  "benchmarks": [
    { "name": "BenchmarkEncodeFrame", "lang": "go", "ns_op": 120.0, "bytes_op": 64, "allocs_op": 2 },
    { "name": "BenchmarkDecodeFrame", "lang": "go", "ns_op": 200.0, "bytes_op": 128, "allocs_op": 4 },
    { "name": "encode_frame", "lang": "rust", "ns_op": 900.0, "bytes_op": null, "allocs_op": null }
  ]
}
JSON

run_clean() {
  GO_BENCH_FILE="$WORK/go.txt" CRITERION_ROOT="$WORK/criterion" BASELINE_FILE="$WORK/baseline.json" \
    GITHUB_SHA="deadbeef" "$SUMMARIZE"
}

echo "canonical rows:"
OUT="$(run_clean)"
RC=$?
ROWS="$(printf '%s\n' "$OUT" | grep -E '^\[' | tail -n1)"
assert_eq "clean summary exits 0" "0" "$RC"
assert_eq "three benchmark rows" "3" "$(jq 'length' <<<"$ROWS")"
assert_eq "Go benchmark suffix stripped" "BenchmarkEncodeFrame" "$(jq -r '.[] | select(.name=="BenchmarkEncodeFrame") | .name' <<<"$ROWS")"
assert_num_eq "Go ns/op parsed" "123.4" "$(jq -r '.[] | select(.name=="BenchmarkEncodeFrame") | .ns_op' <<<"$ROWS")"
assert_eq "Go B/op parsed" "64" "$(jq -r '.[] | select(.name=="BenchmarkEncodeFrame") | .bytes_op' <<<"$ROWS")"
assert_eq "Go allocs/op parsed" "2" "$(jq -r '.[] | select(.name=="BenchmarkEncodeFrame") | .allocs_op' <<<"$ROWS")"
assert_num_eq "criterion ns/op parsed" "987.6" "$(jq -r '.[] | select(.name=="encode_frame") | .ns_op' <<<"$ROWS")"
assert_eq "criterion allocations are unavailable" "null" "$(jq -r '.[] | select(.name=="encode_frame") | .allocs_op' <<<"$ROWS")"
assert_eq "commit tagged" "deadbeef" "$(jq -r '.[0].commit' <<<"$ROWS")"

echo
echo "regression gate:"
cat >"$WORK/go-alloc-regression.txt" <<'GO'
BenchmarkEncodeFrame-8        1000000      123.4 ns/op      64 B/op      3 allocs/op
GO
if GO_BENCH_FILE="$WORK/go-alloc-regression.txt" CRITERION_ROOT="$WORK/criterion" BASELINE_FILE="$WORK/baseline.json" "$SUMMARIZE" >/dev/null 2>&1; then
  fail "allocs/op bump should fail"
else
  pass "allocs/op bump fails"
fi

cat >"$WORK/go-ns-wobble.txt" <<'GO'
BenchmarkEncodeFrame-8        1000000      1000.0 ns/op      64 B/op      2 allocs/op
GO
if GO_BENCH_FILE="$WORK/go-ns-wobble.txt" CRITERION_ROOT="$WORK/criterion" BASELINE_FILE="$WORK/baseline.json" "$SUMMARIZE" >/dev/null 2>&1; then
  pass "ns/op-only wobble stays advisory"
else
  fail "ns/op-only wobble should not fail"
fi

ALERT_COUNT="$(GO_BENCH_FILE="$WORK/go-alloc-regression.txt" CRITERION_ROOT="$WORK/criterion" BASELINE_FILE="$WORK/baseline.json" "$SUMMARIZE" 2>&1 | grep -c '^REGRESSION_ALERT:' || true)"
if [ "$ALERT_COUNT" -gt 0 ]; then pass "regression emits alert lines"; else fail "regression should emit alert lines"; fi

echo
echo "baseline generation:"
BASELINE_OUT="$(GO_BENCH_FILE="$WORK/go.txt" CRITERION_ROOT="$WORK/criterion" "$SUMMARIZE" --update-baseline)"
assert_eq "baseline version" "1" "$(jq -r '.version' <<<"$BASELINE_OUT")"
assert_eq "baseline contains three rows" "3" "$(jq -r '.benchmarks | length' <<<"$BASELINE_OUT")"

rc=0
GO_BENCH_FILE="$WORK/missing.txt" CRITERION_ROOT="$WORK/criterion" BASELINE_FILE="$WORK/baseline.json" "$SUMMARIZE" >/dev/null 2>&1 || rc=$?
if [ "$rc" -eq 2 ]; then pass "missing Go file exits 2"; else fail "missing Go file expected exit 2, got $rc"; fi

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
