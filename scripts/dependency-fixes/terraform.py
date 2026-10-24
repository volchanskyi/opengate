"""Terraform: the CLI the workstation pins and the line the workflows ask for, every provider a
lockfile holds, every tflint plugin named with a version, and the cluster's Kubernetes version.

A provider is the Go program github.com/<namespace>/terraform-provider-<type>, whose releases
the Terraform registry lists and whose module file its tag carries.
"""

import functools
import re

import cache
import fetch
import goproxy
import osv
import packages
import repo
import versions
from report import PATCH, Finding, Reading

CLI = "github.com/hashicorp/terraform"
KUBERNETES = "k8s.io/kubernetes"
_LINE = re.compile(r"(?:terraform_version|TERRAFORM_VERSION):\s*['\"]?(\d+\.\d+)\.x\b")
_PROVIDER = re.compile(r'provider\s+"registry\.terraform\.io/([^/"]+)/([^/"]+)"\s*\{[^}]*?version\s*=\s*"([^"]+)"',
                       re.S)
_PLUGIN = re.compile(r'plugin\s+"([^"]+)"\s*\{([^}]*)\}', re.S)
_KUBERNETES = re.compile(r'variable\s+"oke_kubernetes_version"\s*\{[^}]*?default\s*=\s*"v?([^"]+)"', re.S)


def _cli(root):
    """The CLI at the workstation's pin and at the newest release of each line a workflow names."""
    pinned = repo.tracked(root, "*.terraform-version")
    pins = {repo.text(root, path).strip() for path in pinned}
    lines = {match for path in repo.tracked(root, ".github/workflows/*.yml")
             for match in _LINE.findall(repo.text(root, path))}
    releases = goproxy.releases(CLI)
    for line in sorted(lines):
        newest = [v for v in releases if ".".join(map(str, versions.Version(v).line)) == line]
        if not newest:
            raise fetch.Unreadable(f"the workflows ask for Terraform {line}.x, which has no release")
        pins.add(newest[-1].lstrip("v"))
    count, findings = 0, []
    for version in sorted(pins):
        def move(take):
            return f"move {', '.join(pinned)} to {take}"
        read, found = packages.go_module("terraform", "terraform", CLI, version, move)
        count, findings = count + read, findings + found
    return count, findings


@functools.lru_cache(maxsize=None)
def _provider_releases(namespace, kind):
    data = fetch.http_json(f"https://registry.terraform.io/v1/providers/{namespace}/{kind}/versions")
    found = [versions.parse(entry.get("version", "")) for entry in data.get("versions") or []]
    return [str(v) for v in sorted(v for v in found if v and v.final)]


def _provider_requirements(namespace, kind, version):
    url = f"https://raw.githubusercontent.com/{namespace}/terraform-provider-{kind}/v{version.lstrip('v')}/go.mod"
    return goproxy.parse_requirements(cache.remember("gomod", url, lambda: fetch.http_text(url)))


def _providers(root):
    found = {}
    for path in repo.tracked(root, "*.terraform.lock.hcl"):
        for namespace, kind, version in _PROVIDER.findall(repo.text(root, path)):
            found.setdefault((namespace, kind, version), path)
    for path in repo.tracked(root, "*.tflint.hcl"):
        for name, body in _PLUGIN.findall(repo.text(root, path)):
            version, source = re.search(r'version\s*=\s*"([^"]+)"', body), re.search(r'source\s*=\s*"([^"]+)"', body)
            if version and source:
                found.setdefault(("plugin", source.group(1), version.group(1)), path)
            elif version or source:
                raise fetch.Unreadable(f"{path}: plugin {name} names a version or a source but not both")
    count, findings = 0, []
    for (namespace, kind, version), path in sorted(found.items()):
        def move(take, path=path, name=kind if namespace == "plugin" else f"{namespace}/{kind}"):
            return f"move {name} in {path} to {take}"
        if namespace == "plugin":
            read, found_here = packages.go_module("tflint plugin", kind, kind, version, move)
        else:
            module = f"github.com/{namespace}/terraform-provider-{kind}"
            read, found_here = packages.go_module(
                "provider", f"{namespace}/{kind}", module, version, move,
                requirements=functools.partial(_provider_requirements, namespace, kind),
                releases=functools.partial(_provider_releases, namespace, kind))
        count, findings = count + read, findings + found_here
    return count, findings


def _offered():
    """The Kubernetes versions the cluster service offers, which only an authorised CLI can read."""
    out = fetch.run(["oci", "ce", "cluster-options", "get", "--cluster-option-id", "all",
                     "--query", 'data."kubernetes-versions"', "--raw-output"],
                    what="the Kubernetes versions OKE offers")
    return [v.lstrip("v") for v in fetch.parse_json(out, "the Kubernetes versions OKE offers")]


def _kubernetes(root):
    findings, count = [], 0
    for path in repo.tracked(root, "deploy/terraform/variables.tf"):
        for version in _KUBERNETES.findall(repo.text(root, path)):
            count += 1
            for _, record, fix in osv.check_packages([("Go", KUBERNETES, version)]):
                finding = Finding("kubernetes", path, KUBERNETES, version, record["id"], fix, osv.summary(record),
                                  f"move oke_kubernetes_version in {path} to v{fix}", aliases=osv.aliases(record))
                if finding.answer == PATCH:
                    offered = [v for v in _offered() if versions.Version(v).line == versions.Version(version).line
                               and versions.Version(v) >= versions.Version(fix)]
                    if not offered:
                        finding.answer, finding.summary = "no fix", f"{finding.summary} (OKE offers no {fix} or later)"
                    else:
                        finding.fix = min(offered, key=versions.Version)
                        finding.remedy = f"move oke_kubernetes_version in {path} to v{finding.fix}"
                findings.append(finding)
    if not count:
        raise fetch.Unreadable("deploy/terraform/variables.tf names no oke_kubernetes_version")
    return count, findings


def read(root):
    count, findings = 0, []
    for reader in (_cli, _providers, _kubernetes):
        read_here, found = reader(root)
        count, findings = count + read_here, findings + found
    return Reading("Terraform and Kubernetes packages", count, findings)
