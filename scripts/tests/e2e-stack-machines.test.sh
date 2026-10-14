#!/usr/bin/env bash
# Keeps every stack that runs the browser suite carrying real machines.
# The machines a spec may name are pinned in one file, and both stacks are checked against it.

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

if grep -q 'OPENGATE_AGENT_RELAY_URL=ws://server:8080' "$COMPOSE"; then
  pass "machines dial the relay at the server's name on the stack's network"
else
  fail "OPENGATE_AGENT_RELAY_URL is not ws://server:8080, so a machine dials an address it cannot reach"
fi

# Machine names come from the file the specs read, so a new machine there is checked unedited.
mapfile -t PINNED < <(sed -n 's/^export const MACHINE_[A-Z0-9]* = "\([^"]*\)";$/\1/p' "$HELPER")

# Reading only the two staging step bodies keeps a comment's mention from satisfying a check.
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

  # A pod's hostname is its name, so the deploy creates and removes it under the spec's name.
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

# A stack stood up outside these entry points stages no agent binary and mints no token.
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

# A bare `compose up` skips the token mint between the two halves of the bring-up.
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

# The enrolment token is minted per bring-up and is in no checkout, while
# `docker compose config` reads every env_file it is pointed at.
if ! command -v docker >/dev/null 2>&1; then
  fail "docker is not on PATH, so the stack's shape cannot be validated"
else
  # An empty project directory stands in for a clean checkout, so a leftover token cannot mask CI.
  clean_dir="$(mktemp -d)"
  cp "$COMPOSE" "$clean_dir/docker-compose.test.yml"
  if docker compose -f "$clean_dir/docker-compose.test.yml" config --quiet >/dev/null 2>&1; then
    pass "the stack's shape validates without the bring-up's credential"
  else
    fail "the stack's shape needs a file no checkout carries, so config lint fails"
  fi
  STACK_JSON="$(docker compose -f "$clean_dir/docker-compose.test.yml" config --format json 2>/dev/null || true)"
  rm -rf "$clean_dir"
fi

# The direct-link specs rely on where each container sits, read as Compose renders the stack.
stack() { jq -r "$1" <<<"${STACK_JSON:-}"; }
ip_number() {
  local IFS=.
  read -r a b c d <<<"$1"
  echo $(((a << 24) | (b << 16) | (c << 8) | d))
}
in_range() {
  local net="${2%/*}" bits="${2#*/}"
  local mask=$(((0xffffffff << (32 - bits)) & 0xffffffff))
  [ $(($(ip_number "$1") & mask)) -eq $(($(ip_number "$net") & mask)) ]
}

if [ -z "${STACK_JSON:-}" ]; then
  fail "the stack could not be rendered, so where its containers sit cannot be checked"
