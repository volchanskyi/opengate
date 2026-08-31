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

# Every path a step names must exist from where that step runs. A step that
# changes directory into server/ and then names a repository-root script does
# not fail on a missing flag or a bad argument — it fails with "No such file or
# directory" after the stack is already up, which is how all four legs of the
# sweep died on the one night this workflow has ever run.
missing_paths="$(
  python3 - "$WORKFLOW" "$REPO_ROOT" <<'PY'
import os
import re
import sys

import yaml

workflow, root = sys.argv[1], sys.argv[2]
with open(workflow, encoding="utf-8") as handle:
    document = yaml.safe_load(handle)

# Anything that looks like a repository path the step hands to a program.
candidate = re.compile(r"(?<![\w/.-])((?:scripts|load|deploy|policy|benchmarks)/[\w./-]+)")
missing = []
for job in document.get("jobs", {}).values():
    job_dir = (job.get("defaults", {}).get("run", {}) or {}).get("working-directory", "")
    for step in job.get("steps", []) or []:
        script = step.get("run")
        if not script:
            continue
        step_dir = step.get("working-directory", job_dir) or ""
        for path in set(candidate.findall(script)):
            if path.endswith((".", "/")):
                continue
            resolved = os.path.normpath(os.path.join(root, step_dir, path))
            if not os.path.exists(resolved):
                missing.append(f"{step.get('name', '?')}: {path} (from {step_dir or '.'})")
for row in sorted(missing):
    print(row)
PY
)"
if [ -z "$missing_paths" ]; then
  pass "every path a step names exists from where that step runs"
else
  fail "paths named from the wrong directory: $missing_paths"
fi

# The weighing counts rows in tables the schema actually has. Numeric readings
# live in the metrics store, not in Postgres, so a count of a readings table is
# a query that can only ever fail.
weighed_tables="$(grep -oE 'table_rows [a-z_]+' "$REPO_ROOT/scripts/perf-weigh-fixture.sh" | awk '{ print $2 }' | sort -u)"
unknown_tables=""
for table in $weighed_tables; do
  if ! grep -rqE "CREATE TABLE IF NOT EXISTS $table |ALTER TABLE [a-z_]+ RENAME TO $table;" \
    "$REPO_ROOT/server/internal/db/migrations/"; then
    unknown_tables="$unknown_tables $table"
  fi
done
if [ -z "$unknown_tables" ]; then
  pass "the weighing counts only tables the schema creates"
else
  fail "the weighing counts tables that do not exist:$unknown_tables"
fi

# --- Every family enrols; none signs its own ----------------------------------
#
# The stack's certificate authority is created by the server, inside a container
# whose /data is a tmpfs. A harness given no enrolment URL builds an authority of
# its own in a temp directory instead, and the two are then different generations
# of the same-named authority: every dial is refused with "certificate signed by
# unknown authority ... OpenGate CA". All four scaling shards died that way on
# every run the workflow has ever had, 3300 refusals apiece.
job_block() {
  awk -v job="$1" '
    $0 ~ "^  " job ":$" { in_job = 1; next }
    /^  [a-z-]+:$/ { in_job = 0 }
    in_job { print }
  ' "$WORKFLOW"
}
for family in volume scaling; do
  block="$(job_block "$family")"
  if grep -q -- '-enroll-url=' <<<"$block"; then
    pass "the $family family asks the server to sign, so it holds the server's own authority"
  else
    fail "the $family family signs its own certificates against an authority the server never made"
  fi
done

# The scaling family holds the data constant and varies the processors, so the
# fixture its profile declares has to actually be built. Running it against an
# empty database holds the data constant at nothing.
if grep -q -- '-fixture-account=' <<<"$(job_block scaling)"; then
  pass "the scaling family builds the fleet its profile declares"
else
  fail "the scaling family measures against an empty database, so its data is not the profile's"
fi

# --- The bundle's verdict is read back ----------------------------------------
#
# The harness writes what it thought of its own run into the bundle, and the
# volume family passed a run whose bundle said "invalid" and whose fleet was
# 0/500. A verdict nothing reads is a string in a file, which is the same defect
# as a cache save nobody asserts.
if grep -q 'verdict' "$WORKFLOW"; then
  pass "the workflow reads the verdict the harness wrote about the run"
else
  fail "the workflow never reads the bundle's verdict, so a run that measured nothing passes"
fi

# Weekly answered a question that changes when the schema or the read paths do.
# It now runs nightly, in a slot where the twenty-job pool is clear: the mutation
# matrix holds it from 03:00 to about 05:30 and the load test follows at 05:00.
if grep -qE "cron: '0 7 \* \* \*'" "$WORKFLOW"; then
  pass "the stack runs nightly, after the crowded slots have drained"
else
  fail "the stack must run nightly at 07:00 UTC"
fi

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
