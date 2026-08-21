#!/usr/bin/env bash
# toolchain-parity.sh — keeps the workstation's language toolchains level with
# the ones CI resolves.
#
# Sourced by scripts/precommit-gauntlet.sh (and by
# scripts/tests/toolchain-parity.test.sh). NOT executable on its own — this is
# a library of bash functions.
#
# Why it exists: every CI toolchain pin in this repo floats. The Rust jobs ask
# dtolnay/rust-toolchain for `stable` (and fuzz.yml for `nightly`), and the web
# jobs ask actions/setup-node for major `24`; both resolve to the newest
# release at the moment the job runs, while a workstation resolves them once
# and then keeps whatever it downloaded. A workstation that has fallen behind
# runs a gauntlet that cannot see the lints, warnings and behaviour the CI run
# will — a green local gate and a red pipeline, with nothing in the diff to
# explain it.
#
# Go is pinned rather than floating: server/go.mod's `toolchain` directive is
# the single source of truth, every exact go-version in the workflows is held
# equal to it by scripts/tests/ci-govulncheck-go-version.test.sh, and
# GOTOOLCHAIN=auto makes a local `go` command in server/ re-exec into it. So
# the local check is that the re-exec actually lands on the pinned version.
#
# Functions exported (the parsers are pure so the tests can drive them with
# fixtures instead of a network round trip):
#   toolchain_rust_channel_current CHANNEL RUSTUP_CHECK_OUTPUT
#   toolchain_rust_expected CHANNEL RUSTUP_CHECK_OUTPUT
#   toolchain_gomod_pin GO_MOD_PATH
#   toolchain_go_effective GO_VERSION_OUTPUT
#   toolchain_node_latest_for_major MAJOR DIST_INDEX_JSON
#   toolchain_ci_node_major WORKFLOW_DIR
#   toolchain_versions_match LOCAL EXPECTED
#   toolchain_use_nvm_default
#   toolchain_parity_check REPO_ROOT       — the whole gate; logs to stderr

# toolchain_rust_channel_current CHANNEL OUTPUT — 0 when `rustup check` says
# CHANNEL is up to date. A channel rustup did not report is not installed, and
# counts as drift rather than as a pass.
toolchain_rust_channel_current() {
  local channel="$1" output="$2"
  local line
  line="$(printf '%s\n' "$output" | grep -E "^${channel}-[^ ]+ - " || true)"
  [ -n "$line" ] || return 1
  printf '%s\n' "$line" | grep -q ' - up to date'
}

# toolchain_rust_expected CHANNEL OUTPUT — the version CI would resolve for
# CHANNEL: the right-hand side of an "update available" line, or the installed
# version when there is nothing to update to.
toolchain_rust_expected() {
  local channel="$1" output="$2"
  local line
  line="$(printf '%s\n' "$output" | grep -E "^${channel}-[^ ]+ - " || true)"
  [ -n "$line" ] || return 0
  if printf '%s\n' "$line" | grep -q -- '->'; then
    printf '%s\n' "$line" | sed 's/.*-> *//' | awk '{print $1}'
    return 0
  fi
  printf '%s\n' "$line" | sed 's/.*up to date: *//' | awk '{print $1}'
}

# toolchain_gomod_pin GO_MOD_PATH — the Go version the module builds with:
# the `toolchain` directive when present, else the `go` directive.
toolchain_gomod_pin() {
  local gomod="$1" pin
  pin="$(grep -E '^toolchain go[0-9]' "$gomod" 2>/dev/null | awk '{print $2}' | head -1)"
  if [ -z "$pin" ]; then
    pin="go$(grep -E '^go [0-9]' "$gomod" 2>/dev/null | awk '{print $2}' | head -1)"
    [ "$pin" = "go" ] && pin=""
  fi
  printf '%s\n' "$pin"
}

# toolchain_go_effective GO_VERSION_OUTPUT — the goX.Y.Z token off the
# `go version` line, which under GOTOOLCHAIN=auto names the toolchain that
# actually ran. A "go: downloading …" banner on the way there names a
# download rather than what ran, so only the real line is read.
toolchain_go_effective() {
  printf '%s\n' "$1" | sed -n 's/^go version \(go[0-9][^ ]*\).*/\1/p' | head -1
}

# toolchain_node_latest_for_major MAJOR DIST_INDEX_JSON — the newest vMAJOR.x
# release in nodejs.org's release index, which is what setup-node installs for
# a bare major pin. The index is ordered newest-first.
toolchain_node_latest_for_major() {
  local major="$1" json="$2"
  printf '%s' "$json" \
    | grep -oE '"version"[[:space:]]*:[[:space:]]*"v'"$major"'\.[0-9]+\.[0-9]+"' \
    | head -1 \
    | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+'
}

