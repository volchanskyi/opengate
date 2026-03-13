#!/usr/bin/env bash
# Rollback OpenGate to the previous image tag.
# Runs on the VPS via SSH from the CD workflow or manually.
#
# Usage: rollback.sh [--mode <staging|production>]
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=common.sh
source "${SCRIPT_DIR}/common.sh"

# --- Parse arguments ----------------------------------------------------------

MODE="production"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --mode) MODE="$2"; shift 2 ;;
    *) fail "Unknown argument: $1" ;;
  esac
done

[[ "$MODE" != "staging" && "$MODE" != "production" ]] && fail "Invalid mode: $MODE"

# --- Read previous tag --------------------------------------------------------

LOCAL_PREV_TAG_FILE=$(prev_tag_file "$MODE")
LOCAL_ENV_FILE=$(env_file "$MODE")
CONTAINER_NAME="opengate-server"
[[ "$MODE" == "staging" ]] && CONTAINER_NAME="opengate-server-staging"

[[ ! -f "$LOCAL_PREV_TAG_FILE" ]] && fail "No previous tag file found at $LOCAL_PREV_TAG_FILE — nothing to roll back to"

PREV_TAG=$(cat "$LOCAL_PREV_TAG_FILE")
[[ -z "$PREV_TAG" ]] && fail "Previous tag file is empty — nothing to roll back to"

log "Rolling back $MODE to tag: $PREV_TAG"

# --- Update image tag ---------------------------------------------------------

set_env_var IMAGE_TAG "$PREV_TAG" "$LOCAL_ENV_FILE"

# --- Pull and redeploy -------------------------------------------------------

log "Stopping existing containers..."
compose_cmd "$MODE" down --remove-orphans || true

log "Pulling previous image..."
compose_cmd "$MODE" pull server

log "Restarting containers..."
compose_cmd "$MODE" up -d

# --- Wait for health ----------------------------------------------------------

wait_healthy "$CONTAINER_NAME"

# --- Clear previous tag (prevent double-rollback) -----------------------------

rm -f "$LOCAL_PREV_TAG_FILE"

log "Rollback complete: $MODE is now running tag=$PREV_TAG"
