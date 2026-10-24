#!/usr/bin/env bash
# Tests the lockfile readers of scripts/dependency-fixes against reports shaped as npm audit,
# cargo audit and govulncheck write them: a patch fix refuses, no fix passes, silence is unreadable.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
PACKAGE="$ROOT/scripts/dependency-fixes"
FAKES="$SCRIPT_DIR/fixtures/dependency-fixes"

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
mkdir -p "$REPO/scripts" "$REPO/web" "$REPO/agent" "$REPO/server" "$WORK/bin" "$WORK/state" \
  "$HTTP/api.osv.dev/v1/vulns" "$HTTP/vuln.go.dev"
ln -s "$PACKAGE" "$REPO/scripts/dependency-fixes"
ln -s "$ROOT/scripts/govulncheck-scan.sh" "$REPO/scripts/govulncheck-scan.sh"
ln -s "$FAKES/fake-curl.py" "$WORK/bin/curl"
for id in GHSA-mrhx-c7xc-m3w5 RUSTSEC-2021-0003; do
  cp "$FAKES/osv-$id.json" "$HTTP/api.osv.dev/v1/vulns/$id"
done
echo '{}' >"$HTTP/osv-matches.json"
python3 - "$HTTP/vuln.go.dev/vulndb.zip" <<'PY'
import sys, zipfile
with zipfile.ZipFile(sys.argv[1], "w") as archive:
    archive.writestr("index/db.json", '{"modified":"2026-10-23T00:00:00Z"}')
PY

cat >"$REPO/web/package-lock.json" <<'JSON'
{"name": "web", "lockfileVersion": 3, "packages": {"": {"name": "web"},
 "node_modules/zod": {"version": "4.6.5"}, "node_modules/react": {"version": "19.2.0"}}}
JSON
echo '{"name": "web"}' >"$REPO/web/package.json"
printf '[[package]]\nname = "smallvec"\nversion = "1.6.0"\n' >"$REPO/agent/Cargo.lock"
printf '[package]\nname = "agent"\n' >"$REPO/agent/Cargo.toml"
printf 'module example.test/server\n' >"$REPO/server/go.mod"
git -C "$REPO" init -q && git -C "$REPO" add -A

# The npm audit report as npm 11 writes it for the lockfile above, the advisory published today.
cat >"$WORK/zod.json" <<'JSON'
{"auditReportVersion": 2, "vulnerabilities": {"zod": {"name": "zod", "severity": "high", "isDirect": false,
 "via": [{"source": 1243066, "name": "zod", "dependency": "zod",
   "title": "Zod can exhaust memory while validating oversized constrained collections",
   "url": "https://github.com/advisories/GHSA-mrhx-c7xc-m3w5", "severity": "high", "range": "<4.6.6"}],
 "effects": [], "range": "<4.6.6", "nodes": ["node_modules/zod"], "fixAvailable": true}},
 "metadata": {"vulnerabilities": {"high": 1, "total": 1}}}
JSON
echo '{"auditReportVersion": 2, "vulnerabilities": {}, "metadata": {"vulnerabilities": {"total": 0}}}' >"$WORK/clean.json"
echo '{"error": {"code": "ENOTFOUND", "summary": "request to registry.npmjs.org failed"}}' >"$WORK/error.json"
cat >"$WORK/bin/npm" <<'NPM'
#!/usr/bin/env bash
[ "$1" = audit ] || exit 64
n=$(($(cat "$FAKE_STATE/npm" 2>/dev/null || echo 0) + 1))
echo "$n" >"$FAKE_STATE/npm"
if [ "$n" -le "${FAKE_NPM_FAILS:-0}" ]; then cat "$FAKE_ERROR"; else cat "$FAKE_NPM"; fi
exit 1
NPM
# The cargo audit report as cargo-audit 0.22 writes it, trimmed to the fields the reader reads.
cat >"$WORK/smallvec.json" <<'JSON'
{"database": {}, "lockfile": {"dependency-count": 1}, "settings": {}, "warnings": {},
 "vulnerabilities": {"found": true, "count": 1, "list": [{
   "advisory": {"id": "RUSTSEC-2021-0003", "package": "smallvec", "title": "Buffer overflow in SmallVec::insert_many",
     "aliases": ["CVE-2021-25900", "GHSA-43w2-9j62-hq99"]},
   "versions": {"patched": [">=0.6.14, <1.0.0", ">=1.6.1"], "unaffected": ["<0.6.3"]},
   "package": {"name": "smallvec", "version": "1.6.0",
     "source": "registry+https://github.com/rust-lang/crates.io-index"}}]}}
JSON
cat >"$WORK/bin/cargo" <<'CARGO'
#!/usr/bin/env bash
case "$1" in
  locate-project) printf '%s\n' "${@: -1}" ;;
  audit) cat "$FAKE_CARGO"; exit 1 ;;
  generate-lockfile) exit 0 ;;
  *) exit 64 ;;
