"""Python packages as PyPI publishes them, and as a tool's own environment installed them."""

import functools
import os
import re
import shutil

import fetch
import versions

try:
    from packaging.specifiers import InvalidSpecifier, SpecifierSet
    from packaging.requirements import Requirement
except ImportError:
    from pip._vendor.packaging.specifiers import InvalidSpecifier, SpecifierSet
    from pip._vendor.packaging.requirements import Requirement

HERE = os.path.dirname(os.path.abspath(__file__))


def normal(name):
    return re.sub(r"[-_.]+", "-", name).lower()


@functools.lru_cache(maxsize=None)
def releases(name):
    data = fetch.http_json(f"https://pypi.org/pypi/{name}/json")
    found = [versions.parse(v) for v, files in (data.get("releases") or {}).items()
             if files and not all(f.get("yanked") for f in files)]
    return [str(v) for v in sorted(v for v in found if v and v.final)]


def newest_patch(name, version):
    return versions.newest_patch(releases(name), version)


@functools.lru_cache(maxsize=None)
def asks_of(name, version, dependency):
    """What NAME at VERSION asks of DEPENDENCY, read from PyPI's record of that release."""
    data = fetch.http_json(f"https://pypi.org/pypi/{name}/{version}/json")
    for text in (data.get("info") or {}).get("requires_dist") or []:
        requirement = Requirement(text)
        if normal(requirement.name) == normal(dependency):
            return str(requirement.specifier)
    return None


def admits(specifier, version):
    try:
        return SpecifierSet(specifier or "").contains(version, prereleases=False)
    except InvalidSpecifier as error:
        raise fetch.Unreadable(f"the requirement {specifier!r} does not read") from error


def environment(command, distribution):
    """Every distribution the environment behind COMMAND installed for DISTRIBUTION."""
    path = shutil.which(command)
    if not path:
        raise fetch.Unreadable(f"{command} is not installed, so the packages it runs on cannot be read")
    with open(path, "rb") as handle:
        first = handle.readline().decode(errors="replace").strip()
    if not first.startswith("#!"):
        raise fetch.Unreadable(f"{path} names no interpreter")
    interpreter = first[2:].split()[0]
    out = fetch.run([interpreter, "-E", os.path.join(HERE, "pyclosure.py"), distribution],
                    what=f"the packages {command} runs on")
    return fetch.parse_json(out, f"the packages {command} runs on")
