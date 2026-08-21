#!/usr/bin/env bash
# Guards the Go test scope against a silent hole between the local gauntlet and CI.
#
# The gauntlet has always run the whole tree — `go test ./tests/...` — while CI
# ran one package of it. Thirty-two tests across four packages
# (tests/vmramseries, tests/loadtest, tests/vmcardinality, tests/vmbackfill)
# therefore existed, passed locally, and were measured by no pipeline; a new
# package under server/tests/ inherited that hole the moment it was created.
#
# Fix by construction: the two commands must name the same package patterns.
# This test reads the pattern out of each file and compares them, in both the
# unit scope (./internal/...) and the tree scope (./tests/...), so neither side
# can be narrowed without the other.
#
# It also proves both shared services are provisioned for the tree scope. With
# POSTGRES_TEST_URL or VICTORIAMETRICS_TEST_URL unset, every package in the
# scope starts its own container — several metrics stores at once is the memory
# pressure that has the runner kill one mid-run.
#
# Run: ./scripts/tests/go-test-scope-parity.test.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
CI="$ROOT/.github/workflows/ci.yml"
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

for f in "$CI" "$GAUNTLET"; do
  if [ ! -f "$f" ]; then
    fail "missing file: $f"
    printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
    exit 1
  fi
done

# patterns_in FILE — every `./…/...` package pattern a `go test` line names.
# Comment lines are stripped first: both files explain the scope in prose that
# quotes the very command being matched, and counting those would make the
# comparison pass on a scope no test run actually uses.
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
  if printf '%s\n' "$ci_patterns" | grep -qxF "$want"; then
    pass "CI runs $want"
  else
    fail "CI does not run $want"
  fi
  if printf '%s\n' "$gauntlet_patterns" | grep -qxF "$want"; then
    pass "the gauntlet runs $want"
  else
    fail "the gauntlet does not run $want"
  fi
done

# The tree scope is only honest if nothing under server/tests/ is left out of
# it. Every directory holding a _test.go file must be reachable from ./tests/...
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
  if printf '%s\n' "$integration_job" | grep -q "$var:"; then
    pass "the integration job exports $var"
  else
    fail "the integration job does not export $var — each package would start its own container"
  fi
done

if printf '%s\n' "$integration_job" | grep -q 'victoria-metrics:v'; then
  pass "the integration job starts a pinned VictoriaMetrics"
else
  fail "the integration job never starts VictoriaMetrics"
fi

if printf '%s\n' "$integration_job" | grep -q 'postgres:17-alpine'; then
  pass "the integration job starts a pinned Postgres"
else
  fail "the integration job never starts Postgres"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf 'Failures:\n' >&2
  for f in "${FAILURES[@]}"; do printf '  - %s\n' "$f" >&2; done
  exit 1
fi
