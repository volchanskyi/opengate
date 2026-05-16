#!/usr/bin/env bash
# Tests for .claude/hooks/*.sh. Plain bash; no bats dependency.
# Run: ./scripts/tests/hooks.test.sh
#
# Each test sets up a fresh temp git repo (so branch state is isolated),
# feeds the hook a JSON envelope on stdin, and asserts exit code +
# optionally stderr contents.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
HOOKS_DIR="$PROJECT_ROOT/.claude/hooks"

PASS=0
FAIL=0
FAILURES=()

pass() { PASS=$((PASS + 1)); printf '  ok   %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAILURES+=("$1"); printf '  FAIL %s\n' "$1" >&2; }

# Build PreToolUse JSON envelope for a tool call.
# Args: tool_name, input_json_string
build_envelope() {
  local tool="$1" input_json="$2"
  python3 - "$tool" "$input_json" <<'PYEOF'
import json, sys
tool, input_json = sys.argv[1], sys.argv[2]
try:
    tool_input = json.loads(input_json)
except Exception:
    tool_input = {}
print(json.dumps({
    "session_id": "test-session",
    "cwd": ".",
    "hook_event_name": "PreToolUse",
    "tool_name": tool,
    "tool_input": tool_input,
}))
PYEOF
}

# Run a hook with given envelope on stdin. Captures exit, stderr.
# Args: hook_relpath, envelope_json. Sets HOOK_EXIT, HOOK_STDERR.
run_hook() {
  local hook="$1" envelope="$2"
  local stderr_file
  stderr_file="$(mktemp)"
  HOOK_EXIT=0
  if printf '%s' "$envelope" | "$HOOKS_DIR/$hook" >/dev/null 2>"$stderr_file"; then
    HOOK_EXIT=0
  else
    HOOK_EXIT=$?
  fi
  HOOK_STDERR="$(cat "$stderr_file")"
  rm -f "$stderr_file"
}

# Set up a temp git repo on a feature branch off of "dev".
# Sets REPO; caller must call cleanup_repo.
REPO=""
make_repo() {
  REPO="$(mktemp -d)"
  cd "$REPO"
  git init --quiet --initial-branch=dev
  git config user.email "test@example.com"
  git config user.name "Test User"
  echo "base" > base.txt
  git add base.txt
  git commit --quiet -m "init"
  git checkout --quiet -b feat/test
}

cleanup_repo() {
  if [ -n "${REPO:-}" ]; then
    cd "$PROJECT_ROOT" 2>/dev/null || cd /tmp
    rm -rf "$REPO"
    REPO=""
  fi
  return 0
}
trap 'cleanup_repo' EXIT

# Assertion helpers.
assert_exit() {
  local name="$1" expected="$2"
  if [ "$HOOK_EXIT" = "$expected" ]; then pass "$name (exit $expected)"
  else fail "$name (expected exit $expected, got $HOOK_EXIT; stderr: $(printf '%s' "$HOOK_STDERR" | head -1))"
  fi
}
assert_stderr_contains() {
  local name="$1" needle="$2"
  if printf '%s' "$HOOK_STDERR" | grep -qF "$needle"; then pass "$name (stderr ~ '$needle')"
  else fail "$name (stderr missing '$needle'; got: $(printf '%s' "$HOOK_STDERR" | head -1))"
  fi
}

# -------------------------------------------------------------------
# pretooluse-tdd-gate.sh
# -------------------------------------------------------------------
echo
echo "## pretooluse-tdd-gate.sh"

# 1. Non-Write/Edit tool: noop allow.
make_repo
envelope="$(build_envelope Bash '{"command":"ls"}')"
run_hook pretooluse-tdd-gate.sh "$envelope"
assert_exit "Bash tool: allows (not Write/Edit)" 0
cleanup_repo

# 2. Doc-only path: allow on fresh branch.
make_repo
envelope="$(build_envelope Edit '{"file_path":"docs/Home.md","old_string":"a","new_string":"b"}')"
run_hook pretooluse-tdd-gate.sh "$envelope"
assert_exit "Edit docs/Home.md: allow (not source)" 0
cleanup_repo

# 3. Generated file: allow on fresh branch.
make_repo
envelope="$(build_envelope Edit '{"file_path":"server/internal/api/openapi_gen.go","old_string":"a","new_string":"b"}')"
run_hook pretooluse-tdd-gate.sh "$envelope"
assert_exit "Edit openapi_gen.go: allow (generated)" 0
cleanup_repo

# 4. Source path on fresh branch with no test: BLOCK.
make_repo
envelope="$(build_envelope Edit '{"file_path":"server/internal/api/handlers.go","old_string":"a","new_string":"b"}')"
run_hook pretooluse-tdd-gate.sh "$envelope"
assert_exit "Edit handlers.go on fresh branch: BLOCK" 2
assert_stderr_contains "Edit handlers.go: stderr cites TDD" "TDD"
cleanup_repo

# 5. Source path with untracked test on branch: allow.
make_repo
mkdir -p server/internal/api
echo "package api" > server/internal/api/foo_test.go
envelope="$(build_envelope Edit '{"file_path":"server/internal/api/handlers.go","old_string":"a","new_string":"b"}')"
run_hook pretooluse-tdd-gate.sh "$envelope"
assert_exit "Edit handlers.go with untracked _test.go: allow" 0
cleanup_repo

# 6. Source path with committed test on branch: allow.
make_repo
mkdir -p server/internal/api
echo "package api" > server/internal/api/foo_test.go
git add server/internal/api/foo_test.go
git commit --quiet -m "add test"
envelope="$(build_envelope Edit '{"file_path":"server/internal/api/handlers.go","old_string":"a","new_string":"b"}')"
run_hook pretooluse-tdd-gate.sh "$envelope"
assert_exit "Edit handlers.go with committed test: allow" 0
cleanup_repo

# 7. Write (new file) on fresh branch with no test: BLOCK.
make_repo
envelope="$(build_envelope Write '{"file_path":"server/internal/api/new.go","content":"package api"}')"
run_hook pretooluse-tdd-gate.sh "$envelope"
assert_exit "Write new.go on fresh branch: BLOCK" 2
cleanup_repo

# 8. OPENGATE_HOOK_BYPASS env var is ignored — still blocks.
make_repo
envelope="$(build_envelope Edit '{"file_path":"server/internal/api/handlers.go","old_string":"a","new_string":"b"}')"
HOOK_EXIT=0
HOOK_STDERR=""
stderr_file="$(mktemp)"
if printf '%s' "$envelope" | OPENGATE_HOOK_BYPASS=tdd-test-first "$HOOKS_DIR/pretooluse-tdd-gate.sh" >/dev/null 2>"$stderr_file"; then HOOK_EXIT=0; else HOOK_EXIT=$?; fi
HOOK_STDERR="$(cat "$stderr_file")"; rm -f "$stderr_file"
assert_exit "OPENGATE_HOOK_BYPASS ignored: still BLOCK" 2
cleanup_repo

# -------------------------------------------------------------------
# pretooluse-bash-source-write-guard.sh
# -------------------------------------------------------------------
echo
echo "## pretooluse-bash-source-write-guard.sh"

# 1. Non-write Bash: allow.
make_repo
envelope="$(build_envelope Bash '{"command":"ls -la"}')"
run_hook pretooluse-bash-source-write-guard.sh "$envelope"
assert_exit "Bash 'ls -la': allow" 0
cleanup_repo

# 2. Redirect to source on fresh branch: BLOCK.
make_repo
envelope="$(build_envelope Bash '{"command":"echo package > server/internal/foo/new.go"}')"
run_hook pretooluse-bash-source-write-guard.sh "$envelope"
assert_exit "Bash 'echo > new.go' on fresh branch: BLOCK" 2
cleanup_repo

# 3. sed -i on source on fresh branch: BLOCK.
make_repo
envelope="$(build_envelope Bash '{"command":"sed -i s/old/new/ server/internal/api/handlers.go"}')"
run_hook pretooluse-bash-source-write-guard.sh "$envelope"
assert_exit "Bash 'sed -i ... handlers.go' on fresh branch: BLOCK" 2
cleanup_repo

# 4. Redirect to /tmp: allow (path outside repo).
make_repo
envelope="$(build_envelope Bash '{"command":"echo ok > /tmp/scratch.txt"}')"
run_hook pretooluse-bash-source-write-guard.sh "$envelope"
assert_exit "Bash 'echo > /tmp/scratch.txt': allow" 0
cleanup_repo

# 5. Redirect to doc: allow (not source).
make_repo
envelope="$(build_envelope Bash '{"command":"echo hi > docs/notes.md"}')"
run_hook pretooluse-bash-source-write-guard.sh "$envelope"
assert_exit "Bash 'echo > docs/notes.md': allow (not source)" 0
cleanup_repo

# 6. Redirect to source after test exists: allow.
make_repo
mkdir -p server/internal/api
echo "package api" > server/internal/api/foo_test.go
envelope="$(build_envelope Bash '{"command":"echo package > server/internal/api/new.go"}')"
run_hook pretooluse-bash-source-write-guard.sh "$envelope"
assert_exit "Bash 'echo > new.go' with test present: allow" 0
cleanup_repo

# -------------------------------------------------------------------
# pretooluse-git-commit-guard.sh
# -------------------------------------------------------------------
echo
echo "## pretooluse-git-commit-guard.sh"

# Write a stub gauntlet script that exits with the requested code. The
# commit-guard hook invokes `$(project_root)/scripts/precommit-gauntlet.sh`,
# and in tests `project_root` resolves to the temp repo, so this stub is
# what the hook actually runs (no slow real gauntlet inside tests).
stub_gauntlet() {
  local exit_code="${1:-0}"
  mkdir -p scripts
  cat > scripts/precommit-gauntlet.sh <<EOF
#!/usr/bin/env bash
exit $exit_code
EOF
  chmod +x scripts/precommit-gauntlet.sh
}

setup_passing_commit_repo() {
  make_repo
  git config user.name "Ivan Volchanskyi"
  git config user.email "ivan.volchanskyi@gmail.com"
  # Branch tests so TDD backup check passes.
  mkdir -p server/internal/api
  echo "package api" > server/internal/api/foo_test.go
  git add server/internal/api/foo_test.go
  git commit --quiet -m "add test"
  # Stub a passing gauntlet so the commit-guard's gate runs in <100ms.
  stub_gauntlet 0
}

# 1. Non-commit Bash: noop allow.
make_repo
envelope="$(build_envelope Bash '{"command":"ls"}')"
run_hook pretooluse-git-commit-guard.sh "$envelope"
assert_exit "Bash 'ls': allow" 0
cleanup_repo

# 2. Commit with Co-Authored-By trailer: BLOCK.
setup_passing_commit_repo
envelope="$(build_envelope Bash '{"command":"git commit -m \"feat: x\n\nCo-Authored-By: Bot <bot@x.com>\""}')"
run_hook pretooluse-git-commit-guard.sh "$envelope"
assert_exit "git commit w/ Co-Authored-By: BLOCK" 2
assert_stderr_contains "Co-Authored-By: stderr cites it" "Co-Authored-By"
cleanup_repo

