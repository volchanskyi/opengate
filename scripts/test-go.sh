#!/usr/bin/env bash
# Run the Go test suite against ONE shared Postgres and ONE shared
# VictoriaMetrics.
#
# testpg and testvm memoize their container per test BINARY, and `go test ./...`
# builds one binary per package. With the URLs unset, every Postgres-touching
# package therefore starts its own throwaway Postgres, and each VictoriaMetrics
# package its own VM — a dozen-plus containers and their reapers for one run.
# Provisioning once here and exporting both URLs collapses that to two.
#
# Externally-supplied URLs win: CI sets both, and a developer with a long-lived
# local stack (`make postgres-test-up`) pays no container cost at all.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

PG_CONTAINER="opengate-pg-test"
VM_CONTAINER="opengate-vm-test"
# Keep these in lockstep with testpg.PostgresImage and testvm's image pin —
# scripts/tests/test-go-provision.test.sh fails the gauntlet if they drift, so a
# developer running `make test-go` and one running a bare `go test` never
# exercise different versions.
PG_IMAGE="postgres:17-alpine"
VM_IMAGE="victoriametrics/victoria-metrics:v1.114.0"

PG_PORT="${OPENGATE_TEST_PG_PORT:-5432}"
VM_PORT="${OPENGATE_TEST_VM_PORT:-8428}"

# testutil.NewTestStore creates one schema per test for parallel-safe isolation;
# at default `go test` parallelism the working set of transient connections
# exceeds the Postgres 100-conn default. Mirrors the ci.yml / mutation.yml setup.
PG_MAX_CONNECTIONS=400

# Containers this run started, and therefore owns the teardown of.
STARTED=()

cleanup() {
  local name
  for name in ${STARTED[@]+"${STARTED[@]}"}; do
    docker rm -f "$name" >/dev/null 2>&1 || true
  done
}
trap cleanup EXIT INT TERM

start_postgres() {
  docker rm -f "$PG_CONTAINER" >/dev/null 2>&1 || true
  docker run -d --rm --name "$PG_CONTAINER" \
    -e POSTGRES_USER=opengate -e POSTGRES_PASSWORD=opengate -e POSTGRES_DB=opengate_test \
    -p "$PG_PORT:5432" "$PG_IMAGE" -c "max_connections=$PG_MAX_CONNECTIONS" >/dev/null || return 1
  STARTED+=("$PG_CONTAINER")

  local attempt
  for attempt in $(seq 1 30); do
    if docker exec "$PG_CONTAINER" pg_isready -U opengate -d opengate_test >/dev/null 2>&1; then
      return 0
    fi
    echo "test-go: waiting for Postgres ($attempt/30)" >&2
    sleep 1
  done
  echo "test-go: Postgres did not become ready within 30s" >&2
  return 1
}

start_victoriametrics() {
  docker rm -f "$VM_CONTAINER" >/dev/null 2>&1 || true
  docker run -d --rm --name "$VM_CONTAINER" \
    -p "$VM_PORT:8428" "$VM_IMAGE" >/dev/null || return 1
  STARTED+=("$VM_CONTAINER")

  local attempt
  for attempt in $(seq 1 30); do
    if curl -fsS --max-time 2 "http://127.0.0.1:$VM_PORT/health" >/dev/null 2>&1; then
      return 0
    fi
    echo "test-go: waiting for VictoriaMetrics ($attempt/30)" >&2
    sleep 1
  done
  echo "test-go: VictoriaMetrics did not become ready within 30s" >&2
  return 1
}

if [ -z "${POSTGRES_TEST_URL:-}" ]; then
  start_postgres || exit 1
  export POSTGRES_TEST_URL="postgres://opengate:opengate@localhost:$PG_PORT/opengate_test?sslmode=disable"
fi

if [ -z "${VICTORIAMETRICS_TEST_URL:-}" ]; then
  start_victoriametrics || exit 1
  export VICTORIAMETRICS_TEST_URL="http://127.0.0.1:$VM_PORT"
fi

# GO_TEST_CMD exists so the provisioning contract is testable without running the
# real suite; `make test-go` leaves it unset.
if [ -n "${GO_TEST_CMD:-}" ]; then
  rc=0
  "$GO_TEST_CMD" || rc=$?
  exit "$rc"
fi

cd "$ROOT/server" || exit 1
go test -race -timeout 5m ./...
