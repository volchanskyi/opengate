#!/usr/bin/env bash
# Rollback OpenGate to the previous image tag.
# Runs on the VPS via SSH from the CD workflow or manually.
#
# Usage: rollback.sh [--mode <staging|production>]
#
# Database rollback note (Phase 13a):
# This script reverts the server image tag. Postgres data persists across
# rollbacks in the postgres-data volume — the new image will reconnect to
# the same database. If a rollback to pre-Postgres is needed, also remove
# DATABASE_URL from .env so the server falls back to SQLite, and the
# preserved server-data volume still has the old opengate.db file.
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

validate_mode "$MODE"

# --- Read previous tag --------------------------------------------------------

LOCAL_PREV_TAG_FILE=$(prev_tag_file "$MODE")
LOCAL_ENV_FILE=$(env_file "$MODE")

[[ ! -f "$LOCAL_PREV_TAG_FILE" ]] && fail "No previous tag file found at $LOCAL_PREV_TAG_FILE — nothing to roll back to"

PREV_TAG=$(cat "$LOCAL_PREV_TAG_FILE")
[[ -z "$PREV_TAG" ]] && fail "Previous tag file is empty — nothing to roll back to"

log "Rolling back $MODE to tag: $PREV_TAG"

# --- Update image tag ---------------------------------------------------------

set_env_var IMAGE_TAG "$PREV_TAG" "$LOCAL_ENV_FILE"

# --- Pull and redeploy -------------------------------------------------------

redeploy "$MODE"

# --- Wait for health ----------------------------------------------------------

wait_healthy "$(container_name "$MODE")"

# --- Clear previous tag (prevent double-rollback) -----------------------------

rm -f "$LOCAL_PREV_TAG_FILE"

log "Rollback complete: $MODE is now running tag=$PREV_TAG"
