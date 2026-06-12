#!/usr/bin/env bash
# Offline cache, session, and failure-cleanup tests for the bastion wrapper.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
BASTION="$REPO_ROOT/deploy/scripts/bastion-session.sh"

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

fixture="$TMP_ROOT/fixture"
mkdir -p "$fixture/bin" "$fixture/cache" "$fixture/tmp"
printf 'private\n' >"$fixture/id_ed25519"
printf 'public\n' >"$fixture/id_ed25519.pub"

cat >"$fixture/bin/oci" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$OCI_TRACE"
case "$*" in
  *"session list"*)
    printf '%s\n' '[]'
    ;;
  *"create-managed-ssh"*)
    printf '%s' "ocid1.bastionsession.test"
    ;;
  *"session get"*)
    if [ "${OCI_FAIL_GET:-0}" = "1" ]; then
      printf '%s\n' "session get failed" >&2
      exit 17
    fi
    printf '%s\n' '{"ssh-metadata":{"command":"printf connected"},"session-ttl-in-seconds":1800}'
    ;;
  *"session delete"*)
    printf '%s\n' deleted
    ;;
  *)
    printf 'unexpected oci command: %s\n' "$*" >&2
    exit 64
    ;;
esac
EOF
chmod +x "$fixture/bin/oci"

run_bastion() {
  env \
    PATH="$fixture/bin:$PATH" \
    HOME="$fixture" \
    XDG_CACHE_HOME="$fixture/cache" \
    TMPDIR="$fixture/tmp" \
    BASTION_OCID="ocid1.bastion.test" \
    INSTANCE_OCID="ocid1.instance.test" \
    INSTANCE_PRIVATE_IP="10.0.0.10" \
    BASTION_SSH_KEY="$fixture/id_ed25519" \
    OCI_TRACE="$fixture/oci.trace" \
    "$BASTION" "$@"
}

echo "bastion-session:"

if output="$(run_bastion ssh 2>&1)" \
  && grep -qF "connected" <<<"$output" \
  && jq -e '.session_id == "ocid1.bastionsession.test"' "$fixture/cache/opengate/bastion-session.json" >/dev/null \
  && [ "$(stat -c %a "$fixture/cache/opengate/bastion-session.json")" = "600" ]; then
  pass "fresh session creates a protected reusable cache"
else
  fail "fresh session creates a protected reusable cache"
fi

: >"$fixture/oci.trace"
if output="$(run_bastion ssh 2>&1)" \
  && grep -qF "Reusing cached session" <<<"$output" \
  && [ ! -s "$fixture/oci.trace" ]; then
  pass "fresh cache avoids a new OCI session"
else
  fail "fresh cache avoids a new OCI session"
fi

if run_bastion purge >/dev/null \
  && run_bastion purge >/dev/null \
  && [ ! -e "$fixture/cache/opengate/bastion-session.json" ]; then
  pass "cache purge is idempotent"
else
  fail "cache purge is idempotent"
fi

: >"$fixture/oci.trace"
if output="$(OCI_FAIL_GET=1 run_bastion ssh 2>&1)"; then
  fail "session-get failure propagates"
elif grep -qF "session get failed" <<<"$output" \
  && grep -qF "session delete --session-id ocid1.bastionsession.test --force" "$fixture/oci.trace" \
  && [ ! -e "$fixture/cache/opengate/bastion-session.json" ] \
  && [ -z "$(find "$fixture/tmp" -mindepth 1 -print -quit)" ]; then
  pass "failure removes the new remote session and local temp artifacts"
else
  fail "failure removes the new remote session and local temp artifacts"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ]
