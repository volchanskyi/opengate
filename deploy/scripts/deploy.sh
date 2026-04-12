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
validate_mode "$MODE"

log "Deploying OpenGate ($MODE) with image tag: $TAG"

# --- Update image tag in .env -------------------------------------------------
# Note: previous tag is saved by the CD workflow BEFORE overwriting the .env
# file, so rollback.sh can restore it. See cd.yml "Deploy staging/production".

LOCAL_ENV_FILE=$(env_file "$MODE")

set_env_var IMAGE_TAG "$TAG" "$LOCAL_ENV_FILE"
log "Set IMAGE_TAG=$TAG in $LOCAL_ENV_FILE"

# --- Ensure POSTGRES_PASSWORD is set ------------------------------------------

PG_VAR="POSTGRES_PASSWORD"
[[ "$MODE" == "staging" ]] && PG_VAR="STAGING_POSTGRES_PASSWORD"

if ! grep -q "^${PG_VAR}=" "$LOCAL_ENV_FILE" 2>/dev/null; then
  fail "${PG_VAR} missing from $LOCAL_ENV_FILE — add it before deploying"
fi

# --- Pull and deploy ----------------------------------------------------------

redeploy "$MODE"

# --- Wait for health ----------------------------------------------------------

wait_healthy "$(container_name "$MODE")"

# --- Deploy monitoring stack (production only) --------------------------------

if [[ "$MODE" == "production" ]]; then
  MONITORING_COMPOSE="${DEPLOY_DIR}/docker-compose.monitoring.yml"
  MONITORING_ENV="${DEPLOY_DIR}/.env.monitoring"
  if [[ -f "$MONITORING_COMPOSE" && -f "$MONITORING_ENV" ]]; then
    log "Deploying monitoring stack..."
    docker compose \
      --project-name opengate-monitoring \
      -f "$MONITORING_COMPOSE" \
      --env-file "$MONITORING_ENV" \
      up -d --remove-orphans
    log "Monitoring stack deployed"
  else
    log "Skipping monitoring stack (compose or env file not found)"
  fi
fi

# --- Prune old images ---------------------------------------------------------

log "Pruning images older than 7 days..."
docker image prune -f --filter "until=168h" > /dev/null 2>&1 || true

log "Deploy ($MODE) complete: tag=$TAG"
