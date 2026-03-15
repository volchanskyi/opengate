#!/usr/bin/env bash
# wait-for-server.sh — Poll /api/v1/health until the server is ready.
# Usage: ./wait-for-server.sh [base_url] [timeout_seconds]

set -euo pipefail

BASE_URL="${1:-http://localhost:8080}"
TIMEOUT="${2:-30}"
INTERVAL=2
ELAPSED=0

echo "Waiting for server at ${BASE_URL}/api/v1/health (timeout: ${TIMEOUT}s)..."

while [ "$ELAPSED" -lt "$TIMEOUT" ]; do
  if curl -sf "${BASE_URL}/api/v1/health" > /dev/null 2>&1; then
    echo "Server is ready (${ELAPSED}s elapsed)"
    exit 0
  fi
  sleep "$INTERVAL"
  ELAPSED=$((ELAPSED + INTERVAL))
done

echo "ERROR: Server not ready after ${TIMEOUT}s"
exit 1
