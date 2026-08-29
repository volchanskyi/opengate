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

# The credential plugin the real cluster is reached through writes a warning to
# stderr on every single call, whatever the verb and whatever the outcome. A
# caller that folds the two streams together reads that line as the first line
# of the JSON.
if [ -n "${FAKE_NOISY_STDERR:-}" ]; then
  echo "Warning: To increase security of your API key located at /home/runner/.oci/key.pem, append an extra line with 'OCI_API_KEY' at the end." >&2
fi

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

# The API server decodes acquireTime and renewTime as MicroTime — RFC3339 with
# exactly six digits of fractional seconds — and refuses the whole object when
# either is shaped any other way, before it considers who holds what. The
# stand-in holds the same line, so a manifest that could not be written to a
# real cluster cannot pass here either.
check_stamps() {
  local src="$1" name value
  for name in acquireTime renewTime; do
    value="$(field "$name" "$src")"
    if ! printf '%s' "$value" | grep -qE '^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{6}Z$'; then
      echo "Error from server (BadRequest): Lease in version \"v1\" cannot be handled as a Lease: parsing time \"$value\" as \"2006-01-02T15:04:05.000000Z07:00\"" >&2
      exit 1
    fi
  done
}

# Stands in for every refusal that is not a lost race — a credential without the
# rights, a webhook, a namespace being torn down.
refuse_write() {
  if [ -n "${FAKE_REFUSE_WRITE:-}" ]; then
    echo 'Error from server (Forbidden): leases.coordination.k8s.io is forbidden' >&2
    exit 1
  fi
}

# A namespace that is not there answers NotFound to every verb, so a `get` that
# reads as "no lease yet" is followed by a `create` that says NotFound too. That
# wording is a lost race on a replace and never on a create, and the difference
# is the whole of whether a run waits on a holder that cannot exist.
missing_namespace() {
  if [ -n "${FAKE_NO_NAMESPACE:-}" ]; then
    echo "Error from server (NotFound): namespaces \"opengate-staging\" not found" >&2
    exit 1
  fi
}

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
    missing_namespace
    if [ -f "$STATE" ]; then
      cat "$STATE"
      # The holder releases while a waiter is mid-loop, so the next read finds
      # the namespace empty and the waiter is left holding only a name that has
      # since left.
      [ -n "${FAKE_VANISH_AFTER:-}" ] && rm -f "$STATE"
      exit 0
    fi
    echo 'Error from server (NotFound): leases.coordination.k8s.io "guard" not found' >&2
    exit 1
    ;;
  create)
    cat >"$WORKDIR_IN"
    check_stamps "$WORKDIR_IN"
    missing_namespace
    refuse_write
    # The race itself: two runs both read an empty namespace and both create, so
    # the loser is told the object already exists while `get` still answers
    # NotFound, leaving it only the create's own answer to go on.
    if [ -n "${FAKE_CREATE_TAKEN:-}" ] || [ -f "$STATE" ]; then
      echo 'Error from server (AlreadyExists): leases.coordination.k8s.io "guard" already exists' >&2
      exit 1
    fi
    write_state "$WORKDIR_IN" 1
    exit 0
    ;;
  replace)
    cat >"$WORKDIR_IN"
    check_stamps "$WORKDIR_IN"
    refuse_write
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
# that somebody else is holding. Callers pass the renew stamp in the MicroTime
# the API stores, so the expiry read meets the shape it meets on a real cluster.
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

# The stamp is the whole object's admission ticket: a Lease whose times are not
# MicroTime is refused at decode, so nothing about holders is ever reached.
stamp_written="$(sed -n 's/^ *renewTime: "\(.*\)"$/\1/p' "$WORKDIR_IN" | head -1)"
if printf '%s' "$stamp_written" \
  | grep -qE '^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{6}Z$'; then
  pass "the claim it writes carries a timestamp the API accepts"
else
  fail "the claim it writes carries a timestamp the API accepts (got=[$stamp_written])"
fi

# Asking twice is not an error — a job that retries a step must not deadlock
# against the claim it already owns.
if run_lease acquire cd-1 >/dev/null 2>&1; then
  assert_eq "the holder can re-acquire its own claim" "cd-1" "$(holder_now)"
