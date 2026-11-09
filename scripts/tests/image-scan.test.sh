#!/usr/bin/env bash
# Tests scripts/image-scan.sh against a stand-in for Trivy's JSON report: a serious hole with a fix
# refuses, an exception passes one, and a scan that cannot be read is never a pass.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

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
HTTP="$WORK/http"
mkdir -p "$REPO/scripts/lib" "$WORK/bin" "$HTTP/proxy.golang.org/golang.org/x/net/@v"
ln -s "$SCRIPT_DIR/fixtures/dependency-fixes/fake-curl.py" "$WORK/bin/curl"
ln -s "$ROOT/scripts/image-scan.sh" "$REPO/scripts/image-scan.sh"
ln -s "$ROOT/scripts/require-tool.sh" "$REPO/scripts/require-tool.sh"
ln -s "$ROOT/scripts/lib/tool-versions.sh" "$REPO/scripts/lib/tool-versions.sh"
ln -s "$ROOT/scripts/dependency-fixes" "$REPO/scripts/dependency-fixes"
echo '[]' >"$REPO/scripts/lib/vulnerability-exceptions.json"
# shellcheck source=../lib/tool-versions.sh
. "$ROOT/scripts/lib/tool-versions.sh"

# Trivy answers --version as the pinned release, writes FAKE_TRIVY_REPORT where --output names,
# and, when FAKE_TRIVY_FAILS is set, fails that many times first as a database download does.
cat >"$WORK/bin/trivy" <<'TRIVY'
#!/usr/bin/env bash
if [ "${1:-}" = "--version" ]; then
  printf 'Version: %s\nVulnerability DB:\n' "$FAKE_TRIVY_VERSION"
  exit 0
fi
printf '%s\n' "$*" >>"$FAKE_TRIVY_LOG"
failures="$(cat "$FAKE_TRIVY_STATE" 2>/dev/null || echo 0)"
if [ "$failures" -lt "${FAKE_TRIVY_FAILS:-0}" ]; then
  echo $((failures + 1)) >"$FAKE_TRIVY_STATE"
  echo "FATAL init error: DB error: failed to download vulnerability DB" >&2
  exit 1
fi
args=("$@")
for i in "${!args[@]}"; do
  if [ "${args[$i]}" = "--output" ]; then
    cp "$FAKE_TRIVY_REPORT" "${args[$((i + 1))]}"
  fi
done
TRIVY
cat >"$WORK/bin/sleep" <<'SLEEP'
#!/usr/bin/env bash
exit 0
SLEEP
chmod +x "$WORK/bin/"*

cat >"$WORK/serious.json" <<'JSON'
{"SchemaVersion": 2, "ArtifactName": "opengate-e2e-server:local", "Results": [
 {"Target": "app/meshserver", "Class": "lang-pkgs", "Type": "gobinary", "Vulnerabilities": [
  {"VulnerabilityID": "CVE-2026-78661", "PkgName": "golang.org/x/net", "InstalledVersion": "v0.61.0",
   "FixedVersion": "0.62.0", "Severity": "HIGH", "Title": "golang.org/x/net/html: quadratic parsing"}]}]}
JSON
cat >"$WORK/clean.json" <<'JSON'
{"SchemaVersion": 2, "ArtifactName": "opengate-e2e-server:local", "Results": [
 {"Target": "app/meshserver", "Class": "lang-pkgs", "Type": "gobinary"}]}
JSON
printf 'not a report' >"$WORK/garbage.json"

scan() { # report, then arguments for the scan
  local report="$1"
  shift
  rm -f "$WORK/trivy.log" "$WORK/trivy.state"
  env PATH="$WORK/bin:$PATH" FAKE_TRIVY_VERSION="$TOOL_VERSION_TRIVY" FAKE_TRIVY_REPORT="$report" \
    FAKE_HTTP_DIR="$HTTP" FAKE_HTTP_LOG="$WORK/http.log" \
    FAKE_TRIVY_LOG="$WORK/trivy.log" FAKE_TRIVY_STATE="$WORK/trivy.state" XDG_CACHE_HOME="$WORK/cache" \
    "$REPO/scripts/image-scan.sh" "$@" >"$WORK/out" 2>&1
}
out_has() { grep -qF -- "$1" "$WORK/out"; }

