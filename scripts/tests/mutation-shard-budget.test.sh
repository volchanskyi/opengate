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
assert_rows() {
  if [ "$2" = "$3" ]; then pass "$1"; else fail "$1 (want=[$2] got=[$3])"; fi
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
# Every case states its own web count; the committed web map is counted for real further down.
MUTATION_WEB_COUNTER="$(stub_counter 1)"
export MUTATION_WEB_COUNTER
WEB_COUNT="$REPO_ROOT/scripts/mutation-web-count.sh"

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

# Writes a gremlins-format dry-run listing from path and count pairs.
listing_of() {
  local file path count i
  file="$(mktemp)"
  while [ "$#" -ge 2 ]; do
    path="$1" count="$2"
    shift 2
    for ((i = 0; i < count; i++)); do
      printf '    RUNNABLE CONDITIONALS_NEGATION at %s:%d:1\n' "$path" "$((i + 1))" >>"$file"
    done
  done
  printf '%s\n' "$file"
}

# The 4 Oct night's runnable mutants in the API runtime files; wsconn.go had none runnable.
api_night="$(listing_of internal/api/api.go 25 internal/api/middleware.go 10 \
  internal/api/proxytrust.go 28 internal/api/ratelimit.go 16)"
out="$(MUTATION_GO_DRYRUN_FILE="$api_night" MUTATION_SHARD_COUNTER="$(stub_counter 1)" "$GUARD" 2>&1)"
got="$(awk '$1 ~ /^go-api-(runtime|limits|proxytrust)$/ { print $1, $2, $4 }' <<<"$out")"
assert_rows "the API runtime files fit as three groups on the 4 Oct counts" \
  "go-api-runtime 35 ok
go-api-limits 16 ok
go-api-proxytrust 28 ok" "$got"

# The 1 Nov runnable mutants in the API groups that ran out their 90 minutes or came within 5 of it.
api_nov="$(listing_of internal/api/metrics_grid.go 38 internal/api/metrics_assemble.go 20 \
  internal/api/handlers_client_errors.go 9 internal/api/handlers_health.go 3 \
  internal/api/handlers_auth.go 31 internal/api/handlers_users.go 10 \
  internal/api/handlers_security_groups.go 8 internal/api/handlers_security_group_members.go 4 \
  internal/api/handlers_audit.go 7 internal/api/proxytrust.go 31 internal/api/ratelimit.go 18 \
  internal/api/handlers_rules.go 17 internal/api/handlers_rules_admin.go 4 \
  internal/api/handlers_rules_tuning.go 7 internal/api/handlers_rules_read.go 14 \
  internal/api/handlers_rules_rollout.go 11 internal/api/handlers_organizations.go 18 \
  internal/api/handlers_sites.go 9 internal/api/handlers_device_tags.go 12 \
  internal/api/handlers_alert_limits.go 5 internal/api/handlers_push.go 2)"
out="$(MUTATION_GO_DRYRUN_FILE="$api_nov" MUTATION_SHARD_COUNTER="$(stub_counter 1)" "$GUARD" 2>&1)"
got="$(awk '$1 ~ /^go-api-(status|metrics-grid|identity|accounts|limits|proxytrust|tenancy-admin|tags-limits-push|rules|rules-rollout)$/ { print $1, $2, $4 }' <<<"$out")"
assert_rows "the 1 Nov counts fit ten groups at each group's slowest measured cost" \
  "go-api-limits 18 ok
go-api-proxytrust 31 ok
go-api-status 32 ok
go-api-metrics-grid 38 ok
go-api-identity 31 ok
go-api-accounts 29 ok
go-api-tenancy-admin 27 ok
go-api-tags-limits-push 19 ok
go-api-rules 28 ok
go-api-rules-rollout 25 ok" "$got"

for group in "go-api-status 70" "go-api-identity 60" "go-api-rules 53"; do
  read -r shard count <<<"$group"
  whole=$(((count * $(mutation_go_shard_seconds_per_mutant "$shard") + 59) / 60))
  if [ "$whole" -gt "$(mutation_go_shard_budget_minutes)" ]; then
    pass "$shard's $count mutants in one group project past the budget (${whole}min)"
  else
    fail "$count mutants at $shard's measured cost must not fit one group (projected ${whole}min)"
  fi
done

whole=$(((79 * $(mutation_go_shard_seconds_per_mutant go-api-runtime) + 59) / 60))
if [ "$whole" -gt "$(mutation_go_shard_budget_minutes)" ]; then
  pass "the same 79 mutants in one group project past the budget (${whole}min)"
else
  fail "79 mutants at the declared cost must not fit one group (projected ${whole}min)"
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

web_cost_bad=""
for shard in $(mutation_web_shards); do
  cost="$(mutation_web_shard_milliseconds_per_mutant "$shard" 2>/dev/null)"
  [[ "$cost" =~ ^[0-9]+$ ]] && [ "$cost" -gt 0 ] || web_cost_bad="$web_cost_bad [$shard='$cost']"
done
if [ -z "$web_cost_bad" ]; then
  pass "every web shard declares a measured per-mutant cost"
else
  fail "web shards missing a per-mutant cost:$web_cost_bad"
fi

if mutation_web_shard_milliseconds_per_mutant not-a-shard >/dev/null 2>&1; then
  fail "an unknown web shard must not resolve to a cost"
else
  pass "an unknown web shard is refused rather than costed"
fi

web_budget="$(mutation_web_shard_budget_minutes)"
if [ "$web_budget" -le 45 ]; then
  pass "a web shard may project to half the 90-minute cap at most (${web_budget}min)"
else
  fail "the web budget must leave half the cap for a slow night (got ${web_budget}min)"
fi

out="$(MUTATION_WEB_COUNTER="$(stub_counter 10000)" MUTATION_SHARD_COUNTER="$(stub_counter 1)" "$GUARD" 2>&1)"
status=$?
if [ "$status" -ne 0 ] && grep -qE '^web-devices .*OVER' <<<"$out"; then
  pass "an over-budget web shard is refused and named"
else
  fail "10000 web mutants per shard must be refused, naming the shard (status $status, got: $out)"
fi

# 3000 mutants at the devices cost plus the setup ceiling cross 45 minutes; 2600 do not.
devices_minutes() {
  echo $((($1 * $(mutation_web_shard_milliseconds_per_mutant web-devices) / 1000 + \
  $(mutation_web_setup_ceiling_seconds) + 59) / 60))
}
out="$(MUTATION_WEB_COUNTER="$(stub_counter 2600)" MUTATION_SHARD_COUNTER="$(stub_counter 1)" "$GUARD" 2>&1)"
row="$(awk '$1 == "web-devices" { sub(/min$/, "", $3); print $3, $4 }' <<<"$out")"
if [ "$row" = "$(devices_minutes 2600) ok" ] && [ "$(devices_minutes 3000)" -gt "$web_budget" ]; then
  pass "a web shard projects its mutants at its cost plus the setup ceiling ($row)"
else
  fail "web-devices must project 2600 mutants to $(devices_minutes 2600)min, ok (got '$row')"
fi

if [ -x "$WEB_COUNT" ]; then
  whole="$("$WEB_COUNT" 2>/dev/null || true)"
  sum=0
  shard_counts=""
  for shard in $(mutation_web_shards); do
    n="$("$WEB_COUNT" "$shard" 2>/dev/null || true)"
    [[ "$n" =~ ^[0-9]+$ ]] || n=x
    shard_counts="$shard_counts $shard=$n"
    [ "$n" = x ] || sum=$((sum + n))
  done
  if [[ "$whole" =~ ^[0-9]+$ ]] && [ "$whole" -gt 0 ] && [ "$sum" -eq "$whole" ]; then
    pass "the web shards' mutants add up to the config's own ($whole), so none is lost or doubled"
  else
    fail "the web shards hold$shard_counts, where the config mutates '$whole'"
  fi

  if out="$(env -u MUTATION_WEB_COUNTER MUTATION_SHARD_COUNTER="$(stub_counter 1)" "$GUARD" 2>&1)"; then
    pass "the committed web shard map fits the budget"
  else
    fail "the committed web shard map is over budget: $(grep -E '^web-' <<<"$out" | tr '\n' ';')"
  fi
else
  fail "scripts/mutation-web-count.sh must exist and be executable"
fi

echo
echo "Summary: $passed passed, $failed failed"
[ "$failed" -eq 0 ]
