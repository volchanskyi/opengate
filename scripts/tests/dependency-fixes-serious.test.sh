#!/usr/bin/env bash
# Tests scripts/dependency-fixes on a serious hole only a newer line fixes: severity from aliases,
# the server build it ships in, the exception list, and the image scan's findings against it.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
PACKAGE="$ROOT/scripts/dependency-fixes"

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

echo "dependency-fixes serious holes:"

# Each line the driver prints is a verdict: PASS or FAIL, then what was checked. A driver that
# stops early fails the test, so a crash never reads as a clean run.
driver_rc=0
python3 - "$PACKAGE" >"$WORK/verdicts" 2>"$WORK/driver.err" <<'PY' || driver_rc=$?
import sys
sys.path.insert(0, sys.argv[1])
import cvss
import exceptions
import report
import severity
from report import EXCEPTED, NEWER_LINE, PATCH, SERIOUS, Finding, Reading


def verdict(name, want, got):
    print(f"{'PASS' if want == got else 'FAIL'} {name}" + ("" if want == got else f" (want {want!r}, got {got!r})"))


verdict("every impact high over the network is critical", "CRITICAL",
        cvss.rating("CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H"))
verdict("a remote denial of service is high", "HIGH",
        cvss.rating("CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:N/I:N/A:H"))
verdict("a changed scope lifts the score past nine", "CRITICAL",
        cvss.rating("CVSS:3.1/AV:N/AC:L/PR:L/UI:N/S:C/C:H/I:H/A:H"))
verdict("a hard, user-assisted leak is low", "LOW",
        cvss.rating("CVSS:3.1/AV:N/AC:H/PR:N/UI:R/S:U/C:L/I:N/A:N"))
verdict("a score of 6.1 is medium", "MEDIUM",
        cvss.rating("CVSS:3.0/AV:N/AC:L/PR:N/UI:R/S:C/C:L/I:L/A:N"))
verdict("no impact is none", "NONE", cvss.rating("CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:N/I:N/A:N"))
verdict("a version-four vector is not scored", "", cvss.rating("CVSS:4.0/AV:N/AC:L/AT:N/PR:N/UI:N"))
verdict("a vector missing a metric is not scored", "", cvss.rating("CVSS:3.1/AV:N/AC:L"))

records = {
    "GHSA-high": {"id": "GHSA-high", "database_specific": {"severity": "HIGH"}},
    "GHSA-moderate": {"id": "GHSA-moderate", "database_specific": {"severity": "MODERATE"}},
    "CVE-dos": {"id": "CVE-dos", "severity": [
        {"type": "CVSS_V4", "score": "CVSS:4.0/AV:N"},
        {"type": "CVSS_V3", "score": "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:N/I:N/A:H"}]},
}
lookup = records.get
verdict("a GitHub alias's own rating decides", "HIGH",
        severity.of({"id": "GO-1", "aliases": ["CVE-dos", "GHSA-high"]}, lookup))
verdict("a moderate GitHub rating is what it says", "MODERATE",
        severity.of({"id": "GO-2", "aliases": ["GHSA-moderate", "CVE-dos"]}, lookup))
verdict("with no GitHub alias, a CVE alias's CVSS score decides", "HIGH",
        severity.of({"id": "GO-3", "aliases": ["CVE-dos"]}, lookup))
verdict("an alias OSV holds no record of carries no severity", "",
        severity.of({"id": "GO-4", "aliases": ["CVE-unknown"]}, lookup))
verdict("an advisory with no alias carries no severity", "", severity.of({"id": "GO-5"}, lookup))


def go(name, version, advisory, fix):
    return Finding("go", "server", name, version, advisory, fix, aliases=(f"GHSA-{advisory}",))


net = go("golang.org/x/net", "0.61.0", "GO-2026-6860", "0.62.0")
quiet = go("golang.org/x/net", "0.61.0", "GO-2026-0001", "0.62.0")
tests_only = go("github.com/stretchr/testify", "1.9.0", "GO-2026-0002", "2.0.0")
patched = go("golang.org/x/crypto", "0.40.0", "GO-2026-0003", "0.40.1")
ratings = {"GO-2026-6860": "HIGH", "GO-2026-0001": "MODERATE", "GO-2026-0002": "CRITICAL", "GO-2026-0003": "HIGH"}
severity.mark([net, quiet, tests_only, patched], {"golang.org/x/net", "golang.org/x/crypto", "stdlib"},
              lambda finding: ratings[finding.advisory])
verdict("a high hole a newer line fixes, in the server build, is serious", SERIOUS, net.answer)
verdict("its severity rides the finding", "HIGH", net.severity)
verdict("a moderate one stays listed", NEWER_LINE, quiet.answer)
verdict("one only the tests build stays listed", NEWER_LINE, tests_only.answer)
verdict("a patch fix stays a patch fix", PATCH, patched.answer)

lines, status = report.render([Reading("Go modules", 3, [net, quiet])])
verdict("a serious hole refuses the commit", 1, status)
verdict("and is printed in full with the release that fixes it", True,
        any("golang.org/x/net 0.61.0: GO-2026-6860 fixed in 0.62.0" in line and "HIGH" in line for line in lines))
