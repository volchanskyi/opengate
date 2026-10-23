#!/usr/bin/env bash
# Creates a release's shared secret once; on a later deploy updates only the application password.
# The login may create a secret and patch this one, and reads none.
#
# Usage: deploy/scripts/shared-secret-sync.sh
#
# Environment:
#   NAMESPACE              the release's namespace (required)
#   JWT_SECRET             the session signing key (required)
#   POSTGRES_PASSWORD      the database owner's password (required)
#   POSTGRES_APP_PASSWORD  the application role's password (required)
#   AMT_PASS               the Intel AMT password (required)
#   VAPID_CONTACT          the push contact address (required)
#
# Exit codes:
#   0  the secret exists and carries the current application password
#   1  an input is missing, or the cluster refused for any reason but the secret existing
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/lib/kubectl-retry.sh
. "$ROOT/scripts/lib/kubectl-retry.sh"

: "${NAMESPACE:?NAMESPACE is required}"
: "${JWT_SECRET:?JWT_SECRET is required}"
: "${POSTGRES_PASSWORD:?POSTGRES_PASSWORD is required}"
: "${POSTGRES_APP_PASSWORD:?POSTGRES_APP_PASSWORD is required}"
: "${AMT_PASS:?AMT_PASS is required}"
: "${VAPID_CONTACT:?VAPID_CONTACT is required}"

# The client renders the secret and the server creates it, so an existing one is reported with the
# server's AlreadyExists reason; the client's own create wording carries no reason.
rendered="$(kubectl create secret generic opengate-secrets \
  --from-literal=JWT_SECRET="$JWT_SECRET" \
  --from-literal=POSTGRES_PASSWORD="$POSTGRES_PASSWORD" \
  --from-literal=POSTGRES_APP_PASSWORD="$POSTGRES_APP_PASSWORD" \
  --from-literal=AMT_PASS="$AMT_PASS" \
  --from-literal=VAPID_CONTACT="$VAPID_CONTACT" \
  --dry-run=client -o json)"

status=0
out="$(kubectl_retry --stdin -n "$NAMESPACE" create -f - 2>&1 <<<"$rendered")" || status=$?
if [ "$status" -eq 0 ]; then
  echo "shared-secret-sync: created opengate-secrets in $NAMESPACE"
  exit 0
fi
if ! grep -qF 'AlreadyExists' <<<"$out"; then
  echo "$out" >&2
  exit 1
fi

app_password_b64="$(printf '%s' "$POSTGRES_APP_PASSWORD" | base64 | tr -d '\n')"
kubectl_retry -n "$NAMESPACE" patch secret opengate-secrets --type merge \
  -p "{\"data\":{\"POSTGRES_APP_PASSWORD\":\"${app_password_b64}\"}}" >/dev/null
echo "shared-secret-sync: opengate-secrets in $NAMESPACE carries the current application password"
