#!/usr/bin/env bash
# Tests deploy/scripts/deploy-images-platform-check.sh: an image the deploy installs with no build
# for a node's platform fails by name, a workflow's image is left out, and no answer fails.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
FAKES="$SCRIPT_DIR/fixtures/dependency-fixes"

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
REPO="$WORK/repo"
HTTP="$WORK/http"
HUB="$HTTP/registry-1.docker.io/v2"
QUAY="$HTTP/quay.io/v2/jetstack/cert-manager-controller"
mkdir -p "$REPO/scripts/lib" "$REPO/deploy/scripts" "$REPO/deploy/helm/demo" "$REPO/.github/workflows" \
  "$WORK/bin" "$HUB/example/dash/manifests" "$HUB/example/dash/blobs" "$HUB/library/alpine/manifests" \
  "$QUAY/manifests"
ln -s "$ROOT/scripts/dependency-fixes" "$REPO/scripts/dependency-fixes"
ln -s "$ROOT/scripts/lib/kubectl-retry.sh" "$REPO/scripts/lib/kubectl-retry.sh"
ln -s "$ROOT/deploy/scripts/deploy-images-platform-check.sh" "$REPO/deploy/scripts/deploy-images-platform-check.sh"
ln -s "$FAKES/fake-curl.py" "$WORK/bin/curl"
cat >"$REPO/scripts/lib/tool-versions.sh" <<'MANIFEST'
export TOOL_VERSION_CERT_MANAGER_CHART="v1.20.2"
export TOOL_SOURCE_CERT_MANAGER_CHART="chart:https://charts.example.test#cert-manager"
MANIFEST
printf 'dash:\n  image: docker.io/example/dash:1.0.0\n' >"$REPO/deploy/helm/demo/values.yaml"
printf 'kubectl run probe --image=docker.io/library/alpine:3.21.9\n' >"$REPO/deploy/scripts/probe.sh"
printf 'jobs:\n  scan:\n    container: docker.io/example/scanner:2.0.0\n' >"$REPO/.github/workflows/scan.yml"
git -C "$REPO" init -q && git -C "$REPO" add -A

for repository in example/dash library/alpine; do
  token="$HTTP/auth.docker.io/token%3Fservice=registry.docker.io&scope=repository:$repository:pull"
  mkdir -p "$(dirname "$token")"
  echo '{"token": "anonymous"}' >"$token"
done
# digest DIR TAG N writes the digest a tag points at; index DIR N PLATFORM... the builds it names.
digest() {
  printf 'HTTP/2 200\r\ndocker-content-digest: sha256:%064d\r\n\r\n' "$3" >"$1/manifests/$2.headers"
}
index() {
  local dir="$1" n="$2" entries="" platform
  shift 2
  for platform in "$@"; do
    entries+="{\"platform\": {\"os\": \"${platform%/*}\", \"architecture\": \"${platform#*/}\"}}, "
  done
  printf '{"manifests": [%s{"platform": {"os": "unknown", "architecture": "unknown"}}]}\n' "$entries" \
    >"$dir/manifests/sha256:$(printf '%064d' "$n")"
}
# A tag pushed for one platform answers with its build, whose configuration names the platform.
digest "$HUB/example/dash" 1.0.0 1
printf '{"config": {"digest": "sha256:%064d"}}\n' 51 >"$HUB/example/dash/manifests/sha256:$(printf '%064d' 1)"
printf '{"os": "linux", "architecture": "amd64"}\n' >"$HUB/example/dash/blobs/sha256:$(printf '%064d' 51)"
digest "$HUB/library/alpine" 3.21.9 2
index "$HUB/library/alpine" 2 linux/amd64 linux/arm64
digest "$QUAY" v1.20.2 3
index "$QUAY" 3 linux/amd64 linux/arm64

