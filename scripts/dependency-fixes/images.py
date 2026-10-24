"""Container images: every reference a tracked file makes, the packages inside each, and whether
a newer tag of the same shape no longer carries a finding.

A tag of three or more numbers names a release, and a newer patch tag of it can be taken; a tag
of fewer (`17-alpine`, `3.20`) names a line, which is read at the build it points at now.
"""

import concurrent.futures
import fnmatch
import json
import os
import re

import fetch
import registry
import repo
import sbom
import tools
from report import NEWER_LINE, NO_FIX, PATCH, Finding, Reading, named

SCOPE = os.path.join(os.path.dirname(os.path.abspath(__file__)), "scope.json")
# Official images a reference may name without a registry or a namespace.
OFFICIAL = {"alpine", "busybox", "debian", "golang", "nginx", "node", "postgres", "python", "redis", "ubuntu"}
_REFERENCE = re.compile(
    r"(?<![\w./@:${-])((?:[a-z0-9-]+(?:\.[a-z0-9-]+)+(?::\d+)?/)?[a-z0-9]+(?:[._-][a-z0-9]+)*"
    r"(?:/[a-z0-9]+(?:[._-][a-z0-9]+)*)*):([A-Za-z0-9_$][A-Za-z0-9_.{}$-]*)(@sha256:[0-9a-f]{64})?")
_TAG = re.compile(r"^(v?\d[\w.-]*|latest|\$\{?\w+.*)$")
_VARIABLE = re.compile(r"\$\{?([A-Za-z_][A-Za-z0-9_]*)\}?")
# A string escape such as the \n in a printf separates the word after it from the one before.
_ESCAPE = re.compile(r"\\[nrt]")


def scope():
    with open(SCOPE, encoding="utf-8") as handle:
        return json.load(handle)


def _candidate(name, tag):
    if not _TAG.match(tag) or (tag.isdigit() and len(tag) >= 4):
        return False
    first, _, rest = name.partition("/")
    if rest and "." in first:
        return "." not in rest
    return "." not in name and ("/" in name or name in OFFICIAL)


def _variables(root):
    """The values a reference's tag may name: the manifest's pins, and the browser image's
    Playwright release, which the e2e stack reads from the web lockfile."""
    values = {f"TOOL_VERSION_{key}": version for key, version, _ in tools.rows(root)}
    if "web/package-lock.json" in repo.tracked(root, "web/package-lock.json"):
        packages = json.loads(repo.text(root, "web/package-lock.json")).get("packages") or {}
        playwright = (packages.get("node_modules/@playwright/test") or {}).get("version")
        if playwright:
            values["PLAYWRIGHT_VERSION"] = playwright
    return values


def references(root):
    """Every image reference a tracked file makes outside the scope list: {reference: [files]}."""
    listed, values = scope(), _variables(root)
    skipped = [entry["path"] for entry in listed.get("files", [])]
    owned = [entry["image"] for entry in listed.get("images", [])]
    found = {}
    for path in repo.tracked(root):
        if any(fnmatch.fnmatch(path, pattern) for pattern in skipped):
            continue
        try:
            text = repo.text(root, path)
        except OSError:
            continue
        if "\0" in text:
            continue
        for name, tag, pinned in _REFERENCE.findall(_ESCAPE.sub(" ", text)):
            tag = tag[:-1] if tag.endswith("}") and tag.count("{") < tag.count("}") else tag
            resolved = _VARIABLE.sub(lambda m: values.get(m.group(1), m.group(0)), tag)
            if not _candidate(name, resolved) or any(name.startswith(prefix) for prefix in owned):
                continue
            if "$" in resolved or "{" in resolved:
                raise fetch.Unreadable(f"{path}: the image {name}:{tag} names a value nothing resolves")
            found.setdefault(str(registry.parse(f"{name}:{resolved}{pinned}")), set()).add(path)
    return {reference: sorted(paths) for reference, paths in found.items()}


def _numbers(tag):
    return tuple(int(n) for n in re.findall(r"\d+", tag))


def _shape(tag):
    return re.sub(r"\d+", "N", tag)


def newer(image, same_line):
    """The newest tag of IMAGE's shape after its own: within its line when SAME_LINE."""
    current = _numbers(image.tag)
    if same_line and len(current) < 3:
        return None
    candidates = [tag for tag in registry.tags(image.at(""))
                  if _shape(tag) == _shape(image.tag) and _numbers(tag) > current
                  and (not same_line or _numbers(tag)[:2] == current[:2])]
    return image.at(max(candidates, key=_numbers)) if candidates else None


def judge(image, where, move):
    """IMAGE's findings: those a newer patch tag drops, those only a newer line drops, the rest."""
    pinned = registry.Image(image.registry, image.repository, image.tag, registry.digest(image))
    count, now = sbom.fixable(pinned)
    findings, left = [], dict(now)
    for same_line, answer in ((True, PATCH), (False, NEWER_LINE)):
        if not left:
            break
        later = newer(image, same_line)
        if not later:
            continue
        _, then = sbom.fixable(registry.Image(later.registry, later.repository, later.tag, registry.digest(later)))
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
