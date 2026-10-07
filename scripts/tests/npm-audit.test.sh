#!/usr/bin/env bash
# Tests scripts/npm-audit.sh: a high advisory fails unless excepted, and an exception lapses
# on its review date, on a new release of its package, and once the audit stops reporting it.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
AUDIT="$ROOT/scripts/npm-audit.sh"
EXCEPTIONS="$ROOT/scripts/lib/npm-audit-exceptions.json"

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
REPO="$WORK/repo"
mkdir -p "$WORK/bin" "$WORK/state" "$REPO/scripts/lib" "$REPO/web" "$REPO/tools/docs"
ln -s "$AUDIT" "$REPO/scripts/npm-audit.sh"

cat >"$WORK/clean.json" <<'JSON'
{"auditReportVersion":2,"vulnerabilities":{},"metadata":{"vulnerabilities":{"info":0,"low":0,"moderate":0,"high":0,"critical":0,"total":0}}}
JSON
cat >"$WORK/braces.json" <<'JSON'
{"auditReportVersion":2,"vulnerabilities":{
 "braces":{"name":"braces","severity":"high","isDirect":false,"via":[{"source":1240992,"name":"braces","dependency":"braces","title":"braces vulnerable to stack-exhaustion denial of service through deeply nested patterns","url":"https://github.com/advisories/GHSA-vfj7-8cjw-p6xm","severity":"high","range":"<=3.0.3"}],"effects":["micromatch"],"range":"*","nodes":["node_modules/braces"]},
 "micromatch":{"name":"micromatch","severity":"high","isDirect":false,"via":["braces"],"effects":["fast-glob"],"range":">=0.2.0","nodes":["node_modules/micromatch"]}},
 "metadata":{"vulnerabilities":{"info":0,"low":0,"moderate":0,"high":2,"critical":0,"total":2}}}
JSON
cat >"$WORK/low.json" <<'JSON'
{"auditReportVersion":2,"vulnerabilities":{
 "dompurify":{"name":"dompurify","severity":"low","isDirect":false,"via":[{"source":1240621,"name":"dompurify","dependency":"dompurify","title":"DOMPurify node-removing hook","url":"https://github.com/advisories/GHSA-p98j-92pf-mc4p","severity":"low","range":"<=3.4.15"}],"effects":[],"range":"<=3.4.15","nodes":["node_modules/dompurify"]}},
 "metadata":{"vulnerabilities":{"info":0,"low":1,"moderate":0,"high":0,"critical":0,"total":1}}}
JSON
cat >"$WORK/critical.json" <<'JSON'
{"auditReportVersion":2,"vulnerabilities":{
 "tar":{"name":"tar","severity":"critical","isDirect":false,"via":[{"source":1240001,"name":"tar","dependency":"tar","title":"tar arbitrary file write","url":"https://github.com/advisories/GHSA-aaaa-bbbb-cccc","severity":"critical","range":"<7.0.0"}],"effects":[],"range":"<7.0.0","nodes":["node_modules/tar"]}},
 "metadata":{"vulnerabilities":{"info":0,"low":0,"moderate":0,"high":0,"critical":1,"total":1}}}
JSON
cat >"$WORK/error.json" <<'JSON'
{"error":{"code":"ENOTFOUND","summary":"request to https://registry.npmjs.org/-/npm/v1/security/advisories/bulk failed","detail":""}}
JSON

cat >"$WORK/bin/npm" <<'NPM'
#!/usr/bin/env bash
case "$1" in
  audit)
    n=$(($(cat "$FAKE_STATE/audits" 2>/dev/null || echo 0) + 1))
    echo "$n" >"$FAKE_STATE/audits"
    printf '%s\n' "$PWD" >>"$FAKE_STATE/audit-dirs"
    if [ "$n" -le "${FAKE_AUDIT_FAILS:-0}" ]; then
      cat "$FAKE_ERROR"
      exit 1
    fi
    cat "$FAKE_AUDIT"
    exit 1
    ;;
  view)
    if [ -n "${FAKE_VIEW_FAIL:-}" ]; then
      echo "npm error code ENOTFOUND" >&2
      exit 1
    fi
    printf '%s\n' "$2" >>"$FAKE_STATE/views"
    echo "$FAKE_LATEST"
    ;;
  *) exit 64 ;;
