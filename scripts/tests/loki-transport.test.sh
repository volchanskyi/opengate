#!/usr/bin/env bash
# Offline payload and transport tests for private in-cluster Loki access.

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
if [[ "$*" == *"loki-query-"* ]]; then
  printf '%s\n' '{"data":{"result":[{"value":[0,"91.25"]}]}}'
fi
exit "${KUBECTL_STATUS:-0}"
EOF
chmod +x "$bin_dir/kubectl"

run_with_transport() {
  local args_file="$1"
  local stdin_file="$2"
  shift 2
  env \
    PATH="$bin_dir:$PATH" \
    KUBECTL_ARGS="$args_file" \
    KUBECTL_STDIN="$stdin_file" \
    LOKI_NAMESPACE="observability" \
    LOKI_SERVICE="private-loki" \
    "$@"
}

echo "loki transport:"

cat >"$TMP_ROOT/pmat.json" <<'EOF'
{"commit":"abc123","repo_score":91.25,"below_bplus":0}
EOF
if output="$(
  run_with_transport "$TMP_ROOT/pmat.args" "$TMP_ROOT/pmat.stdin" \
    "$REPO_ROOT/scripts/pmat-loki-push.sh" "$TMP_ROOT/pmat.json" 2>&1
)" \
  && grep -qF -- "--rm -i --restart=Never" "$TMP_ROOT/pmat.args" \
  && grep -qF -- "--image=docker.io/curlimages/curl:8.11.1" "$TMP_ROOT/pmat.args" \
  && grep -qF "http://private-loki.observability.svc:3100/loki/api/v1/push" "$TMP_ROOT/pmat.args" \
  && jq -e '.streams[0].stream.job == "pmat-trend"' "$TMP_ROOT/pmat.stdin" >/dev/null \
  && jq -e '.streams[0].values[0][1] | fromjson | .repo_score == 91.25' "$TMP_ROOT/pmat.stdin" >/dev/null \
  && [ -z "$output" ]; then
  pass "PMAT push uses an auto-cleaned kubectl pod and preserves the canonical row"
else
  fail "PMAT push uses an auto-cleaned kubectl pod and preserves the canonical row"
fi

cat >"$TMP_ROOT/mutation.json" <<'EOF'
{"commit":"abc123","scores":{"rust":{"score":80},"go":{"score":81},"web":{"score":82}}}
EOF
if run_with_transport "$TMP_ROOT/mutation.args" "$TMP_ROOT/mutation.stdin" \
  "$REPO_ROOT/scripts/mutation-loki-push.sh" "$TMP_ROOT/mutation.json" \
  && [ "$(jq '.streams | length' "$TMP_ROOT/mutation.stdin")" -eq 3 ] \
  && jq -e '.streams[] | select(.stream.language == "go") | .values[0][1] | fromjson | .score == 81' \
    "$TMP_ROOT/mutation.stdin" >/dev/null; then
  pass "mutation push emits one validated stream per language"
else
  fail "mutation push emits one validated stream per language"
fi

cat >"$TMP_ROOT/drift.json" <<'EOF'
{"commit":"abc123","changes":2}
EOF
if run_with_transport "$TMP_ROOT/drift.args" "$TMP_ROOT/drift.stdin" \
  "$REPO_ROOT/scripts/terraform-drift-loki-push.sh" "$TMP_ROOT/drift.json" \
  && jq -e '.streams[0].stream.source == "terraform-drift"' "$TMP_ROOT/drift.stdin" >/dev/null; then
  pass "terraform drift push labels the private transport payload"
else
  fail "terraform drift push labels the private transport payload"
fi

if value="$(
  run_with_transport "$TMP_ROOT/query.args" "$TMP_ROOT/query.stdin" \
    "$REPO_ROOT/scripts/pmat-loki-query.sh" repo_score
)" \
  && [ "$value" = "91.25" ] \
  && grep -qF -- "--rm -i --restart=Never" "$TMP_ROOT/query.args" \
  && grep -qF -- "--image=docker.io/curlimages/curl:8.11.1" "$TMP_ROOT/query.args" \
  && [ ! -s "$TMP_ROOT/query.stdin" ]; then
  pass "PMAT query returns the latest scalar through an auto-cleaned pod"
else
  fail "PMAT query returns the latest scalar through an auto-cleaned pod"
fi

if KUBECTL_STATUS=19 run_with_transport "$TMP_ROOT/fail.args" "$TMP_ROOT/fail.stdin" \
  "$REPO_ROOT/scripts/pmat-loki-push.sh" "$TMP_ROOT/pmat.json" >/dev/null 2>&1; then
  fail "kubectl transport failure propagates"
else
  pass "kubectl transport failure propagates"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ]
