"""A cache for answers that never change once published: a module file or lockfile at a release,
an advisory at its modification time, and the package list of an image digest."""

import hashlib
import json
import os
import pathlib


def directory():
    base = os.environ.get("XDG_CACHE_HOME") or os.path.join(os.path.expanduser("~"), ".cache")
    return pathlib.Path(base) / "opengate" / "dependency-fixes"


def path(kind, key):
    """Where KIND's entry for KEY lives; the key is hashed, so any text names a file."""
    digest = hashlib.sha256(key.encode()).hexdigest()
    return directory() / kind / digest[:2] / digest


def read(kind, key):
    entry = path(kind, key)
    try:
        return json.loads(entry.read_text(encoding="utf-8"))
    except (OSError, ValueError):
        return None


def write(kind, key, value):
    """Stores VALUE whole or not at all: it is written beside its place and renamed into it."""
    entry = path(kind, key)
    entry.parent.mkdir(parents=True, exist_ok=True)
    partial = entry.with_suffix(f".{os.getpid()}.part")
    partial.write_text(json.dumps(value), encoding="utf-8")
    partial.replace(entry)
    return value


def remember(kind, key, produce):
    """KIND's entry for KEY, produced and stored when absent."""
    found = read(kind, key)
    return found if found is not None else write(kind, key, produce())