esac
NPM
cat >"$WORK/bin/date" <<'DATE'
#!/usr/bin/env bash
echo "$FAKE_TODAY"
DATE
cat >"$WORK/bin/sleep" <<'SLEEP'
#!/usr/bin/env bash
echo "$1" >>"$FAKE_STATE/sleeps"
SLEEP
chmod +x "$WORK/bin/npm" "$WORK/bin/date" "$WORK/bin/sleep"

write_exceptions() { # review_by, newest, dir
  cat >"$REPO/scripts/lib/npm-audit-exceptions.json" <<JSON
[
  {
    "dir": "${3:-web}",
    "advisory": "GHSA-vfj7-8cjw-p6xm",
    "package": "braces",
    "newest": "$2",
    "review_by": "$1",
    "reason": "No patched release exists; reached only by tools reading patterns in this repository."
  }
]
JSON
}

RC=0
run_audit() { # dir, audit report, then VAR=value overrides for the stand-ins
  local dir="$1" report="$2"
  shift 2
  rm -f "$WORK/state/"*
  RC=0
  env PATH="$WORK/bin:$PATH" FAKE_STATE="$WORK/state" FAKE_AUDIT="$report" \
    FAKE_ERROR="$WORK/error.json" FAKE_TODAY=2026-10-06 FAKE_LATEST=3.0.3 "$@" \
    bash "$REPO/scripts/npm-audit.sh" "$dir" >"$WORK/out" 2>&1 || RC=$?
}
out() { cat "$WORK/out"; }

echo "npm-audit:"

echo '[]' >"$REPO/scripts/lib/npm-audit-exceptions.json"
run_audit web "$WORK/clean.json"
if [ "$RC" -eq 0 ]; then
  pass "a clean audit passes"
else
  fail "a clean audit passes (rc=$RC out=[$(out)])"
fi
if grep -qxF "$REPO/web" "$WORK/state/audit-dirs"; then
  pass "and audits the lockfile of the directory it was given"
else
  fail "and audits the lockfile of the directory it was given (dirs=[$(cat "$WORK/state/audit-dirs")])"
fi

run_audit web "$WORK/low.json"
if [ "$RC" -eq 0 ]; then
  pass "an advisory below high passes"
else
  fail "an advisory below high passes (rc=$RC out=[$(out)])"
fi

run_audit web "$WORK/braces.json"
if [ "$RC" -eq 1 ] && grep -qF 'GHSA-vfj7-8cjw-p6xm' <<<"$(out)" && grep -qF 'braces' <<<"$(out)"; then
  pass "a high advisory with no exception fails, naming the advisory and its package"
else
  fail "a high advisory with no exception fails, naming the advisory and its package (rc=$RC out=[$(out)])"
fi

run_audit web "$WORK/critical.json"
if [ "$RC" -eq 1 ] && grep -qF 'GHSA-aaaa-bbbb-cccc' <<<"$(out)"; then
  pass "a critical advisory with no exception fails"
else
  fail "a critical advisory with no exception fails (rc=$RC out=[$(out)])"
fi

write_exceptions 2026-11-06 3.0.3
run_audit web "$WORK/braces.json"
if [ "$RC" -eq 0 ] && grep -qF 'until 2026-11-06' <<<"$(out)"; then
  pass "an excepted advisory passes and is printed with its review date"
else
  fail "an excepted advisory passes and is printed with its review date (rc=$RC out=[$(out)])"
fi
if grep -qxF 'braces' "$WORK/state/views"; then
  pass "and the package's newest release is read from the registry"
else
  fail "and the package's newest release is read from the registry"
fi

run_audit web "$WORK/braces.json" FAKE_TODAY=2026-11-06
if [ "$RC" -eq 0 ]; then
  pass "an exception holds through its review date"
else
  fail "an exception holds through its review date (rc=$RC out=[$(out)])"
fi

run_audit web "$WORK/braces.json" FAKE_TODAY=2026-11-07
if [ "$RC" -eq 1 ] && grep -qF '2026-11-06' <<<"$(out)"; then
  pass "an exception past its review date fails, naming the date"
else
  fail "an exception past its review date fails, naming the date (rc=$RC out=[$(out)])"
fi

run_audit web "$WORK/braces.json" FAKE_LATEST=3.0.4
if [ "$RC" -eq 1 ] && grep -qF '3.0.4' <<<"$(out)"; then
  pass "a new release of the excepted package fails, naming the release"
else
  fail "a new release of the excepted package fails, naming the release (rc=$RC out=[$(out)])"
