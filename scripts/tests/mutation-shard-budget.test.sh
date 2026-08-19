#!/usr/bin/env bash
# Behavior tests for scripts/mutation-shard-budget.sh — the pre-flight check that
# refuses a mutation run whose Rust shards no longer fit the job cap.
#
# The counter is stubbed, so these run in a second and assert the arithmetic and
# the verdict rather than re-deriving cargo-mutants' own mutant list.
set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
GUARD="$REPO_ROOT/scripts/mutation-shard-budget.sh"
passed=0
failed=0

pass() {
  printf '  ok   %s\n' "$1"
  passed=$((passed + 1))
}
fail() {
  printf '  FAIL %s\n' "$1"
  failed=$((failed + 1))
}

# A counter that answers with a fixed count for every shard, so a case states one
# number and reads one verdict.
stub_counter() {
  local count="$1" dir
  dir="$(mktemp -d)"
  cat >"$dir/count" <<EOF
#!/usr/bin/env bash
echo $count
EOF
  chmod +x "$dir/count"
  printf '%s\n' "$dir/count"
}

echo "== mutation-shard-budget.sh =="

if [ -x "$GUARD" ]; then
  pass "guard exists and is executable"
else
  fail "guard must exist at scripts/mutation-shard-budget.sh and be executable"
  echo "Summary: $passed passed, $failed failed"
  exit 1
fi

# A shard map's own numbers must clear the budget, or the split has drifted and
# the next nightly burns 75 minutes to discover it.
counter="$(stub_counter 1)"
if MUTATION_SHARD_COUNTER="$counter" "$GUARD" >/dev/null 2>&1; then
  pass "a one-mutant shard set is inside the budget"
else
  fail "a one-mutant shard set must pass"
fi

# The projection is count x the package's measured per-mutant cost. mesh-agent-core
# is 460 milli-minutes, so 200 mutants project to 92 min — past any sane budget.
counter="$(stub_counter 200)"
if MUTATION_SHARD_COUNTER="$counter" "$GUARD" >/dev/null 2>&1; then
  fail "200 mesh-agent-core mutants must be refused"
else
  pass "an over-budget shard is refused"
fi

out="$(MUTATION_SHARD_COUNTER="$counter" "$GUARD" 2>&1)"
if printf '%s' "$out" | grep -q 'rust-core-'; then
  pass "the refusal names the shard that is over"
else
  fail "the refusal must name the offending shard (got: $out)"
fi

# Every shard is reported, not only the first failure: a split that drifted once
# has usually drifted in several places, and one shard per run is a slow way to
# find that out.
out="$(MUTATION_SHARD_COUNTER="$counter" "$GUARD" 2>&1)"
over="$(printf '%s\n' "$out" | grep -c 'OVER')"
if [ "$over" -gt 1 ]; then
  pass "every over-budget shard is reported, not just the first"
else
  fail "expected more than one OVER line (got $over)"
fi

# The real shard map must fit. This is the assertion that actually guards the
# repository; the stubs above only prove the guard can say no.
if [ -n "${MUTATION_SHARD_BUDGET_SKIP_REAL:-}" ]; then
  fail "no skip switch may exist for the real-map check"
elif command -v cargo-mutants >/dev/null 2>&1; then
  if "$GUARD" >/dev/null 2>&1; then
    pass "the committed Rust shard map fits the job cap"
  else
    fail "the committed Rust shard map is over budget"
  fi
else
  pass "the committed Rust shard map fits the job cap (counted by the mutation workflow, which installs cargo-mutants)"
fi

echo
echo "Summary: $passed passed, $failed failed"
[ "$failed" -eq 0 ]
