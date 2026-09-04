#!/usr/bin/env bash
# Keeps every stack that runs the browser suite carrying real machines.
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
# The suite runs against two stacks, not one. The staging config derives from
# the local config and deletes only the webServer block — which is also the
# thing that installed the machines — so fourteen specs asked a fleet of
# nothing for a machine by name and every deploy since went red. The machines
# a spec may name are pinned in one file, and both stacks are checked against
# that file rather than against a list written here.
#
# Run: ./scripts/tests/e2e-stack-machines.test.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
COMPOSE="$ROOT/deploy/docker-compose.test.yml"
BRINGUP="$ROOT/deploy/scripts/e2e-stack-up.sh"
SPECS="$ROOT/web/e2e"
PLAYWRIGHT_CONFIG="$ROOT/web/playwright.config.ts"
HELPER="$SPECS/helpers/enrolled-machine.ts"
CD="$ROOT/.github/workflows/cd.yml"
SERVER_DEPLOYMENT="$ROOT/deploy/helm/opengate/templates/server-deployment.yaml"
MACHINE_POD="$ROOT/deploy/scripts/e2e-machine-pod.sh"

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

for f in "$COMPOSE" "$BRINGUP" "$PLAYWRIGHT_CONFIG" "$HELPER" "$CD" "$SERVER_DEPLOYMENT" "$MACHINE_POD"; do
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

# Two machines, so a spec may disturb one without breaking the rest. The names
# come from the file the specs read them from, so a third machine added there
# is checked here without this list being edited.
mapfile -t PINNED < <(sed -n 's/^export const MACHINE_[A-Z0-9]* = "\([^"]*\)";$/\1/p' "$HELPER")

# The bodies of the two staging steps that own the machines' lifetime. Reading
# them rather than the whole file keeps "brought up" from being satisfied by a
# passing mention in a comment.
cd_step_body() {
  awk -v want="      - name: $1" '
    $0 == want { in_step = 1; next }
    in_step && /^      - / { exit }
    in_step { print }
  ' "$CD"
}
CD_ENROL="$(cd_step_body "Enrol two machines against staging")"
CD_REMOVE="$(cd_step_body "Remove the staging machines")"

if [ "${#PINNED[@]}" -ge 2 ]; then
  pass "the specs pin ${#PINNED[@]} machines by name"
else
  fail "fewer than two machines are pinned — a spec cannot disturb one without breaking the rest"
fi

for machine in "${PINNED[@]}"; do
  if grep -q "hostname: $machine" "$COMPOSE"; then
    pass "$machine is in the local stack with a pinned hostname"
  else
    fail "$machine is missing from the local stack, or its hostname is not pinned — a spec cannot name a container id"
  fi

  # A pod's hostname is its name, so the deploy has to create it under exactly
  # the name the spec asks for — and take it away again afterwards, or the next
  # run inherits its device row.
  if grep -qE "(^|[[:space:]])$machine([[:space:]]|$)" <<<"$CD_ENROL"; then
    pass "$machine is brought up by the staging deploy"
  else
    fail "$machine is brought up by no staging deploy step — every spec that names it times out"
  fi

  if grep -qE "(^|[[:space:]])$machine([[:space:]]|$)" <<<"$CD_REMOVE"; then
    pass "$machine is removed when the staging run ends"
  else
    fail "$machine outlives the staging run, so the next run reads a machine nobody brought up"
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

# The staging machines dial the server by name, and the agent takes its TLS
# name from the host half of that address with no way to be told another. So
# the name a machine is given has to be the name the chart puts on the
# certificate, or every handshake is refused for a name the certificate does
# not carry. Both halves are read here, from the two files that decide them.
POD_MANIFEST="$(
  MACHINE=agent-a \
    RELEASE=rel \
    NODE_ARCH=arm64 \
    SERVER_POD_IP=203.0.113.1 \
    ENROLMENT_SECRET=secret-name \
    "$MACHINE_POD" 2>/dev/null || true
)"

if [ -z "$POD_MANIFEST" ]; then
  fail "the staging machine manifest could not be rendered"
