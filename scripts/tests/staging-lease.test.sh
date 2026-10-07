#!/usr/bin/env bash
# Tests for scripts/staging-lease.sh: one holder at a time, and an abandoned claim expires.
# The kubectl stand-in keeps its state in a file, so a wrongly accepted create shows as a holder.
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

# The cluster's credential plugin writes a warning to stderr on every call, so a caller that
# merges the streams reads that line as the first line of the JSON.
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
    get | create | replace | delete | exec)
      verb="$a"
      break
      ;;
  esac
done

# Grafana silences calls arrive as `sh -c <script> <$0> <method> <path>` after `--`.
# Each open, extension and close is recorded with the silence comment naming the holder.
quiet_call() {
  local remote=() after="" method path body id comment
  for a in "$@"; do
    if [ -n "$after" ]; then
      remote+=("$a")
    elif [ "$a" = "--" ]; then
      after=1
    fi
  done
  method="${remote[4]:-}"
  path="${remote[5]:-}"
  [ -f "$FAKE_SILENCES" ] || echo '[]' >"$FAKE_SILENCES"
  if [ -n "${FAKE_QUIET_REFUSE:-}" ]; then
    echo '{"message":"Invalid username or password"}'
    echo "command terminated with exit code 22" >&2
    exit 22
  fi
  case "$method $path" in
    "GET /silences")
      cat "$FAKE_SILENCES"
      ;;
    "POST /silences")
      body="$(cat)"
      id="$(jq -r '.id // empty' <<<"$body")"
      [ -n "$id" ] || id="s-$(($(jq 'length' "$FAKE_SILENCES") + 1))"
      jq --arg id "$id" --argjson s "$body" \
        '[.[] | select(.id != $id)] + [$s + {id: $id, status: {state: "active"}}]' \
        "$FAKE_SILENCES" >"$FAKE_SILENCES.new"
      mv "$FAKE_SILENCES.new" "$FAKE_SILENCES"
      printf 'POST %s\n' "$(jq -r '.comment' <<<"$body")" >>"$FAKE_QUIET_CALLS"
      printf '{"silenceID":"%s"}\n' "$id"
      ;;
    "DELETE /silence/"*)
      id="${path#/silence/}"
      comment="$(jq -r --arg id "$id" '.[] | select(.id == $id) | .comment' "$FAKE_SILENCES")"
      jq --arg id "$id" 'map(if .id == $id then .status.state = "expired" else . end)' \
        "$FAKE_SILENCES" >"$FAKE_SILENCES.new"
      mv "$FAKE_SILENCES.new" "$FAKE_SILENCES"
      printf 'DELETE %s\n' "$comment" >>"$FAKE_QUIET_CALLS"
      echo '{"message":"silence deleted"}'
      ;;
    *)
      echo "fake kubectl: unhandled quiet-period call: ${remote[*]}" >&2
      exit 1
      ;;
  esac
}

# The manifest as the API server reads it: converted from YAML to JSON, and refused whole when
# that fails.
manifest_json() {
  python3 -c '
import json, sys, yaml
try:
    doc = yaml.safe_load(open(sys.argv[1], encoding="utf-8"))
except yaml.YAMLError as err:
    detail = " ".join(str(err).split())
    sys.stderr.write("error: error parsing STDIN: error converting YAML to JSON: %s\n" % detail)
    sys.exit(1)
json.dump(doc, sys.stdout)
' "$1"
}

# field <jq path> <manifest json> prints one value of the parsed manifest.
field() {
  jq -r "$1 // empty" <<<"$2"
}

# The API server decodes acquireTime and renewTime as MicroTime, RFC3339 with exactly six
# fractional digits, and refuses the whole object otherwise.
check_stamps() {
  local doc="$1" name value
  for name in acquireTime renewTime; do
    value="$(field ".spec.$name" "$doc")"
    if ! grep -qE '^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{6}Z$' <<<"$value"; then
      echo "Error from server (BadRequest): Lease in version \"v1\" cannot be handled as a Lease: parsing time \"$value\" as \"2006-01-02T15:04:05.000000Z07:00\"" >&2
      exit 1
    fi
  done
}

# Stands in for every refusal other than a lost race.
refuse_write() {
  if [ -n "${FAKE_REFUSE_WRITE:-}" ]; then
    echo 'Error from server (Forbidden): leases.coordination.k8s.io is forbidden' >&2
    exit 1
  fi
}

# A missing namespace answers NotFound to every verb, create included; NotFound means a lost
# race on a replace only.
missing_namespace() {
  if [ -n "${FAKE_NO_NAMESPACE:-}" ]; then
    echo "Error from server (NotFound): namespaces \"opengate-staging\" not found" >&2
    exit 1
  fi
}

