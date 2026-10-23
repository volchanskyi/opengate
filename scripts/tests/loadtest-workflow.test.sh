#!/usr/bin/env bash
# Contract tests for .github/workflows/load-test.yml, asserted on the workflow text.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
WORKFLOW="$REPO_ROOT/.github/workflows/load-test.yml"

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

echo "loadtest-workflow:"

[ -f "$WORKFLOW" ] || {
  echo "FAIL: $WORKFLOW not found" >&2
  exit 1
}

# psql expands a -v variable in a file and on standard input, and not inside -c.
if grep -nE -- "-c[[:space:]]+\"[^\"]*:'" "$WORKFLOW" >/dev/null; then
  fail "a psql -c string names a variable psql will not expand there"
else
  pass "no psql -c string relies on a variable psql does not expand"
fi

if grep -q 'DELETE FROM enrollment_tokens' "$WORKFLOW"; then
  pass "the run removes the token it spent"
else
  fail "the run must remove the token it spent"
fi

# An unset run identifier falls back to a fixed string, so every night would register the same
# addresses.
if grep -q 'LOADTEST_RUN_ID' "$WORKFLOW"; then
  pass "the workflow gives the run its own identifier"
else
  fail "the workflow must set LOADTEST_RUN_ID so each night's identities are its own"
fi

if grep -qE 'LOADTEST_RUN_ID:[[:space:]]*\$\{\{[[:space:]]*github\.run_id' "$WORKFLOW" \
  || grep -qE 'LOADTEST_RUN_ID=\$\{?GITHUB_RUN_ID' "$WORKFLOW"; then
  pass "the identifier is the run's own, not a fixed string"
else
  fail "LOADTEST_RUN_ID must come from the workflow run id"
fi

# The first line a pattern appears on, or nothing; a grep pipeline would answer with grep's status.
first_line_of() {
  local matches
  matches="$(grep -nE -- "$1" "$2" || true)"
  [ -n "$matches" ] || return 0
  cut -d: -f1 <<<"${matches%%$'\n'*}"
}

first_line_in() {
  local matches
  matches="$(grep -nE -- "$1" <<<"$2" || true)"
  [ -n "$matches" ] || return 0
  cut -d: -f1 <<<"${matches%%$'\n'*}"
}

summary_line="$(first_line_of 'Build canonical load-test summary' "$WORKFLOW")"
completeness_line="$(first_line_of 'Record run completeness' "$WORKFLOW")"
if [ -n "$summary_line" ] && [ -n "$completeness_line" ] \
  && [ "$summary_line" -lt "$completeness_line" ]; then
  pass "the canonical summary is built before the step that reads it"
else
  fail "completeness (line $completeness_line) must follow the summary build (line $summary_line)"
fi

# Reservation decides admission, so the generators' CPU requests together stay within 150m.
generator_cpu="$(grep -oE '"requests":\{"cpu":"[0-9]+m"' "$WORKFLOW" | grep -oE '[0-9]+' | awk '{ total += $1 } END { print total + 0 }')"
if [ -n "$generator_cpu" ] && [ "$generator_cpu" -gt 0 ] && [ "$generator_cpu" -le 150 ]; then
  pass "the two generator pods together request a share the node can spare"
else
  fail "generator requests must total 150m or less (got ${generator_cpu}m)"
fi

if grep -q 'LOADTEST_SERVICE_ACCOUNT' "$WORKFLOW"; then
  pass "the run mints against the seeded service account"
else
  fail "the mint step must name the seeded service account, not the oldest admin it finds"
fi

if grep -qE 'WHERE u\.is_admin[[:space:]]*$' "$WORKFLOW"; then
  fail "the mint step must not pick whichever administrator happens to be oldest"
else
  pass "the mint step does not pick an arbitrary administrator"
fi

# The staging reset removes the account, so the run seeds it from the file the chart hook reads.
if grep -qF 'deploy/scripts/loadtest-account-sql.sh' "$WORKFLOW"; then
  pass "the run seeds its administrator from the same emitter the chart hook reads"
