#!/usr/bin/env bash
# Tests for deploy/scripts/workflow-access-readback.sh: a deploy passes only when its own login is
# told no to reading the master key's secret and to listing secrets, and fails on yes or no answer.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
READBACK="$REPO_ROOT/deploy/scripts/workflow-access-readback.sh"
[ -x "$READBACK" ] || {
  echo "FAIL: $READBACK not executable" >&2
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
assert_contains() {
  local name="$1" needle="$2" haystack="$3"
  if grep -qF -- "$needle" <<<"$haystack"; then pass "$name"; else fail "$name (missing [$needle])"; fi
}

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
mkdir -p "$WORK/bin"

cat >"$WORK/bin/kubectl" <<'FAKE_KUBECTL'
#!/usr/bin/env bash
# Stand-in for kubectl: answers each can-i question as FAKE_KEY and FAKE_LIST say.
set -uo pipefail
printf '%s\n' "$*" >>"$FAKE_TRACE"
args=" $* "
case "$args" in
  *" auth can-i get secret/opengate-authority -n opengate "*) answer="$FAKE_KEY" ;;
  *" auth can-i list secrets -n "*) answer="$FAKE_LIST" ;;
  *)
    echo "unexpected kubectl call: $*" >&2
    exit 64
    ;;
esac
case "$answer" in
  yes) echo yes ;;
  no)
    echo no
    exit 1
    ;;
  *)
    echo "error: You must be logged in to the server (Unauthorized)" >&2
    exit 1
    ;;
esac
FAKE_KUBECTL
chmod +x "$WORK/bin/kubectl"

run() {
  : >"$WORK/trace"
  env PATH="$WORK/bin:$PATH" FAKE_TRACE="$WORK/trace" FAKE_KEY=no FAKE_LIST=no NAMESPACE=opengate-staging \
    "$@" bash "$READBACK"
}

echo "workflow-access-readback:"

status=0
out="$(run 2>&1)" || status=$?
assert_eq "two plain no answers pass" "0" "$status"
assert_contains "the deploy's own namespace is asked about listing secrets" \
  "auth can-i list secrets -n opengate-staging" "$(cat "$WORK/trace")"

status=0
out="$(run FAKE_KEY=yes 2>&1)" || status=$?
assert_eq "a login that may read the master key fails the deploy" "1" "$status"
assert_contains "the read is named" "this login may read the master key's secret" "$out"

status=0
out="$(run FAKE_LIST=yes 2>&1)" || status=$?
assert_eq "a login that may list secrets fails the deploy" "1" "$status"
assert_contains "the listing is named" "this login may list every secret in opengate-staging" "$out"

status=0
out="$(run FAKE_KEY=error 2>&1)" || status=$?
assert_eq "a question the cluster never answered fails the deploy" "1" "$status"
assert_contains "the missing answer is named" "the cluster gave no answer" "$out"

echo
echo "workflow-access-readback: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
