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

# Every entry point into the suite brings up the same stack. There are four of
# them and they are easy to add to, so they are named here rather than assumed:
# a stack stood up any other way stages no agent binary and mints no enrolment
# token, and its machines never arrive.
ENTRY_POINTS=(
  "$ROOT/Makefile"
  "$PLAYWRIGHT_CONFIG"
  "$ROOT/.github/workflows/ci.yml"
  "$ROOT/.github/workflows/e2e-cross-browser.yml"
)

missing_bringup=""
for entry in "${ENTRY_POINTS[@]}"; do
  if [ ! -f "$entry" ]; then
    fail "missing entry point: $entry"
    continue
  fi
  grep -q 'e2e-stack-up.sh' "$entry" || missing_bringup="$missing_bringup $(basename "$entry")"
done

if [ -z "$missing_bringup" ]; then
  pass "every entry point into the suite runs the same bring-up"
else
  fail "these entry points stand up a different stack:$missing_bringup"
fi

# ...and neither they nor the page that tells a reader how to run the suite by
# hand stands the stack up on its own. A bare `compose up` skips the mint
# between the two halves of the bring-up, so the machines start with no token
# to install with and the server never sees them.
own_bringup=""
for entry in "${ENTRY_POINTS[@]}" "$ROOT/docs/infrastructure/Testing.md"; do
  [ -f "$entry" ] || continue
  if grep -qE 'docker compose.*docker-compose\.test\.yml.*[[:space:]]up([[:space:]]|$)' "$entry"; then
    own_bringup="$own_bringup $(basename "$entry")"
  fi
done

if [ -z "$own_bringup" ]; then
  pass "no entry point brings the stack up behind the bring-up's back"
else
  fail "these entry points run their own compose up, so no token is minted:$own_bringup"
fi

# The stack's shape validates on a clean checkout. The token the machines
# install with is minted per bring-up and is a credential, so it is in no
# checkout — and `docker compose config`, which CI's config lint and
# `make lint-deploy` both run, reads every env_file it is pointed at.
if ! command -v docker >/dev/null 2>&1; then
  fail "docker is not on PATH, so the stack's shape cannot be validated"
else
  # An empty project directory stands in for a clean checkout, so a token an
  # earlier bring-up left in deploy/ cannot make this pass locally and fail in
  # CI — which is exactly how it got through the first time.
  clean_dir="$(mktemp -d)"
  cp "$COMPOSE" "$clean_dir/docker-compose.test.yml"
  if docker compose -f "$clean_dir/docker-compose.test.yml" config --quiet >/dev/null 2>&1; then
    pass "the stack's shape validates without the bring-up's credential"
  else
    fail "the stack's shape needs a file no checkout carries, so config lint fails"
  fi
  rm -rf "$clean_dir"
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
