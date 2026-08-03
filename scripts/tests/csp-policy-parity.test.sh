#!/usr/bin/env bash
# The Content-Security-Policy and Permissions-Policy are declared twice: the Go
# server sends them on every response, and the ingress add-headers ConfigMap
# layers the same values at the edge. Two sources drift silently — a browser
# receiving both enforces the intersection, so a divergence quietly tightens or
# loosens the effective policy. This test pins them together.
#
# Run: ./scripts/tests/csp-policy-parity.test.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
MIDDLEWARE="$REPO_ROOT/server/internal/api/middleware.go"
VALUES="$REPO_ROOT/deploy/helm/opengate/values.yaml"

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

# Concatenate the Go string-literal pieces of a const into one line.
go_const_value() {
  local name="$1"
  awk -v name="$name" '
    $0 ~ "^const " name " = " { collecting = 1 }
    collecting {
      while (match($0, /"[^"]*"/)) {
        piece = substr($0, RSTART + 1, RLENGTH - 2)
        out = out piece
        $0 = substr($0, RSTART + RLENGTH)
      }
      if ($0 !~ /\+[[:space:]]*$/) { print out; exit }
    }
  ' "$MIDDLEWARE"
}

# Read a YAML scalar that may be a folded (>-) block or a quoted one-liner, and
# normalize the fold back to single spaces.
yaml_value() {
  local key="$1"
  awk -v key="$key" '
    $0 ~ "^  " key ": >-" { folded = 1; next }
    folded && /^    / { gsub(/^[[:space:]]+|[[:space:]]+$/, ""); out = out (out ? " " : "") $0; next }
    folded { print out; out = ""; exit }
    $0 ~ "^  " key ": \"" {
      line = $0
      sub(/^[^"]*"/, "", line)
      sub(/"[[:space:]]*$/, "", line)
      print line
      exit
    }
    END { if (folded && out) print out }
  ' "$VALUES"
}

echo "csp-policy-parity:"

go_csp="$(go_const_value contentSecurityPolicy)"
yaml_csp="$(yaml_value csp)"
go_pp="$(go_const_value permissionsPolicy)"
yaml_pp="$(yaml_value permissionsPolicy)"

if [ -n "$go_csp" ]; then
  pass "server declares a Content-Security-Policy"
else
  fail "server declares a Content-Security-Policy"
fi

if [ -n "$yaml_csp" ]; then
  pass "chart declares ingress.csp"
else
  fail "chart declares ingress.csp"
fi

if [ "$go_csp" = "$yaml_csp" ]; then
  pass "Content-Security-Policy matches between server and chart"
else
  fail "Content-Security-Policy drift: server=[$go_csp] chart=[$yaml_csp]"
fi

if [ "$go_pp" = "$yaml_pp" ]; then
  pass "Permissions-Policy matches between server and chart"
else
  fail "Permissions-Policy drift: server=[$go_pp] chart=[$yaml_pp]"
fi

# frame-ancestors cannot be set by a meta tag and is the clickjacking control
# that X-Frame-Options only approximates; keep it in the policy.
case "$go_csp" in
  *"frame-ancestors 'none'"*) pass "policy denies framing" ;;
  *) fail "policy denies framing" ;;
esac

printf '\n%d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf 'failures:\n' >&2
  for f in "${FAILURES[@]}"; do printf '  - %s\n' "$f" >&2; done
  exit 1
fi
exit 0
