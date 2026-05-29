#!/usr/bin/env bash
# Tests for scripts/arch-lint-flip.sh. Plain bash; no bats dependency.
# Run: ./scripts/tests/arch-lint-flip.test.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
FLIP_SCRIPT="$SCRIPT_DIR/../arch-lint-flip.sh"

if [ ! -x "$FLIP_SCRIPT" ]; then
  echo "FAIL: $FLIP_SCRIPT not found or not executable" >&2
  exit 1
fi

PASS=0
FAIL=0
FAILURES=()

pass() { PASS=$((PASS + 1)); printf '  ok   %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAILURES+=("$1"); printf '  FAIL %s\n' "$1" >&2; }

assert_contains() {
  local name="$1" haystack="$2" needle="$3"
  if [[ "$haystack" == *"$needle"* ]]; then pass "$name"; else
    fail "$name (expected '$needle' in output: $haystack)"
  fi
}
assert_not_contains() {
  local name="$1" haystack="$2" needle="$3"
  if [[ "$haystack" != *"$needle"* ]]; then pass "$name"; else
    fail "$name (did not expect '$needle' in output: $haystack)"
  fi
}
assert_file_exists() {
  local name="$1" path="$2"
  if [ -f "$path" ]; then pass "$name"; else
    fail "$name (expected file: $path)"
  fi
}
assert_file_missing() {
  local name="$1" path="$2"
  if [ ! -f "$path" ]; then pass "$name"; else
    fail "$name (unexpected file: $path)"
  fi
}

# Build a temp repo seeded with the snapshot file the script reads.
make_fake_repo() {
  REPO="$(mktemp -d)"
  cd "$REPO"
  git init --quiet --initial-branch=dev
  git config user.email "test@example.com"
  git config user.name "Test"
  mkdir -p web
}

cleanup_repo() {
  if [ -n "${REPO:-}" ]; then
    rm -rf "$REPO"
    REPO=""
  fi
  return 0
}
trap 'cleanup_repo' EXIT

# ----------------------------------------------------------------------------
echo "depcruise gate — dirty (warn>0):"
make_fake_repo
echo '{"warn": 5}' > web/dependency-cruiser.snapshot.json
out=$("$FLIP_SCRIPT" --check)
assert_contains "reports dirty state"   "$out" "dirty (warn=5)"
assert_not_contains "no marker mentioned" "$out" "flipped"
"$FLIP_SCRIPT" --apply >/dev/null
assert_file_missing "--apply does not create marker on dirty" .claude/.markers/arch-lint-flipped/depcruise
cleanup_repo

# ----------------------------------------------------------------------------
echo "depcruise gate — eligible (warn=0), --check:"
make_fake_repo
echo '{"warn": 0}' > web/dependency-cruiser.snapshot.json
out=$("$FLIP_SCRIPT" --check)
assert_contains "reports eligible" "$out" "eligible to flip"
assert_file_missing "--check does not create marker" .claude/.markers/arch-lint-flipped/depcruise
cleanup_repo

# ----------------------------------------------------------------------------
echo "depcruise gate — --apply on eligible:"
make_fake_repo
echo '{"warn": 0}' > web/dependency-cruiser.snapshot.json
out=$("$FLIP_SCRIPT" --apply)
assert_file_exists "--apply creates marker" .claude/.markers/arch-lint-flipped/depcruise
assert_contains "summary mentions flip count" "$out" "1 gate(s) flipped"
# Idempotency: re-run should not error and report flipped state.
out2=$("$FLIP_SCRIPT" --apply)
assert_contains "idempotent — reports flipped" "$out2" "flipped (marker present)"
assert_contains "idempotent — no new flips" "$out2" "No gates flipped"
cleanup_repo

# ----------------------------------------------------------------------------
echo "depcruise gate — missing snapshot:"
make_fake_repo
out=$("$FLIP_SCRIPT" --check)
assert_contains "reports no snapshot" "$out" "no snapshot"
"$FLIP_SCRIPT" --apply >/dev/null
assert_file_missing "--apply does not create marker without snapshot" .claude/.markers/arch-lint-flipped/depcruise
cleanup_repo

# ----------------------------------------------------------------------------
# eslint-boundaries gate — config-severity state machine (added 2026-05-28
# for ADR-020 §5.4 flip). State derives from web/eslint.config.js severity
# token AND marker presence:
#   - severity 'warn' AND no marker  → eligible
#   - severity 'error' OR marker     → flipped
#   - missing config file            → no config
# `--apply` on eligible mutates the severity token to 'error' AND writes
# the marker; idempotent re-apply is a no-op.
# ----------------------------------------------------------------------------

write_eslint_config() {
  # $1 = severity token to embed (warn|error|<other>)
  local sev="$1"
  cat > web/eslint.config.js <<EOF
export default [{
  files: ['src/**/*.{ts,tsx}'],
  rules: {
    'boundaries/dependencies': ['${sev}', { default: 'disallow' }],
  },
}]
EOF
}

echo "eslint-boundaries gate — no config file:"
make_fake_repo
out=$("$FLIP_SCRIPT" --check)
assert_contains "reports no config" "$out" "no config"
"$FLIP_SCRIPT" --apply >/dev/null
assert_file_missing "--apply does not create marker without config" .claude/.markers/arch-lint-flipped/eslint-boundaries
cleanup_repo

echo "eslint-boundaries gate — eligible (severity=warn, no marker), --check:"
make_fake_repo
write_eslint_config "warn"
out=$("$FLIP_SCRIPT" --check)
assert_contains "reports eligible"        "$out" "eslint-boundaries"
assert_contains "eligible to flip phrase" "$out" "eligible to flip"
assert_file_missing "--check does not create marker" .claude/.markers/arch-lint-flipped/eslint-boundaries
# config must remain at warn after --check (no mutation)
if grep -q "'boundaries/dependencies': \['warn'" web/eslint.config.js; then
  pass "--check does not mutate config severity"
else
  fail "--check mutated config severity (expected 'warn')"
fi
cleanup_repo

echo "eslint-boundaries gate — --apply on eligible:"
make_fake_repo
write_eslint_config "warn"
out=$("$FLIP_SCRIPT" --apply)
assert_file_exists "--apply creates marker" .claude/.markers/arch-lint-flipped/eslint-boundaries
assert_contains "summary mentions flip count" "$out" "gate(s) flipped"
if grep -q "'boundaries/dependencies': \['error'" web/eslint.config.js; then
  pass "--apply mutates severity warn → error"
else
  fail "--apply did not mutate severity to 'error' (expected error)"
fi
# Idempotency: severity now 'error', marker exists → flipped, no further changes.
out2=$("$FLIP_SCRIPT" --apply)
assert_contains "idempotent — reports flipped" "$out2" "flipped"
cleanup_repo

echo "eslint-boundaries gate — severity already 'error' (reconcile):"
make_fake_repo
write_eslint_config "error"
out=$("$FLIP_SCRIPT" --check)
assert_contains "reports flipped on error severity" "$out" "flipped"
cleanup_repo

# ----------------------------------------------------------------------------
# cargo-deny gate — config-severity state machine (added 2026-05-28 for the
# second ADR-020 §5.4 flip). State derives from agent/deny.toml's
# `multiple-versions` AND `wildcards` severity tokens AND marker presence:
#   - both severities 'warn' AND no marker          → eligible
#   - both severities 'deny' OR marker present      → flipped
#   - missing config                                → no config
# `--apply` on eligible mutates both severities atomically and writes the
# marker. Same pattern as the eslint-boundaries gate.
# ----------------------------------------------------------------------------

write_deny_config() {
  # $1 = severity token to embed (warn|deny)
  local sev="$1"
  mkdir -p agent
  cat > agent/deny.toml <<EOF
[bans]
multiple-versions = "${sev}"
wildcards = "${sev}"
skip = []
EOF
}

echo "cargo-deny gate — no config file:"
make_fake_repo
out=$("$FLIP_SCRIPT" --check)
assert_contains "reports no config" "$out" "no config"
"$FLIP_SCRIPT" --apply >/dev/null
assert_file_missing "--apply does not create marker without config" .claude/.markers/arch-lint-flipped/cargo-deny
cleanup_repo

echo "cargo-deny gate — eligible (severities=warn, no marker), --check:"
make_fake_repo
write_deny_config "warn"
out=$("$FLIP_SCRIPT" --check)
assert_contains "reports eligible"        "$out" "cargo-deny"
assert_contains "eligible to flip phrase" "$out" "eligible to flip"
assert_file_missing "--check does not create marker" .claude/.markers/arch-lint-flipped/cargo-deny
if grep -q '^multiple-versions = "warn"' agent/deny.toml && grep -q '^wildcards = "warn"' agent/deny.toml; then
  pass "--check does not mutate config severities"
else
  fail "--check mutated config severities (expected both 'warn')"
fi
cleanup_repo

echo "cargo-deny gate — --apply on eligible:"
make_fake_repo
write_deny_config "warn"
out=$("$FLIP_SCRIPT" --apply)
assert_file_exists "--apply creates marker" .claude/.markers/arch-lint-flipped/cargo-deny
assert_contains "summary mentions flip count" "$out" "gate(s) flipped"
if grep -q '^multiple-versions = "deny"' agent/deny.toml && grep -q '^wildcards = "deny"' agent/deny.toml; then
  pass "--apply mutates both severities warn → deny"
else
  fail "--apply did not mutate severities to 'deny'"
fi
# Idempotency: severities now 'deny', marker exists → flipped, no further changes.
out2=$("$FLIP_SCRIPT" --apply)
assert_contains "idempotent — reports flipped" "$out2" "flipped"
cleanup_repo

echo "cargo-deny gate — severities already 'deny' (reconcile):"
make_fake_repo
write_deny_config "deny"
out=$("$FLIP_SCRIPT" --check)
assert_contains "reports flipped on deny severities" "$out" "flipped"
cleanup_repo

# ----------------------------------------------------------------------------
echo "remaining already-strict gates always listed:"
make_fake_repo
echo '{"warn": 0}' > web/dependency-cruiser.snapshot.json
out=$("$FLIP_SCRIPT" --check)
assert_contains "eslint-boundaries listed"  "$out" "eslint-boundaries"
assert_contains "cargo-deny listed"         "$out" "cargo-deny"
assert_contains "go-arch-lint listed"       "$out" "go-arch-lint"
assert_contains "cargo-modules listed"      "$out" "cargo-modules"
cleanup_repo

# ----------------------------------------------------------------------------
echo "unknown mode:"
make_fake_repo
echo '{"warn": 0}' > web/dependency-cruiser.snapshot.json
rc=0
"$FLIP_SCRIPT" --bogus >/dev/null 2>&1 || rc=$?
if [ "$rc" = "2" ]; then
  pass "rejects unknown mode with exit 2"
else
  fail "unknown mode rc=$rc (expected 2)"
fi
cleanup_repo

# ----------------------------------------------------------------------------
echo
echo "passed: $PASS    failed: $FAIL"
if [ "$FAIL" -ne 0 ]; then
  echo "FAILURES:"
  for f in "${FAILURES[@]}"; do echo "  - $f" >&2; done
  exit 1
fi
exit 0
