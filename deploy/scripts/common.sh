#!/usr/bin/env bash
# Shared helpers for the OpenGate deploy smoke test.
# Sourced by smoke-test.sh, which cd.yml runs against staging/production over a
# Kubernetes Service port-forward. Pure bash, no external deps.

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
validate_mode() {
  local mode="$1"
  [[ "$mode" == "staging" || "$mode" == "production" ]] || fail "Invalid mode: $mode (expected 'staging' or 'production')"
}
