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

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
