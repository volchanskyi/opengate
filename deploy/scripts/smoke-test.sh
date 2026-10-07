#!/usr/bin/env bash
# Smoke tests for a running server: its API, its SPA and its relay route.
#
# Usage: smoke-test.sh --mode <local|staging|production> --domain <domain>
#                      [--scheme <http|https>] [--edge-address <ip[:port]>]
#    or: smoke-test.sh --mode <local|staging|production> --host <host> --port <port>
#                      --metrics-port <port> [--scheme <http|https>]
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=common.sh
source "${SCRIPT_DIR}/common.sh"

DOMAIN=""
HOST=""
PORT=""
METRICS_PORT=""
MODE=""
# Empty so the branch below can tell "not asked for" from "asked for http".
SCHEME=""
EDGE_ADDRESS=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --domain)
      DOMAIN="$2"
      shift 2
      ;;
    --host)
      HOST="$2"
      shift 2
      ;;
    --port)
      PORT="$2"
      shift 2
      ;;
    --metrics-port)
      METRICS_PORT="$2"
      shift 2
      ;;
    --mode)
      MODE="$2"
      shift 2
      ;;
    --scheme)
      SCHEME="$2"
      shift 2
      ;;
    --edge-address)
      EDGE_ADDRESS="$2"
      shift 2
      ;;
    *) fail "Unknown argument: $1" ;;
  esac
done

[[ -z "$MODE" ]] && fail "Missing required argument: --mode"
validate_mode "$MODE"

# Every request carries these. Empty for a run that reaches its target by name.
CURL_EDGE=()

if [[ -n "$DOMAIN" ]]; then
  # TLS is the default, so an unspecified scheme never downgrades a run to http.
  SCHEME="${SCHEME:-https}"
  if [[ "$SCHEME" == "https" ]]; then EDGE_PORT=443; else EDGE_PORT=80; fi

  if [[ -n "$EDGE_ADDRESS" ]]; then
    # An Ingress routes on the Host header, which public DNS may not resolve; --resolve pins the
    # name to the controller's address and keeps the request the edge's own.
    EDGE_IP="${EDGE_ADDRESS%%:*}"
    [[ "$EDGE_ADDRESS" == *:* ]] && EDGE_PORT="${EDGE_ADDRESS##*:}"
    CURL_EDGE=(--resolve "${DOMAIN}:${EDGE_PORT}:${EDGE_IP}")
    BASE_URL="${SCHEME}://${DOMAIN}:${EDGE_PORT}"
  else
    BASE_URL="${SCHEME}://${DOMAIN}"
  fi
else
  [[ -n "$EDGE_ADDRESS" ]] && fail "--edge-address names the edge for --domain, which was not given"
  SCHEME="${SCHEME:-http}"
  [[ -z "$HOST" ]] && fail "Missing required argument: --host (or use --domain)"
  [[ -z "$PORT" ]] && fail "Missing required argument: --port (or use --domain)"
  # The exposition lives on a second listener; probing the API port would find the SPA fallback
  # and pass without reaching the endpoint.
  [[ -z "$METRICS_PORT" ]] && fail "Missing required argument: --metrics-port (or use --domain)"
  BASE_URL="${SCHEME}://${HOST}:${PORT}"
  METRICS_BASE_URL="${SCHEME}://${HOST}:${METRICS_PORT}"
fi
TESTS_PASSED=0
TESTS_FAILED=0

check() {
  local name="$1"
  shift
  if "$@"; then
    log "PASS: $name"
    TESTS_PASSED=$((TESTS_PASSED + 1))
  else
    log "FAIL: $name"
    TESTS_FAILED=$((TESTS_FAILED + 1))
  fi
}

http_status() {
  curl -s -o /dev/null -w '%{http_code}' --max-time 10 --retry 3 --retry-delay 2 \
    "${CURL_EDGE[@]}" "$@"
}

# http_get URL [CURL_ARGS...]
# Sets RESPONSE_STATUS and RESPONSE_BODY from a GET request.
RESPONSE_STATUS=""
RESPONSE_BODY=""
http_get() {
  local url="$1"
  shift
  local response
  response=$(curl -s -w '\n%{http_code}' --max-time 10 --retry 3 --retry-delay 2 \
    "${CURL_EDGE[@]}" "$@" "$url")
  RESPONSE_STATUS=$(echo "$response" | tail -1)
  RESPONSE_BODY=$(echo "$response" | sed '$d')
}

# curl reports 000 when no transfer happened; absence checks ask this first so a run that
# reached nothing cannot report the boundary green.
edge_answered() {
  [[ "$1" =~ ^[1-5][0-9][0-9]$ ]]
}

# Logs the status and body size a check saw when the answer was not 200.
answered_200() {
  [[ "$RESPONSE_STATUS" == "200" ]] && return 0
  log "  $1 answered ${RESPONSE_STATUS:-nothing}, ${#RESPONSE_BODY} bytes"
  return 1
}

test_health() {
  http_get "${BASE_URL}/api/v1/health"
  [[ "$RESPONSE_STATUS" == "200" ]] || return 1
  grep -q '"status"' <<<"$RESPONSE_BODY" || return 1
}

