#!/usr/bin/env bash
# Tests for scripts/lib/toolchain-parity.sh. Plain bash; no bats dependency.
# Run: ./scripts/tests/toolchain-parity.test.sh
#
# The library's job is to catch a local toolchain that is older than the one
# CI resolves. Every parser here is fed a fixture string, so the suite is
# hermetic: it never queries a release feed and never depends on what happens
# to be installed on the machine running it.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LIB="$SCRIPT_DIR/../lib/toolchain-parity.sh"

if [ ! -f "$LIB" ]; then
  echo "FAIL: $LIB not found" >&2
  exit 1
fi

# shellcheck source=/dev/null
. "$LIB"

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

check() {
  local name="$1" expected="$2" actual="$3"
  if [ "$expected" = "$actual" ]; then
    pass "$name"
  else
    fail "$name (expected '$expected', got '$actual')"
  fi
}

expect_rc() {
  local name="$1" want="$2"
  shift 2
  "$@" >/dev/null 2>&1
  local got=$?
  if [ "$got" -eq "$want" ]; then
    pass "$name"
  else
    fail "$name (expected exit $want, got $got)"
  fi
}

echo "rustup check parsing:"

UP_TO_DATE='stable-x86_64-unknown-linux-gnu - up to date: 1.98.0 (88d9e12ae 2026-08-18)
nightly-x86_64-unknown-linux-gnu - up to date: 1.100.0-nightly (8925ea358 2026-08-20)
rustup - up to date : 1.29.0'

STALE_NIGHTLY='stable-x86_64-unknown-linux-gnu - up to date: 1.98.0 (88d9e12ae 2026-08-18)
nightly-x86_64-unknown-linux-gnu - update available: 1.98.0-nightly (bd08c9e71 2026-06-25) -> 1.100.0-nightly (8925ea358 2026-08-20)
rustup - up to date : 1.29.0'

STALE_STABLE='stable-x86_64-unknown-linux-gnu - update available: 1.95.0 (59807616e 2026-04-14) -> 1.98.0 (88d9e12ae 2026-08-18)
nightly-x86_64-unknown-linux-gnu - up to date: 1.100.0-nightly (8925ea358 2026-08-20)
rustup - up to date : 1.29.0'

expect_rc "current stable passes" 0 toolchain_rust_channel_current stable "$UP_TO_DATE"
expect_rc "current nightly passes" 0 toolchain_rust_channel_current nightly "$UP_TO_DATE"
# The failure this whole gate exists for: CI resolves the channel fresh on
# every run, so a stale local stable is a lint CI sees and the gauntlet cannot.
expect_rc "stale stable fails" 1 toolchain_rust_channel_current stable "$STALE_STABLE"
expect_rc "stale nightly fails" 1 toolchain_rust_channel_current nightly "$STALE_NIGHTLY"
# A channel rustup never mentioned is not installed at all — that is drift too,
# not a pass by omission.
expect_rc "missing channel fails" 1 toolchain_rust_channel_current beta "$UP_TO_DATE"
# `rustup` itself appears in the same output as "rustup - up to date : 1.29.0".
# Matching on it instead of a toolchain line would report a green stable that
# was never checked.
expect_rc "rustup's own line is not a toolchain" 1 toolchain_rust_channel_current rustup "$STALE_STABLE"

check "stale stable reports the version CI would resolve" \
  "1.98.0" "$(toolchain_rust_expected stable "$STALE_STABLE")"
check "stale nightly reports the version CI would resolve" \
  "1.100.0-nightly" "$(toolchain_rust_expected nightly "$STALE_NIGHTLY")"

echo
echo "go.mod toolchain parsing:"

GOMOD_FIXTURE="$(mktemp)"
trap 'rm -f "$GOMOD_FIXTURE"' EXIT
cat >"$GOMOD_FIXTURE" <<'GOMOD'
module github.com/volchanskyi/opengate/server

go 1.26.0

toolchain go1.26.6
GOMOD

check "toolchain directive wins over the go directive" \
  "go1.26.6" "$(toolchain_gomod_pin "$GOMOD_FIXTURE")"

cat >"$GOMOD_FIXTURE" <<'GOMOD'
module github.com/volchanskyi/opengate/server

go 1.26.4
GOMOD

# Without a toolchain directive the `go` line is what the module builds with,
# so it is the version a local install has to match.
check "go directive is the pin when no toolchain directive exists" \
  "go1.26.4" "$(toolchain_gomod_pin "$GOMOD_FIXTURE")"

echo
echo "go version parsing:"

check "effective toolchain is read from go version output" \
  "go1.26.6" "$(toolchain_go_effective 'go version go1.26.6 linux/amd64')"
check "a go version banner with no version yields nothing" \
  "" "$(toolchain_go_effective 'go: downloading go1.26.6')"

echo
echo "node parsing and comparison:"

DIST_JSON='[{"version":"v25.1.0"},{"version":"v24.19.0"},{"version":"v24.14.0"},{"version":"v22.9.0"}]'

check "latest release of the CI major is picked, not the newest overall" \
  "v24.19.0" "$(toolchain_node_latest_for_major 24 "$DIST_JSON")"
check "a major with no releases yields nothing" \
  "" "$(toolchain_node_latest_for_major 23 "$DIST_JSON")"

expect_rc "matching node passes" 0 toolchain_versions_match "v24.19.0" "v24.19.0"
# Five patch releases behind is exactly how a CI-only failure hides.
expect_rc "older node fails" 1 toolchain_versions_match "v24.14.0" "v24.19.0"
expect_rc "newer node fails" 1 toolchain_versions_match "v24.20.0" "v24.19.0"

echo
echo "workflow node pin:"

WF_DIR="$(mktemp -d)"
trap 'rm -f "$GOMOD_FIXTURE"; rm -rf "$WF_DIR"' EXIT
cat >"$WF_DIR/ci.yml" <<'WF'
jobs:
  web:
    steps:
      - uses: actions/setup-node@v7
        with:
          node-version: '24'
WF
cat >"$WF_DIR/cd.yml" <<'WF'
jobs:
  build:
    steps:
      - uses: actions/setup-node@v7
        with:
          node-version: '24'
WF

check "the node major is read from the workflows" \
  "24" "$(toolchain_ci_node_major "$WF_DIR")"

# Two workflows disagreeing means there is no single answer to compare
# against, and picking either one would green a machine that is wrong for the
# other job.
cat >"$WF_DIR/cd.yml" <<'WF'
jobs:
  build:
    steps:
      - uses: actions/setup-node@v7
        with:
          node-version: '22'
WF
expect_rc "disagreeing workflow pins fail" 1 toolchain_ci_node_major "$WF_DIR"

echo
echo "nvm activation:"

# With no nvm to load there is nothing to select, and the parity check that
# follows it is what reports the truth — so the helper must leave the shell
# exactly as it found it rather than erroring out on a machine without nvm.
PATH_BEFORE="$PATH"
NVM_DIR="$(mktemp -d)" toolchain_use_nvm_default
expect_rc "no-op without nvm installed" 0 env NVM_DIR="$(mktemp -d)" bash -c ". $LIB; toolchain_use_nvm_default"
check "PATH is untouched when nvm is absent" "$PATH_BEFORE" "$PATH"

echo
if [ "$FAIL" -gt 0 ]; then
  echo "Summary: $PASS passed, $FAIL failed" >&2
  for f in "${FAILURES[@]}"; do echo "  - $f" >&2; done
  exit 1
fi
echo "Summary: $PASS passed, 0 failed"
