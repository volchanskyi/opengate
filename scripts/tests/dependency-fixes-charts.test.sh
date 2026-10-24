#!/usr/bin/env bash
# Tests the chart reader of scripts/dependency-fixes: the program a chart installs and the images
# its release renders, each judged against the chart's own later releases.
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
RAW="$HTTP/raw.githubusercontent.com/cert-manager/cert-manager"
QUAY="$HTTP/quay.io/v2/jetstack/cert-manager-controller/manifests"
mkdir -p "$REPO/scripts/lib" "$REPO/deploy/cert-manager" "$WORK/bin" "$VULNS" "$QUAY" \
  "$HTTP/charts.example.test" "$RAW/v1.20.2" "$RAW/v1.20.4" "$RAW/v1.21.0"
ln -s "$PACKAGE" "$REPO/scripts/dependency-fixes"
ln -s "$FAKES/fake-curl.py" "$WORK/bin/curl"
ln -s "$FAKES/fake-trivy.py" "$WORK/bin/trivy"
cat >"$REPO/scripts/lib/tool-versions.sh" <<'MANIFEST'
export TOOL_VERSION_CERT_MANAGER_CHART="v1.20.2"
export TOOL_SOURCE_CERT_MANAGER_CHART="chart:https://charts.example.test#cert-manager"
MANIFEST
printf 'crds:\n  enabled: true\n' >"$REPO/deploy/cert-manager/values.yaml"
git -C "$REPO" init -q && git -C "$REPO" add -A

# The chart repository's index lists each release beside the program release it installs.
cat >"$HTTP/charts.example.test/index.yaml" <<'INDEX'
apiVersion: v1
entries:
  cert-manager:
    - {version: v1.21.0, appVersion: v1.21.0}
    - {version: v1.20.4, appVersion: v1.20.4}
    - {version: v1.21.0-alpha.1, appVersion: v1.21.0-alpha.1}
    - {version: v1.20.2, appVersion: v1.20.2}
INDEX
# The program's module file at each release tag: the patch release moves past a hole.
printf 'module github.com/cert-manager/cert-manager\n\nrequire golang.org/x/net v0.30.0\n' >"$RAW/v1.20.2/go.mod"
printf 'module github.com/cert-manager/cert-manager\n\nrequire golang.org/x/net v0.33.0\n' >"$RAW/v1.20.4/go.mod"
printf 'module github.com/cert-manager/cert-manager\n\nrequire golang.org/x/net v0.33.0\n' >"$RAW/v1.21.0/go.mod"

# advisory ID ECOSYSTEM NAME FIXED writes an OSV record whose one range ends at FIXED.
advisory() {
  printf '{"id": "%s", "summary": "%s", "affected": [{"package": {"ecosystem": "%s", "name": "%s"},
    "ranges": [{"type": "SEMVER", "events": [{"introduced": "0"}, {"fixed": "%s"}]}]}]}\n' \
    "$1" "a hole in $3" "$2" "$3" "$4" >"$VULNS/$1"
}
advisory GO-2026-3000 Go golang.org/x/net 0.33.0
advisory GHSA-c111-1111-1111 Go github.com/cert-manager/cert-manager 1.21.0
cat >"$HTTP/osv-matches.json" <<'JSON'
{"Go|golang.org/x/net|0.30.0": ["GO-2026-3000"],
 "Go|github.com/cert-manager/cert-manager|1.20.2": ["GHSA-c111-1111-1111"]}
JSON

# Helm renders the controller image at the chart release's own tag, and logs what it was asked.
cat >"$WORK/bin/helm" <<'HELM'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$FAKE_HELM_LOG"
version=""
while [ "$#" -gt 0 ]; do
  [ "$1" = "--version" ] && version="$2"
  shift
done
printf 'kind: Deployment\nspec:\n  template:\n    spec:\n      containers:\n'
printf '        - name: controller\n          image: quay.io/jetstack/cert-manager-controller:%s\n' "$version"
HELM
chmod +x "$WORK/bin/helm"
# Each tag's digest answers with an index of its builds, one an amd64 and one an arm64 machine.
n=1
for tag in v1.20.2 v1.20.4 v1.21.0; do
  printf 'HTTP/2 200\r\ndocker-content-digest: sha256:%064d\r\n\r\n' "$n" >"$QUAY/$tag.headers"
  printf '{"manifests": [{"platform": {"os": "linux", "architecture": "amd64"}},
    {"platform": {"os": "linux", "architecture": "arm64"}}]}\n' >"$QUAY/sha256:$(printf '%064d' "$n")"
  n=$((n + 1))
done
printf '{"sha256:%064d": ["CVE-2026-0001", "CVE-2026-0002"], "sha256:%064d": ["CVE-2026-0002"]}\n' 1 2 \
  >"$WORK/findings.json"

run() {
  : >"$WORK/helm.log"
  env PATH="$WORK/bin:$PATH" FAKE_HTTP_DIR="$HTTP" FAKE_HTTP_LOG="$WORK/http.log" FAKE_HELM_LOG="$WORK/helm.log" \
    FAKE_TRIVY_LOG="$WORK/trivy.log" FAKE_TRIVY_FINDINGS="$WORK/findings.json" XDG_CACHE_HOME="$WORK/cache" \
    python3 "$REPO/scripts/dependency-fixes" charts >"$WORK/out" 2>&1
}
out_has() { grep -qF -- "$1" "$WORK/out"; }

