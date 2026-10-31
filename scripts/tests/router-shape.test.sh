#!/usr/bin/env bash
# Tests for deploy/router/router.sh shape, which holds each direction to the line's rate before it
# delays it, so a reordered packet skips the delay and never the queue.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
ROUTER="$REPO_ROOT/deploy/router/router.sh"
[[ -x "$ROUTER" ]] || {
  echo "FAIL: $ROUTER not executable" >&2
  exit 1
}

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

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
mkdir -p "$WORK/bin" "$WORK/state"

# The router's two interfaces, as ip reports the addresses they hold.
cat >"$WORK/bin/ip" <<'EOF'
#!/usr/bin/env bash
[ "$*" = "-o -4 addr show" ] || { echo "ip: unexpected $*" >&2; exit 2; }
echo "2: eth0    inet 10.77.1.1/24 brd 10.77.1.255 scope global eth0"
echo "3: eth1    inet 10.77.0.21/24 brd 10.77.0.255 scope global eth1"
EOF

# Tc keeps a root qdisc per device and a child under it; replacing the root keeps the child,
# deleting it takes the child too, and a child needs the root it names.
cat >"$WORK/bin/tc" <<EOF
#!/usr/bin/env bash
state="$WORK/state"
case "\$1 \$2" in
  "qdisc show") cat "\$state/\$4".* 2>/dev/null || true ;;
  "qdisc replace")
    dev="\$4"
    case "\$5 \$6 \$7" in
      "root handle 1:")
        shift 7
        echo "root \$*" >"\$state/\$dev.root"
        ;;
      "parent 1:1 handle")
        [ -e "\$state/\$dev.root" ] || { echo "Error: Failed to find specified qdisc" >&2; exit 2; }
        shift 8
        echo "child \$*" >"\$state/\$dev.child"
        ;;
      *) echo "tc: unexpected \$*" >&2; exit 2 ;;
    esac
    ;;
  "qdisc del")
    [ "\$5" = root ] && [ -e "\$state/\$4.root" ] || { echo "Error: no such qdisc" >&2; exit 2; }
    rm -f "\$state/\$4.root" "\$state/\$4.child"
    ;;
  *) echo "tc: unexpected \$*" >&2; exit 2 ;;
esac
EOF
chmod +x "$WORK/bin/ip" "$WORK/bin/tc"

router() {
  PATH="$WORK/bin:$PATH" ROUTER_LAN_ADDR=10.77.1.1 ROUTER_WAN_ADDR=10.77.0.21 \
    ROUTER_LAN_GATEWAY=10.77.1.254 ROUTER_HELPER=10.77.0.30:3478 "$ROUTER" "$@"
}

qdiscs() {
  cat "$WORK/state/$1".* 2>/dev/null || true
}

shape() {
  if ! router shape "$@" 2>"$WORK/err.txt"; then
    fail "shape $* runs ($(cat "$WORK/err.txt"))"
  fi
}

shape 100 40 1
for dev in eth0 eth1; do
  held="$(qdiscs "$dev")"
  if grep -qxF "root netem rate 100mbit" <<<"$held"; then
    pass "packets leaving $dev queue for the 100 Mbit/s line first"
  else
    fail "packets leaving $dev queue for the 100 Mbit/s line first ($held)"
  fi
  if grep -qxF "child netem delay 20ms reorder 1%" <<<"$held"; then
    pass "past the queue, packets leaving $dev wait half the round trip and 1% skip that wait"
  else
    fail "past the queue, packets leaving $dev wait half the round trip and 1% skip that wait ($held)"
  fi
done

shape 2 40
held="$(qdiscs eth0)"
if grep -qxF "root netem rate 2mbit" <<<"$held" && grep -qxF "child netem delay 20ms" <<<"$held"; then
  pass "a new shape replaces the rate and the delay, and with no reordering every packet waits"
else
  fail "a new shape replaces the rate and the delay, and with no reordering every packet waits ($held)"
fi

shape off
left="$(cat "$WORK/state"/* 2>/dev/null || true)"
if [ -z "$left" ]; then
  pass "shape off leaves no rate or delay behind"
else
  fail "shape off leaves no rate or delay behind ($left)"
fi
if router shape off; then
  pass "shape off on an unshaped router changes nothing and succeeds"
else
  fail "shape off on an unshaped router changes nothing and succeeds"
fi

if router shape 100 >/dev/null 2>&1; then
  fail "a shape with no round trip is refused"
else
  pass "a shape with no round trip is refused"
fi

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
