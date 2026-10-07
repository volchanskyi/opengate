#!/usr/bin/env bash
# Tests for scripts/check-duplication, which reads repeated code from Sonar's kept scan
# report, and for the places that run it.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
FIXTURE="$SCRIPT_DIR/fixtures/check-duplication/real-report.tar.gz"

PASS=0
FAIL=0
FAILURES=()
TMP_DIR="$(mktemp -d)"
READER="$TMP_DIR/check-duplication"
trap 'rm -rf "$TMP_DIR"' EXIT

pass() {
  PASS=$((PASS + 1))
  printf '  ok   %s\n' "$1"
}
fail() {
  FAIL=$((FAIL + 1))
  FAILURES+=("$1")
  printf '  FAIL %s\n' "$1" >&2
}

run_reader() {
  OUTPUT=""
  STATUS=0
  OUTPUT="$("$READER" "$@" 2>&1)" || STATUS=$?
}

expect() {
  local name="$1" want="$2" needle="${3:-}"
  if [ "$STATUS" -eq "$want" ]; then
    pass "$name exits $want"
  else
    fail "$name exits $want (got $STATUS: $OUTPUT)"
  fi
  if [ -n "$needle" ]; then
    if grep -qF -- "$needle" <<<"$OUTPUT"; then
      pass "$name reports $needle"
    else
      fail "$name reports $needle (got: $OUTPUT)"
    fi
  fi
}

echo "check-duplication:"

if (cd "$REPO_ROOT" && GO111MODULE=off go build -o "$READER" ./scripts/check-duplication); then
  pass "reader builds without modules or network"
else
  fail "reader builds without modules or network"
  printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
  exit 1
fi

if (cd "$REPO_ROOT" && GO111MODULE=off go test -count=1 ./scripts/check-duplication >"$TMP_DIR/unit.log" 2>&1); then
  pass "reader unit tests pass"
else
  fail "reader unit tests pass ($(tail -20 "$TMP_DIR/unit.log"))"
fi

tar -xzf "$FIXTURE" -C "$TMP_DIR"
run_reader --report "$TMP_DIR/scanner-report"
expect "the real report" 1 "web/src/lib/transport/ws-transport.ts: 15.7% repeated (22 of 140 lines)"
for path in agent/crates/edge-tsdb/src/redb_compact.rs agent/crates/edge-tsdb/src/redb_store.rs web/src/lib/transport/webrtc-transport.ts; do
  if grep -qF "$path: " <<<"$OUTPUT"; then
    pass "the real report names $path"
  else
    fail "the real report names $path (got: $OUTPUT)"
  fi
done
if grep -qE '^check-duplication: read [0-9]+ production files, 4 above 3%' <<<"$OUTPUT"; then
  pass "the real report counts what it read"
else
  fail "the real report counts what it read (got: $OUTPUT)"
fi

run_reader --report "$TMP_DIR/missing"
expect "a missing report" 2 "make sonar"

mkdir -p "$TMP_DIR/broken"
printf 'not a report' >"$TMP_DIR/broken/metadata.pb"
run_reader --report "$TMP_DIR/broken"
expect "an unreadable report" 1 "unreadable report"

gauntlet="$REPO_ROOT/scripts/precommit-gauntlet.sh"
if grep -qF 'scripts/check-duplication' "$gauntlet" && ! grep -qF 'sonar-duplication-guard' "$gauntlet"; then
  pass "the gauntlet reads the kept report after make sonar"
else
  fail "the gauntlet reads the kept report after make sonar"
fi

scan="$REPO_ROOT/scripts/sonar-scan.sh"
if grep -qF 'scripts/sonar-scan.sh' "$REPO_ROOT/Makefile" \
  && grep -qF 'sonar.scanner.keepReport=true' "$scan" \
  && grep -qF 'sonar.working.directory=.scannerwork' "$scan"; then
  pass "make sonar keeps the report in the work tree"
else
  fail "make sonar keeps the report in the work tree"
fi

ci="$REPO_ROOT/.github/workflows/ci.yml"
sonar_job="$(awk '/^  sonarcloud:/ { in_job = 1; print; next } in_job && /^  [a-z][a-z0-9-]*:/ { exit } in_job { print }' "$ci")"
if grep -qF 'sonar.scanner.keepReport=true' <<<"$sonar_job" \
  && grep -qF 'sonar.working.directory=.scannerwork' <<<"$sonar_job" \
  && grep -qF 'scripts/check-duplication' <<<"$sonar_job"; then
  pass "the CI Sonar job keeps the report and reads it"
else
  fail "the CI Sonar job keeps the report and reads it"
fi

if git -C "$REPO_ROOT" grep -qE 'sonar-scanner-cli:latest' -- Makefile .github scripts ':!scripts/tests'; then
  fail "the scanner image is pinned, never latest"
else
  pass "the scanner image is pinned, never latest"
fi

for ignore in .gitignore .dockerignore; do
  if grep -qxF '.scannerwork/' "$REPO_ROOT/$ignore"; then
    pass "$ignore leaves the kept report out"
  else
    fail "$ignore leaves the kept report out"
  fi
done

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
