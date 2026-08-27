#!/usr/bin/env bash
# Tests for scripts/staging-lease.sh — one holder at a time, and a claim nobody
# is holding any more does not wedge the namespace forever.
#
# The kubectl stand-in keeps real state in a file, so a create that should have
# been refused is visible as the wrong holder afterwards rather than as a fake
# that answered the same way regardless.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
LEASE="$REPO_ROOT/scripts/staging-lease.sh"
[ -x "$LEASE" ] || {
  echo "FAIL: $LEASE not executable" >&2
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
assert_eq() {
  local name="$1" want="$2" got="$3"
  if [ "$want" = "$got" ]; then pass "$name"; else fail "$name (want=[$want] got=[$got])"; fi
}

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

FAKE="$WORK/kubectl"
cat >"$FAKE" <<'FAKE_KUBECTL'
#!/usr/bin/env bash
# Stand-in for kubectl, backed by one JSON file so state really changes.
set -uo pipefail
STATE="$FAKE_STATE"

if [ -n "${FAKE_HARD_ERROR:-}" ]; then
  echo "Error from server (InternalError): the server is having trouble" >&2
  exit 1
fi

verb=""
for a in "$@"; do
  case "$a" in
    get | create | replace | delete)
      verb="$a"
      break
      ;;
  esac
done

# Greedy `.*` would swallow the closing quote, so the value is taken whole and
# unquoted afterwards.
field() { sed -n "s/^ *$1: \(.*\)$/\1/p" "$2" | head -1 | sed 's/^"//; s/"$//'; }

write_state() {
  local src="$1" rv="$2" holder renew dur
  holder="$(field holderIdentity "$src")"
  renew="$(field renewTime "$src")"
  dur="$(sed -n 's/^ *leaseDurationSeconds: \(.*\)$/\1/p' "$src" | head -1)"
  printf '{"metadata":{"resourceVersion":"%s"},"spec":{"holderIdentity":"%s","renewTime":"%s","leaseDurationSeconds":%s}}\n' \
    "$rv" "$holder" "$renew" "$dur" >"$STATE"
}

