#!/usr/bin/env bash
# Tests for scripts/alert-quiet-period.sh — the shared-infrastructure alerts are
# held back while a test holds the staging claim, and only while it does.
#
# The kubectl stand-in answers the way Grafana's own Alertmanager does, over one
# JSON file, so a silence opened twice or closed for the wrong holder shows up
# as the wrong silences afterwards rather than as a fake that answered the same
# way regardless.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
QUIET="$REPO_ROOT/scripts/alert-quiet-period.sh"
RULES_FILE="$REPO_ROOT/deploy/grafana/provisioning/alerting/alert-rules.yml"
[ -x "$QUIET" ] || {
  echo "FAIL: $QUIET not executable" >&2
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
# Stand-in for `kubectl exec` into Grafana, answering the silences API from a
# state file the way Grafana's Alertmanager does.
set -uo pipefail
STATE="$FAKE_STATE"
[ -f "$STATE" ] || echo '[]' >"$STATE"

# The credential plugin the real cluster is reached through writes a warning to
# stderr on every call.
if [ -n "${FAKE_NOISY_STDERR:-}" ]; then
  echo "Warning: To increase security of your API key, append an extra line with 'OCI_API_KEY'." >&2
fi

# Everything after `--` is the command run inside the pod: sh -c <script> <$0>
# <method> <path>. The call is recorded whole, so a test can read what reached
# the pod.
args=("$@")
for i in "${!args[@]}"; do
  if [ "${args[$i]}" = "--" ]; then
    remote=("${args[@]:$((i + 1))}")
    break
  fi
done
printf '%s\n' "${remote[*]}" >>"$FAKE_CALLS"
method="${remote[4]:-}"
path="${remote[5]:-}"

# A refused request comes back as curl's own failure, with Grafana's body on
# standard output and kubectl's account of the exit on standard error.
if [ -n "${FAKE_REFUSE:-}" ]; then
  echo '{"message":"Invalid username or password"}'
  echo "command terminated with exit code 22" >&2
  exit 22
fi

case "$method $path" in
  "GET /silences")
    cat "$STATE"
    ;;
  "POST /silences")
    body="$(cat)"
    if [ -n "${FAKE_NO_ID:-}" ]; then
      echo '{"message":"ok"}'
      exit 0
    fi
    id="$(jq -r '.id // empty' <<<"$body")"
    if [ -n "$id" ] && jq -e --arg id "$id" 'any(.[]; .id == $id)' "$STATE" >/dev/null; then
      jq --arg id "$id" --argjson s "$body" \
        'map(if .id == $id then . + ($s | {endsAt, comment, matchers}) else . end)' \
        "$STATE" >"$STATE.new"
    else
      id="s-$(($(jq 'length' "$STATE") + 1))"
      jq --arg id "$id" --argjson s "$body" \
        '. + [$s + {id: $id, status: {state: "active"}}]' "$STATE" >"$STATE.new"
    fi
    mv "$STATE.new" "$STATE"
    printf '{"silenceID":"%s"}\n' "$id"
    ;;
  "DELETE /silence/"*)
    id="${path#/silence/}"
    jq --arg id "$id" 'map(if .id == $id then .status.state = "expired" else . end)' \
      "$STATE" >"$STATE.new"
    mv "$STATE.new" "$STATE"
    echo '{"message":"silence deleted"}'
    ;;
  *)
    echo "fake kubectl: unhandled call: ${remote[*]}" >&2
    exit 1
    ;;
esac
FAKE_KUBECTL
chmod +x "$FAKE"

STATE="$WORK/silences.json"
export FAKE_STATE="$STATE"
export FAKE_CALLS="$WORK/calls.log"

run_quiet() {
  ALERT_QUIET_KUBECTL="$FAKE" \
    ALERT_QUIET_SECONDS="${SECONDS_OVERRIDE:-2700}" \
    "$QUIET" "$@"
}

# The holder's silences that have not ended, one line each.
live_for() {
  jq -r --arg h "$1" \
    '.[] | select(.status.state != "expired" and (.comment | contains($h))) | .id' "$STATE"
}
live_count() { live_for "$1" | grep -c . || true; }
ends_of() { jq -r --arg h "$1" '[.[] | select(.status.state != "expired" and (.comment | contains($h)))][0].endsAt' "$STATE"; }
epoch() { date -u -d "$1" +%s; }

echo "alert-quiet-period:"

# Opening a quiet period holds back the shared rules, for as long as asked.
rm -f "$STATE" "$FAKE_CALLS"
if run_quiet open load-test-1-1 >/dev/null 2>&1; then
  assert_eq "opening creates one silence for the holder" "1" "$(live_count load-test-1-1)"
else
  fail "opening creates one silence for the holder"
fi
assert_eq "it silences the shared rules" "watches=shared" \
  "$(jq -r '.[0].matchers[] | "\(.name)=\(.value)"' "$STATE")"
assert_eq "it matches the label exactly rather than as a pattern" "false true" \
  "$(jq -r '.[0].matchers[0] | "\(.isRegex) \(.isEqual)"' "$STATE")"
assert_eq "it says who opened it" "staging-claim" "$(jq -r '.[0].createdBy' "$STATE")"
ends="$(epoch "$(ends_of load-test-1-1)")"
wanted=$(($(date -u +%s) + 2700))
if [ "$((wanted - ends))" -ge -5 ] && [ "$((wanted - ends))" -le 60 ]; then
  pass "it lasts the claim's duration from now"
else
  fail "it lasts the claim's duration from now (ends=$ends wanted~$wanted)"
fi

