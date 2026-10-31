"""A tool installed from PyPI: its own advisories, and those on the environment it runs in, each
answered by a reinstall, a later release of the tool, or nothing."""

import fetch
import osv
import pypi
from report import NEWER_LINE, NO_FIX, PATCH, Finding, classify
from toolrow import MANIFEST, label, move, own
from versions import Version


def _answer(key, tool, version, entry, fix):
    """For a dependency whose own patch FIX exists: taken by reinstalling the tool when every
    requirer admits it, by a later release of the tool when only the tool holds it back."""
    blockers = [who for who, spec in entry["asks"] if not pypi.admits(spec, fix)]
    if not blockers:
        return PATCH, fix, f"reinstall {tool} so its environment takes {entry['name']} {fix}"
    if set(blockers) != {pypi.normal(tool)}:
        return NO_FIX, "", ""
    patch = pypi.newest_patch(tool, version)
    if Version(patch) > Version(version) and pypi.admits(pypi.asks_of(tool, patch, entry["name"]), fix):
        return PATCH, patch, move(key, patch)
    newest = pypi.releases(tool)[-1]
    if Version(newest) > Version(version) and pypi.admits(pypi.asks_of(tool, newest, entry["name"]), fix):
        return NEWER_LINE, newest, ""
    return NO_FIX, "", ""


def read(key, spec, version):
    """(packages read, findings) for the tool `spec` names, as `<package>[:<command>]`."""
    tool, _, command = spec.partition(":")
    environment = {entry["name"]: entry for entry in pypi.environment(command or tool, tool)}
    installed = (environment.get(pypi.normal(tool)) or {}).get("version")
    if installed != version:
        raise fetch.Unreadable(f"{command or tool} runs {tool} {installed}, and {MANIFEST} pins {version}")
    deps = [(name, entry["version"]) for name, entry in environment.items() if name != pypi.normal(tool)]
    findings = own(key, "PyPI", tool, version)
    for (_, name, dep_version), record, fix in osv.check_packages([("PyPI", n, v) for n, v in deps]):
        answer, take, remedy = NO_FIX, "", ""
        if fix and classify(dep_version, fix) == PATCH:
            answer, take, remedy = _answer(key, tool, version, environment[name], fix)
        elif fix:
            answer, take = NEWER_LINE, fix
        findings.append(Finding("built from", f"{label(key)} {version}", name, dep_version, record["id"],
                                take, osv.summary(record), remedy, answer, osv.aliases(record),
                                tool if take and take != fix else ""))
    return len(deps) + 1, findings
