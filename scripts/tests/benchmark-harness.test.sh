#!/usr/bin/env bash
# Guards Go benchmarks against a harness that dominates the figure it publishes.
#
# The trend gate reads ns/op as a statement about the code under test. That only
# holds while the measured region is the code — a benchmark whose loop body is
# mostly plumbing publishes the plumbing's variance, and the gate then reports a
# regression nobody wrote.
#
# One shape does this reliably enough to refuse outright: b.StopTimer() and
# b.StartTimer() called per iteration. Both call runtime.ReadMemStats, which
# stops the world, so the pair costs two stop-the-world pauses per iteration and
# restarts the scheduler cold for the region it is protecting. Measured on the
# handshake benchmark, same commit, same machine, six samples each: with the
# toggle the run-to-run spread was 4.0x (33500-135410 ns/op); without it, 1.09x
# (15489-16840). The toggle exists to keep setup allocations out of the figure,
# and it does that -- while making the time it reports unusable.
#
# Setup belongs outside the loop, before b.ResetTimer(). Where per-iteration
# state is unavoidable, build it from a source that cannot block or schedule.
#
# The gate also holds the committed baseline and the benchmark set in agreement,
# in both directions: a renamed benchmark otherwise leaves a baseline row gating
# nothing, and a new one is measured by nothing until somebody notices.
#
# Run: ./scripts/tests/benchmark-harness.test.sh

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

# --- no per-iteration clock toggling -----------------------------------------
#
# awk walks each benchmark body and tracks brace depth. Depth 1 is the function
# body, where a toggle is legitimate: it brackets one-time setup. Anything
# deeper sits inside a loop or a closure that runs per iteration, which is the
# shape being refused. Comments are stripped so the prose above a benchmark --
# and this file's own explanation of the rule -- cannot trip it.
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

# --- baseline and benchmark set agree ----------------------------------------
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