# toolchain_ci_node_major WORKFLOW_DIR — the node major every workflow pins.
# Non-zero when they disagree: there is then no single version a workstation
# can match, and matching one job would mismatch the other.
toolchain_ci_node_major() {
  local dir="$1" majors
  majors="$(grep -rhoE "node-version:[[:space:]]*'[0-9]+'" "$dir" 2>/dev/null \
    | grep -oE "[0-9]+" | sort -u)"
  [ -n "$majors" ] || return 1
  [ "$(printf '%s\n' "$majors" | wc -l)" -eq 1 ] || return 1
  printf '%s\n' "$majors"
}

# toolchain_versions_match LOCAL EXPECTED — 0 only on an exact match. Newer is
# drift too: it means the workstation is testing something CI will not run.
toolchain_versions_match() {
  [ -n "$1" ] && [ "$1" = "$2" ]
}

# toolchain_use_nvm_default — select nvm's default alias in this shell.
# A long-lived session's PATH is a snapshot of the node that was installed when
# it started, and nvm keeps whatever version that PATH already points at, so a
# gauntlet inheriting it can run a version the default alias moved away from
# days ago. Best-effort: when nvm is absent or refuses, the parity check below
# is what reports the truth.
toolchain_use_nvm_default() {
  local nvm_dir="${NVM_DIR:-$HOME/.nvm}"
  [ -s "$nvm_dir/nvm.sh" ] || return 0
  # shellcheck source=/dev/null
  \. "$nvm_dir/nvm.sh" >/dev/null 2>&1 || return 0
  nvm use --silent default >/dev/null 2>&1 || return 0
}

# toolchain_parity_check REPO_ROOT — run the whole gate. Returns 0 when every
# local toolchain matches what CI resolves, 1 otherwise, printing the exact
# command that closes each gap.
toolchain_parity_check() {
  local root="$1" drift=0

  toolchain_use_nvm_default

  # --- Rust: stable for the build/lint jobs, nightly for fuzz.yml ----------
  local rustup_out
  if ! rustup_out="$(rustup check 2>&1)"; then
    echo "✗ 'rustup check' failed — cannot tell whether the local Rust toolchains match CI." >&2
    printf '%s\n' "$rustup_out" >&2
    return 1
  fi
  local channel
  for channel in stable nightly; do
    if ! toolchain_rust_channel_current "$channel" "$rustup_out"; then
      local want
      want="$(toolchain_rust_expected "$channel" "$rustup_out")"
      echo "✗ Rust $channel is behind CI${want:+ (CI resolves $want)}." >&2
      echo "    rustup update $channel" >&2
      drift=1
    fi
  done

  # --- Go: the module's own pin, reached through GOTOOLCHAIN=auto ----------
  local go_pin go_local
  go_pin="$(toolchain_gomod_pin "$root/server/go.mod")"
  go_local="$(toolchain_go_effective "$(cd "$root/server" && go version 2>&1)")"
  if ! toolchain_versions_match "$go_local" "$go_pin"; then
    echo "✗ Go in server/ runs ${go_local:-nothing}, but go.mod pins $go_pin." >&2
    echo "    unset GOTOOLCHAIN  # let the module's toolchain directive select the version" >&2
    drift=1
  fi

  # --- Node: the major the workflows pin, at its newest release -----------
  local node_major
  if ! node_major="$(toolchain_ci_node_major "$root/.github/workflows")"; then
    echo "✗ The workflows do not agree on one node-version, so there is nothing to match." >&2
    return 1
  fi
  local dist
  if ! dist="$(curl -sS --max-time 20 https://nodejs.org/dist/index.json 2>&1)"; then
    echo "✗ Could not read nodejs.org's release index — cannot tell whether Node matches CI." >&2
    printf '%s\n' "$dist" >&2
    return 1
  fi
  local node_want node_local
  node_want="$(toolchain_node_latest_for_major "$node_major" "$dist")"
  if [ -z "$node_want" ]; then
    echo "✗ nodejs.org lists no v$node_major release, which is the major the workflows pin." >&2
    return 1
  fi
  node_local="$(node --version 2>/dev/null)"
  if ! toolchain_versions_match "$node_local" "$node_want"; then
    echo "✗ Node is ${node_local:-missing}, but CI's 'node-version: $node_major' resolves to $node_want." >&2
    echo "    nvm install $node_major --reinstall-packages-from=current && nvm alias default $node_major" >&2
    drift=1
  fi

  return "$drift"
}
