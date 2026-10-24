"""What the check found, sorted into the three answers the rule gives an advisory."""

import dataclasses

import versions

# A patch release of the line in use fixes it: the commit is refused until it is taken.
PATCH = "patch"
# Only a newer minor or major release fixes it: it is listed for Ivan to decide.
NEWER_LINE = "newer line"
# No release fixes it: there is nothing to take.
NO_FIX = "no fix"


@dataclasses.dataclass
class Finding:
    kind: str
    where: str
    name: str
    version: str
    advisory: str
    fix: str
    summary: str = ""
    remedy: str = ""
    answer: str = ""
    aliases: tuple = ()
    fix_of: str = ""

    def __post_init__(self):
        if not self.answer:
            self.answer = classify(self.version, self.fix)

    def line(self):
        owner = f"{self.fix_of} " if self.fix_of else ""
        fixed = f"fixed in {owner}{self.fix}" if self.fix else "no fixed release"
        text = f"{self.kind} ({self.where}) {self.name} {self.version}: {self.advisory} {fixed}"
        if self.summary:
            text += f" — {self.summary}"
        if self.remedy and self.answer == PATCH:
            text += f"\n      take it: {self.remedy}"
        return text


def classify(version, fix):
    """PATCH when FIX is a later release of VERSION's own line, NEWER_LINE for any other fix."""
    if not fix:
        return NO_FIX
    current, fixed = versions.parse(version), versions.parse(fix)
    if current is None or fixed is None:
        return NEWER_LINE
    return PATCH if fixed.line == current.line and fixed > current else NEWER_LINE


@dataclasses.dataclass
class Reading:
    """One reader's answer: how many dependencies it read and what it found among them."""

    kind: str
    read: int
    findings: list


def render(readings):
    """The report's lines and the exit status: 1 when a patch release fixes anything, else 0."""
    unique = merged([finding for reading in readings for finding in reading.findings])
    by_answer = {answer: [f for f in unique if f.answer == answer]
                 for answer in (PATCH, NEWER_LINE, NO_FIX)}
    lines = []
    if by_answer[PATCH]:
        lines.append(f"dependency-fixes: {len(by_answer[PATCH])} advisories a patch release of the line "
                     "in use fixes; take each before the commit:")
        lines += [f"  {f.line()}" for f in sorted(by_answer[PATCH], key=_order)]
    if by_answer[NEWER_LINE]:
        lines.append(f"dependency-fixes: {len(by_answer[NEWER_LINE])} advisories fixed only in a newer "
                     "minor or major release; bring each to Ivan as a question before anything moves:")
        lines += [f"  {f.line()}" for f in sorted(by_answer[NEWER_LINE], key=_order)]
    read = ", ".join(f"{reading.read} {reading.kind}" for reading in readings)
    lines.append(f"dependency-fixes: read {read}; {len(by_answer[NO_FIX])} advisories have no fixed release")
    return lines, 1 if by_answer[PATCH] else 0


def named(findings):
    """The first advisory of FINDINGS, (advisory, package) pairs, and how many follow it."""
    advisories = sorted({advisory for advisory, _ in findings})
    return advisories[0] + (f" and {len(advisories) - 1} more" if len(advisories) > 1 else "")


def merged(findings):
    """One finding per hole: records of one dependency sharing an identifier or alias are one."""
    kept, seen = [], {}
    for finding in findings:
        place = (finding.kind, finding.where, finding.name, finding.version)
        names = {finding.advisory, *finding.aliases}
        known = seen.setdefault(place, [])
        twin = next((entry for entry in known if entry[0] & names), None)
        if twin:
            twin[0].update(names)
            continue
        known.append((names, finding))
        kept.append(finding)
    return kept


def _order(finding):
    return (finding.kind, finding.where, finding.name, finding.advisory)
