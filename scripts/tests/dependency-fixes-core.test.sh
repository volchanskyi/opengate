#!/usr/bin/env bash
# Tests scripts/dependency-fixes' core: the fix an advisory's ranges name, the answer it earns,
# one hole under two names, the exit status, an unreadable lookup and the record cache.
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

echo "dependency-fixes core:"

# Each line the driver prints is a verdict: PASS or FAIL, then what was checked.
while IFS= read -r line; do
  case "$line" in
    PASS\ *) pass "${line#PASS }" ;;
    FAIL\ *) fail "${line#FAIL }" ;;
  esac
done < <(
  python3 - "$PACKAGE" <<'PY'
import sys
sys.path.insert(0, sys.argv[1])
import report
import versions
from report import NEWER_LINE, NO_FIX, PATCH, Finding, Reading


def verdict(name, want, got):
    print(f"{'PASS' if want == got else 'FAIL'} {name}" + ("" if want == got else f" (want {want!r}, got {got!r})"))


def semver(*events):
    return [{"type": "SEMVER", "events": list(events)}]


verdict("a release before the fix is fixed by it", "4.6.6",
        versions.fix_for("4.6.5", semver({"introduced": "0"}, {"fixed": "4.6.6"})))
verdict("a release at the fix is not covered", None,
        versions.fix_for("4.6.6", semver({"introduced": "0"}, {"fixed": "4.6.6"})))
verdict("a line with a fix of its own takes that fix", "1.6.1",
        versions.fix_for("1.6.0", semver({"introduced": "0.6.3"}, {"fixed": "0.6.14"},
                                         {"introduced": "1.0.0"}, {"fixed": "1.6.1"})))
verdict("a release between two ranges is not covered", None,
        versions.fix_for("0.9.0", semver({"introduced": "0.6.3"}, {"fixed": "0.6.14"},
                                         {"introduced": "1.0.0"}, {"fixed": "1.6.1"})))
verdict("a range open to the newest release has no fix", "",
        versions.fix_for("2.0.0", semver({"introduced": "1.0.0"})))
verdict("the last affected release has no fix", "",
        versions.fix_for("1.2.4", semver({"introduced": "0"}, {"last_affected": "1.2.4"})))
verdict("a release after the last affected one is not covered", None,
        versions.fix_for("1.2.5", semver({"introduced": "0"}, {"last_affected": "1.2.4"})))
verdict("a second range with no fix leaves the release unfixed", "",
        versions.fix_for("1.0.0", semver({"introduced": "0"}, {"fixed": "1.0.1"}) + semver({"introduced": "0"})))
verdict("a commit range is left to the commit query", None,
        versions.fix_for("1.0.0", [{"type": "GIT", "events": [{"introduced": "0"}, {"fixed": "abc"}]}]))

order = ["0.0.0-20260908162731-ac862fd6552b", "1.0.0rc1", "1.0.0", "1.0.0.post1", "1.0.1", "v1.2.0", "1.10.0"]
verdict("versions sort as releases do, pre-releases and pseudo-versions before their release",
        order, [str(v) for v in sorted(versions.Version(t) for t in order)])
verdict("a pseudo-version sorts before the release it precedes", True,
        versions.Version("2.29.1-0.20261012130337-71577268350e") < versions.Version("2.29.1"))
verdict("a pre-release is not a release", False, versions.Version("3.17.0-rc.1").final)
verdict("the newest patch of a line, later releases of other lines left out", "3.16.4",
        versions.newest_patch(["3.16.2", "3.16.4", "3.17.0", "3.16.3"], "3.16.3"))
verdict("the newest patch of a line at its newest is the release itself", "3.16.4",
        versions.newest_patch(["3.16.4", "3.17.0"], "3.16.4"))

for version, fix, want in (("0.70.0", "0.70.2", PATCH), ("0.70.0", "0.72.0", NEWER_LINE),
                           ("3.16.3", "3.17.3", NEWER_LINE), ("1.27.3", "1.27.4", PATCH),
                           ("11.6.0", "12.0.0", NEWER_LINE), ("4.6.5", "", NO_FIX)):
    verdict(f"{version} fixed in {fix or 'nothing'} is answered {want}", want, report.classify(version, fix))

twins = [Finding("rust", "agent", "aws-lc-sys", "0.37.0", "GHSA-9f94-5g5w-gf6r", "0.37.1",
                 aliases=("CVE-2026-1",)),
         Finding("rust", "agent", "aws-lc-sys", "0.37.0", "RUSTSEC-2026-0048", "0.37.1",
                 aliases=("CVE-2026-1", "GHSA-9f94-5g5w-gf6r")),
         Finding("rust", "agent", "aws-lc-sys", "0.37.0", "RUSTSEC-2026-0049", "0.37.1")]
verdict("one hole under two names is one finding, a second hole another", 2, len(report.merged(twins)))

lines, status = report.render([Reading("Rust crates", 3, twins)])
verdict("a hole a patch fixes refuses the commit", 1, status)
verdict("and the report names the patch", True, any("fixed in 0.37.1" in line for line in lines))
newer = [Finding("tool", "jq", "jqlang/jq", "1.7.1", "CVE-2026-39979", "1.8.2")]
lines, status = report.render([Reading("tool packages", 1, newer)])
verdict("a hole only a newer line fixes passes the commit", 0, status)
verdict("and is listed for Ivan to decide", True, any("newer minor or major" in line for line in lines))
PY
)

