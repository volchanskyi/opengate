#!/usr/bin/env bash
# Keeps the workstation's language toolchains level with the ones CI resolves.
# Rust and Node float in CI while a workstation keeps what it downloaded; Go follows server/go.mod.

# toolchain_rust_channel_current CHANNEL OUTPUT returns 0 when `rustup check` reports the channel
# up to date; an unreported channel counts as drift.
toolchain_rust_channel_current() {
  local channel="$1" output="$2"
  local line
  line="$(printf '%s\n' "$output" | grep -E "^${channel}-[^ ]+ - " || true)"
  [ -n "$line" ] || return 1
  printf '%s\n' "$line" | grep -q ' - up to date'
}

# toolchain_rust_expected CHANNEL OUTPUT prints the version CI would resolve: the "update available"
# target, else the installed version.
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

# toolchain_gomod_pin GO_MOD_PATH prints the `toolchain` directive when present, else `go`'s.
toolchain_gomod_pin() {
  local gomod="$1" pin
  pin="$(grep -E '^toolchain go[0-9]' "$gomod" 2>/dev/null | awk '{print $2}' | head -1)"
  if [ -z "$pin" ]; then
    pin="go$(grep -E '^go [0-9]' "$gomod" 2>/dev/null | awk '{print $2}' | head -1)"
    [ "$pin" = "go" ] && pin=""
  fi
  printf '%s\n' "$pin"
}

# toolchain_go_effective GO_VERSION_OUTPUT prints the goX.Y.Z token of the `go version` line, which
# names the toolchain that ran under GOTOOLCHAIN=auto.
toolchain_go_effective() {
  printf '%s\n' "$1" | sed -n 's/^go version \(go[0-9][^ ]*\).*/\1/p' | head -1
}

# toolchain_go_advice LOCAL PIN prints the command that lands on the pinned toolchain;
# GOTOOLCHAIN=auto only upgrades, so a local Go newer than the pin needs the pin named.
toolchain_go_advice() {
  local local_version="$1" pin="$2"
  if [ -n "$local_version" ] \
    && [ "$(printf '%s\n%s\n' "${local_version#go}" "${pin#go}" | sort -V | head -1)" = "${pin#go}" ]; then
    printf 'GOTOOLCHAIN=%s\n' "$pin"
    return 0
  fi
  printf 'unset GOTOOLCHAIN\n'
}

# toolchain_node_latest_for_major MAJOR DIST_INDEX_JSON prints the first vMAJOR.x in the
# newest-first index, which is what setup-node installs for a bare major.
toolchain_node_latest_for_major() {
  local major="$1" json="$2"
  printf '%s' "$json" \
    | grep -oE '"version"[[:space:]]*:[[:space:]]*"v'"$major"'\.[0-9]+\.[0-9]+"' \
    | head -1 \
    | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+'
}

# toolchain_ci_node_major WORKFLOW_DIR prints the node major every workflow pins, and fails when
# they disagree since no single version then matches them all.
toolchain_ci_node_major() {
  local dir="$1" majors
  majors="$(grep -rhoE "node-version:[[:space:]]*'[0-9]+'" "$dir" 2>/dev/null \
    | grep -oE "[0-9]+" | sort -u)"
  [ -n "$majors" ] || return 1
  [ "$(printf '%s\n' "$majors" | wc -l)" -eq 1 ] || return 1
  printf '%s\n' "$majors"
}

# toolchain_versions_match LOCAL EXPECTED returns 0 only on an exact match; newer is drift too.
toolchain_versions_match() {
  [ -n "$1" ] && [ "$1" = "$2" ]
}

# toolchain_use_nvm_default selects nvm's default alias, since a long-lived session's PATH keeps the
# node it started with; it is best effort and the parity check reports the truth.
toolchain_use_nvm_default() {
  local nvm_dir="${NVM_DIR:-$HOME/.nvm}"
  [ -s "$nvm_dir/nvm.sh" ] || return 0
  # shellcheck source=/dev/null
  \. "$nvm_dir/nvm.sh" >/dev/null 2>&1 || return 0
  nvm use --silent default >/dev/null 2>&1 || return 0
}

toolchain_parity_check() {
  local root="$1" drift=0

  toolchain_use_nvm_default

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

  local go_pin go_local
  go_pin="$(toolchain_gomod_pin "$root/server/go.mod")"
  go_local="$(toolchain_go_effective "$(cd "$root/server" && go version 2>&1)")"
  if ! toolchain_versions_match "$go_local" "$go_pin"; then
    echo "✗ Go in server/ runs ${go_local:-nothing}, but go.mod pins $go_pin." >&2
    echo "    $(toolchain_go_advice "$go_local" "$go_pin")  # run what go.mod pins; put it in .env so every step of the run sees it" >&2
    drift=1
  fi

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

  # Pinned tools never float, so the check is whether PATH resolves the copy that was installed.
  if ! toolchain_pinned_tools_check "$root"; then
    drift=1
  fi

  return "$drift"
}

# The pinned tools the gauntlet runs, each checked by scripts/require-tool.sh; a manifest row for
# one missing here fails scripts/tests/tool-version-parity.test.sh.
TOOLCHAIN_PINNED_TOOLS=(
  jq shellcheck shfmt age age-keygen zstd
  govulncheck staticcheck gosec go-arch-lint oapi-codegen
  cargo-audit cargo-deny cargo-modules cargo-nextest cargo-llvm-cov
  checkov yamllint conftest gitleaks hadolint helm kubeconform tflint trivy
  actionlint semgrep pmat
)

toolchain_pinned_tools_check() {
  local root="$1" bad=0 tool out
  for tool in "${TOOLCHAIN_PINNED_TOOLS[@]}"; do
    if ! out="$("$root/scripts/require-tool.sh" "$tool" 2>&1)"; then
      bad=1
      printf '%s\n' "${out//ERROR: /✗ }" >&2
    fi
  done
  if [ "$bad" -ne 0 ]; then
    echo "  The copy PATH resolves is the one checked: an older copy earlier on PATH is drift too." >&2
  fi
  return "$bad"
}
