#!/usr/bin/env bash
# Tests for .claude/hooks/post-push-clean-caches.sh — the best-effort post-push
# local cache reclaim. Plain bash; no bats dependency.
# Run: ./scripts/tests/post-push-clean-caches.test.sh
#
# Each test builds a throwaway repo root with an agent/ workspace and stubs
# cargo/go/docker on PATH so we assert which cleanup commands ran without
# touching the real toolchain or Docker daemon.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
HOOK="$PROJECT_ROOT/.claude/hooks/post-push-clean-caches.sh"

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

# Build a fresh fake repo root with an agent workspace and a stub-tool bin dir.
# Echoes the root path. Sets STUB_LOG (global) to the command-trace file.
make_fixture() {
  local root
  root="$(mktemp -d)"
  mkdir -p "$root/agent" "$root/bin"
  printf '[package]\nname = "x"\n' >"$root/agent/Cargo.toml"
  STUB_LOG="$root/trace.log"
  : >"$STUB_LOG"
  for tool in cargo go docker; do
    cat >"$root/bin/$tool" <<EOF
#!/usr/bin/env bash
printf '%s %s\n' "$tool" "\$*" >>"$STUB_LOG"
exit 0
EOF
    chmod +x "$root/bin/$tool"
  done
  printf '%s' "$root"
}

# Run the hook from inside the fixture root with the stub bin on PATH.
# Args: root [env assignments...]. Sets HOOK_EXIT.
run_hook() {
  local root="$1"
  shift
  (
    cd "$root" || exit 99
    env -u CI -u GITHUB_ACTIONS "PATH=$root/bin:$PATH" "$@" bash "$HOOK" "$root"
  ) >/dev/null 2>&1
  HOOK_EXIT=$?
}

# --- T1: runs the documented core cleanup with all tools present -------------
t_runs_core_cleanup() {
  local root
  root="$(make_fixture)"
  STUB_LOG="$root/trace.log"
  run_hook "$root"
  if [[ "$HOOK_EXIT" -ne 0 ]]; then
    fail "core cleanup: expected exit 0, got $HOOK_EXIT"
    return
  fi
  grep -q '^cargo clean' "$STUB_LOG" || {
    fail "core cleanup: cargo clean not invoked"
    return
  }
  grep -q '^go clean -cache' "$STUB_LOG" || {
    fail "core cleanup: go clean -cache not invoked"
    return
  }
  grep -q '^docker volume prune -f' "$STUB_LOG" || {
    fail "core cleanup: docker volume prune not invoked"
    return
  }
  pass "runs cargo clean + go clean -cache + docker volume prune -f"
}

# --- T2: no-op under CI -------------------------------------------------------
t_skips_in_ci() {
  local root
  root="$(make_fixture)"
  STUB_LOG="$root/trace.log"
  run_hook "$root" CI=1
  if [[ "$HOOK_EXIT" -ne 0 ]]; then
    fail "CI skip: expected exit 0, got $HOOK_EXIT"
    return
  fi
  if [[ -s "$STUB_LOG" ]]; then
    fail "CI skip: expected no cleanup commands, got: $(tr '\n' ';' <"$STUB_LOG")"
    return
  fi
  pass "skips all cleanup under CI"
}

# --- T3: explicit opt-out ----------------------------------------------------
t_opt_out() {
  local root
  root="$(make_fixture)"
  STUB_LOG="$root/trace.log"
  run_hook "$root" OPENGATE_SKIP_CACHE_CLEAN=1
  if [[ "$HOOK_EXIT" -ne 0 ]]; then
    fail "opt-out: expected exit 0, got $HOOK_EXIT"
    return
  fi
  if [[ -s "$STUB_LOG" ]]; then
    fail "opt-out: expected no cleanup commands, got: $(tr '\n' ';' <"$STUB_LOG")"
    return
  fi
  pass "OPENGATE_SKIP_CACHE_CLEAN disables the hook"
}

# --- T4: missing tool is non-fatal ------------------------------------------
t_missing_tool_non_fatal() {
  local root
  root="$(make_fixture)"
  STUB_LOG="$root/trace.log"
  rm -f "$root/bin/docker" # docker absent
  run_hook "$root"
  if [[ "$HOOK_EXIT" -ne 0 ]]; then
    fail "missing tool: expected exit 0, got $HOOK_EXIT"
    return
  fi
  grep -q '^cargo clean' "$STUB_LOG" || {
    fail "missing tool: cargo clean should still run"
    return
  }
  if grep -q '^docker' "$STUB_LOG"; then
    fail "missing tool: docker must not be invoked when absent"
    return
  fi
  pass "missing docker is a non-fatal skip; other cleanup still runs"
}

# --- T5: never touches the dependency caches (registry / module cache) -------
t_never_clears_dep_caches() {
  # Behavioral invariants (assert the commands, not explanatory comments):
  # the Go module cache wipe flag must never appear...
  if grep -Eq 'go[[:space:]]+clean[^|&]*-modcache|[[:space:]]-modcache' "$HOOK"; then
    fail "safety: hook runs 'go clean -modcache' (wipes the module cache)"
    return
  fi
  # ...and nothing may rm the cargo registry or the Go module cache.
  if grep -Eq 'rm[[:space:]].*(registry|pkg/mod|GOMODCACHE)' "$HOOK"; then
    fail "safety: hook rm's a dependency cache it must never clear"
    return
  fi
  # And cargo clean must be scoped to the agent workspace, not a registry wipe.
  if ! grep -q 'agent' "$HOOK"; then
    fail "safety: cargo clean is not scoped to the agent workspace"
    return
  fi
  pass "never clears cargo registry / Go module cache"
}

t_runs_core_cleanup
t_skips_in_ci
t_opt_out
t_missing_tool_non_fatal
t_never_clears_dep_caches

printf '\n%d passed, %d failed\n' "$PASS" "$FAIL"
if [[ "$FAIL" -gt 0 ]]; then
  printf 'FAILURES:\n'
  for f in "${FAILURES[@]}"; do printf '  - %s\n' "$f"; done
  exit 1
fi
