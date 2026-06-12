#!/usr/bin/env bash
# Tests for scripts/build-image-gate.sh — the path-detect helper used by
# .github/workflows/build-image.yml to decide whether the server container
# image needs a full rebuild on a given main-branch commit. Pure bash; no
# bats dependency. Mirrors the pattern from scripts/tests/release-agent-gate.test.sh.
#
# `crane` is mocked via a shim on $PATH that resolves the image-config
# response from a fixture file the test writes (no network access required).
#
# Run: ./scripts/tests/build-image-gate.test.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GATE="$SCRIPT_DIR/../build-image-gate.sh"

if [ ! -x "$GATE" ]; then
  echo "FAIL: $GATE not found or not executable" >&2
  exit 1
fi

PASS=0
FAIL=0
FAILURES=()

pass() { PASS=$((PASS + 1)); printf '  ok   %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAILURES+=("$1"); printf '  FAIL %s\n' "$1" >&2; }

# Build a temp git repo seeded with two commits — initial baseline and one
# follow-up. The follow-up's tree is what HEAD points at; PREV_SHA points at
# the initial commit. Caller mutates either side before invoking the gate.
make_repo() {
  REPO="$(mktemp -d)"
  cd "$REPO"
  git init --quiet --initial-branch=main
  git config user.email "test@example.com"
  git config user.name "Test"
  mkdir -p server/internal/api web/src agent/src deploy
  echo "fn main() {}" > agent/src/main.rs
  echo "package main" > server/internal/api/handlers.go
  printf '{"name": "web"}\n' > web/package.json
  echo "import './main';" > web/src/index.ts
  cat > Dockerfile <<'EOF'
FROM scratch
EOF
  git add .
  git commit --quiet -m "init"
  PREV_SHA="$(git rev-parse HEAD)"

  # A no-op follow-up commit so $PREV_SHA..$HEAD is non-empty by default —
  # individual tests overwrite this with their own follow-up.
  echo "// touch" >> agent/src/main.rs
  git add agent/src/main.rs
  git commit --quiet -m "agent-only follow-up"
  HEAD_SHA="$(git rev-parse HEAD)"
}

cleanup_repo() {
  if [ -n "${REPO:-}" ]; then
    rm -rf "$REPO"
    REPO=""
  fi
  return 0
}

# Invoked through the EXIT trap.
# shellcheck disable=SC2329
cleanup_mock() {
  if [ -n "${MOCK_DIR:-}" ]; then
    rm -rf "$MOCK_DIR"
    MOCK_DIR=""
  fi
  return 0
}

trap 'cleanup_repo; cleanup_mock' EXIT

# Install a crane shim on $PATH that prints the contents of $CRANE_FIXTURE
# for the `crane config <ref>` subcommand and exits non-zero otherwise.
# Mirrors how the gate consumes `crane config "$IMAGE:latest"`.
install_crane_mock() {
  MOCK_DIR="$(mktemp -d)"
  cat > "$MOCK_DIR/crane" <<'SHIM'
#!/usr/bin/env bash
# Test shim. Honors:
#   CRANE_FIXTURE  - path to a JSON file printed for `crane config <ref>`.
#   CRANE_FAIL=1   - exits non-zero (simulates "image not found").
if [ "${CRANE_FAIL:-}" = "1" ]; then
  echo "crane: not found" >&2
  exit 1
fi
case "${1:-}" in
  config)
    if [ -n "${CRANE_FIXTURE:-}" ] && [ -f "${CRANE_FIXTURE}" ]; then
      cat "${CRANE_FIXTURE}"
      exit 0
    fi
    echo "crane shim: no fixture set" >&2
    exit 1
    ;;
  *)
    echo "crane shim: unsupported subcommand: $*" >&2
    exit 2
    ;;
esac
SHIM
  chmod +x "$MOCK_DIR/crane"
  PATH="$MOCK_DIR:$PATH"
  export PATH
}

# Write a fixture whose revision label is $1.
write_fixture_with_revision() {
  CRANE_FIXTURE="$REPO/fixture.json"
  cat > "$CRANE_FIXTURE" <<EOF
{
  "config": {
    "Labels": {
      "org.opencontainers.image.revision": "$1",
      "org.opencontainers.image.source": "https://github.com/volchanskyi/opengate"
    }
  }
}
EOF
  export CRANE_FIXTURE
}

run_gate() {
  RESULT="$(IMAGE="ghcr.io/volchanskyi/opengate-server" HEAD_SHA="$HEAD_SHA" "$GATE")"
}

install_crane_mock

echo "build-image-gate:"

# --- Case 1: :latest does not exist → fall open to image_changed=true.
make_repo
RESULT="$(CRANE_FAIL=1 \
  IMAGE="ghcr.io/volchanskyi/opengate-server" \
  HEAD_SHA="$HEAD_SHA" \
  "$GATE" 2>/dev/null)"
if grep -q '^image_changed=true$' <<<"$RESULT" \
   && grep -q '^prev_sha=$' <<<"$RESULT"; then
  pass "no :latest in registry → image_changed=true, prev_sha empty"
else
  fail "no :latest → expected image_changed=true + empty prev_sha, got: $RESULT"
fi
cleanup_repo

