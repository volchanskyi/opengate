#!/usr/bin/env bash
# Tests for scripts/release-agent-gate.sh — the path-detect helper used by
# .github/workflows/release-agent.yml to decide whether the agent matrix
# build should run for a given v* tag. Pure bash; no bats dependency.
# Mirrors the pattern from scripts/tests/tdd-check.test.sh.
#
# Run: ./scripts/tests/release-agent-gate.test.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GATE="$SCRIPT_DIR/../release-agent-gate.sh"

if [ ! -x "$GATE" ]; then
  echo "FAIL: $GATE not found or not executable" >&2
  exit 1
fi

PASS=0
FAIL=0
FAILURES=()

pass() { PASS=$((PASS + 1)); printf '  ok   %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAILURES+=("$1"); printf '  FAIL %s\n' "$1" >&2; }

# Build a temp git repo seeded with a single initial commit. Sets REPO and
# cd's into it. Caller must trap-cleanup.
make_repo() {
  REPO="$(mktemp -d)"
  cd "$REPO"
  git init --quiet --initial-branch=main
  git config user.email "test@example.com"
  git config user.name "Test"
  mkdir -p agent/src server/internal
  echo "fn main() {}" > agent/src/main.rs
  echo "package main" > server/internal/server.go
  git add .
  git commit --quiet -m "init"
}

cleanup_repo() {
  if [ -n "${REPO:-}" ]; then
    rm -rf "$REPO"
    REPO=""
  fi
  return 0
}
trap 'cleanup_repo' EXIT

# Helper: run the gate against $TAG and capture key=value lines into $RESULT.
run_gate() {
  local tag="$1"
  RESULT="$("$GATE" "$tag")"
}

echo "release-agent-gate:"

# --- Case 1: no prior v* tag (first release) — must build.
make_repo
git tag v0.1.0
if run_gate v0.1.0; then
  if grep -q '^agent_changed=true$' <<<"$RESULT" \
     && grep -q '^prev_tag=$' <<<"$RESULT"; then
    pass "no prior v* tag → agent_changed=true, prev_tag empty"
  else
    fail "no prior v* tag → expected agent_changed=true + empty prev_tag, got: $RESULT"
  fi
else
  fail "no prior v* tag → gate exited non-zero (output: $RESULT)"
fi
cleanup_repo

# --- Case 2: prior tag exists, agent/ unchanged in range — must skip.
make_repo
git tag v0.1.0
# Land a non-agent commit and tag it.
echo "// server-only change" >> server/internal/server.go
git add server/internal/server.go
git commit --quiet -m "fix(server): tweak"
git tag v0.1.1
if run_gate v0.1.1; then
  if grep -q '^agent_changed=false$' <<<"$RESULT" \
     && grep -q '^prev_tag=v0.1.0$' <<<"$RESULT"; then
    pass "agent/ unchanged → agent_changed=false, prev_tag=v0.1.0"
  else
    fail "agent/ unchanged → expected agent_changed=false + prev_tag=v0.1.0, got: $RESULT"
  fi
else
  fail "agent/ unchanged → gate exited non-zero (output: $RESULT)"
fi
cleanup_repo

# --- Case 3: prior tag exists, agent/ changed in range — must build.
make_repo
git tag v0.1.0
echo "// agent change" >> agent/src/main.rs
git add agent/src/main.rs
git commit --quiet -m "fix(agent): tweak"
git tag v0.2.0
if run_gate v0.2.0; then
  if grep -q '^agent_changed=true$' <<<"$RESULT" \
     && grep -q '^prev_tag=v0.1.0$' <<<"$RESULT"; then
    pass "agent/ changed → agent_changed=true, prev_tag=v0.1.0"
  else
    fail "agent/ changed → expected agent_changed=true + prev_tag=v0.1.0, got: $RESULT"
  fi
else
  fail "agent/ changed → gate exited non-zero (output: $RESULT)"
fi
cleanup_repo

# --- Case 4: tag arg missing → must exit non-zero with usage message.
make_repo
git tag v0.1.0
if "$GATE" >/dev/null 2>&1; then
  fail "missing tag arg → expected non-zero exit, got 0"
else
  pass "missing tag arg → non-zero exit"
fi
cleanup_repo

# --- Case 5: unknown tag → must exit non-zero (don't silently default).
make_repo
git tag v0.1.0
if "$GATE" v9.9.9 >/dev/null 2>&1; then
  fail "unknown tag → expected non-zero exit, got 0"
else
  pass "unknown tag → non-zero exit"
fi
cleanup_repo

# --- Case 6: prior tag exists, agent/ changed but only inside a subdir
#     deeper than the top-level — pathspec 'agent/**' must still match.
make_repo
git tag v0.1.0
mkdir -p agent/crates/mesh-agent/src
echo "fn x() {}" > agent/crates/mesh-agent/src/lib.rs
git add agent/crates/mesh-agent/src/lib.rs
git commit --quiet -m "feat(agent): deep file"
git tag v0.2.0
if run_gate v0.2.0; then
  if grep -q '^agent_changed=true$' <<<"$RESULT"; then
    pass "deep agent/ subdir change → agent_changed=true"
  else
    fail "deep agent/ subdir change → expected agent_changed=true, got: $RESULT"
  fi
else
  fail "deep agent/ subdir change → gate exited non-zero (output: $RESULT)"
fi
cleanup_repo

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
