#!/usr/bin/env bash
# Provisions the pinned age and zstd that compress and encrypt the endurance run's core dump.
# age is the release binary checked against its digest; zstd builds from source with a C compiler.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/tool-versions.sh
. "$SCRIPT_DIR/lib/tool-versions.sh"

AGE_VERSION="$TOOL_VERSION_AGE"
ZSTD_VERSION="$TOOL_VERSION_ZSTD"

AGE_BASE_URL="${AGE_BASE_URL:-https://github.com/FiloSottile/age/releases/download/v${AGE_VERSION}}"
ZSTD_BASE_URL="${ZSTD_BASE_URL:-https://github.com/facebook/zstd/releases/download/v${ZSTD_VERSION}}"
TOOLS_CACHE="${DUMP_TOOLS_CACHE:-${XDG_DATA_HOME:-$HOME/.local/share}/opengate/dump-tools}"
BIN_DIR="${DUMP_TOOLS_BIN_DIR:-$HOME/.local/bin}"
ARCH_NAME="${DUMP_TOOLS_UNAME_M:-$(uname -m)}"

# The source tarball is the same on every platform.
ZSTD_SHA="eb33e51f49a15e023950cd7825ca74a4a2b43db8354825ac24fc1b7ee09e6fa3"

log() { printf '[install-dump-tools] %s\n' "$1" >&2; }

age_version_of() {
  "$1" --version 2>/dev/null | sed -n '1{s/^v//;p;}'
}

zstd_version_of() {
  "$1" --version 2>/dev/null | sed -n '1{s/.* v\([0-9][0-9.]*\),.*/\1/p;}'
}

case "$ARCH_NAME" in
  x86_64 | amd64)
    age_arch="amd64"
    age_sha="cbe24006683f8eb669266162894b9a522a1af52f2665fbc63a4bb032ed26ac10"
    ;;
  aarch64 | arm64)
    age_arch="arm64"
    age_sha="6b8dc4333c53a5a57c9e5834e3a48f92605d7154014cd07269ff3327db5d37f4"
    ;;
  *)
    log "ERROR: unsupported architecture ${ARCH_NAME}"
    exit 1
    ;;
esac

age_path="$(command -v age 2>/dev/null || true)"
keygen_path="$(command -v age-keygen 2>/dev/null || true)"
age_ok=false
if [ -n "$age_path" ] && [ -n "$keygen_path" ] \
  && [ "$(age_version_of "$age_path")" = "$AGE_VERSION" ] \
  && [ "$(age_version_of "$keygen_path")" = "$AGE_VERSION" ]; then
  age_ok=true
fi

zstd_path="$(command -v zstd 2>/dev/null || true)"
zstd_ok=false
if [ -n "$zstd_path" ] && [ "$(zstd_version_of "$zstd_path")" = "$ZSTD_VERSION" ]; then
  zstd_ok=true
fi

# A copy already at the pinned version is linked where the installs go, so the
# directory the job and the gauntlet put first on PATH holds all three.
mkdir -p "$BIN_DIR"
if "$age_ok"; then
  [ "$age_path" = "$BIN_DIR/age" ] || ln -sfn "$age_path" "$BIN_DIR/age"
  [ "$keygen_path" = "$BIN_DIR/age-keygen" ] || ln -sfn "$keygen_path" "$BIN_DIR/age-keygen"
fi
if "$zstd_ok"; then
  [ "$zstd_path" = "$BIN_DIR/zstd" ] || ln -sfn "$zstd_path" "$BIN_DIR/zstd"
fi

if "$age_ok" && "$zstd_ok"; then
  log "age ${AGE_VERSION} and zstd ${ZSTD_VERSION} already present — nothing to do."
  exit 0
fi

for tool in curl sha256sum tar; do
  command -v "$tool" >/dev/null 2>&1 || {
    log "ERROR: $tool is required"
    exit 1
  }
done

mkdir -p "$TOOLS_CACHE"
work_dir="$(mktemp -d "$TOOLS_CACHE/install.XXXXXX")"
trap 'rm -rf "$work_dir"' EXIT

download_and_verify() {
  local url="$1" destination="$2" expected_sha="$3" actual_sha
  curl --fail --location --retry 3 --silent --show-error "$url" -o "$destination"
  actual_sha="$(sha256sum "$destination" | awk '{print $1}')"
  if [ "$actual_sha" != "$expected_sha" ]; then
    log "ERROR: checksum mismatch for $(basename "$destination"): got $actual_sha, expected $expected_sha"
    exit 1
  fi
}

if ! "$age_ok"; then
  age_asset="age-v${AGE_VERSION}-linux-${age_arch}.tar.gz"
  age_dir="$TOOLS_CACHE/age-${AGE_VERSION}"
  log "downloading ${age_asset}"
  download_and_verify "$AGE_BASE_URL/$age_asset" "$work_dir/$age_asset" "$age_sha"
  tar -xzf "$work_dir/$age_asset" -C "$work_dir"
  rm -rf "$age_dir"
  mkdir -p "$age_dir"
  install -m 0755 "$work_dir/age/age" "$age_dir/age"
  install -m 0755 "$work_dir/age/age-keygen" "$age_dir/age-keygen"
  ln -sfn "$age_dir/age" "$BIN_DIR/age"
  ln -sfn "$age_dir/age-keygen" "$BIN_DIR/age-keygen"
fi

if ! "$zstd_ok"; then
  for tool in make cc; do
    command -v "$tool" >/dev/null 2>&1 || {
      log "ERROR: $tool is required to build zstd from its release source"
      exit 1
    }
  done
  zstd_asset="zstd-${ZSTD_VERSION}.tar.gz"
  zstd_dir="$TOOLS_CACHE/zstd-${ZSTD_VERSION}"
  log "downloading and building ${zstd_asset}"
  download_and_verify "$ZSTD_BASE_URL/$zstd_asset" "$work_dir/$zstd_asset" "$ZSTD_SHA"
  tar -xzf "$work_dir/$zstd_asset" -C "$work_dir"
  make -s -C "$work_dir/zstd-${ZSTD_VERSION}/programs" -j "$(nproc)" zstd >/dev/null
  rm -rf "$zstd_dir"
  mkdir -p "$zstd_dir"
  install -m 0755 "$work_dir/zstd-${ZSTD_VERSION}/programs/zstd" "$zstd_dir/zstd"
  ln -sfn "$zstd_dir/zstd" "$BIN_DIR/zstd"
fi

for tool in age age-keygen; do
  if [ "$(age_version_of "$BIN_DIR/$tool")" != "$AGE_VERSION" ]; then
    log "ERROR: $tool post-install version check failed"
    exit 1
  fi
done
if [ "$(zstd_version_of "$BIN_DIR/zstd")" != "$ZSTD_VERSION" ]; then
  log "ERROR: zstd post-install version check failed"
  exit 1
fi

log "installed age ${AGE_VERSION} and zstd ${ZSTD_VERSION} in ${TOOLS_CACHE}"
if [[ ":$PATH:" != *":$BIN_DIR:"* ]]; then
  log "NOTE: add ${BIN_DIR} to PATH"
fi
