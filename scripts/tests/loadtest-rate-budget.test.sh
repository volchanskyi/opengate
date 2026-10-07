#!/usr/bin/env bash
# Guards the load run against outgrowing the server's per-address rate limit.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
API="$ROOT/server/internal/api/api.go"
SCENARIO_DIR="$ROOT/load/k6/scenarios"
PROFILE_DIR="$ROOT/load/profiles"
SESSION_LIB="$ROOT/load/k6/lib/session.js"
RATELIMIT="$ROOT/server/internal/api/ratelimit.go"
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

# The leading non-letter excludes AuthRateLimiter, which ends in the same name.
LIMIT="$(grep -oE '[^A-Za-z]RateLimiter\([0-9]+(\.[0-9]+)?, *[0-9]+' "$API" | head -1 \
  | grep -oE '\(([0-9]+(\.[0-9]+)?)' | tr -d '(' || true)"

if [ -z "$LIMIT" ]; then
  fail "api.go declares a per-address RateLimiter(rps, burst) the budget can be read from"
  summarize
fi
pass "per-address rate limit read from api.go: ${LIMIT} rps"

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
  offered="$reqs"
  if awk -v o="$offered" -v l="$LIMIT" 'BEGIN { exit !(o < l) }'; then
    pass "$name offers ${offered} rps per presented address (${reqs} req a journey) under the ${LIMIT} rps limit"
  else
    fail "$name offers ${offered} rps per presented address at or over the ${LIMIT} rps limit — one technician would collect 429s and the run would measure the rate limiter"
  fi
done

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

if grep -q 'new Counter("requests_refused")' "$SESSION_LIB"; then
  pass "the shared session helper counts what the server refused"
else
  fail "the shared session helper counts no refusal, so a night refused at the door reads the same as one that was not"
fi

if grep -qE 'refused\.add\(.*\? 1 : 0\)' "$SESSION_LIB"; then
  pass "it adds a nought on a request that was answered, so the series exists on a clean night"
else
  fail "it counts only refusals, so an export without the series cannot be told from a night nobody was refused"
fi

if [ ! -f "$RATELIMIT" ]; then
  fail "server/internal/api/ratelimit.go is readable, so what the limiter answers with can be read"
elif grep -q 'http.StatusTooManyRequests' "$RATELIMIT" && grep -q 'REFUSED_STATUS = 429' "$SESSION_LIB"; then
  pass "the status counted is the one the limiter answers with (429)"
else
  fail "the status the helper counts is not the one the limiter answers with"
fi

for f in "${scenarios[@]}"; do
  name="$(basename "$f" .js)"
  if grep -qE '^import http from "k6/http"' "$f"; then
    fail "$name asks k6 for the request client directly, so the requests it makes are outside the count"
  else
    pass "$name makes its requests through the shared helper"
  fi
done

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
