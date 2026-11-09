"""The one list of dated exceptions for a serious hole only a newer line fixes, read by the image
scan and by the dependency check alike, and the image scan's findings judged against it.

An exception lapses on its own: when its review date passes, when its package publishes a release
after the one it recorded, or when the check that judges it no longer reports its advisory.
"""

import json
import os
import re

import report
from severity import SERIOUS_RATINGS

LIST = os.path.join("scripts", "lib", "vulnerability-exceptions.json")
FIELDS = ("advisory", "package", "newest", "review_by", "reason")
_DATE = re.compile(r"^\d{4}-\d{2}-\d{2}$")


class Malformed(Exception):
    """An exception list, or a scan report, that does not read as one."""


def go_modules(entry):
    """Whether the dependency check judges ENTRY: one for a Go module or the standard library."""
    return entry["package"] == "stdlib" or "/" in entry["package"]


def every_package(_entry):
    """Whether the image scan judges ENTRY: it sees every package the image carries."""
    return True


def validate(entries):
    """ENTRIES, refused unless each names every field and a YYYY-MM-DD review date."""
    if not isinstance(entries, list):
        raise Malformed(f"{LIST} is not a JSON array")
    for entry in entries:
        missing = [field for field in FIELDS if not str((entry or {}).get(field, "")).strip()]
        if missing:
            raise Malformed(f"{LIST}: {entry.get('advisory', 'an entry')} has no {', '.join(missing)}")
        if not _DATE.match(entry["review_by"]):
            raise Malformed(f"{LIST}: {entry['advisory']} review_by {entry['review_by']} is not YYYY-MM-DD")
    return entries


def load(root):
    with open(os.path.join(root, LIST), encoding="utf-8") as handle:
        try:
            return validate(json.load(handle))
        except ValueError as error:
            raise Malformed(f"{LIST} is not JSON: {error}") from error


def _names(finding):
    return {finding.advisory, *finding.aliases}


def apply(entries, findings, today, newest_of, judges):
    """Marks each serious finding a live exception names excepted, and returns a line for each
    exception that lapsed. JUDGES picks the entries this check rules on; NEWEST_OF reads a
    package's newest release, None where nothing can be read."""
    serious = [f for f in findings if f.answer == report.SERIOUS]
    problems = []
    for entry in (e for e in entries if judges(e)):
        named = [f for f in serious if f.name == entry["package"] and entry["advisory"] in _names(f)]
        if not named:
            problems.append(f"the check no longer reports {entry['advisory']} ({entry['package']}); "
                            "delete its exception")
            continue
        if today > entry["review_by"]:
            problems.append(f"the exception for {entry['advisory']} ({entry['package']}) passed its review "
                            f"date {entry['review_by']}; renew or remove it")
            continue
        latest = newest_of(entry["package"])
        if latest and latest.lstrip("v") != entry["newest"].lstrip("v"):
            problems.append(f"{entry['package']} {latest} is published, after the {entry['newest']} the "
                            f"exception for {entry['advisory']} recorded; check whether it fixes the hole")
            continue
        for finding in named:
            finding.answer = report.EXCEPTED
            finding.excepted_until = entry["review_by"]
    return problems


def image_findings(scan):
    """A finding for each high or critical hole with a fixed release in a Trivy JSON report."""
    results = scan.get("Results") if isinstance(scan, dict) else None
    if not isinstance(results, list):
        raise Malformed("the scan report holds no Results list")
    found = []
    for result in results:
        for hole in result.get("Vulnerabilities") or []:
            if hole.get("Severity") not in SERIOUS_RATINGS or not hole.get("FixedVersion"):
                continue
            found.append(report.Finding(
                "image", result.get("Target", ""), hole["PkgName"], hole.get("InstalledVersion", "").lstrip("v"),
                hole["VulnerabilityID"], hole["FixedVersion"], summary=hole.get("Title", ""),
                answer=report.SERIOUS, severity=hole["Severity"]))
    return found


def judge_image(scan, entries, today, newest_of):
    """The lines and exit status for an image scan: 1 for a hole no live exception names, or for
    an exception that lapsed."""
    findings = image_findings(scan)
    problems = apply(entries, findings, today, newest_of, every_package)
    lines = [f"::error::image scan: {f.line()}" for f in findings if f.answer == report.SERIOUS]
    lines += [f"::error::image scan: {problem}" for problem in problems]
    lines += [f"image scan: {f.line()}" for f in findings if f.answer == report.EXCEPTED]
    if not lines:
        lines.append("image scan: no high or critical hole with a fixed release outside a live exception")
    refused = any(f.answer == report.SERIOUS for f in findings) or bool(problems)
    return lines, 1 if refused else 0
