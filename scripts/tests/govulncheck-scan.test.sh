#!/usr/bin/env bash
# Tests scripts/govulncheck-scan.sh: the database fetch retries, and the scan runs once locally.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
SCAN="$ROOT/scripts/govulncheck-scan.sh"

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
mkdir -p "$WORK/bin"

python3 - "$WORK" <<'PY'
import sys, zipfile
work = sys.argv[1]
with zipfile.ZipFile(f"{work}/good.zip", "w") as z:
    z.writestr("index/db.json", '{"modified":"2026-09-28T00:00:00Z"}')
    z.writestr("index/modules.json", "[]")
with zipfile.ZipFile(f"{work}/bad.zip", "w") as z:
    z.writestr("README", "not a database")
PY

cat >"$WORK/bin/curl" <<'CURL'
#!/usr/bin/env bash
out=""
while [ "$#" -gt 0 ]; do
  [ "$1" = "-o" ] && out="$2"
  shift
done
[ -n "${FAKE_DB:-}" ] || { echo "curl: (22) The requested URL returned error: 503" >&2; exit 22; }
cp "$FAKE_DB" "$out"
CURL
cat >"$WORK/bin/govulncheck" <<'SCANNER'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$FAKE_CALLS"
exit "${FAKE_SCAN_RC:-0}"
SCANNER
chmod +x "$WORK/bin/curl" "$WORK/bin/govulncheck"

run_scan() { # db archive (or empty), scanner rc
  : >"$WORK/calls"
  PATH="$WORK/bin:$PATH" FAKE_DB="$1" FAKE_SCAN_RC="$2" FAKE_CALLS="$WORK/calls" \
    bash "$SCAN" >"$WORK/out" 2>&1
}

echo "govulncheck-scan:"

if run_scan "$WORK/good.zip" 0; then
  pass "a clean scan passes"
else
  fail "a clean scan passes (out=[$(cat "$WORK/out")])"
fi
if grep -qE '^-db file://[^ ]+ \./\.\.\.$' "$WORK/calls"; then
  pass "and scans against the local copy of the database"
else
  fail "and scans against the local copy of the database (calls=[$(cat "$WORK/calls")])"
fi

if run_scan "$WORK/good.zip" 3; then
  fail "a scan that fails fails the script"
else
  pass "a scan that fails fails the script"
fi
calls="$(wc -l <"$WORK/calls")"
if [ "$calls" -eq 1 ]; then
  pass "after exactly one scan"
else
  fail "after exactly one scan (scanned $calls times)"
fi

if run_scan "" 0; then
  fail "an unreachable database fails the script"
elif [ -s "$WORK/calls" ]; then
  fail "an unreachable database fails before any scan (it scanned)"
else
  pass "an unreachable database fails before any scan"
fi

if run_scan "$WORK/bad.zip" 0; then
  fail "an archive that is not a database is refused"
elif grep -qF 'index/db.json' "$WORK/out" && [ ! -s "$WORK/calls" ]; then
  pass "an archive that is not a database is refused, naming what it lacks"
else
  fail "an archive that is not a database is refused, naming what it lacks (out=[$(cat "$WORK/out")])"
fi

if grep -qF 'scripts/govulncheck-scan.sh' "$ROOT/scripts/precommit-gauntlet.sh"; then
  pass "the gauntlet runs the script"
else
  fail "the gauntlet runs the script"
fi
security_audit="$(awk '/^  security-audit:/ { on = 1; next } on && /^  [A-Za-z0-9_-]+:[[:space:]]*$/ { on = 0 } on' "$ROOT/.github/workflows/ci.yml")"
if [ -z "$security_audit" ]; then
  fail "CI's security-audit job was found"
elif grep -qF 'scripts/govulncheck-scan.sh' <<<"$security_audit" \
  && ! grep -qE '(^|[^A-Za-z0-9_-])govulncheck \./\.\.\.' <<<"$security_audit"; then
  pass "CI's security audit runs the script, and no scan of its own"
else
  fail "CI's security audit runs the script, and no scan of its own"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