# Helm renders the chart's controller image; kubectl names each node's platform per the jsonpath.
cat >"$WORK/bin/helm" <<'HELM'
#!/usr/bin/env bash
[ "$1" = template ] || exit 64
printf 'kind: Deployment\nspec:\n  template:\n    spec:\n      containers:\n'
printf '        - image: quay.io/jetstack/cert-manager-controller:v1.20.2\n'
HELM
cat >"$WORK/bin/kubectl" <<'KUBECTL'
#!/usr/bin/env bash
[ "$1 $2" = "get nodes" ] || exit 64
[ -z "${FAKE_UNREACHABLE:-}" ] || {
  echo "Unable to connect to the server: dial tcp 10.0.0.1:6443: i/o timeout" >&2
  exit 1
}
printf '%b' "$FAKE_NODES"
KUBECTL
chmod +x "$WORK/bin/helm" "$WORK/bin/kubectl"

run() { # VAR=value overrides for the stand-ins
  : >"$WORK/http.log"
  env PATH="$WORK/bin:$PATH" FAKE_HTTP_DIR="$HTTP" FAKE_HTTP_LOG="$WORK/http.log" XDG_CACHE_HOME="$WORK/cache" \
    KUBECTL_RETRY_DELAY=0 KUBECTL_RETRY_ATTEMPTS=1 FAKE_NODES='linux/arm64\n' "$@" \
    bash "$REPO/deploy/scripts/deploy-images-platform-check.sh" >"$WORK/out" 2>&1
}
out_has() { grep -qF -- "$1" "$WORK/out"; }

echo "deploy-images-platform-check:"

status=0
run || status=$?
if [ "$status" -eq 1 ] && out_has 'example/dash:1.0.0 has no build for linux/arm64; it is built for linux/amd64' \
  && ! out_has 'alpine' && ! out_has 'cert-manager-controller'; then
  pass "an image the deploy installs with no build for the node's platform fails the check, by name"
else
  fail "the amd64-only image must be named (status=$status out=[$(cat "$WORK/out")])"
fi
if ! grep -qF 'example/scanner' "$WORK/http.log"; then
  pass "an image only a workflow runs is left out of what the cluster must run"
else
  fail "the workflow's image must never be read (http=[$(cat "$WORK/http.log")])"
fi

digest "$HUB/example/dash" 1.0.0 4
index "$HUB/example/dash" 4 linux/amd64 linux/arm64
status=0
run || status=$?
if [ "$status" -eq 0 ] && out_has 'every one of the 3 images the deploy installs has a build for linux/arm64'; then
  pass "a deploy whose images, the cluster chart's render among them, have arm64 builds passes"
else
  fail "a deploy built for its node must pass (status=$status out=[$(cat "$WORK/out")])"
fi

digest "$HUB/example/dash" 1.0.0 5
index "$HUB/example/dash" 5 linux/arm64
status=0
run FAKE_NODES='linux/arm64\nlinux/amd64\nlinux/arm64\n' || status=$?
if [ "$status" -eq 1 ] && out_has 'example/dash:1.0.0 has no build for linux/amd64; it is built for linux/arm64'; then
  pass "every node's platform is asked for, each once"
else
  fail "the amd64 node's platform must be asked for (status=$status out=[$(cat "$WORK/out")])"
fi

status=0
run FAKE_UNREACHABLE=1 || status=$?
if [ "$status" -eq 1 ] && out_has "the cluster's nodes could not be read"; then
  pass "a cluster that gives no answer about its nodes fails the check"
else
  fail "an unread node list must fail (status=$status out=[$(cat "$WORK/out")])"
fi

status=0
run FAKE_NODES='' || status=$?
if [ "$status" -eq 1 ] && out_has "the cluster's nodes could not be read"; then
  pass "a cluster that names no node fails the check"
else
  fail "an empty node list must fail (status=$status out=[$(cat "$WORK/out")])"
fi

rm "$HUB/library/alpine/manifests/3.21.9.headers"
status=0
run || status=$?
if [ "$status" -eq 2 ] && out_has 'library/alpine/manifests/3.21.9'; then
  pass "a registry that gives no answer for an image fails the check as unreadable"
else
  fail "an unanswered registry must exit 2 (status=$status out=[$(cat "$WORK/out")])"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
