#!/usr/bin/env bash
# Tests for scripts/sonar-duplication-guard.sh. Plain bash; no network and no git
# — changed files come from DUP_CHANGED_OVERRIDE and densities from
# DUP_DENSITY_OVERRIDE (or a stubbed CURL_BIN).
# Run: ./scripts/tests/sonar-duplication-guard.test.sh
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GUARD="$SCRIPT_DIR/../sonar-duplication-guard.sh"
[ -f "$GUARD" ] || {
  echo "FAIL: $GUARD not found" >&2
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
assert_ok() {
  local n="$1"
  shift
  if "$@" >/dev/null 2>&1; then pass "$n"; else fail "$n (expected 0, got $?)"; fi
}
assert_fail() {
  local n="$1"
  shift
  if "$@" >/dev/null 2>&1; then fail "$n (expected non-zero)"; else pass "$n"; fi
}
assert_rc() {
  local n="$1" want="$2"
  shift 2
  "$@" >/dev/null 2>&1
  local got=$?
  if [ "$got" = "$want" ]; then pass "$n"; else fail "$n (want rc=$want got=$got)"; fi
}

# --- Stub curl: echoes a canned SonarCloud measures response from STUB_JSON. ---
STUB_DIR="$(mktemp -d)"
cat >"$STUB_DIR/curl" <<'STUB'
#!/usr/bin/env bash
printf '%s' "${STUB_JSON:-}"
STUB
chmod +x "$STUB_DIR/curl"
# Invoked through the EXIT trap.
# shellcheck disable=SC2329
cleanup() { rm -rf "$STUB_DIR"; }
trap cleanup EXIT

# shellcheck source=../sonar-duplication-guard.sh disable=SC1091
source "$GUARD"

echo "sdup_above_ceiling (float-safe numeric compare):"
assert_ok "7.4 > 3 is above (the gate value)" sdup_above_ceiling 7.4 3
assert_ok "33.3 > 3 is above" sdup_above_ceiling 33.3 3
assert_ok "3.01 > 3 is above" sdup_above_ceiling 3.01 3
assert_fail "3 is not above 3" sdup_above_ceiling 3 3
assert_fail "0 is not above 3" sdup_above_ceiling 0 3
assert_fail "2.9 is not above 3" sdup_above_ceiling 2.9 3

echo
echo "sdup_is_source (production Rust/Go/TS under a sonar.sources root):"
assert_ok "agent rust source" sdup_is_source agent/crates/edge-tsdb/src/redb_compact.rs
assert_ok "server go source" sdup_is_source server/internal/api/handlers.go
assert_ok "web tsx source" sdup_is_source web/src/App.tsx
assert_fail "go test file excluded" sdup_is_source server/internal/api/handlers_test.go
assert_fail "rust integration test excluded" sdup_is_source agent/crates/edge-tsdb/tests/store_test.rs
assert_fail "ts test file excluded" sdup_is_source web/src/foo.test.ts
assert_fail "generated go excluded" sdup_is_source server/internal/api/openapi_gen.go
assert_fail "pb.go excluded" sdup_is_source server/internal/proto/msg.pb.go
assert_fail "outside sonar.sources roots" sdup_is_source scripts/foo.rs
assert_fail "markdown is not source" sdup_is_source agent/crates/edge-tsdb/src/README.md

echo
echo "sdup_density via DUP_DENSITY_OVERRIDE:"
export DUP_DENSITY_OVERRIDE=$'agent/crates/edge-tsdb/src/redb_compact.rs=33.3\nagent/crates/edge-tsdb/src/redb_store.rs=0.0'
if [ "$(sdup_density agent/crates/edge-tsdb/src/redb_compact.rs)" = "33.3" ]; then
  pass "override returns known density"
else
  fail "override returns known density"
fi
if [ -z "$(sdup_density agent/crates/edge-tsdb/src/absent.rs)" ]; then
  pass "override empty for unknown file"
else
  fail "override empty for unknown file"
fi
unset DUP_DENSITY_OVERRIDE

echo
echo "sdup_main via overrides (no network, no git):"
export DUP_CEILING=3
DUP_CHANGED_OVERRIDE="agent/crates/edge-tsdb/src/redb_compact.rs" \
  DUP_DENSITY_OVERRIDE="agent/crates/edge-tsdb/src/redb_compact.rs=33.3" \
  assert_fail "33.3% changed file fails the 3% ceiling" sdup_main
DUP_CHANGED_OVERRIDE="agent/crates/edge-tsdb/src/redb_compact.rs" \
  DUP_DENSITY_OVERRIDE="agent/crates/edge-tsdb/src/redb_compact.rs=0.0" \
  assert_ok "0% changed file clears the ceiling" sdup_main
DUP_CHANGED_OVERRIDE=$'agent/crates/edge-tsdb/src/redb_store.rs\nagent/crates/edge-tsdb/src/redb_compact.rs' \
  DUP_DENSITY_OVERRIDE=$'agent/crates/edge-tsdb/src/redb_store.rs=0.0\nagent/crates/edge-tsdb/src/redb_compact.rs=7.4' \
  assert_fail "one clean + one over-ceiling fails" sdup_main
DUP_CHANGED_OVERRIDE="docs/Home.md" \
  DUP_DENSITY_OVERRIDE="ignored=1" \
  assert_ok "no changed SOURCE files → pass" sdup_main
# A changed source file SonarCloud has no measure for (brand-new, not yet
# analysed) is skipped, not failed.
DUP_CHANGED_OVERRIDE="agent/crates/edge-tsdb/src/redb_backend.rs" \
  DUP_DENSITY_OVERRIDE="other=9.9" \
  assert_ok "changed file with no measure is skipped" sdup_main

echo
echo "sdup_main via stubbed API (CURL_BIN):"
export CURL_BIN="$STUB_DIR/curl"
export SONAR_TOKEN="dummy"
unset DUP_DENSITY_OVERRIDE
STUB_JSON='{"component":{"measures":[{"metric":"duplicated_lines_density","value":"0.0"}]}}' \
  DUP_CHANGED_OVERRIDE="agent/crates/edge-tsdb/src/gorilla.rs" \
  assert_ok "API 0.0 clears the ceiling" sdup_main
STUB_JSON='{"component":{"measures":[{"metric":"duplicated_lines_density","value":"33.3"}]}}' \
  DUP_CHANGED_OVERRIDE="agent/crates/edge-tsdb/src/redb_compact.rs" \
  assert_fail "API 33.3 fails the ceiling" sdup_main

echo
echo "sdup_main prerequisite:"
(
  unset SONAR_TOKEN
  unset DUP_DENSITY_OVERRIDE
  CURL_BIN=/bin/false
  DUP_CHANGED_OVERRIDE="agent/crates/edge-tsdb/src/redb_compact.rs"
  assert_rc "no token + no override → rc 2" 2 sdup_main
)

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
