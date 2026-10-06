#!/usr/bin/env bash
# Provisions the pinned Semgrep wheel into a venv under the XDG data dir, linked onto PATH.
# It does nothing when the pinned version is already present.
#
# Exit codes:
#   0  semgrep is installed at the pinned version
#   1  installation failed: python3 missing, network failure or broken import
set -euo pipefail

# The pinned version comes from the manifest.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/tool-versions.sh
. "$SCRIPT_DIR/lib/tool-versions.sh"
SEMGREP_VERSION="$TOOL_VERSION_SEMGREP"

# Semgrep imports pkg_resources on every invocation, and setuptools 82 and later omit it.
SETUPTOOLS_CONSTRAINT="setuptools<81"

# The install skips Semgrep's version-check network call.
export SEMGREP_ENABLE_VERSION_CHECK=0

VENV_DIR="${XDG_DATA_HOME:-$HOME/.local/share}/opengate/semgrep-venv"
BIN_DIR="${HOME}/.local/bin"
LINK="${BIN_DIR}/semgrep"

log() { printf '[install-semgrep] %s\n' "$1" >&2; }

# Prints the first X.Y.Z in `semgrep --version`, taken from a variable because `head` would
# leave the writer a failed write that pipefail reports.
semgrep_version_of() {
  local reported versions
  reported="$("$1" --version 2>/dev/null || true)"
  versions="$(grep -oE '[0-9]+\.[0-9]+\.[0-9]+' <<<"$reported" || true)"
  printf '%s\n' "${versions%%$'\n'*}"
}

# A pinned version already on PATH ends the script.
if command -v semgrep >/dev/null 2>&1; then
  have="$(semgrep_version_of semgrep)"
  if [ "$have" = "$SEMGREP_VERSION" ]; then
    log "semgrep ${SEMGREP_VERSION} already present — nothing to do."
    exit 0
  fi
  log "semgrep on PATH is '${have}', want ${SEMGREP_VERSION} — reprovisioning venv."
fi

if ! command -v python3 >/dev/null 2>&1; then
  log "ERROR: python3 not found. Semgrep requires Python 3.9+."
  exit 1
fi

log "creating venv at ${VENV_DIR}"
python3 -m venv "$VENV_DIR"
# shellcheck disable=SC1091
"$VENV_DIR/bin/pip" install --quiet --upgrade pip
log "installing semgrep==${SEMGREP_VERSION} (this can take a minute)"
# The setuptools constraint keeps the pkg_resources import available in the venv.
"$VENV_DIR/bin/pip" install --quiet "semgrep==${SEMGREP_VERSION}"
"$VENV_DIR/bin/pip" install --quiet "${SETUPTOOLS_CONSTRAINT}"

mkdir -p "$BIN_DIR"
ln -sf "$VENV_DIR/bin/semgrep" "$LINK"
log "symlinked ${LINK} -> ${VENV_DIR}/bin/semgrep"

# The smoke test runs the launcher, whose import chain surfaces a broken dependency here.
if ! out="$("$LINK" --version 2>&1)"; then
  log "ERROR: 'semgrep --version' failed to run after install. Output:"
  printf '%s\n' "$out" >&2
  exit 1
fi
got="$(printf '%s\n' "$out" | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1)"
if [ "$got" != "$SEMGREP_VERSION" ]; then
  log "ERROR: post-install version is '${got:-<none>}', expected ${SEMGREP_VERSION}. Full output:"
  printf '%s\n' "$out" >&2
  exit 1
fi

if ! command -v semgrep >/dev/null 2>&1; then
  log "NOTE: ${BIN_DIR} is not on PATH in this shell. Add it:"
  log "  export PATH=\"\$HOME/.local/bin:\$PATH\""
fi
log "semgrep ${SEMGREP_VERSION} installed."
exit 0
