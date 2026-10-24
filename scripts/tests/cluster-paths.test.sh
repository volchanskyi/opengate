#!/usr/bin/env bash
# Tests scripts/lib/cluster-paths.sh: a change to a chart the cluster job installs sends the deploy
# through that job, and a change to any other manifest row does not.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

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

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
git -C "$WORK" init -q
git -C "$WORK" config user.email test@example.test
git -C "$WORK" config user.name test
mkdir -p "$WORK/scripts/lib"
record_manifest() { # manifest text, message
  printf '%s\n' "$1" >"$WORK/scripts/lib/tool-versions.sh"
  git -C "$WORK" add -A
  git -C "$WORK" commit -qm "$2"
  git -C "$WORK" rev-parse HEAD
}

# shellcheck source=../lib/cluster-paths.sh
. "$ROOT/scripts/lib/cluster-paths.sh"

echo "cluster-paths:"

base="$(record_manifest 'export TOOL_VERSION_HELM="3.20.2"
export TOOL_VERSION_CERT_MANAGER_CHART="v1.20.2"
export TOOL_VERSION_INGRESS_NGINX_CHART="4.15.1"' base)"
tool="$(record_manifest 'export TOOL_VERSION_HELM="3.20.3"
export TOOL_VERSION_CERT_MANAGER_CHART="v1.20.2"
export TOOL_VERSION_INGRESS_NGINX_CHART="4.15.1"' tool)"
chart="$(record_manifest 'export TOOL_VERSION_HELM="3.20.3"
export TOOL_VERSION_CERT_MANAGER_CHART="v1.20.4"
export TOOL_VERSION_INGRESS_NGINX_CHART="4.15.1"' chart)"

if changed="$(cd "$WORK" && cluster_rows_changed "$base" "$tool")"; then
  fail "a tool's row is not a cluster piece (changed=[$changed])"
else
  pass "a change to a tool's row leaves the cluster job alone"
fi
if changed="$(cd "$WORK" && cluster_rows_changed "$tool" "$chart")" \
  && [ "$changed" = "TOOL_VERSION_CERT_MANAGER_CHART" ]; then
  pass "a change to a chart the cluster job installs sends the deploy through it, naming the row"
else
  fail "a chart row's change must send the deploy through the cluster job (changed=[${changed:-}])"
fi

rows="$(cluster_rows)"
for row in TOOL_VERSION_INGRESS_NGINX_CHART TOOL_VERSION_CERT_MANAGER_CHART; do
  if grep -qxF "$row" <<<"$rows" && grep -qF "\$$row" "$ROOT/deploy/scripts/cluster-apply.sh" \
    "$ROOT/deploy/scripts/ingress-controller-apply.sh"; then
    pass "$row is a cluster row, and the cluster job installs what it pins"
  else
    fail "$row must be a cluster row that the cluster job reads"
  fi
done
if grep -qF 'cluster_rows_changed' "$ROOT/.github/workflows/cd.yml"; then
  pass "the deploy asks whether a cluster row changed"
else
  fail "the deploy must ask whether a cluster row changed"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
