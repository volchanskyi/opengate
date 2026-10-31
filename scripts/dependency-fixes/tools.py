"""Every pinned tool: each manifest row's source names where its releases and advisories live,
and the packages the tool itself is built from or runs on are read where that source keeps them.

A source is `Go:<module>`, `crates.io:<crate>`, `PyPI:<package>[:<command>]`,
`Maven:<group>:<artifact>`, `git:<owner>/<repo>@<tag with {v}>`, `git-rev:<owner>/<repo>`,
`npm:<package>`, `image:<repository>` or `chart:<repository url>#<chart>`; images and charts are
read by their own readers.
"""

import functools

import crates
import fetch
import packages
import toolgit
import toolnpm
import toolpython
from report import Reading
from toolrow import MANIFEST, label, move, own, rows


def _go(key, module, version):
    return packages.go_module("tool", label(key), module, version, functools.partial(move, key))


def _crate(key, name, version):
    count, carried = packages.carried(
        f"{label(key)} {version}", version, "crates.io", functools.partial(crates.requirements, name),
        functools.partial(crates.newest_patch, name), lambda: crates.latest(name), functools.partial(move, key))
    return count + 1, own(key, "crates.io", name, version) + carried


_READERS = {
    "Go": _go,
    "crates.io": _crate,
    "PyPI": toolpython.read,
    "Maven": lambda key, spec, version: (1, own(key, "Maven", spec, version)),
    "git": toolgit.by_tag,
    "git-rev": toolgit.by_commit,
    "npm": toolnpm.read,
}


def read(root):
    count, findings = 0, []
    for key, version, source in rows(root):
        ecosystem, _, spec = source.partition(":")
        if ecosystem in ("image", "chart"):
            continue
        reader = _READERS.get(ecosystem)
        if reader is None:
            raise fetch.Unreadable(f"{MANIFEST}: TOOL_SOURCE_{key} names {ecosystem!r}, which nothing reads")
        read_here, found = reader(key, spec, version)
        count += read_here
        findings += found
    return Reading("tool packages", count, findings)
