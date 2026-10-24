"""Charts installed into the cluster: the program each chart installs, read as a Go release, and
the images each chart release renders, judged against the images its later releases render for
the same platforms."""

import functools
import os

import yaml

import cache
import fetch
import goproxy
import packages
import registry
import sbom
import tools
import versions
from report import NEWER_LINE, PATCH, Finding, Reading, named

# The program each chart installs: its Go module, its repository, and the tag a release carries.
PROGRAMS = {
    "ingress-nginx": ("k8s.io/ingress-nginx", "kubernetes/ingress-nginx", "controller-v{v}"),
    "cert-manager": ("github.com/cert-manager/cert-manager", "cert-manager/cert-manager", "v{v}"),
}


def charts(root):
    """Every (key, chart version, repository url, chart name) the manifest pins."""
    found = []
    for key, version, source in tools.rows(root):
        kind, _, spec = source.partition(":")
        if kind == "chart":
            url, _, name = spec.partition("#")
            if name not in PROGRAMS:
                raise fetch.Unreadable(f"the chart {name} installs a program charts.PROGRAMS does not name")
            found.append((key, version, url, name))
    return found


@functools.lru_cache(maxsize=None)
def _index(url, name):
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
    if isinstance(node, dict):
        for key, value in node.items():
            if key == "image" and isinstance(value, str):
                found.add(value)
            else:
                _collect(value, found)
    elif isinstance(node, list):
        for item in node:
            _collect(item, found)


def _program(key, version, url, name):
    module, repository, template = PROGRAMS[name]
    index = _index(url, name)
    app = dict(index).get(version)
    if app is None:
        raise fetch.Unreadable(f"{url} lists no {name} {version}")
    chart_of = {app_version: chart for chart, app_version in index}

    def move(take):
        return f"move TOOL_VERSION_{key} to {chart_of.get(take, take)}, the chart release installing {take}"

    def requirements(release):
        tag = template.replace("{v}", release.lstrip("v"))
        url_mod = f"https://raw.githubusercontent.com/{repository}/{tag}/go.mod"
        return goproxy.parse_requirements(cache.remember("gomod", url_mod, lambda: fetch.http_text(url_mod)))

    apps = [a for _, a in index if versions.parse(a) and versions.parse(a).final]
    count, findings = packages.go_module("chart", f"{name} {version}", module, app, move, requirements,
                                         lambda: sorted(set(apps), key=versions.Version))
    for finding in findings:
        if finding.kind == "built from":
            finding.where, finding.fix = f"{name} {version}", chart_of.get(finding.fix, finding.fix)
    return count, findings


def _images(root, key, version, url, name):
    index = [chart for chart, _ in _index(url, name)]
    later = [(versions.newest_patch(index, version), PATCH), (index[-1], NEWER_LINE)]
    count, findings = 0, []
    for reference in rendered(root, url, name, version):
        image = registry.pinned(registry.parse(reference))
        read, left = sbom.fixable(image)
        count += read
        for chart, answer in later:
            if not left or versions.Version(chart) <= versions.Version(version):
                continue
            twin = next((registry.pinned(registry.parse(r)) for r in rendered(root, url, name, chart)
                         if registry.parse(r).repository == image.repository), None)
            if twin is None or not registry.platforms(image) <= registry.platforms(twin):
                continue
            _, then = sbom.fixable(twin)
            dropped = sorted(k for k in left if k not in then)
            if dropped:
                findings.append(Finding("chart image", f"{name} {version}", image.name, image.tag,
                                        named(dropped), chart,
                                        f"{len(dropped)} findings the chart release no longer carries",
                                        f"move TOOL_VERSION_{key} to {chart}", answer,
                                        tuple(advisory for advisory, _ in dropped), name))
                left = {k: v for k, v in left.items() if k in then}
    return count, findings


def read(root):
    count, findings = 0, []
    for key, version, url, name in charts(root):
        program_read, program_found = _program(key, version, url, name)
        images_read, images_found = _images(root, key, version, url, name)
        count += program_read + images_read
        findings += program_found + images_found
    return Reading("chart packages", count, findings)