verdict("under a heading of its own", True,
        any(line.startswith("dependency-fixes: 1 serious advisories") for line in lines))

entry = {"advisory": "CVE-2026-78661", "package": "golang.org/x/net", "newest": "0.62.0",
         "review_by": "2026-12-01", "reason": "the server parses no HTML"}
aliased = Finding("go", "server", "golang.org/x/net", "0.61.0", "GO-2026-6860", "0.62.0",
                  aliases=("CVE-2026-78661",), answer=SERIOUS)
newest = {"golang.org/x/net": "0.62.0"}.get
problems = exceptions.apply([entry], [aliased], "2026-11-08", newest, exceptions.go_modules)
verdict("an exception naming an alias excepts the hole", EXCEPTED, aliased.answer)
verdict("and raises nothing", [], problems)
lines, status = report.render([Reading("Go modules", 1, [aliased])])
verdict("an excepted hole passes", 0, status)
verdict("and is printed with its review date", True,
        any("excepted until 2026-12-01" in line for line in lines))

def problems_for(entries, findings, today="2026-11-08", newest_of=newest, judges=exceptions.go_modules):
    return exceptions.apply(entries, findings, today, newest_of, judges)

fresh = lambda: Finding("go", "server", "golang.org/x/net", "0.61.0", "GO-2026-6860", "0.62.0",
                        aliases=("CVE-2026-78661",), answer=SERIOUS)
late = fresh()
verdict("an exception past its review date lapses", True,
        any("2026-12-01" in p for p in problems_for([entry], [late], today="2026-12-02")))
verdict("and excepts nothing", SERIOUS, late.answer)
moved = fresh()
verdict("an exception lapses when its package releases again", True,
        any("0.63.0" in p for p in problems_for([entry], [moved], newest_of={"golang.org/x/net": "0.63.0"}.get)))
verdict("an exception the check no longer reports lapses", True,
        any("no longer" in p for p in problems_for([entry], [])))
image_only = dict(entry, advisory="CVE-2026-1111", package="openssl", newest="3.5.4")
verdict("an image package's exception is left to the image scan", [],
        problems_for([image_only], [], judges=exceptions.go_modules))
verdict("which judges it", True,
        any("no longer" in p for p in problems_for([image_only], [], judges=exceptions.every_package)))
for field in exceptions.FIELDS:
    broken = {k: v for k, v in entry.items() if k != field}
    try:
        exceptions.validate([broken])
        verdict(f"an exception with no {field} is refused", True, False)
    except exceptions.Malformed:
        verdict(f"an exception with no {field} is refused", True, True)
try:
    exceptions.validate([dict(entry, review_by="next month")])
    verdict("an undated review is refused", True, False)
except exceptions.Malformed:
    verdict("an undated review is refused", True, True)

trivy = {"Results": [{"Target": "app/meshserver", "Type": "gobinary", "Vulnerabilities": [
    {"VulnerabilityID": "CVE-2026-78661", "PkgName": "golang.org/x/net", "InstalledVersion": "v0.61.0",
     "FixedVersion": "0.62.0", "Severity": "HIGH", "Title": "html: quadratic parse"},
    {"VulnerabilityID": "CVE-2026-2222", "PkgName": "golang.org/x/net", "InstalledVersion": "v0.61.0",
     "FixedVersion": "", "Severity": "CRITICAL", "Title": "no fix yet"},
    {"VulnerabilityID": "CVE-2026-3333", "PkgName": "golang.org/x/text", "InstalledVersion": "v0.20.0",
     "FixedVersion": "0.21.0", "Severity": "MEDIUM", "Title": "not serious"}]}]}
lines, status = exceptions.judge_image(trivy, [], "2026-11-08", newest)
verdict("an image hole with a fix refuses the image", 1, status)
verdict("and names the advisory, the package and the fix", True,
        any("CVE-2026-78661" in l and "golang.org/x/net" in l and "0.62.0" in l for l in lines))
verdict("a hole with no fix and a medium one are not refused", False,
        any("CVE-2026-2222" in l or "CVE-2026-3333" in l for l in lines if l.startswith("::error::")))
lines, status = exceptions.judge_image(trivy, [entry], "2026-11-08", newest)
verdict("an excepted image hole passes", 0, status)
lines, status = exceptions.judge_image(trivy, [entry], "2026-12-02", newest)
verdict("a lapsed exception refuses the image", 1, status)
try:
    exceptions.judge_image({"nothing": []}, [], "2026-11-08", newest)
    verdict("a report with no results is unreadable", True, False)
except exceptions.Malformed:
    verdict("a report with no results is unreadable", True, True)
print("DONE")
PY
while IFS= read -r line; do
  case "$line" in
    PASS\ *) pass "${line#PASS }" ;;
    FAIL\ *) fail "${line#FAIL }" ;;
  esac
done <"$WORK/verdicts"
if [ "$driver_rc" -ne 0 ] || ! grep -qx DONE "$WORK/verdicts"; then
  fail "the driver stopped before its last check (exit $driver_rc): $(tail -n 3 "$WORK/driver.err" | tr '\n' ' ')"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
