#!/usr/bin/env bash
# Shared helpers for the deploy smoke test, sourced by smoke-test.sh.

log() {
  echo "[$(date -u '+%Y-%m-%dT%H:%M:%SZ')] $*"
}

fail() {
  echo "[$(date -u '+%Y-%m-%dT%H:%M:%SZ')] FATAL: $*" >&2
  exit 1
}

# The mode decides what a run may do to the environment; the gates are in smoke-test.sh.
validate_mode() {
  local mode="$1"
  [[ "$mode" == "local" || "$mode" == "staging" || "$mode" == "production" ]] \
    || fail "Invalid mode: $mode (expected 'local', 'staging' or 'production')"
}
