#!/usr/bin/env bash
# Provision the pinned ShellCheck, shfmt and jq binaries that the shell-quality
# gates and the repository's scripts run on.
#
# CI and the workstation both run this script, and the versions come from
# scripts/lib/tool-versions.sh, so neither side can be on a tool the other is
# not. jq is here for that reason rather than for the gates: 33 scripts read
# JSON with it and the two sides had silently disagreed about which one, which
# renders numbers differently and failed a nightly drill's test on every CI run
# while passing on the workstation.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/tool-versions.sh
. "$SCRIPT_DIR/lib/tool-versions.sh"

SHELLCHECK_VERSION="$TOOL_VERSION_SHELLCHECK"
SHFMT_VERSION="$TOOL_VERSION_SHFMT"
JQ_VERSION="$TOOL_VERSION_JQ"

SHELLCHECK_BASE_URL="${SHELLCHECK_BASE_URL:-https://github.com/koalaman/shellcheck/releases/download/v${SHELLCHECK_VERSION}}"
SHFMT_BASE_URL="${SHFMT_BASE_URL:-https://github.com/mvdan/sh/releases/download/v${SHFMT_VERSION}}"
JQ_BASE_URL="${JQ_BASE_URL:-https://github.com/jqlang/jq/releases/download/jq-${JQ_VERSION}}"
TOOLS_CACHE="${SHELL_TOOLS_CACHE:-${XDG_DATA_HOME:-$HOME/.local/share}/opengate/shell-tools}"
BIN_DIR="${SHELL_TOOLS_BIN_DIR:-$HOME/.local/bin}"
OS_NAME="${SHELL_TOOLS_UNAME_S:-$(uname -s)}"
ARCH_NAME="${SHELL_TOOLS_UNAME_M:-$(uname -m)}"

log() { printf '[install-shell-tools] %s\n' "$1" >&2; }

shellcheck_version_of() {
  "$1" --version 2>/dev/null | awk '/^version:/ { print $2; exit }'
}

shfmt_version_of() {
  "$1" --version 2>/dev/null | sed -n '1{s/^v//;p;}'
}

jq_version_of() {
  "$1" --version 2>/dev/null | sed -n '1{s/^jq-//;p;}'
}

case "${OS_NAME}:${ARCH_NAME}" in
  Linux:x86_64 | Linux:amd64)
    shellcheck_arch="x86_64"
    shellcheck_sha="8c3be12b05d5c177a04c29e3c78ce89ac86f1595681cab149b65b97c4e227198"
    shfmt_arch="amd64"
    shfmt_sha="fb096c5d1ac6beabbdbaa2874d025badb03ee07929f0c9ff67563ce8c75398b1"
    jq_arch="amd64"
    jq_sha="5942c9b0934e510ee61eb3e30273f1b3fe2590df93933a93d7c58b81d19c8ff5"
    ;;
  Linux:aarch64 | Linux:arm64)
    shellcheck_arch="aarch64"
    shellcheck_sha="12b331c1d2db6b9eb13cfca64306b1b157a86eb69db83023e261eaa7e7c14588"
    shfmt_arch="arm64"
    shfmt_sha="32d92acaa5cd8abb29fc49dac123dc412442d5713967819d8af2c29f1b3857c7"
    jq_arch="arm64"
    jq_sha="4dd2d8a0661df0b22f1bb9a1f9830f06b6f3b8f7d91211a1ef5d7c4f06a8b4a5"
    ;;
  *)
    log "ERROR: unsupported platform ${OS_NAME}/${ARCH_NAME}"
    exit 1
    ;;
esac

shellcheck_path="$(command -v shellcheck 2>/dev/null || true)"
shellcheck_ok=false
if [ -n "$shellcheck_path" ] \
  && [ "$(shellcheck_version_of "$shellcheck_path")" = "$SHELLCHECK_VERSION" ]; then
  shellcheck_ok=true
fi

shfmt_path="$(command -v shfmt 2>/dev/null || true)"
shfmt_ok=false
if [ -n "$shfmt_path" ] \
  && [ "$(shfmt_version_of "$shfmt_path")" = "$SHFMT_VERSION" ]; then
  shfmt_ok=true
fi

jq_path="$(command -v jq 2>/dev/null || true)"
jq_ok=false
if [ -n "$jq_path" ] \
  && [ "$(jq_version_of "$jq_path")" = "$JQ_VERSION" ]; then
  jq_ok=true
fi

