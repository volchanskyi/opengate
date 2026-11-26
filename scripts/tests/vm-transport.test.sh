#!/usr/bin/env bash
# Offline payload and transport tests for private in-cluster VictoriaMetrics access.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

PASS=0
FAIL=0
TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "$TMP_ROOT"' EXIT

pass() {
  PASS=$((PASS + 1))
  printf '  ok   %s\n' "$1"
}

fail() {
  FAIL=$((FAIL + 1))
  printf '  FAIL %s\n' "$1" >&2
}

bin_dir="$TMP_ROOT/bin"
mkdir -p "$bin_dir"
cat >"$bin_dir/kubectl" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >"$KUBECTL_ARGS"
cat >"$KUBECTL_STDIN"
exit "${KUBECTL_STATUS:-0}"
EOF
chmod +x "$bin_dir/kubectl"

run_vm_push() {
  local args_file="$1"
  local stdin_file="$2"
  shift 2
  env \
    PATH="$bin_dir:$PATH" \
    KUBECTL_ARGS="$args_file" \
    KUBECTL_STDIN="$stdin_file" \
    VM_NAMESPACE="observability" \
    VM_SERVICE="private-vm" \
    VM_RUN_STARTED_AT="${STARTED_OVERRIDE-1790000000}" \
    GITHUB_WORKFLOW="Nightly Thing" \
    GITHUB_SHA="abc123" \
    GITHUB_RUN_ID="4242" \
    "$@"
}

echo "vm transport:"

cat >"$TMP_ROOT/metrics.prom" <<'EOF'
# TYPE mutation_score gauge
mutation_score{env="ci",lang="go"} 85.5
EOF

if output="$(
  run_vm_push "$TMP_ROOT/push.args" "$TMP_ROOT/push.stdin" \
    "$REPO_ROOT/scripts/lib/vm-push.sh" "$TMP_ROOT/metrics.prom" 2>&1
)" \
  && grep -qF -- "--rm -i --restart=Never" "$TMP_ROOT/push.args" \
  && grep -qF -- "--image=docker.io/curlimages/curl:8.11.1" "$TMP_ROOT/push.args" \
  && grep -qF "http://private-vm.observability.svc:8428/api/v1/import/prometheus" "$TMP_ROOT/push.args" \
  && grep -qF "Content-Type: text/plain; version=0.0.4" "$TMP_ROOT/push.args" \
  && [ -z "$output" ]; then
  pass "VM push uses an auto-cleaned kubectl pod"
else
  fail "VM push uses an auto-cleaned kubectl pod (output=[$output])"
fi

if grep -qxF 'mutation_score{env="ci",lang="go"} 85.5 1790000000000' "$TMP_ROOT/push.stdin"; then
  pass "a sample carries the run's start as its time"
else
  fail "a sample carries the run's start as its time (sent=[$(cat "$TMP_ROOT/push.stdin")])"
fi

if grep -qxF 'ci_run_info{env="ci",workflow="Nightly Thing",commit="abc123",run_id="4242"} 1 1790000000000' "$TMP_ROOT/push.stdin" \
  && [ "$(grep -c '^ci_run_info' "$TMP_ROOT/push.stdin")" = "1" ]; then
  pass "a push writes one ci_run_info naming the workflow, the commit and the run"
else
  fail "a push writes one ci_run_info naming the workflow, the commit and the run (sent=[$(cat "$TMP_ROOT/push.stdin")])"
fi

for label in commit run_id grade; do
  printf 'pmat_repo_score{env="ci",%s="x"} 91.25\n' "$label" >"$TMP_ROOT/$label.prom"
  : >"$TMP_ROOT/$label.args"
  if run_vm_push "$TMP_ROOT/$label.args" "$TMP_ROOT/$label.stdin" \
    "$REPO_ROOT/scripts/lib/vm-push.sh" "$TMP_ROOT/$label.prom" >/dev/null 2>&1; then
    fail "a sample carrying $label is refused"
  elif [ ! -s "$TMP_ROOT/$label.args" ]; then
    pass "a sample carrying $label is refused before kubectl"
  else
    fail "a sample carrying $label is refused before kubectl"
  fi
done

: >"$TMP_ROOT/nostart.args"
if STARTED_OVERRIDE="" run_vm_push "$TMP_ROOT/nostart.args" "$TMP_ROOT/nostart.stdin" \
  "$REPO_ROOT/scripts/lib/vm-push.sh" "$TMP_ROOT/metrics.prom" >/dev/null 2>&1; then
  fail "a push with no run start is refused"
elif [ ! -s "$TMP_ROOT/nostart.args" ]; then
  pass "a push with no run start is refused before kubectl"
else
  fail "a push with no run start is refused before kubectl"
fi

cat >"$TMP_ROOT/stdin.prom" <<'EOF'
pmat_repo_score{env="ci"} 91.25
EOF
if run_vm_push "$TMP_ROOT/stdin.args" "$TMP_ROOT/stdin.captured" \
  "$REPO_ROOT/scripts/lib/vm-push.sh" <"$TMP_ROOT/stdin.prom" \
  && grep -qxF 'pmat_repo_score{env="ci"} 91.25 1790000000000' "$TMP_ROOT/stdin.captured"; then
  pass "VM push reads Prometheus text from stdin when no file is provided"
else
  fail "VM push reads Prometheus text from stdin when no file is provided"
fi

