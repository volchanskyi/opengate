#!/usr/bin/env bash
# Contract tests for .github/workflows/load-test.yml.
#
# Four faults kept this nightly red, and none of them was visible in any test:
# a delete written in a form the database never expands, a run identifier nobody
# ever set so every night reused the same three addresses, a step that read a
# file the following step writes, and generator pods asking for more of the node
# than it had left. Each is asserted here, on the workflow text, because that is
# where each one lived.
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

# --- The spent credential is actually removed ---------------------------------
#
# psql expands a -v variable in a file and on standard input, and not inside -c.
# Written with -c the server receives the colon and the quotes literally and
# answers with a syntax error, so the token outlives the run that spent it.
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

# --- Every night uses its own identities ---------------------------------------
#
# The generator builds each address from a run identifier. Unset, it falls back
# to a fixed string, every night registers the same three addresses, and the
# second night is refused as a duplicate.
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

# --- A file is written before it is read ---------------------------------------
summary_line="$(grep -n 'Build canonical load-test summary' "$WORKFLOW" | head -1 | cut -d: -f1)"
completeness_line="$(grep -n 'Record run completeness' "$WORKFLOW" | head -1 | cut -d: -f1)"
if [ -n "$summary_line" ] && [ -n "$completeness_line" ] \
  && [ "$summary_line" -lt "$completeness_line" ]; then
  pass "the canonical summary is built before the step that reads it"
else
  fail "completeness (line $completeness_line) must follow the summary build (line $summary_line)"
fi

# --- The generators fit beside everything else on the node ---------------------
#
# One node, 1830m of processor, and most of it already claimed. Two generator
# pods that ask for more than is left do not fail loudly — they sit Pending
# until a wait times out, and the night reads as a broken cluster.
#
# What is left shrank when the staging server took production's own reservation:
# 1680m is now spoken for, so 150m is the whole of what a generator may reserve.
# Bursting past it is fine and expected — the machine is three-quarters idle —
# but reservation is what admission is decided on.
generator_cpu="$(grep -oE '"requests":\{"cpu":"[0-9]+m"' "$WORKFLOW" | grep -oE '[0-9]+' | awk '{ total += $1 } END { print total + 0 }')"
if [ -n "$generator_cpu" ] && [ "$generator_cpu" -gt 0 ] && [ "$generator_cpu" -le 150 ]; then
  pass "the two generator pods together request a share the node can spare"
else
  fail "generator requests must total 150m or less (got ${generator_cpu}m)"
fi

# --- The token is minted against an account no cleanup removes -----------------
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

# --- The run provides the account it spends ------------------------------------

# The staging deploy's database reset takes this account away, and whether it is
# there when a run starts would otherwise depend on what a deploy did hours
# earlier. The run seeds it before it spends it, from the file the chart's
# post-upgrade hook reads, so there is one copy of the statements rather than a
# second one here to drift from it.
if grep -qF 'deploy/scripts/loadtest-account-sql.sh' "$WORKFLOW"; then
  pass "the run seeds its administrator from the same emitter the chart hook reads"
else
  fail "the run depends on somebody else having seeded its administrator, or restates the SQL"
fi

# A command line is readable by every process sharing the Postgres pod and is
# recorded verbatim in the API server's audit entry for the exec subresource, so
# the password reaches psql over standard input.
if grep -qE -- '(--set=|-v[[:space:]]+)account_password' "$WORKFLOW"; then
  fail "the account password never rides the psql command line"
else
  pass "the account password never rides the psql command line"
fi

# --- The fleet's own account of itself survives a failed scenario --------------
#
# The fleet is held in the cluster, so its log and its verdict are read back by
# a later step. That step ran only while everything before it had passed
# — which is every case except the one where the log is the answer. Three weeks
# of nights failed with "no online machine to open a session against" and the
# fleet's own reason for not being there was collected by nothing and printed
# nowhere.
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

# --- The fleet dials a name the certificate carries ----------------------------
#
# An agent takes its TLS name from the host half of the address it is given, and
# a pod IP is in no certificate — it changes on every restart, so no SAN list can
# name one. Dialling the pod IP refused every agent at the handshake
# ("certificate is valid for 127.0.0.1, not 10.244.0.99") for over a week, and
# the relay scenario failed behind it for want of a machine to open a session
# against. The name the chart signs the server for is its Service name, so that
# is the name the fleet dials; the packets still reach the pod, because the alias
# below points the name at it.
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

# The Service carries the HTTP port only, so the name is pointed at the server
# pod itself — the same arrangement the staging browser suite's machines use.
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

# --- A run that measured nothing does not enter the trend ----------------------
#
# The completeness verdict is written by the load-test job and was read by
# nothing: the publish job ran on every path and pushed whatever rows existed, so
# nine nights of rps 0 / error_rate 1 went into the fourteen-day window and took
# its median from 113 to 57. A depressed median is a gate that fires later, which
# is the cost the invalid classification exists to avoid.
publish_block="$(awk '/^  publish:/ { found = 1; next } found && /^  [a-z]/ { exit } found { print }' "$WORKFLOW")"
if grep -q 'loadtest-completeness.json' <<<"$publish_block"; then
  pass "the publish job reads the verdict the run wrote about itself"