# 3. Commit with --no-verify: BLOCK.
setup_passing_commit_repo
envelope="$(build_envelope Bash '{"command":"git commit --no-verify -m feat"}')"
run_hook pretooluse-git-commit-guard.sh "$envelope"
assert_exit "git commit --no-verify: BLOCK" 2
cleanup_repo

# 4. Commit with wrong identity: BLOCK.
make_repo
git config user.name "Wrong Person"
git config user.email "wrong@example.com"
stub_gauntlet 0
envelope="$(build_envelope Bash '{"command":"git commit -m feat"}')"
run_hook pretooluse-git-commit-guard.sh "$envelope"
assert_exit "git commit wrong identity: BLOCK" 2
assert_stderr_contains "wrong identity: stderr cites Ivan" "Ivan"
cleanup_repo

# 5. Commit on main branch: BLOCK.
make_repo
git config user.name "Ivan Volchanskyi"
git config user.email "ivan.volchanskyi@gmail.com"
git checkout --quiet -b main
stub_gauntlet 0
envelope="$(build_envelope Bash '{"command":"git commit -m feat"}')"
run_hook pretooluse-git-commit-guard.sh "$envelope"
assert_exit "git commit on main: BLOCK" 2
cleanup_repo

# 6. Commit without scripts/precommit-gauntlet.sh: BLOCK (hook needs the script to enforce).
make_repo
git config user.name "Ivan Volchanskyi"
git config user.email "ivan.volchanskyi@gmail.com"
mkdir -p server/internal/api
echo "package api" > server/internal/api/foo_test.go
git add server/internal/api/foo_test.go
git commit --quiet -m "add test"
envelope="$(build_envelope Bash '{"command":"git commit -m feat"}')"
run_hook pretooluse-git-commit-guard.sh "$envelope"
assert_exit "git commit no gauntlet script: BLOCK" 2
assert_stderr_contains "no gauntlet: stderr cites precommit-gauntlet" "precommit-gauntlet"
cleanup_repo