fi

run_audit web "$WORK/clean.json"
if [ "$RC" -eq 1 ] && grep -qF 'GHSA-vfj7-8cjw-p6xm' <<<"$(out)"; then
  pass "an exception the audit no longer reports fails, naming the advisory"
else
  fail "an exception the audit no longer reports fails, naming the advisory (rc=$RC out=[$(out)])"
fi

run_audit tools/docs "$WORK/braces.json"
if [ "$RC" -eq 1 ]; then
  pass "an exception covers only the directory it names"
else
  fail "an exception covers only the directory it names (rc=$RC out=[$(out)])"
fi

jq -s '.[0] * .[1]' "$WORK/braces.json" "$WORK/critical.json" >"$WORK/both.json"
run_audit web "$WORK/both.json"
if [ "$RC" -eq 1 ] && grep -qF 'GHSA-aaaa-bbbb-cccc' <<<"$(out)" \
  && ! grep -qF 'no longer reports' <<<"$(out)"; then
  pass "an exception for one advisory leaves every other advisory failing"
else
  fail "an exception for one advisory leaves every other advisory failing (rc=$RC out=[$(out)])"
fi

run_audit web "$WORK/braces.json" FAKE_VIEW_FAIL=1
if [ "$RC" -eq 2 ]; then
  pass "a registry that cannot be read is a missing prerequisite"
else
  fail "a registry that cannot be read is a missing prerequisite (rc=$RC out=[$(out)])"
fi

echo '[]' >"$REPO/scripts/lib/npm-audit-exceptions.json"
run_audit web "$WORK/clean.json" FAKE_AUDIT_FAILS=2
audits="$(cat "$WORK/state/audits")"
if [ "$RC" -eq 0 ] && [ "$audits" -eq 3 ] && [ -s "$WORK/state/sleeps" ]; then
  pass "an audit that could not be read is retried after a pause"
else
  fail "an audit that could not be read is retried after a pause (rc=$RC audits=$audits out=[$(out)])"
fi

run_audit web "$WORK/clean.json" FAKE_AUDIT_FAILS=99
if [ "$RC" -eq 2 ] && grep -qF 'ENOTFOUND' <<<"$(out)"; then
  pass "an audit unreadable on every attempt is a missing prerequisite, with npm's reason"
else
  fail "an audit unreadable on every attempt is a missing prerequisite, with npm's reason (rc=$RC out=[$(out)])"
fi

run_audit web "$WORK/braces.json" FAKE_AUDIT_FAILS=0
audits="$(cat "$WORK/state/audits")"
if [ "$RC" -eq 1 ] && [ "$audits" -eq 1 ]; then
  pass "an audit that reports a finding runs once"
else
  fail "an audit that reports a finding runs once (rc=$RC audits=$audits)"
fi

cat >"$REPO/scripts/lib/npm-audit-exceptions.json" <<'JSON'
[{"dir": "web", "advisory": "GHSA-vfj7-8cjw-p6xm", "package": "braces", "newest": "3.0.3", "review_by": "2026-11-06"}]
JSON
run_audit web "$WORK/braces.json"
if [ "$RC" -eq 2 ] && grep -qF 'reason' <<<"$(out)"; then
  pass "an exception with no reason is refused, naming the missing field"
else
  fail "an exception with no reason is refused, naming the missing field (rc=$RC out=[$(out)])"
fi

write_exceptions 06/11/2026 3.0.3
run_audit web "$WORK/braces.json"
if [ "$RC" -eq 2 ] && grep -qF 'review_by' <<<"$(out)"; then
  pass "a review date not written as YYYY-MM-DD is refused"
else
  fail "a review date not written as YYYY-MM-DD is refused (rc=$RC out=[$(out)])"
fi

if jq -e 'type == "array"' "$EXCEPTIONS" >/dev/null 2>&1; then
  pass "the shipped exception list is a JSON array"
else
  fail "the shipped exception list is a JSON array"
fi
unknown_dirs=""
while IFS= read -r dir; do
  [ -f "$ROOT/$dir/package-lock.json" ] || unknown_dirs+="$dir "
done < <(jq -r '.[].dir' "$EXCEPTIONS" 2>/dev/null)
if [ -z "$unknown_dirs" ]; then
  pass "every shipped exception names a directory with a lockfile"
else
  fail "every shipped exception names a directory with a lockfile (unknown: $unknown_dirs)"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
