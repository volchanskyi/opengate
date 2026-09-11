#!/usr/bin/env bash
# Guards the load run against outgrowing the server's per-address rate limit.
#
# The server counts requests per address, at about a hundred a second. That
# ceiling used to be the run's throughput: every virtual user left one pod, so
# a whole scenario spent one allowance between all of it, and the sleep in each
# journey was sized to stay under it. What the night measured then was the rate
# limiter rather than the server, its latency figures described an idle system,
# and no throughput regression could ever show.
#
# So each simulated technician presents an address of its own, and each machine
# presents one too. The limit still exists and is still enforced at full
# strength — nothing is switched off — but the budget is now per technician
# rather than per fleet, and the binding constraint has moved:
#
#   * the load a scenario offers comes from the profile, as an arrival rate, and
#     is spread over as many addresses as there are virtual users;
#   * one technician's own share is what has to stay under the limit;
#   * and the whole chain that makes the presented address believed has to hold,
#     because the moment one link of it breaks, every technician is back behind
#     one allowance and the night is measuring the limiter again — silently,
#     since a 429 is an error the scenario reports as the server failing.
#
# That last part is the half a static gate can actually check, and it is checked
# here because it is a contract stated in five files and satisfied in none of
# them on its own: the chart's service selects a label, the workflow's pods
# carry it, the chart's trusted list names that service, the scenarios present
# from the shared helper, and the harness presents per machine.
#
# A scenario whose numbers cannot be read fails rather than passes: a parser
# that quietly matches nothing would report every scenario as within budget,
# which is the false green this gate exists to close.
#
# Run: ./scripts/tests/loadtest-rate-budget.test.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
API="$ROOT/server/internal/api/api.go"
SCENARIO_DIR="$ROOT/load/k6/scenarios"
PROFILE_DIR="$ROOT/load/profiles"
SESSION_LIB="$ROOT/load/k6/lib/session.js"
WORKFLOW="$ROOT/.github/workflows/load-test.yml"
CHART="$ROOT/deploy/helm/opengate"
# shellcheck source=scripts/lib/loadtest-profile.sh
. "$ROOT/scripts/lib/loadtest-profile.sh"

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

# Every exit goes through here, so a run that bails early reports what a run that
# finishes reports.
summarize() {
  echo
  echo "Summary: $PASS passed, $FAIL failed"
  if [ "$FAIL" -gt 0 ]; then
    printf '  - %s\n' "${FAILURES[@]}" >&2
    exit 1
  fi
  exit 0
}

echo "loadtest-rate-budget:"

if [ ! -f "$API" ]; then
  fail "server/internal/api/api.go is readable"
  summarize
fi

# The limit the run has to live under, read from the router that applies it
# rather than restated here, so raising or lowering it re-sizes this gate.
#
# The leading non-letter is load-bearing: AuthRateLimiter ends in this name, and
# matching it would read the tighter limit guarding the login endpoints and call
# it the fleet's budget. Only the router-wide limiter is the number here.
LIMIT="$(grep -oE '[^A-Za-z]RateLimiter\([0-9]+(\.[0-9]+)?, *[0-9]+' "$API" | head -1 \
  | grep -oE '\(([0-9]+(\.[0-9]+)?)' | tr -d '(' || true)"

if [ -z "$LIMIT" ]; then
  fail "api.go declares a per-address RateLimiter(rps, burst) the budget can be read from"
  summarize
fi
pass "per-address rate limit read from api.go: ${LIMIT} rps"

# --- what one technician offers ----------------------------------------------
#
# A scenario asks for journeys at the rate its profile declares and spreads them
# over the virtual users it allocates, so one technician's share is the requests
# in a journey divided by how long one technician waits between its own — which
# is one journey per (users / rate) seconds. The generator allocates a user per
# journey-second, so the worst case is one technician running journeys back to
# back, and that is what is sized here.
requests_per_iteration() {
  local f="$1" body
  body="$(awk '/^export default function/{on=1} on' "$f")"
  grep -cE 'http\.(get|post|put|patch|del|options|head|request)\(' <<<"$body" || true
}