# --- Case 2: :latest exists, only agent/ changed in range → image_changed=false.
make_repo
write_fixture_with_revision "$PREV_SHA"
# Default follow-up commit from make_repo already touches agent/ only.
if run_gate; then
  if grep -q '^image_changed=false$' <<<"$RESULT" \
     && grep -q "^prev_sha=${PREV_SHA}$" <<<"$RESULT"; then
    pass "agent-only change → image_changed=false"
  else
    fail "agent-only change → expected image_changed=false, got: $RESULT"
  fi
else
  fail "agent-only change → gate exited non-zero (output: $RESULT)"
fi
cleanup_repo

# --- Case 3: server/ changed in range → image_changed=true.
make_repo
write_fixture_with_revision "$PREV_SHA"
echo "// server tweak" >> server/internal/api/handlers.go
git add server/internal/api/handlers.go
git commit --quiet -m "fix(server): tweak"
HEAD_SHA="$(git rev-parse HEAD)"
if run_gate; then
  if grep -q '^image_changed=true$' <<<"$RESULT"; then
    pass "server/ changed → image_changed=true"
  else
    fail "server/ changed → expected image_changed=true, got: $RESULT"
  fi
else
  fail "server/ changed → gate exited non-zero (output: $RESULT)"
fi
cleanup_repo

# --- Case 4: web/ changed in range → image_changed=true.
make_repo
write_fixture_with_revision "$PREV_SHA"
echo "// web tweak" >> web/src/index.ts
git add web/src/index.ts
git commit --quiet -m "feat(web): tweak"
HEAD_SHA="$(git rev-parse HEAD)"
if run_gate; then
  if grep -q '^image_changed=true$' <<<"$RESULT"; then
    pass "web/ changed → image_changed=true"
  else
    fail "web/ changed → expected image_changed=true, got: $RESULT"
  fi
else
  fail "web/ changed → gate exited non-zero (output: $RESULT)"
fi
cleanup_repo

# --- Case 5: Dockerfile changed in range → image_changed=true.
make_repo
write_fixture_with_revision "$PREV_SHA"
cat >> Dockerfile <<'EOF'
LABEL foo=bar
EOF
git add Dockerfile
git commit --quiet -m "build: dockerfile tweak"
HEAD_SHA="$(git rev-parse HEAD)"
if run_gate; then
  if grep -q '^image_changed=true$' <<<"$RESULT"; then
    pass "Dockerfile changed → image_changed=true"
  else
    fail "Dockerfile changed → expected image_changed=true, got: $RESULT"
  fi
else
  fail "Dockerfile changed → gate exited non-zero (output: $RESULT)"
fi
cleanup_repo

# --- Case 6: deeply-nested server/ subdir change — pathspec must still match.
make_repo
write_fixture_with_revision "$PREV_SHA"
mkdir -p server/internal/relay/inner
echo "package relay" > server/internal/relay/inner/deep.go
git add server/internal/relay/inner/deep.go
git commit --quiet -m "feat(server): deep file"
HEAD_SHA="$(git rev-parse HEAD)"
if run_gate; then
  if grep -q '^image_changed=true$' <<<"$RESULT"; then
    pass "deep server/ subdir change → image_changed=true"
  else
    fail "deep server/ subdir change → expected image_changed=true, got: $RESULT"
  fi
else
  fail "deep server/ subdir change → gate exited non-zero (output: $RESULT)"
fi
cleanup_repo

# --- Case 7: :latest exists but the revision label is missing → fall open.
# Guards the silent-skip regression where a manually-pushed image (no metadata)
# would otherwise return prev_sha="" + image_changed=false.
make_repo
CRANE_FIXTURE="$REPO/fixture.json"
cat > "$CRANE_FIXTURE" <<'EOF'
{"config": {"Labels": {"foo": "bar"}}}
EOF
export CRANE_FIXTURE
if run_gate; then
  if grep -q '^image_changed=true$' <<<"$RESULT" \
     && grep -q '^prev_sha=$' <<<"$RESULT"; then
    pass "missing revision label → image_changed=true, prev_sha empty"
  else
    fail "missing label → expected image_changed=true + empty prev_sha, got: $RESULT"
  fi
else
  fail "missing label → gate exited non-zero (output: $RESULT)"
fi
cleanup_repo

# --- Case 8: required env vars missing → non-zero exit, no silent default.
make_repo
if "$GATE" >/dev/null 2>&1; then
  fail "missing IMAGE/HEAD_SHA → expected non-zero exit, got 0"
else
  pass "missing IMAGE/HEAD_SHA → non-zero exit"
fi
cleanup_repo

# --- Case 9: prev_sha resolved but not present in the repo (force-push /
# rebase scenario) → fall open to image_changed=true rather than diffing
# against a phantom baseline.
make_repo
write_fixture_with_revision "deadbeef0000000000000000000000000000dead"
if run_gate; then
  if grep -q '^image_changed=true$' <<<"$RESULT"; then
    pass "prev_sha unknown to repo → image_changed=true"
  else
    fail "prev_sha unknown → expected image_changed=true, got: $RESULT"
  fi
else
  fail "prev_sha unknown → gate exited non-zero (output: $RESULT)"
fi
cleanup_repo

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