else
  fail "the publish job pushes rows without reading whether the run measured anything"
fi

push_line="$(grep -n 'loadtest-vm-push.sh' <<<"$publish_block" | head -1 | cut -d: -f1)"
verdict_line="$(grep -n 'loadtest-completeness.json' <<<"$publish_block" | head -1 | cut -d: -f1)"
if [ -n "$push_line" ] && [ -n "$verdict_line" ] && [ "$verdict_line" -lt "$push_line" ]; then
  pass "the verdict is read before anything is pushed"
else
  fail "the verdict (line $verdict_line) must be read before the push (line $push_line)"
fi

# --- The reset the next run stands on empties what a run creates ---------------
#
# A run's fixture asks the server for a customer by name, and a customer's name
# is unique inside its tenant. The deploy's reset emptied the accounts and the
# sites and left the customers, so the night after a deploy was refused the first
# customer it asked for and never built a fleet at all.
CD_WORKFLOW="$REPO_ROOT/.github/workflows/cd.yml"
reset_tables="$(awk '/TRUNCATE TABLE/ {found = 1; next} found {print} found && /RESTART IDENTITY/ {exit}' "$CD_WORKFLOW")"
for table in organizations sites devices users; do
  if grep -qw "$table" <<<"$reset_tables"; then
    pass "the staging reset empties $table"
  else
    fail "the staging reset leaves $table behind for the next run to collide with"
  fi
done

# --- the harness reads its target's health off the right listener -------------
#
# The exposition moved to the server's second listener, which the Service
# publishes on its own port. A harness pointed at the API port would find the
# SPA fallback where the exposition should be, parse nothing out of it, and
# record a target it never measured. The port is read back against the chart's.

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

# --- the night's verdict reads what the harness recorded about its target -----
#
# The harness writes what the target was holding either side of the run into its
# bundle; the completeness gate folds that in. Two steps, two paths, and a run
# has one verdict however many places compute it — so the path the bundle is
# collected to and the path the gate reads have to be the same one.

BUNDLE_PATH='loadtest-bundle/quic-agents.json'
if grep -qF "$BUNDLE_PATH" "$WORKFLOW" \
  && grep -qF "LOADTEST_BUNDLE=$BUNDLE_PATH" "$WORKFLOW"; then
  pass "the completeness gate reads the bundle the run collected"
else
  fail "the completeness gate does not read the collected bundle, so the target's own verdict is dropped"
fi

# --- A path the in-pod harness is given resolves inside the pod ---------------
#
# The QUIC harness runs inside the staging pod, and the pod holds no checkout —
# it is an alpine image the run copies one binary into. A repository-relative
# path therefore resolves on this runner and nowhere else, and the harness dies
# at its first line reporting a file that is not there. What the run shows is
# "the QUIC harness exited before it offered a fleet", which is the same
# sentence a broken image, a wrong architecture and an unschedulable pod all
# produce.
#
# It is the shape ci-cd-determinism.md names: a contract stated in one file and
# satisfied in another is checked in neither unless something is made to read
# both. Here the two are one file, and still nothing read them together.
#
# So every value the in-pod harness is handed that looks like a path is either
# absolute — a place inside the pod — or something the run copied there.

# The call as one line, so a flag written across a continuation is still read.
harness_call="$(tr '\n' ' ' <"$WORKFLOW" \
  | grep -oE 'loadtest-quic-incluster\.sh start --.*-bundle=[^ ]+' || true)"

if [ -n "$harness_call" ]; then
  pass "the in-cluster harness call was found"
else
  fail "the in-cluster harness call was not found, so this sweep checked nothing"
fi

# Every -flag=value pair on that call, resolved through what the workflow sets,
# and then judged. Resolving first is the whole of it: "$LOADTEST_PROFILE" has
# no slash in it, so a sweep that asks what a value looks like before asking
# what it is skips every path the workflow passes by name — which is every one
# of them.
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

  # A URL names a service, not a file; a bare word or a host:port names neither.
  # Nor does a command substitution: it is evaluated where the step runs, so
  # what reaches the pod is its output rather than the path inside it.
  case "$value" in
    http://* | https://*) continue ;;
    *"\$("*) continue ;;
    */*) ;;
    *) continue ;;
  esac

  pathlike=$((pathlike + 1))
  # Absolute is a place inside the pod. Anything else is this runner's tree,
  # which the pod has no copy of.
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

# And the profile it names is actually put there, or the flag points at a path
# inside a pod that holds no such file.
if grep -qE 'kubectl .*cp .*LOADTEST_PROFILE' "$WORKFLOW"; then
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