echo "dependency-fixes charts:"

status=0
run || status=$?
patch_list="$(sed -n '/patch release of the line/,/newer minor or major/p' "$WORK/out")"
newer_list="$(sed -n '/newer minor or major/,$p' "$WORK/out")"
if [ "$status" -eq 1 ] \
  && grep -qF 'built from (cert-manager v1.20.2) golang.org/x/net v0.30.0: GO-2026-3000 fixed in cert-manager v1.20.4' <<<"$patch_list" \
  && grep -qF 'move TOOL_VERSION_CERT_MANAGER_CHART to v1.20.4, the chart release installing 1.20.4' <<<"$patch_list"; then
  pass "a hole the installed program's patch release builds past refuses the commit, naming that chart release"
else
  fail "the program's patch release must be named the fix (status=$status out=[$(cat "$WORK/out")])"
fi
if grep -qF 'chart (cert-manager v1.20.2) github.com/cert-manager/cert-manager 1.20.2: GHSA-c111-1111-1111 fixed in 1.21.0' <<<"$newer_list"; then
  pass "a hole in the program itself that only a newer chart line fixes is listed for Ivan"
else
  fail "the program's own newer-line fix must be listed (out=[$(cat "$WORK/out")])"
fi
if grep -qF 'chart image (cert-manager v1.20.2) quay.io/jetstack/cert-manager-controller v1.20.2: CVE-2026-0001 fixed in cert-manager v1.20.4' <<<"$patch_list" \
  && grep -qF 'chart image (cert-manager v1.20.2) quay.io/jetstack/cert-manager-controller v1.20.2: CVE-2026-0002 fixed in cert-manager v1.21.0' <<<"$newer_list"; then
  pass "an image the chart renders is judged against the image its own patch and newest releases render"
else
  fail "each rendered image's findings must be answered by a later chart release (out=[$(cat "$WORK/out")])"
fi
if ! out_has 'alpha' && grep -qF -- "--version v1.20.2 -f $REPO/deploy/cert-manager/values.yaml" "$WORK/helm.log"; then
  pass "a pre-release in the index is never a fix, and the chart renders with the repository's values"
else
  fail "the render must use deploy/cert-manager/values.yaml (helm=[$(cat "$WORK/helm.log")] out=[$(cat "$WORK/out")])"
fi
if out_has 'read 4 chart packages'; then
  pass "the program's module and its requirements are counted beside the packages of every rendered image"
else
  fail "the count must cover the program and its images (out=[$(cat "$WORK/out")])"
fi

# The patch release's image moves to a tag pushed for amd64 alone, which an arm64 node cannot run.
printf 'HTTP/2 200\r\ndocker-content-digest: sha256:%064d\r\n\r\n' 4 >"$QUAY/v1.20.4.headers"
printf '{"config": {"digest": "sha256:%064d"}}\n' 54 >"$QUAY/sha256:$(printf '%064d' 4)"
mkdir -p "$HTTP/quay.io/v2/jetstack/cert-manager-controller/blobs"
printf '{"os": "linux", "architecture": "amd64"}\n' \
  >"$HTTP/quay.io/v2/jetstack/cert-manager-controller/blobs/sha256:$(printf '%064d' 54)"
status=0
run || status=$?
patch_list="$(sed -n '/patch release of the line/,/newer minor or major/p' "$WORK/out")"
newer_list="$(sed -n '/newer minor or major/,$p' "$WORK/out")"
if ! grep -qF 'chart image' <<<"$patch_list" \
  && grep -qF 'cert-manager-controller v1.20.2: CVE-2026-0001 and 1 more fixed in cert-manager v1.21.0' <<<"$newer_list"; then
  pass "a chart release whose image has no build for a platform the image in use has is never the fix for it"
else
  fail "the amd64-only image must be passed over (status=$status out=[$(cat "$WORK/out")])"
fi

sed -i 's/TOOL_VERSION_CERT_MANAGER_CHART="v1.20.2"/TOOL_VERSION_CERT_MANAGER_CHART="v1.19.9"/' \
  "$REPO/scripts/lib/tool-versions.sh"
status=0
run || status=$?
if [ "$status" -eq 2 ] && out_has 'https://charts.example.test lists no cert-manager v1.19.9'; then
  pass "a chart version the repository's index does not list fails the check as unreadable"
else
  fail "an unlisted chart version must exit 2 (status=$status out=[$(cat "$WORK/out")])"
fi

cat >"$REPO/scripts/lib/tool-versions.sh" <<'MANIFEST'
export TOOL_VERSION_OTHER_CHART="1.0.0"
export TOOL_SOURCE_OTHER_CHART="chart:https://charts.example.test#other"
MANIFEST
status=0
run || status=$?
if [ "$status" -eq 2 ] && out_has 'the chart other installs a program charts.PROGRAMS does not name'; then
  pass "a chart whose program the reader has no source for fails the check as unreadable"
else
  fail "an unknown chart must exit 2 (status=$status out=[$(cat "$WORK/out")])"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
