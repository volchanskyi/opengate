#!/usr/bin/env bash
# Tests the image reader of scripts/dependency-fixes: a finding a newer patch tag drops refuses,
# one only a newer line drops is listed, a digest is read once, and an unresolved tag is refused.
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
REGISTRY="$HTTP/registry-1.docker.io/v2"
mkdir -p "$REPO/scripts/lib" "$REPO/deploy" "$WORK/bin" "$HTTP/auth.docker.io" \
  "$REGISTRY/example/app/manifests" "$REGISTRY/library/alpine/manifests" \
  "$REGISTRY/example/app/tags" "$REGISTRY/library/alpine/tags"
ln -s "$PACKAGE" "$REPO/scripts/dependency-fixes"
ln -s "$FAKES/fake-curl.py" "$WORK/bin/curl"
printf 'export TOOL_VERSION_DEMO="1.0.0"\nexport TOOL_SOURCE_DEMO="Go:example.test/demo"\n' \
  >"$REPO/scripts/lib/tool-versions.sh"
printf 'FROM alpine:3.20\n' >"$REPO/Dockerfile"
printf 'app:\n  image: docker.io/example/app:1.2.3\n' >"$REPO/deploy/values.yaml"
# shellcheck disable=SC2016 # the file holds a printf whose escape runs into the image after it
printf 'printf "base\\nexample/app:1.2.3\\n"\n' >"$REPO/deploy/write-list.sh"
git -C "$REPO" init -q && git -C "$REPO" add -A

# digest NAME TAG N writes the manifest answer for one tag, its digest N repeated.
digest() {
  printf 'HTTP/2 200\r\ndocker-content-digest: sha256:%s\r\n\r\n' "$(printf "%064d" "$3")" \
    >"$REGISTRY/$1/manifests/$2.headers"
}
digest example/app 1.2.3 1
digest example/app 1.2.5 2
digest example/app 1.3.0 3
digest library/alpine 3.20 4
digest library/alpine 3.22 5
for repository in example/app library/alpine; do
  token="$HTTP/auth.docker.io/token%3Fservice=registry.docker.io&scope=repository:$repository:pull"
  mkdir -p "$(dirname "$token")"
  echo '{"token": "anonymous"}' >"$token"
done
# A registry hands its tags out a page at a time, the Link header naming the next page.
printf '{"name": "example/app", "tags": ["1.2.3", "1.2.5-debug"]}\n' >"$REGISTRY/example/app/tags/list%3Fn=10000"
printf 'HTTP/2 200\r\nlink: </v2/example/app/tags/list?n=10000&last=1.2.5-debug>; rel="next"\r\n\r\n' \
  >"$REGISTRY/example/app/tags/list%3Fn=10000.headers"
printf '{"name": "example/app", "tags": ["1.2.5", "1.3.0"]}\n' \
  >"$REGISTRY/example/app/tags/list%3Fn=10000&last=1.2.5-debug"
printf '{"name": "library/alpine", "tags": ["3.20", "3.22", "3.22-edge"]}\n' \
  >"$REGISTRY/library/alpine/tags/list%3Fn=10000"

# Trivy names, for each digest, the advisories its package source has a fix for.
ln -s "$FAKES/fake-trivy.py" "$WORK/bin/trivy"
printf '{"sha256:%064d": ["CVE-2026-0001", "CVE-2026-0002"], "sha256:%064d": ["CVE-2026-0002"],
  "sha256:%064d": ["CVE-2026-0003"]}\n' 1 2 4 >"$WORK/findings.json"

run() {
  : >"$WORK/trivy.log"
  env PATH="$WORK/bin:$PATH" FAKE_HTTP_DIR="$HTTP" FAKE_HTTP_LOG="$WORK/http.log" \
    FAKE_TRIVY_LOG="$WORK/trivy.log" FAKE_TRIVY_FINDINGS="$WORK/findings.json" FAKE_TRIVY_READ_MINUTES=12 \
    XDG_CACHE_HOME="$WORK/cache" \
    python3 "$REPO/scripts/dependency-fixes" images >"$WORK/out" 2>&1
}
out_has() { grep -qF -- "$1" "$WORK/out"; }

echo "dependency-fixes images:"

status=0
run || status=$?
patch_list="$(sed -n '/patch release of the line/,/newer minor or major/p' "$WORK/out")"
newer_list="$(sed -n '/newer minor or major/,$p' "$WORK/out")"
if [ "$status" -eq 1 ] && grep -qF 'image (deploy/values.yaml) example/app 1.2.3: CVE-2026-0001 fixed in 1.2.5' <<<"$patch_list" \
  && grep -qF 'move example/app:1.2.3 to tag 1.2.5 in deploy/values.yaml' <<<"$patch_list" && ! grep -qF '1.2.5-debug' "$WORK/out"; then
  pass "a finding the newest patch tag of the same shape drops refuses the commit, naming that tag, after an image read longer than trivy's own five minutes"
else
  fail "the patch tag must be named the fix (status=$status out=[$(cat "$WORK/out")])"
fi
if grep -qF 'GET https://registry-1.docker.io/v2/example/app/tags/list?n=10000&last=1.2.5-debug' "$WORK/http.log"; then
  pass "a registry's tag listing is read on every page its Link header names"
else
  fail "the listing's next page must be read (http=[$(cat "$WORK/http.log")])"
fi
if grep -qF 'image (deploy/values.yaml) example/app 1.2.3: CVE-2026-0002 fixed in 1.3.0' <<<"$newer_list"; then
  pass "a finding the patch tag still carries, and a newer line drops, is listed for Ivan"
else
  fail "the newer line's drop must be listed (out=[$(cat "$WORK/out")])"
fi
if grep -qF 'image (Dockerfile) library/alpine 3.20: CVE-2026-0003 fixed in 3.22' <<<"$newer_list" \
  && ! grep -qF 'alpine' <<<"$patch_list"; then
  pass "a line tag is read at its current build, and only a newer line can drop its findings"
else
  fail "the line tag's findings must be listed, never refused (out=[$(cat "$WORK/out")])"
fi
if ! grep -qF 'nexample' "$WORK/http.log" && grep -qF 'move example/app:1.2.3 to tag 1.2.5' "$WORK/out"; then
  pass "an escape such as \\n before an image reference separates it from the word before"
else
  fail "the escape must not join the image name (http=[$(cat "$WORK/http.log")] out=[$(cat "$WORK/out")])"
fi
if out_has 'read 4 image packages'; then
  pass "an advisory the image's package source has no fix for is left out, and packages are counted"
else
  fail "the run must count each image's packages (out=[$(cat "$WORK/out")])"
fi

run || true
if ! grep -q '^image ' "$WORK/trivy.log" && grep -q '^sbom ' "$WORK/trivy.log"; then
  pass "a digest's packages are read once, and every run judges them against the day's database"
else
  fail "a second run must judge cached package lists (trivy=[$(cat "$WORK/trivy.log")])"
fi

# shellcheck disable=SC2016 # the tag names a variable the reader must resolve, not the shell
printf 'docker run --rm example/tool:${UNSET_TAG}\n' >"$REPO/deploy/run.sh"
git -C "$REPO" add -A
status=0
run || status=$?
# shellcheck disable=SC2016 # the text the reader prints carries the variable it could not resolve
if [ "$status" -eq 2 ] && out_has 'deploy/run.sh: the image example/tool:${UNSET_TAG} names a value nothing resolves'; then
  pass "an image whose tag names a value nothing resolves fails the check as unreadable"
else
  fail "an unresolved tag must exit 2 (status=$status out=[$(cat "$WORK/out")])"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