# 7. Gauntlet exits 1 (check failed): BLOCK.
setup_passing_commit_repo
stub_gauntlet 1
envelope="$(build_envelope Bash '{"command":"git commit -m feat"}')"
run_hook pretooluse-git-commit-guard.sh "$envelope"
assert_exit "git commit gauntlet exit 1: BLOCK" 2
assert_stderr_contains "gauntlet fail: stderr cites failed" "failed"
cleanup_repo

# 7b. Gauntlet exits 2 (prerequisite missing): BLOCK with prereq message.
setup_passing_commit_repo
stub_gauntlet 2
envelope="$(build_envelope Bash '{"command":"git commit -m feat"}')"
run_hook pretooluse-git-commit-guard.sh "$envelope"
assert_exit "git commit gauntlet exit 2 (prereq): BLOCK" 2
assert_stderr_contains "gauntlet prereq: stderr cites prerequisite" "prerequisite"
cleanup_repo

# 8. Source-only branch (no test): TDD backup BLOCK even if other checks pass.
make_repo
git config user.name "Ivan Volchanskyi"
git config user.email "ivan.volchanskyi@gmail.com"
mkdir -p server/internal/api
echo "package api" > server/internal/api/source.go
git add server/internal/api/source.go
git commit --quiet -m "src only"
stub_gauntlet 0
envelope="$(build_envelope Bash '{"command":"git commit -m feat"}')"
run_hook pretooluse-git-commit-guard.sh "$envelope"
assert_exit "git commit TDD backup (source-only): BLOCK" 2
assert_stderr_contains "TDD backup: stderr cites TDD" "TDD"
cleanup_repo

