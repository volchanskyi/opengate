#!/usr/bin/env bash
# Provisions one shared VictoriaMetrics for the gauntlet: testvm starts a container per test binary,
# and several stores at once put enough memory pressure on the kernel to kill one mid-run.

vm_probe() {
  local host="${1:-127.0.0.1}"
  local port="${2:-${VICTORIAMETRICS_TEST_PORT:-8428}}"
  # The /dev/tcp built-in needs no curl or nc, and the subshell keeps the fd out of the caller.
  (exec 3<>"/dev/tcp/$host/$port") 2>/dev/null
}

vm_test_url() {
  local port="${1:-${VICTORIAMETRICS_TEST_PORT:-8428}}"
  echo "http://127.0.0.1:$port"
}

vm_ensure_up() {
  local host="${1:-${VICTORIAMETRICS_TEST_HOST:-127.0.0.1}}"
  local port="${2:-${VICTORIAMETRICS_TEST_PORT:-8428}}"
  local timeout="${3:-30}"
  # VM_PREREQ_START_CMD overrides the start command so tests can prove the failure path sans Docker.
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
