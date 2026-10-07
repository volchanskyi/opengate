#!/usr/bin/env bash
# Holds every API path and query key the hand-written callers spell to api/openapi.yaml.
# The callers are smoke-test.sh, e2e-stack-up.sh and the k6 scenarios under load/.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
SPEC="$ROOT/api/openapi.yaml"

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
summarize() {
  echo
  echo "Summary: $PASS passed, $FAIL failed"
  if [ "$FAIL" -gt 0 ]; then
    printf '  - %s\n' "${FAILURES[@]}" >&2
    exit 1
  fi
  exit 0
}

echo "api-endpoint-drift:"

CALLERS=(deploy/scripts/smoke-test.sh deploy/scripts/e2e-stack-up.sh)
# Under load/, a caller is a file that imports k6's HTTP module.
found_js=0
while IFS= read -r js; do
  found_js=$((found_js + 1))
  if grep -q 'from "k6/http"' "$ROOT/$js"; then
    CALLERS+=("$js")
  fi
done < <(cd "$ROOT" && find load -type f -name '*.js' | sort)

# A selection that reaches nothing fails here, since every later check would pass vacuously.
if [ "$found_js" -eq 0 ]; then
  echo "FAIL: no JavaScript under load/ — the caller selection reached nothing" >&2
  exit 1
fi

for f in "$SPEC" "${CALLERS[@]/#/$ROOT/}"; do
  if [ ! -f "$f" ]; then
    echo "FAIL: $f not found" >&2
    exit 1
  fi
done

# Spec paths are the two-space-indented top-level keys, with each {param} name flattened.
declared="$(grep -oE '^  (/[^ :]+):' "$SPEC" | tr -d ' :' \
  | awk '{ gsub(/[{][A-Za-z0-9_]*[}]/, "{param}"); print }' | sort -u)"
if [ -z "$declared" ]; then
  fail "api/openapi.yaml declares at least one path (nothing matched — a grep that finds nothing is a failure, not a pass)"
  summarize
fi
pass "api/openapi.yaml declares $(wc -l <<<"$declared") paths"

# Only spec parameters living in the query string compare to a URL's `?key=`.
declared_params="$(awk '
  /^[[:space:]]*-[[:space:]]*name:[[:space:]]*/ { n = $NF; next }
  /^[[:space:]]*in:[[:space:]]*query[[:space:]]*$/ { if (n != "") { print n; n = "" } }
' "$SPEC" | sort -u)"
if [ -z "$declared_params" ]; then
  fail "api/openapi.yaml declares at least one query parameter (nothing matched — the check would pass vacuously)"
  summarize
fi
pass "api/openapi.yaml declares $(wc -l <<<"$declared_params") query parameters"

# Comments are stripped, then every shell expansion and JS template substitution becomes {param}.
normalize() {
  grep -vE '^[[:space:]]*(#|//)' "$1" \
    | awk '{
        gsub(/[$][{][^}]*[}]/, "{param}")
        gsub(/[$][A-Za-z_][A-Za-z0-9_]*/, "{param}")
        print
      }'
}

drift=0
for caller in "${CALLERS[@]}"; do
  normalized="$(normalize "$ROOT/$caller")"

  probed="$(grep -oE '/api/v1/[-A-Za-z0-9_./{}]*' <<<"$normalized" \
    | awk '{ sub(/\/$/, ""); print }' | sort -u)"
  if [ -z "$probed" ]; then
    fail "$caller probes at least one /api/v1 path (extraction matched nothing — the check would pass vacuously)"
    drift=1
    continue
  fi

  while IFS= read -r path; do
    [ -n "$path" ] || continue
    if grep -qxF "$path" <<<"$declared"; then
      pass "$caller probes $path, declared in the spec"
    else
      drift=1
      fail "$caller probes $path, which api/openapi.yaml does not declare"
    fi
  done <<<"$probed"

  # Most requests carry no query keys, so an empty extraction passes.
  sent_params="$(grep -oE '/api/v1/[-A-Za-z0-9_./{}]*\?[^"'"'"' ]*' <<<"$normalized" \
    | grep -oE '[?&][A-Za-z_][A-Za-z0-9_]*=' \
    | tr -d '?&=' | sort -u || true)"
  while IFS= read -r param; do
    [ -n "$param" ] || continue
    if grep -qxF "$param" <<<"$declared_params"; then
      pass "$caller sends ?$param, declared in the spec"
    else
      drift=1
      fail "$caller sends ?$param, which api/openapi.yaml does not declare as a query parameter"
    fi
  done <<<"$sent_params"
done

if [ "$drift" -eq 0 ]; then
  pass "no API drift between the hand-written callers and the spec"
fi

summarize
