#!/usr/bin/env bash
# The runner-hosted performance stack must be able to host the load it is for.
#
# The disposable stack this replaces could not: it published only the
# browser-facing port, so no simulated machine could reach it; it declared no
# resource bounds, so "the server ran out of processor" and "the database did"
# were the same observation; and it ran no metrics store, so a slow run could
# report latency and nothing about why.
#
# Run: ./scripts/tests/perf-stack.test.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
COMPOSE="$REPO_ROOT/deploy/docker-compose.perf.yml"
WORKFLOW="$REPO_ROOT/.github/workflows/perf-stack.yml"

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

echo "perf stack:"

for f in "$COMPOSE" "$WORKFLOW"; do
  if [ ! -f "$f" ]; then
    fail "missing file: $f"
    printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
    exit 1
  fi
done

# A machine reaches the server over QUIC, which is UDP. A stack that publishes
# only the browser port can host operator load and no fleet at all.
if grep -qE '^\s*-\s*"9090:9090/udp"' "$COMPOSE"; then
  pass "the machine-facing QUIC port is published"
else
  fail "the machine-facing QUIC port is published"
fi

# Every service is bounded, separately. Without that, the sweep has nothing to
# vary and a saturated stack cannot say which part saturated.
for service in postgres server metrics; do
  if awk -v svc="$service" '
      $0 ~ "^  " svc ":" { in_svc = 1; next }
      /^  [a-z-]+:$/ { in_svc = 0 }
      in_svc && /cpus:/ { found = 1 }
      END { exit !found }
    ' "$COMPOSE"; then
    pass "$service declares a processor bound"
  else
    fail "$service declares a processor bound"
  fi
  if awk -v svc="$service" '
      $0 ~ "^  " svc ":" { in_svc = 1; next }
      /^  [a-z-]+:$/ { in_svc = 0 }
      in_svc && /memory:/ { found = 1 }
      END { exit !found }
    ' "$COMPOSE"; then
    pass "$service declares a memory bound"
  else
    fail "$service declares a memory bound"
  fi
done

# The server's processor count is the sweep's variable, so it cannot be a
# literal: several stacks that also differ in unrecorded ways are not a sweep.
if grep -q 'PERF_SERVER_CPUS' "$COMPOSE"; then
  pass "the server's processor count is the sweep's variable"
else
  fail "the server's processor count is the sweep's variable"
fi

# The volume family measures how much disk a fixture occupies. A database in
# memory has no size, so this stack must not put its data on tmpfs.
postgres_block() {
  awk '
    /^  postgres:/ { in_svc = 1; next }
    /^  [a-z-]+:$/ { in_svc = 0 }
    in_svc { print }
  ' "$COMPOSE"
}
if postgres_block | grep -qE '^[[:space:]]+tmpfs:'; then
  fail "the database writes to tmpfs, so a fixture would have no measurable size"
elif postgres_block | grep -q 'perf-postgres:/var/lib/postgresql/data'; then
  pass "the database writes to a volume, so a fixture has a measurable size"
else
  fail "the database names no data volume"
fi

# A stack that cannot observe itself reports latency and nothing about why.
if grep -q 'victoria-metrics' "$COMPOSE"; then
  pass "the stack runs a metrics store"
else
  fail "the stack runs a metrics store"
fi

# A variable no Go source reads is a setting somebody will one day try to
# change, expecting something to happen.
if grep -q 'OPENGATE_TEST_MODE' "$COMPOSE"; then
  fail "the perf stack sets OPENGATE_TEST_MODE, which no Go source reads"
else
  pass "the perf stack sets no variable the server does not read"
fi

# Compose must be able to parse it — a stack nobody can bring up is not a stack.
if command -v docker >/dev/null 2>&1; then
  if DOCKER_CONFIG="$("$REPO_ROOT/scripts/docker-credstore-guard.sh")" \
    docker compose -f "$COMPOSE" config >/dev/null 2>&1; then
    pass "compose parses the perf stack"
  else
    fail "compose parses the perf stack"
  fi
else
  # A machine without Docker still checks the shape above; only the parse is
  # skipped, and it is stated rather than counted as a pass.
  echo "  note docker not on PATH; compose parse not exercised here (CI runs it)"
fi

# The workflow must drive the two families this stack exists for, and must not
# claim absolute capacity from a runner.
for family in volume scaling; do
  if grep -q "load/profiles/${family}.yaml" "$WORKFLOW"; then
    pass "the workflow runs the $family profile"
  else
    fail "the workflow runs the $family profile"
  fi
done

if grep -q 'weigh' "$WORKFLOW"; then
  pass "the workflow weighs the fixture it built"
else
  fail "the workflow weighs the fixture it built"
fi

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
