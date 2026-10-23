#!/usr/bin/env bash
# Tests for scripts/benchmark-confirm.sh judge, over fixed sample files.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CONFIRM="$SCRIPT_DIR/../benchmark-confirm.sh"
[ -x "$CONFIRM" ] || {
  echo "FAIL: $CONFIRM not executable" >&2
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

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
export BENCHMARK_CONFIRM_SUMMARY="$WORK/confirm.md"

# window NAME VALUE… writes the nights' readings as Go benchmark lines.
window() {
  local name="$1" v
  shift
  for v in "$@"; do printf '%s 1 %s ns/op\n' "$name" "$v"; done >>"$WORK/window.txt"
}
# rerun NAME VALUE… writes ten re-run readings as `go test -cpu 4` prints them, between the header
# lines it opens each package with and the lines it closes each package with.
rerun() {
  local name="$1" v
  shift
  {
    printf 'goos: linux\ngoarch: amd64\n'
    printf 'pkg: github.com/volchanskyi/opengate/server/internal/protocol\n'
    printf 'cpu: AMD EPYC 7763 64-Core Processor\n'
    for v in "$@"; do printf '%s-4 \t 1000000\t %s ns/op\t 64 B/op\t 2 allocs/op\n' "$name" "$v"; done
    printf 'PASS\nok  \tgithub.com/volchanskyi/opengate/server/internal/protocol\t12.345s\n'
  } >>"$WORK/rerun.txt"
}
judge() {
  : >"$BENCHMARK_CONFIRM_SUMMARY"
  "$CONFIRM" judge "$WORK/flagged.tsv" "$WORK/window.txt" "$WORK/rerun.txt"
}
reset() { : >"$WORK/window.txt" && : >"$WORK/rerun.txt" && : >"$WORK/flagged.tsv"; }

echo "benchmark-confirm judge:"

reset
printf 'BenchmarkSteady\t150\t5\n' >"$WORK/flagged.tsv"
window BenchmarkSteady 100 102 98 101 99
rerun BenchmarkSteady 99 101 100 102 98 100 101 99 100 300
rc=0
judge >"$WORK/out" 2>&1 || rc=$?
if [ "$rc" -eq 0 ]; then pass "one outlier among ten passes"; else fail "one outlier among ten passes (rc=$rc: $(cat "$WORK/out"))"; fi
if grep -qF '~' "$BENCHMARK_CONFIRM_SUMMARY"; then
  pass "the summary quotes benchstat's ~"
else
  fail "the summary quotes benchstat's ~ (got: $(cat "$BENCHMARK_CONFIRM_SUMMARY"))"
fi

reset
printf 'BenchmarkSlower\t120\t5\n' >"$WORK/flagged.tsv"
window BenchmarkSlower 100 102 98 101 99
rerun BenchmarkSlower 129 131 130 132 128 130 131 129 130 130
rc=0
judge >"$WORK/out" 2>&1 || rc=$?
if [ "$rc" -eq 1 ]; then pass "a consistent 30% slowdown fails"; else fail "a consistent 30% slowdown fails (rc=$rc)"; fi
if grep -q '^REGRESSION_ALERT:.*BenchmarkSlower' "$WORK/out"; then
  pass "the failure names the benchmark"
else
  fail "the failure names the benchmark (got: $(cat "$WORK/out"))"
fi

reset
printf 'BenchmarkUnder\t150\t5\n' >"$WORK/flagged.tsv"
window BenchmarkUnder 100 102 98 101 99
rerun BenchmarkUnder 109 111 110 112 108 110 111 109 110 110
rc=0
judge >"$WORK/out" 2>&1 || rc=$?
if [ "$rc" -eq 0 ]; then
  pass "a significant change still under the line passes"
else
  fail "a significant change still under the line passes (rc=$rc: $(cat "$WORK/out"))"
fi

reset
printf 'BenchmarkThin\t120\t2\n' >"$WORK/flagged.tsv"
window BenchmarkThin 100 101
rerun BenchmarkThin 199 201 200 202 198 200 201 199 200 200
rc=0
judge >"$WORK/out" 2>&1 || rc=$?
if [ "$rc" -eq 0 ] && grep -qF 'reported only' "$BENCHMARK_CONFIRM_SUMMARY"; then
  pass "fewer than three window nights reports without gating"
else
  fail "fewer than three window nights reports without gating (rc=$rc: $(cat "$BENCHMARK_CONFIRM_SUMMARY"))"
fi

reset
printf 'BenchmarkGone\t120\t5\n' >"$WORK/flagged.tsv"
window BenchmarkGone 100 102 98 101 99
rerun BenchmarkOther 100 100 100 100 100 100 100 100 100 100
rc=0
judge >/dev/null 2>&1 || rc=$?
if [ "$rc" -eq 2 ]; then pass "a flagged benchmark the re-run never measured exits 2"; else fail "a flagged benchmark the re-run never measured exits 2 (rc=$rc)"; fi

# A benchstat that cannot say its version stands in for one at another pin.
mkdir -p "$WORK/bin"
printf '#!/usr/bin/env bash\nexit 0\n' >"$WORK/bin/benchstat"
chmod +x "$WORK/bin/benchstat"
reset
printf 'BenchmarkSteady\t150\t5\n' >"$WORK/flagged.tsv"
window BenchmarkSteady 100 102 98 101 99
rerun BenchmarkSteady 99 101 100 102 98 100 101 99 100 300
rc=0
PATH="$WORK/bin:$PATH" judge >"$WORK/out" 2>&1 || rc=$?
if [ "$rc" -eq 2 ] && grep -qF 'install-benchstat.sh' "$WORK/out"; then
  pass "a benchstat at another version exits 2 with the install command"
else
  fail "a benchstat at another version exits 2 with the install command (rc=$rc: $(cat "$WORK/out"))"
fi

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
