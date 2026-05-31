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
# Exit 1 = installation failed (python3 missing, network failure, broken import).
set -euo pipefail

# Exact pin — treat upgrades like any other dependency (staged through dev).
# Keep in sync with scripts/pentest-review.sh's SEMGREP_VERSION assertion and
# the ci.yml pentest-review job.
SEMGREP_VERSION="1.108.0"

# pkg_resources fix (the actual cause of the CI install failure, runs
# 26697942185 + 26703402241): semgrep 1.108.0 transitively imports
# `pkg_resources` (via opentelemetry-instrumentation, loaded on EVERY semgrep
# invocation including `--version`). A fresh Py3.12 venv pulls setuptools >=82,
# which REMOVED pkg_resources, so semgrep crashes with
# `ModuleNotFoundError: No module named 'pkg_resources'`. Pinning setuptools<81
# keeps pkg_resources available. See semgrep#11069 and setuptools 82.0.0
# history. (An earlier fix mis-attributed this to the version-upgrade notice;
# that notice does not appear in a clean CI env — the post-install smoke test
# below now surfaces the real import error if this ever regresses.)
SETUPTOOLS_CONSTRAINT="setuptools<81"

# Avoid Semgrep's version-check network call at install time (harmless, faster).
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
# Install semgrep, then constrain setuptools<81 in the same venv so the
# pkg_resources import path semgrep relies on stays available (see header).
"$VENV_DIR/bin/pip" install --quiet "semgrep==${SEMGREP_VERSION}"
"$VENV_DIR/bin/pip" install --quiet "${SETUPTOOLS_CONSTRAINT}"

mkdir -p "$BIN_DIR"
ln -sf "$VENV_DIR/bin/semgrep" "$LINK"
log "symlinked ${LINK} -> ${VENV_DIR}/bin/semgrep"

# Post-install smoke test. Run the launcher for real (stderr VISIBLE) — a bare
# `semgrep --version` exercises the full import chain, so a broken transitive
# dependency (e.g. the pkg_resources/setuptools break) fails HERE with the
# actual traceback rather than silently downstream. Do not swallow stderr.
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
