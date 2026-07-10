#!/usr/bin/env bash
# Guards against a Go-version skew between the local precommit gauntlet and CI
# for govulncheck.
#
# The gauntlet's govulncheck honors server/go.mod's `toolchain` directive
# (GOTOOLCHAIN=auto), so it analyzes against whatever stdlib that pins. CI's
# "Security Audit" job pins an exact go-version for setup-go so govulncheck
# analyzes a specific patched stdlib (the '1.26' minor manifest entry can lag a
# fresh patch release). If those two diverge, a stdlib CVE fixed by bumping
# go.mod's toolchain passes the gauntlet while CI still scans the vulnerable
# patch — a false-green gauntlet. That is exactly how a go1.26.4 -> 1.26.5 bump
# once landed green locally and red in CI.
#
# Fix by construction: server/go.mod is the single source of truth. Every exact
# (three-part) go-version pinned in any workflow must equal it, and the Security
# Audit job must carry such an exact pin (never a floating minor that can lag
# go.mod and silently miss a fix).
#
# Run: ./scripts/tests/ci-govulncheck-go-version.test.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
GOMOD="$ROOT/server/go.mod"
CI="$ROOT/.github/workflows/ci.yml"
WORKFLOWS="$ROOT/.github/workflows"

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

echo "ci-govulncheck-go-version:"

for f in "$GOMOD" "$CI"; do
  if [ ! -f "$f" ]; then
    echo "FAIL: $f not found" >&2
    exit 1
  fi
done

# Source of truth: the three-part toolchain directive in server/go.mod.
MOD_VER="$(grep -oE '^toolchain go[0-9]+\.[0-9]+\.[0-9]+' "$GOMOD" | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1 || true)"
if [ -n "$MOD_VER" ]; then
  pass "server/go.mod pins a three-part toolchain ($MOD_VER)"
else
  fail "server/go.mod pins a three-part toolchain directive"
  echo
  echo "Summary: $PASS passed, $FAIL failed"
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi

# (A) Every exact three-part go-version pinned in any workflow must equal it.
drift=0
found=0
while IFS= read -r pin; do
  [ -n "$pin" ] || continue
  found=1
  if [ "$pin" != "$MOD_VER" ]; then
    drift=1
    fail "a workflow pins go-version $pin != go.mod toolchain $MOD_VER"
  fi
done < <(grep -rhoE "go-version: *'[0-9]+\.[0-9]+\.[0-9]+'" "$WORKFLOWS" | grep -oE '[0-9]+\.[0-9]+\.[0-9]+')
if [ "$found" -eq 0 ]; then
  fail "at least one workflow pins an exact go-version to match go.mod"
elif [ "$drift" -eq 0 ]; then
  pass "all exact workflow go-version pins == go.mod toolchain ($MOD_VER)"
fi

# (B) The CI "Security Audit" job must carry an exact pin == go.mod toolchain,
# so govulncheck deterministically scans the patched stdlib and never silently
# falls back to a floating minor that lags go.mod.
audit_block="$(awk '
  /^  [A-Za-z0-9_-]+:[[:space:]]*$/ { if (buf ~ /name: Security Audit/) print buf; buf = "" }
  { buf = buf $0 "\n" }
  END { if (buf ~ /name: Security Audit/) print buf }
' "$CI")"
audit_pin="$(grep -oE "go-version: *'[0-9]+\.[0-9]+\.[0-9]+'" <<<"$audit_block" | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1 || true)"
if [ -z "$audit_block" ]; then
  fail "ci.yml defines a 'Security Audit' job"
elif [ -z "$audit_pin" ]; then
  fail "Security Audit job pins an exact go-version (found a floating minor or none)"
elif [ "$audit_pin" = "$MOD_VER" ]; then
  pass "Security Audit go-version ($audit_pin) == go.mod toolchain"
else
  fail "Security Audit go-version $audit_pin != go.mod toolchain $MOD_VER"
fi

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