else
  fail "the run depends on somebody else having seeded its administrator, or restates the SQL"
fi

# A command line is visible to every process in the Postgres pod and in the exec audit entry,
# so the password reaches psql over standard input.
if grep -qE -- '(--set=|-v[[:space:]]+)account_password' "$WORKFLOW"; then
  fail "the account password never rides the psql command line"
else
  pass "the account password never rides the psql command line"
fi

# The fleet's log and verdict are read back by a later step, which must also run after a failure.
fleet_wait_block="$(awk '
  /^[[:space:]]*- name: Wait for the QUIC fleet to finish/ { found = 1; next }
  found && /^[[:space:]]*- name:/ { exit }
  found { print }
' "$WORKFLOW")"
if grep -qE 'if:[[:space:]]*(\$\{\{[[:space:]]*)?always\(\)' <<<"$fleet_wait_block"; then
  pass "the fleet's verdict is collected on the failing path too"
else
  fail "the step that reads the fleet's log must run whatever the scenarios did"
fi

# An agent takes its TLS name from the address host and a pod IP is in no certificate, so the
# fleet dials the Service name the chart signs.
quic_start_block="$(awk '
  /^[[:space:]]*- name: Start the QUIC fleet and hold it connected/ { found = 1; next }
  found && /^[[:space:]]*- name:/ { exit }
  found { print }
' "$WORKFLOW")"
if grep -qE -- '-addr="\$\{?SERVER_POD_IP\}?:9090"' <<<"$quic_start_block"; then
  fail "the fleet dials the server's pod IP, which no certificate can name"
else
  pass "the fleet does not dial an address no certificate can name"
fi

if grep -qE -- '-addr="\$\{RELEASE\}-server:9090"' <<<"$quic_start_block"; then
  pass "the fleet dials the server by the name its certificate carries"
else
  fail "the fleet must dial the server's in-cluster name, the one on the certificate"
fi

# The Service carries the HTTP port only, so the name is pointed at the server pod itself.
quic_stage_block="$(awk '
  /^[[:space:]]*- name: Stage QUIC harness in cluster/ { found = 1; next }
  found && /^[[:space:]]*- name:/ { exit }
  found { print }
' "$WORKFLOW")"
if grep -q 'hostAliases' <<<"$quic_stage_block" \
  && grep -q 'SERVER_POD_IP' <<<"$quic_stage_block"; then
  pass "the name the fleet dials resolves to the server pod the listener is in"
else
  fail "without an alias the name resolves to a Service with no QUIC port, and the packets reach nothing"
fi

# The publish job reads the completeness verdict, so an empty run stays out of the trend.
publish_block="$(awk '/^  publish:/ { found = 1; next } found && /^  [a-z]/ { exit } found { print }' "$WORKFLOW")"
if grep -q 'loadtest-completeness.json' <<<"$publish_block"; then
  pass "the publish job reads the verdict the run wrote about itself"
else
  fail "the publish job pushes rows without reading whether the run measured anything"
fi

push_line="$(first_line_in 'loadtest-vm-push\.sh' "$publish_block")"
verdict_line="$(first_line_in 'loadtest-completeness\.json' "$publish_block")"
if [ -n "$push_line" ] && [ -n "$verdict_line" ] && [ "$verdict_line" -lt "$push_line" ]; then
  pass "the verdict is read before anything is pushed"
else
  fail "the verdict (line $verdict_line) must be read before the push (line $push_line)"
fi

# The staging reset empties what a run's fixture creates, so the next run's names do not collide.
CD_WORKFLOW="$REPO_ROOT/.github/workflows/cd.yml"
reset_tables="$(awk '/TRUNCATE TABLE/ {found = 1; next} found {print} found && /RESTART IDENTITY/ {exit}' "$CD_WORKFLOW")"
for table in organizations sites devices users; do
  if grep -qw "$table" <<<"$reset_tables"; then
    pass "the staging reset empties $table"
  else
    fail "the staging reset leaves $table behind for the next run to collide with"
  fi