# A lookup that answers with something unreadable is a refusal, never a clean answer.
REPO="$WORK/repo"
TOOL="$WORK/http/proxy.golang.org/example.test/tool/@v"
mkdir -p "$REPO/scripts/lib" "$WORK/bin" "$WORK/http/api.osv.dev/v1/vulns" "$TOOL"
ln -s "$PACKAGE" "$REPO/scripts/dependency-fixes"
cat >"$REPO/scripts/lib/tool-versions.sh" <<'MANIFEST'
export TOOL_VERSION_DEMO="1.2.3"
export TOOL_SOURCE_DEMO="Go:example.test/tool"
MANIFEST
git -C "$REPO" init -q && git -C "$REPO" add -A
printf 'module example.test/tool\n\nrequire example.test/lib v0.4.0\n' >"$TOOL/v1.2.3.mod"
printf 'v1.2.3\n' >"$TOOL/list"
cat >"$WORK/http/api.osv.dev/v1/vulns/GO-2026-0001" <<'JSON'
{"id": "GO-2026-0001", "summary": "A hole in the tool", "aliases": ["CVE-2026-0001"],
 "affected": [{"package": {"ecosystem": "Go", "name": "example.test/tool"},
   "ranges": [{"type": "SEMVER", "events": [{"introduced": "0"}, {"fixed": "1.2.4"}]}]}]}
JSON
echo '{"Go|example.test/tool|1.2.3": ["GO-2026-0001"]}' >"$WORK/http/osv-matches.json"
ln -s "$FAKES/fake-curl.py" "$WORK/bin/curl"

run() {
  : >"$WORK/log"
  env PATH="$WORK/bin:$PATH" FAKE_HTTP_DIR="$WORK/http" FAKE_HTTP_LOG="$WORK/log" XDG_CACHE_HOME="$WORK/cache" \
    python3 "$REPO/scripts/dependency-fixes" "$@" >"$WORK/out" 2>&1
}

status=0
run tools || status=$?
if [ "$status" -eq 1 ] && grep -qF 'tool (demo) example.test/tool 1.2.3: GO-2026-0001 fixed in 1.2.4' "$WORK/out"; then
  pass "a pinned tool a patch fixes refuses the commit, naming the advisory and the patch"
else
  fail "a pinned tool a patch fixes must refuse the commit (status=$status out=[$(cat "$WORK/out")])"
fi

run tools || true
if ! grep -qF 'GET https://api.osv.dev/v1/vulns/GO-2026-0001' "$WORK/log"; then
  pass "an advisory's record is read once and cached for as long as it is unchanged"
else
  fail "a second run must read the record from the cache (log=[$(cat "$WORK/log")])"
fi

echo 'not json' >"$WORK/http/osv-matches.json"
status=0
run tools || status=$?
if [ "$status" -eq 2 ] && grep -qF '::error::dependency-fixes:' "$WORK/out"; then
  pass "an advisory database that answers with nothing readable fails the check as unreadable"
else
  fail "an unreadable answer must exit 2, never pass (status=$status out=[$(cat "$WORK/out")])"
fi

sed -i '/TOOL_SOURCE_DEMO/d' "$REPO/scripts/lib/tool-versions.sh"
status=0
run tools || status=$?
if [ "$status" -eq 2 ] && grep -qF 'no TOOL_SOURCE row for DEMO' "$WORK/out"; then
  pass "a manifest row that names no source is refused"
else
  fail "a manifest row without a source must be refused (status=$status out=[$(cat "$WORK/out")])"
fi

# The gauntlet asks the check first once its prerequisites hold, and a refusal ends the run there.
gauntlet="$ROOT/scripts/precommit-gauntlet.sh"
prerequisites_at="$(grep -m1 -nF 'all prerequisites present' "$gauntlet" | cut -d: -f1 || true)"
check_at="$(grep -m1 -nF 'python3 scripts/dependency-fixes' "$gauntlet" | cut -d: -f1 || true)"
lints_at="$(grep -m1 -nF 'banner "Lints"' "$gauntlet" | cut -d: -f1 || true)"
if [ -n "$prerequisites_at" ] && [ -n "$check_at" ] && [ -n "$lints_at" ] \
  && [ "$prerequisites_at" -lt "$check_at" ] && [ "$check_at" -lt "$lints_at" ] \
  && grep -qE '^[[:space:]]+exit 1$' <<<"$(sed -n "${check_at},${lints_at}p" "$gauntlet")"; then
  pass "the gauntlet runs the check before its first lint, and stops on a refusal"
else
  fail "the gauntlet must run the check after its prerequisites and exit on a refusal (lines: $prerequisites_at $check_at $lints_at)"
fi

# CI's security audit runs the same check, read as structure so a comment naming it is no step.
audit_steps="$(
  python3 - "$ROOT/.github/workflows/ci.yml" <<'PY'
import sys
import yaml
steps = yaml.safe_load(open(sys.argv[1]))["jobs"]["security-audit"]["steps"]
print("\n".join(step.get("run", "") for step in steps))
PY
)"
if grep -qE '^python3 scripts/dependency-fixes$' <<<"$audit_steps"; then
  pass "CI's security audit runs the check over every reader"
else
  fail "the security-audit job in ci.yml must run python3 scripts/dependency-fixes"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
