#!/usr/bin/env bash
# Deploy OpenGate server to staging or production.
# Runs on the VPS via SSH from the CD workflow.
#
# Usage: deploy.sh --mode <staging|production> --tag <image_tag>
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=common.sh
source "${SCRIPT_DIR}/common.sh"

# --- Parse arguments ----------------------------------------------------------

MODE=""
TAG=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --mode) MODE="$2"; shift 2 ;;
    --tag)  TAG="$2";  shift 2 ;;
    *) fail "Unknown argument: $1" ;;
  esac
done

[[ -z "$MODE" ]] && fail "Missing required argument: --mode <staging|production>"
[[ -z "$TAG" ]]  && fail "Missing required argument: --tag <image_tag>"
[[ "$MODE" != "staging" && "$MODE" != "production" ]] && fail "Invalid mode: $MODE"

log "Deploying OpenGate ($MODE) with image tag: $TAG"

# --- Save previous tag (for rollback) -----------------------------------------

LOCAL_ENV_FILE=$(env_file "$MODE")
LOCAL_PREV_TAG_FILE=$(prev_tag_file "$MODE")

CURRENT_TAG=$(grep -oP '^IMAGE_TAG=\K.*' "$LOCAL_ENV_FILE" 2>/dev/null || echo "")
if [[ -n "$CURRENT_TAG" && "$CURRENT_TAG" != "$TAG" ]]; then
  echo "$CURRENT_TAG" > "$LOCAL_PREV_TAG_FILE"
  log "Saved previous tag '$CURRENT_TAG' to $LOCAL_PREV_TAG_FILE"
fi

# --- Update image tag in .env -------------------------------------------------

set_env_var IMAGE_TAG "$TAG" "$LOCAL_ENV_FILE"
log "Set IMAGE_TAG=$TAG in $LOCAL_ENV_FILE"

# --- Pull and deploy ----------------------------------------------------------

CONTAINER_NAME="opengate-server"
[[ "$MODE" == "staging" ]] && CONTAINER_NAME="opengate-server-staging"

log "Stopping existing containers..."
compose_cmd "$MODE" down --remove-orphans || true

log "Pulling image..."
compose_cmd "$MODE" pull server

log "Starting containers..."
compose_cmd "$MODE" up -d

# --- Wait for health ----------------------------------------------------------

wait_healthy "$CONTAINER_NAME"

# --- Prune old images ---------------------------------------------------------

log "Pruning images older than 7 days..."
docker image prune -f --filter "until=168h" > /dev/null 2>&1 || true

log "Deploy ($MODE) complete: tag=$TAG"
