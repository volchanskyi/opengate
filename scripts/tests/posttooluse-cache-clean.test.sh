#!/usr/bin/env bash
# Tests for .claude/hooks/posttooluse-cache-clean.sh — the PostToolUse janitor
# that guarantees "clean the cache after every push" holds no matter HOW the
# push happened, plus a free-disk floor that reclaims even when a push is missed
# entirely. Plain bash; no bats dependency.
# Run: ./scripts/tests/posttooluse-cache-clean.test.sh
#
# Each test copies the hook into a throwaway dir next to a STUB cleaner, so the
# hook's own relative resolution finds the stub and the real cargo/docker
# cleanup never runs. `df` is stubbed on PATH to drive the disk-floor branch.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
HOOK="$PROJECT_ROOT/.claude/hooks/posttooluse-cache-clean.sh"

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

# Build a sandbox: a copy of the hook, a stub cleaner that records its calls,
# and a stub `df` reporting $1 KiB available. Sets SANDBOX and CLEAN_LOG.
# Called directly, never in a command substitution — a subshell would discard
# both globals.
make_sandbox() {
  local avail_kb="$1" dir
  dir="$(mktemp -d)"
  SANDBOX="$dir"
  mkdir -p "$dir/hooks" "$dir/bin"
  cp "$HOOK" "$dir/hooks/posttooluse-cache-clean.sh"
  chmod +x "$dir/hooks/posttooluse-cache-clean.sh"
  # The hook sources lib/common.sh relative to its own location.
  cp -R "$PROJECT_ROOT/.claude/hooks/lib" "$dir/hooks/lib"
  CLEAN_LOG="$dir/cleaned.log"
  : >"$CLEAN_LOG"
  cat >"$SANDBOX/hooks/post-push-clean-caches.sh" <<EOF
#!/usr/bin/env bash
printf 'cleaner ran\n' >>"$CLEAN_LOG"
exit 0
EOF
  chmod +x "$SANDBOX/hooks/post-push-clean-caches.sh"
  cat >"$dir/bin/df" <<EOF
#!/usr/bin/env bash
printf 'Filesystem 1K-blocks Used Available Use%% Mounted on\n'
printf '/dev/sdd 263000000 1000 %s 50%% /\n' "$avail_kb"
exit 0
EOF
  chmod +x "$dir/bin/df"
  # Default: nothing is building. stub_busy() flips this.
  cat >"$dir/bin/pgrep" <<'EOF'
#!/usr/bin/env bash
exit 1
EOF
  chmod +x "$dir/bin/pgrep"
}

# Make the sandbox look like a build is in flight (pgrep finds a match).
stub_busy() {
  cat >"$SANDBOX/bin/pgrep" <<'EOF'
#!/usr/bin/env bash
printf '4242\n'
exit 0
EOF
  chmod +x "$SANDBOX/bin/pgrep"
}

# Feed the hook a PostToolUse payload. Args: tool_name, command. Runs against
# the sandbox built by the preceding make_sandbox. Sets HOOK_EXIT.
run_hook() {
  local tool="$1" cmd="$2"
  local dir="$SANDBOX" payload
  payload="$(printf '{"tool_name":"%s","tool_input":{"command":"%s"}}' "$tool" "$cmd")"
  (
    cd "$dir" || exit 99
    printf '%s' "$payload" \
      | env -u CI -u GITHUB_ACTIONS "PATH=$dir/bin:$PATH" \
        bash "$dir/hooks/posttooluse-cache-clean.sh"
  ) >/dev/null 2>&1
  HOOK_EXIT=$?
}

cleaner_ran() { [[ -s "$CLEAN_LOG" ]]; }

# --- T1: a push cleans, regardless of disk headroom --------------------------
# The rule is "clean after EVERY push" — not "clean when the disk looks tight".
t_push_always_cleans() {
  make_sandbox 200000000 # ~190 GiB free: no disk pressure at all
  run_hook Bash "git push origin dev"
  if [[ "$HOOK_EXIT" -ne 0 ]]; then
    fail "push: expected exit 0, got $HOOK_EXIT"
    return
  fi
  if ! cleaner_ran; then
    fail "push: cleaner did not run after a push"
    return
  fi
  pass "a push triggers the cleanup even with a nearly empty disk"
}

