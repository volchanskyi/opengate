#!/usr/bin/env bash
# Offline contract tests for OCI and OKE client setup.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
ACTION="$REPO_ROOT/.github/actions/oci-kube-setup/action.yml"
SETUP="$REPO_ROOT/.github/actions/oci-kube-setup/oci-kube-setup.sh"

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

echo "oci-kube-setup:"

if [ -x "$SETUP" ]; then
  pass "extracted setup script exists and is executable"
else
  fail "extracted setup script exists and is executable"
fi

for mode in configure-oci install-kube-tools fetch-kubeconfig; do
  if grep -qF "\$GITHUB_ACTION_PATH/oci-kube-setup.sh $mode" "$ACTION"; then
    pass "composite action invokes $mode"
  else
    fail "composite action invokes $mode"
  fi
done

# shellcheck disable=SC2016
if [ -f "$SETUP" ] && ! grep -qF '${{' "$SETUP"; then
  pass "script source contains no GitHub expression interpolation"
else
  fail "script source contains no GitHub expression interpolation"
fi

if [ -x "$SETUP" ]; then
  home_dir="$TMP_DIR/home"
  mkdir -p "$home_dir"
  if HOME="$home_dir" \
    OCI_TENANCY="ocid1.tenancy.oc1.test" \
    OCI_USER="ocid1.user.oc1.test" \
    OCI_FINGERPRINT="aa:bb" \
    OCI_KEY="private-key" \
    OCI_REGION="us-sanjose-1" \
    "$SETUP" configure-oci \
    && grep -qF "tenancy=ocid1.tenancy.oc1.test" "$home_dir/.oci/config" \
    && grep -qF "private-key" "$home_dir/.oci/key.pem" \
    && [ "$(stat -c %a "$home_dir/.oci/config")" = "600" ] \
    && [ "$(stat -c %a "$home_dir/.oci/key.pem")" = "600" ]; then
    pass "configure-oci writes protected OCI files"
  else
    fail "configure-oci writes protected OCI files"
  fi

  if output="$(
    HOME="$home_dir" CLUSTER_ID="" OCI_REGION="us-sanjose-1" \
      "$SETUP" fetch-kubeconfig 2>&1
  )"; then
    fail "empty cluster ID is rejected"
  elif grep -qF "cluster-id" <<<"$output"; then
    pass "empty cluster ID is rejected"
  else
    fail "empty cluster ID reports its cause"
  fi

  bin_dir="$TMP_DIR/bin"
  trace="$TMP_DIR/oci-trace"
  mkdir -p "$bin_dir"
  cat >"$bin_dir/oci" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >"$OCI_TRACE"
while [ "$#" -gt 0 ]; do
  if [ "$1" = "--file" ]; then
    mkdir -p "$(dirname "$2")"
    : >"$2"
    exit 0
  fi
  shift
done
exit 64
EOF
  cat >"$bin_dir/kubectl" <<'EOF'
#!/usr/bin/env bash
exit "${KUBECTL_STATUS:-0}"
EOF
  cat >"$bin_dir/curl" <<'EOF'
#!/usr/bin/env bash
exit 42
EOF
  chmod +x "$bin_dir/oci" "$bin_dir/kubectl" "$bin_dir/curl"

  if output="$(
    HOME="$home_dir" \
      PATH="$bin_dir:$PATH" \
      OCI_TRACE="$trace" \
      CLUSTER_ID="ocid1.cluster.oc1.test" \
      OCI_REGION="us-sanjose-1" \
      "$SETUP" fetch-kubeconfig
  )" \
    && grep -qF "kubeconfig ready" <<<"$output" \
    && grep -qF -- "--cluster-id ocid1.cluster.oc1.test" "$trace" \
    && [ "$(stat -c %a "$home_dir/.kube/config")" = "600" ]; then
    pass "fetch-kubeconfig forwards OCI inputs and protects output"
  else
    fail "fetch-kubeconfig forwards OCI inputs and protects output"
  fi

  if PATH="$bin_dir:$PATH" HELM_VERSION="3.16.3" "$SETUP" install-kube-tools >/dev/null 2>&1; then
    fail "tool download failure propagates"
  elif [ "$?" -eq 42 ]; then
    pass "tool download failure propagates"
  else
    fail "tool download failure preserves external status"
  fi
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ]
