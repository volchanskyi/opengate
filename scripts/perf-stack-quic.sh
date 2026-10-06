#!/usr/bin/env bash
# Gives the QUIC path production's buffers, since the transport asks for 7 MiB per socket and a
# runner caps one at 1 MiB; the generator dials the certificate's name mapped to the container.
#
# Usage:
#   perf-stack-quic.sh raise-buffers   before the stack starts
#   perf-stack-quic.sh map-server      once it is up; publishes the target's counters location
#   perf-stack-quic.sh check <output>  after the run; fails when an end lacked its buffer
#
# Environment:
#   PERF_SERVER_CONTAINER  the server's container (default opengate-perf-server)
#   GITHUB_ENV             where map-server writes PERF_TARGET_NET_COUNTERS
set -euo pipefail

SERVER_CONTAINER="${PERF_SERVER_CONTAINER:-opengate-perf-server}"

# The name the server's certificate carries, from OPENGATE_QUIC_HOST in
# deploy/docker-compose.perf.yml.
SERVER_NAME=server

# Production's node, read from the host's own network namespace.
RMEM_MAX=31457280
WMEM_MAX=31457280
RMEM_DEFAULT=12582912

# What the transport prints when the kernel gave a socket less than it asked.
SHORT_BUFFER='failed to sufficiently increase receive buffer size'

usage() {
  echo "usage: $0 {raise-buffers|map-server|check <harness-output>}" >&2
}

raise_buffers() {
  sudo -n sysctl -w \
    "net.core.rmem_max=${RMEM_MAX}" \
    "net.core.wmem_max=${WMEM_MAX}" \
    "net.core.rmem_default=${RMEM_DEFAULT}"
}

map_server() {
  local address pid counters
  address="$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "$SERVER_CONTAINER")"
  if [ -z "$address" ]; then
    echo "::error::${SERVER_CONTAINER} has no address to dial" >&2
    return 1
  fi
  printf '%s %s\n' "$address" "$SERVER_NAME" | sudo -n tee -a /etc/hosts >/dev/null
  echo "the generator dials ${SERVER_NAME} at ${address}, past the published port"

  # The target's network namespace is the one its process runs in, and its
  # counters page is readable from here because the two share this kernel.
  pid="$(docker inspect -f '{{.State.Pid}}' "$SERVER_CONTAINER")"
  counters="/proc/${pid}/net/snmp"
  if ! grep -q '^Udp:' "$counters" 2>/dev/null; then
    echo "::error::the server's network counters at ${counters} cannot be read, so the drops at its end would go uncounted" >&2
    return 1
  fi
  if [ -n "${GITHUB_ENV:-}" ]; then
    printf 'PERF_TARGET_NET_COUNTERS=%s\n' "$counters" >>"$GITHUB_ENV"
  fi
}

check() {
  local output="${1:-}" server_log
  if [ ! -s "$output" ]; then
    echo "::error::there is no harness output at ${output:-nothing} to read, so whether its buffers held is unknown" >&2
    return 1
  fi
  if grep -qF "$SHORT_BUFFER" "$output"; then
    echo "::error::the generator's kernel gave its socket less receive buffer than the transport asked for, so this run lost datagrams at the generator's end" >&2
    return 1
  fi
  server_log="$(docker logs "$SERVER_CONTAINER" 2>&1)"
  if grep -qF "$SHORT_BUFFER" <<<"$server_log"; then
    echo "::error::the server's kernel gave its socket less receive buffer than the transport asked for, so this run lost datagrams at the server's end" >&2
    return 1
  fi
  echo "both ends had the receive buffer the transport asked for"
}

main() {
  case "${1:-}" in
    raise-buffers) raise_buffers ;;
    map-server) map_server ;;
    check) check "${2:-}" ;;
    *)
      usage
      return 2
      ;;
  esac
}

main "$@"