# --- T2: a push with flags/options is still a push ---------------------------
t_push_variants_clean() {
  local variant
  for variant in "git push" "git push --force-with-lease origin dev" \
    "git -c color.ui=false push origin dev" "cd /home/ivan/opengate && git push origin dev"; do
    make_sandbox 200000000
    run_hook Bash "$variant"
    if ! cleaner_ran; then
      fail "push variant: cleaner did not run for '$variant'"
      return
    fi
  done
  pass "cleanup fires for bare, flagged, -c-prefixed and chained push forms"
}

# --- T3: ordinary commands do not clean when there is headroom ---------------
t_no_clean_when_roomy() {
  make_sandbox 200000000
  run_hook Bash "ls -la"
  if [[ "$HOOK_EXIT" -ne 0 ]]; then
    fail "roomy: expected exit 0, got $HOOK_EXIT"
    return
  fi
  if cleaner_ran; then
    fail "roomy: cleaner ran for a non-push command with plenty of free disk"
    return
  fi
  pass "non-push commands are a no-op while free space is above the floor"
}

# --- T4: the disk floor reclaims even when no push happened ------------------
# The safety net for the failure this hook exists to prevent: a missed push
# leaves caches growing until the disk fills and breaks local development.
t_disk_floor_cleans() {
  make_sandbox 1000000 # ~0.95 GiB free: far below any sane floor
  run_hook Bash "ls -la"
  if [[ "$HOOK_EXIT" -ne 0 ]]; then
    fail "disk floor: expected exit 0, got $HOOK_EXIT"
    return
  fi
  if ! cleaner_ran; then
    fail "disk floor: cleaner did not run with the disk nearly full"
    return
  fi
  pass "free space below the floor reclaims even without a push"
}

# --- T5: non-Bash tools are ignored ------------------------------------------
t_ignores_other_tools() {
  make_sandbox 1000000
  run_hook Write "irrelevant"
  if [[ "$HOOK_EXIT" -ne 0 ]]; then
    fail "other tool: expected exit 0, got $HOOK_EXIT"
    return
  fi
  if cleaner_ran; then
    fail "other tool: cleaner ran for a non-Bash tool call"
    return
  fi
  pass "non-Bash tool calls are ignored"
}

# --- T6: never breaks the session --------------------------------------------
# PostToolUse runs after the work is already done; a janitor failure must never
# surface as a tool error.
t_never_fails_the_call() {
  make_sandbox 1000000
  printf '#!/usr/bin/env bash\nexit 17\n' >"$SANDBOX/hooks/post-push-clean-caches.sh"
  chmod +x "$SANDBOX/hooks/post-push-clean-caches.sh"
  run_hook Bash "git push origin dev"
  if [[ "$HOOK_EXIT" -ne 0 ]]; then
    fail "resilience: a failing cleaner surfaced as exit $HOOK_EXIT"
    return
  fi
  # A missing cleaner must be survivable too.
  rm -f "$SANDBOX/hooks/post-push-clean-caches.sh"
  run_hook Bash "git push origin dev"
  if [[ "$HOOK_EXIT" -ne 0 ]]; then
    fail "resilience: a missing cleaner surfaced as exit $HOOK_EXIT"
    return
  fi
  pass "a failing or missing cleaner never fails the tool call"
}

# --- T7: never yanks the cache out from under a running build ----------------
# The gauntlet is routinely run BACKGROUNDED while other Bash calls continue.
# `cargo clean` against a live `cargo build` target dir corrupts that build, so
# an in-flight build suppresses the reclaim — on both triggers.
t_defers_while_building() {
  make_sandbox 1000000 # below the floor: the disk trigger would otherwise fire
  stub_busy
  run_hook Bash "ls -la"
  if cleaner_ran; then
    fail "busy: disk-floor reclaim ran while a build was in flight"
    return
  fi
  make_sandbox 1000000
  stub_busy
  run_hook Bash "git push origin dev"
  if cleaner_ran; then
    fail "busy: push reclaim ran while a build was in flight"
    return
  fi
  pass "an in-flight build defers the reclaim on both triggers"
}

t_push_always_cleans
t_push_variants_clean
t_no_clean_when_roomy
t_disk_floor_cleans
t_ignores_other_tools
t_never_fails_the_call
t_defers_while_building

printf '\n%d passed, %d failed\n' "$PASS" "$FAIL"
if [[ "$FAIL" -gt 0 ]]; then
  printf 'FAILURES:\n'
  for f in "${FAILURES[@]}"; do printf '  - %s\n' "$f"; done
  exit 1
fi
