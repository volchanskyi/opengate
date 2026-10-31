#!/usr/bin/env bash
# Tests for scripts/benchmark-summarize.sh and the deterministic benchmark gate.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SUMMARIZE="$SCRIPT_DIR/../benchmark-summarize.sh"
# shellcheck source=scripts/lib/summary-table.sh
. "$SCRIPT_DIR/../lib/summary-table.sh"
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

# The kubectl mock serves canned VictoriaMetrics vectors and records its args for the whole file.
# KUBECONFIG names a nonexistent file and the VM namespace and service match nothing real.
BIN_DIR="$WORK/bin"
mkdir -p "$BIN_DIR"
cat >"$BIN_DIR/kubectl" <<'EOF'
#!/usr/bin/env bash
set -uo pipefail
printf '%s\n' "$*" >>"${KUBECTL_ARGS:-/dev/null}"
metric=benchmark_allocs_op
[[ "$*" =~ match\[\]=([a-z_]+) ]] && metric="${BASH_REMATCH[1]}"
# One export line per series of the asked metric: a reading a night, the last being last night's.
series() {
  local want="$1" bench="$2" lang="$3" n i=0 v stamps=()
  shift 3
  [ "$want" = "$metric" ] || return 0
  n=$#
  for v in "$@"; do
    stamps+=("$(((STORE_MIDNIGHT - (n - i) * 86400 + 36000) * 1000))")
    i=$((i + 1))
  done
  printf '{"metric":{"__name__":"%s","env":"ci","benchmark":"%s","lang":"%s"},"values":[%s],"timestamps":[%s]}\n' \
    "$metric" "$bench" "$lang" "$(IFS=,; printf '%s' "$*")" "$(IFS=,; printf '%s' "${stamps[*]}")"
}
nights() { for _ in $(seq 1 "$2"); do printf '%s ' "$1"; done; }
case "${VM_PROFILE:-full}" in
  empty) ;;
  fresh)
    # A benchmark with no committed numbers, ${VM_COUNT:-3} nights since its history began.
    read -r -a ns <<<"$(nights 100 "${VM_COUNT:-3}")"
    read -r -a allocs <<<"$(nights 2 "${VM_COUNT:-3}")"
    read -r -a bytes <<<"$(nights 64 "${VM_COUNT:-3}")"
    series benchmark_ns_op BenchmarkFresh go "${ns[@]}"
    series benchmark_allocs_op BenchmarkFresh go "${allocs[@]}"
    series benchmark_bytes_op BenchmarkFresh go "${bytes[@]}"
    ;;
  *)
    # Ten nights of allocations a series.
    read -r -a encode <<<"$(nights 2 10)"
    read -r -a decode <<<"$(nights 4 10)"
    series benchmark_allocs_op BenchmarkEncodeFrame go "${encode[@]}"
    series benchmark_allocs_op BenchmarkDecodeFrame go "${decode[@]}"
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
export BENCHMARK_SUMMARY_FILE="$WORK/benchmark-summary.md"

echo "kubectl hermeticity:"
assert_eq "kubectl resolves to the test mock" "$BIN_DIR/kubectl" "$(command -v kubectl)"

run_clean() {
  GO_BENCH_FILE="$WORK/go.txt" CRITERION_ROOT="$WORK/criterion" BASELINE_FILE="$WORK/baseline.json" \
    GITHUB_SHA="deadbeef" "$SUMMARIZE"
}

write_go_ns() { printf 'BenchmarkEncodeFrame-8   1000000   %s ns/op   64 B/op   2 allocs/op\n' "$1" >"$WORK/go-ns.txt"; }

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
assert_eq "criterion fixture is data-only (no HTML report)" "" "$(find "$WORK/criterion" -name '*.html' -print -quit)"
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
echo "timings:"

# Tonight's timings are judged against the anchor commit on one machine, so here they judge nothing.
mkdir -p "$WORK/criterion-slow/encode_frame/new"
printf '{"mean":{"point_estimate":5000.0}}\n' >"$WORK/criterion-slow/encode_frame/new/estimates.json"
write_go_ns 900
rc=0
OUT="$(GO_BENCH_FILE="$WORK/go-ns.txt" CRITERION_ROOT="$WORK/criterion-slow" \
  BASELINE_FILE="$WORK/baseline.json" GITHUB_SHA="deadbeef" "$SUMMARIZE" 2>&1)" || rc=$?
assert_eq "a timing far past its baseline fails nothing here" "0" "$rc"
if grep -q '^REGRESSION_ALERT:' <<<"$OUT"; then
  fail "a timing raises no alert here"
else
  pass "a timing raises no alert here"
fi
assert_num_eq "and is still carried in its row for the trend" "900" \
  "$(grep -E '^\[' <<<"$OUT" | tail -n1 | jq -r '.[] | select(.name=="BenchmarkEncodeFrame") | .ns_op')"
assert_eq "a run whose rows hold committed allocations reads no nights" "0" \
  "$(grep -c 'api/v1/export' "$KUBECTL_ARGS" || true)"

