#!/usr/bin/env bash
# Post-deploy smoke tests for OpenGate.
# Runs on the VPS via SSH from the CD workflow.
#
# Usage: smoke-test.sh --host <host> --port <port> --mode <staging|production> [--scheme <http|https>]
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=common.sh
source "${SCRIPT_DIR}/common.sh"

# --- Parse arguments ----------------------------------------------------------

HOST=""
PORT=""
MODE=""
SCHEME="http"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --host)   HOST="$2";   shift 2 ;;
    --port)   PORT="$2";   shift 2 ;;
    --mode)   MODE="$2";   shift 2 ;;
    --scheme) SCHEME="$2"; shift 2 ;;
    *) fail "Unknown argument: $1" ;;
  esac
done

[[ -z "$HOST" ]] && fail "Missing required argument: --host"
[[ -z "$PORT" ]] && fail "Missing required argument: --port"
[[ -z "$MODE" ]] && fail "Missing required argument: --mode"
[[ "$MODE" != "staging" && "$MODE" != "production" ]] && fail "Invalid mode: $MODE"

BASE_URL="${SCHEME}://${HOST}:${PORT}"
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
  curl -s -o /dev/null -w '%{http_code}' --max-time 10 "$@"
}

# --- Health check (both modes) ------------------------------------------------

test_health() {
  local response status body
  response=$(curl -s -w '\n%{http_code}' --max-time 10 "${BASE_URL}/api/v1/health")
  status=$(echo "$response" | tail -1)
  body=$(echo "$response" | sed '$d')

  [[ "$status" == "200" ]] || return 1
  echo "$body" | grep -q '"status"' || return 1
}

check "GET /api/v1/health returns 200" test_health

# --- Staging-only tests -------------------------------------------------------

if [[ "$MODE" == "staging" ]]; then

  # Register a test user
  TIMESTAMP=$(date +%s)
  TEST_EMAIL="smoke-test-${TIMESTAMP}@test.local"
  TEST_PASS="SmokeTestPass123!"

  test_register() {
    local response status
    response=$(curl -s -w '\n%{http_code}' --max-time 10 \
      -X POST "${BASE_URL}/api/v1/auth/register" \
      -H 'Content-Type: application/json' \
      -d "{\"email\":\"${TEST_EMAIL}\",\"password\":\"${TEST_PASS}\"}")

    status=$(echo "$response" | tail -1)
    local body
    body=$(echo "$response" | sed '$d')

    [[ "$status" == "201" ]] || return 1

    # Extract JWT token for subsequent tests
    JWT=$(echo "$body" | grep -oP '"token"\s*:\s*"\K[^"]+' || echo "")
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
    # 400 or 401 means the route exists (bad request or unauthorized)
    [[ "$status" == "400" || "$status" == "401" ]]
  }

  check "GET /ws/relay route exists (400 or 401)" test_relay_route

fi

# --- Summary ------------------------------------------------------------------

log "Smoke tests complete: ${TESTS_PASSED} passed, ${TESTS_FAILED} failed"

if [[ "$TESTS_FAILED" -gt 0 ]]; then
  fail "Smoke tests failed"
fi
