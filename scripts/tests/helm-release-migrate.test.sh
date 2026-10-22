#!/usr/bin/env bash
# Tests for deploy/scripts/helm-release-migrate.sh: each Helm release record kept as a secret is
# copied once into the configmap Helm's configmap driver reads, and a second run changes nothing.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
MIGRATE="$REPO_ROOT/deploy/scripts/helm-release-migrate.sh"
[ -x "$MIGRATE" ] || {
  echo "FAIL: $MIGRATE not executable" >&2
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
mkdir -p "$WORK/bin" "$WORK/store"

# The release as Helm encodes it, and the secret driver's second layer around it.
ENCODED="H4sIAAAAAAAA/6tWKk4tSs1LTsxRslIqLinKTM9TqgUAm5wNjhcAAAA="
DOUBLE="$(printf '%s' "$ENCODED" | base64 | tr -d '\n')"
cat >"$WORK/secrets.json" <<SECRETS
{"items": [
  {"metadata": {"name": "sh.helm.release.v1.opengate.v41", "labels": {"name": "opengate", "owner": "helm",
    "status": "superseded", "version": "41"}}, "type": "helm.sh/release.v1", "data": {"release": "$DOUBLE"}},
  {"metadata": {"name": "sh.helm.release.v1.opengate.v42", "labels": {"name": "opengate", "owner": "helm",
    "status": "deployed", "version": "42"}}, "type": "helm.sh/release.v1", "data": {"release": "$DOUBLE"}}
]}
SECRETS

cat >"$WORK/bin/kubectl" <<'FAKE_KUBECTL'
#!/usr/bin/env bash
# Stand-in for kubectl: lists the fixture's release secrets and keeps each created configmap.
set -uo pipefail
args=" $* "
case "$args" in
  *" get secrets -l owner=helm "*)
    cat "$FAKE_SECRETS"
    ;;
  *" create -f - "*)
    body="$(cat)"
    name="$(jq -r '.metadata.name' <<<"$body")"
    if [ -f "$FAKE_STORE/$name" ]; then
      echo "Error from server (AlreadyExists): configmaps \"$name\" already exists" >&2
      exit 1
    fi
    printf '%s' "$body" >"$FAKE_STORE/$name"
    echo "configmap/$name created"
    ;;
  *)
    echo "unexpected kubectl call: $*" >&2
    exit 64
    ;;
esac
FAKE_KUBECTL
chmod +x "$WORK/bin/kubectl"

run() {
  env PATH="$WORK/bin:$PATH" FAKE_SECRETS="$WORK/secrets.json" FAKE_STORE="$WORK/store" \
    KUBECTL_RETRY_DELAY=0 bash "$MIGRATE" opengate
}

echo "helm-release-migrate:"

status=0
out="$(run 2>&1)" || status=$?
assert_eq "the first run succeeds" "0" "$status"
assert_eq "every release record is copied" "2" "$(find "$WORK/store" -type f | wc -l | tr -d ' ')"
record="$WORK/store/sh.helm.release.v1.opengate.v42"
assert_eq "the configmap holds the release as the configmap driver reads it" \
  "$ENCODED" "$(jq -r '.data.release' "$record")"
assert_eq "the configmap keeps the record's labels" \
  "opengate helm deployed 42" "$(jq -r '.metadata.labels | "\(.name) \(.owner) \(.status) \(.version)"' "$record")"
assert_eq "the configmap lands in the release's namespace" "opengate" "$(jq -r '.metadata.namespace' "$record")"
assert_eq "the first run reports what it copied" "1" "$(grep -c 'opengate: 2 copied, 0 already there' <<<"$out")"

before="$(cat "$WORK"/store/*)"
status=0
out="$(run 2>&1)" || status=$?
assert_eq "a second run succeeds" "0" "$status"
assert_eq "a second run changes nothing" "$before" "$(cat "$WORK"/store/*)"
assert_eq "a second run reports nothing copied" "1" "$(grep -c 'opengate: 0 copied, 2 already there' <<<"$out")"

echo
echo "helm-release-migrate: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
