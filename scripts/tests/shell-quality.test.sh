#!/usr/bin/env bash
# Tests for the canonical shell-quality runner.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
RUNNER="$REPO_ROOT/scripts/shell-quality.sh"
# The stub tools answer with the pinned versions, which the runner requires.
# shellcheck source=../lib/tool-versions.sh
. "$REPO_ROOT/scripts/lib/tool-versions.sh"
export STUB_SHELLCHECK_VERSION="$TOOL_VERSION_SHELLCHECK" STUB_SHFMT_VERSION="$TOOL_VERSION_SHFMT"

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
  mkdir -p "$REPO/.claude" "$REPO/scripts/tests" "$REPO/target" "$BIN"
  : >"$REPO/.claude/shell-policy.exceptions"

  cat >"$BIN/shellcheck" <<'EOF'
#!/usr/bin/env bash
if [ "${1:-}" = "--version" ]; then
  printf '%s\n' 'ShellCheck - shell script analysis tool' "version: $STUB_SHELLCHECK_VERSION"
  exit 0
fi
printf 'shellcheck' >>"$TRACE"
printf ' %s' "$@" >>"$TRACE"
printf '\n' >>"$TRACE"
EOF
  cat >"$BIN/shfmt" <<'EOF'
#!/usr/bin/env bash
if [ "${1:-}" = "--version" ]; then
  printf '%s\n' "v$STUB_SHFMT_VERSION"
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
  git -C "$REPO" add .claude/shell-policy.exceptions .gitignore clean.sh other.sh
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

  # A linter off its pin judges the scripts by rules CI does not apply.
  if out="$(STUB_SHELLCHECK_VERSION=0.0.1 TRACE="$TRACE" PATH="$BIN:$PATH" SHELL_QUALITY_ROOT="$REPO" "$RUNNER" check 2>&1)"; then
    fail "a ShellCheck off its pin is refused"
  elif grep -qF 'shellcheck is 0.0.1' <<<"$out"; then
    pass "a ShellCheck off its pin is refused, naming what it found"
  else
    fail "a ShellCheck off its pin is refused, naming what it found (out=[$out])"
  fi
fi

if grep -qF 'RUST_LOG=off NO_COLOR=1 cargo modules structure' "$REPO_ROOT/scripts/precommit-gauntlet.sh"; then
  pass "cargo module snapshot disables ambient tracing filters"
else
  fail "cargo module snapshot disables ambient tracing filters"
fi

# The gauntlet executes each test file, so the runner refuses one without its executable bit.
demo_dir="$TMP_DIR/execbit"
mkdir -p "$demo_dir"
cat >"$demo_dir/sample.test.sh" <<'DEMO'
#!/usr/bin/env bash
echo ran
DEMO
chmod -x "$demo_dir/sample.test.sh"

if bash "$demo_dir/sample.test.sh" >/dev/null 2>&1; then
  pass "a test file with no executable bit still runs when handed to bash"
else
  fail "the masking direction no longer reproduces"
fi
if ! "$demo_dir/sample.test.sh" >/dev/null 2>&1; then
  pass "and is refused when executed, which is how the gate runs it"
else
  fail "a non-executable file was executed"
fi

EXEC_REPO="$TMP_DIR/execrepo"
mkdir -p "$EXEC_REPO/scripts/tests"
git -C "$EXEC_REPO" init -q
cp "$demo_dir/sample.test.sh" "$EXEC_REPO/scripts/tests/sample.test.sh"
if out="$(SHELL_QUALITY_ROOT="$EXEC_REPO" "$RUNNER" test 2>&1)"; then
  fail "the runner hands test files to bash, so a missing executable bit is invisible until a commit attempt"
elif grep -qF 'not executable: scripts/tests/sample.test.sh' <<<"$out"; then
  pass "the runner refuses a test file the gate could not execute"
else
  fail "the runner refuses a test file the gate could not execute (out=[$out])"
fi

# Each test is handed summary, output, environment and path files of its own, and a write fails.
STEP_REPO="$TMP_DIR/steprepo"
mkdir -p "$STEP_REPO/scripts/tests"
git -C "$STEP_REPO" init -q
cat >"$STEP_REPO/scripts/tests/quiet.test.sh" <<'EOF'
#!/usr/bin/env bash
echo "quiet"
EOF
cat >"$STEP_REPO/scripts/tests/chatty.test.sh" <<'EOF'
#!/usr/bin/env bash
echo "k6 scenario api-baseline crossed one of its own thresholds" >>"$GITHUB_STEP_SUMMARY"
EOF
chmod +x "$STEP_REPO/scripts/tests/quiet.test.sh" "$STEP_REPO/scripts/tests/chatty.test.sh"
JOB_SUMMARY="$TMP_DIR/job-summary.md"
: >"$JOB_SUMMARY"
if out="$(GITHUB_STEP_SUMMARY="$JOB_SUMMARY" SHELL_QUALITY_ROOT="$STEP_REPO" "$RUNNER" test 2>&1)"; then
  fail "a test that writes into the job's step summary fails the run"
elif grep -qF 'chatty.test.sh' <<<"$out" && grep -qF 'GITHUB_STEP_SUMMARY' <<<"$out"; then
  pass "a test that writes into the job's step summary fails the run, naming the test and the file"
else
  fail "a test that writes into the job's step summary fails the run, naming the test and the file (out=[$out])"
fi
if [ -s "$JOB_SUMMARY" ]; then
  fail "the job's own step summary is left as it was (got [$(cat "$JOB_SUMMARY")])"
else
  pass "the job's own step summary is left as it was"
fi
rm -f "$STEP_REPO/scripts/tests/chatty.test.sh"
if GITHUB_STEP_SUMMARY="$JOB_SUMMARY" SHELL_QUALITY_ROOT="$STEP_REPO" "$RUNNER" test >/dev/null 2>&1; then
  pass "tests that write nothing into the job pass"
else
  fail "tests that write nothing into the job pass"
fi

if grep -qF 'scripts/shell-quality.sh test' "$REPO_ROOT/scripts/precommit-gauntlet.sh"; then
  pass "the gauntlet's shell-tests step is this runner"
else
  fail "the gauntlet's shell-tests step is this runner"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ]