write_state() {
  local doc="$1" rv="$2"
  jq -c --arg rv "$rv" \
    '{metadata: {resourceVersion: $rv},
      spec: {holderIdentity: .spec.holderIdentity, acquireTime: .spec.acquireTime,
             renewTime: .spec.renewTime, leaseDurationSeconds: .spec.leaseDurationSeconds}}' \
    <<<"$doc" >"$STATE"
}

case "$verb" in
  get)
    missing_namespace
    if [ -f "$STATE" ]; then
      cat "$STATE"
      # The holder releases mid-loop, so the waiter's next read finds the namespace empty.
      [ -n "${FAKE_VANISH_AFTER:-}" ] && rm -f "$STATE"
      exit 0
    fi
    echo 'Error from server (NotFound): leases.coordination.k8s.io "guard" not found' >&2
    exit 1
    ;;
  create)
    cat >"$WORKDIR_IN"
    doc="$(manifest_json "$WORKDIR_IN")" || exit 1
    check_stamps "$doc"
    missing_namespace
    refuse_write
    # Two runs both read an empty namespace and both create; the loser hears AlreadyExists
    # while `get` still answers NotFound.
    if [ -n "${FAKE_CREATE_TAKEN:-}" ] || [ -f "$STATE" ]; then
      echo 'Error from server (AlreadyExists): leases.coordination.k8s.io "guard" already exists' >&2
      exit 1
    fi
    write_state "$doc" 1
    exit 0
    ;;
  replace)
    cat >"$WORKDIR_IN"
    doc="$(manifest_json "$WORKDIR_IN")" || exit 1
    check_stamps "$doc"
    printf '%s\n' "$doc" >>"$FAKE_REPLACES"
    refuse_write
    [ -f "$STATE" ] || {
      echo 'Error from server (NotFound)' >&2
      exit 1
    }
    sent="$(field .metadata.resourceVersion "$doc")"
    have="$(jq -r '.metadata.resourceVersion' "$STATE")"
    if [ "$sent" != "$have" ]; then
      echo 'Error from server (Conflict): the object has been modified' >&2
      exit 1
    fi
    write_state "$doc" "$((have + 1))"
    exit 0
    ;;
  delete)
    rm -f "$STATE"
    exit 0
    ;;
  exec)
    quiet_call "$@"
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
export FAKE_SILENCES="$WORK/silences.json"
export FAKE_QUIET_CALLS="$WORK/quiet-calls.log"
touch "$FAKE_QUIET_CALLS"
export FAKE_REPLACES="$WORK/replaces.jsonl"
: >"$FAKE_REPLACES"

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

# Writes a claim directly with a MicroTime renew stamp, as a lease another run holds.
seed_lease() {
  local holder="$1" renew="$2" dur="$3"
  printf '{"metadata":{"resourceVersion":"7"},"spec":{"holderIdentity":"%s","renewTime":"%s","leaseDurationSeconds":%s}}\n' \
    "$holder" "$renew" "$dur" >"$STATE"
}

echo "staging-lease:"

rm -f "$STATE"
if run_lease acquire cd-1 >/dev/null 2>&1; then
  assert_eq "an unheld namespace is acquired" "cd-1" "$(holder_now)"
else
  fail "an unheld namespace is acquired"
fi

stamp_written="$(python3 -c 'import json, sys, yaml; print(yaml.safe_load(open(sys.argv[1]))["spec"]["renewTime"])' "$WORKDIR_IN" || true)"
if grep -qE '^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{6}Z$' \
  <<<"$stamp_written"; then
  pass "the claim it writes carries a timestamp the API accepts"
else
  fail "the claim it writes carries a timestamp the API accepts (got=[$stamp_written])"
fi

if run_lease acquire cd-1 >/dev/null 2>&1; then
  assert_eq "the holder can re-acquire its own claim" "cd-1" "$(holder_now)"
else
  fail "the holder can re-acquire its own claim"
fi

seed_lease other-run "$(date -u +%Y-%m-%dT%H:%M:%S.000000Z)" 2700
if run_lease acquire cd-2 >/dev/null 2>&1; then
  fail "a live claim held by another run is refused"
else
  pass "a live claim held by another run is refused"
fi
assert_eq "the live holder is left in place" "other-run" "$(holder_now)"

seed_lease dead-run "$(date -u -d '2 hours ago' +%Y-%m-%dT%H:%M:%S.000000Z)" 60
if run_lease acquire cd-3 >/dev/null 2>&1; then
  assert_eq "an expired claim is taken over" "cd-3" "$(holder_now)"
