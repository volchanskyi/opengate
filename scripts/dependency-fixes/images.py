"""Container images: the packages inside each one a tracked file names, and whether a newer tag
of the same shape, built for the same platforms, no longer carries a finding.

A tag of three or more numbers names a release, and a newer patch tag of it can be taken; a tag
of fewer (`17-alpine`, `3.20`) names a line, which is read at the build it points at now.
"""

import concurrent.futures
import re

import registry
import sbom
from imagerefs import references
from report import NEWER_LINE, NO_FIX, PATCH, Finding, Reading, named


def _numbers(tag):
    return tuple(int(n) for n in re.findall(r"\d+", tag))


def _shape(tag):
    return re.sub(r"\d+", "N", tag)


def newer(image, same_line):
    """The newest tag of IMAGE's shape after its own, within its line when SAME_LINE, built for
    every platform IMAGE, which carries its digest, is built for; the tag carries its digest."""
    current = _numbers(image.tag)
    if same_line and len(current) < 3:
        return None
    candidates = sorted((tag for tag in registry.tags(image.at(""))
                         if _shape(tag) == _shape(image.tag) and _numbers(tag) > current
                         and (not same_line or _numbers(tag)[:2] == current[:2])), key=_numbers, reverse=True)
    if not candidates:
        return None
    needed = registry.platforms(image)
    for tag in candidates:
        later = registry.pinned(image.at(tag))
        if needed <= registry.platforms(later):
            return later
    return None


def judge(image, where, move):
    """IMAGE's findings: those a newer patch tag drops, those only a newer line drops, the rest."""
    pinned = registry.pinned(image)
    count, now = sbom.fixable(pinned)
    findings, left = [], dict(now)
    for same_line, answer in ((True, PATCH), (False, NEWER_LINE)):
        if not left:
            break
        later = newer(pinned, same_line)
        if not later:
            continue
        _, then = sbom.fixable(later)
        dropped = sorted(key for key in left if key not in then)
        if dropped:
            findings.append(Finding("image", where, image.name, image.tag, named(dropped), later.tag,
                                    f"{len(dropped)} findings the tag no longer carries", move(later.tag), answer,
                                    tuple(advisory for advisory, _ in dropped)))
            left = {key: value for key, value in left.items() if key in then}
    if left:
        findings.append(Finding("image", where, image.name, image.tag, named(sorted(left)), "",
                                f"{len(left)} findings", "", NO_FIX, tuple(advisory for advisory, _ in left)))
    return count, findings


def read(root):
    found = references(root)
    images = {reference: registry.parse(reference) for reference in found}
    count, findings = 0, []

    def one(reference):
        def move(tag):
            return f"move {reference} to tag {tag} in {', '.join(found[reference])}"
        return judge(images[reference], found[reference][0], move)

    with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
        for read_here, found_here in pool.map(one, sorted(found)):
            count, findings = count + read_here, findings + found_here
    return Reading("image packages", count, findings)
