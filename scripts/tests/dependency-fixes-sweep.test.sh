#!/usr/bin/env bash
# Tests what scripts/dependency-fixes reaches: each `uses:` resolved to the release it points at,
# and every declaration either read by a reader, listed with a reason, or refused.
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
GH="$WORK/gh/repos"
mkdir -p "$REPO/scripts/lib" "$REPO/.github/workflows" "$REPO/.github/actions/local" "$REPO/docs" \
  "$REPO/deploy" "$REPO/tools/extra" "$WORK/bin" "$HTTP/api.osv.dev/v1/vulns" "$GH/foo/bar" "$GH/foo/baz/commits"
ln -s "$PACKAGE" "$REPO/scripts/dependency-fixes"
ln -s "$FAKES/fake-curl.py" "$WORK/bin/curl"
ln -s "$FAKES/fake-gh.py" "$WORK/bin/gh"
PINNED=0123456789abcdef0123456789abcdef01234567
cat >"$REPO/scripts/lib/tool-versions.sh" <<'MANIFEST'
export TOOL_VERSION_GOTOOL="1.2.3"
export TOOL_VERSION_YAMLLINT="1.38.0"
export TOOL_VERSION_CROSS="8c1a8aa4b661711f4b7b6ac07c2e8929ce2f7d27"
export TOOL_VERSION_LHCI="0.14.0"
export TOOL_SOURCE_GOTOOL="Go:example.test/gotool"
export TOOL_SOURCE_YAMLLINT="PyPI:yamllint"
export TOOL_SOURCE_CROSS="git-rev:cross-rs/cross"
export TOOL_SOURCE_LHCI="npm:@lhci/cli"
MANIFEST
cat >"$REPO/.github/workflows/ci.yml" <<YAML
on: push
jobs:
  build:
    steps:
      - uses: actions/checkout@v7
      - uses: foo/bar@$PINNED # v1.2.3
      - uses: ./.github/actions/local
      - uses: docker://alpine:3.20
      - run: |
          go install example.test/gotool/cmd/gotool@v1.2.3
          pip install --user yamllint==1.38.0
          cargo install --locked cross \\
            --git https://github.com/cross-rs/cross --rev 8c1a8aa4b661711f4b7b6ac07c2e8929ce2f7d27
          npm install -g @lhci/cli@0.14.0
YAML
cat >"$REPO/.github/actions/local/action.yml" <<'YAML'
runs:
  using: composite
  steps:
    - uses: foo/baz/sub@v2.0.0
YAML
echo 'uses: foo/bar@v1 is how a step names an action.' >"$REPO/docs/actions.yml"
git -C "$REPO" init -q && git -C "$REPO" add -A

# foo/bar's pin is release 1.2.3, which a patch fixes; foo/baz's tag is 2.0.0, fixed only in 3.0.0.
printf '[{"name": "v1.2.3", "commit": {"sha": "%s"}}, {"name": "v1", "commit": {"sha": "%s"}}]\n' "$PINNED" "$PINNED" \
  >"$GH/foo/bar/tags%3Fper_page=100"
printf '[{"name": "v2.0.0", "commit": {"sha": "cccc"}}]\n' >"$GH/foo/baz/tags%3Fper_page=100"
printf '{"sha": "cccc"}\n' >"$GH/foo/baz/commits/v2.0.0"
for pair in GHSA-bar1-0000-0000:foo/bar:1.2.4 GHSA-baz1-0000-0000:foo/baz:3.0.0; do
  IFS=: read -r id name fixed <<<"$pair"
  printf '{"id": "%s", "summary": "a hole in %s", "affected": [{"package": {"ecosystem": "GitHub Actions",
    "name": "%s"}, "ranges": [{"type": "ECOSYSTEM", "events": [{"introduced": "0"}, {"fixed": "%s"}]}]}]}\n' \
    "$id" "$name" "$name" "$fixed" >"$HTTP/api.osv.dev/v1/vulns/$id"