done

# The exposition is on the server's second listener, which the Service publishes on its own port.

VALUES="$REPO_ROOT/deploy/helm/opengate/values.yaml"
METRICS_PORT="$(awk '/^[[:space:]]+metricsPort:/ { print $2; exit }' "$VALUES")"

if [ -n "$METRICS_PORT" ] && grep -qF "LOADTEST_METRICS_URL=http://\${RELEASE}-server:${METRICS_PORT}" "$WORKFLOW"; then
  pass "the run addresses the exposition on the chart's internal port ($METRICS_PORT)"
else
  fail "the run's metrics URL does not name the chart's internal port ($METRICS_PORT)"
fi

# shellcheck disable=SC2016 # the pattern is the workflow's literal text, not an expansion
if grep -qF -- '-metrics-url="$LOADTEST_METRICS_URL"' "$WORKFLOW"; then
  pass "the harness is given that URL rather than the API's"
else
  fail "the harness must read its target off LOADTEST_METRICS_URL, not the API base URL"
fi

# The path the bundle is collected to is the path the completeness gate reads.

BUNDLE_PATH='loadtest-bundle/quic-agents.json'
if grep -qF "$BUNDLE_PATH" "$WORKFLOW" \
  && grep -qF "LOADTEST_BUNDLE=$BUNDLE_PATH" "$WORKFLOW"; then
  pass "the completeness gate reads the bundle the run collected"
else
  fail "the completeness gate does not read the collected bundle, so the target's own verdict is dropped"
fi

# The pod holds no checkout, so every path-like value given to the in-pod harness is absolute.

# The call as one line, so a flag written across a continuation is still read.
harness_call="$(tr '\n' ' ' <"$WORKFLOW" \
  | grep -oE 'loadtest-quic-incluster\.sh start --.*-bundle=[^ ]+' || true)"

if [ -n "$harness_call" ]; then
  pass "the in-cluster harness call was found"
else
  fail "the in-cluster harness call was not found, so this sweep checked nothing"
fi

# Each -flag=value pair is resolved through what the workflow sets before it is judged.
resolve() {
  local value="$1" name
  case "$value" in
    \$*) ;;
    *)
      printf '%s' "$value"
      return
      ;;
  esac
  name="${value#\$}"
  name="${name#\{}"
  name="${name%\}}"
  # Two spellings set a name in a workflow: the YAML env block writes
  # "NAME: value" and a step writing to $GITHUB_ENV writes "NAME=value".
  local resolved
  resolved="$(sed -n "s/^ *${name}: *//p" "$WORKFLOW" | head -1)"
  [ -n "$resolved" ] || resolved="$(grep -oE "${name}=[^ \"]+" "$WORKFLOW" | head -1 | sed "s/^${name}=//")"
  printf '%s' "$resolved"
}

