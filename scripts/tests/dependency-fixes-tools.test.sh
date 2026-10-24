#!/usr/bin/env bash
# Tests the pinned-tools reader of scripts/dependency-fixes: each source kind's own advisories, and
# what each tool is built from, answered by whether a later release of the tool still carries it.
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
VULNS="$HTTP/api.osv.dev/v1/vulns"
GOTOOL="$HTTP/proxy.golang.org/example.test/gotool/@v"
mkdir -p "$REPO/scripts/lib" "$WORK/bin" "$WORK/gh/repos/jqlang/jq" "$VULNS" "$GOTOOL" \
  "$HTTP/index.crates.io/cr/at" "$HTTP/static.crates.io/crates/cratetool" "$HTTP/registry.npmjs.org"
ln -s "$PACKAGE" "$REPO/scripts/dependency-fixes"
ln -s "$FAKES/fake-curl.py" "$WORK/bin/curl"
ln -s "$FAKES/fake-gh.py" "$WORK/bin/gh"
cat >"$REPO/scripts/lib/tool-versions.sh" <<'MANIFEST'
export TOOL_VERSION_GOTOOL="1.2.3"
export TOOL_VERSION_JQ="1.7.1"
export TOOL_VERSION_PYTOOL="2.0.0"
export TOOL_VERSION_CRATETOOL="0.9.0"
export TOOL_VERSION_LHCI="0.14.0"
export TOOL_VERSION_SCANNER_IMAGE="1.0.0"
export TOOL_SOURCE_GOTOOL="Go:example.test/gotool"
export TOOL_SOURCE_JQ="git:jqlang/jq@jq-{v}"
export TOOL_SOURCE_PYTOOL="PyPI:pytool"
export TOOL_SOURCE_CRATETOOL="crates.io:cratetool"
export TOOL_SOURCE_LHCI="npm:@lhci/cli"
export TOOL_SOURCE_SCANNER_IMAGE="image:docker.io/example/scanner"
MANIFEST
git -C "$REPO" init -q && git -C "$REPO" add -A

# advisory ID ECOSYSTEM NAME FIXED writes an OSV record whose one range ends at FIXED.
advisory() {
  printf '{"id": "%s", "summary": "%s", "affected": [{"package": {"ecosystem": "%s", "name": "%s"},
    "ranges": [{"type": "ECOSYSTEM", "events": [{"introduced": "0"}, {"fixed": "%s"}]}]}]}\n' \
    "$1" "a hole in $3" "$2" "$3" "$4" >"$VULNS/$1"
}

# A Go tool: its patch release builds with the fixed dependency.
printf 'v1.2.3\nv1.2.5\nv1.3.0\n' >"$GOTOOL/list"
printf 'module example.test/gotool\n\nrequire (\n\texample.test/dep v0.4.0 // indirect\n)\n' >"$GOTOOL/v1.2.3.mod"
printf 'module example.test/gotool\n\nrequire example.test/dep v0.4.1\n' >"$GOTOOL/v1.2.5.mod"
printf 'module example.test/gotool\n\nrequire example.test/dep v0.4.1\n' >"$GOTOOL/v1.3.0.mod"
advisory GO-2026-1000 Go example.test/dep 0.4.1
# A tagged source release: only the next minor drops the hole its commit carries.
printf '[{"name": "jq-1.8.2", "commit": {"sha": "bbbb"}}, {"name": "jq-1.7.1", "commit": {"sha": "aaaa"}}]\n' \
  >"$WORK/gh/repos/jqlang/jq/tags%3Fper_page=100"
printf '{"id": "CVE-2026-39979", "summary": "jq reads out of bounds", "affected": []}\n' >"$VULNS/CVE-2026-39979"
# A Python tool's environment: urllib3 is affected, and the tool's requirement admits the fix.
python3 -m venv --system-site-packages --without-pip "$WORK/venv"
SITE="$("$WORK/venv/bin/python" -c 'import sysconfig; print(sysconfig.get_paths()["purelib"])')"
mkdir -p "$SITE/pytool-2.0.0.dist-info" "$SITE/urllib3-2.2.0.dist-info"
printf 'Metadata-Version: 2.1\nName: pytool\nVersion: 2.0.0\nRequires-Dist: urllib3>=2.0\n' \
  >"$SITE/pytool-2.0.0.dist-info/METADATA"
printf 'Metadata-Version: 2.1\nName: urllib3\nVersion: 2.2.0\n' >"$SITE/urllib3-2.2.0.dist-info/METADATA"
printf '#!%s\n' "$WORK/venv/bin/python" >"$WORK/bin/pytool"
advisory GHSA-u111-1111-1111 PyPI urllib3 2.2.3
# A crate tool: its published lockfile pins a crate its patch release moves past.
printf '{"name":"cratetool","vers":"0.9.0"}\n{"name":"cratetool","vers":"0.9.1"}\n' >"$HTTP/index.crates.io/cr/at/cratetool"
python3 - "$HTTP/static.crates.io/crates/cratetool" <<'PY'
import io, sys, tarfile
for version, dep in (("0.9.0", "1.0.0"), ("0.9.1", "1.0.1")):
    lock = (f'[[package]]\nname = "cratetool"\nversion = "{version}"\n\n[[package]]\nname = "depcrate"\n'
            f'version = "{dep}"\nsource = "registry+https://github.com/rust-lang/crates.io-index"\n').encode()
    with tarfile.open(f"{sys.argv[1]}/cratetool-{version}.crate", "w:gz") as tar:
        entry = tarfile.TarInfo(f"cratetool-{version}/Cargo.lock")
        entry.size = len(lock)
        tar.addfile(entry, io.BytesIO(lock))
