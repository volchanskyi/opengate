#!/usr/bin/env bash
# postgres-prereq.sh — Postgres reachability gate for the gauntlet.
#
# Sourced by scripts/precommit-gauntlet.sh (and by tests in
# scripts/tests/postgres-prereq.test.sh). NOT executable on its own —
# this is a library of bash functions.
#
# Functions exported:
#   pg_probe HOST PORT      — pure-bash TCP probe (no pg_isready/nc/psql
#                              required). Exit 0 = reachable, non-zero =
#                              unreachable. Safe to call repeatedly.
#   pg_ensure_up [HOST PORT TIMEOUT]
#                            — if Postgres is unreachable, run
#                              `make postgres-test-up` and wait up to
#                              TIMEOUT seconds (default 30) for it to
#                              accept connections. Returns 0 on success,
#                              non-zero on failure. HOST defaults to
#                              $POSTGRES_TEST_HOST or "localhost"; PORT
#                              defaults to $POSTGRES_TEST_PORT or 5432.
#
# Why a separate library: testability. The gauntlet's prereq phase is
# part of a 7-15 minute script — exercising it end-to-end in unit tests
# would be a budget killer. Sourcing this file lets
# scripts/tests/postgres-prereq.test.sh probe the functions in isolation.

# pg_probe HOST PORT  — return 0 if HOST:PORT accepts a TCP connection.
pg_probe() {
  local host="${1:-localhost}"
  local port="${2:-5432}"
  # /dev/tcp is a bash built-in — no external binary needed. Wrapping in
  # a subshell so the fd state doesn't leak into the caller.
  (exec 3<>"/dev/tcp/$host/$port") 2>/dev/null
  local rc=$?
  # Close the fd if open. The fd was opened inside the subshell context
  # above; the parent shell never received it, so this is a no-op here
  # but kept for documentation.
  return "$rc"
}

# pg_ensure_up [HOST PORT TIMEOUT]  — guarantee Postgres is reachable,
# starting it if needed. Logs to stderr.
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
