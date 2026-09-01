#!/usr/bin/env bash
# Guards the k6 scenarios against outgrowing the server's per-IP rate limit.
#
# Every k6 scenario drives the server from a single pod, so every virtual user
# shares one source address and therefore one token bucket. The API router
# applies a per-IP limit; a scenario whose virtual users together offer more
# requests per second than that limit spends the run collecting 429s, and the
# numbers it reports then describe the rate limiter rather than the server. The
# error-rate gate reds, and the run says "regression" while nothing regressed.
#
# The budget was sized once by hand and then lost: the scenarios were tuned to
# sit under the limit while each iteration issued four requests, the note that
# said so lived only in a commit message, and two requests added to one journey
# months later pushed it to ~120 rps against a 100 rps limit. Nothing recomputed
# the sum, so the breach surfaced as a nightly error-rate regression instead.
#
# This recomputes it on every commit, from the numbers the scenarios themselves
# declare: peak virtual users x requests per iteration / the shortest sleep. A
# request added to a hot loop moves that product, and the gate names the
# scenario and the arithmetic rather than leaving it to a nightly symptom.
#
# WebSocket upgrades are excluded because the relay routes are registered
# outside the rate-limited subrouter.
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

# offered_rps <file> — peak requests per second the scenario's iteration loop
# asks of one token bucket, or empty when the file declares no such loop.
offered_rps() {
  local f="$1" peak body reqs sleep_s

  # Peak concurrent virtual users: the largest `target:` across ramping stages,
  # or the `vus:` of a constant-rate scenario.
  peak="$(grep -oE '(target|vus): *[0-9]+' "$f" | grep -oE '[0-9]+' \
    | sort -n | tail -1 || true)"

  # The iteration body is what repeats, so it is what the budget is spent on.
  body="$(awk '/^export default function/{on=1} on' "$f")"

  # Requests billed to the limiter. Every http.<verb>( in the iteration counts,
  # including those on conditional paths: the guard sizes the worst case, which
  # is the case that empties the bucket.
  reqs="$(grep -cE 'http\.(get|post|put|patch|del|options|head|request)\(' <<<"$body" || true)"

  # The shortest sleep is the fastest the loop can turn over.
  sleep_s="$(grep -oE 'sleep\([0-9]+(\.[0-9]+)?\)' <<<"$body" | grep -oE '[0-9]+(\.[0-9]+)?' \
    | sort -n | head -1 || true)"

  if [ -z "$peak" ] || [ -z "$sleep_s" ] || [ "$reqs" -eq 0 ]; then
    return 1
  fi
  printf '%s\t%s\t%s\t%s\n' \
    "$(awk -v v="$peak" -v r="$reqs" -v s="$sleep_s" 'BEGIN { printf "%.1f", v * r / s }')" \
    "$peak" "$reqs" "$sleep_s"
}

if [ ! -f "$API" ]; then
  fail "server/internal/api/api.go is readable"
  summarize
fi

# The limit the scenarios have to live under, read from the router that applies
# it rather than restated here, so raising or lowering it re-sizes this gate.
#
# The leading non-letter is load-bearing: AuthRateLimiter ends in this name, and
# matching it would read the tighter limit guarding the login endpoints and call
# it the fleet's budget. Only the router-wide limiter is the number here.
LIMIT="$(grep -oE '[^A-Za-z]RateLimiter\([0-9]+(\.[0-9]+)?, *[0-9]+\)' "$API" | head -1 \
  | grep -oE '\(([0-9]+(\.[0-9]+)?)' | tr -d '(' || true)"

if [ -z "$LIMIT" ]; then
  fail "api.go declares a per-IP RateLimiter(rps, burst) the budget can be read from"
  summarize
fi
pass "per-IP rate limit read from api.go: ${LIMIT} rps"

shopt -s nullglob
scenarios=("$SCENARIO_DIR"/*.js)
if [ "${#scenarios[@]}" -eq 0 ]; then
  fail "load/k6/scenarios contains at least one scenario"
fi

for f in "${scenarios[@]}"; do
  name="$(basename "$f" .js)"
  if ! read -r offered peak reqs sleep_s < <(offered_rps "$f"); then
    fail "$name declares peak VUs, iteration requests and a sleep the budget can be read from"
    continue
  fi
  if awk -v o="$offered" -v l="$LIMIT" 'BEGIN { exit !(o < l) }'; then
    pass "$name offers ${offered} rps (${peak} VUs x ${reqs} req / ${sleep_s}s) under the ${LIMIT} rps limit"
  else
    fail "$name offers ${offered} rps (${peak} VUs x ${reqs} req / ${sleep_s}s) at or over the ${LIMIT} rps per-IP limit — it will collect 429s and measure the rate limiter; raise the sleep or lower the VU target"
  fi
done

# The detector has to be shown detecting. A silently broken matcher would call
# every scenario above compliant, so it is put against a fleet whose answers are
# known before its verdict on the real ones is trusted.
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

cat >"$TMP/over.js" <<'FIXTURE'
export const options = { stages: [{ duration: "1m", target: 20 }] };
export default function () {
  http.get(`${BASE_URL}/a`);
  http.get(`${BASE_URL}/b`);
  http.post(`${BASE_URL}/c`, "{}");
  http.get(`${BASE_URL}/d`);
  http.get(`${BASE_URL}/e`);
  http.get(`${BASE_URL}/f`);
  sleep(1);
}
FIXTURE

cat >"$TMP/under.js" <<'FIXTURE'
export const options = { scenarios: { s: { executor: "constant-vus", vus: 10 } } };
export default function () {
  http.get(`${BASE_URL}/a`);
  http.get(`${BASE_URL}/b`);
  sleep(1);
}
FIXTURE

cat >"$TMP/unreadable.js" <<'FIXTURE'
export const options = { stages: [{ duration: "1m", target: 20 }] };
export function helper() {
  return 1;
}
FIXTURE

read -r over_rps _ < <(offered_rps "$TMP/over.js")
if [ "$over_rps" = "120.0" ]; then
  pass "detector reads an over-budget fleet as ${over_rps} rps"
else
  fail "detector read an over-budget fleet as '${over_rps}' rps, expected 120.0"
fi

read -r under_rps _ < <(offered_rps "$TMP/under.js")
if [ "$under_rps" = "20.0" ]; then
  pass "detector reads a constant-vus fleet as ${under_rps} rps"
else
  fail "detector read a constant-vus fleet as '${under_rps}' rps, expected 20.0"
fi

if offered_rps "$TMP/unreadable.js" >/dev/null 2>&1; then
  fail "detector reports a verdict on a scenario with no iteration loop instead of refusing"
else
  pass "detector refuses a scenario whose iteration loop it cannot read"
fi

summarize
