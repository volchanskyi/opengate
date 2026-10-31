"""A tool npm installs globally: its own advisories, and those on the tree it resolves into, each
answered by whether a later release of the tool still resolves into it."""

import functools
import shutil

import fetch
import npm
import versions
from report import NEWER_LINE, NO_FIX, PATCH
from toolrow import label, move, own
from versions import Version


@functools.lru_cache(maxsize=None)
def _releases(name):
    data = fetch.http_json(f"https://registry.npmjs.org/{name.replace('/', '%2f')}")
    found = [versions.parse(v) for v in data.get("versions") or {}]
    return [str(v) for v in sorted(v for v in found if v and v.final)]


@functools.lru_cache(maxsize=None)
def _advisories(name, version):
    """{(package, advisory)} an install of NAME at VERSION resolves into today."""
    work, tree = npm.resolved(name, version)
    try:
        return {(f.name, f.advisory) for f in npm.findings(work, work, tree, lambda *_: "")}
    finally:
        shutil.rmtree(work, ignore_errors=True)


def read(key, name, version):
    """(packages read, findings) for the npm package NAME at VERSION."""
    work, tree = npm.resolved(name, version)
    try:
        built = npm.findings(work, f"{label(key)} {version}", tree, lambda *_: "")
    finally:
        shutil.rmtree(work, ignore_errors=True)
    releases = _releases(name)
    later = [(versions.newest_patch(releases, version), PATCH), (releases[-1], NEWER_LINE)]
    for finding in built:
        finding.kind, finding.answer, finding.fix, finding.fix_of = "built from", NO_FIX, "", ""
        for release, answer in later:
            if Version(release) > Version(version) and \
                    (finding.name, finding.advisory) not in _advisories(name, release):
                finding.answer, finding.fix, finding.fix_of = answer, release, label(key)
                finding.remedy = move(key, release)
                break
    return len(tree), own(key, "npm", name, version) + built
