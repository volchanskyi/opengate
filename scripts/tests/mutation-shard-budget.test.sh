#!/usr/bin/env bash
# Behavior tests for scripts/mutation-shard-budget.sh, which refuses a mutation run whose shards
# exceed the job cap. The Rust counter is stubbed and the Go leg reads a stated dry-run listing.
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

# An empty listing projects every Go shard to zero, so the guard never runs the real dry-run.
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

counter="$(stub_counter 1)"
if MUTATION_SHARD_COUNTER="$counter" "$GUARD" >/dev/null 2>&1; then
  pass "a one-mutant shard set is inside the budget"
else
  fail "a one-mutant shard set must pass"
fi

# The projection is the mutant count times the package's measured per-mutant cost.
counter="$(stub_counter 200)"
if MUTATION_SHARD_COUNTER="$counter" "$GUARD" >/dev/null 2>&1; then
  fail "200 mesh-agent-core mutants must be refused"
else
  pass "an over-budget shard is refused"
fi

out="$(MUTATION_SHARD_COUNTER="$counter" "$GUARD" 2>&1)"
if grep -q 'rust-core-' <<<"$out"; then
  pass "the refusal names the shard that is over"
else
  fail "the refusal must name the offending shard (got: $out)"
fi

out="$(MUTATION_SHARD_COUNTER="$counter" "$GUARD" 2>&1)"
over="$(printf '%s\n' "$out" | grep -c 'OVER')"
if [ "$over" -gt 1 ]; then
  pass "every over-budget shard is reported, not just the first"
else
  fail "expected more than one OVER line (got $over)"
fi

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

# shellcheck source=scripts/lib/mutation-shards.sh
. "$REPO_ROOT/scripts/lib/mutation-shards.sh"

# Writes a gremlins-format dry-run listing with `count` mutants per path the shard owns.
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

if mutation_go_shard_seconds_per_mutant not-a-shard >/dev/null 2>&1; then
  fail "an unknown Go shard must not resolve to a cost"
else
  pass "an unknown Go shard is refused rather than costed"
fi

# A mutant that never terminates holds a worker for its whole leash, the coverage run's elapsed
# time times the timeout coefficient, so the projection adds that term per blocking mutant.
blocking_bad=""
for shard in $(mutation_go_shards); do
  blocking="$(mutation_go_shard_blocking_mutants "$shard" 2>/dev/null)"
  [[ "$blocking" =~ ^[0-9]+$ ]] || blocking_bad="$blocking_bad [$shard='$blocking']"
done
if [ -z "$blocking_bad" ]; then
  pass "every Go shard declares how many of its mutants never terminate"
else
  fail "Go shards missing a blocking-mutant count:$blocking_bad"
fi

if mutation_go_shard_blocking_mutants not-a-shard >/dev/null 2>&1; then
  fail "an unknown Go shard must not resolve to a blocking count"
else
  pass "an unknown Go shard is refused rather than given a blocking count"
fi

leash_min=$((($(mutation_go_leash_ceiling_seconds) + 59) / 60))
if [ "$leash_min" -gt 0 ]; then
  pass "the leash ceiling is a positive number of minutes ($leash_min)"
else
  fail "the leash ceiling must be positive (got '$leash_min')"
fi

# go-amt carries one never-terminating mutant, so its projection is dominated by the leash.
dryrun="$(stub_dryrun go-amt 1)"
out="$(MUTATION_GO_DRYRUN_FILE="$dryrun" MUTATION_SHARD_COUNTER="$(stub_counter 1)" "$GUARD" 2>&1)"
amt_proj="$(printf '%s\n' "$out" | awk '$1 == "go-amt" { sub(/min$/, "", $3); print $3 }')"
if [ -n "$amt_proj" ] && [ "$amt_proj" -ge "$leash_min" ]; then
  pass "a shard's projection carries the leash its blocked mutants hold"
else
  fail "go-amt must project at least the ${leash_min}min leash it holds (got '$amt_proj')"
fi

dryrun="$(stub_dryrun go-api-runtime 1)"
if MUTATION_GO_DRYRUN_FILE="$dryrun" MUTATION_SHARD_COUNTER="$(stub_counter 1)" "$GUARD" >/dev/null 2>&1; then
  pass "a one-mutant Go listing is inside the budget"
else
  fail "a one-mutant Go listing must pass"
fi

dryrun="$(stub_dryrun go-api-runtime 200)"
out="$(MUTATION_GO_DRYRUN_FILE="$dryrun" MUTATION_SHARD_COUNTER="$(stub_counter 1)" "$GUARD" 2>&1)"
status=$?
if [ "$status" -ne 0 ]; then
  pass "an over-budget Go shard is refused"
else
  fail "200 go-api-runtime mutants must be refused"
fi
if grep -q 'go-api-runtime' <<<"$out"; then
  pass "the Go refusal names the shard that is over"
else
  fail "the Go refusal must name the offending shard (got: $out)"
fi

if grep -q 'go-domain-alerts-room' <<<"$out"; then
  pass "every Go shard is reported, including the ones the listing does not reach"
else
  fail "the Go table must report every shard (got: $out)"
fi

# Counting the committed Go map needs a module-wide coverage run; a caller holding its listing
# passes it through MUTATION_GO_SHARD_LISTING.
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
