#!/usr/bin/env bash
# Brings up the browser test stack: a database, a server, two real machines and a browser server,
# with agent-a and the browser each behind a router. The machines enrol with a token it mints.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEPLOY_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
ROOT="$(cd "$DEPLOY_DIR/.." && pwd)"
COMPOSE_FILE="$DEPLOY_DIR/docker-compose.test.yml"
AGENT_ENV="$DEPLOY_DIR/.e2e-agent.env"
AGENT_BINARY="$DEPLOY_DIR/agent-bin/mesh-agent"

# The stack's networks use gateway priority and trusted host interfaces, which Engine 28 added.
engine="$(docker version --format '{{.Server.Version}}' 2>/dev/null || true)"
engine_major="${engine%%.*}"
if ! [[ "$engine_major" =~ ^[0-9]+$ ]] || [ "$engine_major" -lt 28 ]; then
  echo "✗ the browser test stack needs Docker Engine 28; this machine reports '${engine:-no engine}'" >&2
  echo "  Fix: upgrade Docker Engine to 28 or later." >&2
  exit 1
fi

BASE_URL="${E2E_BASE_URL:-http://localhost:8080}"
# The first registered user becomes administrator, so the bootstrap operator registers first.
BOOTSTRAP_EMAIL="bootstrap-admin@test.local"
BOOTSTRAP_PASSWORD="BootstrapPass123!"

# The browser server runs the suite's own Playwright, which a remote browser has to match.
PLAYWRIGHT_VERSION="$(node -p "require('$ROOT/web/package-lock.json').packages['node_modules/@playwright/test'].version")"
export PLAYWRIGHT_VERSION

compose() {
  DOCKER_CONFIG="$("$ROOT/scripts/docker-credstore-guard.sh")" \
    docker compose -f "$COMPOSE_FILE" "$@"
}

if [ ! -f "$AGENT_BINARY" ]; then
  echo "✗ the agent binary is missing: $AGENT_BINARY" >&2
  echo "  Build it with: make agent-binary" >&2
  echo "  (CI's rust-agent-binary job produces it once and hands it to the e2e job.)" >&2
  exit 1
fi

echo "▶ bringing up the database and the server"
compose up -d --build --wait postgres server

# A refusal is not fatal: registering an existing account is normal on a re-run.
bearer_token() {
  curl -sS -X POST "$1" \
    -H 'Content-Type: application/json' \
    -d "{\"email\":\"$BOOTSTRAP_EMAIL\",\"password\":\"$BOOTSTRAP_PASSWORD\"}" 2>/dev/null \
    | sed -n 's/.*"token":"\([^"]*\)".*/\1/p' || true
}

echo "▶ signing in as the bootstrap operator"
token="$(bearer_token "$BASE_URL/api/v1/auth/register")"
if [ -z "$token" ]; then
  token="$(bearer_token "$BASE_URL/api/v1/auth/login")"
fi

if [ -z "$token" ]; then
  echo "✗ could not sign in as the bootstrap operator" >&2
  exit 1
fi

echo "▶ minting an enrolment token"
enrolment="$(curl -sS -X POST "$BASE_URL/api/v1/enrollment-tokens" \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $token" \
  -d '{"label":"browser test stack","max_uses":0,"expires_in_hours":24}' \
  | sed -n 's/.*"token":"\([^"]*\)".*/\1/p' || true)"

if [ -z "$enrolment" ]; then
  echo "✗ could not mint an enrolment token" >&2
  exit 1
fi

umask 077
printf 'OPENGATE_ENROLL_TOKEN=%s\n' "$enrolment" >"$AGENT_ENV"

echo "▶ starting the machines and the browser server behind their routers"
compose up -d --build agent-a agent-b
compose up -d --build --wait browser

echo "▶ waiting for both machines to come online"
for attempt in $(seq 1 60); do
  online="$(curl -sS "$BASE_URL/api/v1/devices" -H "Authorization: Bearer $token" 2>/dev/null \
    | tr ',' '\n' | grep -c '"status":"online"' || true)"
  if [ "$online" -ge 2 ]; then
    echo "✓ two machines are online (took ${attempt}s)"
    exit 0
  fi
  sleep 1
done

echo "✗ the machines never came online. Their logs:" >&2
compose logs agent-a agent-b >&2
exit 1
