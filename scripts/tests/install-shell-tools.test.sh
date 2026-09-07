#!/usr/bin/env bash
# Tests for the pinned ShellCheck and shfmt provisioner.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
INSTALLER="$REPO_ROOT/scripts/install-shell-tools.sh"

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

expect_failure() {
  local name="$1"
  local expected="$2"
  shift 2
  local output

  if output="$("$@" 2>&1)"; then
    fail "$name exits non-zero"
    return
  fi
  pass "$name exits non-zero"

  if grep -qF "$expected" <<<"$output"; then
    pass "$name reports $expected"
  else
    fail "$name reports $expected (got: $output)"
  fi
}

echo "install-shell-tools:"

if [ -x "$INSTALLER" ]; then
  pass "installer exists and is executable"
else
  fail "installer exists and is executable"
fi

# The installer takes its versions from the manifest rather than spelling them
# out, so the workstation and CI cannot provision different ones. Asserting the
# literal here would put the version back in a second file — which is the defect
# the manifest exists to close.
# shellcheck source=../lib/tool-versions.sh
. "$REPO_ROOT/scripts/lib/tool-versions.sh"

if grep -q 'lib/tool-versions.sh' "$INSTALLER" 2>/dev/null; then
  pass "the installer reads the version manifest"
else
  fail "the installer reads the version manifest"
fi

for tool in SHELLCHECK SHFMT JQ; do
  want="$(tool_version "$tool")"
  if [ -n "$want" ]; then
    pass "the manifest pins $tool at $want"
  else
    fail "the manifest pins $tool"
  fi
done

# jq is provisioned here for the same reason the gates' tools are: 33 scripts
# read JSON with it, and the runner image's copy and the workstation's had
# disagreed about how a number renders.
if grep -q 'JQ_VERSION' "$INSTALLER" 2>/dev/null \
  && grep -q 'jq_version_of' "$INSTALLER" 2>/dev/null; then
  pass "the installer provisions jq and reads back what it installed"
else
  fail "the installer provisions jq and reads back what it installed"
fi

if [ -x "$INSTALLER" ]; then
  expect_failure \
    "unsupported architecture" \
    "unsupported platform" \
    env \
    HOME="$TMP_DIR/unsupported-home" \
    SHELL_TOOLS_UNAME_S="Linux" \
    SHELL_TOOLS_UNAME_M="sparc64" \
    SHELL_TOOLS_CACHE="$TMP_DIR/unsupported-cache" \
    SHELL_TOOLS_BIN_DIR="$TMP_DIR/unsupported-bin" \
    "$INSTALLER"

  mkdir -p "$TMP_DIR/assets"
  printf 'not the release asset\n' >"$TMP_DIR/assets/shellcheck-v0.11.0.linux.x86_64.tar.xz"
  mkdir -p "$TMP_DIR/old-bin"
  cat >"$TMP_DIR/old-bin/shellcheck" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' 'version: 0.8.0'
EOF
  cat >"$TMP_DIR/old-bin/shfmt" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' 'v3.0.0'
EOF
  chmod +x "$TMP_DIR/old-bin/shellcheck" "$TMP_DIR/old-bin/shfmt"
  expect_failure \
    "checksum mismatch" \
    "checksum mismatch" \
    env \
    PATH="$TMP_DIR/old-bin:$PATH" \
    HOME="$TMP_DIR/checksum-home" \
    SHELL_TOOLS_UNAME_S="Linux" \
    SHELL_TOOLS_UNAME_M="x86_64" \
    SHELL_TOOLS_CACHE="$TMP_DIR/checksum-cache" \
    SHELL_TOOLS_BIN_DIR="$TMP_DIR/checksum-bin" \
    SHELLCHECK_BASE_URL="file://$TMP_DIR/assets" \
    "$INSTALLER"

  mkdir -p "$TMP_DIR/exact-bin"
  cat >"$TMP_DIR/exact-bin/shellcheck" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' 'ShellCheck - shell script analysis tool' 'version: 0.11.0'
EOF
  cat >"$TMP_DIR/exact-bin/shfmt" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' 'v3.13.1'
EOF
  cat >"$TMP_DIR/exact-bin/curl" <<EOF
#!/usr/bin/env bash
touch "$TMP_DIR/curl-called"
exit 99
EOF
  chmod +x "$TMP_DIR/exact-bin/shellcheck" "$TMP_DIR/exact-bin/shfmt" "$TMP_DIR/exact-bin/curl"

  if output="$(
    PATH="$TMP_DIR/exact-bin:$PATH" \
      HOME="$TMP_DIR/exact-home" \
      SHELL_TOOLS_CACHE="$TMP_DIR/exact-cache" \
      SHELL_TOOLS_BIN_DIR="$TMP_DIR/exact-links" \
      "$INSTALLER" 2>&1
  )" \
    && PATH="$TMP_DIR/exact-bin:$PATH" \
      HOME="$TMP_DIR/exact-home" \
      SHELL_TOOLS_CACHE="$TMP_DIR/exact-cache" \
      SHELL_TOOLS_BIN_DIR="$TMP_DIR/exact-links" \
      "$INSTALLER" >/dev/null 2>&1 \
    && grep -qF "already present" <<<"$output" \
    && [ -x "$TMP_DIR/exact-links/shellcheck" ] \
    && [ -x "$TMP_DIR/exact-links/shfmt" ] \
    && [ ! -e "$TMP_DIR/curl-called" ]; then
    pass "exact versions populate the requested bin directory without network"
  else
    fail "exact versions populate the requested bin directory without network"
  fi
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ]
