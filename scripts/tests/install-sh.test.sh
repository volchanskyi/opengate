#!/usr/bin/env bash
# Offline behavioral tests for the downloadable agent installer.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
INSTALLER="$REPO_ROOT/server/internal/api/install.sh"

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
  mkdir -p "$fixture/bin" "$fixture/root/usr/local/bin" \
    "$fixture/root/etc" "$fixture/root/var/lib" "$fixture/tmp"
  printf 'agent-binary\n' >"$fixture/asset"
  sha256sum "$fixture/asset" | awk '{print $1}' >"$fixture/sha"

  cat >"$fixture/bin/curl" <<'EOF'
#!/usr/bin/env bash
output=""
url=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o)
      output="$2"
      shift 2
      ;;
    -*)
      shift
      if [ "${1:-}" = "POST" ] || [ "${1:-}" = "Content-Type: application/json" ] || [ "${1:-}" = '{"csr_pem":""}' ]; then
        shift
      fi
      ;;
    *)
      url="$1"
      shift
      ;;
  esac
done
case "$url" in
  */api/v1/enroll/*)
    printf '%s\n' '{"server_addr":"quic.example.test:443"}'
    ;;
  */api/v1/updates/manifests)
    printf '[{"os":"linux","arch":"amd64","url":"https://download.example/mesh-agent","sha256":"%s"}]\n' "$STUB_SHA"
    ;;
  https://download.example/mesh-agent)
    cp "$STUB_ASSET" "$output"
    ;;
  *)
    printf 'unexpected curl URL: %s\n' "$url" >&2
    exit 64
    ;;
esac
EOF
  cat >"$fixture/bin/systemctl" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$SYSTEMCTL_TRACE"
EOF
  chmod +x "$fixture/bin/curl" "$fixture/bin/systemctl"
}

run_installer() {
  local fixture="$1"
  shift
  env \
    PATH="$fixture/bin:$PATH" \
    OPENGATE_EFFECTIVE_UID=0 \
    OPENGATE_SERVER="https://control.example.test" \
    OPENGATE_INSTALL_DIR="$fixture/root/usr/local/bin" \
    OPENGATE_CONFIG_DIR="$fixture/root/etc/opengate-agent" \
    OPENGATE_DATA_DIR="$fixture/root/var/lib/opengate-agent" \
    OPENGATE_SYSTEMD_DIR="$fixture/root/etc/systemd/system" \
    OPENGATE_TMP_PARENT="$fixture/tmp" \
    STUB_ASSET="$fixture/asset" \
    STUB_SHA="$(cat "$fixture/sha")" \
    SYSTEMCTL_TRACE="$fixture/systemctl.trace" \
    bash "$INSTALLER" "$@"
}

echo "install.sh:"

fixture="$TMP_ROOT/success"
make_fixture "$fixture"
token="enroll-token-do-not-log"
if output="$(run_installer "$fixture" "$token" 2>&1)" \
  && [ -x "$fixture/root/usr/local/bin/mesh-agent" ] \
  && grep -qF -- "--server-addr quic.example.test:443" "$fixture/root/etc/systemd/system/mesh-agent.service" \
  && grep -qF -- "--enroll-token $token" "$fixture/root/etc/systemd/system/mesh-agent.service" \
  && grep -qF "enable --now mesh-agent" "$fixture/systemctl.trace" \
  && ! grep -qF "$token" <<<"$output"; then
  pass "clean install writes binary/service and redacts the token from output"
else
  fail "clean install writes binary/service and redacts the token from output"
fi

before_sha="$(sha256sum "$fixture/root/usr/local/bin/mesh-agent")"
if run_installer "$fixture" "$token" >/dev/null \
  && [ "$(sha256sum "$fixture/root/usr/local/bin/mesh-agent")" = "$before_sha" ] \
  && [ "$(grep -cF "enable --now mesh-agent" "$fixture/systemctl.trace")" -eq 2 ]; then
  pass "re-running the installer is idempotent"
else
  fail "re-running the installer is idempotent"
fi

fixture="$TMP_ROOT/mismatch"
make_fixture "$fixture"
printf '%064d\n' 0 >"$fixture/sha"
if output="$(run_installer "$fixture" "$token" 2>&1)"; then
  fail "checksum mismatch aborts"
elif grep -qF "SHA256 mismatch" <<<"$output" \
  && [ ! -e "$fixture/root/usr/local/bin/mesh-agent" ] \
  && [ ! -e "$fixture/root/etc/systemd/system/mesh-agent.service" ] \
  && [ -z "$(find "$fixture/tmp" -mindepth 1 -print -quit)" ]; then
  pass "checksum mismatch aborts and removes temporary artifacts"
else
  fail "checksum mismatch aborts and removes temporary artifacts"
fi

fixture="$TMP_ROOT/malformed"
make_fixture "$fixture"
if output="$(run_installer "$fixture" 2>&1)"; then
  fail "missing token is rejected"
elif grep -qF "Usage:" <<<"$output"; then
  pass "missing token is rejected"
else
  fail "missing token reports usage"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ]
