#!/usr/bin/env bash
# Tests scripts/benchmark-local-check.sh: this machine's Go allocations held to the CI baseline.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
CHECK="$REPO_ROOT/scripts/benchmark-local-check.sh"
NIGHTLY="$REPO_ROOT/.github/workflows/benchmark.yml"
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
  if [ "$2" = "$3" ]; then pass "$1"; else fail "$1 (want=[$2] got=[$3])"; fi
}
contains() {
  if grep -qF -- "$2" <<<"$3"; then pass "$1"; else fail "$1 (missing [$2] in [$3])"; fi
}
lacks() {
  if grep -qF -- "$2" <<<"$3"; then fail "$1 (found [$2] in [$3])"; else pass "$1"; fi
}

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

cat >"$WORK/baseline.json" <<'JSON'
{
  "version": 1,
  "default_tolerances": { "allocs_op": 0.02, "bytes_op": 0.02, "ns_op": 1.5 },
  "benchmarks": [
    { "name": "BenchmarkSign", "lang": "go", "ns_op": 150000, "bytes_op": 16836, "allocs_op": 292 },
    { "name": "BenchmarkEncode", "lang": "go", "ns_op": 800, "bytes_op": 264, "allocs_op": 4 },
    { "name": "frame_encode_control", "lang": "rust", "ns_op": 293.8, "bytes_op": null, "allocs_op": null }
  ]
}
JSON

# Prints a Go benchmark output carrying the given SignAgent bytes and Encode allocations.
bench_output() {
  cat <<GO
goos: linux
goarch: amd64
pkg: github.com/volchanskyi/opengate/server/internal/cert
BenchmarkSign-4     	    9500	    126343 ns/op	   $1 B/op	     290 allocs/op
BenchmarkEncode-4   	 2400000	       451.9 ns/op	     264 B/op	       $2 allocs/op
BenchmarkNewOnDev-4 	  733749	      1373 ns/op	    1304 B/op	      12 allocs/op
PASS
ok  	github.com/volchanskyi/opengate/server/internal/cert	3.1s
GO
}

check() {
  rc=0
  out="$(BASELINE_FILE="$WORK/baseline.json" "$CHECK" go "$1" 2>&1)" || rc=$?
}

echo "▶ readings inside the CI baseline's tolerance"
bench_output 16772 4 >"$WORK/inside.txt"
check "$WORK/inside.txt"
assert_eq "a run inside tolerance passes" 0 "$rc"
contains "it says how many benchmarks it compared" "compared 2 Go benchmarks with the CI baseline" "$out"
contains "a benchmark CI has not measured is named and skipped" "not in the CI baseline yet: BenchmarkNewOnDev" "$out"
lacks "time per call is never judged here" "ns_op" "$out"

echo "▶ bytes past the tolerance"
bench_output 18309 4 >"$WORK/bytes.txt"
check "$WORK/bytes.txt"
assert_eq "bytes past tolerance fail the run" 1 "$rc"
contains "the regression names the benchmark, the metric and both readings" \
  "BenchmarkSign bytes_op: 16836 → 18309 (>2% tolerance)" "$out"

echo "▶ allocations past the tolerance"
bench_output 16772 5 >"$WORK/allocs.txt"
check "$WORK/allocs.txt"
assert_eq "allocations past tolerance fail the run" 1 "$rc"
contains "the allocation regression is named" "BenchmarkEncode allocs_op: 4 → 5 (>2% tolerance)" "$out"

echo "▶ an output with no benchmark in it"
printf 'PASS\nok  \tgithub.com/volchanskyi/opengate/server/internal/cert\t0.1s\n' >"$WORK/empty.txt"
check "$WORK/empty.txt"
assert_eq "an output that reached no benchmark is refused" 2 "$rc"
contains "the refusal says why" "read no Go benchmark" "$out"

echo "▶ usage"
rc=0
"$CHECK" >/dev/null 2>&1 || rc=$?
assert_eq "no mode is a usage error" 2 "$rc"
rc=0
"$CHECK" go "$WORK/absent.txt" >/dev/null 2>&1 || rc=$?
assert_eq "a missing output file is a usage error" 2 "$rc"

echo "▶ the run uses the nightly's processor count"
local_cpus="$(sed -n 's/^BENCH_CPUS=\([0-9][0-9]*\)$/\1/p' "$CHECK")"
nightly_cpus="$(sed -n 's/.*go test .*-cpu \([0-9][0-9]*\).*/\1/p' "$NIGHTLY")"
if [ -n "$local_cpus" ]; then pass "the local check names its processor count"; else fail "the local check names no processor count"; fi
assert_eq "the local check and the nightly run at the same processor count" "$nightly_cpus" "$local_cpus"

echo "▶ the local check runs the Rust suites the nightly publishes"
local_crates="$(sed -n 's/^RUST_SUITES=(\(.*\))$/\1/p' "$CHECK" | tr ' ' '\n' | sort)"
nightly_crates="$(grep -oE "cargo bench( -p [a-z-]+)+ --bench '\\*'" "$NIGHTLY" | grep -oE '\-p [a-z-]+' | awk '{print $2}' | sort)"
if [ -n "$local_crates" ]; then pass "the local check names its Rust crates"; else fail "the local check names no Rust crate"; fi
assert_eq "the local check and the nightly bench the same Rust crates" "$nightly_crates" "$local_crates"

echo
echo "benchmark-local-check: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  for f in "${FAILURES[@]}"; do printf '  - %s\n' "$f" >&2; done
  exit 1
fi
