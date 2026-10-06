#!/usr/bin/env bash
# Tests for scripts/assert-cache-written.sh against a gh stand-in that answers from FAKE_KEYS.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
GUARD="$REPO_ROOT/scripts/assert-cache-written.sh"
[ -x "$GUARD" ] || {
  echo "FAIL: $GUARD not executable" >&2
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

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

cat >"$WORK/gh" <<'FAKE_GH'
#!/usr/bin/env bash
# Stand-in for gh: prints the keys the test put in FAKE_KEYS, or fails when
# FAKE_GH_FAILS is set, so an unreachable API is told apart from an empty one.
set -uo pipefail
if [ -n "${FAKE_GH_FAILS:-}" ]; then
  echo "gh: HTTP 403" >&2
  exit 1
fi
printf '%s' "${FAKE_KEYS:-}"
FAKE_GH
chmod +x "$WORK/gh"
export PATH="$WORK:$PATH"
export GITHUB_REPOSITORY="volchanskyi/opengate"

echo "assert-cache-written:"

run_guard() {
  "$GUARD" "$@" >"$WORK/out" 2>&1
}

FAKE_KEYS="$(printf 'node-cache-Linux-x64-npm-abc\nv0-rust-aarch64-unknown-linux-musl-build-agent-Linux-x64-1-2\n')"
export FAKE_KEYS
if run_guard "v0-rust-aarch64-unknown-linux-musl-build-agent"; then
  pass "a key that is present passes"
else
  fail "a key that is present was reported missing: $(cat "$WORK/out")"
fi

if run_guard "v0-rust-x86_64-unknown-linux-musl-build-agent"; then
  fail "a key that is absent passed, so a refused write stays invisible"
else
  pass "a key that is absent fails the job"
fi

if grep -qF '::error::' "$WORK/out"; then
  pass "the absent key is reported where a workflow log shows it"
else
  fail "the absent key is reported without a workflow annotation"
fi

FAKE_KEYS=""
export FAKE_KEYS
if run_guard "anything"; then
  fail "an empty cache list passed"
else
  pass "an empty cache list fails"
fi

FAKE_KEYS="$(printf 'v0-rust-something\n')"
export FAKE_KEYS
export FAKE_GH_FAILS=1
if run_guard "v0-rust-something"; then
  fail "an unreachable cache API passed, so the guard answers yes when it cannot ask"
else
  pass "an unreachable cache API fails"
fi
unset FAKE_GH_FAILS

if run_guard; then
  fail "the guard passed with no key named"
else
  pass "the guard refuses to run with no key named"
fi

unset GITHUB_REPOSITORY
FAKE_KEYS="$(printf 'v0-rust-something\n')"
export FAKE_KEYS
if run_guard "v0-rust-something"; then
  fail "the guard passed without knowing which repository to ask"
else
  pass "the guard refuses to run without a repository"
fi

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