pathlike=0
offending=""
while read -r pair; do
  [ -n "$pair" ] || continue
  value="${pair#*=}"
  value="${value//\"/}"
  value="$(resolve "$value")"

  # A URL or a command substitution names no file; a substitution runs where the step runs.
  case "$value" in
    http://* | https://*) continue ;;
    *"\$("*) continue ;;
    */*) ;;
    *) continue ;;
  esac

  pathlike=$((pathlike + 1))
  # An absolute path is a place inside the pod; anything else is this runner's tree.
  case "$value" in
    /*) ;;
    *) offending="$offending ${pair%%=*}=$value" ;;
  esac
done < <(grep -oE '[-][a-z-]+=[^ ]+' <<<"$harness_call")

if [ "$pathlike" -gt 0 ]; then
  pass "the harness is handed $pathlike path-shaped values to check"
else
  fail "no path-shaped value was found on the harness call, so this sweep checked nothing"
fi

if [ -z "$offending" ]; then
  pass "every path the in-pod harness is given is a place inside the pod"
else
  fail "the in-pod harness is given a path from this runner's tree:$offending"
fi

# The profile the flag names is copied into the pod, through either spelling of the call.
if grep -qE 'kubectl(_retry)? .*cp .*LOADTEST_PROFILE' "$WORKFLOW"; then
  pass "the profile is copied into the pod that reads it"
else
  fail "nothing copies the profile into the pod, so the harness reads a file that is not there"
fi

# A night the shared server made no room for is red, and its alert names the busy server: the
# publish job's verdict step and the alert step run here against a not-started verdict.
step_script() {
  python3 - "$WORKFLOW" "$1" "$2" <<'EOF'
import sys, yaml
workflow, job, name = sys.argv[1], sys.argv[2], sys.argv[3]
for step in yaml.safe_load(open(workflow))["jobs"][job]["steps"]:
    if step.get("name") == name:
        print(step["run"])
EOF
}
reason='the shared server stayed 89–93% busy for 30 minutes, past the 85% the run allows'
sandbox="$(mktemp -d)"
mkdir -p "$sandbox/artifacts/loadtest" "$sandbox/scripts"
jq -n --arg reason "$reason" '{result: "not_started", reason: $reason}' \
  >"$sandbox/artifacts/loadtest/loadtest-completeness.json"
: >"$sandbox/output"
verdict_status=0
(cd "$sandbox" && GITHUB_OUTPUT="$sandbox/output" \
  bash -e -o pipefail -c "$(step_script publish "Read the run's own verdict before publishing anything")") \
  >/dev/null 2>&1 || verdict_status=$?
if [ "$verdict_status" -ne 0 ] && grep -qxF -- "not_started=$reason" "$sandbox/output"; then
  pass "a night that never started keeps out of the trend and hands its reason on"
else
  fail "the verdict step must fail a not-started night and output not_started=<reason> (status=$verdict_status, output=[$(cat "$sandbox/output")])"
fi
if python3 -c 'import sys, yaml; o = yaml.safe_load(open(sys.argv[1]))["jobs"]["publish"]["outputs"]; sys.exit(0 if "not_started" in o else 1)' "$WORKFLOW"; then
  pass "the publish job declares the reason as an output"
else
  fail "the publish job must declare a not_started output"
fi

# shellcheck disable=SC2016 # $1 belongs to the stand-in script this line writes
printf '#!/usr/bin/env bash\nprintf "%%s" "$1" >"%s/sent"\n' "$sandbox" >"$sandbox/scripts/telegram-alert.sh"
chmod +x "$sandbox/scripts/telegram-alert.sh"
(cd "$sandbox" && JOB_STATUS=failure FINDING=0 ALERT_TEXT="" NOT_STARTED="$reason" RUN_URL=https://example.test/run \
  bash -e -o pipefail -c "$(step_script alert "Send Telegram alert")") >/dev/null 2>&1 || true
if grep -qF -- "$reason" "$sandbox/sent" 2>/dev/null; then
  pass "the alert for a night that never started quotes why"
else
  fail "the alert must quote the not-started reason (sent=[$(cat "$sandbox/sent" 2>/dev/null)])"
fi
alert_env="$(python3 -c 'import sys, yaml
for st in yaml.safe_load(open(sys.argv[1]))["jobs"]["alert"]["steps"]:
    if st.get("name") == "Send Telegram alert": print(st.get("env", {}).get("NOT_STARTED", ""))' "$WORKFLOW")"
# shellcheck disable=SC2016 # the workflow's expression text, compared unexpanded
if [ "$alert_env" = '${{ needs.load-test.outputs.not_started || needs.publish.outputs.not_started }}' ]; then
  pass "the alert reads why staging was refused, then the reason the publish job handed on"
else
  fail "the alert step must set NOT_STARTED from the load-test job's reason, then the publish job's (got=[$alert_env])"
fi
rm -rf "$sandbox"

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