# The password is expanded by the pod's own shell and never crosses to the
# runner: what reaches the pod names the variable, not its value.
if grep -qF "\$GF_SECURITY_ADMIN_PASSWORD" "$FAKE_CALLS"; then
  pass "the administrator password stays inside the pod"
else
  fail "the call must name \$GF_SECURITY_ADMIN_PASSWORD for the pod to expand (got=[$(cat "$FAKE_CALLS")])"
fi

# A step retried, or a claim taken again by the run already holding it, does
# not stack a second silence on the first.
first_id="$(live_for load-test-1-1)"
if run_quiet open load-test-1-1 >/dev/null 2>&1; then
  assert_eq "a second open reuses the holder's silence" "$first_id" "$(live_for load-test-1-1)"
else
  fail "a second open reuses the holder's silence"
fi

# Each renewal of the claim carries the quiet period forward with it.
before="$(epoch "$(ends_of load-test-1-1)")"
if SECONDS_OVERRIDE=5400 run_quiet extend load-test-1-1 >/dev/null 2>&1; then
  after="$(epoch "$(ends_of load-test-1-1)")"
  if [ "$after" -gt "$before" ] && [ "$(live_for load-test-1-1)" = "$first_id" ]; then
    pass "extending moves the holder's silence forward"
  else
    fail "extending moves the holder's silence forward (before=$before after=$after)"
  fi
else
  fail "extending moves the holder's silence forward"
fi

# Grafana's own state is not durable: a pod restart forgets every silence. An
# extension that finds none puts one back rather than leaving the rest of the
# run loud.
jq 'map(.status.state = "expired")' "$STATE" >"$STATE.new" && mv "$STATE.new" "$STATE"
if run_quiet extend load-test-1-1 >/dev/null 2>&1; then
  assert_eq "extending with none left opens a new one" "1" "$(live_count load-test-1-1)"
else
  fail "extending with none left opens a new one"
fi

# Closing ends this holder's quiet period and nobody else's.
run_quiet open network-drill-2-1 >/dev/null 2>&1 || true
if run_quiet close load-test-1-1 >/dev/null 2>&1; then
  assert_eq "closing ends the holder's silence" "0" "$(live_count load-test-1-1)"
  assert_eq "and leaves another holder's in place" "1" "$(live_count network-drill-2-1)"
else
  fail "closing ends the holder's silence"
fi

# Closing when there is nothing to close is not a failure: the release runs on
# every path, including one where the open was refused.
if run_quiet close load-test-1-1 >/dev/null 2>&1; then
  pass "closing nothing succeeds"
else
  fail "closing nothing succeeds"
fi

# A noisy credential plugin is not part of the answer.
rm -f "$STATE"
if out="$(FAKE_NOISY_STDERR=1 run_quiet open cd-3-1 2>&1)" \
  && [ "$(live_count cd-3-1)" = "1" ]; then
  pass "a warning on stderr does not become part of Grafana's answer"
else
  fail "a warning on stderr does not become part of Grafana's answer (got=[$out])"
fi

# A refusal is reported with Grafana's own reason, and the step says it failed.
rm -f "$STATE"
if out="$(FAKE_REFUSE=1 run_quiet open cd-4-1 2>&1)"; then
  fail "a refused call fails"
elif grep -qF 'Invalid username or password' <<<"$out"; then
  pass "a refused call fails with Grafana's reason"
else
  fail "a refused call fails with Grafana's reason (got=[$out])"
fi

# An answer shaped like success that names no silence opened nothing.
rm -f "$STATE"
if out="$(FAKE_NO_ID=1 run_quiet open cd-5-1 2>&1)"; then
  fail "an answer naming no silence is refused (got=[$out])"
elif grep -qi 'silence' <<<"$out"; then
  pass "an answer naming no silence is refused"
else
  fail "an answer naming no silence is refused (got=[$out])"
fi

# How long a quiet period lasts is the caller's to say, every time.
if ALERT_QUIET_KUBECTL="$FAKE" "$QUIET" open cd-6-1 >/dev/null 2>&1; then
  fail "a quiet period with no duration is refused"
else
  pass "a quiet period with no duration is refused"
fi

for bad in "open" "hold cd-7-1"; do
  # shellcheck disable=SC2086 # each case is a word list on purpose
  if run_quiet $bad >/dev/null 2>&1; then
    fail "a malformed call is refused: $bad"
  else
    pass "a malformed call is refused: $bad"
  fi
done

# --- the silence and the rules agree ------------------------------------------
#
# The matcher is a label value written in two files. A rule renamed on one side
# is a silence that matches nothing, which reads exactly like a quiet night.
rm -f "$STATE"
run_quiet open cd-8-1 >/dev/null 2>&1 || true
label="$(jq -r '.[0].matchers[0].name' "$STATE")"
value="$(jq -r '.[0].matchers[0].value' "$STATE")"
silenced="$(
  python3 - "$RULES_FILE" "$label" "$value" <<'PY'
import sys, yaml

with open(sys.argv[1], encoding="utf-8") as fh:
    doc = yaml.safe_load(fh)
rules = [r for g in doc.get("groups", []) for r in g.get("rules", [])]
print(sum(1 for r in rules if r.get("labels", {}).get(sys.argv[2]) == sys.argv[3]))
PY
)"
if [ "${silenced:-0}" -gt 0 ]; then
  pass "the silence holds back $silenced rules the rules file declares shared"
else
  fail "the silence matches $label=$value, which no rule in alert-rules.yml carries"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf 'Failures:\n' >&2
  for f in "${FAILURES[@]}"; do printf '  - %s\n' "$f" >&2; done
  exit 1
fi
