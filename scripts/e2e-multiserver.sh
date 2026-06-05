#!/usr/bin/env bash
# Phase 13b PR-D — multiserver e2e orchestration.
#
# Brings up the two-server + Redis + Postgres topology, runs the host-side wire
# driver (server/tests/e2e-multiserver), and tears the stack down unconditionally
# (even on failure / Ctrl-C) so no containers or volumes leak. The driver itself
# stops/starts individual services to exercise the owner-death and Redis-loss
# scenarios; this script owns only the outer lifecycle.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
COMPOSE_FILE="${REPO_ROOT}/deploy/docker-compose.multiserver.yml"
COMPOSE_PROJECT="opengate-ms"
COMPOSE=(docker compose -f "${COMPOSE_FILE}" -p "${COMPOSE_PROJECT}")

cleanup() {
  rc=$?
  if [ "${rc}" -ne 0 ]; then
    echo "==> run failed (rc=${rc}); dumping container logs for diagnosis"
    "${COMPOSE[@]}" logs --no-color --tail=200 || true
  fi
  echo "==> tearing down multiserver stack"
  "${COMPOSE[@]}" down -v --remove-orphans >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "==> building + starting multiserver stack (postgres, redis, server-a, server-b)"
"${COMPOSE[@]}" up -d --build --wait

echo "==> running multiserver e2e driver"
cd "${REPO_ROOT}/server"
OPENGATE_MULTISERVER_E2E=1 \
  E2E_SERVER_A_URL="http://localhost:18081" \
  E2E_SERVER_B_URL="http://localhost:18082" \
  E2E_DATABASE_URL="postgres://opengate:e2e-test-password@localhost:15432/opengate?sslmode=disable" \
  E2E_COMPOSE_FILE="${COMPOSE_FILE}" \
  E2E_COMPOSE_PROJECT="${COMPOSE_PROJECT}" \
  E2E_LOAD_SAMPLES="${E2E_LOAD_SAMPLES:-}" \
  go run ./tests/e2e-multiserver
