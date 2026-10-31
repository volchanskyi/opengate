"""Where the cluster's charts come from: the manifest's chart rows, each repository's index of
releases, and the images a chart release renders."""

import functools
import os

import yaml

import fetch
import toolrow
import versions

# The program each chart installs: its Go module, its repository, and the tag a release carries.
PROGRAMS = {
    "ingress-nginx": ("k8s.io/ingress-nginx", "kubernetes/ingress-nginx", "controller-v{v}"),
    "cert-manager": ("github.com/cert-manager/cert-manager", "cert-manager/cert-manager", "v{v}"),
}


def charts(root):
    """Every (key, chart version, repository url, chart name) the manifest pins."""
    found = []
    for key, version, source in toolrow.rows(root):
        kind, _, spec = source.partition(":")
        if kind == "chart":
            url, _, name = spec.partition("#")
            if name not in PROGRAMS:
                raise fetch.Unreadable(f"the chart {name} installs a program chartsource.PROGRAMS does not name")
            found.append((key, version, url, name))
    return found


@functools.lru_cache(maxsize=None)
def index(url, name):
    """(chart version, app version) for every final release in the repository's index."""
    entries = (yaml.safe_load(fetch.http_text(f"{url.rstrip('/')}/index.yaml")) or {}).get("entries") or {}
    found = [(entry["version"], str(entry.get("appVersion", "")).lstrip("v")) for entry in entries.get(name) or []
             if versions.parse(entry["version"]) and versions.parse(entry["version"]).final]
    if not found:
        raise fetch.Unreadable(f"{url} lists no release of {name}")
    return sorted(found, key=lambda pair: versions.Version(pair[0]))


@functools.lru_cache(maxsize=None)
def rendered(root, url, name, version):
    """Every image the chart release renders, with the values the repository gives it."""
    command = ["helm", "template", name, name, "--repo", url, "--version", version]
    values = os.path.join(root, "deploy", name, "values.yaml")
    if os.path.exists(values):
        command += ["-f", values]
    found = set()
    for document in yaml.safe_load_all(fetch.run(command, what=f"helm template {name} {version}")):
        _collect(document, found)
    return sorted(found)


def _collect(node, found):
    """Adds every string an `image` key holds anywhere under NODE to FOUND."""
    if isinstance(node, dict):
        for key, value in node.items():
            if key == "image" and isinstance(value, str):
                found.add(value)
            else:
                _collect(value, found)
    elif isinstance(node, list):
        for item in node:
            _collect(item, found)
