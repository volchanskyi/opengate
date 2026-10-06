#!/usr/bin/env bash
# Refuses per-iteration b.StopTimer()/b.StartTimer(); both call the stop-the-world ReadMemStats.
# Holds the committed baseline and the benchmark set in agreement in both directions.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
SERVER="$ROOT/server"
BASELINE="$ROOT/benchmarks/baseline.json"

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

echo "benchmark-harness:"

if [ ! -f "$BASELINE" ]; then
  fail "missing file: $BASELINE"
  printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
  exit 1
fi

# awk tracks brace depth: depth 1 is the function body, where a toggle brackets one-time setup.
# $0 and the field references below belong to awk, not to the shell.
# shellcheck disable=SC2016
toggles="$(
  find "$SERVER" -name '*_test.go' -print0 \
    | xargs -0 awk '
      FNR == 1 { depth = 0; inbench = 0 }
      { line = $0; sub(/\/\/.*/, "", line) }
      line ~ /^func Benchmark[A-Za-z0-9_]*\(/ { inbench = 1; depth = 0 }
      inbench {
        opens = gsub(/{/, "{", line)
        closes = gsub(/}/, "}", line)
        if (depth > 1 && line ~ /b\.(Stop|Start)Timer\(\)/) {
          printf "%s:%d\n", FILENAME, FNR
        }
        depth += opens - closes
        if (depth <= 0) { inbench = 0 }
      }
    '
)"

if [ -z "$toggles" ]; then
  pass "no benchmark toggles its own clock inside the measured loop"
else
  while IFS= read -r hit; do
    fail "per-iteration b.StopTimer()/b.StartTimer() at ${hit#"$ROOT/"}"
  done <<<"$toggles"
fi

declared="$(
  grep -oE '"name": "Benchmark[A-Za-z0-9_]*"' "$BASELINE" \
    | sed -E 's/.*"(Benchmark[A-Za-z0-9_]*)".*/\1/' | sort -u
)"
defined="$(
  find "$SERVER" -name '*_test.go' -exec grep -hoE '^func (Benchmark[A-Za-z0-9_]*)\(' {} + \
    | sed -E 's/^func (Benchmark[A-Za-z0-9_]*)\(/\1/' | sort -u
)"

missing_baseline="$(comm -13 <(printf '%s\n' "$declared") <(printf '%s\n' "$defined"))"
stale_baseline="$(comm -23 <(printf '%s\n' "$declared") <(printf '%s\n' "$defined"))"

if [ -z "$missing_baseline" ]; then
  pass "every Go benchmark has a committed baseline row"
else
  fail "benchmarks with no baseline row: $(echo "$missing_baseline" | tr '\n' ' ')"
fi

if [ -z "$stale_baseline" ]; then
  pass "every baseline row names a benchmark that exists"
else
  fail "baseline rows gating nothing: $(echo "$stale_baseline" | tr '\n' ' ')"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf 'Failures:\n' >&2
  for f in "${FAILURES[@]}"; do printf '  - %s\n' "$f" >&2; done
  exit 1
fi
