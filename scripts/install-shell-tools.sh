#!/usr/bin/env bash
# Provision the pinned ShellCheck and shfmt binaries used by shell-quality gates.

set -euo pipefail

SHELLCHECK_VERSION="0.11.0"
SHFMT_VERSION="3.13.1"

SHELLCHECK_BASE_URL="${SHELLCHECK_BASE_URL:-https://github.com/koalaman/shellcheck/releases/download/v${SHELLCHECK_VERSION}}"
SHFMT_BASE_URL="${SHFMT_BASE_URL:-https://github.com/mvdan/sh/releases/download/v${SHFMT_VERSION}}"
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

case "${OS_NAME}:${ARCH_NAME}" in
  Linux:x86_64 | Linux:amd64)
    shellcheck_arch="x86_64"
    shellcheck_sha="8c3be12b05d5c177a04c29e3c78ce89ac86f1595681cab149b65b97c4e227198"
    shfmt_arch="amd64"
    shfmt_sha="fb096c5d1ac6beabbdbaa2874d025badb03ee07929f0c9ff67563ce8c75398b1"
    ;;
  Linux:aarch64 | Linux:arm64)
    shellcheck_arch="aarch64"
    shellcheck_sha="12b331c1d2db6b9eb13cfca64306b1b157a86eb69db83023e261eaa7e7c14588"
    shfmt_arch="arm64"
    shfmt_sha="32d92acaa5cd8abb29fc49dac123dc412442d5713967819d8af2c29f1b3857c7"
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

if "$shellcheck_ok" && "$shfmt_ok"; then
  mkdir -p "$BIN_DIR"
  if [ "$shellcheck_path" != "$BIN_DIR/shellcheck" ]; then
    ln -sfn "$shellcheck_path" "$BIN_DIR/shellcheck"
  fi
  if [ "$shfmt_path" != "$BIN_DIR/shfmt" ]; then
    ln -sfn "$shfmt_path" "$BIN_DIR/shfmt"
  fi
  log "ShellCheck ${SHELLCHECK_VERSION} and shfmt ${SHFMT_VERSION} already present — nothing to do."
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

log "installed ShellCheck ${SHELLCHECK_VERSION} and shfmt ${SHFMT_VERSION} in ${TOOLS_CACHE}"
if [[ ":$PATH:" != *":$BIN_DIR:"* ]]; then
  log "NOTE: add ${BIN_DIR} to PATH"
fi
