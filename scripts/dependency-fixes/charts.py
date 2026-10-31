"""Charts installed into the cluster: the program each chart installs, read as a Go release, and
the images each chart release renders, judged against the images its later releases render for
the same platforms."""

import cache
import fetch
import goproxy
import packages
import registry
import sbom
import versions
from chartsource import PROGRAMS, charts, index, rendered
from report import NEWER_LINE, PATCH, Finding, Reading, named


def _program(key, version, url, name):
    module, repository, template = PROGRAMS[name]
    releases = index(url, name)
    app = dict(releases).get(version)
    if app is None:
        raise fetch.Unreadable(f"{url} lists no {name} {version}")
    chart_of = {app_version: chart for chart, app_version in releases}

    def move(take):
        return f"move TOOL_VERSION_{key} to {chart_of.get(take, take)}, the chart release installing {take}"

    def requirements(release):
        tag = template.replace("{v}", release.lstrip("v"))
        url_mod = f"https://raw.githubusercontent.com/{repository}/{tag}/go.mod"
        return goproxy.parse_requirements(cache.remember("gomod", url_mod, lambda: fetch.http_text(url_mod)))

    apps = [a for _, a in releases if versions.parse(a) and versions.parse(a).final]
    count, findings = packages.go_module("chart", f"{name} {version}", module, app, move, requirements,
                                         lambda: sorted(set(apps), key=versions.Version))
    for finding in findings:
        if finding.kind == "built from":
            finding.where, finding.fix = f"{name} {version}", chart_of.get(finding.fix, finding.fix)
    return count, findings


def _images(root, key, version, url, name):
    releases = [chart for chart, _ in index(url, name)]
    later = [(versions.newest_patch(releases, version), PATCH), (releases[-1], NEWER_LINE)]
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
