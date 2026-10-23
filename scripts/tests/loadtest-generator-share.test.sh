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
mkdir -p "$WORK/bin" "$WORK/sbin" "$WORK/caller-bin"

# The system tools sit on sudo's secure path; the caller's own tools sit only on the caller's PATH.
SECURE_PATH="$WORK/sbin:/usr/sbin:/usr/bin:/sbin:/bin"

# Sudo as the runners configure it: the environment is reset unless the caller asks to keep it and
# sudoers lets it, and PATH is the secure path either way.
cat >"$WORK/bin/sudo" <<EOF
#!/usr/bin/env bash
set -euo pipefail
keep=0
while [ "\$#" -gt 0 ]; do
  case "\$1" in
    -n) shift ;;
    -E | --preserve-env) keep=1; shift ;;
    *) break ;;
  esac
done
if [ "\$keep" = 1 ] && [ "\$(cat "$WORK/setenv-allowed")" != "1" ]; then
  echo "sudo: sorry, you are not allowed to preserve the environment" >&2
  exit 1
fi
printf 'sudo %s\n' "\$*" >>"$WORK/calls.txt"
if [ "\$keep" = 1 ]; then
  exec env PATH="$SECURE_PATH" "\$@"
fi
exec env -i PATH="$SECURE_PATH" HOME="\$HOME" "\$@"
EOF
cat >"$WORK/caller-bin/caller-only-tool" <<'EOF'
#!/usr/bin/env bash
printf 'profile=%s\n' "${LOADTEST_PROFILE:-}"
EOF
cat >"$WORK/sbin/systemd-run" <<EOF
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
cat >"$WORK/sbin/setpriv" <<EOF
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
cat >"$WORK/sbin/systemctl" <<EOF
#!/usr/bin/env bash
set -euo pipefail
printf 'systemctl %s\n' "\$*" >>"$WORK/calls.txt"
EOF
chmod +x "$WORK/bin/sudo" "$WORK/caller-bin/caller-only-tool" \
  "$WORK/sbin/systemd-run" "$WORK/sbin/setpriv" "$WORK/sbin/systemctl"
echo 0 >"$WORK/scopes-allowed"
echo 1 >"$WORK/setenv-allowed"

run_share() {
  rm -f "$WORK/calls.txt"
  STATUS=0
  PATH="$WORK/caller-bin:$WORK/bin:$WORK/sbin:$PATH" "$SHARE" "$@" >"$WORK/out.txt" 2>&1 \
    || STATUS=$?
}

echo "loadtest-generator-share:"

run_share 1 8G -- printf 'the harness ran\n'
assert_eq "the wrapped command's status travels back" "0" "$STATUS"
if grep -qF 'the harness ran' "$WORK/out.txt"; then
  pass "the wrapped command's output reaches the caller"
else
  fail "the wrapped command's output reaches the caller"
fi
if grep -qF 'systemctl set-property --runtime opengate-generator.slice CPUQuota=100% MemoryMax=8G' "$WORK/calls.txt"; then
  pass "one processor and the memory are asked for on the generators' shared slice"
else
  fail "one processor and the memory are asked for on the generators' shared slice ($(cat "$WORK/calls.txt"))"
fi
if grep -qF 'systemd-run --scope --quiet --slice=opengate-generator.slice' "$WORK/calls.txt"; then
  pass "the command runs inside that slice, beside every other generator given the allowance"
else
  fail "the command runs inside that slice, beside every other generator given the allowance"
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

LOADTEST_PROFILE=nightly run_share 1 8G -- caller-only-tool
assert_eq "a tool on the caller's PATH runs inside the allowance" "0" "$STATUS"
if grep -qxF 'profile=nightly' "$WORK/out.txt"; then
  pass "the caller's environment reaches the command through sudo"
else
  fail "the caller's environment reaches the command through sudo ($(cat "$WORK/out.txt"))"
fi

echo 0 >"$WORK/setenv-allowed"
LOADTEST_PROFILE=nightly run_share 1 8G -- caller-only-tool
echo 1 >"$WORK/setenv-allowed"
if grep -qF 'cannot give the generator an allowance' "$WORK/out.txt" \
  && grep -qxF 'profile=nightly' "$WORK/out.txt"; then
  pass "a sudo that will not keep the environment leaves the generator unshared, and says so"
else
  fail "a sudo that will not keep the environment leaves the generator unshared, and says so ($(cat "$WORK/out.txt"))"
fi

run_share 1 8G
if [ "$STATUS" -eq 2 ]; then
  pass "a call with no command is refused"
else
  fail "a call with no command is refused"
fi

WORKFLOW="$REPO_ROOT/.github/workflows/perf-stack.yml"
# The allowance as each call in the workflow spells it, with its variables unexpanded.
allowance="loadtest-generator-share.sh \"\$PERF_GENERATOR_CPUS\" \"\$PERF_GENERATOR_MEMORY\" --"
harness_bound="$(grep -cF -- "$allowance \\" "$WORKFLOW" || true)"
k6_bound="$(grep -cF -- "$allowance scripts/loadtest-k6-alongside.sh" "$WORKFLOW" || true)"
if [ "$harness_bound" -gt 0 ] && [ "$harness_bound" = "$k6_bound" ]; then
  pass "perf-stack.yml bounds each leg's harness and its technicians with one allowance ($harness_bound legs)"
else
  fail "perf-stack.yml bounds each leg's harness and its technicians with one allowance (harness $harness_bound, k6 $k6_bound)"
fi

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
