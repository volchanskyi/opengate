"""Rust crates: cargo audit over each tracked lockfile, with the fixing release read from the
advisory's record."""

import json
import os
import time

import fetch
import osv
import repo
from report import Finding, Reading

ATTEMPTS = 3


def audit(directory):
    """The audit report for DIRECTORY; cargo audit fetches its database on each run, so a run
    that gives no report is retried."""
    last = "no answer"
    for attempt in range(1, ATTEMPTS + 1):
        try:
            out = fetch.run(["cargo", "audit", "--json"], cwd=directory, ok=(0, 1),
                            what=f"cargo audit in {directory}")
            report = json.loads(out)
            if isinstance((report.get("vulnerabilities") or {}).get("list"), list):
                return report
            last = out[:200]
        except (fetch.Unreadable, ValueError) as error:
            last = str(error)
        if attempt < ATTEMPTS:
            time.sleep(attempt * 5)
    raise fetch.Unreadable(f"cargo audit in {directory} stayed unreadable: {last}")


def workspaces(root):
    """The root manifest of every workspace a tracked Cargo.toml belongs to."""
    found = set()
    for manifest in repo.tracked(root, "*Cargo.toml"):
        out = fetch.run(["cargo", "locate-project", "--workspace", "--message-format", "plain",
                         "--manifest-path", os.path.join(root, manifest)], what=f"the workspace of {manifest}")
        found.add(os.path.relpath(out.strip(), root))
    return sorted(found)


def read(root):
    findings, count = [], 0
    tracked = set(repo.tracked(root, "*Cargo.lock"))
    for manifest in workspaces(root):
        relative = os.path.dirname(manifest) or "."
        lockfile = os.path.join(relative, "Cargo.lock")
        if lockfile not in tracked:
            # A workspace that tracks no lockfile resolves afresh on every build, as this does.
            fetch.run(["cargo", "generate-lockfile", "--manifest-path", os.path.join(root, manifest)],
                      what=f"the lockfile {manifest} resolves to")
        count += repo.text(root, lockfile).count("[[package]]")
        for entry in audit(os.path.join(root, relative))["vulnerabilities"]["list"]:
            advisory, package = entry["advisory"]["id"], entry["package"]
            record = osv.record({"id": advisory})
            fix = osv.fixes("crates.io", package["name"], package["version"], record)
            if fix is None:
                raise fetch.Unreadable(f"cargo audit names {advisory} on {package['name']} "
                                       f"{package['version']}, which its record's ranges do not cover")
            findings.append(Finding(
                "rust", relative, package["name"], package["version"], advisory, fix,
                entry["advisory"].get("title", ""),
                f"cd {relative} && cargo update -p {package['name']} --precise {fix}",
                aliases=osv.aliases(record)))
    return Reading("Rust crates", count, findings)
