#!/usr/bin/env bash
# Tests deploy/scripts/running-images-check.sh: every image the cluster runs is declared, an image
# pinned by digest counts by its tag, an undeclared image fails, and an unread namespace fails.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
CHECK="$ROOT/deploy/scripts/running-images-check.sh"

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
mkdir -p "$WORK/bin"

# The charts render the images their pods run; the stand-in answers as `helm template` does.
cat >"$WORK/bin/helm" <<'HELM'
#!/usr/bin/env bash
[ "$1" = template ] || exit 64
case "$3" in
  ingress-nginx)
    printf 'kind: Deployment\nspec:\n  template:\n  spec:\n    containers:\n      - image: "registry.k8s.io/ingress-nginx/controller:v1.15.1@sha256:%064d"\n---\nkind: Job\nspec:\n  containers:\n    - image: registry.k8s.io/ingress-nginx/kube-webhook-certgen:v1.6.9\n' 0
    ;;
  cert-manager)
    printf 'kind: Deployment\nspec:\n  containers:\n    - image: "quay.io/jetstack/cert-manager-controller:v1.20.2"\n'
    ;;
  *) exit 64 ;;
esac
HELM
# Each namespace answers with the images its pods run, one a line, as the jsonpath asks.
cat >"$WORK/bin/kubectl" <<'KUBECTL'
#!/usr/bin/env bash
namespace="$2"
[ "$namespace" = "${FAKE_UNREAD:-}" ] && { echo "error: the server is unreachable" >&2; exit 1; }
case "$namespace" in
  opengate) printf 'ghcr.io/volchanskyi/opengate-server:sha-ed77bc1\ndocker.io/library/postgres:17-alpine\n' ;;
  ingress-nginx) printf 'registry.k8s.io/ingress-nginx/controller:v1.15.1@sha256:%064d\n' 1 ;;
  cert-manager) printf 'quay.io/jetstack/cert-manager-controller:v1.20.2\n%s' "${FAKE_EXTRA:-}" ;;
  *) printf '' ;;
esac
KUBECTL
chmod +x "$WORK/bin/helm" "$WORK/bin/kubectl"

run() { # VAR=value overrides for the stand-ins
  env PATH="$WORK/bin:$PATH" KUBECTL_RETRY_DELAY=0 KUBECTL_RETRY_ATTEMPTS=1 "$@" \
    bash "$CHECK" >"$WORK/out" 2>&1
}

echo "running-images-check:"

if run; then
  pass "a cluster running only declared images, one pinned by digest, passes"
else
  fail "a cluster running only declared images must pass (out=[$(cat "$WORK/out")])"
fi

if run FAKE_EXTRA=$'docker.io/library/redis:7\n'; then
  fail "an image the repository does not declare must fail the check"
elif grep -qF 'the cluster runs docker.io/library/redis:7, which nothing in the repository declares' "$WORK/out"; then
  pass "an image the repository does not declare fails the check, by name"
else
  fail "an undeclared image must be named (out=[$(cat "$WORK/out")])"
fi

if run FAKE_UNREAD=cert-manager; then
  fail "a namespace whose pods could not be read must fail the check"
elif grep -qF 'the pods of cert-manager could not be read' "$WORK/out"; then
  pass "a namespace whose pods could not be read fails the check, by name"
else
  fail "an unread namespace must be named (out=[$(cat "$WORK/out")])"
fi

if grep -qF 'deploy/scripts/running-images-check.sh' "$ROOT/.github/workflows/cd.yml"; then
  pass "the production deploy reads back what the cluster runs"
else
  fail "the production deploy must read back what the cluster runs"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
