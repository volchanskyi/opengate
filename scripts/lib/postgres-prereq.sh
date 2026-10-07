#!/usr/bin/env bash
# pg_probe HOST PORT connects over pure-bash TCP; pg_ensure_up [HOST PORT TIMEOUT] runs
# `make postgres-test-up` when Postgres is unreachable and waits up to TIMEOUT seconds.

pg_probe() {
  local host="${1:-localhost}"
  local port="${2:-5432}"
  # The /dev/tcp built-in runs in a subshell so the fd stays out of the caller.
  (exec 3<>"/dev/tcp/$host/$port") 2>/dev/null
  local rc=$?
  return "$rc"
}

pg_ensure_up() {
  local host="${1:-${POSTGRES_TEST_HOST:-localhost}}"
  local port="${2:-${POSTGRES_TEST_PORT:-5432}}"
  local timeout="${3:-30}"

  if pg_probe "$host" "$port"; then
    return 0
  fi

  echo "ℹ Postgres unreachable on $host:$port — starting test container via 'make postgres-test-up'..." >&2
  if ! make postgres-test-up >/dev/null 2>&1; then
    echo "✗ 'make postgres-test-up' failed. Start the container manually:" >&2
    echo "    make postgres-test-up" >&2
    return 1
  fi

  local i=0
  while [ "$i" -lt "$timeout" ]; do
    if pg_probe "$host" "$port"; then
      echo "✓ Postgres test container is up (took ${i}s)." >&2
      return 0
    fi
    sleep 1
    i=$((i + 1))
  done

  echo "✗ Postgres container started but is not accepting connections on $host:$port after ${timeout}s." >&2
  return 1
}