# 9. Happy path: identity ok, branch ok, marker fresh, test on branch: PASS.
setup_passing_commit_repo
envelope="$(build_envelope Bash '{"command":"git commit -m \"feat: thing\""}')"
run_hook pretooluse-git-commit-guard.sh "$envelope"
assert_exit "git commit happy path: PASS" 0
cleanup_repo

# -------------------------------------------------------------------
# pretooluse-git-push-guard.sh
# -------------------------------------------------------------------
echo
echo "## pretooluse-git-push-guard.sh"

# 1. Non-push Bash: noop allow.
make_repo
envelope="$(build_envelope Bash '{"command":"ls"}')"
run_hook pretooluse-git-push-guard.sh "$envelope"
assert_exit "Bash 'ls': allow" 0
cleanup_repo

# 2. Push to main: BLOCK.
make_repo
envelope="$(build_envelope Bash '{"command":"git push origin main"}')"
run_hook pretooluse-git-push-guard.sh "$envelope"
assert_exit "git push origin main: BLOCK" 2
assert_stderr_contains "push main: stderr cites main" "main"
cleanup_repo

# 3. Force push to main: BLOCK.
make_repo
envelope="$(build_envelope Bash '{"command":"git push --force origin main"}')"
run_hook pretooluse-git-push-guard.sh "$envelope"
assert_exit "git push --force main: BLOCK" 2
cleanup_repo

# 4. Push doc-only branch without refactor marker: allow.
make_repo
echo "# new" > newdoc.md
git add newdoc.md
git commit --quiet -m "docs"
envelope="$(build_envelope Bash '{"command":"git push origin dev"}')"
run_hook pretooluse-git-push-guard.sh "$envelope"
assert_exit "git push doc-only no marker: allow" 0
cleanup_repo

# 5. Push branch with source commits but no refactor marker: BLOCK.
make_repo
mkdir -p server/internal/api
echo "package api" > server/internal/api/foo_test.go
git add server/internal/api/foo_test.go
git commit --quiet -m "add test"
echo "package api" > server/internal/api/handlers.go
git add server/internal/api/handlers.go
git commit --quiet -m "feat"
envelope="$(build_envelope Bash '{"command":"git push origin dev"}')"
run_hook pretooluse-git-push-guard.sh "$envelope"
assert_exit "git push with source commits no marker: BLOCK" 2
assert_stderr_contains "no refactor marker: stderr cites /refactor" "/refactor"
cleanup_repo

# 6. Push branch with source commits and matching refactor marker: PASS.
make_repo
mkdir -p server/internal/api
echo "package api" > server/internal/api/foo_test.go
git add server/internal/api/foo_test.go
git commit --quiet -m "add test"
echo "package api" > server/internal/api/handlers.go
git add server/internal/api/handlers.go
git commit --quiet -m "feat"
mkdir -p .claude/.markers
git rev-parse HEAD > .claude/.markers/refactor.head
envelope="$(build_envelope Bash '{"command":"git push origin dev"}')"
run_hook pretooluse-git-push-guard.sh "$envelope"
assert_exit "git push source commits w/ refactor marker: PASS" 0
cleanup_repo

# -------------------------------------------------------------------
# pretooluse-write-guard.sh
# -------------------------------------------------------------------
echo
echo "## pretooluse-write-guard.sh"

