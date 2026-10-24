"""The repository the check reads: its root and the files git tracks in it."""

import pathlib

import fetch


def tracked(root, *patterns):
    """Every tracked path matching PATTERNS, relative to ROOT, in git's order."""
    out = fetch.run(["git", "-C", str(root), "ls-files", "-z", "--", *patterns], what="git ls-files")
    return [path for path in out.split("\0") if path]


def text(root, relative):
    return (pathlib.Path(root) / relative).read_text(encoding="utf-8", errors="replace")