check "GET /api/v1/health returns 200" test_health

# Metrics and profiler answer on the second listener, which the Ingress does not route;
# a --domain run asserts the public edge serves neither.

# A labelled counter has no sample until incremented, so the check reads the opengate_ series
# that exist from registration; each failing arm logs what it saw.
test_metrics() {
  http_get "${METRICS_BASE_URL}/metrics"
  answered_200 "${METRICS_BASE_URL}/metrics" || return 1
  if ! grep -q '^# HELP ' <<<"$RESPONSE_BODY"; then
    log "  the exposition carried no '# HELP' line in ${#RESPONSE_BODY} bytes"
    return 1
  fi
  if ! grep -q '^opengate_' <<<"$RESPONSE_BODY"; then
    log "  the exposition carried no opengate_ series in ${#RESPONSE_BODY} bytes"
    return 1
  fi
}

test_profiler() {
  http_get "${METRICS_BASE_URL}/debug/pprof/"
  answered_200 "${METRICS_BASE_URL}/debug/pprof/" || return 1
  if ! grep -q 'Types of profiles available' <<<"$RESPONSE_BODY"; then
    log "  the profiler index was not what answered, in ${#RESPONSE_BODY} bytes"
    return 1
  fi
}

# The catch-all ingress rule sends unrouted paths to the SPA, so the body is asserted:
# a status code cannot tell a served page from a served registry.
test_metrics_off_the_edge() {
  http_get "${BASE_URL}/metrics"
  edge_answered "$RESPONSE_STATUS" || return 1
  if grep -q 'opengate_http_requests_total' <<<"$RESPONSE_BODY"; then
    return 1
  fi
  if grep -q '^# HELP ' <<<"$RESPONSE_BODY"; then
    return 1
  fi
  return 0
}

test_profiler_off_the_edge() {
  http_get "${BASE_URL}/debug/pprof/"
  edge_answered "$RESPONSE_STATUS" || return 1
  if grep -q 'Types of profiles available' <<<"$RESPONSE_BODY"; then
    return 1
  fi
  return 0
}

if [[ -n "$DOMAIN" ]]; then
  check "GET /metrics through the ingress is not the exposition" test_metrics_off_the_edge
  check "GET /debug/pprof/ through the ingress is not the profiler" test_profiler_off_the_edge
else
  check "GET /metrics returns Prometheus metrics" test_metrics
  check "GET /debug/pprof/ returns the profiler index" test_profiler
fi

test_web_index() {
  http_get "${BASE_URL}/"
  [[ "$RESPONSE_STATUS" == "200" ]] || return 1
  grep -q '<div id="root">' <<<"$RESPONSE_BODY" || return 1
}

check "GET / returns 200 with index.html" test_web_index

test_web_spa_fallback() {
  local status
  status=$(http_status "${BASE_URL}/devices")
  [[ "$status" == "200" ]]
}

check "GET /devices returns 200 (SPA fallback)" test_web_spa_fallback

test_web_static_asset() {
  local status
  status=$(http_status "${BASE_URL}/vite.svg")
  [[ "$status" == "200" ]]
}

check "GET /vite.svg returns 200 (static file)" test_web_static_asset

# These create a throwaway account, so they run only where it is disposable, never in production.

if [[ "$MODE" == "local" || "$MODE" == "staging" ]]; then

  TIMESTAMP=$(date +%s)
  TEST_EMAIL="smoke-test-${TIMESTAMP}@test.local"
  TEST_PASS="SmokeTestPass123!"

  test_register() {
    http_get "${BASE_URL}/api/v1/auth/register" \
      -X POST -H 'Content-Type: application/json' \
      -d "{\"email\":\"${TEST_EMAIL}\",\"password\":\"${TEST_PASS}\"}"

    [[ "$RESPONSE_STATUS" == "201" ]] || return 1

    JWT=$(grep -oP '"token"\s*:\s*"\K[^"]+' <<<"$RESPONSE_BODY" || echo "")
    [[ -n "$JWT" ]] || return 1
    export JWT
  }

  check "POST /api/v1/auth/register returns 201 + JWT" test_register

  # A tenant-scoped fleet read: a 200 proves the JWT, tenant context and database together.
  test_sites() {
    [[ -z "${JWT:-}" ]] && return 1
    local status
    status=$(http_status -H "Authorization: Bearer ${JWT}" "${BASE_URL}/api/v1/sites")
    [[ "$status" == "200" ]]
  }

  check "GET /api/v1/sites with JWT returns 200" test_sites

  test_relay_route() {
    local status
    status=$(http_status "${BASE_URL}/ws/relay/test-token?side=browser")
    # A registered route answers plain curl with 200 or 400 depending on the WebSocket library,
    # so any non-404 proves it exists.
    edge_answered "$status" || return 1
    [[ "$status" != "404" ]]
  }

  check "GET /ws/relay route exists (non-404)" test_relay_route

fi

log "Smoke tests complete: ${TESTS_PASSED} passed, ${TESTS_FAILED} failed"

if [[ "$TESTS_FAILED" -gt 0 ]]; then
  fail "Smoke tests failed"
fi