# 1. Non-Write/Edit tool: allow.
make_repo
envelope="$(build_envelope Bash '{"command":"ls"}')"
run_hook pretooluse-write-guard.sh "$envelope"
assert_exit "Bash tool: allow" 0
cleanup_repo

# 2. Write to ~/.claude/plans/: BLOCK.
make_repo
envelope="$(build_envelope Write '{"file_path":"/home/ivan/.claude/plans/foo.md","content":"x"}')"
run_hook pretooluse-write-guard.sh "$envelope"
assert_exit "Write to ~/.claude/plans/: BLOCK" 2
assert_stderr_contains "plans dir: stderr cites project plans" "opengate/.claude/plans"
cleanup_repo

# 3. Edit existing ADR file: BLOCK.
make_repo
mkdir -p docs/adr
echo "# ADR-013" > docs/adr/ADR-013-foo.md
git add docs/adr/ADR-013-foo.md
git commit --quiet -m "adr"
envelope="$(build_envelope Edit '{"file_path":"docs/adr/ADR-013-foo.md","old_string":"a","new_string":"b"}')"
run_hook pretooluse-write-guard.sh "$envelope"
assert_exit "Edit existing ADR: BLOCK" 2
assert_stderr_contains "ADR: stderr cites supersede" "supersede"
cleanup_repo

# 4. Write a NEW ADR file: allow.
make_repo
mkdir -p docs/adr
envelope="$(build_envelope Write '{"file_path":"docs/adr/ADR-099-new.md","content":"# new"}')"
run_hook pretooluse-write-guard.sh "$envelope"
assert_exit "Write new ADR file: allow" 0
cleanup_repo

# 5. Edit adds NOSONAR: BLOCK.
make_repo
envelope="$(build_envelope Edit '{"file_path":"server/internal/api/handlers.go","old_string":"a","new_string":"a // NOSONAR (rationale)"}')"
run_hook pretooluse-write-guard.sh "$envelope"
assert_exit "Edit adds NOSONAR: BLOCK" 2
cleanup_repo

# 6. Edit adds //nolint: BLOCK.
make_repo
envelope="$(build_envelope Edit '{"file_path":"server/internal/api/handlers.go","old_string":"a","new_string":"a //nolint:gosec"}')"
run_hook pretooluse-write-guard.sh "$envelope"
assert_exit "Edit adds //nolint: BLOCK" 2
cleanup_repo

# 7. Edit adds eslint-disable: BLOCK.
make_repo
envelope="$(build_envelope Edit '{"file_path":"web/src/foo.ts","old_string":"a","new_string":"// eslint-disable-next-line foo\nbar"}')"
run_hook pretooluse-write-guard.sh "$envelope"
assert_exit "Edit adds eslint-disable: BLOCK" 2
cleanup_repo

# 8. Write to project plans dir: allow.
make_repo
mkdir -p .claude/plans
envelope="$(build_envelope Write "$(printf '{"file_path":"%s/.claude/plans/foo.md","content":"x"}' "$REPO")")"
run_hook pretooluse-write-guard.sh "$envelope"
assert_exit "Write to project .claude/plans/: allow" 0
cleanup_repo

# -------------------------------------------------------------------
# session-start-context-load.sh
# -------------------------------------------------------------------
echo
echo "## session-start-context-load.sh"

# 1. SessionStart in any repo: outputs additionalContext JSON, exit 0.
make_repo
envelope='{"session_id":"test","cwd":".","hook_event_name":"SessionStart"}'
HOOK_EXIT=0; HOOK_STDOUT=""; HOOK_STDERR=""
stdout_file="$(mktemp)"; stderr_file="$(mktemp)"
if printf '%s' "$envelope" | "$HOOKS_DIR/session-start-context-load.sh" >"$stdout_file" 2>"$stderr_file"; then HOOK_EXIT=0; else HOOK_EXIT=$?; fi
HOOK_STDOUT="$(cat "$stdout_file")"; HOOK_STDERR="$(cat "$stderr_file")"
rm -f "$stdout_file" "$stderr_file"
assert_exit "SessionStart: exit 0" 0
if printf '%s' "$HOOK_STDOUT" | python3 -c 'import json,sys; d=json.load(sys.stdin); assert "hookSpecificOutput" in d and "additionalContext" in d["hookSpecificOutput"], d' 2>/dev/null; then
  pass "SessionStart: emits hookSpecificOutput.additionalContext JSON"
else
  fail "SessionStart: stdout is not valid hookSpecificOutput JSON (got: $(printf '%s' "$HOOK_STDOUT" | head -c 200))"
fi
cleanup_repo

# -------------------------------------------------------------------
# Summary
# -------------------------------------------------------------------
echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
