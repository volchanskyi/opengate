"""Rust crates as crates.io publishes them: the releases of a crate, and the lockfile a published
binary crate carries, which `cargo install --locked` builds from."""

import functools
import io
import json
import tarfile

import cache
import fetch
import versions

INDEX = "https://index.crates.io"
DOWNLOAD = "https://static.crates.io/crates/{0}/{0}-{1}.crate"


def _index_path(name):
    name = name.lower()
    if len(name) <= 2:
        return f"{len(name)}/{name}"
    if len(name) == 3:
        return f"3/{name[0]}/{name}"
    return f"{name[:2]}/{name[2:4]}/{name}"


@functools.lru_cache(maxsize=None)
def releases(name):
    """Every release of NAME the index lists that is neither yanked nor a pre-release."""
    found = []
    for line in fetch.http_text(f"{INDEX}/{_index_path(name)}").splitlines():
        if not line.strip():
            continue
        entry = fetch.parse_json(line, f"the crates.io index entry of {name}")
        version = versions.parse(entry.get("vers", ""))
        if version and version.final and not entry.get("yanked"):
            found.append(version)
    return [str(v) for v in sorted(found)]


def newest_patch(name, version):
    return versions.newest_patch(releases(name), version)


def latest(name):
    return releases(name)[-1]


def _lockfile(name, version):
    archive = fetch.http_bytes(DOWNLOAD.format(name, version))
    try:
        with tarfile.open(fileobj=io.BytesIO(archive), mode="r:gz") as tar:
            member = tar.extractfile(f"{name}-{version}/Cargo.lock")
            return member.read().decode() if member else ""
    except (tarfile.TarError, KeyError) as error:
        raise fetch.Unreadable(f"{name} {version}: the published crate carries no lockfile") from error


def requirements(name, version):
    """Every registry crate (name, version) NAME's published lockfile at VERSION holds."""
    return parse_lockfile(cache.remember("cargolock", f"{name}@{version}", lambda: _lockfile(name, version)), name)


def parse_lockfile(text, own=""):
    """Every registry crate (name, version) a Cargo.lock holds, OWN left out."""
    found, entry = [], {}
    for line in text.splitlines() + ["[[package]]"]:
        line = line.strip()
        if line == "[[package]]":
            if entry.get("source", "").startswith("registry+") and entry.get("name") != own:
                found.append((entry["name"], entry["version"]))
            entry = {}
        elif "=" in line:
            key, _, value = line.partition("=")
            try:
                entry[key.strip()] = json.loads(value.strip())
            except ValueError:
                continue
    return sorted(set(found))