echo
echo "summary page:"
write_go_ns 200
BENCHMARK_SUMMARY_FILE="$WORK/bench-table.md" run_ns_gate >/dev/null 2>&1 || true
table="$(cat "$WORK/bench-table.md" 2>/dev/null || true)"
if grep -qxF "$(summary_table_header | sed -n 1p)" <<<"$table"; then
  pass "the page is the four-column table"
else
  fail "the page is the four-column table (got: $table)"
fi
if grep -qF "$(summary_term ns/op)" <<<"$table"; then
  fail "the page leaves timings to the anchor comparison (got: $table)"
else
  pass "the page leaves timings to the anchor comparison"
fi
if grep -qF "| go/BenchmarkEncodeFrame $(summary_term allocs/op) | ≤ 2.04 (baseline 2 + 2 %) | 2 | pass |" <<<"$table"; then
  pass "an allocation count inside its baseline passes"
else
  fail "an allocation count inside its baseline passes (got: $table)"
fi
if grep -q '^- \*\*' <<<"$table"; then fail "no legend follows the table"; else pass "no legend follows the table"; fi

echo
echo "history_from:"
cat >"$WORK/baseline-history.json" <<'JSON'
{
  "version": 1,
  "default_tolerances": { "allocs_op": 0.02, "bytes_op": 0.02 },
  "benchmarks": [
    { "name": "BenchmarkEncodeFrame", "lang": "go", "bytes_op": 64, "allocs_op": 2,
      "history_from": "2026-09-26" },
    { "name": "BenchmarkFresh", "lang": "go", "history_from": "2026-09-26" },
    { "name": "encode_frame", "lang": "rust", "ns_op": 900.0, "bytes_op": null, "allocs_op": null }
  ]
}
JSON
run_history() {
  GO_BENCH_FILE="$WORK/go-history.txt" CRITERION_ROOT="$WORK/criterion" \
    BASELINE_FILE="$WORK/baseline-history.json" GITHUB_SHA="deadbeef" "$SUMMARIZE"
}

printf 'BenchmarkFresh-8   1000000   100 ns/op   64 B/op   3 allocs/op\n' >"$WORK/go-history.txt"
if VM_PROFILE=fresh run_history >/dev/null 2>&1; then
  fail "a row with no numbers is held to the median of its nights once three exist"
else
  pass "a row with no numbers is held to the median of its nights once three exist"
fi

rc=0
VM_PROFILE=fresh VM_COUNT=2 run_history >/dev/null 2>&1 || rc=$?
assert_eq "a row with fewer than three nights since history_from is reported, not gated" "0" "$rc"
BENCHMARK_SUMMARY_FILE="$WORK/fresh-table.md" VM_PROFILE=fresh VM_COUNT=2 run_history >/dev/null 2>&1 || true
if grep -qF "| go/BenchmarkFresh $(summary_term allocs/op) | no limit yet (2 of 3 nights since 2026-09-26) | 3 | — |" "$WORK/fresh-table.md"; then
  pass "the page says how many nights a new row still needs"
else
  fail "the page says how many nights a new row still needs (got: $(cat "$WORK/fresh-table.md"))"
fi

echo
echo "baseline generation:"
BASELINE_OUT="$(GO_BENCH_FILE="$WORK/go.txt" CRITERION_ROOT="$WORK/criterion" "$SUMMARIZE" --update-baseline)"
assert_eq "baseline version" "1" "$(jq -r '.version' <<<"$BASELINE_OUT")"
assert_eq "baseline contains three rows" "3" "$(jq -r '.benchmarks | length' <<<"$BASELINE_OUT")"
assert_eq "no unread time tolerance is written" "null" "$(jq -r '.default_tolerances.ns_op' <<<"$BASELINE_OUT")"
KEPT="$(GO_BENCH_FILE="$WORK/go.txt" CRITERION_ROOT="$WORK/criterion" BASELINE_FILE="$WORK/baseline-history.json" \
  "$SUMMARIZE" --update-baseline)"
assert_eq "a regenerated baseline keeps each row's history_from" "2026-09-26" \
  "$(jq -r '.benchmarks[] | select(.name == "BenchmarkEncodeFrame") | .history_from' <<<"$KEPT")"
jq '.anchor = "a1b2c3"' "$WORK/baseline-history.json" >"$WORK/baseline-anchor.json"
ANCHORED="$(GO_BENCH_FILE="$WORK/go.txt" CRITERION_ROOT="$WORK/criterion" BASELINE_FILE="$WORK/baseline-anchor.json" \
  "$SUMMARIZE" --update-baseline)"
assert_eq "and the anchor tonight is compared with, which only a commit accepting a change moves" \
  "a1b2c3" "$(jq -r '.anchor' <<<"$ANCHORED")"

rc=0
GO_BENCH_FILE="$WORK/missing.txt" CRITERION_ROOT="$WORK/criterion" BASELINE_FILE="$WORK/baseline.json" "$SUMMARIZE" >/dev/null 2>&1 || rc=$?
if [ "$rc" -eq 2 ]; then pass "missing Go file exits 2"; else fail "missing Go file expected exit 2, got $rc"; fi

echo
echo "kubectl blast radius:"
# Every recorded query carries the test-only namespace.
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