case "$verb" in
  get)
    if [ -f "$STATE" ]; then
      cat "$STATE"
      exit 0
    fi
    echo 'Error from server (NotFound): leases.coordination.k8s.io "guard" not found' >&2
    exit 1
    ;;
  create)
    cat >"$WORKDIR_IN"
    if [ -f "$STATE" ]; then
      echo 'Error from server (AlreadyExists): leases.coordination.k8s.io "guard" already exists' >&2
      exit 1
    fi
    write_state "$WORKDIR_IN" 1
    exit 0
    ;;
  replace)
    cat >"$WORKDIR_IN"
    [ -f "$STATE" ] || {
      echo 'Error from server (NotFound)' >&2
      exit 1
    }
    sent="$(sed -n 's/^ *resourceVersion: "\(.*\)"$/\1/p' "$WORKDIR_IN" | head -1)"
    have="$(sed -n 's/.*"resourceVersion":"\([^"]*\)".*/\1/p' "$STATE")"
    if [ "$sent" != "$have" ]; then
      echo 'Error from server (Conflict): the object has been modified' >&2
      exit 1
    fi
    write_state "$WORKDIR_IN" "$((have + 1))"
    exit 0
    ;;
  delete)
    rm -f "$STATE"
    exit 0
    ;;
esac
echo "fake kubectl: unhandled: $*" >&2
exit 1
FAKE_KUBECTL
chmod +x "$FAKE"

STATE="$WORK/lease.json"
export FAKE_STATE="$STATE"
export WORKDIR_IN="$WORK/stdin.yaml"

run_lease() {
  NAMESPACE=opengate-staging \
    STAGING_LEASE_NAME=guard \
    STAGING_LEASE_KUBECTL="$FAKE" \
    STAGING_LEASE_WAIT_SECONDS="${WAIT_OVERRIDE:-0}" \
    STAGING_LEASE_POLL_SECONDS=1 \
    STAGING_LEASE_TTL_SECONDS="${TTL_OVERRIDE:-2700}" \
    "$LEASE" "$@"
}

holder_now() { sed -n 's/.*"holderIdentity":"\([^"]*\)".*/\1/p' "$STATE"; }

# Writes a claim directly, bypassing the script, so a test can set up a lease
# that somebody else is holding.
seed_lease() {
  local holder="$1" renew="$2" dur="$3"
  printf '{"metadata":{"resourceVersion":"7"},"spec":{"holderIdentity":"%s","renewTime":"%s","leaseDurationSeconds":%s}}\n' \
    "$holder" "$renew" "$dur" >"$STATE"
}

echo "staging-lease:"

# A free namespace is taken.
rm -f "$STATE"
if run_lease acquire cd-1 >/dev/null 2>&1; then
  assert_eq "an unheld namespace is acquired" "cd-1" "$(holder_now)"
else
  fail "an unheld namespace is acquired"
fi

# Asking twice is not an error — a job that retries a step must not deadlock
# against the claim it already owns.
if run_lease acquire cd-1 >/dev/null 2>&1; then
  assert_eq "the holder can re-acquire its own claim" "cd-1" "$(holder_now)"
else
  fail "the holder can re-acquire its own claim"
fi

# Somebody else's live claim is waited on and then refused, rather than stolen.
seed_lease other-run "$(date -u +%Y-%m-%dT%H:%M:%SZ)" 2700
if run_lease acquire cd-2 >/dev/null 2>&1; then
  fail "a live claim held by another run is refused"
else
  pass "a live claim held by another run is refused"
fi
assert_eq "the live holder is left in place" "other-run" "$(holder_now)"

# A holder that died without releasing must not wedge the namespace until
# somebody notices: the claim ages out and the next run takes it.
seed_lease dead-run "$(date -u -d '2 hours ago' +%Y-%m-%dT%H:%M:%SZ)" 60
if run_lease acquire cd-3 >/dev/null 2>&1; then
  assert_eq "an expired claim is taken over" "cd-3" "$(holder_now)"
else
  fail "an expired claim is taken over"
fi

# Releasing frees it for the next run.
if run_lease release cd-3 >/dev/null 2>&1; then
  if [ -f "$STATE" ]; then
    fail "releasing the claim removes it"
  else
    pass "releasing the claim removes it"
  fi
else
  fail "releasing the claim removes it"
fi

# Releasing a claim that has already been taken over must not drop the live
# holder's lock — the cleanup step runs `always()`, including after the job
# overran its own lease.
seed_lease someone-else "$(date -u +%Y-%m-%dT%H:%M:%SZ)" 2700
run_lease release cd-3 >/dev/null 2>&1 || true
assert_eq "releasing somebody else's claim leaves it alone" "someone-else" "$(holder_now)"

# Releasing when there is nothing there is a no-op, not a failure.
rm -f "$STATE"
if run_lease release cd-3 >/dev/null 2>&1; then
  pass "releasing nothing succeeds"
else
  fail "releasing nothing succeeds"
fi

# A cluster that cannot be read is not an empty namespace. Treating the two the
# same would hand the lock to every waiter at once during an outage.
rm -f "$STATE"
if FAKE_HARD_ERROR=1 run_lease acquire cd-4 >/dev/null 2>&1; then
  fail "an unreadable cluster fails rather than reading as unheld"
else
  pass "an unreadable cluster fails rather than reading as unheld"
fi

# A missing holder identity is a usage error, not a lease held by the empty
# string that nothing can ever release.
if run_lease acquire >/dev/null 2>&1; then
  fail "a missing holder identity is refused"
else
  pass "a missing holder identity is refused"
fi

# --- the workflows name one holder, and both steps use it ----------------------
#
# A claim is released only by the identity holding it. Written out at each call
# site, acquire and release can drift apart without anything failing: the
# release matches nothing, says so, and the namespace stays locked until the
# claim times out. So each workflow names its holder once and both steps read
# that.
for wf in "$REPO_ROOT"/.github/workflows/*.yml; do
  grep -qF 'staging-lease.sh' "$wf" || continue
  name="$(basename "$wf")"

  if grep -qE '^ +STAGING_LEASE_HOLDER:' "$wf"; then
    pass "$name names its lease holder once"
  else
    fail "$name calls the lease without naming a holder its steps can share"
  fi

  inline="$(grep -c -E 'staging-lease\.sh (acquire|release) "[^$]' "$wf" || true)"
  if [ "$inline" -eq 0 ]; then
    pass "$name takes and releases the claim under the same identity"
  else
    fail "$name writes the holder out at $inline call site(s) instead of reading the shared one"
  fi
done

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf 'Failures:\n' >&2
  for f in "${FAILURES[@]}"; do printf '  - %s\n' "$f" >&2; done
  exit 1
fi
