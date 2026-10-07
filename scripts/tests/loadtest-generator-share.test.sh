#!/usr/bin/env bash
# Tests for scripts/loadtest-generator-share.sh, which gives the generator its own allowance.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
SHARE="$REPO_ROOT/scripts/loadtest-generator-share.sh"
[ -x "$SHARE" ] || {
  echo "FAIL: $SHARE not executable" >&2
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
mkdir -p "$WORK/bin"

cat >"$WORK/bin/sudo" <<EOF
#!/usr/bin/env bash
set -euo pipefail
if [ "\$1" = "-n" ]; then shift; fi
printf 'sudo %s\n' "\$*" >>"$WORK/calls.txt"
exec "\$@"
EOF
cat >"$WORK/bin/systemd-run" <<EOF
#!/usr/bin/env bash
set -euo pipefail
[ "\$(cat "$WORK/scopes-allowed")" = "0" ] || exit 1
printf 'systemd-run %s\n' "\$*" >>"$WORK/calls.txt"
for arg in "\$@"; do
  shift
  [ "\$arg" = "--" ] && break
done
exec "\$@"
EOF
cat >"$WORK/bin/setpriv" <<EOF
#!/usr/bin/env bash
set -euo pipefail
printf 'setpriv %s\n' "\$*" >>"$WORK/calls.txt"
while [ "\$#" -gt 0 ]; do
  case "\$1" in
    --reuid=* | --regid=* | --init-groups) shift ;;
    *) break ;;
  esac
done
exec "\$@"
EOF
chmod +x "$WORK/bin/sudo" "$WORK/bin/systemd-run" "$WORK/bin/setpriv"
echo 0 >"$WORK/scopes-allowed"

run_share() {
  rm -f "$WORK/calls.txt"
  STATUS=0
  PATH="$WORK/bin:$PATH" "$SHARE" "$@" >"$WORK/out.txt" 2>&1 || STATUS=$?
}

echo "loadtest-generator-share:"

run_share 1 8G -- printf 'the harness ran\n'
assert_eq "the wrapped command's status travels back" "0" "$STATUS"
if grep -qF 'the harness ran' "$WORK/out.txt"; then
  pass "the wrapped command's output reaches the caller"
else
  fail "the wrapped command's output reaches the caller"
fi
if grep -qF 'CPUQuota=100%' "$WORK/calls.txt"; then
  pass "one processor is asked for as one processor's worth of quota"
else
  fail "one processor is asked for as one processor's worth of quota"
fi
if grep -qF 'MemoryMax=8G' "$WORK/calls.txt"; then
  pass "the memory allowance is asked for"
else
  fail "the memory allowance is asked for"
fi
if grep -qF "setpriv --reuid=$(id -u) --regid=$(id -g) --init-groups" "$WORK/calls.txt"; then
  pass "the harness runs as the invoking user, so its bundle is readable"
else
  fail "the harness runs as the invoking user, so its bundle is readable"
fi

run_share 0.5 4G -- true
if grep -qF 'CPUQuota=50%' "$WORK/calls.txt"; then
  pass "half a processor is asked for as half a processor's worth of quota"
else
  fail "half a processor is asked for as half a processor's worth of quota"
fi

run_share 1 8G -- sh -c 'exit 3'
assert_eq "a failing command fails the wrapper" "3" "$STATUS"

echo 1 >"$WORK/scopes-allowed"
run_share 1 8G -- printf 'the harness ran\n'
echo 0 >"$WORK/scopes-allowed"
assert_eq "an allowance that cannot be applied still runs the harness" "0" "$STATUS"
if grep -qF 'cannot give the generator an allowance' "$WORK/out.txt" \
  && grep -qF 'the harness ran' "$WORK/out.txt"; then
  pass "an allowance that cannot be applied is announced rather than assumed"
else
  fail "an allowance that cannot be applied is announced rather than assumed"
fi

run_share 1 8G
if [ "$STATUS" -eq 2 ]; then
  pass "a call with no command is refused"
else
  fail "a call with no command is refused"
fi

WORKFLOW="$REPO_ROOT/.github/workflows/perf-stack.yml"
if grep -qF 'loadtest-generator-share.sh' "$WORKFLOW"; then
  pass "perf-stack.yml bounds its generator"
else
  fail "perf-stack.yml bounds its generator"
fi

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