shopt -s nullglob
scenarios=("$SCENARIO_DIR"/*.js)
if [ "${#scenarios[@]}" -eq 0 ]; then
  fail "load/k6/scenarios contains at least one scenario"
fi

# The busiest rate any profile asks for. Every scenario is driven by whichever
# profile the night walks, so the budget is sized against the highest one rather
# than against the one that happens to run tonight.
peak_rate=0
for profile in "$PROFILE_DIR"/*.yaml; do
  rate="$(profile_phases "$profile" | jq -r '[.[].arrivals_per_second] | max')" || {
    fail "$(basename "$profile") declares a walk this gate can read"
    continue
  }
  peak_rate="$(awk -v a="$peak_rate" -v b="$rate" 'BEGIN { print (b > a ? b : a) }')"
done
if [ "$(awk -v r="$peak_rate" 'BEGIN { print (r > 0) }')" != "1" ]; then
  fail "the profiles declare an arrival rate this gate can size a budget against"
  summarize
fi
pass "busiest declared arrival rate across the profiles: ${peak_rate}/s"

for f in "${scenarios[@]}"; do
  name="$(basename "$f" .js)"
  reqs="$(requests_per_iteration "$f")"
  if [ "$reqs" -eq 0 ]; then
    fail "$name declares an iteration loop whose requests this gate can count"
    continue
  fi
  # One technician, running its journeys back to back: the generator holds a
  # user per journey-second, so a journey that takes a second is one iteration a
  # second on that address.
  offered="$reqs"
  if awk -v o="$offered" -v l="$LIMIT" 'BEGIN { exit !(o < l) }'; then
    pass "$name offers ${offered} rps per presented address (${reqs} req a journey) under the ${LIMIT} rps limit"
  else
    fail "$name offers ${offered} rps per presented address at or over the ${LIMIT} rps limit — one technician would collect 429s and the run would measure the rate limiter"
  fi
done

# --- the chain that makes a presented address believed -----------------------
#
# Every link below is what stops a night falling back to one allowance for the
# whole fleet. None of them fails loudly at run time: the run collects 429s, the
# error-rate gate reds, and the night says "regression" while nothing regressed.

if grep -q 'X-Forwarded-For' "$SESSION_LIB"; then
  pass "the shared session helper presents an address"
else
  fail "the shared session helper presents an address — without it every virtual user spends the pod's one allowance"
fi

for f in "${scenarios[@]}"; do
  name="$(basename "$f" .js)"
  if grep -qE 'from "\.\./lib/session\.js"' "$f"; then
    pass "$name takes its headers from the shared session helper"
  else
    fail "$name takes its headers from the shared session helper, which is what presents the address"
  fi
done

# Two scenarios presenting from the same block would share one technician's
# allowance between them, which is the defect one level down. The block is
# derived from the scenario's own name, so this recomputes that derivation.
block_of() {
  node -e '
    const name = process.argv[1];
    let hash = 2166136261;
    for (let i = 0; i < name.length; i++) {
      hash ^= name.charCodeAt(i);
      hash = Math.imul(hash, 16777619) >>> 0;
    }
    process.stdout.write(String(hash % 16));
  ' "$1"
}

blocks=()
for f in "${scenarios[@]}"; do
  blocks+=("$(block_of "$(basename "$f" .js)")")
done
distinct="$(printf '%s\n' "${blocks[@]}" | sort -u | grep -c .)"
if [ "$distinct" -eq "${#blocks[@]}" ]; then
  pass "the ${#blocks[@]} scenarios present from ${distinct} distinct address blocks"
else
  fail "two scenarios present from the same address block, so they share one technician's allowance"
fi

# The generator pods have to be behind the service the server was told about.
# The label is the join, and it is written in two files.
CHART_LABEL="$(grep -oE '\{\{ \.Values\.loadTest\.generators\.label \}\}' \
  "$CHART/templates/loadtest-generators-service.yaml" || true)"
if [ -n "$CHART_LABEL" ]; then
  pass "the generators service selects on the label the values declare"
else
  fail "the generators service selects on the label the values declare"
fi

VALUES_LABEL="$(grep -oE '^\s+label: (\S+)' "$CHART/values.yaml" | awk '{print $2}' | head -1 || true)"
WORKFLOW_LABEL="$(grep -oE '^\s+LOADTEST_GENERATOR_LABEL: (\S+)' "$WORKFLOW" | awk '{print $2}' || true)"
if [ -n "$VALUES_LABEL" ] && [ "$VALUES_LABEL" = "$WORKFLOW_LABEL" ]; then
  pass "the generator pods carry the label the service selects ($VALUES_LABEL)"
else
  fail "the generator pods carry the label the service selects: chart says '${VALUES_LABEL:-<none>}', workflow says '${WORKFLOW_LABEL:-<none>}'"
fi

labelled="$(grep -cF -- 'LOADTEST_GENERATOR_LABEL=true' "$WORKFLOW" || true)"
if [ "$labelled" -eq 2 ]; then
  pass "both generator pods are created carrying it"
else
  fail "both generator pods are created carrying it — $labelled of 2 do"
fi

# And the server has to have been told the service is a proxy. The entry is
# "<service>.<namespace>", which is what the cluster's resolver answers with.
RELEASE="$(grep -oE '^\s+RELEASE: (\S+)' "$WORKFLOW" | awk '{print $2}' || true)"
NAMESPACE="$(grep -oE '^\s+NAMESPACE: (\S+)' "$WORKFLOW" | awk '{print $2}' || true)"
EXPECTED_ENTRY="${RELEASE}-loadtest-generators.${NAMESPACE}"
if grep -qF "$EXPECTED_ENTRY" "$CHART/values-staging.yaml"; then
  pass "the staging server is told the generators service is a proxy ($EXPECTED_ENTRY)"
else
  fail "the staging server is told the generators service is a proxy — no '$EXPECTED_ENTRY' in values-staging.yaml"
fi

if grep -qE '^\s+- ingress-nginx-controller\.ingress-nginx$' "$CHART/values.yaml"; then
  pass "the edge is named in the chart's own default, so production believes only it"
else
  fail "the edge is named in the chart's own default"
fi

# The machine half. A fleet that presents one address between all of it runs
# into the same ceiling on arrivals alone.
HARNESS="$ROOT/server/tests/loadtest"
if grep -q 'PresentedAddress: presentedAddress(' "$HARNESS/credentials.go"; then
  pass "an arriving machine presents its own address"
else
  fail "an arriving machine presents its own address — otherwise a fleet enrols behind one allowance"
fi
if grep -q 'presented := presentedAddress(index)' "$HARNESS/fixture_build.go"; then
  pass "filing a machine is charged to that same address"
else
  fail "filing a machine is charged to that same address"
fi

# The detector has to be shown detecting. A silently broken counter would call
# every scenario above compliant, so it is put against files whose answers are
# known before its verdict on the real ones is trusted.
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

cat >"$TMP/over.js" <<'FIXTURE'
export default function () {
  http.get(`${BASE_URL}/a`);
  http.get(`${BASE_URL}/b`);
  http.post(`${BASE_URL}/c`, "{}");
}
FIXTURE

cat >"$TMP/unreadable.js" <<'FIXTURE'
export function helper() {
  return 1;
}
FIXTURE

if [ "$(requests_per_iteration "$TMP/over.js")" = "3" ]; then
  pass "the counter reads a three-request journey as three"
else
  fail "the counter read a three-request journey as $(requests_per_iteration "$TMP/over.js")"
fi

if [ "$(requests_per_iteration "$TMP/unreadable.js")" = "0" ]; then
  pass "the counter refuses a file with no iteration loop rather than guessing"
else
  fail "the counter reported a verdict on a file with no iteration loop"
fi

summarize
