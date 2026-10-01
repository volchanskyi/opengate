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

# --- kubectl mock: hermetic for the WHOLE file --------------------------------
# The ns/op gate reads a 14d window median (and sample count for cold-start) from
# VictoriaMetrics through scripts/lib/vm-query.sh's kubectl curl-pod transport,
# and regression_check runs it on EVERY summarizer invocation — so the mock must
# be installed before the first run, not just around the ns/op cases. An unmocked
# call issues `kubectl -n monitoring run vm-query-$$ --rm …` against whatever
# cluster the caller is logged into, and the gate fails open, so the damage is
# silent. Three layers make that impossible: the mock shadows kubectl on PATH,
# KUBECONFIG names a nonexistent file, and the VM namespace/service are test-only
# names that match nothing real.
#
# The mock serves canned /api/v1/query vectors keyed by {benchmark,lang},
# selecting median vs. count by the aggregation in the PromQL. VM_PROFILE picks a
# scenario; empty/transport-fail exercise fail-open (⇒ absolute-only, never red on
# infra). The mock also records its args so we can assert the current commit is
# excluded from the window query — and that no query ever addressed a real target.
BIN_DIR="$WORK/bin"
mkdir -p "$BIN_DIR"
cat >"$BIN_DIR/kubectl" <<'EOF'
#!/usr/bin/env bash
set -uo pipefail
printf '%s\n' "$*" >>"${KUBECTL_ARGS:-/dev/null}"
# One export line per series: a reading a night, the last being last night's.
series() {
  local bench="$1" lang="$2" n i=0 v stamps=()
  shift 2
  n=$#
  for v in "$@"; do
    stamps+=("$(((STORE_MIDNIGHT - (n - i) * 86400 + 36000) * 1000))")
    i=$((i + 1))
  done
  printf '{"metric":{"__name__":"benchmark_ns_op","env":"ci","benchmark":"%s","lang":"%s"},"values":[%s],"timestamps":[%s]}\n' \
    "$bench" "$lang" "$(IFS=,; printf '%s' "$*")" "$(IFS=,; printf '%s' "${stamps[*]}")"
}
nights() { for _ in $(seq 1 "$2"); do printf '%s ' "$1"; done; }
case "${VM_PROFILE:-full}" in
  empty) ;;
  twelve)
    # Twelve nights on eight commits: four commits ran two nights each at 100
    # ns, then four ran one night each at 400–700.
    series BenchmarkEncodeFrame go 100 100 100 100 100 100 100 100 400 500 600 700
    ;;
  *)
    # ${VM_COUNT:-10} nights per series — fewer than NS_MIN_WINDOW_SAMPLES forces cold-start.
    read -r -a encode <<<"$(nights 123 "${VM_COUNT:-10}")"
    read -r -a decode <<<"$(nights 245 "${VM_COUNT:-10}")"
    read -r -a rust <<<"$(nights 987 "${VM_COUNT:-10}")"
    series BenchmarkEncodeFrame go "${encode[@]}"
    series BenchmarkDecodeFrame go "${decode[@]}"
    series encode_frame rust "${rust[@]}"
    ;;
esac
exit "${KUBECTL_STATUS:-0}"
EOF
chmod +x "$BIN_DIR/kubectl"

export PATH="$BIN_DIR:$PATH"
export KUBECONFIG="$WORK/nonexistent-kubeconfig"
export VM_NAMESPACE="benchmark-summarize-test"
export VM_SERVICE="benchmark-summarize-test-vm"
export KUBECTL_ARGS="$WORK/kubectl.args"
: >"$KUBECTL_ARGS"
# Tonight is the 29th; the store's nights end on the 28th.
VM_RUN_STARTED_AT="$(date -u -d '2026-09-29 10:33' +%s)"
STORE_MIDNIGHT="$(date -u -d '2026-09-29 00:00' +%s)"
export VM_RUN_STARTED_AT STORE_MIDNIGHT
# The summary page lands in the work directory, not wherever the suite ran from.
export BENCHMARK_SUMMARY_FILE="$WORK/benchmark-summary.md"

echo "kubectl hermeticity:"
assert_eq "kubectl resolves to the test mock" "$BIN_DIR/kubectl" "$(command -v kubectl)"

run_clean() {
  GO_BENCH_FILE="$WORK/go.txt" CRITERION_ROOT="$WORK/criterion" BASELINE_FILE="$WORK/baseline.json" \
    GITHUB_SHA="deadbeef" "$SUMMARIZE"
}

# Write a one-line go.txt benchmark for EncodeFrame at the given ns/op, with the
# baseline's own bytes/allocs so only the ns/op dimension moves.
write_go_ns() { printf 'BenchmarkEncodeFrame-8   1000000   %s ns/op   64 B/op   2 allocs/op\n' "$1" >"$WORK/go-ns.txt"; }

