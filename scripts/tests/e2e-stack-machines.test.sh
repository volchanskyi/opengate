#!/usr/bin/env bash
# Keeps the browser test stack carrying real machines.
#
# The stack ran a server and a database and no agent, so nine of twenty-one
# specs reached for page.route and six of them fabricated a whole machine —
# its device row, its hardware, its inventory, its sessions. Sixteen tests
# asserted that the browser renders a tab against a server that was not there.
#
# Three things have to hold together for that to stay fixed, and each is easy
# to undo alone: the server has to listen for machines, the machines have to be
# in the stack, and no spec may go back to inventing one.
#
# Run: ./scripts/tests/e2e-stack-machines.test.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
COMPOSE="$ROOT/deploy/docker-compose.test.yml"
BRINGUP="$ROOT/deploy/scripts/e2e-stack-up.sh"
SPECS="$ROOT/web/e2e"
PLAYWRIGHT_CONFIG="$ROOT/web/playwright.config.ts"

PASS=0
FAIL=0
FAILURES=()
pass() {
  PASS=$((PASS + 1))
  printf '  ok   %s\n' "$1"
}
fail() {
  FAIL=$((FAIL + 1))
  FAILURES+=("$1")
  printf '  FAIL %s\n' "$1" >&2
}

echo "e2e-stack-machines:"

for f in "$COMPOSE" "$BRINGUP" "$PLAYWRIGHT_CONFIG"; do
  if [ ! -f "$f" ]; then
    fail "missing file: $f"
    printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
    exit 1
  fi
done

# The server has to listen for machines, and be reachable at the name they dial.
if grep -q '\-quic-listen' "$COMPOSE"; then
  pass "the server listens for machines"
else
  fail "the server's command omits -quic-listen, so no machine can reach it"
fi

if grep -q '9090:9090/udp' "$COMPOSE"; then
  pass "the machine-facing listener is published"
else
  fail "9090/udp is not published, so the listener is unreachable from outside the stack"
fi

if grep -q 'OPENGATE_QUIC_HOST=server' "$COMPOSE"; then
  pass "the machine-facing certificate carries the name the machines dial"
else
  fail "OPENGATE_QUIC_HOST is unset, so the certificate names localhost and every handshake fails"
fi

# Two machines, so a spec may disturb one without breaking the rest.
for machine in agent-a agent-b; do
  if grep -q "hostname: $machine" "$COMPOSE"; then
    pass "$machine is in the stack with a pinned hostname"
  else
    fail "$machine is missing, or its hostname is not pinned — a spec cannot name a container id"
  fi
done

# Both paths into the suite bring up the same stack.
if grep -q 'e2e-stack-up.sh' "$PLAYWRIGHT_CONFIG" && grep -q 'e2e-stack-up.sh' "$ROOT/Makefile"; then
  pass "make e2e and playwright's webServer run the same bring-up"
else
  fail "the two entry points into the suite bring up different stacks"
fi

# The machines install through the public enrolment endpoint, with a token
# minted at bring-up. No key is copied and no test-only bypass exists.
if grep -q '/api/v1/enrollment-tokens' "$BRINGUP" && grep -q 'OPENGATE_ENROLL_TOKEN' "$BRINGUP"; then
  pass "the machines install with a token minted through the public endpoint"
else
  fail "the bring-up does not mint an enrolment token"
fi

if grep -qE '^\s*OPENGATE_TEST_MODE' "$COMPOSE"; then
  fail "OPENGATE_TEST_MODE is set but no Go source reads it"
else
  pass "the stack carries no variable the server does not read"
fi

# No spec may invent a machine again. A fabricated device is a device_id
# constant fulfilled by a page.route on the device endpoint; the real thing is
# looked up by hostname through the enrolled-machine helper.
#
# chat.spec.ts is the stated exception: its tab is shown only for a machine
# reporting RemoteDesktop, and a Linux agent reports the null implementation
# there in production as much as in a container. The spec says so in its own
# header, which is what this checks.
# The pattern is built rather than written inline: a literal carrying ${…}
# reads to ShellCheck as an expansion nobody meant, and quoting it the other way
# would expand it for real.
dollar='$'
fabricated_device="page.route(\`**/api/v1/devices/${dollar}{DEVICE_ID}\`"

fabricating=""
while IFS= read -r spec; do
  base="$(basename "$spec")"
  [ "$base" = "chat.spec.ts" ] && continue
  grep -qF "$fabricated_device" "$spec" && fabricating="$fabricating $base"
done < <(find "$SPECS" -name '*.spec.ts' | sort)

if [ -z "$fabricating" ]; then
  pass "no spec fabricates a machine the stack could supply"
else
  fail "these specs fabricate a machine:$fabricating"
fi

if grep -q 'RemoteDesktop' "$SPECS/chat.spec.ts"; then
  pass "chat.spec.ts states why its machine is described rather than enrolled"
else
  fail "chat.spec.ts fabricates a machine without saying why"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf 'Failures:\n' >&2
  for f in "${FAILURES[@]}"; do printf '  - %s\n' "$f" >&2; done
  exit 1
fi
