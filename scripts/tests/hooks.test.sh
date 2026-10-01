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

pass() {
  PASS=$((PASS + 1))
  printf '  ok   %s\n' "$1"
}
fail() {
  FAIL=$((FAIL + 1))
  FAILURES+=("$1")
  printf '  FAIL %s\n' "$1" >&2
}

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
  echo "base" >base.txt
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
  if [ -n "${REMOTE:-}" ]; then
    rm -rf "$REMOTE"
    REMOTE=""
  fi
  return 0
}
trap 'cleanup_repo' EXIT

# Assertion helpers.
assert_exit() {
  local name="$1" expected="$2"
  if [ "$HOOK_EXIT" = "$expected" ]; then
    pass "$name (exit $expected)"
  else
    fail "$name (expected exit $expected, got $HOOK_EXIT; stderr: $(printf '%s' "$HOOK_STDERR" | head -1))"
  fi
}
assert_stderr_contains() {
  local name="$1" needle="$2"
  if grep -qF "$needle" <<<"$HOOK_STDERR"; then
    pass "$name (stderr ~ '$needle')"
  else
    fail "$name (stderr missing '$needle'; got: $(printf '%s' "$HOOK_STDERR" | head -1))"
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
echo "package api" >server/internal/api/foo_test.go
envelope="$(build_envelope Edit '{"file_path":"server/internal/api/handlers.go","old_string":"a","new_string":"b"}')"
run_hook pretooluse-tdd-gate.sh "$envelope"
assert_exit "Edit handlers.go with untracked _test.go: allow" 0
cleanup_repo

# 6. Source path with committed test on branch: allow.
make_repo
mkdir -p server/internal/api
echo "package api" >server/internal/api/foo_test.go
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
HOOK_STDERR="$(cat "$stderr_file")"
rm -f "$stderr_file"
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
echo "package api" >server/internal/api/foo_test.go
envelope="$(build_envelope Bash '{"command":"echo package > server/internal/api/new.go"}')"
run_hook pretooluse-bash-source-write-guard.sh "$envelope"
assert_exit "Bash 'echo > new.go' with test present: allow" 0
cleanup_repo

# 7. A marker written from the shell: BLOCK, in every shape a file is written —
#    with a test on the branch, so it is the marker rule and not TDD answering.
make_repo
mkdir -p server/internal/api
echo "package api" >server/internal/api/foo_test.go
marker_cmds=(
  'git rev-parse HEAD > .claude/.markers/refactor.head'
  "git rev-parse HEAD >\"\$PWD/.claude/.markers/refactor.head\""
  'echo x | tee .claude/.markers/gauntlet.pass'
  'cp /tmp/proof .claude/.markers/refactor.done'
  'mv /tmp/proof .claude/.markers/refactor.done'
  'touch .claude/.markers/refactor.start'
  'rm -f .claude/.markers/gauntlet.pass'
  'sed -i s/a/b/ .claude/.markers/refactor.done'
  "python3 -c \"open('.claude/.markers/refactor.done','w').write('x')\""
)
for marker_cmd in "${marker_cmds[@]}"; do
  envelope="$(build_envelope Bash "$(python3 -c 'import json,sys; print(json.dumps({"command": sys.argv[1]}))' "$marker_cmd")")"
  run_hook pretooluse-bash-source-write-guard.sh "$envelope"
  assert_exit "Bash [$marker_cmd]: BLOCK" 2
done
assert_stderr_contains "marker shell write: stderr cites the writer" "refactor-gate.sh"

# Reading one, and the writers themselves, are not writes.
for allowed_cmd in 'cat .claude/.markers/refactor.head' 'ls -la .claude/.markers 2>/dev/null' \
  'scripts/refactor-gate.sh finish' './scripts/precommit-gauntlet.sh > /tmp/gauntlet.log 2>&1'; do
  envelope="$(build_envelope Bash "$(python3 -c 'import json,sys; print(json.dumps({"command": sys.argv[1]}))' "$allowed_cmd")")"
  run_hook pretooluse-bash-source-write-guard.sh "$envelope"
  assert_exit "Bash [$allowed_cmd]: allow" 0
done
cleanup_repo

# -------------------------------------------------------------------
# pretooluse-tool-install-guard.sh
# -------------------------------------------------------------------
echo
echo "## pretooluse-tool-install-guard.sh"

# The workstation's govulncheck was replaced by a typed `go install …@latest`,
# which no file sweep can see. A manifest tool installed from the command line
# names its pin, or is refused.
# shellcheck source=../lib/tool-versions.sh
. "$PROJECT_ROOT/scripts/lib/tool-versions.sh"
install_case() { # expected exit, command
  local envelope
  envelope="$(build_envelope Bash "$(python3 -c 'import json,sys; print(json.dumps({"command": sys.argv[1]}))' "$2")")"
  run_hook pretooluse-tool-install-guard.sh "$envelope"
  assert_exit "Bash [$2]" "$1"
}
make_repo
install_case 2 'go install golang.org/x/vuln/cmd/govulncheck@latest'
assert_stderr_contains "@latest refused: names the pinned install" "govulncheck@v$TOOL_VERSION_GOVULNCHECK"
install_case 2 'go install golang.org/x/vuln/cmd/govulncheck'
install_case 2 'cd server && go install golang.org/x/vuln/cmd/govulncheck@v1.1.4'
install_case 0 "go install golang.org/x/vuln/cmd/govulncheck@v$TOOL_VERSION_GOVULNCHECK"
install_case 2 'cargo install cargo-audit'
install_case 2 'cargo install --locked cargo-audit@0.0.1'
install_case 0 "cargo install --locked --version $TOOL_VERSION_CARGO_AUDIT cargo-audit"
install_case 0 "cargo install --locked cargo-deny@$TOOL_VERSION_CARGO_DENY"
install_case 2 'pipx install checkov'
install_case 2 'pip install --user yamllint==0.0.1'
install_case 2 'python3 -m pip install --upgrade semgrep'
install_case 0 "pipx install checkov==$TOOL_VERSION_CHECKOV --force"
install_case 2 'sudo apt-get install -y jq'
install_case 2 'snap install helm --classic'
assert_stderr_contains "distribution install refused: names the pin" "$TOOL_VERSION_HELM"
# Not a manifest tool, or not an install at all: not this guard's business.
install_case 0 'go install golang.org/x/tools/gopls@latest'
install_case 0 'cd server && go install ./cmd/meshserver'
install_case 0 'cargo install --locked cross --git https://github.com/cross-rs/cross'
install_case 0 'sudo apt-get install -y build-essential'
install_case 0 'npm install'
install_case 0 "go version -m \"\$(command -v govulncheck)\""
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
  cat >scripts/precommit-gauntlet.sh <<EOF
#!/usr/bin/env bash
exit $exit_code
EOF
  chmod +x scripts/precommit-gauntlet.sh
}

