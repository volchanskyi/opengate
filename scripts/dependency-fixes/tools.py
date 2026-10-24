"""Every pinned tool: each manifest row's source names where its releases and advisories live,
and the packages the tool itself is built from or runs on are read where that source keeps them.

A source is `Go:<module>`, `crates.io:<crate>`, `PyPI:<package>[:<command>]`,
`Maven:<group>:<artifact>`, `git:<owner>/<repo>@<tag with {v}>`, `image:<repository>` or
`chart:<repository url>#<chart>`; images and charts are read by their own readers.
"""

import functools
import re
import shutil

import cache
import crates
import fetch
import gitsource
import npm
import osv
import packages
import pypi
import repo
import versions
from report import NEWER_LINE, NO_FIX, PATCH, Finding, Reading, classify
from versions import Version

MANIFEST = "scripts/lib/tool-versions.sh"
_ROW = re.compile(r'^export TOOL_(VERSION|SOURCE)_([A-Z0-9_]+)="([^"]*)"', re.M)


def rows(root):
    """Every (key, version, source) the manifest pins; a row with no source is refused."""
    found = _ROW.findall(repo.text(root, MANIFEST))
    pinned = {key: value for kind, key, value in found if kind == "VERSION"}
    sources = {key: value for kind, key, value in found if kind == "SOURCE"}
    unsourced = sorted(set(pinned) - set(sources))
    if unsourced:
        raise fetch.Unreadable(f"{MANIFEST}: no TOOL_SOURCE row for {', '.join(unsourced)}")
    orphaned = sorted(set(sources) - set(pinned))
    if orphaned:
        raise fetch.Unreadable(f"{MANIFEST}: a TOOL_SOURCE row with no version: {', '.join(orphaned)}")
    return [(key, pinned[key], sources[key]) for key in pinned]


def _label(key):
    return key.lower().replace("_", "-")


def _move(key, version):
    return f"move TOOL_VERSION_{key} in {MANIFEST}, and every copy the parity sweep holds, to {version}"


def _own(key, ecosystem, name, version):
    return packages.own("tool", _label(key), ecosystem, name, version, functools.partial(_move, key))


def _go(key, module, version):
    return packages.go_module("tool", _label(key), module, version, functools.partial(_move, key))


def _crate(key, name, version):
    count, carried = packages.carried(
        f"{_label(key)} {version}", version, "crates.io", functools.partial(crates.requirements, name),
        functools.partial(crates.newest_patch, name), lambda: crates.latest(name), functools.partial(_move, key))
    return count + 1, _own(key, "crates.io", name, version) + carried


def _python_answer(key, tool, version, entry, fix):
    """For a dependency whose own patch FIX exists: taken by reinstalling the tool when every
    requirer admits it, by a later release of the tool when only the tool holds it back."""
    blockers = [who for who, spec in entry["asks"] if not pypi.admits(spec, fix)]
    if not blockers:
        return PATCH, fix, f"reinstall {tool} so its environment takes {entry['name']} {fix}"
    if set(blockers) != {pypi.normal(tool)}:
        return NO_FIX, "", ""
    patch = pypi.newest_patch(tool, version)
    if Version(patch) > Version(version) and pypi.admits(pypi.asks_of(tool, patch, entry["name"]), fix):
        return PATCH, patch, _move(key, patch)
    newest = pypi.releases(tool)[-1]
    if Version(newest) > Version(version) and pypi.admits(pypi.asks_of(tool, newest, entry["name"]), fix):
        return NEWER_LINE, newest, ""
    return NO_FIX, "", ""


def _python(key, spec, version):
    tool, _, command = spec.partition(":")
    environment = {entry["name"]: entry for entry in pypi.environment(command or tool, tool)}
    installed = (environment.get(pypi.normal(tool)) or {}).get("version")
    if installed != version:
        raise fetch.Unreadable(f"{command or tool} runs {tool} {installed}, and {MANIFEST} pins {version}")
    deps = [(name, entry["version"]) for name, entry in environment.items() if name != pypi.normal(tool)]
    findings = _own(key, "PyPI", tool, version)
    for (_, name, dep_version), record, fix in osv.check_packages([("PyPI", n, v) for n, v in deps]):
        answer, take, remedy = NO_FIX, "", ""
        if fix and classify(dep_version, fix) == PATCH:
            answer, take, remedy = _python_answer(key, tool, version, environment[name], fix)
        elif fix:
            answer, take = NEWER_LINE, fix
        findings.append(Finding("built from", f"{_label(key)} {version}", name, dep_version, record["id"],
                                take, osv.summary(record), remedy, answer, osv.aliases(record),
                                tool if take and take != fix else ""))
    return len(deps) + 1, findings


