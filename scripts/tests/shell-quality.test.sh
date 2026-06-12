#!/usr/bin/env bash
# Tests for the canonical shell-quality runner.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
RUNNER="$REPO_ROOT/scripts/shell-quality.sh"

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

echo "shell-quality:"

if [ -x "$RUNNER" ]; then
  pass "runner exists and is executable"
else
  fail "runner exists and is executable"
fi

if [ -x "$RUNNER" ]; then
  REPO="$TMP_DIR/repo"
  BIN="$TMP_DIR/bin"
  TRACE="$TMP_DIR/trace"
  mkdir -p "$REPO/scripts/tests" "$REPO/target" "$BIN"

  cat >"$BIN/shellcheck" <<'EOF'
#!/usr/bin/env bash
if [ "${1:-}" = "--version" ]; then
  printf '%s\n' 'ShellCheck - shell script analysis tool' 'version: 0.11.0'
  exit 0
fi
printf 'shellcheck' >>"$TRACE"
printf ' %s' "$@" >>"$TRACE"
printf '\n' >>"$TRACE"
EOF
  cat >"$BIN/shfmt" <<'EOF'
#!/usr/bin/env bash
if [ "${1:-}" = "--version" ]; then
  printf '%s\n' 'v3.13.1'
  exit 0
fi
printf 'shfmt' >>"$TRACE"
printf ' %s' "$@" >>"$TRACE"
printf '\n' >>"$TRACE"
for arg in "$@"; do
  if [ -f "$arg" ] && grep -qF BAD_FORMAT "$arg"; then
    exit 1
  fi
done
EOF
  chmod +x "$BIN/shellcheck" "$BIN/shfmt"

  cat >"$REPO/clean.sh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' clean
EOF
  cat >"$REPO/other.sh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' other
EOF
  cat >"$REPO/target/ignored.sh" <<'EOF'
#!/usr/bin/env bash
BAD_FORMAT
EOF
  printf 'target/\n' >"$REPO/.gitignore"

  git -C "$REPO" init -q
  git -C "$REPO" config user.name test
  git -C "$REPO" config user.email test@example.com
  git -C "$REPO" add .gitignore clean.sh other.sh
  git -C "$REPO" commit -qm baseline

  : >"$TRACE"
  if TRACE="$TRACE" PATH="$BIN:$PATH" SHELL_QUALITY_ROOT="$REPO" "$RUNNER" check \
    && grep -qF "$REPO/clean.sh" "$TRACE" \
    && ! grep -qF "$REPO/target/ignored.sh" "$TRACE"; then
    pass "check enumerates tracked scripts only"
  else
    fail "check enumerates tracked scripts only"
  fi

  cat >>"$REPO/other.sh" <<'EOF'

printf '%s\n' changed
EOF
  : >"$TRACE"
  if TRACE="$TRACE" PATH="$BIN:$PATH" SHELL_QUALITY_ROOT="$REPO" "$RUNNER" changed HEAD \
    && grep -qF "$REPO/other.sh" "$TRACE" \
    && ! grep -qF "$REPO/clean.sh" "$TRACE"; then
    pass "changed validates only diffed tracked scripts"
  else
    fail "changed validates only diffed tracked scripts"
  fi

  printf '\nBAD_FORMAT\n' >>"$REPO/other.sh"
  if TRACE="$TRACE" PATH="$BIN:$PATH" SHELL_QUALITY_ROOT="$REPO" "$RUNNER" changed HEAD >/dev/null 2>&1; then
    fail "format drift exits non-zero"
  else
    pass "format drift exits non-zero"
  fi
fi

if grep -qF 'RUST_LOG=off NO_COLOR=1 cargo modules structure' "$REPO_ROOT/scripts/precommit-gauntlet.sh"; then
  pass "cargo module snapshot disables ambient tracing filters"
else
  fail "cargo module snapshot disables ambient tracing filters"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ]
