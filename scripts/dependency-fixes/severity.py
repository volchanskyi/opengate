"""How serious an advisory is, read from the records its aliases name, since the Go database
carries no severity of its own, and which newer-line findings that makes serious."""

import cvss
import osv
import report

SERIOUS_RATINGS = ("HIGH", "CRITICAL")


def _cvss_rating(record):
    for entry in record.get("severity") or []:
        if entry.get("type") == "CVSS_V3":
            found = cvss.rating(entry.get("score", ""))
            if found:
                return found
    return ""


def of(record, lookup=osv.alias_record):
    """RECORD's severity: a GitHub alias's own rating first, then the CVSS version 3 score its own
    record or a CVE alias carries; '' when nothing carries one. LOOKUP reads an alias's record,
    None when the database holds none."""
    aliases = osv.aliases(record)
    for alias in (a for a in aliases if a.startswith("GHSA-")):
        rated = ((lookup(alias) or {}).get("database_specific") or {}).get("severity", "")
        if rated:
            return rated.upper()
    own = _cvss_rating(record)
    if own:
        return own
    for alias in (a for a in aliases if a.startswith("CVE-")):
        found = _cvss_rating(lookup(alias) or {})
        if found:
            return found
    return ""


def mark(findings, shipped, rating_of):
    """Marks each newer-line finding serious whose module SHIPPED names and whose rating, read by
    RATING_OF, is high or critical; the rating rides every newer-line finding it was read for."""
    for finding in findings:
        if finding.answer != report.NEWER_LINE or finding.name not in shipped:
            continue
        finding.severity = rating_of(finding)
        if finding.severity in SERIOUS_RATINGS:
            finding.answer = report.SERIOUS
