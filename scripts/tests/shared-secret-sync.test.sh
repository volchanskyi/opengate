#!/usr/bin/env bash
# Tests for deploy/scripts/shared-secret-sync.sh: the release's shared secret is created once, a
# later run updates only the application password, and any other refusal fails the deploy.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
SYNC="$REPO_ROOT/deploy/scripts/shared-secret-sync.sh"
[ -x "$SYNC" ] || {
  echo "FAIL: $SYNC not executable" >&2
  exit 1
}

PASS=0
FAIL=0
FAILURES=()
pass() {
  PASS=$((PASS + 1))
  printf '  ok   %s\n' "$1"
}
fail() {
  FAIL=$((FAIL + 1))
  FAILURES+=("$1")
  printf '  FAIL %s\n' "$1" >&2
}
assert_eq() {
  local name="$1" want="$2" got="$3"
  if [ "$want" = "$got" ]; then pass "$name"; else fail "$name (want=[$want] got=[$got])"; fi
}

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
mkdir -p "$WORK/bin"

cat >"$WORK/bin/kubectl" <<'FAKE_KUBECTL'
#!/usr/bin/env bash
# Stand-in for kubectl 1.34, answering each call the way the real client and server word it.
set -uo pipefail
printf '%s\n' "$*" >>"$FAKE_DIR/calls"
args=" $* "
case "$args" in
  *" create secret generic opengate-secrets "*"--dry-run=client"*)
    printf '{"apiVersion":"v1","kind":"Secret","metadata":{"name":"opengate-secrets"},"args":"%s"}\n' \
      "$(printf '%s' "$*" | base64 | tr -d '\n')"
    ;;
  *" create secret generic opengate-secrets "*)
    if [ -f "$FAKE_DIR/secret" ]; then
      echo 'error: failed to create secret secrets "opengate-secrets" already exists' >&2
      exit 1
    fi
    echo "created" >"$FAKE_DIR/secret"
    echo "secret/opengate-secrets created"
    ;;
  *" create -f - "*)
    if [ -n "${FAKE_REFUSE:-}" ]; then
      echo "Error from server (Forbidden): $FAKE_REFUSE" >&2
      exit 1
    fi
    body="$(cat)"
    if [ -f "$FAKE_DIR/secret" ]; then
      echo 'Error from server (AlreadyExists): error when creating "STDIN": secrets "opengate-secrets" already exists' >&2
      exit 1
    fi
    printf '%s' "$body" >"$FAKE_DIR/secret"
    echo "secret/opengate-secrets created"
    ;;
  *" patch secret opengate-secrets "*)
    printf '%s' "$*" >"$FAKE_DIR/patch"
    echo "secret/opengate-secrets patched"
    ;;
  *)
    echo "unexpected kubectl call: $*" >&2
    exit 64
    ;;
esac
FAKE_KUBECTL
chmod +x "$WORK/bin/kubectl"

APP_PASSWORD="app-pass-7f3e"

# run [VAR=value...] runs the sync against a fresh fake directory named by FAKE_DIR.
run() {
  env PATH="$WORK/bin:$PATH" KUBECTL_RETRY_DELAY=0 \
    NAMESPACE=opengate-staging JWT_SECRET=jwt-secret POSTGRES_PASSWORD=pg-pass \
    POSTGRES_APP_PASSWORD="$APP_PASSWORD" AMT_PASS=amt-pass VAPID_CONTACT=mailto:ops@example.com \
    "$@" bash "$SYNC"
}

echo "shared-secret-sync:"

fresh="$WORK/fresh"
mkdir -p "$fresh"
status=0
out="$(run FAKE_DIR="$fresh" 2>&1)" || status=$?
assert_eq "a namespace without the secret gets it" "0" "$status"
assert_eq "the created secret is opengate-secrets" "opengate-secrets" \
  "$(jq -r '.metadata.name' "$fresh/secret" 2>/dev/null)"
created_args="$(jq -r '.args' "$fresh/secret" 2>/dev/null | base64 -d 2>/dev/null || true)"
assert_eq "the created secret carries every key" "1" \
  "$(grep -c 'JWT_SECRET=jwt-secret.*POSTGRES_PASSWORD=pg-pass.*POSTGRES_APP_PASSWORD=app-pass-7f3e.*AMT_PASS=amt-pass.*VAPID_CONTACT=mailto:ops@example.com' <<<"$created_args")"
assert_eq "a created secret is not patched" "absent" "$([ -f "$fresh/patch" ] && echo present || echo absent)"
assert_eq "the create lands in the release's namespace" "1" \
  "$(grep -c -- '-n opengate-staging create -f -' "$fresh/calls")"

existing="$WORK/existing"
mkdir -p "$existing"
echo "kept" >"$existing/secret"
status=0
out="$(run FAKE_DIR="$existing" 2>&1)" || status=$?
assert_eq "a namespace that has the secret succeeds" "0" "$status"
assert_eq "the existing secret keeps its other keys" "kept" "$(cat "$existing/secret")"
want_patch="{\"data\":{\"POSTGRES_APP_PASSWORD\":\"$(printf '%s' "$APP_PASSWORD" | base64 | tr -d '\n')\"}}"
assert_eq "only the application password is patched" "1" \
  "$(grep -cF -- "$want_patch" "$existing/patch" 2>/dev/null || echo 0)"
assert_eq "no secret value reaches the log" "0" "$(grep -cE 'app-pass-7f3e|jwt-secret|pg-pass' <<<"$out" || true)"

refused="$WORK/refused"
mkdir -p "$refused"
status=0
out="$(run FAKE_DIR="$refused" FAKE_REFUSE='secrets is forbidden' 2>&1)" || status=$?
assert_eq "any other refusal fails the sync" "1" "$status"
assert_eq "the refusal is shown" "1" "$(grep -c 'secrets is forbidden' <<<"$out")"
assert_eq "a refused create is not patched" "absent" "$([ -f "$refused/patch" ] && echo present || echo absent)"

missing="$WORK/missing"
mkdir -p "$missing"
status=0
out="$(run FAKE_DIR="$missing" JWT_SECRET= 2>&1)" || status=$?
assert_eq "a missing input is refused" "1" "$([ "$status" -ne 0 ] && echo 1 || echo 0)"
assert_eq "a missing input is named" "1" "$(grep -c 'JWT_SECRET' <<<"$out")"
assert_eq "a missing input reaches no cluster call" "absent" \
  "$([ -f "$missing/calls" ] && echo present || echo absent)"

echo
echo "shared-secret-sync: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
