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
#                      [--scheme <http|https>] [--edge-address <ip[:port]>]
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
# Left empty so the branch below can tell "not asked for" from "asked for http",
# and pick the default that suits the target rather than one that suits both.
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
  # TLS unless the caller says otherwise. Production terminates it and staging
  # does not, and of the two a default that guessed http is the one that would
  # quietly downgrade the run nobody wants downgraded.
  SCHEME="${SCHEME:-https}"
  if [[ "$SCHEME" == "https" ]]; then EDGE_PORT=443; else EDGE_PORT=80; fi

  if [[ -n "$EDGE_ADDRESS" ]]; then
    # An Ingress routes on the Host header, and the host it matches need not be
    # a name the public resolver can answer for — staging's is not. Pinning the
    # name to the address the controller published for it keeps the request the
    # edge's own, same host and same rules, and sends it somewhere that exists.
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

# curl reports 000 when the transfer never happened at all — a name that
# resolved nowhere, a refused connection, a TLS handshake against a port serving
# plain HTTP. Three of the checks below are shaped as absences, and an absence is
# exactly what that looks like: an empty body matches no pattern, and no status
# is not 404. So each of them asks first whether the edge answered. Without it a
# run that reached nothing reports the boundary green, which is the failure this
# whole file exists to make loud.
edge_answered() {
  [[ "$1" =~ ^[1-5][0-9][0-9]$ ]]
}

# answered_200 WHAT — did the last request come back 200, and if not, what did
# it come back as? A check with several ways to fail that reports one word costs
# a whole run to tell apart afterwards.
answered_200() {
  [[ "$RESPONSE_STATUS" == "200" ]] && return 0
  log "  $1 answered ${RESPONSE_STATUS:-nothing}, ${#RESPONSE_BODY} bytes"
  return 1
}

# --- Health check (both modes) ------------------------------------------------

test_health() {
  http_get "${BASE_URL}/api/v1/health"
  [[ "$RESPONSE_STATUS" == "200" ]] || return 1
  grep -q '"status"' <<<"$RESPONSE_BODY" || return 1
}

check "GET /api/v1/health returns 200" test_health

# --- The two listeners --------------------------------------------------------
# The exposition and the profiler answer on the server's second listener, which
# the Service publishes and the Ingress does not route. A run that talks to that
# port directly — the compose published port, or a port-forward into the Service
# — reads them there. A --domain run goes through the ingress, and its job is
# the other half of the same claim: the public edge answers those paths with
# something that is not the process's own internals.

# A labelled counter publishes no sample until one of its label sets is
# incremented, so the request counter is absent for the first moments of a
# server's life and is the wrong thing to ask for here — a restart between the
# traffic and the scrape reads as no exposition at all. What is always there is
# the namespace: a gauge is published from the moment it is registered. That the
# request counter rises with traffic is asserted where it can be asserted
# deterministically, in the metrics middleware's own test.
#
# Each arm says what it saw. A check with three ways to fail that reports one
# word costs a whole run to tell apart afterwards: a 500 from the exposition, a
# registry carrying nothing of ours, and a body that never arrived are three
# different faults with three different owners, and "FAIL" names none of them.
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

# Whatever the edge answers with — the SPA fallback, or a 404 — the one thing it
# must not be is the exposition. Asserting the body rather than the status is
# deliberate: the catch-all ingress rule sends every unrouted path to the SPA,
# so a status code alone cannot tell a served page from a served registry.
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

# --- Web UI tests (all modes) -------------------------------------------------

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
    JWT=$(grep -oP '"token"\s*:\s*"\K[^"]+' <<<"$RESPONSE_BODY" || echo "")
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
    # An answer first, then which answer. Any non-404 proves the route is
    # registered: plain curl (non-WebSocket) may get 200 (handler returns
    # without writing) or 400 depending on the WebSocket library version — both
    # confirm the route exists, and neither is what a dead edge returns.
    edge_answered "$status" || return 1
    [[ "$status" != "404" ]]
  }

  check "GET /ws/relay route exists (non-404)" test_relay_route

fi

# --- Summary ------------------------------------------------------------------

log "Smoke tests complete: ${TESTS_PASSED} passed, ${TESTS_FAILED} failed"

if [[ "$TESTS_FAILED" -gt 0 ]]; then
  fail "Smoke tests failed"
fi