done
echo '{"GitHub Actions|foo/bar|": ["GHSA-bar1-0000-0000"], "GitHub Actions|foo/baz|": ["GHSA-baz1-0000-0000"]}' \
  >"$HTTP/osv-matches.json"

run() {
  : >"$WORK/gh.log"
  env PATH="$WORK/bin:$PATH" FAKE_HTTP_DIR="$HTTP" FAKE_HTTP_LOG="$WORK/http.log" FAKE_GH_DIR="$WORK/gh" \
    FAKE_GH_LOG="$WORK/gh.log" XDG_CACHE_HOME="$WORK/cache" python3 "$REPO/scripts/dependency-fixes" "$@" \
    >"$WORK/out" 2>&1
}
out_has() { grep -qF -- "$1" "$WORK/out"; }

echo "dependency-fixes sweep:"

status=0
run actions || status=$?
if [ "$status" -eq 1 ] && out_has 'action (.github/workflows/ci.yml) foo/bar 1.2.3: GHSA-bar1-0000-0000 fixed in 1.2.4'; then
  pass "an action pinned by commit is resolved to its release, and a patch fixing it refuses the commit"
else
  fail "the commit-pinned action's patch must refuse the commit (status=$status out=[$(cat "$WORK/out")])"
fi
if out_has 'action (.github/actions/local/action.yml) foo/baz 2.0.0: GHSA-baz1-0000-0000 fixed in 3.0.0' \
  && out_has 'newer minor or major'; then
  pass "a shared action's tag is resolved through its commit, and a fix only a major holds is listed"
else
  fail "the shared action's newer-major fix must be listed (out=[$(cat "$WORK/out")])"
fi
if out_has 'read 3 action references' && ! grep -qF 'actions/checkout' "$WORK/gh.log"; then
  pass "local and image steps are left out, and an action no advisory names is never looked up"
else
  fail "only the actions an advisory names may be resolved (gh=[$(cat "$WORK/gh.log")] out=[$(cat "$WORK/out")])"
fi

rm "$GH/foo/bar/tags%3Fper_page=100"
status=0
run actions || status=$?
if [ "$status" -eq 2 ] && out_has "no release tag points at commit $PINNED" \
  || { [ "$status" -eq 2 ] && out_has 'Not Found'; }; then
  pass "an action whose release cannot be read fails the check as unreadable"
else
  fail "an unresolvable action must exit 2 (status=$status out=[$(cat "$WORK/out")])"
fi

status=0
run declarations || status=$?
if [ "$status" -eq 0 ] && out_has 'every declaration is one a reader reads'; then
  pass "installs a manifest row covers, workflow uses: lines and a listed docs file are all read"
else
  fail "a repository whose every declaration is read must pass the sweep (status=$status out=[$(cat "$WORK/out")])"
fi

touch "$REPO/yarn.lock"
echo '{"name": "extra"}' >"$REPO/tools/extra/package.json"
printf 'steps:\n  - uses: foo/qux@v1\n' >"$REPO/deploy/pipeline.yml"
printf 'go install example.test/stray/cmd/stray@v0.1.0\n' >"$REPO/deploy/setup.sh"
git -C "$REPO" add -A
status=0
run declarations || status=$?
if [ "$status" -eq 1 ] && out_has 'yarn.lock: a dependency file no reader reads' \
  && out_has 'tools/extra/package.json: an npm manifest with no lockfile beside it' \
  && out_has 'deploy/pipeline.yml: "uses: foo/qux@v1" outside the workflows' \
  && out_has 'deploy/setup.sh: "go install example.test/stray/cmd/stray@v0.1.0" installs a package no manifest row names' \
  && ! out_has 'docs/actions.yml'; then
  pass "a lockfile, a manifest, a uses: line and an install no reader reads each fail the sweep, by file"
else
  fail "each unread declaration must be named (status=$status out=[$(cat "$WORK/out")])"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
