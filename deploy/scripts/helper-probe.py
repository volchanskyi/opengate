#!/usr/bin/env python3
"""Asks a helper what address it sees this machine coming from, and fails unless it is expected.

A helper behind a hop that rewrites the source address answers with the hop's address, and a
direct link never forms across a router; the expected address is what another path saw.

Usage:
  helper-probe.py stun:<host>:<port> <expected-ipv4>

Exit codes:
  0  the helper named the expected address
  1  it named another, answered with something else, or did not answer
"""

import ipaddress
import os
import socket
import struct
import sys

COOKIE = 0x2112A442
BINDING_REQUEST = 0x0001
BINDING_SUCCESS = 0x0101
XOR_MAPPED_ADDRESS = 0x0020
# RFC 8489 §6.2.1: seven sends from 500 ms, doubling, then sixteen times that for the last answer.
FIRST_WAIT = 0.5
SENDS = 7
LAST_WAITS = 16


def fail(message):
    print(f"::error::{message}", file=sys.stderr)
    sys.exit(1)


def helper_address(url):
    if not url.startswith("stun:"):
        fail(f"{url} is not a stun: URL")
    host, sep, port = url[len("stun:"):].rpartition(":")
    if not sep or not host or not port.isdigit():
        fail(f"{url} names no host and port")
    return host, int(port)


def mapped_address(reply, transaction):
    """The address an answer to transaction names, or None for any other datagram."""
    if len(reply) < 20:
        return None
    kind, length, cookie = struct.unpack("!HHI", reply[:8])
    if kind != BINDING_SUCCESS or cookie != COOKIE or reply[8:20] != transaction:
        return None
    attributes = reply[20:20 + length]
    while len(attributes) >= 4:
        attr, size = struct.unpack("!HH", attributes[:4])
        value = attributes[4:4 + size]
        if attr == XOR_MAPPED_ADDRESS and len(value) >= 8 and value[1] == 1:
            xaddr = struct.unpack("!I", value[4:8])[0] ^ COOKIE
            return str(ipaddress.IPv4Address(xaddr))
        attributes = attributes[4 + size + (-size % 4):]
    return None


def ask(host, port):
    transaction = os.urandom(12)
    request = struct.pack("!HHI", BINDING_REQUEST, 0, COOKIE) + transaction
    with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as sock:
        # A connected socket hears an unreachable notice from a port nobody holds.
        sock.connect((host, port))
        wait = FIRST_WAIT
        for send in range(1, SENDS + 1):
            sock.send(request)
            sock.settimeout(FIRST_WAIT * LAST_WAITS if send == SENDS else wait)
            try:
                while True:
                    seen = mapped_address(sock.recv(2048), transaction)
                    if seen:
                        return seen
            except socket.timeout:
                wait *= 2
            except OSError as error:
                fail(f"no answer from the helper at {host}:{port}: {error}")
    fail(f"no answer from the helper at {host}:{port} after {SENDS} requests")
    return None


def main():
    if len(sys.argv) != 3:
        fail("usage: helper-probe.py stun:<host>:<port> <expected-ipv4>")
    host, port = helper_address(sys.argv[1])
    expected = sys.argv[2]
    seen = ask(host, port)
    if seen != expected:
        fail(f"the helper at {host}:{port} saw {seen}, and this machine comes from {expected}: "
             "a hop in front of it rewrites the source address")
    print(f"the helper at {host}:{port} saw {seen}, this machine's own address")


if __name__ == "__main__":
    main()
