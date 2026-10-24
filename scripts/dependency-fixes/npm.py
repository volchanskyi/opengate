"""npm packages: each tracked lockfile's audit, with each copy's version read from the lockfile
and the fixing release read from the advisory's record."""

import json
import os
import tempfile
import time

import fetch
import osv
import repo
from report import Finding, Reading

ATTEMPTS = 3


def audit(directory):
    """The audit report for DIRECTORY, retried while it cannot be read."""
    last = "no answer"
    for attempt in range(1, ATTEMPTS + 1):
        out = fetch.run(["npm", "audit", "--json"], cwd=directory, ok=(0, 1), what=f"npm audit in {directory}")
        try:
            report = json.loads(out)
        except ValueError:
            report = {}
        if isinstance(report.get("vulnerabilities"), dict) and "error" not in report:
            return report
        last = (report.get("error") or {}).get("summary") or out[:200]
        if attempt < ATTEMPTS:
            time.sleep(attempt * 5)
    raise fetch.Unreadable(f"npm audit in {directory} stayed unreadable: {last}")


def findings(directory, label, packages, remedy):
    """The audit's advisories on DIRECTORY's tree, each copy's version read from PACKAGES."""
    found = []
    for entry in audit(directory)["vulnerabilities"].values():
        for via in entry.get("via") or []:
            if not isinstance(via, dict):
                continue
            advisory = (via.get("url") or "").rsplit("/", 1)[-1]
            if not advisory:
                raise fetch.Unreadable(f"npm audit in {label}: an advisory of {via.get('name')} names no id")
            record = osv.record({"id": advisory})
            for node in entry.get("nodes") or []:
                version = (packages.get(node) or {}).get("version")
                if not version:
                    raise fetch.Unreadable(f"{label}: {node} has no version")
                fix = osv.fixes("npm", via["name"], version, record)
                if fix is None:
                    raise fetch.Unreadable(f"npm audit names {advisory} on {via['name']} {version}, "
                                           "which the advisory's own ranges do not cover")
                found.append(Finding("npm", label, via["name"], version, advisory, fix, osv.summary(record),
                                     remedy(via["name"]), aliases=osv.aliases(record)))
    return found


def read(root):
    found, count = [], 0
    for lockfile in repo.tracked(root, "*package-lock.json"):
        relative = os.path.dirname(lockfile) or "."
        packages = json.loads(repo.text(root, lockfile)).get("packages") or {}
        count += sum(1 for key in packages if key)
        found += findings(os.path.join(root, relative), relative, packages,
                          lambda name, relative=relative: f"cd {relative} && npm update {name} --package-lock-only")
    return Reading("npm packages", count, found)


def resolved(package, version):
    """The packages an install of PACKAGE at VERSION resolves today, in a scratch directory."""
    work = tempfile.mkdtemp(prefix="dependency-fixes-npm-")
    with open(os.path.join(work, "package.json"), "w", encoding="utf-8") as handle:
        json.dump({"name": "probe", "version": "0.0.0", "private": True,
                   "dependencies": {package: version}}, handle)
    fetch.run(["npm", "install", "--package-lock-only", "--ignore-scripts", "--no-audit", "--no-fund"],
              cwd=work, what=f"the packages {package} {version} resolves")
    with open(os.path.join(work, "package-lock.json"), encoding="utf-8") as handle:
        return work, json.load(handle).get("packages") or {}
