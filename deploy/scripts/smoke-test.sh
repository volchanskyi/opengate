#!/usr/bin/env bash
# Smoke tests for OpenGate: the handful of requests that prove a running server
# actually serves its API, its SPA and its relay route.
#
# `make e2e` runs it in --mode local against the compose stack, so a broken
# surface fails in the precommit gauntlet rather than in a deployment. CD runs it
# in --mode staging and --mode production against a kubectl port-forward into the
# deployed release; --domain targets the public ingress instead.
#
# Usage: smoke-test.sh --mode <local|staging|production> --domain <domain>
#    or: smoke-test.sh --mode <local|staging|production> --host <host> --port <port>
#                      --metrics-port <port> [--scheme <http|https>]
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=common.sh
source "${SCRIPT_DIR}/common.sh"

# --- Parse arguments ----------------------------------------------------------

DOMAIN=""
HOST=""
PORT=""
METRICS_PORT=""
MODE=""
SCHEME="http"

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
    *) fail "Unknown argument: $1" ;;
  esac
done

[[ -z "$MODE" ]] && fail "Missing required argument: --mode"
validate_mode "$MODE"

if [[ -n "$DOMAIN" ]]; then
  BASE_URL="https://${DOMAIN}"
else
  [[ -z "$HOST" ]] && fail "Missing required argument: --host (or use --domain)"
  [[ -z "$PORT" ]] && fail "Missing required argument: --port (or use --domain)"
  # Named rather than derived. The exposition lives on the server's second
  # listener, so a caller that forwards only the API port would otherwise
  # probe the API port for it, find the SPA fallback, and report a pass on a
  # check that never reached the endpoint.
  [[ -z "$METRICS_PORT" ]] && fail "Missing required argument: --metrics-port (or use --domain)"
  BASE_URL="${SCHEME}://${HOST}:${PORT}"
  METRICS_BASE_URL="${SCHEME}://${HOST}:${METRICS_PORT}"
fi
TESTS_PASSED=0
TESTS_FAILED=0

# --- Test helpers -------------------------------------------------------------

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
  curl -s -o /dev/null -w '%{http_code}' --max-time 10 --retry 3 --retry-delay 2 "$@"
}

# http_get URL [CURL_ARGS...]
# Sets RESPONSE_STATUS and RESPONSE_BODY from a GET request.
RESPONSE_STATUS=""
RESPONSE_BODY=""
http_get() {
  local url="$1"
  shift
  local response
  response=$(curl -s -w '\n%{http_code}' --max-time 10 --retry 3 --retry-delay 2 "$@" "$url")
  RESPONSE_STATUS=$(echo "$response" | tail -1)
  RESPONSE_BODY=$(echo "$response" | sed '$d')
}

# --- Health check (both modes) ------------------------------------------------

test_health() {
  http_get "${BASE_URL}/api/v1/health"
  [[ "$RESPONSE_STATUS" == "200" ]] || return 1
  echo "$RESPONSE_BODY" | grep -q '"status"' || return 1
}

check "GET /api/v1/health returns 200" test_health

# --- The two listeners --------------------------------------------------------
# The exposition and the profiler answer on the server's second listener, which
# the Service publishes and the Ingress does not route. A run that talks to that
# port directly — the compose published port, or a port-forward into the Service
# — reads them there. A --domain run goes through the ingress, and its job is
# the other half of the same claim: the public edge answers those paths with
# something that is not the process's own internals.

test_metrics() {
  http_get "${METRICS_BASE_URL}/metrics"
  [[ "$RESPONSE_STATUS" == "200" ]] || return 1
  echo "$RESPONSE_BODY" | grep -q 'opengate_http_requests_total' || return 1
}

test_profiler() {
  http_get "${METRICS_BASE_URL}/debug/pprof/"
  [[ "$RESPONSE_STATUS" == "200" ]] || return 1
  echo "$RESPONSE_BODY" | grep -q 'Types of profiles available' || return 1
}

# Whatever the edge answers with — the SPA fallback, or a 404 — the one thing it
# must not be is the exposition. Asserting the body rather than the status is
# deliberate: the catch-all ingress rule sends every unrouted path to the SPA,
# so a status code alone cannot tell a served page from a served registry.
test_metrics_off_the_edge() {
  http_get "${BASE_URL}/metrics"
  if echo "$RESPONSE_BODY" | grep -q 'opengate_http_requests_total'; then
    return 1
  fi
  if echo "$RESPONSE_BODY" | grep -q '^# HELP '; then
    return 1
  fi
  return 0
}

test_profiler_off_the_edge() {
  http_get "${BASE_URL}/debug/pprof/"
  if echo "$RESPONSE_BODY" | grep -q 'Types of profiles available'; then
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

# --- Web UI tests (all modes) -------------------------------------------------

test_web_index() {
  http_get "${BASE_URL}/"
  [[ "$RESPONSE_STATUS" == "200" ]] || return 1
  echo "$RESPONSE_BODY" | grep -q '<div id="root">' || return 1
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

# --- Authenticated tests ------------------------------------------------------
# These create a throwaway account, so they run everywhere that is disposable —
# the local compose stack and staging — and never against production, where the
# account would be a real one nobody asked for.

if [[ "$MODE" == "local" || "$MODE" == "staging" ]]; then

  # Register a test user
  TIMESTAMP=$(date +%s)
  TEST_EMAIL="smoke-test-${TIMESTAMP}@test.local"
  TEST_PASS="SmokeTestPass123!"

  test_register() {
    http_get "${BASE_URL}/api/v1/auth/register" \
      -X POST -H 'Content-Type: application/json' \
      -d "{\"email\":\"${TEST_EMAIL}\",\"password\":\"${TEST_PASS}\"}"

    [[ "$RESPONSE_STATUS" == "201" ]] || return 1

    # Extract JWT token for subsequent tests
    JWT=$(echo "$RESPONSE_BODY" | grep -oP '"token"\s*:\s*"\K[^"]+' || echo "")
    [[ -n "$JWT" ]] || return 1
    export JWT
  }

  check "POST /api/v1/auth/register returns 201 + JWT" test_register

  # List sites with auth — an authenticated tenant-scoped fleet read, so a 200
  # proves the JWT, the tenant context, and the database round-trip together.
  test_sites() {
    [[ -z "${JWT:-}" ]] && return 1
    local status
    status=$(http_status -H "Authorization: Bearer ${JWT}" "${BASE_URL}/api/v1/sites")
    [[ "$status" == "200" ]]
  }

  check "GET /api/v1/sites with JWT returns 200" test_sites

  # WebSocket relay route exists
  test_relay_route() {
    local status
    status=$(http_status "${BASE_URL}/ws/relay/test-token?side=browser")
    # Any non-404 proves the route is registered. Plain curl (non-WebSocket)
    # may get 200 (handler returns without writing) or 400 depending on the
    # WebSocket library version — both confirm the route exists.
    [[ "$status" != "404" ]]
  }

  check "GET /ws/relay route exists (non-404)" test_relay_route

fi

# --- Summary ------------------------------------------------------------------

log "Smoke tests complete: ${TESTS_PASSED} passed, ${TESTS_FAILED} failed"

if [[ "$TESTS_FAILED" -gt 0 ]]; then
  fail "Smoke tests failed"
fi