echo "image scan:"

status=0
scan "$WORK/serious.json" opengate-e2e-server:local || status=$?
if [ "$status" -eq 1 ] && out_has 'CVE-2026-78661' && out_has 'golang.org/x/net' && out_has 'fixed in 0.62.0'; then
  pass "a high hole with a fixed release refuses the image and names its fix"
else
  fail "a serious hole must refuse the image (status=$status out=[$(cat "$WORK/out")])"
fi
asked="$(cat "$WORK/trivy.log")"
if grep -qF -- '--severity HIGH,CRITICAL' <<<"$asked" && grep -qF -- '--ignore-unfixed' <<<"$asked" \
  && grep -qF -- '--format json' <<<"$asked" && grep -qE -- ' opengate-e2e-server:local$' <<<"$asked"; then
  pass "the scanner is asked for high and critical holes with a fix, on the image named"
else
  fail "the scanner must be asked for serious holes with a fix (asked=[$asked])"
fi

status=0
scan "$WORK/clean.json" ghcr.io/volchanskyi/opengate-server@sha256:abc --platform linux/arm64 || status=$?
if [ "$status" -eq 0 ] && grep -qF -- '--platform linux/arm64' "$WORK/trivy.log"; then
  pass "a clean image passes, scanned for the platform asked"
else
  fail "a clean image must pass on its platform (status=$status out=[$(cat "$WORK/out")] asked=[$(cat "$WORK/trivy.log")])"
fi

printf '[{"advisory": "CVE-2026-78661", "package": "golang.org/x/net", "newest": "0.62.0",
  "review_by": "2999-01-01", "reason": "the server parses no HTML"}]\n' \
  >"$REPO/scripts/lib/vulnerability-exceptions.json"
printf 'v0.61.0\nv0.62.0\n' >"$HTTP/proxy.golang.org/golang.org/x/net/@v/list"
status=0
scan "$WORK/serious.json" opengate-e2e-server:local || status=$?
if [ "$status" -eq 0 ] && out_has 'excepted until 2999-01-01'; then
  pass "a live exception passes the hole it names, with its review date"
else
  fail "a live exception must pass its hole (status=$status out=[$(cat "$WORK/out")])"
fi
printf 'v0.61.0\nv0.62.0\nv0.63.0\n' >"$HTTP/proxy.golang.org/golang.org/x/net/@v/list"
status=0
scan "$WORK/serious.json" opengate-e2e-server:local || status=$?
if [ "$status" -eq 1 ] && out_has 'golang.org/x/net v0.63.0 is published'; then
  pass "an exception lapses once its package releases again"
else
  fail "a new release must lapse the exception (status=$status out=[$(cat "$WORK/out")])"
fi
echo '[]' >"$REPO/scripts/lib/vulnerability-exceptions.json"

status=0
scan "$WORK/garbage.json" opengate-e2e-server:local || status=$?
if [ "$status" -eq 2 ]; then
  pass "a report that does not read is unreadable, never clean"
else
  fail "an unreadable report must exit 2 (status=$status out=[$(cat "$WORK/out")])"
fi

status=0
FAKE_TRIVY_FAILS=2 scan "$WORK/clean.json" opengate-e2e-server:local || status=$?
if [ "$status" -eq 0 ] && [ "$(wc -l <"$WORK/trivy.log")" -eq 3 ]; then
  pass "a scan whose database download fails is tried again"
else
  fail "a failed download must be retried (status=$status calls=$(wc -l <"$WORK/trivy.log"))"
fi
status=0
FAKE_TRIVY_FAILS=9 scan "$WORK/clean.json" opengate-e2e-server:local || status=$?
if [ "$status" -eq 2 ] && out_has 'could not be scanned'; then
  pass "a scan that never runs is unreadable"
else
  fail "a scan that never runs must exit 2 (status=$status out=[$(cat "$WORK/out")])"
fi

status=0
env PATH="$WORK/bin:$PATH" "$REPO/scripts/image-scan.sh" >"$WORK/out" 2>&1 || status=$?
if [ "$status" -eq 2 ] && out_has 'Usage'; then
  pass "a scan named no image prints its usage"
else
  fail "a scan with no image must exit 2 with its usage (status=$status)"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
