#!/usr/bin/env bash
# CURL_BIN is stubbed for the whole file and answers with STUB_JSON, so no case reaches the API.
# The RATING_*_OVERRIDE variables supply changed files, findings and the processing state.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GUARD="$SCRIPT_DIR/../sonar-rating-guard.sh"
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
# The output is captured before the search because a piped `grep -q` loses the match to SIGPIPE.
assert_says() {
  local n="$1" want="$2"
  shift 2
  local out
  out="$("$@" 2>&1)"
  case "$out" in
    *"$want"*) pass "$n" ;;
    *) fail "$n (no '$want' in output)" ;;
  esac
}

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

# shellcheck source=../sonar-rating-guard.sh disable=SC1091
source "$GUARD"

export CURL_BIN="$STUB_DIR/curl"
export SONAR_TOKEN=stub
export RATING_PENDING_OVERRIDE=0
export RATING_SETTLE_RETRIES=0
export RATING_SETTLE_SLEEP=0

echo "srat_is_source (main production code — what the ratings are computed over):"
assert_ok "server go source" srat_is_source server/internal/alerts/postgres_noise.go
assert_ok "agent rust source" srat_is_source agent/crates/edge-tsdb/src/redb_store.rs
assert_ok "web tsx source" srat_is_source web/src/features/rules/TuningPanel.tsx
assert_fail "go test file is not main code" srat_is_source server/internal/api/handlers_test.go
assert_fail "ts test file is not main code" srat_is_source web/src/features/rules/RuleList.test.tsx
assert_fail "rust integration test is not main code" srat_is_source agent/crates/edge-tsdb/tests/store_test.rs
# A file leaves the ratings' reach through sonar.exclusions, whatever its path reads like.
assert_ok "analysed Go under server/tests is main code" srat_is_source server/tests/loadtest/credentials.go
assert_fail "generated go is not main code" srat_is_source server/internal/api/openapi_gen.go
assert_fail "outside the sonar roots" srat_is_source scripts/foo.go
assert_fail "markdown is not source" srat_is_source docs/Home.md

echo
echo "srat_is_analyzed (anything SonarCloud reads, tests included):"
assert_ok "main code is analyzed" srat_is_analyzed server/internal/alerts/postgres_noise.go
assert_ok "a ts test file is analyzed" srat_is_analyzed web/src/features/rules/RuleList.test.tsx
assert_ok "a go test file is analyzed" srat_is_analyzed server/internal/api/handlers_test.go
assert_fail "generated code is not analyzed" srat_is_analyzed server/internal/api/openapi_gen.go
assert_fail "markdown is not analyzed" srat_is_analyzed docs/Home.md

echo
echo "srat_blocks (which finding types can drop a rating below A):"
assert_ok "a bug drops new_reliability_rating" srat_blocks BUG
assert_ok "a vulnerability drops new_security_rating" srat_blocks VULNERABILITY
assert_fail "a code smell only moves maintainability" srat_blocks CODE_SMELL

echo
echo "srat_main — a finding on a file this change touched:"
RATING_CHANGED_OVERRIDE="web/src/features/rules/TuningPanel.tsx" \
  RATING_ISSUES_OVERRIDE="web/src/features/rules/TuningPanel.tsx	BUG	CRITICAL	typescript:S2871	88	Provide a compare function" \
  assert_rc "a BUG on a changed source file fails" 1 srat_main
RATING_CHANGED_OVERRIDE="server/internal/alerts/postgres_noise.go" \
  RATING_ISSUES_OVERRIDE="server/internal/alerts/postgres_noise.go	VULNERABILITY	MAJOR	go:S2077	45	dynamically formatted SQL" \
  assert_rc "a VULNERABILITY on a changed source file fails" 1 srat_main
RATING_CHANGED_OVERRIDE="server/internal/alerts/postgres_noise.go" \
  RATING_HOTSPOTS_OVERRIDE="server/internal/alerts/postgres_noise.go	45	go:S2077" \
  assert_rc "an unreviewed hotspot on a changed source file fails" 1 srat_main
RATING_CHANGED_OVERRIDE="web/src/features/rules/TuningPanel.tsx" \
  RATING_ISSUES_OVERRIDE="web/src/features/rules/TuningPanel.tsx	BUG	CRITICAL	typescript:S2871	88	Provide a compare function" \
  assert_says "and the failure names the rule and the line" "TuningPanel.tsx:88" srat_main

echo
echo "srat_main — a clean change:"
RATING_CHANGED_OVERRIDE="server/internal/alerts/postgres_noise.go" \
  assert_rc "no findings at all passes" 0 srat_main
