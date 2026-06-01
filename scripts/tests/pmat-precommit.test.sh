#!/usr/bin/env bash
# Tests for scripts/pmat-precommit.sh (ADR-019 precommit TDG gate).
# Plain bash; no bats. Stubs the pmat binary and uses throwaway git repos so
# the test is fast and deterministic (no real grading). Run:
#   ./scripts/tests/pmat-precommit.test.sh
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WRAPPER="$SCRIPT_DIR/../pmat-precommit.sh"
[ -f "$WRAPPER" ] || { echo "FAIL: $WRAPPER not found" >&2; exit 1; }

PASS=0
FAIL=0
FAILURES=()
pass() { PASS=$((PASS + 1)); printf '  ok   %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAILURES+=("$1"); printf '  FAIL %s\n' "$1" >&2; }

assert_eq() {
  local name="$1" want="$2" got="$3"
  if [ "$want" = "$got" ]; then pass "$name"; else fail "$name (want=[$want] got=[$got])"; fi
}
# assert_ok / assert_fail run a shell FUNCTION (not external cmd) and check exit.
assert_ok()   { local n="$1"; shift; if "$@" >/dev/null 2>&1; then pass "$n"; else fail "$n (expected 0, got $?)"; fi; }
assert_fail() { local n="$1"; shift; if "$@" >/dev/null 2>&1; then fail "$n (expected non-zero)"; else pass "$n"; fi; }

# --- Stub pmat ----------------------------------------------------------------
# Reads STUB_VERSION (for --version) and STUB_FAIL_SUBSTR (a check-quality on a
# -p path containing this substring exits 3 with a violation JSON) from env at
# call time, so cases can vary behavior without rewriting the stub.
STUB_DIR="$(mktemp -d)"
cat > "$STUB_DIR/pmat" <<'STUB'
#!/usr/bin/env bash
case "${1:-}" in
  --version) echo "pmat ${STUB_VERSION:-3.17.0}"; exit 0 ;;
esac
path=""
while [ $# -gt 0 ]; do [ "$1" = "-p" ] && path="${2:-}"; shift; done
echo "🔍 Checking quality thresholds..."   # banner, like the real tool
if [ -n "${STUB_FAIL_SUBSTR:-}" ] && printf '%s' "$path" | grep -q "$STUB_FAIL_SUBSTR"; then
  printf '{"passed":false,"violations":[{"path":"%s","new_grade":"C","new_score":64.8}],"message":"fail"}\n' "$path"
  exit 3
fi
echo '{"passed":true,"violations":[],"message":"ok"}'
exit 0
STUB
chmod +x "$STUB_DIR/pmat"

# --- Temp git repo helpers (mirror tdd-check.test.sh) -------------------------
make_repo() {
  REPO="$(mktemp -d)"
  cd "$REPO" || exit 1
  git init --quiet --initial-branch=dev
  git config user.email "test@example.com"
  git config user.name "Test"
  echo base > base.txt
  git add base.txt
  git commit --quiet -m init
  git checkout --quiet -b feat/test
}
cleanup_repo() { if [ -n "${REPO:-}" ]; then rm -rf "$REPO"; REPO=""; fi; return 0; }
cleanup_all()  { cleanup_repo; rm -rf "$STUB_DIR"; return 0; }
trap 'cleanup_all' EXIT

# --- Source the wrapper with the stub wired in --------------------------------
export PMAT_BIN="$STUB_DIR/pmat"
export PMAT_PIN=""                 # disabled by default; re-enabled per case
export PMAT_BASELINE_REF="origin/dev"
# shellcheck source=../pmat-precommit.sh disable=SC1091
source "$WRAPPER"

echo "version pin (pmat_version_ok):"
PMAT_BIN="$STUB_DIR/pmat"
PMAT_PIN="3.17.0"; export STUB_VERSION="3.17.0"
assert_ok   "matching version passes"        pmat_version_ok
PMAT_PIN="9.9.9"
assert_fail "mismatched version rejected"     pmat_version_ok
PMAT_PIN=""
assert_ok   "empty pin disables the check"    pmat_version_ok
PMAT_PIN="3.17.0"; PMAT_BIN="/nonexistent/pmat"
assert_fail "missing binary rejected"         pmat_version_ok
PMAT_BIN="$STUB_DIR/pmat"

echo
echo "pmat_changed_code_files (tests included, generated/non-code excluded):"
make_repo
echo x > good.go
echo x > helper_test.go
echo x > notes.md
echo x > thing_gen.go
echo x > script.sh
got="$(PMAT_BASELINE_REF=origin/dev; pmat_changed_code_files | tr '\n' ' ' | sed 's/ *$//')"
assert_eq "only code files (incl _test.go), excl .md/_gen.go/.sh" "good.go helper_test.go" "$got"
cleanup_repo

echo
echo "pmat_precommit_main (end to end with stub):"
PMAT_PIN="3.17.0"; export STUB_VERSION="3.17.0"

# 1. Changed code all passes.
make_repo
echo x > good.go
unset STUB_FAIL_SUBSTR
assert_ok "clean changed code passes" pmat_precommit_main
cleanup_repo

# 2. A changed file below the floor fails the gate.
make_repo
echo x > good.go
echo x > bad.go
export STUB_FAIL_SUBSTR="bad"
assert_fail "below-floor changed file fails" pmat_precommit_main
unset STUB_FAIL_SUBSTR
cleanup_repo

# 3. No changed code files → passes trivially (docs/CI-only commit).
make_repo
echo x > notes.md
echo x > workflow.yml
assert_ok "no changed code files passes" pmat_precommit_main
cleanup_repo

# 4. Wrong pmat version → prerequisite failure (exit 2, treated as non-zero).
make_repo
echo x > good.go
PMAT_PIN="9.9.9"
assert_fail "wrong pmat version blocks the gate" pmat_precommit_main
PMAT_PIN="3.17.0"
cleanup_repo

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
