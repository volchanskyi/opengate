#!/usr/bin/env bash
# Offline integration tests for deploy and rollback state transitions.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
DEPLOY="$REPO_ROOT/deploy/scripts/deploy.sh"
ROLLBACK="$REPO_ROOT/deploy/scripts/rollback.sh"

PASS=0
FAIL=0
TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "$TMP_ROOT"' EXIT

pass() {
  PASS=$((PASS + 1))
  printf '  ok   %s\n' "$1"
}

fail() {
  FAIL=$((FAIL + 1))
  printf '  FAIL %s\n' "$1" >&2
}

make_fixture() {
  local fixture="$1"
  mkdir -p "$fixture/bin" "$fixture/deploy"
  cat >"$fixture/deploy/.env" <<'EOF'
IMAGE_TAG=old-tag
POSTGRES_PASSWORD=not-logged
EOF
  : >"$fixture/deploy/docker-compose.yml"
  cat >"$fixture/bin/docker" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$DOCKER_TRACE"
case "$*" in
  "image inspect "*)
    printf '%s\n' "registry/image@${DOCKER_DIGEST:-sha256:new}"
    ;;
  *" up -d"*)
    exit "${DOCKER_UP_STATUS:-0}"
    ;;
  "inspect --format="*)
    printf '%s\n' healthy
    ;;
esac
EOF
  cat >"$fixture/bin/sleep" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
  chmod +x "$fixture/bin/docker" "$fixture/bin/sleep"
}

run_deploy() {
  local fixture="$1"
  local deploy_dir_name="DEPLOY_"
  deploy_dir_name+="DIR"
  shift
  env \
    PATH="$fixture/bin:$PATH" \
    "$deploy_dir_name=$fixture/deploy" \
    COSIGN_VERIFY=false \
    HEALTH_TIMEOUT=2 \
    DOCKER_TRACE="$fixture/docker.trace" \
    "$DEPLOY" "$@"
}

run_rollback() {
  local fixture="$1"
  local deploy_dir_name="DEPLOY_"
  deploy_dir_name+="DIR"
  shift
  env \
    PATH="$fixture/bin:$PATH" \
    "$deploy_dir_name=$fixture/deploy" \
    COSIGN_VERIFY=false \
    HEALTH_TIMEOUT=2 \
    DOCKER_TRACE="$fixture/docker.trace" \
    "$ROLLBACK" "$@"
}

echo "deploy/rollback:"

fixture="$TMP_ROOT/success"
make_fixture "$fixture"
if run_deploy "$fixture" --mode production --tag new-tag --git-sha abc123 --deploy-changed true >/dev/null \
  && grep -qF "IMAGE_TAG=new-tag" "$fixture/deploy/.env" \
  && grep -qF "image_digest=sha256:new" "$fixture/deploy/.last-deployed" \
  && grep -qF "git_sha=abc123" "$fixture/deploy/.last-deployed"; then
  pass "deploy updates environment and durable sentinel state"
else
  fail "deploy updates environment and durable sentinel state"
fi

before_restarts="$(grep -cE 'compose .* (down|up)' "$fixture/docker.trace" || true)"
if run_deploy "$fixture" --mode production --tag new-tag --git-sha def456 --deploy-changed false >/dev/null; then
  after_restarts="$(grep -cE 'compose .* (down|up)' "$fixture/docker.trace" || true)"
  if [ "$after_restarts" -eq "$before_restarts" ] \
    && grep -qF "git_sha=def456" "$fixture/deploy/.last-deployed"; then
    pass "unchanged idempotent deploy skips container restart"
  else
    fail "unchanged idempotent deploy skips container restart"
  fi
else
  fail "unchanged idempotent deploy succeeds"
fi

fixture="$TMP_ROOT/rollback"
make_fixture "$fixture"
printf '%s\n' old-tag >"$fixture/deploy/.previous-tag"
if DOCKER_UP_STATUS=23 run_deploy "$fixture" --mode production --tag broken-tag >/dev/null 2>&1; then
  fail "dependency failure propagates from deploy"
elif grep -qF "IMAGE_TAG=broken-tag" "$fixture/deploy/.env" \
  && run_rollback "$fixture" --mode production >/dev/null \
  && grep -qF "IMAGE_TAG=old-tag" "$fixture/deploy/.env" \
  && [ ! -e "$fixture/deploy/.previous-tag" ]; then
  pass "rollback restores the prior tag after a failed deploy"
else
  fail "rollback restores the prior tag after a failed deploy"
fi

if output="$(run_deploy "$fixture" --mode invalid --tag x 2>&1)"; then
  fail "malformed deploy mode is rejected"
elif grep -qF "Invalid mode" <<<"$output"; then
  pass "malformed deploy mode is rejected"
else
  fail "malformed deploy mode reports its cause"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ]