esac
CARGO
# govulncheck 1.8's stream: a stdlib hole with a fixed release, and a module hole with none.
cat >"$WORK/govuln.json" <<'JSON'
{"config": {"protocol_version": "v1.0.0", "scanner_name": "govulncheck", "scan_level": "package"}}
{"SBOM": {"go_version": "go1.27.3", "modules": [{"path": "example.test/server"},
  {"path": "golang.org/x/crypto", "version": "v0.58.0"}]}}
{"finding": {"osv": "GO-2026-6001", "fixed_version": "v1.27.4", "trace": [{"module": "stdlib", "version": "v1.27.3"}]}}
{"finding": {"osv": "GO-2026-5932", "trace": [{"module": "golang.org/x/crypto", "version": "v0.58.0"}]}}
JSON
cat >"$WORK/bin/govulncheck" <<'GOVULN'
#!/usr/bin/env bash
cat "$FAKE_GOVULN"
GOVULN
cat >"$WORK/bin/sleep" <<'SLEEP'
#!/usr/bin/env bash
echo "$1" >>"$FAKE_STATE/sleeps"
SLEEP
chmod +x "$WORK/bin/"*

run() { # reader, then VAR=value overrides for the stand-ins
  local reader="$1"
  shift
  rm -f "$WORK/state/"*
  env PATH="$WORK/bin:$PATH" FAKE_HTTP_DIR="$HTTP" FAKE_HTTP_LOG="$WORK/http.log" FAKE_STATE="$WORK/state" \
    FAKE_NPM="$WORK/zod.json" FAKE_ERROR="$WORK/error.json" FAKE_CARGO="$WORK/smallvec.json" \
    FAKE_GOVULN="$WORK/govuln.json" XDG_CACHE_HOME="$WORK/cache" "$@" \
    python3 "$REPO/scripts/dependency-fixes" "$reader" >"$WORK/out" 2>&1
}
out_has() { grep -qF -- "$1" "$WORK/out"; }

echo "dependency-fixes lockfiles:"

status=0
run npm || status=$?
if [ "$status" -eq 1 ] && out_has 'npm (web) zod 4.6.5: GHSA-mrhx-c7xc-m3w5 fixed in 4.6.6' \
  && out_has 'take it: cd web && npm update zod --package-lock-only'; then
  pass "an npm advisory a patch fixes refuses the commit and names the update that takes it"
else
  fail "an npm advisory a patch fixes must refuse the commit (status=$status out=[$(cat "$WORK/out")])"
fi

status=0
run npm FAKE_NPM="$WORK/clean.json" || status=$?
if [ "$status" -eq 0 ] && out_has 'read 2 npm packages'; then
  pass "a clean audit passes and counts the packages the lockfile holds"
else
  fail "a clean audit must pass, counting what it read (status=$status out=[$(cat "$WORK/out")])"
fi

status=0
run npm FAKE_NPM_FAILS=1 FAKE_NPM="$WORK/clean.json" || status=$?
if [ "$status" -eq 0 ] && [ "$(cat "$WORK/state/npm")" = 2 ]; then
  pass "an audit the registry did not answer is asked again"
else
  fail "an unanswered audit must be retried (status=$status out=[$(cat "$WORK/out")])"
fi

status=0
run npm FAKE_NPM_FAILS=9 || status=$?
if [ "$status" -eq 2 ] && out_has 'stayed unreadable'; then
  pass "an audit that never answers fails the check as unreadable"
else
  fail "an audit that never answers must exit 2 (status=$status out=[$(cat "$WORK/out")])"
fi

status=0
run rust || status=$?
if [ "$status" -eq 1 ] && out_has 'rust (agent) smallvec 1.6.0: RUSTSEC-2021-0003 fixed in 1.6.1' \
  && out_has 'cargo update -p smallvec --precise 1.6.1'; then
  pass "a crate advisory fixed in its own line refuses the commit, the line read from the record"
else
  fail "a crate a patch fixes must refuse the commit (status=$status out=[$(cat "$WORK/out")])"
fi

status=0
run go || status=$?
if [ "$status" -eq 1 ] && out_has 'go (server) stdlib 1.27.3: GO-2026-6001 fixed in 1.27.4'; then
  pass "a standard library hole a patch fixes refuses the commit"
else
  fail "a standard library patch must refuse the commit (status=$status out=[$(cat "$WORK/out")])"
fi
if out_has '1 advisories have no fixed release' && ! out_has 'GO-2026-5932'; then
  pass "a module hole no release fixes passes, counted and unlisted"
else
  fail "a hole with no fix must pass unlisted (out=[$(cat "$WORK/out")])"
fi

printf '{"config": {}}\n' >"$WORK/silent.json"
status=0
run go FAKE_GOVULN="$WORK/silent.json" || status=$?
if [ "$status" -eq 2 ] && out_has 'lists no modules'; then
  pass "a scan that names no modules is unreadable, never clean"
else
  fail "a scan with no module list must exit 2 (status=$status out=[$(cat "$WORK/out")])"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
