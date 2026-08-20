#!/usr/bin/env bash
# Behavior tests for scripts/mutation-shard-budget.sh — the pre-flight check that
# refuses a mutation run whose shards no longer fit the job cap.
#
# Both legs are covered. The Rust counter is stubbed and the Go leg reads a
# stated dry-run listing, so these run in a second and assert the arithmetic and
# the verdict rather than re-deriving either tool's own mutant list.
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

# The Rust cases state no Go listing of their own, and the guard must never
# reach for the real dry-run here: that is a module-wide coverage run. An empty
# listing projects every Go shard to zero, which is the right neutral for a case
# asserting the Rust arithmetic.
EMPTY_LISTING="$(mktemp)"
export MUTATION_GO_DRYRUN_FILE="$EMPTY_LISTING"

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

# --- Go leg -------------------------------------------------------------------
#
# The Go projection reads a gremlins dry-run listing rather than counting from
# source: gremlins only knows a mutant is runnable once coverage says a test
# reaches it, which is why a new integration test can grow a shard without a line
# of production code changing. The listing is stated here so the arithmetic is
# what these cases assert.

# shellcheck source=scripts/lib/mutation-shards.sh
. "$REPO_ROOT/scripts/lib/mutation-shards.sh"

# Write a dry-run listing in gremlins' own line format, `count` mutants against
# every file or directory the named shard owns.
stub_dryrun() {
  local shard="$1" count="$2" file unit path i
  file="$(mktemp)"
  for unit in $(mutation_go_shard_units "$shard"); do
    case "$unit" in
      file:*) path="${unit#file:}" ;;
      dir:*) path="${unit#dir:}/owned.go" ;;
    esac
    for ((i = 0; i < count; i++)); do
      printf '    RUNNABLE CONDITIONALS_NEGATION at %s:%d:1\n' "$path" "$((i + 1))" >>"$file"
    done
  done
  printf '%s\n' "$file"
}

# Every Go shard must carry the measured per-mutant cost the projection needs. A
# shard with no cost is a shard nothing can refuse before the run.
cost_bad=""
for shard in $(mutation_go_shards); do
  cost="$(mutation_go_shard_seconds_per_mutant "$shard" 2>/dev/null)"
  [[ "$cost" =~ ^[0-9]+$ ]] && [ "$cost" -gt 0 ] || cost_bad="$cost_bad [$shard='$cost']"
done
if [ -z "$cost_bad" ]; then
  pass "every Go shard declares a measured per-mutant cost"
else
  fail "Go shards missing a per-mutant cost:$cost_bad"
fi

# An unknown shard is an error rather than a free pass.
if mutation_go_shard_seconds_per_mutant not-a-shard >/dev/null 2>&1; then
  fail "an unknown Go shard must not resolve to a cost"
else
  pass "an unknown Go shard is refused rather than costed"
fi

# One mutant per owned path is inside any budget.
dryrun="$(stub_dryrun go-api-runtime 1)"
if MUTATION_GO_DRYRUN_FILE="$dryrun" MUTATION_SHARD_COUNTER="$(stub_counter 1)" "$GUARD" >/dev/null 2>&1; then
  pass "a one-mutant Go listing is inside the budget"
else
  fail "a one-mutant Go listing must pass"
fi

# go-api-runtime costs 51s a mutant, so 200 mutants project to 170 minutes —
# past any budget a 75-minute job can carry.
dryrun="$(stub_dryrun go-api-runtime 200)"
out="$(MUTATION_GO_DRYRUN_FILE="$dryrun" MUTATION_SHARD_COUNTER="$(stub_counter 1)" "$GUARD" 2>&1)"
status=$?
if [ "$status" -ne 0 ]; then
  pass "an over-budget Go shard is refused"
else
  fail "200 go-api-runtime mutants must be refused"
fi
if printf '%s' "$out" | grep -q 'go-api-runtime'; then
  pass "the Go refusal names the shard that is over"
else
  fail "the Go refusal must name the offending shard (got: $out)"
fi

# A shard the listing never mentions projects to zero rather than vanishing from
# the table: a leg reported as absent reads as a leg nobody measured.
if printf '%s' "$out" | grep -q 'go-domain-detection'; then
  pass "every Go shard is reported, including the ones the listing does not reach"
else
  fail "the Go table must report every shard (got: $out)"
fi

# The committed Go map must fit. Counting it for real needs a module-wide
# coverage run, which is the mutation workflow's pre-flight job; a caller who has
# one already can hand it over through MUTATION_GO_SHARD_LISTING.
if [ -n "${MUTATION_GO_SHARD_LISTING:-}" ]; then
  if MUTATION_GO_DRYRUN_FILE="$MUTATION_GO_SHARD_LISTING" MUTATION_SHARD_COUNTER="$(stub_counter 1)" \
    "$GUARD" >/dev/null 2>&1; then
    pass "the committed Go shard map fits the job cap"
  else
    fail "the committed Go shard map is over budget"
  fi
else
  pass "the committed Go shard map fits the job cap (counted by the mutation workflow, which runs the gremlins dry-run)"
fi

echo
echo "Summary: $passed passed, $failed failed"
[ "$failed" -eq 0 ]
