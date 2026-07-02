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

# --- VM-window ns/op gate: mock kubectl on PATH -------------------------------
# The ns/op gate reads a 14d window median (and sample count for cold-start) from
# VictoriaMetrics through scripts/lib/vm-query.sh's kubectl curl-pod transport.
# This mock serves canned /api/v1/query vectors keyed by {benchmark,lang},
# selecting median vs. count by the aggregation in the PromQL. VM_PROFILE picks a
# scenario; empty/transport-fail exercise fail-open (⇒ absolute-only, never red on
# infra). The mock also records its args so we can assert the current commit is
# excluded from the window query.
BIN_DIR="$WORK/bin"
mkdir -p "$BIN_DIR"
cat >"$BIN_DIR/kubectl" <<'EOF'
#!/usr/bin/env bash
set -uo pipefail
printf '%s\n' "$*" >>"${KUBECTL_ARGS:-/dev/null}"
args="$*"
vec() { printf '{"status":"success","data":{"resultType":"vector","result":[%s]}}\n' "$1"; }
s() { printf '{"metric":{"benchmark":"%s","lang":"%s"},"value":[2000,"%s"]}' "$1" "$2" "$3"; }
case "${VM_PROFILE:-full}" in
  empty) ;;
  *)
    if printf '%s' "$args" | grep -q 'count_over_time'; then
      # ${VM_COUNT:-10} runs per series — < NS_MIN_WINDOW_SAMPLES forces cold-start.
      vec "$(s BenchmarkEncodeFrame go "${VM_COUNT:-10}"),$(s BenchmarkDecodeFrame go "${VM_COUNT:-10}"),$(s encode_frame rust "${VM_COUNT:-10}")"
    elif printf '%s' "$args" | grep -q 'median_over_time'; then
      vec "$(s BenchmarkEncodeFrame go 123),$(s BenchmarkDecodeFrame go 245),$(s encode_frame rust 987)"
    fi
    ;;
esac
exit "${KUBECTL_STATUS:-0}"
EOF
chmod +x "$BIN_DIR/kubectl"

# Write a one-line go.txt benchmark for EncodeFrame at the given ns/op, with the
# baseline's own bytes/allocs so only the ns/op dimension moves.
write_go_ns() { printf 'BenchmarkEncodeFrame-8   1000000   %s ns/op   64 B/op   2 allocs/op\n' "$1" >"$WORK/go-ns.txt"; }

# Run the summarizer with the mock kubectl on PATH and the VM transport env.
# Per-case knobs (VM_PROFILE / VM_COUNT / KUBECTL_STATUS) are inherited.
run_ns_gate() {
  (
    export PATH="$BIN_DIR:$PATH"
    export KUBECTL_ARGS="$WORK/kubectl.args"
    GO_BENCH_FILE="$WORK/go-ns.txt" CRITERION_ROOT="$WORK/criterion" \
      BASELINE_FILE="$WORK/baseline.json" GITHUB_SHA="deadbeef" "$SUMMARIZE"
  )
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

ALERT_COUNT="$(GO_BENCH_FILE="$WORK/go-alloc-regression.txt" CRITERION_ROOT="$WORK/criterion" BASELINE_FILE="$WORK/baseline.json" "$SUMMARIZE" 2>&1 | grep -c '^REGRESSION_ALERT:' || true)"
if [ "$ALERT_COUNT" -gt 0 ]; then pass "regression emits alert lines"; else fail "regression should emit alert lines"; fi

echo
echo "ns/op VM-window gate:"

# Relative rule, isolated: 200 ns/op > window median 123 × (1 + 0.50) = 184.5, but
# < absolute ceiling (baseline 120 × 2 = 240). Only the window rule may fire.
write_go_ns 200
if OUT="$(run_ns_gate 2>&1)"; then
  fail "ns/op over the window band should fail red"
else
  if printf '%s\n' "$OUT" | grep -q '^REGRESSION_ALERT:.*ns_op'; then
    pass "ns/op over window median×1.5 reds and alerts (relative rule)"
  else
    fail "ns/op window regression should emit an ns_op alert (got: $OUT)"
  fi
fi

# Current commit must be excluded from the window query so a re-run never compares
# against its own just-pushed sample.
if grep -qF 'commit!="deadbeef"' "$WORK/kubectl.args"; then
  pass "window query excludes the current commit"
else
  fail "window query must exclude the current commit"
fi

# Sub-tol: 150 ns/op < 184.5 band and < 240 ceiling ⇒ silent (no red).
write_go_ns 150
if run_ns_gate >/dev/null 2>&1; then
  pass "ns/op inside the window band stays silent"
else
  fail "ns/op inside the window band should not fail"
fi

# Absolute ceiling, isolated: window empty (cold-start) so the relative rule is
# skipped; 300 ns/op > baseline 120 × 2 = 240 ⇒ the absolute backstop reds.
write_go_ns 300
if VM_PROFILE=empty run_ns_gate >/dev/null 2>&1; then
  fail "ns/op over the absolute ceiling should fail even with no window history"
else
  pass "ns/op over baseline×2 reds via the absolute backstop (cold-start)"
fi

# Cold-start fail-open: window empty AND under the ceiling ⇒ exit 0, never a red
# or an exit-2 on missing history.
write_go_ns 150
rc=0
VM_PROFILE=empty run_ns_gate >/dev/null 2>&1 || rc=$?
assert_eq "empty window under ceiling is fail-open (exit 0)" "0" "$rc"

# Transport failure fail-open: kubectl non-zero ⇒ absolute-only, no red on infra.
write_go_ns 200
rc=0
KUBECTL_STATUS=19 run_ns_gate >/dev/null 2>&1 || rc=$?
assert_eq "VM transport failure is fail-open (exit 0)" "0" "$rc"

# Thin window (fewer than NS_MIN_WINDOW_SAMPLES): the relative rule is skipped even
# though a median exists; 200 ns/op is over the band but under the ceiling ⇒ silent.
write_go_ns 200
if VM_COUNT=2 run_ns_gate >/dev/null 2>&1; then
  pass "thin window (< min samples) skips the relative rule, stays silent"
else
  fail "thin window should not red on the relative rule"
fi

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
