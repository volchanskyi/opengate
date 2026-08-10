#!/usr/bin/env bash
# victoriametrics-prereq.sh — VictoriaMetrics reachability gate for the gauntlet.
#
# Sourced by scripts/precommit-gauntlet.sh (and by tests in
# scripts/tests/victoriametrics-prereq.test.sh). NOT executable on its own —
# this is a library of bash functions.
#
# Functions exported:
#   vm_probe HOST PORT       — pure-bash TCP probe. Exit 0 = reachable,
#                              non-zero = unreachable. Safe to call repeatedly.
#   vm_ensure_up [HOST PORT TIMEOUT]
#                            — if VictoriaMetrics is unreachable, run
#                              `make victoriametrics-test-up` and wait up to
#                              TIMEOUT seconds (default 30) for it to accept
#                              connections. Returns 0 on success, non-zero on
#                              failure.
#   vm_test_url [PORT]       — the URL to export as VICTORIAMETRICS_TEST_URL.
#
# Why the gauntlet needs this: testvm memoizes its container per test BINARY and
# `go test ./tests/...` builds one binary per package, so with the URL unset every
# VictoriaMetrics-touching package starts its own instance. Several stores holding
# a fleet's worth of series at once is enough memory pressure to have the kernel
# kill one mid-run, which surfaces as an unrelated test failing on a refused
# connection. One shared instance, provisioned here, removes the whole class.
#
# Why a separate library: testability, exactly as with postgres-prereq.sh — the
# prereq phase sits at the head of a 7-15 minute script, so its logic is proven
# in isolation instead of end to end.

# vm_probe HOST PORT  — return 0 if HOST:PORT accepts a TCP connection.
vm_probe() {
  local host="${1:-127.0.0.1}"
  local port="${2:-${VICTORIAMETRICS_TEST_PORT:-8428}}"
  # /dev/tcp is a bash built-in — no curl or nc needed. The subshell keeps the
  # file descriptor from leaking into the caller.
  (exec 3<>"/dev/tcp/$host/$port") 2>/dev/null
}

# vm_test_url [PORT]  — the base URL a shared instance is reachable at.
vm_test_url() {
  local port="${1:-${VICTORIAMETRICS_TEST_PORT:-8428}}"
  echo "http://127.0.0.1:$port"
}

# vm_ensure_up [HOST PORT TIMEOUT]  — guarantee VictoriaMetrics is reachable,
# starting it if needed. Logs to stderr.
vm_ensure_up() {
  local host="${1:-${VICTORIAMETRICS_TEST_HOST:-127.0.0.1}}"
  local port="${2:-${VICTORIAMETRICS_TEST_PORT:-8428}}"
  local timeout="${3:-30}"
  # VM_PREREQ_START_CMD is the start command, overridable so the tests can prove
  # the failure path without a Docker daemon.
  local start_cmd="${VM_PREREQ_START_CMD:-make victoriametrics-test-up}"

  if vm_probe "$host" "$port"; then
    return 0
  fi

  echo "ℹ VictoriaMetrics unreachable on $host:$port — starting test container via '$start_cmd'..." >&2
  if ! $start_cmd >/dev/null 2>&1; then
    echo "✗ '$start_cmd' failed. Start the container manually:" >&2
    echo "    make victoriametrics-test-up" >&2
    return 1
  fi

  local i=0
  while [ "$i" -lt "$timeout" ]; do
    if vm_probe "$host" "$port"; then
      echo "✓ VictoriaMetrics test container is up (took ${i}s)." >&2
      return 0
    fi
    sleep 1
    i=$((i + 1))
  done

  echo "✗ VictoriaMetrics container started but is not accepting connections on $host:$port after ${timeout}s." >&2
  return 1
}