else
  pass "the staging machine manifest renders"

  if grep -qF 'value: rel-server:9090' <<<"$POD_MANIFEST"; then
    pass "a staging machine dials the server by its in-cluster name"
  else
    fail "a staging machine dials something other than the server's in-cluster name — the certificate names no such host"
  fi

  # ...and the packets go to the server pod, because the Service carries the
  # HTTP port only and there is nothing listening on UDP behind its address.
  if grep -qF '203.0.113.1' <<<"$POD_MANIFEST" && grep -qF -- '- rel-server' <<<"$POD_MANIFEST"; then
    pass "the name resolves to the server pod the QUIC listener is in"
  else
    fail "the name is not pointed at the server pod, so the QUIC packets reach nothing"
  fi

  # Enrolment and QUIC are two addresses, aimed independently. The QUIC address
  # is the name on the certificate and stays the short one; the enrolment URL is
  # plain HTTP to the Service and defaults to the same short name here, which is
  # what the staging browser suite uses. Naming it separately is what lets the
  # network drill send enrolment to the fully-qualified Service name while the
  # certificate's name is pointed at its link shaper — an /etc/hosts entry for
  # the short name does not intercept the qualified form.
  enroll_url="$(grep -A1 'name: OPENGATE_ENROLL_URL' <<<"$POD_MANIFEST" | sed -n 's/^ *value: //p')"
  if [ "$enroll_url" = "http://rel-server:8080" ]; then
    pass "a machine enrols through the server's in-cluster name by default"
  else
    fail "the default enrolment URL is '$enroll_url', not the server's in-cluster name"
  fi

  OVERRIDDEN_MANIFEST="$(
    MACHINE=agent-a \
      RELEASE=rel \
      NODE_ARCH=arm64 \
      SERVER_POD_IP=203.0.113.1 \
      ENROLMENT_SECRET=secret-name \
      OPENGATE_ENROLL_URL=http://rel-server.ns.svc.cluster.local:8080 \
      "$MACHINE_POD" 2>/dev/null || true
  )"
  overridden_enroll="$(grep -A1 'name: OPENGATE_ENROLL_URL' <<<"$OVERRIDDEN_MANIFEST" | sed -n 's/^ *value: //p')"
  if [ "$overridden_enroll" = "http://rel-server.ns.svc.cluster.local:8080" ]; then
    pass "the enrolment URL can be aimed somewhere other than the QUIC address"
  else
    fail "the enrolment URL ignored the address it was given ('$overridden_enroll')"
  fi

  if grep -qF 'value: rel-server:9090' <<<"$OVERRIDDEN_MANIFEST"; then
    pass "aiming enrolment elsewhere leaves the certificate's name on the QUIC address"
  else
    fail "aiming enrolment elsewhere moved the QUIC address off the name the certificate carries"
  fi

  # The pod's name is its hostname, which is what the specs look a machine up by.
  if grep -qE '^  name: agent-a$' <<<"$POD_MANIFEST"; then
    pass "a staging machine's pod is named for the machine the specs ask for"
  else
    fail "the pod is not named for the machine, so its hostname reaches the API as something else"
  fi

  # The image is stock Alpine and the container is not root, so every directory
  # the agent writes has to be one that user can create. The log directory
  # defaults to a path under /var/log, which it cannot: both machines started,
  # died on their first line, and the fleet the suite waits for stayed empty.
  for dir_var in OPENGATE_DATA_DIR OPENGATE_LOG_DIR; do
    dir_value="$(grep -A1 "name: $dir_var" <<<"$POD_MANIFEST" | sed -n 's/^ *value: //p')"
    case "$dir_value" in
      /tmp/*)
        pass "$dir_var is a directory the machine's user can write"
        ;;
      "")
        fail "$dir_var is unset, so the agent takes a default its non-root user cannot write"
        ;;
      *)
        fail "$dir_var is $dir_value, which the machine's non-root user cannot write"
        ;;
    esac
  done

  # The token is a credential minted per run; it reaches the machine through a
  # Secret rather than sitting in a manifest anyone can read back.
  token_env="$(grep -A3 'name: OPENGATE_ENROLL_TOKEN' <<<"$POD_MANIFEST")"
  if grep -qF 'secretKeyRef' <<<"$token_env" && ! grep -qF 'value:' <<<"$token_env"; then
    pass "the enrolment token reaches the machine through a Secret"
  else
    fail "the enrolment token is written into the pod manifest in the clear"
  fi
fi

if grep -qF 'printf "%s-server" (include "opengate.fullname" .)' "$SERVER_DEPLOYMENT"; then
  pass "the certificate carries the in-cluster name by default"
else
  fail "the chart puts the in-cluster server name on the certificate nowhere, so an in-cluster machine cannot verify it"
fi

# The helper polls until a deadline and then throws a message naming the fleet
# it actually saw. Given the same deadline as the per-test timeout it never
# reaches the throw: the test dies first, and fourteen runs reported a bare
# timeout instead of "the fleet holds: an empty fleet".
helper_deadline="$(sed -n 's/.*Date\.now() + \([0-9_]*\).*/\1/p' "$HELPER" | tr -d '_' | head -n 1)"
test_timeout="$(sed -n 's/^[[:space:]]*timeout: \([0-9_]*\),.*/\1/p' "$PLAYWRIGHT_CONFIG" | tr -d '_' | head -n 1)"

if [ -z "$helper_deadline" ] || [ -z "$test_timeout" ]; then
  fail "could not read the helper's deadline or the per-test timeout"
elif [ "$helper_deadline" -lt "$test_timeout" ]; then
  pass "the helper gives up before the test does, so its message is printed"
else
  fail "the helper's deadline (${helper_deadline}ms) is not below the per-test timeout (${test_timeout}ms) — the test dies before the helper can say what the fleet held"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf 'Failures:\n' >&2
  for f in "${FAILURES[@]}"; do printf '  - %s\n' "$f" >&2; done
  exit 1
fi
