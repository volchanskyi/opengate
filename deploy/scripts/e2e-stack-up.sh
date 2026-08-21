#!/usr/bin/env bash
# Brings up the browser test stack: a database, a server, and two real machines.
#
# The machines cannot be started with everything else, because a machine needs
# an enrolment token and a token can only be minted once the server is
# answering. So the bring-up is in two halves with a mint between them, and it
# uses the same public endpoints an installer uses — no test-only affordance
# exists in the shipped server, and no key is copied anywhere.
#
# Both `make e2e` and playwright.config.ts's webServer call this script, so the
# two paths cannot bring up different stacks.
#
# Run: bash deploy/scripts/e2e-stack-up.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEPLOY_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
ROOT="$(cd "$DEPLOY_DIR/.." && pwd)"
COMPOSE_FILE="$DEPLOY_DIR/docker-compose.test.yml"
AGENT_ENV="$DEPLOY_DIR/.e2e-agent.env"
AGENT_BINARY="$DEPLOY_DIR/agent-bin/mesh-agent"

BASE_URL="${E2E_BASE_URL:-http://localhost:8080}"
# The bootstrap operator. Matches web/e2e/global-setup.ts, which logs in as this
# account: the first registered user is promoted to administrator, so whoever
# registers first has to be the one the suite then uses.
BOOTSTRAP_EMAIL="bootstrap-admin@test.local"
BOOTSTRAP_PASSWORD="BootstrapPass123!"

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

# bearer_token URL — POST the bootstrap credentials and read the token out of
# the answer. A refusal is not fatal here: registering an account that already
# exists is the ordinary case on a re-run against a stack that is still up.
bearer_token() {
  curl -sS -X POST "$1" \
    -H 'Content-Type: application/json' \
    -d "{\"email\":\"$BOOTSTRAP_EMAIL\",\"password\":\"$BOOTSTRAP_PASSWORD\"}" 2>/dev/null \
    | sed -n 's/.*"token":"\([^"]*\)".*/\1/p' || true
}

echo "▶ signing in as the bootstrap operator"
# The first account registered is promoted to administrator, so this has to run
# before anything else creates one.
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

echo "▶ starting the machines"
compose up -d --build agent-a agent-b

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
