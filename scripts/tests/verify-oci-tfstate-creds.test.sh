#!/usr/bin/env bash
# Offline contract tests for OCI tfstate credential diagnostics.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
ACTION="$REPO_ROOT/.github/actions/verify-oci-tfstate-creds/action.yml"
CHECKER="$REPO_ROOT/.github/actions/verify-oci-tfstate-creds/verify-oci-tfstate-creds.sh"

PASS=0
FAIL=0
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

pass() {
  PASS=$((PASS + 1))
  printf '  ok   %s\n' "$1"
}

fail() {
  FAIL=$((FAIL + 1))
  printf '  FAIL %s\n' "$1" >&2
}

expect_failure() {
  local name="$1"
  local expected="$2"
  shift 2
  local output
  local status

  if output="$("$@" 2>&1)"; then
    fail "$name exits non-zero"
    return
  else
    status=$?
  fi
  if [ "$status" -ne 0 ] && grep -qF "$expected" <<<"$output"; then
    pass "$name"
  else
    fail "$name (status=$status output=$output)"
  fi
}

echo "verify-oci-tfstate-creds:"

if [ -x "$CHECKER" ]; then
  pass "extracted checker exists and is executable"
else
  fail "extracted checker exists and is executable"
fi

# shellcheck disable=SC2016
if grep -qF '$GITHUB_ACTION_PATH/verify-oci-tfstate-creds.sh dns-and-s3' "$ACTION" \
  && grep -qF '$GITHUB_ACTION_PATH/verify-oci-tfstate-creds.sh api-key' "$ACTION"; then
  pass "composite action invokes both extracted modes"
else
  fail "composite action invokes both extracted modes"
fi

# shellcheck disable=SC2016
if [ -f "$CHECKER" ] && ! grep -qF '${{' "$CHECKER"; then
  pass "script source contains no GitHub expression interpolation"
else
  fail "script source contains no GitHub expression interpolation"
fi

if [ -x "$CHECKER" ]; then
  bin_dir="$TMP_DIR/bin"
  mkdir -p "$bin_dir"
  cat >"$bin_dir/getent" <<'EOF'
#!/usr/bin/env bash
case "${2:-}" in
  objectstorage.*) exit 0 ;;
  *) exit 1 ;;
esac
EOF
  cat >"$bin_dir/openssl" <<'EOF'
#!/usr/bin/env bash
case "$*" in
  "pkey -noout")
    cat >/dev/null
    [ "${OPENSSL_MODE:-ok}" != "parse-fail" ]
    ;;
  "pkey -pubout -outform DER")
    cat >/dev/null
    printf '%s' DER
    ;;
  "md5 -c")
    cat >/dev/null
    printf 'MD5(stdin)= %s\n' "${STUB_FINGERPRINT:?}"
    ;;
  *)
    exit 64
    ;;
esac
EOF
  chmod +x "$bin_dir/getent" "$bin_dir/openssl"

  expect_failure \
    "empty namespace is rejected" \
    "OCI_TFSTATE_NAMESPACE secret is empty" \
    env \
    PATH="$bin_dir:$PATH" \
    OCI_TFSTATE_NAMESPACE="" \
    OCI_REGION="us-sanjose-1" \
    TFSTATE_S3_ACCESS_KEY="01234567890123456789012345678901" \
    TFSTATE_S3_SECRET_KEY="0123456789012345678901234567" \
    "$CHECKER" dns-and-s3

  if output="$(
    PATH="$bin_dir:$PATH" \
      OCI_TFSTATE_NAMESPACE="validnamespace" \
      OCI_REGION="us-sanjose-1" \
      TFSTATE_S3_ACCESS_KEY="short" \
      TFSTATE_S3_SECRET_KEY="short" \
      "$CHECKER" dns-and-s3
  )" \
    && grep -qF "Region resolves but namespace-scoped endpoint does not" <<<"$output" \
    && grep -qF "S3 credentials look stale or truncated" <<<"$output"; then
    pass "DNS and stale-credential branches remain diagnostic"
  else
    fail "DNS and stale-credential branches remain diagnostic"
  fi

  fingerprint="aa:aa:aa:aa:aa:aa:aa:aa:aa:aa:aa:aa:aa:aa:aa:aa"
  private_key="$(printf '%s\n' '-----BEGIN PRIVATE'" KEY-----" test '-----END PRIVATE'" KEY-----")"
  if output="$(
    PATH="$bin_dir:$PATH" \
      STUB_FINGERPRINT="$fingerprint" \
      OCI_TENANCY_OCID="ocid1.tenancy.oc1.test" \
      OCI_DRIFT_USER_OCID="ocid1.user.oc1.test" \
      OCI_DRIFT_FINGERPRINT="$fingerprint" \
      OCI_DRIFT_PRIVATE_KEY="$private_key" \
      "$CHECKER" api-key
  )" \
    && grep -qF "fingerprint matches private key" <<<"$output"; then
    pass "matching API key fingerprint passes"
  else
    fail "matching API key fingerprint passes"
  fi

  expect_failure \
    "openssl parse failure propagates" \
    "fails openssl parse" \
    env \
    PATH="$bin_dir:$PATH" \
    OPENSSL_MODE="parse-fail" \
    STUB_FINGERPRINT="$fingerprint" \
    OCI_TENANCY_OCID="ocid1.tenancy.oc1.test" \
    OCI_DRIFT_USER_OCID="ocid1.user.oc1.test" \
    OCI_DRIFT_FINGERPRINT="$fingerprint" \
    OCI_DRIFT_PRIVATE_KEY="$private_key" \
    "$CHECKER" api-key

  expect_failure \
    "fingerprint mismatch is rejected" \
    "does NOT match" \
    env \
    PATH="$bin_dir:$PATH" \
    STUB_FINGERPRINT="bb:bb:bb:bb:bb:bb:bb:bb:bb:bb:bb:bb:bb:bb:bb:bb" \
    OCI_TENANCY_OCID="ocid1.tenancy.oc1.test" \
    OCI_DRIFT_USER_OCID="ocid1.user.oc1.test" \
    OCI_DRIFT_FINGERPRINT="$fingerprint" \
    OCI_DRIFT_PRIVATE_KEY="$private_key" \
    "$CHECKER" api-key
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ]