PY
advisory RUSTSEC-2026-1000 crates.io depcrate 1.0.1
# An npm tool: an install today resolves an affected cookie, which only its next minor drops.
printf '{"versions": {"0.14.0": {}, "0.15.1": {}}}\n' >"$HTTP/registry.npmjs.org/@lhci%2fcli"
cat >"$WORK/bin/npm" <<'NPM'
#!/usr/bin/env bash
case "$1" in
  install)
    cookie=0.4.2
    grep -qF '"0.15.1"' package.json && cookie=0.7.0
    printf '{"packages": {"": {}, "node_modules/cookie": {"version": "%s"}}}\n' "$cookie" >package-lock.json
    ;;
  audit)
    if grep -qF '"0.4.2"' package-lock.json; then
      printf '{"vulnerabilities": {"cookie": {"name": "cookie", "via": [{"name": "cookie",
        "url": "https://github.com/advisories/GHSA-pxg6-pf52-xh8x"}], "nodes": ["node_modules/cookie"]}}}\n'
    else
      printf '{"vulnerabilities": {}}\n'
    fi
    exit 1
    ;;
esac
NPM
advisory GHSA-pxg6-pf52-xh8x npm cookie 0.7.0
chmod +x "$WORK/bin/"*
cat >"$HTTP/osv-matches.json" <<'JSON'
{"Go|example.test/dep|0.4.0": ["GO-2026-1000"], "commit|aaaa": ["CVE-2026-39979"],
 "PyPI|urllib3|2.2.0": ["GHSA-u111-1111-1111"], "crates.io|depcrate|1.0.0": ["RUSTSEC-2026-1000"]}
JSON

status=0
env PATH="$WORK/bin:$PATH" FAKE_HTTP_DIR="$HTTP" FAKE_HTTP_LOG="$WORK/http.log" FAKE_GH_DIR="$WORK/gh" \
  XDG_CACHE_HOME="$WORK/cache" python3 "$REPO/scripts/dependency-fixes" tools >"$WORK/out" 2>&1 || status=$?
out_has() { grep -qF -- "$1" "$WORK/out"; }
patch_list="$(sed -n '/patch release of the line/,/newer minor or major/p' "$WORK/out")"
newer_list="$(sed -n '/newer minor or major/,$p' "$WORK/out")"

echo "dependency-fixes tools:"

if [ "$status" -eq 1 ]; then
  pass "a tool built from a hole its own patch release drops refuses the commit"
else
  fail "the run must refuse the commit (status=$status out=[$(cat "$WORK/out")])"
fi
if grep -qF 'built from (gotool 1.2.3) example.test/dep v0.4.0: GO-2026-1000 fixed in gotool v1.2.5' <<<"$patch_list" \
  && grep -qF 'move TOOL_VERSION_GOTOOL' <<<"$patch_list"; then
  pass "a Go tool's module file is read, and its newest patch release is the fix"
else
  fail "the Go tool's patch release must be named the fix (out=[$(cat "$WORK/out")])"
fi
if grep -qF 'tool (jq) jqlang/jq 1.7.1: CVE-2026-39979 fixed in 1.8.2' <<<"$newer_list"; then
  pass "a tagged source release's hole only the next minor drops is listed for Ivan, not refused"
else
  fail "the jq hole must be listed for Ivan (out=[$(cat "$WORK/out")])"
fi
if grep -qF 'reinstall pytool so its environment takes urllib3 2.2.3' <<<"$patch_list"; then
  pass "a Python tool's environment is read, and a patch its requirement admits is a reinstall"
else
  fail "the Python environment's patch must be named (out=[$(cat "$WORK/out")])"
fi
if grep -qF 'built from (cratetool 0.9.0) depcrate 1.0.0: RUSTSEC-2026-1000 fixed in cratetool 0.9.1' <<<"$patch_list"; then
  pass "a crate tool's published lockfile is read, and its patch release is the fix"
else
  fail "the crate tool's patch release must be named the fix (out=[$(cat "$WORK/out")])"
fi
if grep -qF 'built from (lhci 0.14.0) cookie 0.4.2: GHSA-pxg6-pf52-xh8x fixed in lhci 0.15.1' <<<"$newer_list"; then
  pass "an npm tool's resolved tree is read, and a hole only its next minor drops is listed for Ivan"
else
  fail "the npm tool's hole must be listed for Ivan (out=[$(cat "$WORK/out")])"
fi
if out_has 'read 9 tool packages' && ! grep -qF 'example/scanner' "$WORK/http.log"; then
  pass "every package each tool is built from is counted, and an image row is left to the image reader"
else
  fail "the count must cover each tool's packages and skip the image row (out=[$(cat "$WORK/out")])"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
