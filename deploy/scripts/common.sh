#!/usr/bin/env bash
# Shared functions for OpenGate deploy scripts.
# Sourced by deploy.sh, smoke-test.sh, and rollback.sh.
# Runs on the VPS — pure bash, no external deps beyond docker.

# --- Constants ----------------------------------------------------------------

DEPLOY_DIR="${DEPLOY_DIR:-/opt/opengate}"
HEALTH_TIMEOUT="${HEALTH_TIMEOUT:-60}"
COSIGN_VERIFY="${COSIGN_VERIFY:-true}"
IMAGE_REGISTRY="${IMAGE_REGISTRY:-ghcr.io}"
IMAGE_OWNER="${IMAGE_OWNER:-volchanskyi}"
IMAGE_NAME="${IMAGE_NAME:-opengate-server}"

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

# sentinel_file MODE — returns mode-specific last-deployed sentinel path.
# Captures what was deployed so the next CD run can skip a no-op restart.
sentinel_file() {
  local mode="$1"
  if [[ "$mode" == "staging" ]]; then
    echo "${DEPLOY_DIR}/.last-deployed-staging"
  else
    echo "${DEPLOY_DIR}/.last-deployed"
  fi
}

# read_sentinel_field MODE FIELD — prints the value of a key=value field in
# the sentinel, or empty if the file or field is missing. Used by the
# digest-equality gate in redeploy().
read_sentinel_field() {
  local mode="$1" field="$2"
  local sf
  sf=$(sentinel_file "$mode")
  [[ -f "$sf" ]] || {
    echo ""
    return 0
  }
  grep -oP "^${field}=\K.*" "$sf" 2>/dev/null || true
}

# write_sentinel MODE TAG DIGEST GIT_SHA — atomically replaces the sentinel
# with the current image+commit so the next run can skip when nothing
# changed. `mktemp + mv` keeps a half-written file from poisoning the
# field reads above.
write_sentinel() {
  local mode="$1" tag="$2" digest="$3" git_sha="$4"
  local sf tmp
  sf=$(sentinel_file "$mode")
  tmp="${sf}.tmp.$$"
  cat >"$tmp" <<EOF
image_tag=${tag}
image_digest=${digest}
git_sha=${git_sha}
deployed_at=$(date -u '+%Y-%m-%dT%H:%M:%SZ')
EOF
  mv "$tmp" "$sf"
}

# inspect_digest TAG — returns the sha256 of the locally-cached image (the
# bare digest, no "@" prefix), or empty if the image is missing. Called
# after `docker compose pull` so the comparison reflects what would be
# deployed, not whatever was running before pull.
inspect_digest() {
  local tag="$1"
  local ref="${IMAGE_REGISTRY}/${IMAGE_OWNER}/${IMAGE_NAME}:${tag}"
  docker image inspect "$ref" \
    --format '{{range .RepoDigests}}{{println .}}{{end}}' 2>/dev/null \
    | awk -F'@' '/sha256:/{print $2; exit}'
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

# container_name MODE — returns the server container name for the given mode.
container_name() {
  local mode="$1"
  if [[ "$mode" == "staging" ]]; then
    echo "opengate-server-staging"
  else
    echo "opengate-server"
  fi
}

# validate_mode MODE — exits with error if mode is invalid.
validate_mode() {
  local mode="$1"
  [[ "$mode" == "staging" || "$mode" == "production" ]] || fail "Invalid mode: $mode (expected 'staging' or 'production')"
}

# verify_image TAG — verifies cosign signature on the image before pulling.
# Requires cosign installed on the VPS. Skipped if COSIGN_VERIFY=false.
verify_image() {
  local tag="$1"
  local full_ref="${IMAGE_REGISTRY}/${IMAGE_OWNER}/${IMAGE_NAME}:${tag}"

  if [[ "$COSIGN_VERIFY" != "true" ]]; then
    log "Cosign verification disabled (COSIGN_VERIFY=$COSIGN_VERIFY)"
    return 0
  fi

  if ! command -v cosign >/dev/null 2>&1; then
    fail "cosign not found — install it or set COSIGN_VERIFY=false"
  fi

  log "Verifying cosign signature for ${full_ref}..."
  cosign verify \
    --certificate-identity-regexp="https://github.com/${IMAGE_OWNER}/.*" \
    --certificate-oidc-issuer="https://token.actions.githubusercontent.com" \
    "$full_ref" >/dev/null 2>&1 \
    || fail "Cosign signature verification failed for ${full_ref}"
  log "Cosign signature verified"
}

# redeploy MODE [GIT_SHA] [DEPLOY_CHANGED] — verifies signature, pulls,
# compares the new digest with the last-deployed sentinel, and short-circuits
# the compose restart only when BOTH the image digest is unchanged AND no
# deploy/** file changed since the last deploy. wait_healthy is called inside
# the restart branch so callers don't double-check.
#
# DEPLOY_CHANGED defaults to "true" — a missing flag (older cd.yml) forces
# the full redeploy path, never silently skips.
redeploy() {
  local mode="$1" git_sha="${2:-}" deploy_changed="${3:-true}"

  local ef
  ef=$(env_file "$mode")
  local tag
  tag=$(grep -oP '^IMAGE_TAG=\K.*' "$ef" 2>/dev/null || echo "latest")
  verify_image "$tag"

  log "Pulling image..."
  compose_cmd "$mode" pull server

  local new_digest prev_digest
  new_digest=$(inspect_digest "$tag")
  prev_digest=$(read_sentinel_field "$mode" image_digest)

  if [[ -n "$new_digest" && "$new_digest" == "$prev_digest" &&
    "$deploy_changed" == "false" ]]; then
    log "Image digest unchanged AND deploy/** unchanged — skipping compose restart."
    write_sentinel "$mode" "$tag" "$new_digest" "$git_sha"
    return 0
  fi

  if [[ -n "$new_digest" && "$new_digest" == "$prev_digest" ]]; then
    log "Image digest unchanged but deploy/** changed — config-only redeploy."
  fi

  log "Stopping existing containers..."
  compose_cmd "$mode" down --remove-orphans || true

  log "Starting containers..."
  compose_cmd "$mode" up -d

  wait_healthy "$(container_name "$mode")"

  write_sentinel "$mode" "$tag" "$new_digest" "$git_sha"
}

# set_env_var KEY VALUE FILE
# Sets or updates a KEY=VALUE pair in the given .env file.
# Uses grep+mv instead of sed to avoid regex injection via VALUE.
set_env_var() {
  local key="$1" value="$2" file="$3"
  if grep -q "^${key}=" "$file" 2>/dev/null; then
    local tmpfile="${file}.tmp.$$"
    grep -v "^${key}=" "$file" >"$tmpfile"
    echo "${key}=${value}" >>"$tmpfile"
    mv "$tmpfile" "$file"
  else
    echo "${key}=${value}" >>"$file"
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