else
  for lan in office home; do
    if [ "$(stack ".networks.$lan.driver_opts[\"com.docker.network.bridge.inhibit_ipv4\"]")" = "true" ]; then
      pass "the $lan bridge holds no host address, so its router is the only way in or out"
    else
      fail "the $lan bridge holds a host address, so the test machine reaches past its router"
    fi
  done

  bridges="$(stack '[.networks.office, .networks.home] | map(.driver_opts["com.docker.network.bridge.name"]) | join(":")')"
  trusted="$(stack '.networks.default.driver_opts["com.docker.network.bridge.trusted_host_interfaces"]')"
  if [ "$trusted" = "$bridges" ]; then
    pass "the host passes what the routers forward from the office and home bridges"
  else
    fail "the internet network trusts '$trusted' where the routers' bridges are '$bridges', so the host drops their traffic"
  fi

  if [ "$(stack '.networks.default.driver_opts["com.docker.network.bridge.enable_ip_masquerade"]')" = "false" ]; then
    pass "the host rewrites no address on the internet network, so a router's flows arrive as it sent them"
  else
    fail "the host rewrites addresses on the internet network, so the far side never sees a router's own mapping"
  fi

  for placed in "server:default" "agent-b:default" "agent-a:office" "browser:home" \
    "router-office:default office" "router-home:default home"; do
    svc="${placed%%:*}"
    want="${placed#*:}"
    got="$(stack ".services[\"$svc\"].networks // {} | keys | sort | join(\" \")")"
    if [ "$got" = "$want" ]; then
      pass "$svc sits on: $want"
    else
      fail "$svc sits on '$got' where the specs expect '$want'"
    fi
  done

  # Sent from several CPUs, a machine's packets cross the host's per-CPU queues out of order.
  for machine in agent-a agent-b; do
    if [ "$(stack ".services[\"$machine\"].cpuset // \"\"")" = "0" ]; then
      pass "$machine runs on one CPU, so its packets reach the far side in the order it sent them"
    else
      fail "$machine is not held to one CPU, so the host reorders its packets and the direct link reads each as a loss"
    fi
  done

  # Firefox gathers no address for a direct link on a page it reached over IPv6 loopback.
  if [ "$(stack '.services.server.ports[] | select(.target == 8080) | .host_ip // ""')" = "0.0.0.0" ]; then
    pass "the console is published on IPv4 alone, so a browser's localhost reaches it over 127.0.0.1"
  else
    fail "the console is published on IPv6 too, so Firefox reaches localhost over ::1 and offers no direct link"
  fi

  server_addr="$(stack '.services.server.networks.default.ipv4_address')"
  if [ "$(stack '.services.server.environment.OPENGATE_ICE_SERVERS')" = "stun:$server_addr:9090" ]; then
    pass "the helper the server names is the server's own fixed address"
  else
    fail "OPENGATE_ICE_SERVERS names something other than stun:$server_addr:9090"
  fi

  for lan in office home; do
    svc="router-$lan"
    env_of() { stack ".services[\"$svc\"].environment.$1"; }
    if [ "$(env_of ROUTER_WAN_ADDR)" = "$(stack ".services[\"$svc\"].networks.default.ipv4_address")" ] \
      && [ "$(env_of ROUTER_LAN_ADDR)" = "$(stack ".services[\"$svc\"].networks.$lan.ipv4_address")" ] \
      && [ "$(env_of ROUTER_LAN_GATEWAY)" = "$(stack ".networks.$lan.ipam.config[0].gateway")" ] \
      && [ "$(env_of ROUTER_HELPER)" = "$server_addr:9090" ]; then
      pass "$svc's rules name the addresses it holds and the server's helper"
    else
      fail "$svc's ROUTER_* settings disagree with the addresses Compose gives it"
    fi
    if [ "$(stack ".services[\"$svc\"].networks.default.gw_priority // 0")" -gt \
      "$(stack ".services[\"$svc\"].networks.$lan.gw_priority // 0")" ]; then
      pass "$svc leaves through the internet network"
    else
      fail "$svc's default route is its own LAN, so nothing it translates leaves"
    fi
  done

  if [ "$(stack '.services["router-home"].environment.ROUTER_FORWARD')" = "3000:$(stack '.services.browser.networks.home.ipv4_address')" ]; then
    pass "the test runner reaches the browser server through the home router"
  else
    fail "the home router forwards the browser server's port to an address the browser does not hold"
  fi

  # A container with no pinned address takes the next free one from ip_range.
  ip_range="$(stack '.networks.default.ipam.config[0].ip_range')"
  taken=""
  while IFS=' ' read -r svc addr; do
    in_range "$addr" "$ip_range" && taken="$taken $svc($addr)"
  done < <(stack '.services | to_entries[] | select(.value.networks.default.ipv4_address) | "\(.key) \(.value.networks.default.ipv4_address)"')
  if [ -z "$taken" ]; then
    pass "no pinned address lies in the range unpinned containers draw from"
  else
    fail "these pinned addresses lie in $ip_range, where a container started first can take them:$taken"
  fi
fi

if grep -qF 'package-lock.json' "$BRINGUP" && grep -qF 'PLAYWRIGHT_VERSION' "$BRINGUP"; then
  pass "the browser server runs the Playwright version the suite's lockfile holds"
else
  fail "the bring-up does not read the browser server's Playwright version from the suite's lockfile"
fi

# The stack's networks use gateway priority and trusted host interfaces, which Engine 28 added.
engine_stub="$(mktemp -d)"
cat >"$engine_stub/docker" <<'STUB'
#!/usr/bin/env bash
if [ "$1" = version ]; then
  echo "$STUB_ENGINE"
  exit 0
fi
echo "stub: docker $1 reached" >&2
exit 99
STUB
chmod +x "$engine_stub/docker"
old_engine="$(STUB_ENGINE=27.5.1 PATH="$engine_stub:$PATH" bash "$BRINGUP" 2>&1)" && old_rc=0 || old_rc=$?
new_engine="$(STUB_ENGINE=28.0.0 PATH="$engine_stub:$PATH" bash "$BRINGUP" 2>&1)" || true
rm -rf "$engine_stub"
if [ "$old_rc" -eq 1 ] && grep -qF 'upgrade Docker Engine to 28 or later' <<<"$old_engine" \
  && ! grep -qF 'stub: docker' <<<"$old_engine"; then
  pass "the bring-up refuses Docker Engine 27, naming the upgrade, before it starts anything"
else
  fail "the bring-up ran on Docker Engine 27 (exit $old_rc), or refused without naming the upgrade"
fi
if grep -qF 'upgrade Docker Engine' <<<"$new_engine"; then
  fail "the bring-up refuses Docker Engine 28"
else
  pass "the bring-up accepts Docker Engine 28"
fi

# The machines install through the public enrolment endpoint with a token minted at bring-up.
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

# chat.spec.ts alone may mock a device: its tab shows only for RemoteDesktop machines.
# The pattern is built, not inline, since ShellCheck reads a literal ${…} as a stray expansion.
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

# The agent takes its TLS name from the host half of the server address, so a machine's
# dial name must be the name the chart puts on the certificate.
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

  # The Service carries the HTTP port only, so the QUIC packets go to the server pod.
  if grep -qF '203.0.113.1' <<<"$POD_MANIFEST" && grep -qF -- '- rel-server' <<<"$POD_MANIFEST"; then
    pass "the name resolves to the server pod the QUIC listener is in"
  else
    fail "the name is not pointed at the server pod, so the QUIC packets reach nothing"
  fi

  # The QUIC address is the certificate's name; the enrolment URL is aimed separately so the
  # network drill can send enrolment to the qualified Service name.
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

  if grep -qE '^  name: agent-a$' <<<"$POD_MANIFEST"; then
    pass "a staging machine's pod is named for the machine the specs ask for"
  else
    fail "the pod is not named for the machine, so its hostname reaches the API as something else"
  fi

  # The non-root container user can create directories only under /tmp; the log default is /var/log.
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

  # The token is a credential, so it reaches the machine through a Secret, not the manifest.
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

# The helper's deadline stays below the per-test timeout so its error naming the fleet is reached.
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
