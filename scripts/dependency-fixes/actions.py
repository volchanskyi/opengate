"""GitHub Actions: every `uses:` in the workflows and shared actions, each resolved through the
GitHub API to the release its commit or moving tag points at."""

import functools
import re

import yaml

import fetch
import osv
import repo
from report import Finding, Reading
from versions import Version

ECOSYSTEM = "GitHub Actions"
_SHA = re.compile(r"^[0-9a-f]{40}$")
_RELEASE = re.compile(r"^v?\d+\.\d+\.\d+$")


def _steps(document):
    """Every `uses:` value in a workflow or a composite action, with where it was written."""
    found = []
    for job in (document.get("jobs") or {}).values():
        if isinstance(job, dict) and job.get("uses"):
            found.append(job["uses"])
        for step in (job.get("steps") or []) if isinstance(job, dict) else []:
            if isinstance(step, dict) and step.get("uses"):
                found.append(step["uses"])
    for step in ((document.get("runs") or {}).get("steps") or []):
        if isinstance(step, dict) and step.get("uses"):
            found.append(step["uses"])
    return found


def references(root):
    """Every external action reference: (owner/repo, ref, file), local and image steps left out."""
    found = []
    files = repo.tracked(root, ".github/workflows/*.yml", ".github/workflows/*.yaml",
                         ".github/actions/*/action.yml", ".github/actions/*/action.yaml")
    for path in files:
        document = yaml.safe_load(repo.text(root, path)) or {}
        for uses in _steps(document):
            if uses.startswith("./") or uses.startswith("docker://"):
                continue
            name, _, ref = uses.partition("@")
            if not ref:
                raise fetch.Unreadable(f"{path}: {uses} names no version")
            found.append(("/".join(name.split("/")[:2]), ref, path))
    if not files:
        raise fetch.Unreadable("no workflow or shared action is tracked")
    return found


@functools.lru_cache(maxsize=None)
def _tags(action):
    return [(tag["name"], tag["commit"]["sha"]) for tag in fetch.github(f"repos/{action}/tags?per_page=100")]


def release(action, ref):
    """The newest release tag pointing where REF points: REF is a commit or a tag."""
    commit = ref if _SHA.match(ref) else fetch.github(f"repos/{action}/commits/{ref}")["sha"]
    named = [Version(name) for name, sha in _tags(action) if sha == commit and _RELEASE.match(name)]
    if not named:
        raise fetch.Unreadable(f"{action}@{ref}: no release tag points at commit {commit}")
    return str(max(named)).lstrip("v")


def read(root):
    found = references(root)
    actions = sorted({action for action, _, _ in found})
    advisories = dict(zip(actions, osv.matches([{"package": {"ecosystem": ECOSYSTEM, "name": a}} for a in actions])))
    findings = []
    for action, ref, path in sorted(set(found)):
        if not advisories[action]:
            continue
        version = release(action, ref)
        for advisory in advisories[action]:
            record = osv.record(advisory)
            fix = osv.fixes(ECOSYSTEM, action, version, record)
            if fix is not None:
                findings.append(Finding("action", path, action, version, record["id"], fix, osv.summary(record),
                                        f"pin {action} in {path} to the commit of release {fix}",
                                        aliases=osv.aliases(record)))
    return Reading("action references", len(found), findings)
