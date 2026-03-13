#!/usr/bin/env bash
# Shared functions for OpenGate deploy scripts.
# Sourced by deploy.sh, smoke-test.sh, and rollback.sh.
# Runs on the VPS — pure bash, no external deps beyond docker.
set -euo pipefail

# --- Constants ----------------------------------------------------------------

DEPLOY_DIR="${DEPLOY_DIR:-/opt/opengate}"
ENV_FILE="${DEPLOY_DIR}/.env"
PREV_TAG_FILE="${DEPLOY_DIR}/.previous-tag"
HEALTH_TIMEOUT="${HEALTH_TIMEOUT:-60}"

# env_file MODE — returns mode-specific .env path.
env_file() {
  local mode="$1"
  if [[ "$mode" == "staging" ]]; then
    echo "${DEPLOY_DIR}/.env.staging"
  else
    echo "${DEPLOY_DIR}/.env"
  fi
}

# prev_tag_file MODE — returns mode-specific previous-tag path.
prev_tag_file() {
  local mode="$1"
  if [[ "$mode" == "staging" ]]; then
    echo "${DEPLOY_DIR}/.previous-tag-staging"
  else
    echo "${DEPLOY_DIR}/.previous-tag"
  fi
}

# --- Logging ------------------------------------------------------------------

log() {
  echo "[$(date -u '+%Y-%m-%dT%H:%M:%SZ')] $*"
}

fail() {
  echo "[$(date -u '+%Y-%m-%dT%H:%M:%SZ')] FATAL: $*" >&2
  exit 1
}

# --- Docker helpers -----------------------------------------------------------

# wait_healthy CONTAINER_NAME [TIMEOUT_SECONDS]
# Polls docker health status until "healthy" or timeout.
wait_healthy() {
  local container="$1"
  local timeout="${2:-$HEALTH_TIMEOUT}"
  local elapsed=0

  log "Waiting for container '$container' to become healthy (timeout: ${timeout}s)..."

  while [ "$elapsed" -lt "$timeout" ]; do
    local status
    status=$(docker inspect --format='{{.State.Health.Status}}' "$container" 2>/dev/null || echo "missing")

    case "$status" in
      healthy)
        log "Container '$container' is healthy (${elapsed}s elapsed)"
        return 0
        ;;
      unhealthy)
        fail "Container '$container' is unhealthy after ${elapsed}s"
        ;;
      missing)
        log "Container '$container' not found yet (${elapsed}s)..."
        ;;
      *)
        log "Container '$container' status: $status (${elapsed}s)..."
        ;;
    esac

    sleep 2
    elapsed=$((elapsed + 2))
  done

  fail "Container '$container' did not become healthy within ${timeout}s"
}

# set_env_var KEY VALUE [FILE]
# Sets or updates a KEY=VALUE pair in the given .env file (defaults to $ENV_FILE).
set_env_var() {
  local key="$1" value="$2" file="${3:-$ENV_FILE}"
  if grep -q "^${key}=" "$file" 2>/dev/null; then
    sed -i "s/^${key}=.*/${key}=${value}/" "$file"
  else
    echo "${key}=${value}" >> "$file"
  fi
}

# compose_cmd MODE [ARGS...]
# Runs docker compose with the correct project name and files for the given mode.
compose_cmd() {
  local mode="$1"
  shift

  local ef
  ef=$(env_file "$mode")

  case "$mode" in
    staging)
      docker compose \
        --project-name opengate-staging \
        -f "${DEPLOY_DIR}/docker-compose.yml" \
        -f "${DEPLOY_DIR}/docker-compose.staging.yml" \
        --env-file "$ef" \
        "$@"
      ;;
    production)
      docker compose \
        --project-name opengate \
        -f "${DEPLOY_DIR}/docker-compose.yml" \
        --env-file "$ef" \
        "$@"
      ;;
    *)
      fail "Unknown mode: $mode (expected 'staging' or 'production')"
      ;;
  esac
}
