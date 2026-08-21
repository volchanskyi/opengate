#!/usr/bin/env bash
# Guards the hand-written API callers against spec drift.
#
# Three callers spell OpenGate API URLs as string literals rather than through a
# generated client: deploy/scripts/smoke-test.sh (cd.yml, after a merge to main,
# against staging and production), deploy/scripts/e2e-stack-up.sh (the browser
# stack's bring-up, which signs in and mints an enrolment token so the machines
# can install), and the k6 scenarios under load/ (the nightly load-test
# workflow). Nothing before those runs reads the URL strings — not the
# OpenAPI spec-drift check (it scans the spec for unguarded mutating ops), not
# check-doc-links (Markdown links under docs/ and .claude/), not the Go or web
# suites. So renaming a route in api/openapi.yaml leaves both probing a path
# that no longer exists, and the first thing that notices is a failed deploy or
# a failed nightly — which is how the /api/v1/groups -> /api/v1/sites rename
# broke CD and then, a day later, the load-test run.
#
# Query parameter names drift the same way and fail more quietly: an unknown
# query key is ignored, so the caller silently measures a different workload
# instead of erroring. Those are checked against the spec too.
#
# Fix by construction: api/openapi.yaml is the single source of truth. Every
# OpenGate API path and query parameter these callers spell must be declared
# there.
#
# Run: ./scripts/tests/api-endpoint-drift.test.sh

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

# Callers that spell API URLs by hand, relative to the repo root.
CALLERS=(deploy/scripts/smoke-test.sh deploy/scripts/e2e-stack-up.sh)
while IFS= read -r js; do
  CALLERS+=("$js")
done < <(cd "$ROOT" && find load -type f -name '*.js' | sort)

for f in "$SPEC" "${CALLERS[@]/#/$ROOT/}"; do
  if [ ! -f "$f" ]; then
    echo "FAIL: $f not found" >&2
    exit 1
  fi
done

# Paths the spec declares, as two-space-indented top-level keys, with each
# {param} name flattened so a placeholder compares by position, not by spelling.
declared="$(grep -oE '^  (/[^ :]+):' "$SPEC" | tr -d ' :' \
  | awk '{ gsub(/[{][A-Za-z0-9_]*[}]/, "{param}"); print }' | sort -u)"
if [ -z "$declared" ]; then
  fail "api/openapi.yaml declares at least one path (nothing matched — a grep that finds nothing is a failure, not a pass)"
  summarize
fi
pass "api/openapi.yaml declares $(wc -l <<<"$declared") paths"

# Query parameter names the spec declares, anywhere. A parameter block names the
# parameter and then says where it lives; only the ones living in the query
# string are comparable to a URL's `?key=`.
declared_params="$(awk '
  /^[[:space:]]*-[[:space:]]*name:[[:space:]]*/ { n = $NF; next }
  /^[[:space:]]*in:[[:space:]]*query[[:space:]]*$/ { if (n != "") { print n; n = "" } }
' "$SPEC" | sort -u)"
if [ -z "$declared_params" ]; then
  fail "api/openapi.yaml declares at least one query parameter (nothing matched — the check would pass vacuously)"
  summarize
fi
pass "api/openapi.yaml declares $(wc -l <<<"$declared_params") query parameters"

# Strip comments, then collapse every shell expansion and JS template
# substitution to the same {param} placeholder, so a URL built from a variable
# compares against the templated declaration. A path only counts when it is
# requested, never when it is described.
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

  # Query keys sent against those paths. Absent keys are fine — most requests
  # carry none — so an empty extraction is not a vacuous pass here.
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