else
  fail "an expired claim is taken over"
fi

last_replace() { tail -n 1 "$FAKE_REPLACES"; }
assert_eq "the takeover it sends parses and carries the version it read" "7" \
  "$(jq -r '.metadata.resourceVersion // empty' <<<"$(last_replace)")"

if run_lease release cd-3 >/dev/null 2>&1; then
  if [ -f "$STATE" ]; then
    fail "releasing the claim removes it"
  else
    pass "releasing the claim removes it"
  fi
else
  fail "releasing the claim removes it"
fi

seed_lease someone-else "$(date -u +%Y-%m-%dT%H:%M:%S.000000Z)" 2700
run_lease release cd-3 >/dev/null 2>&1 || true
assert_eq "releasing somebody else's claim leaves it alone" "someone-else" "$(holder_now)"

rm -f "$STATE"
if run_lease release cd-3 >/dev/null 2>&1; then
  pass "releasing nothing succeeds"
else
  fail "releasing nothing succeeds"
fi

rm -f "$STATE"
if FAKE_HARD_ERROR=1 run_lease acquire cd-4 >/dev/null 2>&1; then
  fail "an unreadable cluster fails rather than reading as unheld"
else
  pass "an unreadable cluster fails rather than reading as unheld"
fi

rm -f "$STATE"
if out="$(FAKE_CREATE_TAKEN=1 timeout 20 env \
  NAMESPACE=opengate-staging \
  STAGING_LEASE_NAME=guard \
  STAGING_LEASE_KUBECTL="$FAKE" \
  STAGING_LEASE_WAIT_SECONDS=2 \
  STAGING_LEASE_POLL_SECONDS=1 \
  "$LEASE" acquire cd-7 2>&1)"; then
  fail "losing the create race waits rather than ending the run"
elif grep -q 'waiting' <<<"$out" \
  && grep -q 'did not free within' <<<"$out"; then
  pass "losing the create race waits rather than ending the run"
else
  fail "losing the create race waits rather than ending the run (got=[$out])"
fi

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
if grep -qi 'forbidden' <<<"$out"; then
  pass "the server's reason for refusing the create is reported"
else
  fail "the server's reason for refusing the create is reported (got=[$out])"
fi

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
if grep -qi 'namespaces' <<<"$out" && ! grep -q 'held by another run' <<<"$out"; then
  pass "the missing namespace is named rather than reported as contention"
else
  fail "the missing namespace is named rather than reported as contention (got=[$out])"
fi

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
elif grep -q 'held by other-run; waiting' <<<"$out" \
  && ! grep -q 'other-run' <<<"$(tail -1 <<<"$out")"; then
  pass "a holder that has gone is not still named once the claim is gone"
else
  fail "a holder that has gone is not still named once the claim is gone (got=[$out])"
fi

rm -f "$STATE"
seed_lease other-run "$(date -u +%Y-%m-%dT%H:%M:%S.000000Z)" 2700
if out="$(FAKE_NOISY_STDERR=1 run_lease acquire cd-10 2>&1)"; then
  fail "a warning on stderr does not become the first line of the lease"
elif grep -q 'held by other-run' <<<"$out" \
  && ! grep -qi 'parse error' <<<"$out"; then
  pass "a warning on stderr does not become the first line of the lease"
else
  fail "a warning on stderr does not become the first line of the lease (got=[$out])"
fi

rm -f "$STATE"
seed_lease cd-11 "$(date -u +%Y-%m-%dT%H:%M:%S.000000Z)" 2700
if out="$(FAKE_NOISY_STDERR=1 run_lease release cd-11 2>&1)" && [ ! -f "$STATE" ]; then
  pass "a noisy credential plugin does not wedge the namespace at release"
else
  fail "a noisy credential plugin does not wedge the namespace at release (got=[$out])"
fi

if run_lease acquire >/dev/null 2>&1; then
  fail "a missing holder identity is refused"
else
  pass "a missing holder identity is refused"
fi

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

  releases="$(grep -c -E 'staging-lease\.sh release' "$wf" || true)"
  swallowed="$(grep -c -E 'staging-lease\.sh release[^#]*\|\|' "$wf" || true)"
  if [ "$releases" -eq 0 ]; then
    fail "$name calls the lease and never releases it"
  elif [ "$swallowed" -eq 0 ]; then
    pass "$name lets a claim lost mid-run fail its release"
  else
    fail "$name throws away the release's status at $swallowed call site(s), so a claim lost mid-run reads green"
  fi
