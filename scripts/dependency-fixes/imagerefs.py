"""Every container image reference a tracked file makes, outside the scope list, with each tag's
variables resolved from the manifest and the web lockfile."""

import fnmatch
import json
import os
import re

import fetch
import registry
import repo
import toolrow

SCOPE = os.path.join(os.path.dirname(os.path.abspath(__file__)), "scope.json")
# Official images a reference may name without a registry or a namespace.
OFFICIAL = {"alpine", "busybox", "debian", "golang", "nginx", "node", "postgres", "python", "redis", "ubuntu"}
_REFERENCE = re.compile(
    r"(?<![\w./@:${-])((?:[a-z0-9-]+(?:\.[a-z0-9-]+)+(?::\d+)?/)?[a-z0-9]+(?:[._-][a-z0-9]+)*"
    r"(?:/[a-z0-9]+(?:[._-][a-z0-9]+)*)*):([A-Za-z0-9_$][A-Za-z0-9_.{}$-]*)(@sha256:[0-9a-f]{64})?")
_TAG = re.compile(r"^(v?\d[\w.-]*|latest|\$\{?\w+.*)$")
_VARIABLE = re.compile(r"\$\{?([A-Za-z_][A-Za-z0-9_]*)\}?")
# A string escape such as the \n in a printf separates the word after it from the one before.
_ESCAPE = re.compile(r"\\[nrt]")


def scope():
    """The scope list: files whose references are skipped, and images this project builds."""
    with open(SCOPE, encoding="utf-8") as handle:
        return json.load(handle)


def _candidate(name, tag):
    if not _TAG.match(tag) or (tag.isdigit() and len(tag) >= 4):
        return False
    first, _, rest = name.partition("/")
    if rest and "." in first:
        return "." not in rest
    return "." not in name and ("/" in name or name in OFFICIAL)


def _variables(root):
    """The values a reference's tag may name: the manifest's pins, and the browser image's
    Playwright release, which the e2e stack reads from the web lockfile."""
    values = {f"TOOL_VERSION_{key}": version for key, version, _ in toolrow.rows(root)}
    if "web/package-lock.json" in repo.tracked(root, "web/package-lock.json"):
        packages = json.loads(repo.text(root, "web/package-lock.json")).get("packages") or {}
        playwright = (packages.get("node_modules/@playwright/test") or {}).get("version")
        if playwright:
            values["PLAYWRIGHT_VERSION"] = playwright
    return values


def references(root):
    """Every image reference a tracked file makes outside the scope list: {reference: [files]}."""
    listed, values = scope(), _variables(root)
    skipped = [entry["path"] for entry in listed.get("files", [])]
    owned = [entry["image"] for entry in listed.get("images", [])]
    found = {}
    for path in repo.tracked(root):
        if any(fnmatch.fnmatch(path, pattern) for pattern in skipped):
            continue
        try:
            text = repo.text(root, path)
        except OSError:
            continue
        if "\0" in text:
            continue
        for name, tag, pinned in _REFERENCE.findall(_ESCAPE.sub(" ", text)):
            tag = tag[:-1] if tag.endswith("}") and tag.count("{") < tag.count("}") else tag
            resolved = _VARIABLE.sub(lambda m: values.get(m.group(1), m.group(0)), tag)
            if not _candidate(name, resolved) or any(name.startswith(prefix) for prefix in owned):
                continue
            if "$" in resolved or "{" in resolved:
                raise fetch.Unreadable(f"{path}: the image {name}:{tag} names a value nothing resolves")
            found.setdefault(str(registry.parse(f"{name}:{resolved}{pinned}")), set()).add(path)
    return {reference: sorted(paths) for reference, paths in found.items()}