if "$shellcheck_ok" && "$shfmt_ok" && "$jq_ok"; then
  mkdir -p "$BIN_DIR"
  if [ "$shellcheck_path" != "$BIN_DIR/shellcheck" ]; then
    ln -sfn "$shellcheck_path" "$BIN_DIR/shellcheck"
  fi
  if [ "$shfmt_path" != "$BIN_DIR/shfmt" ]; then
    ln -sfn "$shfmt_path" "$BIN_DIR/shfmt"
  fi
  if [ "$jq_path" != "$BIN_DIR/jq" ]; then
    ln -sfn "$jq_path" "$BIN_DIR/jq"
  fi
  log "ShellCheck ${SHELLCHECK_VERSION}, shfmt ${SHFMT_VERSION} and jq ${JQ_VERSION} already present — nothing to do."
  exit 0
fi

command -v curl >/dev/null 2>&1 || {
  log "ERROR: curl is required"
  exit 1
}
command -v sha256sum >/dev/null 2>&1 || {
  log "ERROR: sha256sum is required"
  exit 1
}

mkdir -p "$TOOLS_CACHE" "$BIN_DIR"
work_dir="$(mktemp -d "$TOOLS_CACHE/install.XXXXXX")"
trap 'rm -rf "$work_dir"' EXIT

download_and_verify() {
  local url="$1"
  local destination="$2"
  local expected_sha="$3"
  local actual_sha

  curl --fail --location --retry 3 --silent --show-error "$url" -o "$destination"
  actual_sha="$(sha256sum "$destination" | awk '{print $1}')"
  if [ "$actual_sha" != "$expected_sha" ]; then
    log "ERROR: checksum mismatch for $(basename "$destination"): got $actual_sha, expected $expected_sha"
    exit 1
  fi
}

if ! "$shellcheck_ok"; then
  shellcheck_asset="shellcheck-v${SHELLCHECK_VERSION}.linux.${shellcheck_arch}.tar.xz"
  shellcheck_archive="$work_dir/$shellcheck_asset"
  shellcheck_dir="$TOOLS_CACHE/shellcheck-${SHELLCHECK_VERSION}"

  log "downloading ${shellcheck_asset}"
  download_and_verify "$SHELLCHECK_BASE_URL/$shellcheck_asset" "$shellcheck_archive" "$shellcheck_sha"
  rm -rf "$shellcheck_dir"
  mkdir -p "$shellcheck_dir"
  tar -xJf "$shellcheck_archive" -C "$work_dir"
  install -m 0755 \
    "$work_dir/shellcheck-v${SHELLCHECK_VERSION}/shellcheck" \
    "$shellcheck_dir/shellcheck"
  ln -sfn "$shellcheck_dir/shellcheck" "$BIN_DIR/shellcheck"
fi

if ! "$shfmt_ok"; then
  shfmt_asset="shfmt_v${SHFMT_VERSION}_linux_${shfmt_arch}"
  shfmt_download="$work_dir/$shfmt_asset"
  shfmt_dir="$TOOLS_CACHE/shfmt-${SHFMT_VERSION}"

  log "downloading ${shfmt_asset}"
  download_and_verify "$SHFMT_BASE_URL/$shfmt_asset" "$shfmt_download" "$shfmt_sha"
  rm -rf "$shfmt_dir"
  mkdir -p "$shfmt_dir"
  install -m 0755 "$shfmt_download" "$shfmt_dir/shfmt"
  ln -sfn "$shfmt_dir/shfmt" "$BIN_DIR/shfmt"
fi

if ! "$jq_ok"; then
  jq_asset="jq-linux-${jq_arch}"
  jq_download="$work_dir/$jq_asset"
  jq_dir="$TOOLS_CACHE/jq-${JQ_VERSION}"

  log "downloading ${jq_asset}"
  download_and_verify "$JQ_BASE_URL/$jq_asset" "$jq_download" "$jq_sha"
  rm -rf "$jq_dir"
  mkdir -p "$jq_dir"
  install -m 0755 "$jq_download" "$jq_dir/jq"
  ln -sfn "$jq_dir/jq" "$BIN_DIR/jq"
fi

installed_shellcheck="$BIN_DIR/shellcheck"
installed_shfmt="$BIN_DIR/shfmt"
if [ "$(shellcheck_version_of "$installed_shellcheck")" != "$SHELLCHECK_VERSION" ]; then
  log "ERROR: ShellCheck post-install version check failed"
  exit 1
fi
if [ "$(shfmt_version_of "$installed_shfmt")" != "$SHFMT_VERSION" ]; then
  log "ERROR: shfmt post-install version check failed"
  exit 1
fi
if [ "$(jq_version_of "$BIN_DIR/jq")" != "$JQ_VERSION" ]; then
  log "ERROR: jq post-install version check failed"
  exit 1
fi

log "installed ShellCheck ${SHELLCHECK_VERSION}, shfmt ${SHFMT_VERSION} and jq ${JQ_VERSION} in ${TOOLS_CACHE}"
if [[ ":$PATH:" != *":$BIN_DIR:"* ]]; then
  log "NOTE: add ${BIN_DIR} to PATH"
fi