done

RENEW_WORK="$WORK/renew"
mkdir -p "$RENEW_WORK"

run_lease_renewing() {
  NAMESPACE=opengate-staging \
    STAGING_LEASE_NAME=guard \
    STAGING_LEASE_KUBECTL="$FAKE" \
    STAGING_LEASE_WAIT_SECONDS=2 \
    STAGING_LEASE_POLL_SECONDS=1 \
    STAGING_LEASE_TTL_SECONDS="${TTL_OVERRIDE:-2700}" \
    STAGING_LEASE_RENEW_SECONDS="${RENEW_OVERRIDE:-1}" \
    STAGING_LEASE_STATE_DIR="$RENEW_WORK" \
    "$LEASE" "$@"
}

renew_stamp() { sed -n 's/.*"renewTime":"\([^"]*\)".*/\1/p' "$STATE"; }

rm -f "$STATE"
run_lease_renewing acquire cd-r1 >/dev/null 2>&1 || true
run_lease_renewing stop-renewing cd-r1 >/dev/null 2>&1 || true
seed_lease cd-r1 "$(date -u -d '10 minutes ago' +%Y-%m-%dT%H:%M:%S.000000Z)" 2700
before="$(renew_stamp)"
if run_lease_renewing renew cd-r1 >/dev/null 2>&1; then
  after="$(renew_stamp)"
  if [ "$after" != "$before" ] && [ "$(holder_now)" = "cd-r1" ]; then
    pass "a renewal moves the holder's own claim forward"
  else
    fail "a renewal moves the holder's own claim forward (before=[$before] after=[$after] holder=[$(holder_now)])"
  fi
else
  fail "a renewal moves the holder's own claim forward"
fi
assert_eq "the renewal it sends parses and carries the version it read" "7" \
  "$(jq -r '.metadata.resourceVersion // empty' <<<"$(last_replace)")"

seed_lease someone-else "$(date -u +%Y-%m-%dT%H:%M:%S.000000Z)" 2700
if run_lease_renewing renew cd-r1 >/dev/null 2>&1; then
  fail "renewing a claim somebody else now holds is refused"
else
  pass "renewing a claim somebody else now holds is refused"
fi
assert_eq "the new holder is left in place" "someone-else" "$(holder_now)"

rm -f "$STATE"
if run_lease_renewing renew cd-r1 >/dev/null 2>&1; then
  fail "renewing a claim that is gone is refused"
else
  pass "renewing a claim that is gone is refused"
fi

renewer_pid() { cat "$RENEW_WORK/staging-lease-guard.pid" 2>/dev/null || true; }
stamp_epoch() { date -u -d "$1" +%s 2>/dev/null || echo 0; }

# Polls a condition every second for up to 60 seconds.
wait_until() {
  local remaining=60
  while [ "$remaining" -gt 0 ]; do
    if "$@"; then return 0; fi
    remaining=$((remaining - 1))
    sleep 1
  done
  return 1
}

rm -f "$STATE"
if TTL_OVERRIDE=8 RENEW_OVERRIDE=1 run_lease_renewing acquire cd-long >/dev/null 2>&1; then
  acquired_at="$(stamp_epoch "$(renew_stamp)")"
  carried_past_its_duration() {
    local now
    now="$(renew_stamp)"
    [ -n "$now" ] || return 1
    [ "$(($(stamp_epoch "$now") - acquired_at))" -gt 8 ]
  }
  if ! wait_until carried_past_its_duration; then
    fail "a claim outliving its own duration is still held (the renewer never carried it past its duration: $(cat "$RENEW_WORK/staging-lease-guard.lost" 2>/dev/null))"
  elif TTL_OVERRIDE=8 RENEW_OVERRIDE=1 run_lease_renewing acquire cd-thief >/dev/null 2>&1; then
    fail "a claim outliving its own duration is still held (cd-thief took it)"
  else
    assert_eq "a claim outliving its own duration is still held" "cd-long" "$(holder_now)"
  fi
else
  fail "a claim outliving its own duration is still held (acquire failed)"
fi

if [ -f "$RENEW_WORK/staging-lease-guard.lost" ]; then
  fail "a run longer than the renewal interval keeps the claim (the renewer lost it: $(cat "$RENEW_WORK/staging-lease-guard.lost"))"
else
  pass "a run longer than the renewal interval keeps the claim"
fi

