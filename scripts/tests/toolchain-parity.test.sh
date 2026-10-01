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

# What to do about the drift depends on which way it goes, and the gate used to
# name only one of the two. GOTOOLCHAIN=auto upgrades into the module's pin and
# never steps down into it, so a `go` on PATH that is newer than the pin runs
# instead of it — and the advice to unset a variable that was already unset sent
# a reader looking at the wrong thing. A workstation's snap rolled to 1.27.1
# while the project pinned 1.26.7, and that is the case it could not describe.
check "a local Go behind the pin is told to let auto fetch it" \
  "unset GOTOOLCHAIN" "$(toolchain_go_advice 'go1.26.4' 'go1.26.7')"
check "a missing local Go is told the same" \
  "unset GOTOOLCHAIN" "$(toolchain_go_advice '' 'go1.26.7')"
check "a local Go ahead of the pin is told to name the pin" \
  "GOTOOLCHAIN=go1.26.7" "$(toolchain_go_advice 'go1.27.1' 'go1.26.7')"

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
echo "pinned tools on this machine:"

# Every tool the gauntlet runs is asked what it is, through stand-ins answering
# the way each real tool words its version — a Go-built tool through the build
# record `go version -m` reads, since several of them print `dev`. A drifted one
# fails the check and names the command that installs the pin.
PIN_ROOT="$(mktemp -d)"
mkdir -p "$PIN_ROOT/scripts/lib" "$PIN_ROOT/bin"
cp "$SCRIPT_DIR/../lib/tool-versions.sh" "$PIN_ROOT/scripts/lib/tool-versions.sh"
cp "$SCRIPT_DIR/../require-tool.sh" "$PIN_ROOT/scripts/require-tool.sh"
# shellcheck source=../lib/tool-versions.sh
. "$PIN_ROOT/scripts/lib/tool-versions.sh"
stand_in() { # name, version line
  printf '#!/usr/bin/env bash\nprintf "%%s\\n" %q\n' "$2" >"$PIN_ROOT/bin/$1"
  chmod +x "$PIN_ROOT/bin/$1"
}
# A Go-built tool, and the build record a stand-in `go version -m` reads for it.
go_stand_in() { # name, module, version
  stand_in "$1" "dev"
  printf '%s\t%s\n' "$2" "$3" >"$PIN_ROOT/bin/$1.mod"
}
cat >"$PIN_ROOT/bin/go" <<'GO'
#!/usr/bin/env bash
if [ "$1 $2" = "version -m" ] && [ -f "$3.mod" ]; then
  printf '%s: go1.27.1\n' "$3"
  IFS=$'\t' read -r module version <"$3.mod"
  printf '\tpath\t%s\n\tmod\t%s\t%s\th1:x=\n' "$module" "$module" "$version"
  exit 0
fi
echo "go: unexpected call: $*" >&2
exit 1
GO
chmod +x "$PIN_ROOT/bin/go"
pinned_stand_ins() {
  stand_in jq "jq-$TOOL_VERSION_JQ"
  stand_in shellcheck "version: $TOOL_VERSION_SHELLCHECK"
  stand_in shfmt "v$TOOL_VERSION_SHFMT"
  stand_in age "v$TOOL_VERSION_AGE"
  stand_in age-keygen "v$TOOL_VERSION_AGE"
  stand_in zstd "*** Zstandard CLI (64-bit) v$TOOL_VERSION_ZSTD, by Yann Collet ***"
  go_stand_in govulncheck golang.org/x/vuln "v$TOOL_VERSION_GOVULNCHECK"
  go_stand_in staticcheck honnef.co/go/tools "v$TOOL_VERSION_STATICCHECK"
  go_stand_in gosec github.com/securego/gosec/v2 "v$TOOL_VERSION_GOSEC"
  go_stand_in go-arch-lint github.com/fe3dback/go-arch-lint "v$TOOL_VERSION_GO_ARCH_LINT"
  go_stand_in oapi-codegen github.com/oapi-codegen/oapi-codegen/v2 "v$TOOL_VERSION_OAPI_CODEGEN"
  stand_in cargo-audit "cargo-audit-audit $TOOL_VERSION_CARGO_AUDIT"
  stand_in cargo-deny "cargo-deny $TOOL_VERSION_CARGO_DENY"
  stand_in cargo-modules "cargo-modules $TOOL_VERSION_CARGO_MODULES"
  stand_in cargo-nextest "cargo-nextest $TOOL_VERSION_CARGO_NEXTEST (1358a2296 2026-02-22)"
  stand_in cargo-llvm-cov "cargo-llvm-cov $TOOL_VERSION_CARGO_LLVM_COV"
  stand_in checkov "$TOOL_VERSION_CHECKOV"
  stand_in yamllint "yamllint $TOOL_VERSION_YAMLLINT"
  stand_in conftest "Conftest: $TOOL_VERSION_CONFTEST"
  stand_in gitleaks "$TOOL_VERSION_GITLEAKS"
  stand_in hadolint "Haskell Dockerfile Linter $TOOL_VERSION_HADOLINT"
  stand_in helm "v$TOOL_VERSION_HELM+gcfd0749"
  stand_in kubeconform "v$TOOL_VERSION_KUBECONFORM"
  stand_in tflint "TFLint version $TOOL_VERSION_TFLINT"
  stand_in trivy "Version: $TOOL_VERSION_TRIVY"
  stand_in actionlint "v$TOOL_VERSION_ACTIONLINT"
  stand_in semgrep "$TOOL_VERSION_SEMGREP"
  stand_in pmat "pmat $TOOL_VERSION_PMAT"
}
pinned_check() {
  PATH="$PIN_ROOT/bin:$PATH" toolchain_pinned_tools_check "$PIN_ROOT"
}

