#!/usr/bin/env bash
# Tests the precommit TDG gate against a stubbed pmat binary and throwaway git repos.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WRAPPER="$SCRIPT_DIR/../pmat-precommit.sh"
[ -f "$WRAPPER" ] || {
  echo "FAIL: $WRAPPER not found" >&2
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
assert_ok() {
  local n="$1"
  shift
  if "$@" >/dev/null 2>&1; then pass "$n"; else fail "$n (expected 0, got $?)"; fi
}
assert_fail() {
  local n="$1"
  shift
  if "$@" >/dev/null 2>&1; then fail "$n (expected non-zero)"; else pass "$n"; fi
}

# The stub reads STUB_VERSION and STUB_FAIL_SUBSTR at call time; a check-quality on a -p path
# containing STUB_FAIL_SUBSTR exits 3 with a violation JSON.
STUB_DIR="$(mktemp -d)"
cat >"$STUB_DIR/pmat" <<'STUB'
#!/usr/bin/env bash
case "${1:-}" in
  --version) echo "pmat ${STUB_VERSION:-3.17.0}"; exit 0 ;;
esac
path=""
while [ $# -gt 0 ]; do [ "$1" = "-p" ] && path="${2:-}"; shift; done
echo "🔍 Checking quality thresholds..."   # banner, like the real tool
if [ -n "${STUB_FAIL_SUBSTR:-}" ] && grep -q "$STUB_FAIL_SUBSTR" <<<"$path"; then
  printf '{"passed":false,"violations":[{"path":"%s","new_grade":"C","new_score":64.8}],"message":"fail"}\n' "$path"
  exit 3
fi
echo '{"passed":true,"violations":[],"message":"ok"}'
exit 0
STUB
chmod +x "$STUB_DIR/pmat"

# The gofmt stub strips trailing blank lines from a file argument or from stdin.
cat >"$STUB_DIR/gofmt" <<'GOFMT'
#!/usr/bin/env bash
if [ -n "${1:-}" ] && [ -f "${1:-}" ]; then cat -- "$1"; else cat; fi \
  | awk '{l[NR]=$0} END{last=NR; while(last>0 && l[last]==""){last--}; for(i=1;i<=last;i++) print l[i]}'
GOFMT
chmod +x "$STUB_DIR/gofmt"

make_repo() {
  REPO="$(mktemp -d)"
  cd "$REPO" || exit 1
  git init --quiet --initial-branch=dev
  git config user.email "test@example.com"
  git config user.name "Test"
  echo base >base.txt
  git add base.txt
  git commit --quiet -m init
  git checkout --quiet -b feat/test
}
# Invoked through cleanup_all and the EXIT trap.
# shellcheck disable=SC2329
cleanup_repo() {
  if [ -n "${REPO:-}" ]; then
    rm -rf "$REPO"
    REPO=""
  fi
  return 0
}
# Invoked through the EXIT trap.
# shellcheck disable=SC2329
cleanup_all() {
  cleanup_repo
  rm -rf "$STUB_DIR"
  return 0
}
trap 'cleanup_all' EXIT

export PMAT_BIN="$STUB_DIR/pmat"
export PMAT_PIN="" # disabled by default; re-enabled per case
export PMAT_BASELINE_REF="origin/dev"
# shellcheck source=../pmat-precommit.sh disable=SC1091
source "$WRAPPER"

echo "version pin (pmat_version_ok):"
PMAT_BIN="$STUB_DIR/pmat"
PMAT_PIN="3.17.0"
export STUB_VERSION="3.17.0"
assert_ok "matching version passes" pmat_version_ok
PMAT_PIN="9.9.9"
assert_fail "mismatched version rejected" pmat_version_ok
PMAT_PIN=""
assert_ok "empty pin disables the check" pmat_version_ok
PMAT_PIN="3.17.0"
PMAT_BIN="/nonexistent/pmat"
assert_fail "missing binary rejected" pmat_version_ok
PMAT_BIN="$STUB_DIR/pmat"

echo
echo "pmat_changed_code_files (tests included, generated/non-code excluded):"
make_repo
echo x >good.go
echo x >helper_test.go
echo x >notes.md
echo x >thing_gen.go
echo x >script.sh
got="$(
  PMAT_BASELINE_REF=origin/dev
  pmat_changed_code_files | tr '\n' ' ' | sed 's/ *$//'
)"
assert_eq "only code files (incl _test.go), excl .md/_gen.go/.sh" "good.go helper_test.go" "$got"
cleanup_repo

echo
echo "pmat_changed_code_files (gofmt-only Go *test* files excluded; ADR-019):"
export GOFMT_BIN="$STUB_DIR/gofmt"
# The baseline holds three files with trailing blank lines; the branch strips them.
REPO="$(mktemp -d)"
cd "$REPO" || exit 1
git init --quiet --initial-branch=dev
git config user.email "test@example.com"
git config user.name "Test"
printf 'package x\n\nfunc TestA() {}\n\n\n' >fmtonly_test.go # test, fmt-only
printf 'package x\n\nfunc TestB() {}\n' >realchange_test.go  # test, real change
printf 'package x\n\nvar A = 1\n\n\n' >src_fmtonly.go        # source, fmt-only
git add -A
git commit --quiet -m init
git checkout --quiet -b feat/test
printf 'package x\n\nfunc TestA() {}\n' >fmtonly_test.go           # only trailing blanks removed
printf 'package x\n\nfunc TestB() { _ = 1 }\n' >realchange_test.go # body changed
printf 'package x\n\nvar A = 1\n' >src_fmtonly.go                  # only trailing blanks removed
got="$(pmat_changed_code_files | sort | tr '\n' ' ' | sed 's/ *$//')"
assert_eq "drops gofmt-only test, keeps real-change test + fmt-only source" \
  "realchange_test.go src_fmtonly.go" "$got"
cleanup_repo
unset GOFMT_BIN

echo
echo "pmat_precommit_main (end to end with stub):"
PMAT_PIN="3.17.0"
export STUB_VERSION="3.17.0"

make_repo
echo x >good.go
unset STUB_FAIL_SUBSTR
assert_ok "clean changed code passes" pmat_precommit_main
cleanup_repo

make_repo
echo x >good.go
echo x >bad.go
export STUB_FAIL_SUBSTR="bad"
assert_fail "below-floor changed file fails" pmat_precommit_main
unset STUB_FAIL_SUBSTR
cleanup_repo

make_repo
echo x >notes.md
echo x >workflow.yml
assert_ok "no changed code files passes" pmat_precommit_main
cleanup_repo

make_repo
echo x >good.go
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