renewer_before_release="$(renewer_pid)"
if TTL_OVERRIDE=8 RENEW_OVERRIDE=1 run_lease_renewing release cd-long >/dev/null 2>&1; then
  renewer_is_gone() {
    [ -z "$renewer_before_release" ] || ! kill -0 "$renewer_before_release" 2>/dev/null
  }
  if ! wait_until renewer_is_gone; then
    fail "releasing stops the renewing as well (the renewer is still running)"
  elif [ -f "$STATE" ]; then
    fail "releasing stops the renewing as well (the claim came back)"
  else
    pass "releasing stops the renewing as well"
  fi
else
  fail "releasing stops the renewing as well (release failed)"
fi

rm -f "$STATE"
TTL_OVERRIDE=8 RENEW_OVERRIDE=1 run_lease_renewing acquire cd-lost >/dev/null 2>&1 || true
seed_lease a-thief "$(date -u +%Y-%m-%dT%H:%M:%S.000000Z)" 2700
loss_recorded() { [ -f "$RENEW_WORK/staging-lease-guard.lost" ]; }
wait_until loss_recorded || true
if out="$(TTL_OVERRIDE=8 RENEW_OVERRIDE=1 run_lease_renewing release cd-lost 2>&1)"; then
  fail "a claim lost mid-run fails the release rather than passing quietly (got=[$out])"
elif grep -qi 'lost' <<<"$out"; then
  pass "a claim lost mid-run fails the release rather than passing quietly"
else
  fail "a claim lost mid-run fails the release rather than passing quietly (got=[$out])"
fi

quiet_calls() { grep -c -E "^$1 .*$2( |\$)" "$FAKE_QUIET_CALLS" || true; }

rm -f "$STATE" "$FAKE_SILENCES"
: >"$FAKE_QUIET_CALLS"
run_lease acquire cd-q1 >/dev/null 2>&1 || true
assert_eq "taking the claim opens a quiet period for its holder" "1" "$(quiet_calls POST cd-q1)"

run_lease acquire cd-q1 >/dev/null 2>&1 || true
assert_eq "re-taking the claim keeps one quiet period" "1" \
  "$(jq '[.[] | select(.status.state == "active")] | length' "$FAKE_SILENCES")"

run_lease release cd-q1 >/dev/null 2>&1 || true
assert_eq "releasing the claim ends its quiet period" "1" "$(quiet_calls DELETE cd-q1)"

rm -f "$STATE" "$FAKE_SILENCES"
: >"$FAKE_QUIET_CALLS"
TTL_OVERRIDE=8 RENEW_OVERRIDE=1 run_lease_renewing acquire cd-q2 >/dev/null 2>&1 || true
extended_by_renewer() { [ "$(quiet_calls POST cd-q2)" -ge 2 ]; }
if wait_until extended_by_renewer; then
  pass "each renewal extends the quiet period"
else
  fail "each renewal extends the quiet period (posts=$(quiet_calls POST cd-q2))"
fi

seed_lease a-thief "$(date -u +%Y-%m-%dT%H:%M:%S.000000Z)" 2700
wait_until loss_recorded || true
TTL_OVERRIDE=8 RENEW_OVERRIDE=1 run_lease_renewing release cd-q2 >/dev/null 2>&1 || true
assert_eq "a lost claim still ends its holder's quiet period" "1" "$(quiet_calls DELETE cd-q2)"

rm -f "$STATE" "$FAKE_SILENCES"
: >"$FAKE_QUIET_CALLS"
summary="$WORK/step-summary.md"
: >"$summary"
if out="$(GITHUB_STEP_SUMMARY="$summary" FAKE_QUIET_REFUSE=1 run_lease acquire cd-q3 2>&1)"; then
  assert_eq "a refused quiet period still takes the claim" "cd-q3" "$(holder_now)"
else
  fail "a refused quiet period still takes the claim (got=[$out])"
fi
if grep -q '::warning::' <<<"$out" && grep -qF 'Invalid username or password' <<<"$out"; then
  pass "the refusal is a warning carrying Grafana's reason"
else
  fail "the refusal is a warning carrying Grafana's reason (got=[$out])"
fi
if grep -qi 'quiet period' "$summary"; then
  pass "and the run's summary says the alerts were not held back"
else
  fail "and the run's summary says the alerts were not held back (summary=[$(cat "$summary")])"
fi
if GITHUB_STEP_SUMMARY="$summary" FAKE_QUIET_REFUSE=1 run_lease release cd-q3 >/dev/null 2>&1; then
  pass "a refused close does not fail the release"
else
  fail "a refused close does not fail the release"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf 'Failures:\n' >&2
  for f in "${FAILURES[@]}"; do printf '  - %s\n' "$f" >&2; done
  exit 1
fi