# tidy_here FUNCTION ARGS... — one function of the tidy-up library, run against
# the repository in the current directory, in a subshell so nothing it defines
# leaks into the test.
tidy_here() {
  (
    # shellcheck source=../../.claude/hooks/lib/tidy-up.sh
    source "$HOOKS_DIR/lib/tidy-up.sh"
    "$@"
  )
}
fingerprint_here() { tidy_here tidy_fingerprint; }
# gauntlet_pass_here — what a passing gauntlet records, for the content on disk.
gauntlet_pass_here() { tidy_here tidy_gauntlet_passed "$(fingerprint_here)"; }

# prove_tidy_up — the order a commit needs, done for the content on disk now:
# a gauntlet pass, then /refactor's start and finish. Called after every file
# the case needs is in place, because the proof names the content it ran on.
prove_tidy_up() {
  gauntlet_pass_here
  "$PROJECT_ROOT/scripts/refactor-gate.sh" start >/dev/null
  "$PROJECT_ROOT/scripts/refactor-gate.sh" finish >/dev/null
}

setup_passing_commit_repo() {
  make_repo
  git config user.name "Ivan Volchanskyi"
  git config user.email "ivan.volchanskyi@gmail.com"
  # Branch tests so TDD backup check passes.
  mkdir -p server/internal/api
  echo "package api" >server/internal/api/foo_test.go
  git add server/internal/api/foo_test.go
  git commit --quiet -m "add test"
  # Stub a passing gauntlet so the commit-guard's gate runs in <100ms.
  stub_gauntlet 0
  prove_tidy_up
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

# 5b. Commit on main behind a `-c key=value` prefix: BLOCK. Same separate-value
# token blind spot as the push guard — the gauntlet must still run.
make_repo
git config user.name "Ivan Volchanskyi"
git config user.email "ivan.volchanskyi@gmail.com"
git checkout --quiet -b main
stub_gauntlet 0
envelope="$(build_envelope Bash '{"command":"git -c core.hooksPath=/dev/null commit -m feat"}')"
run_hook pretooluse-git-commit-guard.sh "$envelope"
assert_exit "git -c ... commit on main: BLOCK" 2
cleanup_repo

# 6. Commit without scripts/precommit-gauntlet.sh: BLOCK (hook needs the script to enforce).
make_repo
git config user.name "Ivan Volchanskyi"
git config user.email "ivan.volchanskyi@gmail.com"
mkdir -p server/internal/api
echo "package api" >server/internal/api/foo_test.go
git add server/internal/api/foo_test.go
git commit --quiet -m "add test"
prove_tidy_up
envelope="$(build_envelope Bash '{"command":"git commit -m feat"}')"
run_hook pretooluse-git-commit-guard.sh "$envelope"
assert_exit "git commit no gauntlet script: BLOCK" 2
assert_stderr_contains "no gauntlet: stderr cites precommit-gauntlet" "precommit-gauntlet"
cleanup_repo

# 7. Gauntlet exits 1 (check failed): BLOCK.
setup_passing_commit_repo
stub_gauntlet 1
prove_tidy_up
envelope="$(build_envelope Bash '{"command":"git commit -m feat"}')"
run_hook pretooluse-git-commit-guard.sh "$envelope"
assert_exit "git commit gauntlet exit 1: BLOCK" 2
assert_stderr_contains "gauntlet fail: stderr cites failed" "failed"
cleanup_repo

# 7b. Gauntlet exits 2 (prerequisite missing): BLOCK with prereq message.
setup_passing_commit_repo
stub_gauntlet 2
prove_tidy_up
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
echo "package api" >server/internal/api/source.go
git add server/internal/api/source.go
git commit --quiet -m "src only"
stub_gauntlet 0
prove_tidy_up
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

# 10. No tidy-up at all: BLOCK before the gauntlet runs. The order is a gauntlet
# pass, then /refactor, then the commit — and the commit runs the gauntlet again.
setup_passing_commit_repo
rm -f .claude/.markers/gauntlet.pass .claude/.markers/refactor.done
cat >scripts/precommit-gauntlet.sh <<'EOF'
#!/usr/bin/env bash
touch "$(dirname "$0")/../gauntlet-ran"
exit 0
EOF
chmod +x scripts/precommit-gauntlet.sh
envelope="$(build_envelope Bash '{"command":"git commit -m feat"}')"
run_hook pretooluse-git-commit-guard.sh "$envelope"
assert_exit "git commit with no tidy-up: BLOCK" 2
assert_stderr_contains "no tidy-up: stderr cites /refactor" "/refactor"
assert_stderr_contains "no tidy-up: stderr cites the gauntlet" "precommit-gauntlet.sh"
if [ -e gauntlet-ran ]; then
  fail "no tidy-up: refused before the gauntlet runs (it ran)"
else
  pass "no tidy-up: refused before the gauntlet runs"
fi
cleanup_repo

# 11. An edit after the tidy-up: BLOCK. The proof names the content it ran on,
# and this is other content.
setup_passing_commit_repo
echo "package api // edited after /refactor" >>server/internal/api/foo_test.go
envelope="$(build_envelope Bash '{"command":"git commit -am feat"}')"
run_hook pretooluse-git-commit-guard.sh "$envelope"
assert_exit "git commit after an edit following the tidy-up: BLOCK" 2
assert_stderr_contains "edit after tidy-up: stderr cites /refactor" "/refactor"
cleanup_repo

# 12. A new untracked file after the tidy-up is an edit too: the gauntlet ran
# with the tree as it was, and a file it never saw is content it never passed.
setup_passing_commit_repo
echo "late" >late.txt
envelope="$(build_envelope Bash '{"command":"git commit -m feat"}')"
run_hook pretooluse-git-commit-guard.sh "$envelope"
assert_exit "git commit after a new file following the tidy-up: BLOCK" 2
cleanup_repo

# 13. Staging is not an edit: the proof is about what is on disk, not the index.
setup_passing_commit_repo
echo "package api // staged before the tidy-up" >>server/internal/api/foo_test.go
prove_tidy_up
git add server/internal/api/foo_test.go
envelope="$(build_envelope Bash '{"command":"git commit -m feat"}')"
run_hook pretooluse-git-commit-guard.sh "$envelope"
assert_exit "git commit after staging the tidied content: PASS" 0
cleanup_repo

# -------------------------------------------------------------------
# The tidy-up proof: lib/tidy-up.sh and scripts/refactor-gate.sh
# -------------------------------------------------------------------
echo
echo "## tidy-up proof (lib/tidy-up.sh, scripts/refactor-gate.sh)"

GATE="$PROJECT_ROOT/scripts/refactor-gate.sh"
# gate_run SUBCOMMAND — run the gate, capturing its exit and stderr the way
# run_hook does for a hook.
gate_run() {
  local stderr_file
  stderr_file="$(mktemp)"
  HOOK_EXIT=0
  "$GATE" "$1" >/dev/null 2>"$stderr_file" || HOOK_EXIT=$?
  HOOK_STDERR="$(cat "$stderr_file")"
  rm -f "$stderr_file"
}

# 1. The fingerprint is about what is on disk: staging does not move it, an
#    edit and a new file do, an ignored file and the proof itself do not.
make_repo
# One marker ignored, as the repository ignores them: naming an ignored path in
# the add was an error that left no fingerprint at all.
printf '*.log\n/.claude/.markers/refactor.head\n' >.gitignore
mkdir -p .claude/.markers
echo "head" >.claude/.markers/refactor.head
fp0="$(fingerprint_here)"
echo "edited" >>base.txt
fp1="$(fingerprint_here)"
git add base.txt
fp2="$(fingerprint_here)"
echo "noise" >ignored.log
mkdir -p .claude/.markers
echo "proof" >.claude/.markers/refactor.done
fp3="$(fingerprint_here)"
echo "new" >new.txt
fp4="$(fingerprint_here)"
if [ -n "$fp0" ] && [ "$fp0" != "$fp1" ] && [ "$fp1" = "$fp2" ] && [ "$fp2" = "$fp3" ] && [ "$fp3" != "$fp4" ]; then
  pass "fingerprint: an edit and a new file move it; staging, an ignored file and the proof do not"
else
  fail "fingerprint: an edit and a new file move it; staging, an ignored file and the proof do not ($fp0 $fp1 $fp2 $fp3 $fp4)"
fi
if [ "$(git diff --cached --name-only)" = "base.txt" ]; then
  pass "fingerprint: the real index is left as it was"
else
  fail "fingerprint: the real index is left as it was (staged=[$(git diff --cached --name-only | tr '\n' ' ')])"
fi
cleanup_repo

# 2. /refactor before any gauntlet pass: refused.
make_repo
gate_run start
assert_exit "refactor-gate start with no gauntlet pass: BLOCK" 1
assert_stderr_contains "no gauntlet pass: cites the gauntlet" "precommit-gauntlet.sh"
cleanup_repo

# 3. /refactor after a pass on other content: refused.
make_repo
gauntlet_pass_here
echo "edited after the checks" >>base.txt
gate_run start
assert_exit "refactor-gate start after an edit following the pass: BLOCK" 1
cleanup_repo

# 4. A gauntlet run during which the tree changed records no pass.
make_repo
start_fp="$(fingerprint_here)"
echo "edited while the checks ran" >>base.txt
if tidy_here tidy_gauntlet_passed "$start_fp"; then
  fail "a tree edited while the checks ran records no pass"
elif [ -e .claude/.markers/gauntlet.pass ]; then
  fail "a tree edited while the checks ran records no pass (one was written)"
else
  pass "a tree edited while the checks ran records no pass"
fi
cleanup_repo

# 5. finish with no start: refused, and nothing is recorded.
make_repo
gauntlet_pass_here
gate_run finish
assert_exit "refactor-gate finish with no start: BLOCK" 1
if [ -e .claude/.markers/refactor.done ]; then
  fail "finish with no start records nothing (refactor.done written)"
else pass "finish with no start records nothing"; fi
cleanup_repo

# 6. start, tidy, finish: the proof names the content /refactor left, and the
#    start is spent, so the next commit needs a gauntlet pass of its own.
make_repo
echo "work" >>base.txt
gauntlet_pass_here
gate_run start
assert_exit "refactor-gate start after a pass on this content: PASS" 0
echo "tidied" >>base.txt
gate_run finish
assert_exit "refactor-gate finish after a start: PASS" 0
if [ "$(cat .claude/.markers/refactor.done 2>/dev/null)" = "$(fingerprint_here)" ]; then
  pass "finish: the proof names the content /refactor left"
else fail "finish: the proof names the content /refactor left"; fi
gate_run finish
assert_exit "refactor-gate finish a second time on one start: BLOCK" 1
cleanup_repo

# 7. finish with the work uncommitted marks no commit for the push guard; finish
#    with nothing tracked left uncommitted marks HEAD — the content it proved is
#    HEAD's, which is how a commit rebased by hand is cleared to push.
make_repo
echo "work" >>base.txt
gauntlet_pass_here
gate_run start
gate_run finish
if [ -e .claude/.markers/refactor.head ]; then
  fail "finish with uncommitted work marks no commit"
else pass "finish with uncommitted work marks no commit"; fi
git checkout --quiet -- base.txt
gauntlet_pass_here
gate_run start
gate_run finish
if [ "$(cat .claude/.markers/refactor.head 2>/dev/null)" = "$(git rev-parse HEAD)" ]; then
  pass "finish on HEAD's own content marks HEAD for the push guard"
else fail "finish on HEAD's own content marks HEAD for the push guard"; fi
cleanup_repo

# 8. /refactor invoked as a skill before a gauntlet pass: BLOCK, before the
#    skill's instructions load; after a pass it is let through and the start is
#    recorded. Other skills are not its business.
make_repo
envelope="$(build_envelope Skill '{"skill":"refactor"}')"
run_hook pretooluse-refactor-start-gate.sh "$envelope"
assert_exit "Skill refactor with no gauntlet pass: BLOCK" 2
assert_stderr_contains "Skill refactor refused: cites the gauntlet" "precommit-gauntlet.sh"
envelope="$(build_envelope Skill '{"skill":"tests-audit"}')"
run_hook pretooluse-refactor-start-gate.sh "$envelope"
assert_exit "Skill tests-audit: allow (not /refactor)" 0
gauntlet_pass_here
envelope="$(build_envelope Skill '{"skill":"refactor"}')"
run_hook pretooluse-refactor-start-gate.sh "$envelope"
assert_exit "Skill refactor after a pass on this content: allow" 0
if [ "$(cat .claude/.markers/refactor.start 2>/dev/null)" = "$(fingerprint_here)" ]; then
  pass "Skill refactor after a pass records the start"
else fail "Skill refactor after a pass records the start"; fi
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

# 3b. Push to main behind a `-c key=value` prefix: BLOCK. `-c` takes its value
# as a SEPARATE token, so a filter that only skips `-`-prefixed words never
# reaches the push verb and the whole guard silently no-ops.
make_repo
envelope="$(build_envelope Bash '{"command":"git -c protocol.version=2 push origin main"}')"
run_hook pretooluse-git-push-guard.sh "$envelope"
assert_exit "git -c ... push origin main: BLOCK" 2
assert_stderr_contains "-c push main: stderr cites main" "main"
cleanup_repo

# 4. Push doc-only branch WITHOUT refactor marker: BLOCK. Every commit since
# origin/dev requires the marker — no doc-only / CI-only exemption.
make_repo
echo "# new" >newdoc.md
git add newdoc.md
git commit --quiet -m "docs"
envelope="$(build_envelope Bash '{"command":"git push origin dev"}')"
run_hook pretooluse-git-push-guard.sh "$envelope"
assert_exit "git push doc-only no marker: BLOCK" 2
assert_stderr_contains "doc-only no marker: stderr cites /refactor" "/refactor"
cleanup_repo

# 4b. Push doc-only branch WITH a matching refactor marker: PASS.
make_repo
echo "# new" >newdoc.md
git add newdoc.md
git commit --quiet -m "docs"
mkdir -p .claude/.markers
git rev-parse HEAD >.claude/.markers/refactor.head
envelope="$(build_envelope Bash '{"command":"git push origin dev"}')"
run_hook pretooluse-git-push-guard.sh "$envelope"
assert_exit "git push doc-only w/ marker: PASS" 0
cleanup_repo

# 5. Push branch with source commits but no refactor marker: BLOCK.
make_repo
mkdir -p server/internal/api
echo "package api" >server/internal/api/foo_test.go
git add server/internal/api/foo_test.go
git commit --quiet -m "add test"
echo "package api" >server/internal/api/handlers.go
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
echo "package api" >server/internal/api/foo_test.go
git add server/internal/api/foo_test.go
git commit --quiet -m "add test"
echo "package api" >server/internal/api/handlers.go
git add server/internal/api/handlers.go
git commit --quiet -m "feat"
mkdir -p .claude/.markers
git rev-parse HEAD >.claude/.markers/refactor.head
envelope="$(build_envelope Bash '{"command":"git push origin dev"}')"
run_hook pretooluse-git-push-guard.sh "$envelope"
assert_exit "git push source commits w/ refactor marker: PASS" 0
cleanup_repo

# 6b. Push branch with a deploy/ change (no Go/Rust/TS) but no refactor marker:
# BLOCK. A non-source file under deploy/ requires /refactor by the folder rule.
make_repo
mkdir -p deploy
echo "services: {}" >deploy/docker-compose.yml
git add deploy/docker-compose.yml
git commit --quiet -m "tweak compose"
envelope="$(build_envelope Bash '{"command":"git push origin dev"}')"
run_hook pretooluse-git-push-guard.sh "$envelope"
assert_exit "git push deploy/ no marker: BLOCK" 2
assert_stderr_contains "deploy/ no marker: stderr cites /refactor" "/refactor"
cleanup_repo

# 6c. Push branch with a scripts/ change (no Go/Rust/TS) but no refactor marker:
# BLOCK. A .sh under scripts/ is not is-source, so the folder rule requires it.
make_repo
mkdir -p scripts
echo "echo hi" >scripts/foo.sh
git add scripts/foo.sh
git commit --quiet -m "tweak script"
envelope="$(build_envelope Bash '{"command":"git push origin dev"}')"
run_hook pretooluse-git-push-guard.sh "$envelope"
assert_exit "git push scripts/ no marker: BLOCK" 2
assert_stderr_contains "scripts/ no marker: stderr cites /refactor" "/refactor"
cleanup_repo

# 6d. Push branch with a scripts/ change and a matching refactor marker: PASS.
make_repo
mkdir -p scripts
echo "echo hi" >scripts/foo.sh
git add scripts/foo.sh
git commit --quiet -m "tweak script"
mkdir -p .claude/.markers
git rev-parse HEAD >.claude/.markers/refactor.head
envelope="$(build_envelope Bash '{"command":"git push origin dev"}')"
run_hook pretooluse-git-push-guard.sh "$envelope"
assert_exit "git push scripts/ w/ refactor marker: PASS" 0
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

# 3. Edit existing ADR file: allow (ADRs 013+ are mutable).
make_repo
mkdir -p docs/adr
echo "# ADR-080" >docs/adr/ADR-080-foo.md
git add docs/adr/ADR-080-foo.md
git commit --quiet -m "adr"
envelope="$(build_envelope Edit '{"file_path":"docs/adr/ADR-080-foo.md","old_string":"a","new_string":"b"}')"
run_hook pretooluse-write-guard.sh "$envelope"
assert_exit "Edit existing ADR: allow (mutable)" 0
cleanup_repo

# 3b. Overwrite (Write) an existing ADR file: allow (ADRs 013+ are mutable).
make_repo
mkdir -p docs/adr
echo "# ADR-080" >docs/adr/ADR-080-foo.md
git add docs/adr/ADR-080-foo.md
git commit --quiet -m "adr"
envelope="$(build_envelope Write '{"file_path":"docs/adr/ADR-080-foo.md","content":"# ADR-080 revised"}')"
run_hook pretooluse-write-guard.sh "$envelope"
assert_exit "Write over existing ADR: allow (mutable)" 0
cleanup_repo

# 4. Write a NEW ADR file: allow.
make_repo
mkdir -p docs/adr
envelope="$(build_envelope Write '{"file_path":"docs/adr/ADR-082-new.md","content":"# new"}')"
run_hook pretooluse-write-guard.sh "$envelope"
assert_exit "Write new ADR file: allow" 0
cleanup_repo

# 4b. Write a NEW ADR that links a plan file: BLOCK — a plan is deleted when
# its work lands, so an ADR pointing at one rots.
make_repo
mkdir -p docs/adr
envelope="$(build_envelope Write '{"file_path":"docs/adr/ADR-098-bad.md","content":"# ADR-098\n\nSee [plan](../../.claude/plans/foo.md) for detail."}')"
run_hook pretooluse-write-guard.sh "$envelope"
assert_exit "New ADR with plan link: BLOCK" 2
assert_stderr_contains "ADR plan-link: stderr cites the rule" "plans-and-adrs.md"
cleanup_repo

# 4c. Write a NEW ADR with non-plan links (other ADR + decisions index): allow.
make_repo
mkdir -p docs/adr
envelope="$(build_envelope Write '{"file_path":"docs/adr/ADR-097-ok.md","content":"# ADR-097\n\nSee [ADR-080](ADR-080-foo.md) and the [index](../../.claude/decisions.md)."}')"
run_hook pretooluse-write-guard.sh "$envelope"
assert_exit "New ADR with non-plan links: allow" 0
cleanup_repo

# 4d. Write a NEW ADR linking a plan in a subdirectory: BLOCK. A plan is
# deleted when its work lands, so no path under plans/ is a stable target.
make_repo
mkdir -p docs/adr
envelope="$(build_envelope Write '{"file_path":"docs/adr/ADR-093-arch.md","content":"# ADR-093\n\nWorking plan: [plan](../../.claude/plans/archive/foo.md)."}')"
run_hook pretooluse-write-guard.sh "$envelope"
assert_exit "New ADR with nested plan link: BLOCK" 2
cleanup_repo

# 4e. Edit an existing ADR to add an ACTIVE plan link: BLOCK.
make_repo
mkdir -p docs/adr
echo "# ADR-080" >docs/adr/ADR-080-foo.md
git add docs/adr/ADR-080-foo.md
git commit --quiet -m "adr"
envelope="$(build_envelope Edit '{"file_path":"docs/adr/ADR-080-foo.md","old_string":"a","new_string":"see [plan](../../.claude/plans/foo.md)"}')"
run_hook pretooluse-write-guard.sh "$envelope"
assert_exit "Edit ADR adds plan link: BLOCK" 2
assert_stderr_contains "ADR edit plan-link: cites the rule" "plans-and-adrs.md"
cleanup_repo

# 4f. Edit an existing ADR to add a link that is not a plan: allow.
make_repo
mkdir -p docs/adr
echo "# ADR-080" >docs/adr/ADR-080-foo.md
git add docs/adr/ADR-080-foo.md
git commit --quiet -m "adr"
envelope="$(build_envelope Edit '{"file_path":"docs/adr/ADR-080-foo.md","old_string":"a","new_string":"see [index](../../.claude/decisions.md)"}')"
run_hook pretooluse-write-guard.sh "$envelope"
assert_exit "Edit ADR adds non-plan link: allow" 0
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

# 9. A marker written by hand: BLOCK. Each is written by the step it proves —
#    the gauntlet, scripts/refactor-gate.sh, the post-commit hook — and one
#    written any other way proves nothing.
make_repo
for marker in refactor.done refactor.head gauntlet.pass; do
  envelope="$(build_envelope Write "$(printf '{"file_path":"%s/.claude/.markers/%s","content":"x"}' "$REPO" "$marker")")"
  run_hook pretooluse-write-guard.sh "$envelope"
  assert_exit "Write .claude/.markers/$marker: BLOCK" 2
done
envelope="$(build_envelope Edit '{"file_path":".claude/.markers/refactor.done","old_string":"a","new_string":"b"}')"
run_hook pretooluse-write-guard.sh "$envelope"
assert_exit "Edit a relative .claude/.markers/ path: BLOCK" 2
assert_stderr_contains "marker write: stderr cites the writer" "refactor-gate.sh"
cleanup_repo

# -------------------------------------------------------------------
# session-start-context-load.sh
# -------------------------------------------------------------------
echo
echo "## session-start-context-load.sh"

# 1. SessionStart in any repo: outputs additionalContext JSON, exit 0.
make_repo
envelope='{"session_id":"test","cwd":".","hook_event_name":"SessionStart"}'
HOOK_EXIT=0
HOOK_STDOUT=""
HOOK_STDERR=""
stdout_file="$(mktemp)"
stderr_file="$(mktemp)"
if printf '%s' "$envelope" | "$HOOKS_DIR/session-start-context-load.sh" >"$stdout_file" 2>"$stderr_file"; then HOOK_EXIT=0; else HOOK_EXIT=$?; fi
HOOK_STDOUT="$(cat "$stdout_file")"
HOOK_STDERR="$(cat "$stderr_file")"
rm -f "$stdout_file" "$stderr_file"
assert_exit "SessionStart: exit 0" 0
if printf '%s' "$HOOK_STDOUT" | python3 -c 'import json,sys; d=json.load(sys.stdin); assert "hookSpecificOutput" in d and "additionalContext" in d["hookSpecificOutput"], d' 2>/dev/null; then
  pass "SessionStart: emits hookSpecificOutput.additionalContext JSON"
else
  fail "SessionStart: stdout is not valid hookSpecificOutput JSON (got: $(printf '%s' "$HOOK_STDOUT" | head -c 200))"
fi
cleanup_repo

# -------------------------------------------------------------------
# pretooluse-test-skip-guard.sh
# -------------------------------------------------------------------
echo
echo "## pretooluse-test-skip-guard.sh"

# Go: t.Skip in a *_test.go file: BLOCK.
envelope="$(build_envelope Edit '{"file_path":"server/internal/api/foo_test.go","old_string":"a","new_string":"func TestX(t *testing.T){ t.Skip(\"no db\") }"}')"
run_hook pretooluse-test-skip-guard.sh "$envelope"
assert_exit "Go t.Skip in _test.go: BLOCK" 2
assert_stderr_contains "Go skip: stderr cites determinism rule" "tests-determinism.md"

# Go: t.Skipf in a *_test.go file: BLOCK.
envelope="$(build_envelope Write '{"file_path":"server/internal/db/x_test.go","content":"t.Skipf(\"%s unset\", env)"}')"
run_hook pretooluse-test-skip-guard.sh "$envelope"
assert_exit "Go t.Skipf in _test.go: BLOCK" 2

# Go: t.SkipNow in a *_test.go file: BLOCK.
envelope="$(build_envelope Edit '{"file_path":"server/x_test.go","old_string":"a","new_string":"t.SkipNow()"}')"
run_hook pretooluse-test-skip-guard.sh "$envelope"
assert_exit "Go t.SkipNow in _test.go: BLOCK" 2

# Go: no skip in a *_test.go file: allow.
envelope="$(build_envelope Edit '{"file_path":"server/internal/api/foo_test.go","old_string":"a","new_string":"require.NoError(t, err)"}')"
run_hook pretooluse-test-skip-guard.sh "$envelope"
assert_exit "Go _test.go without skip: allow" 0

# Go: .Skip( in a NON-test .go file: allow (not a test file).
envelope="$(build_envelope Write '{"file_path":"server/internal/api/foo.go","content":"scanner.Skip()"}')"
run_hook pretooluse-test-skip-guard.sh "$envelope"
assert_exit "Go non-test file with .Skip(: allow" 0

# Web: it.skip in a *.test.tsx file: BLOCK.
envelope="$(build_envelope Write '{"file_path":"web/src/foo.test.tsx","content":"it.skip(\"x\", () => {})"}')"
run_hook pretooluse-test-skip-guard.sh "$envelope"
assert_exit "Web it.skip in .test.tsx: BLOCK" 2

# Web: test.only in a *.spec.ts file: BLOCK.
envelope="$(build_envelope Write '{"file_path":"web/e2e/foo.spec.ts","content":"test.only(\"x\", async () => {})"}')"
run_hook pretooluse-test-skip-guard.sh "$envelope"
assert_exit "Web test.only in .spec.ts: BLOCK" 2

# Web: describe.skip in a *.test.ts file: BLOCK.
envelope="$(build_envelope Edit '{"file_path":"web/src/foo.test.ts","old_string":"a","new_string":"describe.skip(\"grp\", () => {})"}')"
run_hook pretooluse-test-skip-guard.sh "$envelope"
assert_exit "Web describe.skip in .test.ts: BLOCK" 2

# Web: xit( focus-skip marker: BLOCK.
envelope="$(build_envelope Write '{"file_path":"web/src/foo.test.ts","content":"xit(\"x\", () => {})"}')"
run_hook pretooluse-test-skip-guard.sh "$envelope"
assert_exit "Web xit( in .test.ts: BLOCK" 2

# Web: plain it()/expect in a *.test.ts file: allow.
envelope="$(build_envelope Write '{"file_path":"web/src/foo.test.ts","content":"it(\"x\", () => { expect(1).toBe(1) })"}')"
run_hook pretooluse-test-skip-guard.sh "$envelope"
assert_exit "Web .test.ts without skip: allow" 0

# Rust: #[ignore] attribute: BLOCK.
envelope="$(build_envelope Edit '{"file_path":"agent/crates/mesh-protocol/src/codec.rs","old_string":"a","new_string":"#[ignore]\n#[test]\nfn t() {}"}')"
run_hook pretooluse-test-skip-guard.sh "$envelope"
assert_exit "Rust #[ignore]: BLOCK" 2

# Rust: #[ignore = \"reason\"] attribute: BLOCK.
envelope="$(build_envelope Write '{"file_path":"agent/src/lib.rs","content":"#[ignore = \"flaky\"]\n#[test]\nfn t() {}"}')"
run_hook pretooluse-test-skip-guard.sh "$envelope"
assert_exit "Rust #[ignore = reason]: BLOCK" 2

# Rust: ordinary test without #[ignore]: allow.
envelope="$(build_envelope Edit '{"file_path":"agent/src/lib.rs","old_string":"a","new_string":"#[test]\nfn t() { assert!(true) }"}')"
run_hook pretooluse-test-skip-guard.sh "$envelope"
assert_exit "Rust #[test] without ignore: allow" 0

# Non-test file types containing the words: allow (docs/markdown).
envelope="$(build_envelope Write '{"file_path":"docs/infrastructure/Testing.md","content":"do not use t.Skip() or it.skip or #[ignore]"}')"
run_hook pretooluse-test-skip-guard.sh "$envelope"
assert_exit "Markdown mentioning skip words: allow" 0

# Non-Write/Edit tool: allow (guard only fires on writes).
envelope="$(build_envelope Bash '{"command":"go test ./...","description":"run"}')"
run_hook pretooluse-test-skip-guard.sh "$envelope"
assert_exit "Bash tool: allow (not a write)" 0

# -------------------------------------------------------------------
# git-post-commit.sh (deterministic auto-push) + its SessionStart installer
# -------------------------------------------------------------------
echo
echo "## git-post-commit.sh (auto-push) + installer"

# Set up a dev-branch repo wired to a bare origin remote, with the git native
# post-commit auto-push hook installed exactly as SessionStart installs it (copy
# the logic into the repo, then run the real installer). Pushes are to a local
# bare remote, so every test is deterministic and offline.
REMOTE=""
setup_autopush_repo() {
  REPO="$(mktemp -d)"
  REMOTE="$(mktemp -d)"
  git init --quiet --bare --initial-branch=dev "$REMOTE"
  cd "$REPO"
  git init --quiet --initial-branch=dev
  git config user.email "ivan.volchanskyi@gmail.com"
  git config user.name "Ivan Volchanskyi"
  git remote add origin "$REMOTE"
  echo base >base.txt
  git add base.txt
  git commit --quiet -m init
  git push --quiet -u origin dev
  mkdir -p .claude/hooks/lib
  cp "$HOOKS_DIR/git-post-commit.sh" .claude/hooks/git-post-commit.sh
  cp "$HOOKS_DIR/lib/tidy-up.sh" .claude/hooks/lib/tidy-up.sh
  chmod +x .claude/hooks/git-post-commit.sh
  "$HOOKS_DIR/sessionstart-install-git-hooks.sh" </dev/null >/dev/null 2>&1
}
remote_ref() { git --git-dir="$REMOTE" rev-parse "$1" 2>/dev/null || echo none; }

# autopush_diag dumps git/hook state when an auto-push assertion fails. The
# auto-push cases pass locally but failed only on the CI runner; this surfaces
# the environment difference (hook output, branch, hooks dir, marker) in the CI
# log. Reads $REMOTE/hook.log captured from the triggering `git commit`, kept
# outside the work tree: a log written there is a new file, which is content the
# tidy-up proof never saw.
autopush_diag() {
  {
    echo "---- autopush diag [$1] ----"
    echo "git: $(git --version)"
    echo "env: CI='${CI:-}' GHA='${GITHUB_ACTIONS:-}' ACTIVE='${OPENGATE_AUTOPUSH_ACTIVE:-}'"
    echo "cwd: $(pwd)"
    echo "branch: $(git rev-parse --abbrev-ref HEAD 2>&1)"
    echo "HEAD: $(git rev-parse HEAD 2>&1)  origin/dev: $(remote_ref dev)"
    echo "core.hooksPath: $(git config --get core.hooksPath || echo unset)"
    echo "git-path hooks: $(git rev-parse --git-path hooks 2>&1)"
    echo "post-commit: $(ls -la .git/hooks/post-commit 2>&1)"
    echo "marker: $(cat .claude/.markers/refactor.head 2>/dev/null || echo none)"
    echo "hook.log:"
    cat "$REMOTE/hook.log" 2>&1 || echo "(none)"
    echo "----------------------------"
  } >&2
}

# 1. Installer writes an executable post-commit hook.
setup_autopush_repo
if [ -x .git/hooks/post-commit ]; then
  pass "installer: .git/hooks/post-commit is executable"
else fail "installer: .git/hooks/post-commit missing or not executable"; fi
cleanup_repo

# 2. Commit on dev, of content the gauntlet passed and /refactor finished,
# auto-pushes AND refreshes the refactor marker to HEAD.
setup_autopush_repo
echo change >f.txt
git add f.txt
prove_tidy_up
CI='' GITHUB_ACTIONS='' OPENGATE_AUTOPUSH_DEBUG=1 git commit -q -m "feat: x" >"$REMOTE/hook.log" 2>&1
head="$(git rev-parse HEAD)"
if [ "$(remote_ref dev)" = "$head" ]; then
  pass "auto-push: commit on dev pushed to origin/dev"
else
  fail "auto-push: origin/dev=$(remote_ref dev) != HEAD=$head"
  autopush_diag "case2-push"
fi
if [ "$(cat .claude/.markers/refactor.head 2>/dev/null || echo none)" = "$head" ]; then
  pass "auto-push: refactor marker refreshed to HEAD"
else fail "auto-push: marker != HEAD"; fi
cleanup_repo

# 3. Commit on a NON-dev branch does not push (skip).
setup_autopush_repo
git checkout --quiet -b feat/side
echo s >s.txt
git add s.txt
git commit -q -m "feat: side" >/dev/null 2>&1
if git --git-dir="$REMOTE" rev-parse --verify --quiet feat/side >/dev/null 2>&1; then
  fail "auto-push: non-dev branch should NOT be pushed"
else pass "auto-push: non-dev branch not pushed (skip)"; fi
cleanup_repo

# 4. Re-entrancy guard: OPENGATE_AUTOPUSH_ACTIVE set => no push.
setup_autopush_repo
before="$(remote_ref dev)"
echo r >r.txt
git add r.txt
OPENGATE_AUTOPUSH_ACTIVE=1 git commit -q -m "feat: r" >/dev/null 2>&1
if [ "$(remote_ref dev)" = "$before" ]; then
  pass "auto-push: re-entrancy guard skips push"
else fail "auto-push: re-entrancy guard failed (origin advanced)"; fi
cleanup_repo

# 5. CI guard: CI set => no push.
setup_autopush_repo
before="$(remote_ref dev)"
echo c >c.txt
git add c.txt
CI=1 git commit -q -m "feat: c" >/dev/null 2>&1
if [ "$(remote_ref dev)" = "$before" ]; then
  pass "auto-push: CI guard skips push"
else fail "auto-push: CI guard failed (origin advanced)"; fi
cleanup_repo

# 6. Divergent upstream: hook rebases, re-points the marker to the post-rebase
#    HEAD, and pushes (the "flawless" marker-after-rebase path).
setup_autopush_repo
tmpclone="$(mktemp -d)"
git clone --quiet "$REMOTE" "$tmpclone"
(cd "$tmpclone" && git config user.email o@o && git config user.name o \
  && echo other >other.txt && git add other.txt && git commit -q -m other \
  && git push -q origin dev)
rm -rf "$tmpclone"
echo m >m.txt
git add m.txt
prove_tidy_up
CI='' GITHUB_ACTIONS='' OPENGATE_AUTOPUSH_DEBUG=1 git commit -q -m "feat: m" >"$REMOTE/hook.log" 2>&1
head="$(git rev-parse HEAD)"
if [ "$(remote_ref dev)" = "$head" ]; then
  pass "auto-push: rebased onto divergent upstream and pushed"
else
  fail "auto-push: divergent push failed (origin=$(remote_ref dev) HEAD=$head)"
  autopush_diag "case6-divergent"
fi
if [ "$(cat .claude/.markers/refactor.head 2>/dev/null || echo none)" = "$head" ]; then
  pass "auto-push: marker re-pointed to post-rebase HEAD"
else fail "auto-push: marker stale after rebase"; fi
cleanup_repo

# 7. A commit with no tidy-up behind it: no marker, and no push. The marker is
#    what the push guard reads, so a hook that wrote it unconditionally made
#    the guard unable to fail; and a push the guard would refuse is not one to
#    make from underneath it.
setup_autopush_repo
before="$(remote_ref dev)"
echo u >u.txt
git add u.txt
CI='' GITHUB_ACTIONS='' git commit -q -m "feat: u" >"$REMOTE/hook.log" 2>&1
if [ -e .claude/.markers/refactor.head ]; then
  fail "auto-push: no tidy-up, yet a refactor marker was written"
else pass "auto-push: no tidy-up, no refactor marker"; fi
if [ "$(remote_ref dev)" = "$before" ]; then
  pass "auto-push: no tidy-up, no push"
else fail "auto-push: no tidy-up, yet origin advanced"; fi
if grep -qF '/refactor' "$REMOTE/hook.log"; then
  pass "auto-push: no tidy-up, and it says what is missing"
else fail "auto-push: no tidy-up, and it says what is missing (log=[$(cat "$REMOTE/hook.log")])"; fi
cleanup_repo

# 8. A tidy-up of other content: no marker, no push. The edit after /refactor
#    is in the commit, and nothing checked it.
setup_autopush_repo
before="$(remote_ref dev)"
echo v >v.txt
git add v.txt
prove_tidy_up
echo "after the tidy-up" >>v.txt
git add v.txt
CI='' GITHUB_ACTIONS='' git commit -q -m "feat: v" >"$REMOTE/hook.log" 2>&1
if [ -e .claude/.markers/refactor.head ] || [ "$(remote_ref dev)" != "$before" ]; then
  fail "auto-push: a tidy-up of other content neither marks nor pushes"
else pass "auto-push: a tidy-up of other content neither marks nor pushes"; fi
cleanup_repo

# -------------------------------------------------------------------
# The git verb pattern is one pattern
# -------------------------------------------------------------------
# Five hooks ask whether a Bash command carries a git verb, and the answer had
# drifted: three matched `git -c key=value <verb>` while two stopped at the
# first `-c`, under a comment saying they used the same pattern as the commit
# guard. So the pen-test gate and the settings rebalancer stood down on exactly
# the form the commit guard was strengthened to catch. These cases hold the one
# shared definition to both halves, and the sweep below refuses a sixth copy.
# shellcheck source=../../.claude/hooks/lib/common.sh
source "$HOOKS_DIR/lib/common.sh"

verb_case() { # verb, command, want
  local got
  if grep -qE "$(git_verb_re "$1")" <<<"$2"; then got=DETECTED; else got=ignored; fi
  if [ "$got" = "$3" ]; then
    pass "verb pattern: [$2] -> $got"
  else fail "verb pattern: [$2] -> $got (want $3)"; fi
}

verb_case commit 'git commit -m x' DETECTED
verb_case commit 'git -c core.hooksPath=/dev/null commit -m x' DETECTED
verb_case commit 'git -c a=b -c c=d commit' DETECTED
verb_case commit 'git -C /repo commit -m x' DETECTED
verb_case commit 'git status' ignored
verb_case commit 'git log --grep=commit' ignored
verb_case push 'git push origin dev' DETECTED
verb_case push 'git -c color.ui=false push origin dev' DETECTED
verb_case push 'git status' ignored
verb_case push 'git log --grep=push' ignored

# A hook that spells the pattern out again is a copy free to drift, which is the
# defect these cases exist to close. The count keeps a sweep that reached no
# hook at all from reporting clean.
verb_copies=""
verb_hooks=0
for hook in "$HOOKS_DIR"/*.sh; do
  verb_hooks=$((verb_hooks + 1))
  # The tell is the option-skipping group, not the word `git`: the push guard
  # also asks, separately and legitimately, whether a push names main.
  grep -qF '(-[^[:space:]]+[[:space:]]+' "$hook" \
    && verb_copies="$verb_copies $(basename "$hook")"
done
if [ "$verb_hooks" -eq 0 ]; then
  fail "verb pattern: the sweep read no hooks at all"
elif [ -n "$verb_copies" ]; then
  fail "verb pattern: spelled out again in$verb_copies — call git_verb_re instead"
else
  pass "verb pattern: no hook carries a copy of its own ($verb_hooks read)"
fi

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