cat >"$TMP_ROOT/missing-label.prom" <<'EOF'
mutation_score{lang="go"} 85.5
EOF
: >"$TMP_ROOT/missing-label.args"
if run_vm_push "$TMP_ROOT/missing-label.args" "$TMP_ROOT/missing-label.stdin" \
  "$REPO_ROOT/scripts/lib/vm-push.sh" "$TMP_ROOT/missing-label.prom" >/dev/null 2>&1; then
  fail "VM push rejects metrics missing mandatory env label"
elif [ ! -s "$TMP_ROOT/missing-label.args" ]; then
  pass "VM push rejects metrics missing mandatory env label before kubectl"
else
  fail "VM push rejects metrics missing mandatory env label before kubectl"
fi

cat >"$TMP_ROOT/malformed.prom" <<'EOF'
not a prometheus sample
EOF
: >"$TMP_ROOT/malformed.args"
if run_vm_push "$TMP_ROOT/malformed.args" "$TMP_ROOT/malformed.stdin" \
  "$REPO_ROOT/scripts/lib/vm-push.sh" "$TMP_ROOT/malformed.prom" >/dev/null 2>&1; then
  fail "VM push rejects malformed Prometheus text"
elif [ ! -s "$TMP_ROOT/malformed.args" ]; then
  pass "VM push rejects malformed Prometheus text before kubectl"
else
  fail "VM push rejects malformed Prometheus text before kubectl"
fi

if KUBECTL_STATUS=19 run_vm_push "$TMP_ROOT/fail.args" "$TMP_ROOT/fail.stdin" \
  "$REPO_ROOT/scripts/lib/vm-push.sh" "$TMP_ROOT/metrics.prom" >/dev/null 2>&1; then
  fail "kubectl/VM transport failure propagates"
else
  pass "kubectl/VM transport failure propagates"
fi

drop_dir="$TMP_ROOT/drop-bin"
mkdir -p "$drop_dir"
cat >"$drop_dir/kubectl" <<'EOF'
#!/usr/bin/env bash
if [[ "$*" == *import/prometheus* ]]; then
  n=$(($(cat "$DROP_PUSHES_FILE" 2>/dev/null || echo 0) + 1))
  printf '%s' "$n" >"$DROP_PUSHES_FILE"
  cat >/dev/null
  if [ "$n" -le "$DROP_PUSHES" ]; then
    echo 'error: Internal error occurred: error sending request: Post "https://10.0.2.227:10250/attach/monitoring/vm-push": EOF' >&2
    exit 1
  fi
  exit 0
fi
printf '%s\n' "$*" >>"$DROP_READS_FILE"
if [ "$DROP_LANDED" = unread ]; then
  echo 'Error from server (Forbidden): pods is forbidden' >&2
  exit 1
fi
if [ "$DROP_LANDED" = yes ]; then
  printf '{"metric":{"__name__":"ci_run_info","run_id":"4242"},"values":[1],"timestamps":[1790000000000]}\n'
fi
EOF
chmod +x "$drop_dir/kubectl"

run_dropped_push() {
  local drops="$1" landed="$2"
  : >"$TMP_ROOT/drop.pushes"
  : >"$TMP_ROOT/drop.reads"
  PATH="$drop_dir:$PATH" \
    DROP_PUSHES="$drops" \
    DROP_LANDED="$landed" \
    DROP_PUSHES_FILE="$TMP_ROOT/drop.pushes" \
    DROP_READS_FILE="$TMP_ROOT/drop.reads" \
    KUBECTL_RETRY_DELAY=0 \
    VM_RUN_STARTED_AT=1790000000 \
    GITHUB_WORKFLOW="Nightly Thing" \
    GITHUB_SHA="abc123" \
    GITHUB_RUN_ID="4242" \
    "$REPO_ROOT/scripts/lib/vm-push.sh" "$TMP_ROOT/metrics.prom" >"$TMP_ROOT/drop.out" 2>&1
}
pushes() { cat "$TMP_ROOT/drop.pushes"; }

if run_dropped_push 1 yes && [ "$(pushes)" = "1" ]; then
  pass "a dropped push whose samples landed is not sent again"
else
  fail "a dropped push whose samples landed is not sent again (pushes=$(pushes), out=[$(cat "$TMP_ROOT/drop.out")])"
fi
reads="$(cat "$TMP_ROOT/drop.reads")"
if grep -qF 'match[]=ci_run_info{env="ci",workflow="Nightly Thing",run_id="4242"}' <<<"$reads" \
  && grep -qF "start=1790000000" <<<"$reads" \
  && grep -qF "api/v1/export" <<<"$reads"; then
  pass "and the read-back asks for this run's own record at its start"
else
  fail "and the read-back asks for this run's own record at its start (reads=[$reads])"
fi

if run_dropped_push 1 no && [ "$(pushes)" = "2" ]; then
  pass "a dropped push whose samples are absent is sent again"
else
  fail "a dropped push whose samples are absent is sent again (pushes=$(pushes), out=[$(cat "$TMP_ROOT/drop.out")])"
fi

if run_dropped_push 1 unread && [ "$(pushes)" = "2" ]; then
  pass "a dropped push whose read-back cannot answer is sent again"
else
  fail "a dropped push whose read-back cannot answer is sent again (pushes=$(pushes), out=[$(cat "$TMP_ROOT/drop.out")])"
fi

if run_dropped_push 99 no; then
  fail "a push that never lands fails"
else
  pass "a push that never lands fails"
fi
if [ "$(pushes)" = "4" ]; then
  pass "and stops at the retry budget"
else
  fail "and stops at the retry budget (pushes=$(pushes))"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ]
