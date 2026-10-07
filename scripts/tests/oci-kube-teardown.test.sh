#!/usr/bin/env bash
# Tests for .github/actions/oci-kube-teardown/oci-kube-teardown.sh.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
TEARDOWN="$REPO_ROOT/.github/actions/oci-kube-teardown/oci-kube-teardown.sh"

if [ ! -x "$TEARDOWN" ]; then
  echo "FAIL: $TEARDOWN not found or not executable" >&2
  exit 1
fi

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

assert_absent() {
  local name="$1" path="$2"
  if [ -e "$path" ]; then
    fail "$name"
  else
    pass "$name"
  fi
}

echo "oci-kube-teardown:"

FAKE_HOME="$(mktemp -d)"
trap 'rm -rf "$FAKE_HOME"' EXIT

mkdir -p "$FAKE_HOME/.oci" "$FAKE_HOME/.kube"
printf 'PRIVATE KEY MATERIAL\n' >"$FAKE_HOME/.oci/key.pem"
printf '[DEFAULT]\n' >"$FAKE_HOME/.oci/config"
printf 'apiVersion: v1\n' >"$FAKE_HOME/.kube/config"

HOME="$FAKE_HOME" "$TEARDOWN" >/dev/null

assert_absent "removes the OCI private key" "$FAKE_HOME/.oci/key.pem"
assert_absent "removes the OCI config" "$FAKE_HOME/.oci/config"
assert_absent "removes the OCI directory" "$FAKE_HOME/.oci"
assert_absent "removes the kubeconfig" "$FAKE_HOME/.kube/config"

if HOME="$FAKE_HOME" "$TEARDOWN" >/dev/null 2>&1; then
  pass "succeeds when there is nothing to remove"
else
  fail "succeeds when there is nothing to remove"
fi

mkdir -p "$FAKE_HOME/.kube"
printf 'keep me\n' >"$FAKE_HOME/.kube/other-file"
printf 'keep me\n' >"$FAKE_HOME/.bashrc"
HOME="$FAKE_HOME" "$TEARDOWN" >/dev/null

if [ -f "$FAKE_HOME/.bashrc" ] && [ -f "$FAKE_HOME/.kube/other-file" ]; then
  pass "leaves unrelated files in place"
else
  fail "leaves unrelated files in place"
fi

setup_jobs="$(grep -rlF 'actions/oci-kube-setup' "$REPO_ROOT/.github/workflows" | sort)"
for wf in $setup_jobs; do
  setup_count="$(grep -cF 'actions/oci-kube-setup' "$wf")"
  teardown_count="$(grep -cF 'actions/oci-kube-teardown' "$wf")"
  if [ "$setup_count" -eq "$teardown_count" ]; then
    pass "$(basename "$wf") tears down every credential setup"
  else
    fail "$(basename "$wf") has $setup_count setup(s) but $teardown_count teardown(s)"
  fi
done

printf '\n%d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf 'failures:\n' >&2
  for f in "${FAILURES[@]}"; do printf '  - %s\n' "$f" >&2; done
  exit 1
fi
exit 0
