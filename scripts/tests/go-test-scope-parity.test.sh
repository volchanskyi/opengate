#!/usr/bin/env bash
# Holds the gauntlet and CI to the same Go package patterns, in the unit and the tree scope.
# Both shared services are provisioned for the tree scope, so no package starts its own container.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
CI="$ROOT/.github/workflows/ci.yml"
MUTATION="$ROOT/.github/workflows/mutation.yml"
GAUNTLET="$ROOT/scripts/precommit-gauntlet.sh"
SERVER="$ROOT/server"

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

echo "go-test-scope-parity:"

for f in "$CI" "$MUTATION" "$GAUNTLET"; do
  if [ ! -f "$f" ]; then
    fail "missing file: $f"
    printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
    exit 1
  fi
done

# patterns_in FILE — every `./…/...` package pattern a `go test` line names.
# Comment lines are stripped first, as prose may quote the command being matched.
patterns_in() {
  grep -vE '^[[:space:]]*#' "$1" \
    | grep -oE 'go test [^|>]*' \
    | grep -oE '\./[a-z/]+/\.\.\.' \
    | sort -u
}

ci_patterns="$(patterns_in "$CI")"
gauntlet_patterns="$(patterns_in "$GAUNTLET")"

if [ "$ci_patterns" = "$gauntlet_patterns" ]; then
  pass "CI and the gauntlet run the same Go package patterns"
else
  fail "Go test scope drift — CI runs [$(echo "$ci_patterns" | tr '\n' ' ')], gauntlet runs [$(echo "$gauntlet_patterns" | tr '\n' ' ')]"
fi

for want in './internal/...' './tests/...'; do
  if grep -qxF "$want" <<<"$ci_patterns"; then
    pass "CI runs $want"
  else
    fail "CI does not run $want"
  fi
  if grep -qxF "$want" <<<"$gauntlet_patterns"; then
    pass "the gauntlet runs $want"
  else
    fail "the gauntlet does not run $want"
  fi
done

# Every directory under server/tests/ holding a _test.go file is reachable from ./tests/...
missing_dirs=""
while IFS= read -r dir; do
  rel="${dir#"$SERVER/"}"
  case "$rel" in
    tests/*) ;;
    *) missing_dirs="$missing_dirs $rel" ;;
  esac
done < <(find "$SERVER/tests" -name '*_test.go' -printf '%h\n' | sort -u)

if [ -z "$missing_dirs" ]; then
  pass "every test package under server/tests/ is inside ./tests/..."
else
  fail "test packages outside the scope:$missing_dirs"
fi

# Both shared services must be named in the integration job's env block.
integration_job="$(awk '/^  go-integration:/{flag=1} /^  golden:/{flag=0} flag' "$CI")"

for var in POSTGRES_TEST_URL VICTORIAMETRICS_TEST_URL; do
  if grep -q "$var:" <<<"$integration_job"; then
    pass "the integration job exports $var"
  else
    fail "the integration job does not export $var — each package would start its own container"
  fi
done

if grep -q 'victoria-metrics:v' <<<"$integration_job"; then
  pass "the integration job starts a pinned VictoriaMetrics"
else
  fail "the integration job never starts VictoriaMetrics"
fi

if grep -q 'postgres:17-alpine' <<<"$integration_job"; then
  pass "the integration job starts a pinned Postgres"
else
  fail "the integration job never starts Postgres"
fi

# The mutation shards and the shard-budget pre-flight run the whole tree as a coverage baseline,
# so each needs both shared services.
mutation_budget="$(awk '/^  shard-budget:/{flag=1} /^  mutation:/{flag=0} flag' "$MUTATION")"
mutation_matrix="$(awk '/^  mutation:/{flag=1} /^  publish:/{flag=0} flag' "$MUTATION")"

check_provisioned() {
  local label="$1" job="$2" var
  for var in POSTGRES_TEST_URL VICTORIAMETRICS_TEST_URL; do
    if grep -q "$var:" <<<"$job"; then
      pass "$label exports $var"
    else
      fail "$label does not export $var — each package would start its own container"
    fi
  done
  if grep -q 'victoria-metrics:v' <<<"$job"; then
    pass "$label starts a pinned VictoriaMetrics"
  else
    fail "$label never starts VictoriaMetrics"
  fi
  if grep -q 'postgres:17-alpine' <<<"$job"; then
    pass "$label starts a pinned Postgres"
  else
    fail "$label never starts Postgres"
  fi
}

check_provisioned "the mutation shard-budget job" "$mutation_budget"
check_provisioned "the mutation matrix" "$mutation_matrix"

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf 'Failures:\n' >&2
  for f in "${FAILURES[@]}"; do printf '  - %s\n' "$f" >&2; done
  exit 1
fi
