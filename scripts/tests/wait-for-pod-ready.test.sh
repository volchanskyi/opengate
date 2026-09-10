#!/usr/bin/env bash
# Tests for deploy/scripts/wait-for-pod-ready.sh — the wait that says why.
#
# A wait that reports only "timed out waiting for the condition" sends whoever
# reads it looking in the wrong place, and the cluster that held the answer is
# gone by then. The nightly network drill lost a night to exactly that: a fleet
# pod the scheduler had refused for want of processor reached the log as a
# timeout and then as a message about a pod that would not answer.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
WAITER="$REPO_ROOT/deploy/scripts/wait-for-pod-ready.sh"
[ -x "$WAITER" ] || {
  echo "FAIL: $WAITER not executable" >&2
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
  if grep -qF -- "$needle" <<<"$haystack"; then
    pass "$name"
  else
    fail "$name (no [$needle] in [$haystack])"
  fi
}

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
mkdir -p "$WORK/bin"

# A kubectl stand-in whose answers are set per case, so the waiter is driven
# against a pod that comes up, one the scheduler refused, and one that is not
# there at all.
cat >"$WORK/bin/kubectl" <<'FAKE'
#!/usr/bin/env bash
for arg in "$@"; do
  case "$arg" in
    wait) verb=wait ;;
    get) verb=get ;;
    describe) verb=describe ;;
  esac
done
case "${verb:-}" in
  wait)
    [ "${FAKE_READY:-no}" = yes ] && exit 0
    echo "error: timed out waiting for the condition on pods/fleet" >&2
    exit 1
    ;;
  get)
    printf '%s' "${FAKE_PHASE:-}"
    [ -n "${FAKE_PHASE:-}" ] || exit 1
    ;;
  describe)
    printf '%s\n' "${FAKE_DESCRIBE:-}"
    ;;
esac
exit 0
FAKE
chmod +x "$WORK/bin/kubectl"
export PATH="$WORK/bin:$PATH"

# The status is taken on the failing command's own line, which errexit does not
# fire on — a command to the left of || is tested rather than run for its
# success.
run_waiter() {
  local rc=0
  NAMESPACE=opengate-staging "$WAITER" fleet 1 2>&1 || rc=$?
  printf '%s\n' "__rc=$rc"
}

# --- a pod that comes up says nothing extra ---------------------------------
out="$(FAKE_READY=yes run_waiter)"
assert_contains "a ready pod passes" "__rc=0" "$out"

# --- the scheduler's refusal reaches the log --------------------------------
refusal='Events:
  Warning  FailedScheduling  0/1 nodes are available: 1 Insufficient cpu.'
out="$(FAKE_READY=no FAKE_PHASE=Pending FAKE_DESCRIBE="$refusal" run_waiter)"
assert_contains "a pod that never became ready fails" "__rc=1" "$out"
assert_contains "the phase names the class of problem" "it is Pending" "$out"
assert_contains "the scheduler's own reason is printed" "Insufficient cpu" "$out"

# --- a pod that is not there at all says so, rather than reporting nothing ---
out="$(FAKE_READY=no FAKE_PHASE='' FAKE_DESCRIBE='' run_waiter)"
assert_contains "an absent pod is named as absent" "not there at all" "$out"
assert_contains "an absent pod still fails" "__rc=1" "$out"

# --- the namespace is not guessed at ----------------------------------------
rc=0
out="$(NAMESPACE='' "$WAITER" fleet 1 2>&1)" || rc=$?
assert_eq "a wait with no namespace refuses to run" 1 "$rc"
assert_contains "and says which input it wanted" "NAMESPACE" "$out"

printf '\n%d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '%s\n' "${FAILURES[@]}" >&2
  exit 1
fi
