#!/usr/bin/env bash
# install-semgrep.sh — provision the pinned Semgrep used by the pen-test gate.
#
# Honors the zero-manual-install rule (.claude/rules/editing-and-scope.md):
# one command provisions Semgrep on any fleet machine, idempotently, and
# silently no-ops when the correct version is already present.
#
# Semgrep ships as a Python wheel. We install it into a dedicated venv under
# XDG data dir and symlink the launcher onto PATH, mirroring the local-binary
# precedent set by govulncheck / gitleaks / oapi-codegen (binary on PATH, not
# Docker). Docker is reserved for `make sonar` (needs a JVM); Semgrep does not.
#
# Exit 0 = semgrep is installed at the pinned version (newly or already).
# Exit 1 = installation failed (python3 missing, network failure, etc.).
set -euo pipefail

# Exact pin — treat upgrades like any other dependency (staged through dev).
# Keep in sync with scripts/pentest-review.sh's SEMGREP_VERSION assertion and
# the ci.yml pentest-review job.
SEMGREP_VERSION="1.108.0"

# Suppress Semgrep's "a new version is available" notice. On a fresh HOME (e.g.
# a CI runner) `semgrep --version` otherwise prints a blank line + the upgrade
# notice BEFORE the version number — which broke a naive `head -1` parse and
# failed the install step (see CI run 26697942185). Disabling the check also
# avoids a network call at install time.
export SEMGREP_ENABLE_VERSION_CHECK=0

VENV_DIR="${XDG_DATA_HOME:-$HOME/.local/share}/opengate/semgrep-venv"
BIN_DIR="${HOME}/.local/bin"
LINK="${BIN_DIR}/semgrep"

log() { printf '[install-semgrep] %s\n' "$1" >&2; }

# Parse the X.Y.Z version out of `semgrep --version`, robust to any extra
# notice lines the tool may emit. grep -oE never selects a non-version line.
semgrep_version_of() { "$1" --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1; }

# Already at the pinned version on PATH? No-op.
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
"$VENV_DIR/bin/pip" install --quiet "semgrep==${SEMGREP_VERSION}"

mkdir -p "$BIN_DIR"
ln -sf "$VENV_DIR/bin/semgrep" "$LINK"
log "symlinked ${LINK} -> ${VENV_DIR}/bin/semgrep"

# Verify the symlink resolves to the pinned version. PATH may not yet include
# ~/.local/bin in this shell, so invoke the link directly.
got="$(semgrep_version_of "$LINK")"
if [ "$got" != "$SEMGREP_VERSION" ]; then
  log "ERROR: post-install version is '${got}', expected ${SEMGREP_VERSION}."
  exit 1
fi

if ! command -v semgrep >/dev/null 2>&1; then
  log "NOTE: ${BIN_DIR} is not on PATH in this shell. Add it:"
  log "  export PATH=\"\$HOME/.local/bin:\$PATH\""
fi
log "semgrep ${SEMGREP_VERSION} installed."
exit 0