# Per-case knobs (VM_PROFILE / VM_COUNT / KUBECTL_STATUS) are inherited.
run_ns_gate() {
  GO_BENCH_FILE="$WORK/go-ns.txt" CRITERION_ROOT="$WORK/criterion" \
    BASELINE_FILE="$WORK/baseline.json" GITHUB_SHA="deadbeef" "$SUMMARIZE"
}

echo
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
# Criterion runs data-only now (html_reports dropped): the summarizer must parse
# ns/op from new/estimates.json alone, with no report/ HTML in the artifact.
assert_eq "criterion fixture is data-only (no HTML report)" "" "$(find "$WORK/criterion" -name '*.html' -print -quit)"
assert_eq "commit tagged" "deadbeef" "$(jq -r '.[0].commit' <<<"$ROWS")"
# The canonical run also crosses the VM gate, so its one window read must be
# served by the mock — proof the summarizer never reaches a cluster outside the
# ns/op cases.
assert_eq "clean run's window read went through the mock" "1" "$(grep -c 'api/v1/export' "$KUBECTL_ARGS")"

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
  if grep -q '^REGRESSION_ALERT:.*ns_op' <<<"$OUT"; then
    pass "ns/op over window median×1.5 reds and alerts (relative rule)"
  else
    fail "ns/op window regression should emit an ns_op alert (got: $OUT)"
  fi
fi

# The window is nights, read by date; a re-run of tonight is kept out by its
# date rather than by the commit it ran.
if grep -qF 'commit' "$WORK/kubectl.args"; then
  fail "the window read asks nothing about commits"
else
  pass "the window read asks nothing about commits"
fi

# Twelve nights on eight commits. Folded to one point per commit the median was
# 250 and 200 ns passed under a band of 375; over the twelve nights it is 100,
# and 200 is past the band of 150.
write_go_ns 200
if OUT="$(VM_PROFILE=twelve run_ns_gate 2>&1)"; then
  fail "twelve nights on eight commits are judged against the twelve-night median"
elif grep -q '^REGRESSION_ALERT:.*ns_op: 100 ' <<<"$OUT"; then
  pass "twelve nights on eight commits are judged against the twelve-night median"
else
  fail "twelve nights on eight commits are judged against the twelve-night median (got: $OUT)"
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
echo "summary page:"
# The benchmark dumped its rows as JSON. The check that judges them writes the
# page: each reading beside what it is held to, and what the check made of it.
write_go_ns 200
BENCHMARK_SUMMARY_FILE="$WORK/bench-table.md" run_ns_gate >/dev/null 2>&1 || true
table="$(cat "$WORK/bench-table.md" 2>/dev/null || true)"
if grep -qxF '| Measurement | Expected | Actual | Result |' <<<"$table"; then
  pass "the page is the four-column table"
else
  fail "the page is the four-column table (got: $table)"
fi
if grep -qF '| go/BenchmarkEncodeFrame ns/op | ≤ 184.5 ns (nights'"'"' median × 1.5); ≤ 240 ns (baseline × 2) | 200 ns | FAIL |' <<<"$table"; then
  pass "a timing past the nights' band is FAIL, with both of its limits named"
else
  fail "a timing past the nights' band is FAIL, with both of its limits named (got: $table)"
fi
if grep -qF '| go/BenchmarkEncodeFrame allocs/op | ≤ 2.04 (baseline 2 + 2 %) | 2 | pass |' <<<"$table"; then
  pass "an allocation count inside its baseline passes"
else
  fail "an allocation count inside its baseline passes (got: $table)"
fi
if grep -qF -- '- **Expected**' <<<"$table"; then pass "the page carries the legend"; else fail "the page carries the legend"; fi

echo
echo "baseline generation:"
BASELINE_OUT="$(GO_BENCH_FILE="$WORK/go.txt" CRITERION_ROOT="$WORK/criterion" "$SUMMARIZE" --update-baseline)"
assert_eq "baseline version" "1" "$(jq -r '.version' <<<"$BASELINE_OUT")"
assert_eq "baseline contains three rows" "3" "$(jq -r '.benchmarks | length' <<<"$BASELINE_OUT")"

rc=0
GO_BENCH_FILE="$WORK/missing.txt" CRITERION_ROOT="$WORK/criterion" BASELINE_FILE="$WORK/baseline.json" "$SUMMARIZE" >/dev/null 2>&1 || rc=$?
if [ "$rc" -eq 2 ]; then pass "missing Go file exits 2"; else fail "missing Go file expected exit 2, got $rc"; fi

echo
echo "kubectl blast radius:"
# Every query recorded across the whole file must carry the test-only namespace.
# A single line naming the real monitoring namespace means an invocation escaped
# the mock and addressed the live cluster.
assert_eq "no query addressed the real monitoring namespace" "0" \
  "$(grep -c -- '-n monitoring ' "$KUBECTL_ARGS" || true)"
assert_eq "every recorded query used the test namespace" "0" \
  "$(grep -c -v -- "-n $VM_NAMESPACE " "$KUBECTL_ARGS" || true)"

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
