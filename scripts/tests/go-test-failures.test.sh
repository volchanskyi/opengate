#!/usr/bin/env bash
# Tests for scripts/go-test-failures.sh over the stream a real go test -json writes for a throwaway
# module: a failing test and subtest, a package that times out, one that does not build.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
LISTER="$REPO_ROOT/scripts/go-test-failures.sh"
[ -x "$LISTER" ] || {
  echo "FAIL: $LISTER not executable" >&2
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

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
MOD="$WORK/mod"
mkdir -p "$MOD/passes" "$MOD/fails" "$MOD/slow" "$MOD/broken"
printf 'module example.test/m\n\ngo 1.21\n' >"$MOD/go.mod"
cat >"$MOD/passes/passes_test.go" <<'GO'
package passes

import "testing"

func TestPasses(t *testing.T) { t.Log("quiet line of a passing test") }
GO
cat >"$MOD/fails/fails_test.go" <<'GO'
package fails

import "testing"

func TestPasses(t *testing.T) {}

func TestFails(t *testing.T) {
	t.Run("sub", func(t *testing.T) { t.Error("the subtest's own message") })
	t.Error("the parent's own message")
}
GO
cat >"$MOD/slow/slow_test.go" <<'GO'
package slow

import (
	"testing"
	"time"
)

func TestQuick(t *testing.T) {}

func TestOutlivesTimeout(t *testing.T) { time.Sleep(time.Minute) }
GO
cat >"$MOD/broken/broken_test.go" <<'GO'
package broken

import "testing"

func TestNeverBuilds(t *testing.T) { undefinedHelper() }
GO

# go test exits 1 here by design; its stream is what the lister reads.
(cd "$MOD" && GOWORK=off GOTOOLCHAIN=local GOFLAGS='' go test -json -timeout 5s ./... >"$WORK/results.json" 2>"$WORK/stderr.log") || true
(cd "$MOD" && GOWORK=off GOTOOLCHAIN=local GOFLAGS='' go test -json ./passes >"$WORK/clean.json" 2>/dev/null)

echo "go-test-failures:"

status=0
"$LISTER" "$WORK/results.json" >"$WORK/out" 2>&1 || status=$?
out_has() { grep -qF -- "$1" "$WORK/out"; }
if [ "$status" -eq 0 ]; then
  pass "listing failures is a report, never a verdict of its own"
else
  fail "the lister must exit 0 over a stream with failures (status=$status)"
fi
if out_has '::error title=Go test failed::example.test/m/fails TestFails' \
  && out_has 'the parent'"'"'s own message'; then
  pass "a failed test is named as an error and its output follows"
else
  fail "a failed test must be named with its output (out=[$(cat "$WORK/out")])"
fi
if out_has '::error title=Go test failed::example.test/m/fails TestFails/sub' \
  && out_has 'the subtest'"'"'s own message'; then
  pass "a failed subtest is named and its output follows"
else
  fail "a failed subtest must be named with its output"
fi
if out_has '::error title=Go test failed::example.test/m/slow, the package itself (still running: TestOutlivesTimeout)' \
  && out_has 'panic: test timed out after 5s'; then
  pass "a package that fails with no test failing, as a timeout does, is named with its panic and its running test"
else
  fail "a package-level failure must be named with its output (out=[$(cat "$WORK/out")])"
fi
if out_has 'example.test/m/fails, the package itself'; then
  fail "a package whose tests failed by name is listed by those tests alone"
else
  pass "a package whose tests failed by name is listed by those tests alone"
fi
if out_has '::error title=Go test failed::example.test/m/broken, the package itself' \
  && out_has 'undefinedHelper'; then
  pass "a package that does not build is named with the compiler's message"
else
  fail "a build failure must be named with the compiler's message (out=[$(cat "$WORK/out")])"
fi
if out_has 'quiet line of a passing test' || out_has 'example.test/m/passes'; then
  fail "a passing package and its output stay out of the listing"
else
  pass "a passing package and its output stay out of the listing"
fi
if [ "$(grep -c '^::group::' "$WORK/out")" = "$(grep -c '^::endgroup::' "$WORK/out")" ]; then
  pass "every failure's output sits in a closed group"
else
  fail "every ::group:: must be closed"
fi

status=0
"$LISTER" "$WORK/clean.json" >"$WORK/out" 2>&1 || status=$?
if [ "$status" -eq 0 ] && [ ! -s "$WORK/out" ]; then
  pass "a stream with no failure lists nothing"
else
  fail "a clean stream must list nothing (status=$status out=[$(cat "$WORK/out")])"
fi

status=0
"$LISTER" >/dev/null 2>&1 || status=$?
if [ "$status" -eq 2 ]; then
  pass "no results file is a usage error"
else
  fail "no results file must exit 2 (status=$status)"
fi

for results in unit-results.json integration-results.json; do
  if grep -qE "scripts/go-test-failures\.sh \"?$results\"?" "$REPO_ROOT/.github/workflows/ci.yml"; then
    pass "ci.yml lists the failures in $results into the job log"
  else
    fail "ci.yml must run scripts/go-test-failures.sh on $results"
  fi
done

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