else
  fail "the holder can re-acquire its own claim"
fi

# Somebody else's live claim is waited on and then refused, rather than stolen.
seed_lease other-run "$(date -u +%Y-%m-%dT%H:%M:%S.000000Z)" 2700
if run_lease acquire cd-2 >/dev/null 2>&1; then
  fail "a live claim held by another run is refused"
else
  pass "a live claim held by another run is refused"
fi
assert_eq "the live holder is left in place" "other-run" "$(holder_now)"

# A holder that died without releasing must not wedge the namespace until
# somebody notices: the claim ages out and the next run takes it.
seed_lease dead-run "$(date -u -d '2 hours ago' +%Y-%m-%dT%H:%M:%S.000000Z)" 60
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
seed_lease someone-else "$(date -u +%Y-%m-%dT%H:%M:%S.000000Z)" 2700
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

# Losing the create race must be waited out, not exited on. The refusal arrives
# as a non-zero kubectl inside a script that runs under `set -e`, so the arm
# handling it is one edit away from ending the run instead of looping.
rm -f "$STATE"
if out="$(FAKE_CREATE_TAKEN=1 timeout 20 env \
  NAMESPACE=opengate-staging \
  STAGING_LEASE_NAME=guard \
  STAGING_LEASE_KUBECTL="$FAKE" \
  STAGING_LEASE_WAIT_SECONDS=2 \
  STAGING_LEASE_POLL_SECONDS=1 \
  "$LEASE" acquire cd-7 2>&1)"; then
  fail "losing the create race waits rather than ending the run"
elif printf '%s' "$out" | grep -q 'waiting' \
  && printf '%s' "$out" | grep -q 'did not free within'; then
  pass "losing the create race waits rather than ending the run"
else
  fail "losing the create race waits rather than ending the run (got=[$out])"
fi

# Losing the race is the one refusal this lock is built on. Every other refusal
# is a fault of ours — a manifest the API will not decode, a credential without
# the rights — and spending the wait on it reports a holder that does not exist
# for as long as the deadline allows, which is the shape a stuck deploy takes.
rm -f "$STATE"
started="$(date -u +%s)"
if out="$(FAKE_REFUSE_WRITE=1 timeout 10 env \
  NAMESPACE=opengate-staging \
  STAGING_LEASE_NAME=guard \
  STAGING_LEASE_KUBECTL="$FAKE" \
  STAGING_LEASE_WAIT_SECONDS=60 \
  STAGING_LEASE_POLL_SECONDS=1 \
  "$LEASE" acquire cd-5 2>&1)"; then
  fail "a create refused for anything but a lost race stops rather than waiting"
elif [ "$(($(date -u +%s) - started))" -ge 10 ]; then
  fail "a create refused for anything but a lost race stops rather than waiting (it waited)"
else
  pass "a create refused for anything but a lost race stops rather than waiting"
fi
if printf '%s' "$out" | grep -qi 'forbidden'; then
  pass "the server's reason for refusing the create is reported"
else
  fail "the server's reason for refusing the create is reported (got=[$out])"
fi

# The same for the takeover write: an expired claim that cannot be replaced for
# a reason of ours is not somebody else holding the namespace.
seed_lease dead-run "$(date -u -d '2 hours ago' +%Y-%m-%dT%H:%M:%S.000000Z)" 60
started="$(date -u +%s)"
if out="$(FAKE_REFUSE_WRITE=1 timeout 10 env \
  NAMESPACE=opengate-staging \
  STAGING_LEASE_NAME=guard \
  STAGING_LEASE_KUBECTL="$FAKE" \
  STAGING_LEASE_WAIT_SECONDS=60 \
  STAGING_LEASE_POLL_SECONDS=1 \
  "$LEASE" acquire cd-6 2>&1)"; then
  fail "a takeover refused for anything but a lost race stops rather than waiting"
elif [ "$(($(date -u +%s) - started))" -ge 10 ]; then
  fail "a takeover refused for anything but a lost race stops rather than waiting (it waited)"
else
  pass "a takeover refused for anything but a lost race stops rather than waiting"
fi

