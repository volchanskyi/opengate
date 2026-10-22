#!/usr/bin/env bash
# Brings up the browser test stack: a database, a server, two real machines and a browser server,
# with agent-a and the browser each behind a router. The machines enrol with a note it writes.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEPLOY_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
ROOT="$(cd "$DEPLOY_DIR/.." && pwd)"
COMPOSE_FILE="$DEPLOY_DIR/docker-compose.test.yml"
AGENT_NOTE="$DEPLOY_DIR/.e2e-agent-note.json"
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

# One build from one reading of the tree: the server holds its master key in memory, so a later
# build that changed its image would recreate it under a key the machines' note does not name.
echo "▶ building every image"
compose build

echo "▶ bringing up the database and the server"
compose up -d --wait postgres server

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

# The note is what the installer leaves: the token, the URL the machines reach the server by, and
# the master key the probe announced. The containers' unprivileged user reads it through a mount.
probe="$(curl -sS -X POST "$BASE_URL/api/v1/enroll/$enrolment" \
  -H 'Content-Type: application/json' -d '{"csr_pem":""}')"
"$SCRIPT_DIR/install-note.sh" http://server:8080 "$enrolment" <<<"$probe" >"$AGENT_NOTE"
chmod 0644 "$AGENT_NOTE"

echo "▶ starting the machines and the browser server behind their routers"
compose up -d agent-a agent-b
# agent-c and its copy start only inside the machine-lifecycle spec, which owns their state.
compose create agent-c agent-c-copy
compose up -d --wait browser

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
