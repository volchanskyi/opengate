"""What every tool reader shares: the manifest's rows, a tool's label, the move that takes a
release, and the tool's own advisories."""

import functools
import re

import fetch
import packages
import repo

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


def label(key):
    """The tool's name as findings show it."""
    return key.lower().replace("_", "-")


def move(key, version):
    """The remedy that takes VERSION of the tool."""
    return f"move TOOL_VERSION_{key} in {MANIFEST}, and every copy the parity sweep holds, to {version}"


def own(key, ecosystem, name, version):
    """The advisories on the tool's own package."""
    return packages.own("tool", label(key), ecosystem, name, version, functools.partial(move, key))