def _git(key, spec, version):
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
        findings.append(Finding("tool", _label(key), repository, version, record["id"], take,
                                osv.summary(record), _move(key, take), answer, osv.aliases(record)))
    return 1, findings


@functools.lru_cache(maxsize=None)
def _npm_releases(name):
    data = fetch.http_json(f"https://registry.npmjs.org/{name.replace('/', '%2f')}")
    found = [versions.parse(v) for v in data.get("versions") or {}]
    return [str(v) for v in sorted(v for v in found if v and v.final)]


@functools.lru_cache(maxsize=None)
def _npm_advisories(name, version):
    """{(package, advisory)} an install of NAME at VERSION resolves into today."""
    work, tree = npm.resolved(name, version)
    try:
        return {(f.name, f.advisory) for f in npm.findings(work, work, tree, lambda _: "")}
    finally:
        shutil.rmtree(work, ignore_errors=True)


def _npm(key, name, version):
    """A tool npm installs globally: its own advisories, and those on the tree it resolves into,
    each answered by whether a later release of the tool still resolves into it."""
    work, tree = npm.resolved(name, version)
    try:
        built = npm.findings(work, f"{_label(key)} {version}", tree, lambda _: "")
    finally:
        shutil.rmtree(work, ignore_errors=True)
    releases = _npm_releases(name)
    later = [(versions.newest_patch(releases, version), PATCH), (releases[-1], NEWER_LINE)]
    for finding in built:
        finding.kind, finding.answer, finding.fix, finding.fix_of = "built from", NO_FIX, "", ""
        for release, answer in later:
            if Version(release) > Version(version) and \
                    (finding.name, finding.advisory) not in _npm_advisories(name, release):
                finding.answer, finding.fix, finding.fix_of = answer, release, _label(key)
                finding.remedy = _move(key, release)
                break
    return len(tree), _own(key, "npm", name, version) + built


def _git_rev(key, repository, commit):
    """A tool pinned to a commit: its own advisories by that commit, and those on its lockfile,
    each answered by whether the default branch's newest commit still carries it."""
    def lockfile(rev):
        url = f"https://raw.githubusercontent.com/{repository}/{rev}/Cargo.lock"
        return dict(crates.parse_lockfile(cache.remember("cargolock", url, lambda: fetch.http_text(url))))
    findings = [Finding("tool", _label(key), repository, commit[:12], record["id"], "", osv.summary(record),
                        "", aliases=osv.aliases(record))
                for record in (osv.record(a) for a in osv.matches([{"commit": commit}])[0])]
    deps = lockfile(commit)
    newest = fetch.github(f"repos/{repository}/commits/HEAD")["sha"]
    for (_, name, dep_version), record, fix in osv.check_packages([("crates.io", n, v) for n, v in deps.items()]):
        dropped = fix and newest != commit and not osv.affects("crates.io", name, lockfile(newest).get(name), record)
        findings.append(Finding("built from", f"{_label(key)} {commit[:12]}", name, dep_version, record["id"],
                                newest[:12] if dropped else "", osv.summary(record), "",
                                NEWER_LINE if dropped else NO_FIX, osv.aliases(record), _label(key)))
    return len(deps) + 1, findings


def read(root):
    count, findings = 0, []
    for key, version, source in rows(root):
        ecosystem, _, spec = source.partition(":")
        if ecosystem == "Go":
            read_here, found = _go(key, spec, version)
        elif ecosystem == "crates.io":
            read_here, found = _crate(key, spec, version)
        elif ecosystem == "PyPI":
            read_here, found = _python(key, spec, version)
        elif ecosystem == "Maven":
            read_here, found = 1, _own(key, "Maven", spec, version)
        elif ecosystem == "git":
            read_here, found = _git(key, spec, version)
        elif ecosystem == "git-rev":
            read_here, found = _git_rev(key, spec, version)
        elif ecosystem == "npm":
            read_here, found = _npm(key, spec, version)
        elif ecosystem in ("image", "chart"):
            continue
        else:
            raise fetch.Unreadable(f"{MANIFEST}: TOOL_SOURCE_{key} names {ecosystem!r}, which nothing reads")
        count += read_here
        findings += found
    return Reading("tool packages", count, findings)
