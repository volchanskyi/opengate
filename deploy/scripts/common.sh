#!/usr/bin/env bash
# Shared helpers for the OpenGate deploy smoke test.
# Sourced by smoke-test.sh, which cd.yml runs against staging/production over a
# Kubernetes Service port-forward, and which `make e2e` runs against the local
# compose stack. Pure bash, no external deps.

# --- Logging ------------------------------------------------------------------

log() {
  echo "[$(date -u '+%Y-%m-%dT%H:%M:%SZ')] $*"
}

fail() {
  echo "[$(date -u '+%Y-%m-%dT%H:%M:%SZ')] FATAL: $*" >&2
  exit 1
}

# --- Validation ---------------------------------------------------------------

# validate_mode MODE — exits with error if mode is invalid.
#
# local is the compose stack `make e2e` builds from the working tree; staging
# and production are deployed releases reached over a port-forward. The mode
# decides what the run is allowed to do to the environment, not which checks
# matter — see the mode gates in smoke-test.sh.
validate_mode() {
  local mode="$1"
  [[ "$mode" == "local" || "$mode" == "staging" || "$mode" == "production" ]] \
    || fail "Invalid mode: $mode (expected 'local', 'staging' or 'production')"
}