RATING_CHANGED_OVERRIDE="" \
  RATING_ISSUES_OVERRIDE="server/internal/alerts/postgres_noise.go	BUG	CRITICAL	go:S1	1	x" \
  assert_rc "no changed files → nothing to answer for" 0 srat_main
RATING_CHANGED_OVERRIDE="docs/Home.md" \
  assert_rc "a docs-only change has no analyzed file" 0 srat_main

echo
echo "srat_main — scoping, so somebody else's finding is not this commit's problem:"
RATING_CHANGED_OVERRIDE="server/internal/rules/tags.go" \
  RATING_ISSUES_OVERRIDE="server/internal/alerts/postgres_noise.go	BUG	CRITICAL	go:S1	1	x" \
  assert_rc "a BUG on an untouched file does not fail this change" 0 srat_main

echo
echo "srat_main — what warns instead of failing:"
# A code smell moves no gate condition, so it is reported without blocking.
RATING_CHANGED_OVERRIDE="web/src/features/rules/RuleList.tsx" \
  RATING_ISSUES_OVERRIDE="web/src/features/rules/RuleList.tsx	CODE_SMELL	MAJOR	typescript:S3358	31	nested ternary" \
  assert_rc "a code smell on a changed file passes" 0 srat_main
RATING_CHANGED_OVERRIDE="web/src/features/rules/RuleList.tsx" \
  RATING_ISSUES_OVERRIDE="web/src/features/rules/RuleList.tsx	CODE_SMELL	MAJOR	typescript:S3358	31	nested ternary" \
  assert_says "a code smell is still named in the output" "typescript:S3358" srat_main
# No finding can exist on a file sonar.exclusions removes, so a bug there cannot move a rating.
RATING_CHANGED_OVERRIDE="web/src/features/rules/RuleList.test.tsx" \
  RATING_ISSUES_OVERRIDE="web/src/features/rules/RuleList.test.tsx	BUG	CRITICAL	typescript:S2871	10	sort" \
  assert_rc "a bug in an unanalysed test file does not fail the gate's metric" 0 srat_main

RATING_CHANGED_OVERRIDE="server/tests/loadtest/credentials.go" \
  RATING_ISSUES_OVERRIDE="server/tests/loadtest/credentials.go	VULNERABILITY	MAJOR	go:S2077	45	dynamically formatted SQL" \
  assert_rc "a vulnerability on an analysed file under tests/ fails" 1 srat_main
RATING_CHANGED_OVERRIDE="web/src/features/rules/RuleList.test.tsx" \
  RATING_ISSUES_OVERRIDE="web/src/features/rules/RuleList.test.tsx	BUG	CRITICAL	typescript:S2871	10	sort" \
  assert_says "a bug in a test file is still named" "typescript:S2871" srat_main

echo
echo "srat_main — an unindexed analysis must never read as a clean one:"
# Zero findings and an unfinished count are the same empty list, so the guard refuses the answer.
RATING_PENDING_OVERRIDE=1 \
  RATING_CHANGED_OVERRIDE="server/internal/alerts/postgres_noise.go" \
  assert_rc "still processing → refuse, do not pass" 1 srat_main
RATING_PENDING_OVERRIDE=1 \
  RATING_CHANGED_OVERRIDE="server/internal/alerts/postgres_noise.go" \
  assert_says "and says why" "still being processed" srat_main

echo
echo "srat_fetch_issues parses what the API actually returns:"
issues_json() {
  printf '%s' '{"total":1,"issues":[{"component":"volchanskyi_opengate:web/src/features/rules/TuningPanel.tsx","type":"BUG","severity":"CRITICAL","rule":"typescript:S2871","line":88,"message":"Provide a compare function"}]}'
}
parsed="$(STUB_JSON="$(issues_json)" srat_fetch_issues)"
case "$parsed" in
  *"web/src/features/rules/TuningPanel.tsx"*BUG*typescript:S2871*)
    pass "the project key is stripped and the fields come through"
    ;;
  *) fail "the project key is stripped and the fields come through (got: $parsed)" ;;
esac
STUB_JSON="$(issues_json)" \
RATING_CHANGED_OVERRIDE="web/src/features/rules/TuningPanel.tsx" \
  assert_rc "and the parsed BUG fails the guard" 1 srat_main

echo
echo "srat_main prerequisite:"
SONAR_TOKEN="" RATING_CHANGED_OVERRIDE="server/internal/alerts/postgres_noise.go" \
  assert_rc "no token + no override → rc 2" 2 srat_main

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
