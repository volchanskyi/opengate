#!/usr/bin/env bash
# Holds the browser test stack's bring-up to a fresh start: a stack an earlier run left up,
# database included, is taken down before anything is built, and a failed teardown stops it.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
BRINGUP="$ROOT/deploy/scripts/e2e-stack-up.sh"

PASS=0
FAIL=0
pass() {
  PASS=$((PASS + 1))
  printf '  ok   %s\n' "$1"
}
fail() {
  FAIL=$((FAIL + 1))
  printf '  FAIL %s\n' "$1" >&2
}

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
mkdir -p "$WORK/bin" "$WORK/docker-config"

# The stand-in docker reports Engine 28 and records each call; any build or start fails, so the
# bring-up stops before it would touch a real stack.
cat >"$WORK/bin/docker" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$CALLS"
case "$1" in
  version) echo 28.0.0 ;;
  compose)
    case " $* " in
      *" down "*) exit "${DOWN_STATUS:-0}" ;;
      *) exit 1 ;;
    esac
    ;;
esac
EOF
chmod +x "$WORK/bin/docker"

# bring_up runs the shipped script against the stand-in, setting its exit status and the compose
# calls it made.
bring_up() {
  local down_status="$1"
  : >"$WORK/calls"
  CALLS="$WORK/calls" DOWN_STATUS="$down_status" PATH="$WORK/bin:$PATH" DOCKER_CONFIG="$WORK/docker-config" \
    bash "$BRINGUP" >"$WORK/out" 2>&1 && status=0 || status=$?
  calls="$(grep '^compose ' "$WORK/calls" || true)"
}

echo "e2e-stack-up:"

TEARDOWN="compose -f $ROOT/deploy/docker-compose.test.yml down -v --remove-orphans"

bring_up 0
first="$(sed -n 1p <<<"$calls")"
if [[ "$first" == "$TEARDOWN" ]]; then
  pass "the first call takes the test stack down, its volumes and leftover containers with it"
else
  fail "the first compose call is [$first], so a stack an earlier run left up is reused"
fi

bring_up 1
if [[ "$status" -ne 0 && "$calls" == "$TEARDOWN" ]]; then
  pass "a teardown that fails stops the bring-up before it builds"
else
  fail "a failed teardown went on to [$calls] and exited $status"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
[[ "$FAIL" -eq 0 ]]
