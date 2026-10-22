#!/usr/bin/env bash
# Runs one simulated router of the browser test stack, and changes its rules while a spec runs.
#
# Usage:
#   router.sh start                       translate, filter and stay up; the image's entrypoint
#   router.sh nat keep|scramble           keep each socket's port, or pick a random one per flow
#   router.sh udp open|blocked|helper-only  pass UDP, drop it, or pass only the helper's
#   router.sh shape MBIT RTT_MS [PCT]     hold both directions to a rate and a round trip, sending
#                                         PCT percent of packets ahead of the rest, out of order
#   router.sh shape off                   remove the rate and the delay
#   router.sh sent                        print and zero the bytes the network behind sent out
#   router.sh reset                       nat keep, udp open, shape off
#
# Environment:
#   ROUTER_LAN_ADDR     the router's address on the network behind it (required)
#   ROUTER_WAN_ADDR     the router's address on the network in front of it (required)
#   ROUTER_LAN_GATEWAY  the gateway Docker names for the network behind it (required)
#   ROUTER_HELPER       host:port of the helper, the UDP peer `udp helper-only` keeps (required)
#   ROUTER_FORWARD      tcp port and LAN address, as PORT:ADDR, forwarded from the WAN address

set -euo pipefail

: "${ROUTER_LAN_ADDR:?}"
: "${ROUTER_WAN_ADDR:?}"
: "${ROUTER_LAN_GATEWAY:?}"
: "${ROUTER_HELPER:?}"

READY=/tmp/router-ready

die() {
  printf 'router: %s\n' "$1" >&2
  exit 1
}

# Docker orders a container's interfaces by attach order, so each is found by the address it holds.
interface_of() {
  local found
  found="$(ip -o -4 addr show | awk -v want="$1" '{ split($4, a, "/"); if (a[1] == want) print $2 }')"
  [ -n "$found" ] || die "no interface holds $1"
  printf '%s' "$found"
}

LAN="$(interface_of "$ROUTER_LAN_ADDR")"
WAN="$(interface_of "$ROUTER_WAN_ADDR")"

set_nat() {
  local random=()
  case "$1" in
    keep) ;;
    scramble) random=(--random-fully) ;;
    *) die "nat takes keep or scramble, not $1" ;;
  esac
  iptables -t nat -F ROUTER_NAT
  iptables -t nat -A ROUTER_NAT -o "$WAN" -j MASQUERADE "${random[@]}"
}

# ROUTER_UDP is consulted before the reply rule, so a block also cuts flows already established.
set_udp() {
  local host="${ROUTER_HELPER%:*}" port="${ROUTER_HELPER##*:}"
  iptables -F ROUTER_UDP
  case "$1" in
    open) ;;
    blocked) iptables -A ROUTER_UDP -p udp -j DROP ;;
    helper-only)
      iptables -A ROUTER_UDP -p udp -d "$host" --dport "$port" -j RETURN
      iptables -A ROUTER_UDP -p udp -s "$host" --sport "$port" -j RETURN
      iptables -A ROUTER_UDP -p udp -j DROP
      ;;
    *) die "udp takes open, blocked or helper-only, not $1" ;;
  esac
}

# Each direction leaves through one interface, so half the round trip is added on each. A reordered
# packet skips that delay, so it arrives ahead of the packets sent before it.
set_shape() {
  local dev
  if [ "$1" = off ]; then
    for dev in "$LAN" "$WAN"; do
      if grep -q netem <<<"$(tc qdisc show dev "$dev")"; then
        tc qdisc del dev "$dev" root
      fi
    done
    return
  fi
  [[ "$1" =~ ^[0-9]+$ && "${2:-}" =~ ^[0-9]+$ && "${3:-0}" =~ ^[0-9]+$ ]] \
    || die "shape takes MBIT RTT_MS [REORDER_PCT] or off"
  local reorder=()
  [ "${3:-0}" -gt 0 ] && reorder=(reorder "${3}%")
  for dev in "$LAN" "$WAN"; do
    tc qdisc replace dev "$dev" root netem delay "$(($2 / 2))ms" "${reorder[@]}" rate "${1}mbit"
  done
}

# The bytes the network behind has sent out through this router since the last reading.
sent_out() {
  iptables -L FORWARD -v -x -n | awk -v lan="$LAN" -v wan="$WAN" \
    '$3 == "ACCEPT" && $6 == lan && $7 == wan { print $2; exit }'
  iptables -Z FORWARD
}

start() {
  local prefix
  prefix="$(ip -o -4 addr show dev "$LAN" | awk '{ split($4, a, "/"); print a[2]; exit }')"
  # The bridge behind the router holds no address, so the router answers for the gateway Docker
  # hands every container there, and becomes their way out.
  setpriv --inh-caps +net_admin --ambient-caps +net_admin -- \
    ip addr replace "$ROUTER_LAN_GATEWAY/$prefix" dev "$LAN"

  iptables -t nat -N ROUTER_NAT
  iptables -t nat -A POSTROUTING -j ROUTER_NAT
  iptables -N ROUTER_UDP
  iptables -P FORWARD DROP
  iptables -A FORWARD -j ROUTER_UDP
  iptables -A FORWARD -i "$LAN" -o "$WAN" -j ACCEPT
  iptables -A FORWARD -i "$WAN" -o "$LAN" -m conntrack --ctstate ESTABLISHED,RELATED,DNAT -j ACCEPT
  # An unsolicited packet for the router itself is dropped, so it leaves no tracked flow behind to
  # stop the port a LAN socket already holds from being kept for that socket's next peer.
  iptables -A INPUT -i "$WAN" -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT
  iptables -A INPUT -i "$WAN" -j DROP
  set_nat keep

  if [ -n "${ROUTER_FORWARD:-}" ]; then
    iptables -t nat -A PREROUTING -i "$WAN" -d "$ROUTER_WAN_ADDR" -p tcp \
      --dport "${ROUTER_FORWARD%%:*}" -j DNAT --to-destination "${ROUTER_FORWARD#*:}:${ROUTER_FORWARD%%:*}"
  fi

  touch "$READY"
  exec sleep infinity
}

case "${1:-}" in
  start) start ;;
  nat) set_nat "${2:-}" ;;
  udp) set_udp "${2:-}" ;;
  shape) set_shape "${2:-}" "${3:-}" "${4:-}" ;;
  sent) sent_out ;;
  reset)
    set_nat keep
    set_udp open
    set_shape off
    ;;
  *) die "usage: router.sh start | nat MODE | udp MODE | shape MBIT RTT_MS [PCT] | shape off | sent | reset" ;;
esac
