"""Go modules and the standard library: govulncheck over every package the server builds,
tests included, against the database the gauntlet's own scan fetches."""

import os
import re

import fetch
import osv
import repo
import severity
from report import NEWER_LINE, Finding, Reading

# A pseudo-version a commit after tag vX.Y.Z takes: vX.Y.(Z+1)-0.<time>-<hash>.
PSEUDO_AFTER_TAG = re.compile(r"^v(\d+)\.(\d+)\.(\d+)-0\.\d{14}-[0-9a-f]{12}$")


def _directives(text, verb):
    """The argument words of each VERB directive in a go.mod, single-line and block forms."""
    found, block = [], False
    for raw in text.splitlines():
        words = raw.split("//", 1)[0].split()
        if not words:
            continue
        if block:
            if words == [")"]:
                block = False
            else:
                found.append(words)
        elif words[0] == verb and words[1:] == ["("]:
            block = True
        elif words[0] == verb:
            found.append(words[1:])
    return found


def _built_on(version):
    """The release a copy's pseudo-version descends from, or the version itself when it is one."""
    match = PSEUDO_AFTER_TAG.match(version)
    if not match:
        return version
    major, minor, patch = match.groups()
    return f"v{major}.{minor}.{int(patch) - 1}"


def module_copies(text):
    """Every module go.mod replaces with a copy under another path, as (module, release, copy,
    copy version), the release being the one go.mod requires."""
    required = {words[0]: words[1] for words in _directives(text, "require") if len(words) >= 2}
    found = []
    for words in _directives(text, "replace"):
        if "=>" not in words:
            continue
        source, target = words[:words.index("=>")], words[words.index("=>") + 1:]
        if not source or len(target) != 2 or target[0] == source[0]:
            continue
        module, (copy, copy_version) = source[0], target
        release = required.get(module)
        if release is None:
            raise fetch.Unreadable(f"server/go.mod replaces {module} with {copy}, but requires no release of it")
        if _built_on(copy_version) != release:
            raise fetch.Unreadable(f"the copy {copy}@{copy_version} is built on {_built_on(copy_version)}, "
                                   f"but server/go.mod requires {release} of {module}")
        found.append((module, release, copy, copy_version))
    return found


def copies(root):
    """The advisories of each module taken from a copy, read by the module and release the copy is
    built on, since govulncheck looks a replaced module up by the copy's own path."""
    found = module_copies(repo.text(root, "server/go.mod"))
    by_module = {(module, release.lstrip("v")): (copy, version) for module, release, copy, version in found}
    findings = []
    for (_, module, release), record, fix in osv.check_packages(
            [("Go", module, release.lstrip("v")) for module, release, _, _ in found]):
        copy, version = by_module[(module, release)]
        findings.append(Finding(
            "go", f"server, copy {copy}@{version}", module, release, record["id"], fix, osv.summary(record),
            f"rebuild the copy on {module} {fix} and repin its replace in server/go.mod",
            aliases=osv.aliases(record)))
    return findings


def shipped_modules(root):
    """Every module linked into the server binary, the standard library named stdlib."""
    out = fetch.run(["go", "list", "-deps", "-f", "{{if .Module}}{{.Module.Path}}{{else}}stdlib{{end}}",
                     "./cmd/meshserver"], cwd=os.path.join(root, "server"), what="go list the server build")
    modules = {line.strip() for line in out.splitlines() if line.strip()}
    if not modules:
        raise fetch.Unreadable("go list: the server build names no module")
    return modules


def mark_serious(root, findings):
    """Reads each newer-line finding's record for its aliases, and marks the serious ones."""
    newer = [finding for finding in findings if finding.answer == NEWER_LINE]
    if not newer:
        return
    records = {}
    for finding in newer:
        records[finding.advisory] = osv.alias_record(finding.advisory) or {"id": finding.advisory}
        finding.aliases = osv.aliases(records[finding.advisory])
    severity.mark(newer, shipped_modules(root), lambda finding: severity.of(records[finding.advisory]))


def read(root):
    out = fetch.run(["bash", os.path.join(root, "scripts", "govulncheck-scan.sh"), "-scan", "package",
                     "-format", "json"], what="govulncheck")
    messages = fetch.json_stream(out, "govulncheck")
    modules = [message["SBOM"] for message in messages if "SBOM" in message]
    if not modules:
        raise fetch.Unreadable("govulncheck: the report lists no modules")
    count = len(modules[0].get("modules") or []) + 1
    findings = []
    for message in messages:
        finding = message.get("finding")
        if not finding:
            continue
        top = (finding.get("trace") or [{}])[0]
        module, version = top.get("module"), top.get("version")
        if not module or not version:
            raise fetch.Unreadable(f"govulncheck: {finding.get('osv')} names no module and version")
        fix = (finding.get("fixed_version") or "").lstrip("v")
        if module == "stdlib":
            remedy = f"move server/go.mod's toolchain line and every workflow go-version to go{fix}"
        else:
            remedy = f"cd server && go get {module}@v{fix} && go mod tidy"
        findings.append(Finding("go", "server", module, version.lstrip("v"), finding["osv"], fix,
                                remedy=remedy))
    findings += copies(root)
    mark_serious(root, findings)
    return Reading("Go modules", count, findings)
