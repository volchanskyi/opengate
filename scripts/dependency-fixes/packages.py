"""Advisories on a released package and on what it is built from, shared by every reader of a
release: tools, the Terraform CLI and providers, and the controllers charts install."""

import functools

import goproxy
import osv
import versions
from report import NEWER_LINE, NO_FIX, PATCH, Finding
from versions import Version


def own(kind, where, ecosystem, name, version, move):
    """Advisories on the package itself; MOVE names how a version is taken."""
    return [Finding(kind, where, name, version, record["id"], fix, osv.summary(record), move(fix),
                    aliases=osv.aliases(record))
            for _, record, fix in osv.check_packages([(ecosystem, name, version)])]


def carried(where, version, ecosystem, requirements, newest_patch, latest, move):
    """Advisories on what the release at VERSION is built from, each answered by whether a later
    release still builds with an affected version: its line's newest, then its newest of all."""
    later = functools.lru_cache(maxsize=None)(lambda release: dict(requirements(release)))
    deps = requirements(version)
    findings = []
    for (_, name, dep_version), record, fix in osv.check_packages([(ecosystem, n, v) for n, v in deps]):
        answer, take = NO_FIX, ""
        if fix:
            for release, kind in ((newest_patch(version), PATCH), (latest(), NEWER_LINE)):
                if Version(release) > Version(version) and \
                        not osv.affects(ecosystem, name, later(release).get(name), record):
                    answer, take = kind, release
                    break
        findings.append(Finding("built from", where, name, dep_version, record["id"], take, osv.summary(record),
                                move(take), answer, osv.aliases(record), where.split()[0] if take else ""))
    return len(deps), findings


def go_module(kind, label, module, version, move, requirements=None, releases=None):
    """A Go program released as MODULE: its own advisories and its module file's."""
    requirements = requirements or functools.partial(goproxy.requirements, module)
    releases = releases or functools.partial(goproxy.releases, module)
    count, built = carried(f"{label} {version}", version, "Go", requirements,
                           lambda current: versions.newest_patch(releases(), current),
                           lambda: (releases() or [version])[-1], move)
    return count + 1, own(kind, label, "Go", module, version, move) + built