pinned_stand_ins
out="$(pinned_check 2>&1)"
rc=$?
check "every pinned tool at its pin passes" "0" "$rc"
[ "$rc" -eq 0 ] || printf '%s\n' "$out" >&2

# The scanner that crashed CI under a newer Go, and that the workstation had
# quietly moved past by hand: it is held to the pin like everything else, and
# the refusal names the install that fixes it.
go_stand_in govulncheck golang.org/x/vuln v1.1.4
out="$(pinned_check 2>&1)"
rc=$?
check "a drifted govulncheck fails the check" "1" "$rc"
if grep -qF "go install golang.org/x/vuln/cmd/govulncheck@v$TOOL_VERSION_GOVULNCHECK" <<<"$out"; then
  pass "and names the pinned install that fixes it"
else
  fail "and names the pinned install that fixes it (got=[$out])"
fi

# Each tool the gauntlet runs, drifted on its own. The rest stay at their pins,
# so the refusal is about that tool and nothing else.
drift_case() { # tool, how it reports the drifted version
  pinned_stand_ins
  "$@"
  local tool="$2" got
  got="$(pinned_check 2>&1)"
  if [ $? -eq 1 ] && grep -qF -- "$tool is" <<<"$got"; then
    pass "a drifted $tool fails the check, naming it"
  else
    fail "a drifted $tool fails the check, naming it (got=[$got])"
  fi
}
drift_case go_stand_in staticcheck honnef.co/go/tools v0.0.1
drift_case go_stand_in gosec github.com/securego/gosec/v2 v2.26.1
drift_case go_stand_in go-arch-lint github.com/fe3dback/go-arch-lint v0.0.1
drift_case go_stand_in oapi-codegen github.com/oapi-codegen/oapi-codegen/v2 v0.0.1
drift_case stand_in cargo-audit "cargo-audit-audit 0.0.1"
drift_case stand_in cargo-deny "cargo-deny 0.0.1"
drift_case stand_in cargo-modules "cargo-modules 0.0.1"
drift_case stand_in cargo-nextest "cargo-nextest 0.0.1 (x 2026-01-01)"
drift_case stand_in cargo-llvm-cov "cargo-llvm-cov 0.0.1"
drift_case stand_in checkov "3.2.529"
drift_case stand_in yamllint "yamllint 0.0.1"
drift_case stand_in conftest "Conftest: 0.0.1"
drift_case stand_in gitleaks "0.0.1"
drift_case stand_in hadolint "Haskell Dockerfile Linter 0.0.1"
drift_case stand_in helm "v0.0.1+g0"
drift_case stand_in kubeconform "v0.0.1"
drift_case stand_in tflint "TFLint version 0.61.0"
drift_case stand_in trivy "Version: 0.69.3"
drift_case stand_in actionlint "v0.0.1"
drift_case stand_in semgrep "0.0.1"
drift_case stand_in pmat "pmat 0.0.1"

pinned_stand_ins
stand_in zstd "*** zstd command line interface 64-bits v1.4.8, by Yann Collet ***"
out="$(pinned_check 2>&1)"
rc=$?
check "a drifted zstd fails the check" "1" "$rc"
if grep -qF 'scripts/install-dump-tools.sh' <<<"$out"; then
  pass "and names the installer that fixes it"
else
  fail "and names the installer that fixes it (got=[$out])"
fi

pinned_stand_ins
rm -f "$PIN_ROOT/bin/age"
if PATH="$PIN_ROOT/bin:/usr/bin:/bin" toolchain_pinned_tools_check "$PIN_ROOT" >/dev/null 2>&1; then
  fail "a missing age fails the check"
else
  pass "a missing age fails the check"
fi

# require-tool.sh, which the Makefile targets call before they run a tool, holds
# the same line: present at another version is refused, not just absent.
pinned_stand_ins
stand_in tflint "TFLint version 0.61.0"
out="$(PATH="$PIN_ROOT/bin:$PATH" "$PIN_ROOT/scripts/require-tool.sh" tflint 2>&1)"
rc=$?
check "require-tool.sh refuses a tool present at another version" "1" "$rc"
if grep -qF "$TOOL_VERSION_TFLINT" <<<"$out" && grep -qF '0.61.0' <<<"$out"; then
  pass "and says which version it found and which is pinned"
else
  fail "and says which version it found and which is pinned (got=[$out])"
fi
expect_rc "require-tool.sh accepts the pinned version" 0 \
  env PATH="$PIN_ROOT/bin:$PATH" "$PIN_ROOT/scripts/require-tool.sh" trivy

# A copy that answers without a version is neither missing nor pinned, and the
# refusal says which — rather than ending without a word.
pinned_stand_ins
stand_in hadolint "no version in this answer"
out="$(PATH="$PIN_ROOT/bin:$PATH" "$PIN_ROOT/scripts/require-tool.sh" hadolint 2>&1)"
rc=$?
check "require-tool.sh refuses a tool that does not say its version" "1" "$rc"
if grep -qF 'hadolint is installed but did not say which version it is' <<<"$out"; then
  pass "and says the tool is there but unreadable, not missing"
else
  fail "and says the tool is there but unreadable, not missing (got=[$out])"
fi
rm -rf "$PIN_ROOT"

echo
if [ "$FAIL" -gt 0 ]; then
  echo "Summary: $PASS passed, $FAIL failed" >&2
  for f in "${FAILURES[@]}"; do echo "  - $f" >&2; done
  exit 1
fi
echo "Summary: $PASS passed, 0 failed"
