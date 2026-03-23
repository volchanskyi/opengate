#!/usr/bin/env bash
# Post-deploy smoke tests for OpenGate.
# Runs on the VPS via SSH from the CD workflow.
#
# Usage: smoke-test.sh --mode <staging|production> --domain <domain>
#    or: smoke-test.sh --mode <staging|production> --host <host> --port <port> [--scheme <http|https>]
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=common.sh
source "${SCRIPT_DIR}/common.sh"

# --- Parse arguments ----------------------------------------------------------

DOMAIN=""
HOST=""
PORT=""
MODE=""
SCHEME="http"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --domain) DOMAIN="$2"; shift 2 ;;
    --host)   HOST="$2";   shift 2 ;;
    --port)   PORT="$2";   shift 2 ;;
    --mode)   MODE="$2";   shift 2 ;;
    --scheme) SCHEME="$2"; shift 2 ;;
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
  BASE_URL="${SCHEME}://${HOST}:${PORT}"
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

# --- Metrics endpoint (both modes) -------------------------------------------

test_metrics() {
  # Metrics endpoint is only accessible internally (not proxied by Caddy).
  # When using --domain (via Caddy), skip this test.
  if [[ -n "$DOMAIN" ]]; then
    return 0
  fi
  http_get "${BASE_URL}/metrics"
  [[ "$RESPONSE_STATUS" == "200" ]] || return 1
  echo "$RESPONSE_BODY" | grep -q 'opengate_http_requests_total' || return 1
}

check "GET /metrics returns Prometheus metrics" test_metrics

# --- Web UI tests (both modes) ------------------------------------------------

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

# --- Staging-only tests -------------------------------------------------------

if [[ "$MODE" == "staging" ]]; then

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

  # List groups with auth
  test_groups() {
    [[ -z "${JWT:-}" ]] && return 1
    local status
    status=$(http_status -H "Authorization: Bearer ${JWT}" "${BASE_URL}/api/v1/groups")
    [[ "$status" == "200" ]]
  }

  check "GET /api/v1/groups with JWT returns 200" test_groups

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