# NotFound is a lost race on a replace and never on a create: nothing that
# already exists can answer "not found", so a create told that is refused for a
# reason of ours — a namespace missing or being torn down — and no amount of
# waiting produces the holder the message names. This is the shape the stuck
# deploy took: an empty namespace, a refusal nobody read, and a run that spent
# its whole window reporting a holder that was never there.
rm -f "$STATE"
started="$(date -u +%s)"
if out="$(FAKE_NO_NAMESPACE=1 timeout 10 env \
  NAMESPACE=opengate-staging \
  STAGING_LEASE_NAME=guard \
  STAGING_LEASE_KUBECTL="$FAKE" \
  STAGING_LEASE_WAIT_SECONDS=60 \
  STAGING_LEASE_POLL_SECONDS=1 \
  "$LEASE" acquire cd-8 2>&1)"; then
  fail "a create refused NotFound stops rather than waiting on a phantom holder"
elif [ "$(($(date -u +%s) - started))" -ge 10 ]; then
  fail "a create refused NotFound stops rather than waiting on a phantom holder (it waited)"
else
  pass "a create refused NotFound stops rather than waiting on a phantom holder"
fi
if printf '%s' "$out" | grep -qi 'namespaces' && ! printf '%s' "$out" | grep -q 'held by another run'; then
  pass "the missing namespace is named rather than reported as contention"
else
  fail "the missing namespace is named rather than reported as contention (got=[$out])"
fi

# A run that waits on a live holder and then finds the claim gone must not carry
# the old holder's name into whatever happens next. Losing the create race that
# follows is reported as the race it is, not as the run that has already left.
#
# The assertion is on the last line alone, because how many times the loop polls
# before its deadline depends on how fast the machine is: a busy one spends the
# whole three seconds on one pass, an idle one gets three. The property does not
# — whatever line the run ends on, it must not still be naming a holder that has
# gone.
rm -f "$STATE"
seed_lease other-run "$(date -u +%Y-%m-%dT%H:%M:%S.000000Z)" 2700
if out="$(FAKE_CREATE_TAKEN=1 timeout 20 env \
  NAMESPACE=opengate-staging \
  STAGING_LEASE_NAME=guard \
  STAGING_LEASE_KUBECTL="$FAKE" \
  STAGING_LEASE_WAIT_SECONDS=3 \
  STAGING_LEASE_POLL_SECONDS=1 \
  FAKE_VANISH_AFTER=1 \
  "$LEASE" acquire cd-9 2>&1)"; then
  fail "a holder that has gone is not still named once the claim is gone"
elif printf '%s' "$out" | grep -q 'held by other-run; waiting' \
  && ! printf '%s' "$out" | tail -1 | grep -q 'other-run'; then
  pass "a holder that has gone is not still named once the claim is gone"
else
  fail "a holder that has gone is not still named once the claim is gone (got=[$out])"
fi

# --- the read is JSON, and only JSON -------------------------------------------
#
# Every call to the cluster goes through a credential plugin that writes a
# warning to stderr, so a read that folds stderr into stdout hands its caller a
# line of prose followed by the object. Nothing about the lease is wrong at that
# point; the parse of it is, and the step dies holding a claim it was there to
# release.

# Reading a claim somebody else holds still reports contention.
rm -f "$STATE"
seed_lease other-run "$(date -u +%Y-%m-%dT%H:%M:%S.000000Z)" 2700
if out="$(FAKE_NOISY_STDERR=1 run_lease acquire cd-10 2>&1)"; then
  fail "a warning on stderr does not become the first line of the lease"
elif printf '%s' "$out" | grep -q 'held by other-run' \
  && ! printf '%s' "$out" | grep -qi 'parse error'; then
  pass "a warning on stderr does not become the first line of the lease"
else
  fail "a warning on stderr does not become the first line of the lease (got=[$out])"
fi

# And the release the noise actually broke: the claim comes off the namespace.
rm -f "$STATE"
seed_lease cd-11 "$(date -u +%Y-%m-%dT%H:%M:%S.000000Z)" 2700
if out="$(FAKE_NOISY_STDERR=1 run_lease release cd-11 2>&1)" && [ ! -f "$STATE" ]; then
  pass "a noisy credential plugin does not wedge the namespace at release"
else
  fail "a noisy credential plugin does not wedge the namespace at release (got=[$out])"
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
