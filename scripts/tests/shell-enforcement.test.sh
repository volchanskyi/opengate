#!/usr/bin/env bash
# Integration tests for local, hook, and CI Shell enforcement.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
RUNNER="$REPO_ROOT/scripts/shell-quality.sh"
GAUNTLET="$REPO_ROOT/scripts/precommit-gauntlet.sh"
CI_WORKFLOW="$REPO_ROOT/.github/workflows/ci.yml"
SETTINGS="$REPO_ROOT/.claude/settings.json"

PASS=0
FAIL=0
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

pass() {
  PASS=$((PASS + 1))
  printf '  ok   %s\n' "$1"
}

fail() {
  FAIL=$((FAIL + 1))
  printf '  FAIL %s\n' "$1" >&2
}

assert_file_contains() {
  local name="$1"
  local file="$2"
  local expected="$3"
  if grep -qF "$expected" "$file"; then
    pass "$name"
  else
    fail "$name"
  fi
}

elapsed_ms() {
  local start_ns="$1"
  local end_ns="$2"
  printf '%s\n' "$(((end_ns - start_ns) / 1000000))"
}

echo "shell-enforcement:"

assert_file_contains \
  "gauntlet runs the canonical shell check" \
  "$GAUNTLET" \
  'run_check "shell-check" -- make shell-check'
assert_file_contains \
  "CI provisions pinned shell tools" \
  "$CI_WORKFLOW" \
  'scripts/install-shell-tools.sh'
assert_file_contains \
  "CI runs the canonical shell check" \
  "$CI_WORKFLOW" \
  'make shell-check'
assert_file_contains \
  "CI runs shell behavioral tests" \
  "$CI_WORKFLOW" \
  'make shell-test'
assert_file_contains \
  "actionlint receives an explicit pinned ShellCheck path" \
  "$CI_WORKFLOW" \
  '-shellcheck=/home/runner/.local/bin/shellcheck'
assert_file_contains \
  "agent settings register the post-write shell hook" \
  "$SETTINGS" \
  '.claude/hooks/posttooluse-shell-quality.sh'

repo="$TMP_DIR/repo"
mkdir -p "$repo/.claude"
git -C "$TMP_DIR" init -q repo
git -C "$repo" config user.name test
git -C "$repo" config user.email test@example.com
: >"$repo/.claude/shell-policy.exceptions"
cat >"$repo/clean.sh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' clean
EOF
chmod +x "$repo/clean.sh"
git -C "$repo" add .
git -C "$repo" commit -qm baseline

cat >>"$repo/clean.sh" <<'EOF'
printf '%s\n' changed
EOF
start_ns="$(date +%s%N)"
if SHELL_QUALITY_ROOT="$repo" "$RUNNER" changed HEAD >/dev/null; then
  duration_ms="$(elapsed_ms "$start_ns" "$(date +%s%N)")"
  if [ "$duration_ms" -lt 1500 ]; then
    pass "clean changed-file validation completes in ${duration_ms}ms"
  else
    fail "clean changed-file validation exceeds 1500ms (${duration_ms}ms)"
  fi
else
  fail "clean changed-file validation passes"
fi

sed -i 's/set -euo pipefail/set -uo pipefail/' "$repo/clean.sh"
if SHELL_QUALITY_ROOT="$repo" "$RUNNER" changed HEAD >/dev/null 2>&1; then
  fail "changed-file policy violation fails"
else
  pass "changed-file policy violation fails"
fi

git -C "$repo" checkout -q -- clean.sh
cat >"$repo/new.sh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' new
EOF
chmod +x "$repo/new.sh"
if SHELL_QUALITY_ROOT="$repo" "$RUNNER" changed HEAD >/dev/null; then
  pass "changed-file validation includes clean untracked scripts"
else
  fail "changed-file validation includes clean untracked scripts"
fi

sed -i '/set -euo pipefail/d' "$repo/new.sh"
if SHELL_QUALITY_ROOT="$repo" "$RUNNER" changed HEAD >/dev/null 2>&1; then
  fail "untracked policy violation fails"
else
  pass "untracked policy violation fails"
fi

start_ns="$(date +%s%N)"
if "$RUNNER" check >/dev/null; then
  duration_ms="$(elapsed_ms "$start_ns" "$(date +%s%N)")"
  if [ "$duration_ms" -lt 5000 ]; then
    pass "full shell validation completes in ${duration_ms}ms"
  else
    fail "full shell validation exceeds 5000ms (${duration_ms}ms)"
  fi
else
  fail "full shell validation passes"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ]
