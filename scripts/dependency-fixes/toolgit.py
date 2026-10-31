"""A tool built from a git source: by tag, its advisories answered by a later tag; by commit, its
own advisories and those on its lockfile, answered by the default branch's newest commit."""

import cache
import crates
import fetch
import gitsource
import osv
from report import NEWER_LINE, NO_FIX, PATCH, Finding
from toolrow import label, move
from versions import Version


def by_tag(key, spec, version):
    """(packages read, findings) for `<owner>/<repo>@<tag with {v}>` at VERSION."""
    repository, _, template = spec.partition("@")
    tags = gitsource.releases(repository, template)
    current = Version(version)
    commit = next((sha for v, _, sha in tags if Version(v) == current), None)
    if commit is None:
        raise fetch.Unreadable(f"{repository} has no tag {template.replace('{v}', version)}")
    hits = osv.matches([{"commit": commit}])[0]
    later = [(v, sha) for v, _, sha in tags if Version(v) > current]
    patch = [(v, sha) for v, sha in later if Version(v).line == current.line]
    candidates = [(patch[-1], PATCH)] if patch else []
    candidates += [(later[-1], NEWER_LINE)] if later else []
    carried = {sha: {a["id"] for a in osv.matches([{"commit": sha}])[0]} for (_, sha), _ in candidates} if hits else {}
    findings = []
    for advisory in hits:
        record = osv.record(advisory)
        answer, take = NO_FIX, ""
        for (release, sha), kind in candidates:
            if record["id"] not in carried[sha]:
                answer, take = kind, release
                break
        findings.append(Finding("tool", label(key), repository, version, record["id"], take,
                                osv.summary(record), move(key, take), answer, osv.aliases(record)))
    return 1, findings


def _lockfile(repository, rev):
    url = f"https://raw.githubusercontent.com/{repository}/{rev}/Cargo.lock"
    return dict(crates.parse_lockfile(cache.remember("cargolock", url, lambda: fetch.http_text(url))))


def by_commit(key, repository, commit):
    """(packages read, findings) for a tool pinned to COMMIT of REPOSITORY."""
    findings = [Finding("tool", label(key), repository, commit[:12], record["id"], "", osv.summary(record),
                        "", aliases=osv.aliases(record))
                for record in (osv.record(a) for a in osv.matches([{"commit": commit}])[0])]
    deps = _lockfile(repository, commit)
    newest = fetch.github(f"repos/{repository}/commits/HEAD")["sha"]
    for (_, name, dep_version), record, fix in osv.check_packages([("crates.io", n, v) for n, v in deps.items()]):
        dropped = fix and newest != commit and \
            not osv.affects("crates.io", name, _lockfile(repository, newest).get(name), record)
        findings.append(Finding("built from", f"{label(key)} {commit[:12]}", name, dep_version, record["id"],
                                newest[:12] if dropped else "", osv.summary(record), "",
                                NEWER_LINE if dropped else NO_FIX, osv.aliases(record), label(key)))
    return len(deps) + 1, findings
